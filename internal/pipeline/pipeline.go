// Package pipeline drives the seven review stages of spec 6.5. Go decides
// the order, launches the agents, each with its role's model, and applies
// the fixed rules: validation outcomes, judges, Group B filter, minor cap and
// verdict. The models only answer what each stage asks them.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/prechecks"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/repocache"
	"github.com/LeanMendez/pr-review/internal/rules"
)

// AgentRunner runs one agent and returns its structured output. The caller
// limits how many run at once (agent.Runner does it with its semaphore).
type AgentRunner interface {
	Run(ctx context.Context, s agent.Spec, onActivity func(agent.Activity)) (json.RawMessage, error)
}

var _ AgentRunner = agent.Runner{}

// Deps are the pipeline collaborators. A nil Now means time.Now.
type Deps struct {
	Cfg    config.Config
	Agents AgentRunner
	Now    func() time.Time
}

// Request is one review: the PR context, its diff (fetched without the
// generated files), the head and base worktrees and the mode.
type Request struct {
	Repo  string
	PR    prb.PRContext
	Diff  prb.Diff
	Trees repocache.Worktrees
	Mode  Mode
}

// Result is the outcome of a review.
type Result struct {
	Repo              string
	PR                prb.PR
	Mode              Mode
	Verdict           finding.Verdict
	IncompleteWarning bool
	Phrase            string
	Findings          []finding.Finding
	Doubts            []finding.Doubt
	Split             []finding.SplitPart
	Excluded          []string
	Unreviewed        []string
	RuleFiles         []rules.RuleFile
	FailedAgents      []string
	Failures          []Failure
	AgentCount        int
	Started           time.Time
	Finished          time.Time
}

// Failure is a failed or timed-out agent and what the review lost with it,
// so the report can say so even when the synthesis failed too.
type Failure struct {
	Agent     string   // agent name, as in FailedAgents
	Role      string   // prompt role: resumen, superior_plan, bugs, validador_bugs, juez...
	Files     []string // a specialist's files, or the file of a validator's candidate
	Candidate string   // validators: the ID of the finding left sin_confirmar
	Reason    string   // the cause: "se venció el tiempo", the process error or "salida inválida: ..."
}

// Agent names of the single-instance roles. Specialists are named after
// their role (role-N when the plan splits it in groups), validators after
// their role and the candidate ID.
const (
	nameResumen  = "resumen"
	namePlan     = "superior_plan"
	nameSintesis = "superior_sintesis"
)

// reasonFiltrado is the reason of a Group B finding the filter hides.
const reasonFiltrado = "Grupo B sin hallazgo del Grupo A en el mismo archivo o función"

// specialistRole is a specialist's prompt role and the config role that
// picks its model.
type specialistRole struct {
	prompt string
	cfg    config.Role
}

// specialistRoles are the specialists in report order.
var specialistRoles = []specialistRole{
	{prompts.RoleBugs, config.RoleBugs},
	{prompts.RoleSeguridad, config.RoleSeguridad},
	{prompts.RoleReglas, config.RoleReglas},
	{prompts.RoleTests, config.RoleTests},
}

// Run executes the stages of req.Mode in order and returns the review. A
// failed or timed-out agent never stops the others nor the review: it lands
// in FailedAgents and Failures and makes the review incomplete. Findings are
// never dropped: the ones the rules set aside keep a status that says why.
// Run only fails for an unknown mode, unreadable rules or a cancelled ctx.
//
// Events go to events when it is not nil; the caller must drain it. Run
// sends nothing after it returns and never closes it.
func Run(ctx context.Context, d Deps, req Request, events chan<- Event) (Result, error) {
	mode, err := ParseMode(string(req.Mode))
	if err != nil {
		return Result{}, err
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	r := &run{ctx: ctx, d: d, req: req, mode: mode, events: events, ids: map[string]int{}, sources: map[string][]prb.DiffFile{}}
	return r.review()
}

// run is the state of one review. Agents run in goroutines but only read
// it; every write happens in Run's goroutine, between agent batches.
type run struct {
	ctx    context.Context
	d      Deps
	req    Request
	mode   Mode
	events chan<- Event

	pre      prechecks.Result
	files    []prb.DiffFile // what the agents review: the diff minus the excluded files
	rules    rules.RuleSet
	summary  string
	trivial  []string
	jobs     []job
	findings []finding.Finding
	sources  map[string][]prb.DiffFile // finding ID → the patches it came from
	doubts   []finding.Doubt
	failed   []failure
	phrase   string
	split    []finding.SplitPart
	ids      map[string]int // role → last finding number
	agents   atomic.Int32
}

// job is one specialist instance and the files it reviews.
type job struct {
	name  string
	role  string
	cfg   config.Role
	files []prb.DiffFile
}

// failure is a failed agent and the line that tells the synthesis what the
// review lost because of it.
type failure struct {
	Failure
	detail string
}

func (r *run) review() (Result, error) {
	res := Result{Repo: r.req.Repo, PR: r.req.PR.PullRequest, Mode: r.mode, Started: r.d.Now()}
	steps := []struct {
		stage Stage
		fn    func() error
	}{
		{StagePrechecks, r.prechecks},
		{StageRules, r.loadRules},
		{StagePlan, r.plan},
		{StageSpecialists, r.specialists},
		{StageValidation, r.validate},
		{StageJudges, r.judge},
		{StageReport, r.synthesize},
	}
	stages := Stages(r.mode)
	for _, st := range steps {
		if !slices.Contains(stages, st.stage) {
			r.emit(Event{Kind: StageSkipped, Stage: st.stage})
			continue
		}
		ev := Event{Kind: StageStarted, Stage: st.stage}
		if st.stage == StageValidation {
			ev.Candidates = len(r.candidates())
		}
		r.emit(ev)
		if err := st.fn(); err != nil {
			return Result{}, err
		}
		if err := r.ctx.Err(); err != nil {
			return Result{}, err
		}
		ev.Kind = StageDone
		r.emit(ev)
	}

	for _, f := range r.failed {
		res.FailedAgents = append(res.FailedAgents, f.Agent)
		res.Failures = append(res.Failures, f.Failure)
	}
	incomplete := len(r.failed) > 0 || len(r.pre.Unreviewed) > 0
	res.Verdict, res.IncompleteWarning = finding.ComputeVerdict(r.findings, incomplete)
	res.Phrase, res.Split = r.phrase, r.split
	res.Findings, res.Doubts = r.findings, r.doubts
	res.Excluded, res.Unreviewed = r.pre.Excluded, r.pre.Unreviewed
	res.RuleFiles = r.rules.Files
	res.AgentCount = int(r.agents.Load())
	res.Finished = r.d.Now()
	return res, nil
}

// Stage 1: deterministic checks. The diff comes without the generated files
// (prb --exclude), so those, and the lockfile changes among them, are only
// visible in the PR diffstat. A non-generated file of the diffstat that the
// diff does not bring at all was never reviewed.
func (r *run) prechecks() error {
	pre := prechecks.Run(r.req.PR.PullRequest, r.req.Diff, r.d.Cfg)
	if fs := r.req.PR.Files; fs != nil {
		stat := prechecks.Summarize(fs.Files, r.d.Cfg)
		pre.Excluded = union(pre.Excluded, stat.Excluded)
		pre.Sensitive = union(pre.Sensitive, stat.Sensitive)
		inDiff := map[string]bool{}
		for _, p := range slices.Concat(paths(r.req.Diff.Files), r.req.Diff.OmittedFiles) {
			inDiff[p] = true
		}
		var missing []string
		for _, f := range fs.Files {
			if !inDiff[f.Path] && !prechecks.IsGenerated(f.Path) {
				missing = append(missing, f.Path)
			}
		}
		pre.Unreviewed = union(pre.Unreviewed, missing)
	}
	excluded := map[string]bool{}
	for _, p := range pre.Excluded {
		excluded[p] = true
	}
	for _, f := range r.req.Diff.Files {
		if !excluded[f.Path] {
			r.files = append(r.files, f)
		}
	}
	// Secrets are critico candidates for validador_bugs; process findings
	// come out confirmado.
	for i := range pre.Findings {
		f := &pre.Findings[i]
		f.ID = r.newID(f.Role)
		r.sources[f.ID] = r.filesAt(f.File)
	}
	r.pre = pre
	r.findings = slices.Clone(pre.Findings)
	return nil
}

// Stage 2: the rules come from base, so a PR cannot change the rules it is
// judged by.
func (r *run) loadRules() error {
	changed := slices.Concat(paths(r.files), r.pre.Unreviewed)
	rs, err := rules.Load(r.req.Trees.Base, changed, rules.DefaultLimits())
	if err != nil {
		return err
	}
	r.rules = rs
	return nil
}

// input is what every agent gets; callers narrow Files and add the rest.
func (r *run) input() prompts.Input {
	return prompts.Input{
		PR:        r.req.PR.PullRequest,
		Files:     r.files,
		Prechecks: r.pre,
		Rules:     r.rules,
		Summary:   r.summary,
		HeadDir:   r.req.Trees.Head,
		BaseDir:   r.req.Trees.Base,
	}
}

type summaryOut struct {
	Summary string   `json:"resumen"`
	Trivial []string `json:"triviales"`
}

type planOut struct {
	Specialists []struct {
		Role   string     `json:"rol"`
		Groups [][]string `json:"grupos"`
	} `json:"especialistas"`
}

// Stage 3: the summary and, but in ligero, the superior's plan. Ligero
// always runs bugs and reglas over every file. When the plan fails or
// assigns nothing while there are files to review, it counts as failed and
// the four specialists review every file.
func (r *run) plan() error {
	if out, err := call[summaryOut](r, StagePlan, nameResumen, prompts.RoleResumen, config.RoleResumen, r.input()); err != nil {
		r.fail(Failure{Agent: nameResumen, Role: prompts.RoleResumen, Reason: reasonFor(err)},
			"falló el agente resumen: los demás agentes trabajaron sin el resumen inicial")
	} else {
		r.summary, r.trivial = strings.TrimSpace(out.Summary), out.Trivial
	}

	if r.mode == ModeLigero {
		r.jobs = r.fullPlan(prompts.RoleBugs, prompts.RoleReglas)
		return nil
	}
	in := r.input()
	in.Summary = withTrivial(r.summary, r.trivial)
	out, err := call[planOut](r, StagePlan, namePlan, prompts.RoleSuperiorPlan, config.RoleSuperior, in)
	var jobs []job
	if err == nil {
		jobs = r.jobsFrom(out)
	}
	if err != nil || (len(jobs) == 0 && len(r.files) > 0) {
		reason := "el plan no asignó ningún archivo revisable"
		if err != nil {
			reason = reasonFor(err)
		}
		r.fail(Failure{Agent: namePlan, Role: prompts.RoleSuperiorPlan, Reason: reason},
			"falló el agente superior_plan: los cuatro especialistas revisaron todos los archivos")
		jobs = r.fullPlan(prompts.RoleBugs, prompts.RoleSeguridad, prompts.RoleReglas, prompts.RoleTests)
	}
	r.jobs = jobs
	return nil
}

// withTrivial appends the files the summary marked trivial. They travel with
// the summary, inside the untrusted data, and only the plan sees them: Go
// never drops a file because of them.
func withTrivial(summary string, trivial []string) string {
	if len(trivial) == 0 {
		return summary
	}
	var b strings.Builder
	b.WriteString(summary)
	b.WriteString("\n\nArchivos que el resumen marcó como triviales:\n")
	for _, p := range trivial {
		fmt.Fprintf(&b, "- %q\n", p)
	}
	return strings.TrimSpace(b.String())
}

// fullPlan is one instance per role over every reviewable file.
func (r *run) fullPlan(roles ...string) []job {
	if len(r.files) == 0 {
		return nil
	}
	var jobs []job
	for _, s := range specialistRoles {
		if slices.Contains(roles, s.prompt) {
			jobs = append(jobs, job{name: s.prompt, role: s.prompt, cfg: s.cfg, files: r.files})
		}
	}
	return jobs
}

// jobsFrom turns the plan into specialist instances, one per group. Plan
// paths are normalized and matched against the reviewable files; unknown
// ones are dropped, and so are empty groups. Go enforces the plan's own
// coverage rule, so a plan cannot leave code unread: every reviewable file
// the plan left out of bugs or reglas gets an extra instance of that role.
// A plan that assigns nothing yields no jobs.
func (r *run) jobsFrom(p planOut) []job {
	byPath := map[string]prb.DiffFile{}
	for _, f := range r.files {
		byPath[f.Path] = f
	}
	groups := map[string][][]prb.DiffFile{}
	assigned := map[string]map[string]bool{} // role → paths in its groups
	for _, s := range p.Specialists {
		if !slices.ContainsFunc(specialistRoles, func(sr specialistRole) bool { return sr.prompt == s.Role }) {
			continue
		}
		for _, g := range s.Groups {
			var fs []prb.DiffFile
			for _, raw := range g {
				f, ok := byPath[planPath(raw)]
				if ok && !slices.ContainsFunc(fs, func(x prb.DiffFile) bool { return x.Path == f.Path }) {
					fs = append(fs, f)
				}
			}
			if len(fs) == 0 {
				continue
			}
			groups[s.Role] = append(groups[s.Role], fs)
			if assigned[s.Role] == nil {
				assigned[s.Role] = map[string]bool{}
			}
			for _, f := range fs {
				assigned[s.Role][f.Path] = true
			}
		}
	}
	if len(groups) == 0 {
		return nil
	}
	for _, role := range []string{prompts.RoleBugs, prompts.RoleReglas} {
		var rest []prb.DiffFile
		for _, f := range r.files {
			if !assigned[role][f.Path] {
				rest = append(rest, f)
			}
		}
		if len(rest) > 0 {
			groups[role] = append(groups[role], rest)
		}
	}
	var jobs []job
	for _, s := range specialistRoles {
		gs := groups[s.prompt]
		for i, fs := range gs {
			name := s.prompt
			if len(gs) > 1 {
				name = fmt.Sprintf("%s-%d", s.prompt, i+1)
			}
			jobs = append(jobs, job{name: name, role: s.prompt, cfg: s.cfg, files: fs})
		}
	}
	return jobs
}

type specialistOut struct {
	Findings []finding.Finding `json:"hallazgos"`
	Doubts   []finding.Doubt   `json:"dudas"`
}

// Stage 4: the specialists, in parallel. Critico and alto findings are
// candidates for stage 5, always in Group A (a grave finding is never minor
// quality); medio and bajo are not validated one by one.
func (r *run) specialists() error {
	outs := make([]specialistOut, len(r.jobs))
	errs := make([]error, len(r.jobs))
	var g errgroup.Group // no shared context: one failure never cancels the others
	for i, j := range r.jobs {
		in := r.input()
		in.Files = j.files
		g.Go(func() error {
			outs[i], errs[i] = call[specialistOut](r, StageSpecialists, j.name, j.role, j.cfg, in)
			return nil
		})
	}
	_ = g.Wait()

	for i, j := range r.jobs {
		if errs[i] != nil {
			r.fail(Failure{Agent: j.name, Role: j.role, Files: paths(j.files), Reason: reasonFor(errs[i])},
				fmt.Sprintf("falló el agente %s (%s) y no revisó: %s", j.name, j.role, quoted(paths(j.files))))
			continue
		}
		for _, f := range outs[i].Findings {
			f.ID, f.Role = r.newID(j.role), j.role
			f.Status = finding.StatusConfirmado
			if f.Severity.Grave() {
				f.Status, f.Group = finding.StatusCandidato, finding.GroupA
			}
			f.FixStatus, f.Reason, f.Votes = "", "", nil
			r.sources[f.ID] = j.files
			r.findings = append(r.findings, f)
		}
		r.doubts = append(r.doubts, outs[i].Doubts...)
	}
	return nil
}

type validatorOut struct {
	Result   string         `json:"resultado"`
	Origin   finding.Origin `json:"origen"`
	Evidence []string       `json:"evidencia"`
	Reason   string         `json:"motivo"`
}

// candidates returns the indexes of the findings waiting for a validator.
func (r *run) candidates() []int {
	var idx []int
	for i, f := range r.findings {
		if f.Status == finding.StatusCandidato {
			idx = append(idx, i)
		}
	}
	return idx
}

// Stage 5: one validator per candidate, in parallel: validador_reglas for
// the reglas candidates, validador_bugs for the rest (bugs, seguridad, tests
// and the precheck secrets). Group B is filtered afterwards, against the
// confirmed Group A findings.
func (r *run) validate() error {
	idx := r.candidates()
	names, promptRoles := make([]string, len(idx)), make([]string, len(idx))
	outs := make([]validatorOut, len(idx))
	errs := make([]error, len(idx))
	var g errgroup.Group
	for k, i := range idx {
		c := r.findings[i]
		promptRole, role := prompts.RoleValidadorBugs, config.RoleValidadorBugs
		if c.Role == prompts.RoleReglas {
			promptRole, role = prompts.RoleValidadorReglas, config.RoleValidadorReglas
		}
		names[k], promptRoles[k] = promptRole+"-"+c.ID, promptRole
		in := r.input()
		in.Files, in.Candidate = r.sources[c.ID], &c
		g.Go(func() error {
			outs[k], errs[k] = call[validatorOut](r, StageValidation, names[k], promptRole, role, in)
			return nil
		})
	}
	_ = g.Wait()

	for k, i := range idx {
		f := &r.findings[i]
		if errs[k] != nil {
			f.Status, f.Reason = finding.StatusSinConfirmar, "no se pudo validar: falló el validador"
			r.fail(Failure{Agent: names[k], Role: promptRoles[k], Files: []string{f.File}, Candidate: f.ID, Reason: reasonFor(errs[k])},
				fmt.Sprintf("falló el agente %s: el candidato %s en %q quedó sin confirmar", names[k], f.ID, f.File))
			continue
		}
		applyValidation(f, outs[k])
	}
	r.filterGroupB()
	return nil
}

// applyValidation applies spec 6.9: the validator decides the status and the
// origin. A refutation without evidence does not count: it stays unconfirmed.
func applyValidation(f *finding.Finding, v validatorOut) {
	if validOrigin(v.Origin) {
		f.Origin = v.Origin
	}
	f.Reason = strings.TrimSpace(v.Reason)
	switch {
	case v.Result == string(finding.StatusConfirmado):
		f.Status = finding.StatusConfirmado
		for _, e := range v.Evidence {
			if e = strings.TrimSpace(e); e != "" && !slices.Contains(f.Evidence, e) {
				f.Evidence = append(f.Evidence, e)
			}
		}
	case v.Result == string(finding.StatusRefutado) && hasEvidence(v.Evidence):
		f.Status = finding.StatusRefutado
	case v.Result == string(finding.StatusRefutado):
		f.Status = finding.StatusSinConfirmar
		f.Reason = "el validador lo refutó sin evidencia: " + f.Reason
	default: // sin_resolver
		f.Status = finding.StatusSinConfirmar
	}
}

func validOrigin(o finding.Origin) bool {
	switch o {
	case finding.OriginIntroducido, finding.OriginActivado, finding.OriginEmpeorado, finding.OriginPreexistente, finding.OriginIncierto:
		return true
	}
	return false
}

func hasEvidence(ev []string) bool {
	return slices.ContainsFunc(ev, func(e string) bool { return strings.TrimSpace(e) != "" })
}

type judgeOut struct {
	Votes []finding.JudgeVote `json:"votos"`
}

// Stage 6: two judges, in parallel and blind to each other, over the
// confirmed and unconfirmed findings that are not preexisting. Each finding
// takes the votes with its ID; a missing vote is a zero vote. Group B is
// filtered again, since the judges may have discarded its anchor.
func (r *run) judge() error {
	var idx []int
	var judged []finding.Finding
	for i, f := range r.findings {
		if (f.Status == finding.StatusConfirmado || f.Status == finding.StatusSinConfirmar) && f.Origin != finding.OriginPreexistente {
			idx = append(idx, i)
			judged = append(judged, f)
		}
	}
	if len(judged) == 0 {
		return nil
	}
	judges := []struct {
		name, label string
		role        config.Role
	}{{"juez_a", "A", config.RoleJuezA}, {"juez_b", "B", config.RoleJuezB}}

	in := r.input()
	in.Findings = judged
	outs := make([]judgeOut, len(judges))
	errs := make([]error, len(judges))
	var g errgroup.Group
	for k, j := range judges {
		g.Go(func() error {
			outs[k], errs[k] = call[judgeOut](r, StageJudges, j.name, prompts.RoleJuez, j.role, in)
			return nil
		})
	}
	_ = g.Wait()

	votes := make([]map[string]finding.JudgeVote, len(judges))
	for k, j := range judges {
		votes[k] = map[string]finding.JudgeVote{}
		if errs[k] != nil {
			r.fail(Failure{Agent: j.name, Role: prompts.RoleJuez, Reason: reasonFor(errs[k])},
				fmt.Sprintf("falló el agente %s: sus votos no cuentan", j.name))
			continue
		}
		for _, v := range outs[k].Votes {
			if _, seen := votes[k][v.FindingID]; !seen {
				v.Judge = j.label
				votes[k][v.FindingID] = v
			}
		}
	}
	for _, i := range idx {
		id := r.findings[i].ID
		r.findings[i] = finding.ApplyJudges(r.findings[i], votes[0][id], votes[1][id])
	}
	r.filterGroupB()
	return nil
}

// filterGroupB applies the Group B filter without dropping anything: a shown
// Group B finding with no confirmed Group A finding in its file or function
// becomes filtrado, with the reason, and stays for findings.json. Refuted and
// discarded findings keep their own status.
func (r *run) filterGroupB() {
	kept := map[string]bool{}
	for _, f := range finding.FilterGroupB(r.findings) {
		kept[f.ID] = true
	}
	for i := range r.findings {
		f := &r.findings[i]
		switch f.Status {
		case finding.StatusConfirmado, finding.StatusSinConfirmar, finding.StatusMenor:
		default:
			continue
		}
		if kept[f.ID] {
			continue
		}
		f.Status, f.Reason = finding.StatusFiltrado, reasonFiltrado
	}
}

type synthesisOut struct {
	Phrase string `json:"frase"`
	Split  struct {
		Needed bool                `json:"necesaria"`
		Parts  []finding.SplitPart `json:"partes"`
	} `json:"division"`
}

// Stage 7: the minor cap, then the synthesis. It writes the phrase and, only
// for oversize PRs, the split; it never decides the verdict.
func (r *run) synthesize() error {
	r.capMinor()
	in := r.input()
	in.Findings = r.findings
	in.Incomplete = r.incomplete()
	out, err := call[synthesisOut](r, StageReport, nameSintesis, prompts.RoleSuperiorSintesis, config.RoleSuperior, in)
	if err != nil {
		r.fail(Failure{Agent: nameSintesis, Role: prompts.RoleSuperiorSintesis, Reason: reasonFor(err)},
			"falló el agente superior_sintesis")
		return nil
	}
	r.phrase = strings.TrimSpace(out.Phrase)
	if r.pre.Oversize && out.Split.Needed {
		r.split = out.Split.Parts
	}
	return nil
}

// capMinor keeps MaxMinorDetails of the shown (confirmado or sin_confirmar)
// bajo findings and sends the rest to the minor details. Slots go first to
// the Group A findings from the PR, then to the rest of Group A (uncertain
// origin), then to Group B, each in order; preexisting findings belong to
// the grey section and never take one. So nothing that does not count can
// push a finding that counts out of the verdict.
func (r *run) capMinor() {
	rank := func(f finding.Finding) int {
		switch {
		case f.Origin == finding.OriginPreexistente:
			return -1
		case f.Group == finding.GroupA && f.Origin.FromPR():
			return 0
		case f.Group == finding.GroupA:
			return 1
		}
		return 2
	}
	slots := r.d.Cfg.Thresholds.MaxMinorDetails
	for want := 0; want <= 2; want++ {
		for i := range r.findings {
			f := &r.findings[i]
			if f.Severity != finding.SeverityBajo || rank(*f) != want ||
				(f.Status != finding.StatusConfirmado && f.Status != finding.StatusSinConfirmar) {
				continue
			}
			if slots > 0 {
				slots--
				continue
			}
			f.Status = finding.StatusMenor
		}
	}
}

// incomplete lists what the review did not cover, one line each: the failed
// agents and the non-generated files without a patch. Empty means complete.
func (r *run) incomplete() []string {
	var out []string
	for _, f := range r.failed {
		out = append(out, f.detail)
	}
	for _, p := range r.pre.Unreviewed {
		out = append(out, fmt.Sprintf("%q quedó sin revisar: el diff no trajo su patch", p))
	}
	return out
}

// call runs one agent and decodes its structured output. Any failure
// (prompt, run, timeout or invalid output) is reported as AgentFailed and
// returned; the caller records it.
func call[T any](r *run, stage Stage, name, promptRole string, role config.Role, in prompts.Input) (T, error) {
	var out T
	a := r.d.Cfg.AssignmentFor(role)
	ev := Event{Stage: stage, Agent: name, Role: role, Model: a.Model}
	send := func(kind EventKind, text string, err error) {
		e := ev
		e.Kind, e.Text, e.Err = kind, text, err
		r.emit(e)
	}

	r.agents.Add(1)
	send(AgentStarted, "", nil)
	prompt, err := prompts.User(promptRole, in)
	if err != nil {
		send(AgentFailed, "", err)
		return out, err
	}
	raw, err := r.d.Agents.Run(r.ctx, agent.Spec{
		Name:       name,
		Role:       promptRole,
		Assignment: a,
		System:     prompts.System(promptRole),
		Prompt:     prompt,
		Schema:     prompts.Schema(promptRole),
		Dir:        r.req.Trees.Head,
		AddDirs:    []string{r.req.Trees.Base},
	}, func(act agent.Activity) { send(AgentActivity, act.Text, nil) })
	if err == nil {
		if derr := json.Unmarshal(raw, &out); derr != nil {
			err = fmt.Errorf("salida inválida: %w", derr)
		}
	}
	if err != nil {
		send(AgentFailed, "", err)
		return out, err
	}
	send(AgentDone, "", nil)
	return out, nil
}

// emit sends an event unless there is no channel or the review was cancelled.
func (r *run) emit(e Event) {
	if r.events == nil {
		return
	}
	select {
	case r.events <- e:
	case <-r.ctx.Done():
	}
}

// fail records a failed agent: f for the report, detail for the synthesis.
func (r *run) fail(f Failure, detail string) {
	r.failed = append(r.failed, failure{f, detail})
}

// reasonFor is the cause of an agent failure, for the report.
func reasonFor(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "se venció el tiempo"
	}
	var ae *agent.AgentError
	if errors.As(err, &ae) {
		err = ae.Err
	}
	return err.Error()
}

// planPath normalizes a path written by the plan: no surrounding spaces,
// quotes or backticks, no "./" nor redundant separators.
func planPath(p string) string {
	return path.Clean(strings.Trim(strings.TrimSpace(p), "\"'`"))
}

// newID returns the next finding ID of a role: <rol>-<NNN>.
func (r *run) newID(role string) string {
	r.ids[role]++
	return fmt.Sprintf("%s-%03d", role, r.ids[role])
}

// filesAt returns the reviewable file at p, if any.
func (r *run) filesAt(p string) []prb.DiffFile {
	for _, f := range r.files {
		if f.Path == p {
			return []prb.DiffFile{f}
		}
	}
	return nil
}

func paths(fs []prb.DiffFile) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Path
	}
	return out
}

func quoted(ps []string) string {
	q := make([]string, len(ps))
	for i, p := range ps {
		q[i] = fmt.Sprintf("%q", p)
	}
	return strings.Join(q, ", ")
}

// union returns a followed by the elements of b it lacks, without repeats.
func union(a, b []string) []string {
	var out []string
	for _, s := range slices.Concat(a, b) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}
