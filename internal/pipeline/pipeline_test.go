package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/agent"
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/prompts"
	"github.com/LeanMendez/pr-review/internal/repocache"
	"github.com/LeanMendez/pr-review/internal/rules"
)

// replyFunc lets a test compute an agent's reply, for example to wait for
// another agent.
type replyFunc func(ctx context.Context, s agent.Spec) (any, error)

// fakeAgents answers by Spec.Name, then by Spec.Role, then with the default
// of the role, and records every spec it receives. A reply of type error
// fails the agent.
type fakeAgents struct {
	replies map[string]any

	mu    sync.Mutex
	specs []agent.Spec
}

func newFake(replies map[string]any) *fakeAgents { return &fakeAgents{replies: replies} }

const sintesisFrase = "En los cambios revisados no apareció nada importante."

var defaultReplies = map[string]any{
	prompts.RoleResumen:          map[string]any{"resumen": "Agrega el endpoint de pagos.", "triviales": []string{}},
	prompts.RoleSuperiorPlan:     planReply(map[string][][]string{"bugs": {{"src/a.go"}}, "seguridad": {{"src/a.go"}}, "reglas": {{"src/a.go"}}, "tests": {{"src/a.go"}}}),
	prompts.RoleBugs:             specialistReply(),
	prompts.RoleSeguridad:        specialistReply(),
	prompts.RoleReglas:           specialistReply(),
	prompts.RoleTests:            specialistReply(),
	prompts.RoleValidadorBugs:    validatorReply("confirmado", "introducido", "src/a.go:3"),
	prompts.RoleValidadorReglas:  validatorReply("confirmado", "introducido", "src/a.go:3"),
	prompts.RoleJuez:             map[string]any{"votos": []any{}},
	prompts.RoleSuperiorSintesis: synthesisReply(false),
}

func (f *fakeAgents) Run(ctx context.Context, s agent.Spec, on func(agent.Activity)) (json.RawMessage, error) {
	f.mu.Lock()
	f.specs = append(f.specs, s)
	f.mu.Unlock()
	if on != nil {
		on(agent.Activity{Agent: s.Name, Text: "Leyendo " + s.Name})
	}
	r, ok := f.replies[s.Name]
	if !ok {
		r, ok = f.replies[s.Role]
	}
	if !ok {
		r = defaultReplies[s.Role]
	}
	if fn, ok := r.(replyFunc); ok {
		var err error
		if r, err = fn(ctx, s); err != nil {
			return nil, err
		}
	}
	if err, ok := r.(error); ok {
		return nil, err
	}
	return json.Marshal(r)
}

func (f *fakeAgents) all() []agent.Spec {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.specs)
}

func (f *fakeAgents) byRole(role string) []agent.Spec {
	var out []agent.Spec
	for _, s := range f.all() {
		if s.Role == role {
			out = append(out, s)
		}
	}
	return out
}

func (f *fakeAgents) byName(t *testing.T, name string) agent.Spec {
	t.Helper()
	for _, s := range f.all() {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no agent named %q ran; ran: %v", name, f.names())
	return agent.Spec{}
}

func (f *fakeAgents) names() []string {
	var out []string
	for _, s := range f.all() {
		out = append(out, s.Name)
	}
	slices.Sort(out)
	return out
}

func planReply(groups map[string][][]string) map[string]any {
	var es []map[string]any
	for _, role := range []string{"bugs", "seguridad", "reglas", "tests"} {
		if g, ok := groups[role]; ok {
			es = append(es, map[string]any{"rol": role, "grupos": g})
		}
	}
	return map[string]any{"especialistas": es}
}

func hallazgo(file string, line int, sev finding.Severity, group finding.Group, what string) map[string]any {
	return map[string]any{
		"archivo": file, "linea": line, "severidad": sev, "origen": "introducido", "grupo": group,
		"que_pasa": what, "por_que": "Rompe el flujo de pagos.", "solucion": "- a\n+ b",
		"evidencia": []string{fmt.Sprintf("%s:%d", file, line)},
	}
}

func specialistReply(hs ...map[string]any) map[string]any {
	if hs == nil {
		hs = []map[string]any{}
	}
	return map[string]any{"hallazgos": hs, "dudas": []any{}}
}

func validatorReply(result, origin string, evidence ...string) map[string]any {
	if evidence == nil {
		evidence = []string{}
	}
	return map[string]any{"resultado": result, "origen": origin, "evidencia": evidence, "motivo": "motivo-" + result}
}

func synthesisReply(split bool, parts ...finding.SplitPart) map[string]any {
	if parts == nil {
		parts = []finding.SplitPart{}
	}
	return map[string]any{"frase": sintesisFrase, "division": map[string]any{"necesaria": split, "partes": parts}}
}

func patchFile(path, marker string) prb.DiffFile {
	return prb.DiffFile{
		Path: path, Status: "modified", LinesAdded: 1,
		Patch: "@@ -1,1 +1,2 @@\n linea\n+" + marker + "\n",
	}
}

func newRequest(t *testing.T, mode Mode, files ...prb.DiffFile) Request {
	t.Helper()
	if files == nil {
		files = []prb.DiffFile{patchFile("src/a.go", "cambio_en_a")}
	}
	// The PR context lists the same files as the diff, as prb does.
	stats := &prb.Files{Count: len(files), Files: []prb.FileStat{}}
	for _, f := range files {
		stats.Files = append(stats.Files, prb.FileStat{
			Path: f.Path, OldPath: f.OldPath, Status: f.Status, LinesAdded: f.LinesAdded, LinesRemoved: f.LinesRemoved,
		})
	}
	return Request{
		Repo: "stock",
		PR: prb.PRContext{PullRequest: prb.PR{
			ID: 7, Title: "CU-12 Agregar pagos", Description: "Agrega el endpoint de pagos.",
		}, Files: stats},
		Diff:  prb.Diff{TotalFiles: len(files), Files: files},
		Trees: repocache.Worktrees{Head: t.TempDir(), Base: t.TempDir()},
		Mode:  mode,
	}
}

func newDeps(f *fakeAgents) Deps {
	return Deps{
		Cfg:    config.Defaults(),
		Agents: f,
		Now:    func() time.Time { return time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC) },
	}
}

func mustRun(t *testing.T, d Deps, req Request) Result {
	t.Helper()
	res, err := Run(context.Background(), d, req, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return res
}

func findingByID(t *testing.T, res Result, id string) finding.Finding {
	t.Helper()
	for _, f := range res.Findings {
		if f.ID == id {
			return f
		}
	}
	var ids []string
	for _, f := range res.Findings {
		ids = append(ids, f.ID)
	}
	t.Fatalf("no finding %q; findings: %v", id, ids)
	return finding.Finding{}
}

func TestLigeroClean(t *testing.T) {
	fake := newFake(nil)
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))

	want := []string{"bugs", "reglas", "resumen", "superior_sintesis"}
	if got := fake.names(); !slices.Equal(got, want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	if res.Verdict != finding.VerdictAprobar || res.IncompleteWarning {
		t.Errorf("verdict = %s (warning %v), want aprobar", res.Verdict, res.IncompleteWarning)
	}
	if res.Phrase != sintesisFrase {
		t.Errorf("phrase = %q, want the synthesis phrase", res.Phrase)
	}
	if len(res.Findings) != 0 || len(res.FailedAgents) != 0 || len(res.Unreviewed) != 0 {
		t.Errorf("findings %v, failed %v, unreviewed %v: want none", res.Findings, res.FailedAgents, res.Unreviewed)
	}
	if res.AgentCount != 4 || res.Mode != ModeLigero || res.Repo != "stock" || res.PR.ID != 7 {
		t.Errorf("result = %+v", res)
	}
	if !res.Started.Equal(newDeps(nil).Now()) || !res.Finished.Equal(res.Started) {
		t.Errorf("started %v finished %v", res.Started, res.Finished)
	}
	// A complete review tells the synthesis so.
	if p := fake.byName(t, "superior_sintesis").Prompt; !strings.Contains(p, "La herramienta no informó partes sin revisar.") ||
		strings.Contains(p, "Revisión incompleta") {
		t.Errorf("synthesis prompt should report full coverage:\n%s", p)
	}
}

func TestAgentsUseWorktrees(t *testing.T) {
	req := newRequest(t, ModeLigero)
	writeFile(t, filepath.Join(req.Trees.Base, "CLAUDE.md"), "regla-de-base")
	writeFile(t, filepath.Join(req.Trees.Base, "src", "CLAUDE.md"), "regla-de-src")
	writeFile(t, filepath.Join(req.Trees.Head, "CLAUDE.md"), "regla-de-head")
	fake := newFake(nil)
	res := mustRun(t, newDeps(fake), req)

	for _, s := range fake.all() {
		if s.Dir != req.Trees.Head || !slices.Equal(s.AddDirs, []string{req.Trees.Base}) {
			t.Errorf("%s: Dir %q AddDirs %v, want head and [base]", s.Name, s.Dir, s.AddDirs)
		}
		if !strings.Contains(s.Prompt, ": "+req.Trees.Head+"\n") || !strings.Contains(s.Prompt, ": "+req.Trees.Base+"\n") {
			t.Errorf("%s: prompt does not carry the head and base dirs", s.Name)
		}
		if s.System != prompts.System(s.Role) || s.Schema != prompts.Schema(s.Role) {
			t.Errorf("%s: wrong system prompt or schema for role %s", s.Name, s.Role)
		}
	}
	reglas := fake.byName(t, "reglas").Prompt
	if !strings.Contains(reglas, "regla-de-base") || !strings.Contains(reglas, "regla-de-src") || strings.Contains(reglas, "regla-de-head") {
		t.Errorf("rules must come from base, for the changed folders:\n%s", reglas)
	}
	want := []rules.RuleFile{{Path: "CLAUDE.md", Bytes: 13}, {Path: "src/CLAUDE.md", Bytes: 12}}
	if !slices.Equal(res.RuleFiles, want) {
		t.Errorf("RuleFiles = %v, want %v", res.RuleFiles, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRolesUseConfiguredModels(t *testing.T) {
	d := newDeps(newFake(map[string]any{
		prompts.RoleBugs:   specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "falla-critica")),
		prompts.RoleReglas: specialistReply(hallazgo("src/a.go", 7, finding.SeverityAlto, finding.GroupA, "falla-de-regla")),
	}))
	// A distinct assignment per role, so a mix-up cannot go unnoticed.
	d.Cfg.Preset = config.PresetPersonalizado
	d.Cfg.Models = map[config.Role]config.Assignment{
		config.RoleSuperior:        {Model: config.ModelFable, Effort: config.EffortMax},
		config.RoleResumen:         {Model: config.ModelHaiku},
		config.RoleBugs:            {Model: config.ModelSonnet, Effort: config.EffortHigh},
		config.RoleSeguridad:       {Model: config.ModelOpus, Effort: config.EffortLow},
		config.RoleReglas:          {Model: config.ModelFable, Effort: config.EffortLow},
		config.RoleTests:           {Model: config.ModelSonnet, Effort: config.EffortXHigh},
		config.RoleValidadorBugs:   {Model: config.ModelFable, Effort: config.EffortMedium},
		config.RoleValidadorReglas: {Model: config.ModelOpus, Effort: config.EffortMax},
		config.RoleJuezA:           {Model: config.ModelFable, Effort: config.EffortXHigh},
		config.RoleJuezB:           {Model: config.ModelSonnet, Effort: config.EffortMax},
	}
	fake := d.Agents.(*fakeAgents)
	mustRun(t, d, newRequest(t, ModeJudgment))

	want := map[string]config.Role{
		"resumen":                     config.RoleResumen,
		"superior_plan":               config.RoleSuperior,
		"superior_sintesis":           config.RoleSuperior,
		"bugs":                        config.RoleBugs,
		"seguridad":                   config.RoleSeguridad,
		"reglas":                      config.RoleReglas,
		"tests":                       config.RoleTests,
		"validador_bugs-bugs-001":     config.RoleValidadorBugs,
		"validador_reglas-reglas-001": config.RoleValidadorReglas,
		"juez_a":                      config.RoleJuezA,
		"juez_b":                      config.RoleJuezB,
	}
	specs := fake.all()
	if len(specs) != len(want) {
		t.Fatalf("agents = %v, want %d agents", fake.names(), len(want))
	}
	for _, s := range specs {
		role, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected agent %q", s.Name)
			continue
		}
		if s.Assignment != d.Cfg.AssignmentFor(role) {
			t.Errorf("%s: assignment %v, want %v (%s)", s.Name, s.Assignment, d.Cfg.AssignmentFor(role), role)
		}
	}
	for _, name := range []string{"juez_a", "juez_b"} {
		if r := fake.byName(t, name).Role; r != prompts.RoleJuez {
			t.Errorf("%s: prompt role %q, want %q", name, r, prompts.RoleJuez)
		}
	}
}

func TestExhaustivoFansOutPerGroup(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleSuperiorPlan: planReply(map[string][][]string{
			"bugs":   {{"src/a.go", "no/existe.go"}, {"src/b.go"}, {"fantasma.go"}},
			"reglas": {{"src/a.go", "src/b.go"}},
		}),
	})
	req := newRequest(t, ModeExhaustivo, patchFile("src/a.go", "cambio_en_a"), patchFile("src/b.go", "cambio_en_b"))
	mustRun(t, newDeps(fake), req)

	want := []string{"bugs-1", "bugs-2", "reglas", "resumen", "superior_plan", "superior_sintesis"}
	if got := fake.names(); !slices.Equal(got, want) {
		t.Fatalf("agents = %v, want %v", got, want)
	}
	b1, b2 := fake.byName(t, "bugs-1").Prompt, fake.byName(t, "bugs-2").Prompt
	if !strings.Contains(b1, "+cambio_en_a") || strings.Contains(b1, "cambio_en_b") {
		t.Errorf("bugs-1 must see only group 1:\n%s", b1)
	}
	if !strings.Contains(b2, "+cambio_en_b") || strings.Contains(b2, "cambio_en_a") {
		t.Errorf("bugs-2 must see only group 2:\n%s", b2)
	}
	if r := fake.byName(t, "reglas").Prompt; !strings.Contains(r, "cambio_en_a") || !strings.Contains(r, "cambio_en_b") {
		t.Errorf("reglas must see both files:\n%s", r)
	}
	for _, s := range fake.byRole(prompts.RoleBugs) {
		if strings.Contains(s.Prompt, "existe.go") || strings.Contains(s.Prompt, "fantasma.go") {
			t.Errorf("%s: a path the diff does not have reached the agent", s.Name)
		}
	}
}

func TestTrivialesReachPlanOnly(t *testing.T) {
	resumen := map[string]any{"resumen": "Agrega pagos.", "triviales": []string{"src/a.go"}}

	t.Run("exhaustivo", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleResumen: resumen})
		mustRun(t, newDeps(fake), newRequest(t, ModeExhaustivo))
		plan := fake.byName(t, "superior_plan").Prompt
		i := strings.Index(plan, "Archivos que el resumen marcó como triviales")
		if i < 0 || !strings.Contains(plan[i:], `"src/a.go"`) {
			t.Errorf("the plan must get the trivial files:\n%s", plan)
		}
		bugs := fake.byName(t, "bugs").Prompt
		if !strings.Contains(bugs, "+cambio_en_a") {
			t.Errorf("bugs must still review the trivial file")
		}
		if strings.Contains(bugs, "marcó como triviales") {
			t.Errorf("specialists must not get the trivial list")
		}
	})
	t.Run("ligero", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleResumen: resumen})
		mustRun(t, newDeps(fake), newRequest(t, ModeLigero))
		for _, name := range []string{"bugs", "reglas"} {
			if !strings.Contains(fake.byName(t, name).Prompt, "+cambio_en_a") {
				t.Errorf("%s must still review the trivial file", name)
			}
		}
	})
}

func TestPlanFailureFallsBackToAllSpecialists(t *testing.T) {
	fake := newFake(map[string]any{prompts.RoleSuperiorPlan: errors.New("se venció el tiempo")})
	req := newRequest(t, ModeExhaustivo, patchFile("src/a.go", "cambio_en_a"), patchFile("src/b.go", "cambio_en_b"))
	res := mustRun(t, newDeps(fake), req)

	for _, name := range []string{"bugs", "seguridad", "reglas", "tests"} {
		p := fake.byName(t, name).Prompt
		if !strings.Contains(p, "+cambio_en_a") || !strings.Contains(p, "+cambio_en_b") {
			t.Errorf("%s must review every file when the plan fails", name)
		}
	}
	if !slices.Equal(res.FailedAgents, []string{"superior_plan"}) || res.Verdict != finding.VerdictRevisarAMano {
		t.Errorf("failed %v verdict %s, want [superior_plan] and revisar_a_mano", res.FailedAgents, res.Verdict)
	}
}

func TestGraveGoesThroughValidator(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs: specialistReply(
			hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "falla-critica"),
			hallazgo("src/a.go", 5, finding.SeverityMedio, finding.GroupA, "falla-media"),
		),
		prompts.RoleReglas: specialistReply(hallazgo("src/a.go", 7, finding.SeverityAlto, finding.GroupA, "falla-de-regla")),
	})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))

	vb, vr := fake.byRole(prompts.RoleValidadorBugs), fake.byRole(prompts.RoleValidadorReglas)
	if len(vb) != 1 || len(vr) != 1 {
		t.Fatalf("validators: %d validador_bugs and %d validador_reglas, want one each (agents %v)", len(vb), len(vr), fake.names())
	}
	if vb[0].Name != "validador_bugs-bugs-001" || !strings.Contains(vb[0].Prompt, "falla-critica") || strings.Contains(vb[0].Prompt, "falla-de-regla") {
		t.Errorf("validador_bugs must validate the bugs critico: %s\n%s", vb[0].Name, vb[0].Prompt)
	}
	if vr[0].Name != "validador_reglas-reglas-001" || !strings.Contains(vr[0].Prompt, "falla-de-regla") || strings.Contains(vr[0].Prompt, "falla-critica") {
		t.Errorf("validador_reglas must validate the reglas alto: %s\n%s", vr[0].Name, vr[0].Prompt)
	}
	if !strings.Contains(vb[0].Prompt, "+cambio_en_a") {
		t.Errorf("the validator must get the patches the candidate came from")
	}
	for _, s := range append(vb, vr...) {
		if strings.Contains(s.Prompt, "falla-media") {
			t.Errorf("%s: the medio finding must not be validated", s.Name)
		}
	}
	if f := findingByID(t, res, "bugs-002"); f.Status != finding.StatusConfirmado || f.What != "falla-media" {
		t.Errorf("medio = %+v, want confirmado without validation", f)
	}
	for _, id := range []string{"bugs-001", "reglas-001"} {
		if f := findingByID(t, res, id); f.Status != finding.StatusConfirmado {
			t.Errorf("%s status %s, want confirmado by its validator", id, f.Status)
		}
	}
}

func TestValidatorRoleChoice(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs:      specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "x")),
		prompts.RoleSeguridad: specialistReply(hallazgo("src/a.go", 4, finding.SeverityAlto, finding.GroupA, "x")),
		prompts.RoleTests:     specialistReply(hallazgo("src/a.go", 5, finding.SeverityAlto, finding.GroupA, "x")),
		prompts.RoleReglas:    specialistReply(hallazgo("src/a.go", 6, finding.SeverityCritico, finding.GroupA, "x")),
	})
	mustRun(t, newDeps(fake), newRequest(t, ModeExhaustivo))

	var got []string
	for _, s := range append(fake.byRole(prompts.RoleValidadorBugs), fake.byRole(prompts.RoleValidadorReglas)...) {
		got = append(got, s.Name)
	}
	slices.Sort(got)
	want := []string{"validador_bugs-bugs-001", "validador_bugs-seguridad-001", "validador_bugs-tests-001", "validador_reglas-reglas-001"}
	if !slices.Equal(got, want) {
		t.Errorf("validators = %v, want %v", got, want)
	}
}

func TestValidatorOutcomes(t *testing.T) {
	critico := specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "falla-critica"))
	tests := []struct {
		name      string
		validator any
		status    finding.Status
		origin    finding.Origin
		reason    string
		verdict   finding.Verdict
		failed    []string
	}{
		{"confirmado", validatorReply("confirmado", "introducido", "src/a.go:3"), finding.StatusConfirmado, finding.OriginIntroducido, "motivo-confirmado", finding.VerdictPedirCambios, nil},
		{"confirmado origen incierto", validatorReply("confirmado", "incierto", "src/a.go:3"), finding.StatusConfirmado, finding.OriginIncierto, "motivo-confirmado", finding.VerdictRevisarAMano, nil},
		{"refutado", validatorReply("refutado", "introducido", "src/b.go:10"), finding.StatusRefutado, finding.OriginIntroducido, "motivo-refutado", finding.VerdictAprobar, nil},
		{"refutado sin evidencia", validatorReply("refutado", "introducido", " "), finding.StatusSinConfirmar, finding.OriginIntroducido, "el validador lo refutó sin evidencia: motivo-refutado", finding.VerdictRevisarAMano, nil},
		{"sin resolver", validatorReply("sin_resolver", "introducido"), finding.StatusSinConfirmar, finding.OriginIntroducido, "motivo-sin_resolver", finding.VerdictRevisarAMano, nil},
		{"validador fallido", errors.New("se venció el tiempo"), finding.StatusSinConfirmar, finding.OriginIntroducido, "no se pudo validar: falló el validador", finding.VerdictRevisarAMano, []string{"validador_bugs-bugs-001"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFake(map[string]any{prompts.RoleBugs: critico, prompts.RoleValidadorBugs: tt.validator})
			res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))
			f := findingByID(t, res, "bugs-001")
			if f.Status != tt.status || f.Origin != tt.origin || f.Reason != tt.reason {
				t.Errorf("finding status %s origin %s reason %q, want %s %s %q", f.Status, f.Origin, f.Reason, tt.status, tt.origin, tt.reason)
			}
			if res.Verdict != tt.verdict || res.IncompleteWarning {
				t.Errorf("verdict %s (warning %v), want %s", res.Verdict, res.IncompleteWarning, tt.verdict)
			}
			if !slices.Equal(res.FailedAgents, tt.failed) {
				t.Errorf("failed = %v, want %v", res.FailedAgents, tt.failed)
			}
		})
	}

	t.Run("confirmado agrega su evidencia", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleBugs: critico, prompts.RoleValidadorBugs: validatorReply("confirmado", "empeorado", "base:src/a.go:2", "src/a.go:3")})
		f := findingByID(t, mustRun(t, newDeps(fake), newRequest(t, ModeLigero)), "bugs-001")
		if want := []string{"src/a.go:3", "base:src/a.go:2"}; !slices.Equal(f.Evidence, want) {
			t.Errorf("evidence = %v, want %v", f.Evidence, want)
		}
	})

	// Task 7 rule: an incomplete review still asks for changes, with the warning.
	t.Run("validador fallido con otro critico confirmado", func(t *testing.T) {
		fake := newFake(map[string]any{
			prompts.RoleBugs: specialistReply(
				hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "uno"),
				hallazgo("src/a.go", 9, finding.SeverityCritico, finding.GroupA, "dos"),
			),
			"validador_bugs-bugs-001": errors.New("se venció el tiempo"),
		})
		res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))
		if res.Verdict != finding.VerdictPedirCambios || !res.IncompleteWarning {
			t.Errorf("verdict %s warning %v, want pedir_cambios with the incomplete warning", res.Verdict, res.IncompleteWarning)
		}
		if f := findingByID(t, res, "bugs-001"); f.Status != finding.StatusSinConfirmar {
			t.Errorf("failed candidate status %s, want sin_confirmar", f.Status)
		}
		if !slices.Equal(res.FailedAgents, []string{"validador_bugs-bugs-001"}) {
			t.Errorf("failed = %v", res.FailedAgents)
		}
	})
}

func TestPrecheckSecretValidated(t *testing.T) {
	secret := prb.DiffFile{
		Path: "src/conf.go", Status: "modified", LinesAdded: 1,
		Patch: "@@ -1,1 +1,2 @@\n package conf\n+var token = \"abcdefghijklmnop\"\n",
	}
	fake := newFake(map[string]any{prompts.RoleValidadorBugs: validatorReply("refutado", "introducido", "src/conf.go:2")})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero, secret))

	v := fake.byName(t, "validador_bugs-prechecks-001")
	if !strings.Contains(v.Prompt, "Posible secreto") || !strings.Contains(v.Prompt, "abcdefghijklmnop") {
		t.Errorf("the secret validator must get the candidate and its patch:\n%s", v.Prompt)
	}
	f := findingByID(t, res, "prechecks-001")
	if f.Status != finding.StatusRefutado || f.Severity != finding.SeverityCritico || f.Reason != "motivo-refutado" {
		t.Errorf("secret = %+v, want refutado", f)
	}
	if res.Verdict != finding.VerdictAprobar {
		t.Errorf("verdict %s, want aprobar", res.Verdict)
	}
}

func TestProcessFindingsSkipValidation(t *testing.T) {
	fake := newFake(nil)
	req := newRequest(t, ModeLigero)
	req.PR.PullRequest.Title = "Agregar pagos"
	res := mustRun(t, newDeps(fake), req)

	if n := len(fake.byRole(prompts.RoleValidadorBugs)); n != 0 {
		t.Errorf("%d validators ran for a process finding", n)
	}
	f := findingByID(t, res, "prechecks-001")
	if f.Status != finding.StatusConfirmado || f.Severity != finding.SeverityBajo {
		t.Errorf("process finding = %+v, want confirmado bajo", f)
	}
	if res.Verdict != finding.VerdictAprobarConComentarios {
		t.Errorf("verdict %s, want aprobar_con_comentarios", res.Verdict)
	}
}

func TestPreexistingDoesNotCount(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs:          specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "falla-vieja")),
		prompts.RoleValidadorBugs: validatorReply("confirmado", "preexistente", "base:src/a.go:3"),
	})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))

	f := findingByID(t, res, "bugs-001")
	if f.Origin != finding.OriginPreexistente || f.Status != finding.StatusConfirmado {
		t.Errorf("finding = %+v, want confirmado preexistente", f)
	}
	if res.Verdict != finding.VerdictAprobar {
		t.Errorf("verdict %s, want aprobar", res.Verdict)
	}
}

func TestJudgmentApplied(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleSuperiorPlan: planReply(map[string][][]string{"bugs": {{"src/a.go", "src/b.go"}}}),
		prompts.RoleBugs: specialistReply(
			hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "falla-critica"),
			hallazgo("src/b.go", 5, finding.SeverityMedio, finding.GroupA, "falla-media"),
			hallazgo("src/a.go", 9, finding.SeverityCritico, finding.GroupA, "falla-vieja"),
			hallazgo("src/a.go", 12, finding.SeverityAlto, finding.GroupA, "falla-refutada"),
		),
		"validador_bugs-bugs-003": validatorReply("confirmado", "preexistente", "base:src/a.go:9"),
		"validador_bugs-bugs-004": validatorReply("refutado", "introducido", "src/a.go:11"),
		// Judge A refutes both, in reverse order; judge B confirms bugs-001
		// and has no vote for bugs-002.
		"juez_a": map[string]any{"votos": []finding.JudgeVote{
			{FindingID: "bugs-002", Evidence: []string{"src/b.go:5"}, Note: "nota-del-juez-a"},
			{FindingID: "bugs-001", Evidence: []string{"src/a.go:3"}, Note: "nota-del-juez-a"},
		}},
		"juez_b": map[string]any{"votos": []finding.JudgeVote{
			{FindingID: "bugs-001", Real: true, Worth: true, FixComplies: true, Evidence: []string{"src/a.go:3"}, Note: "nota-del-juez-b"},
		}},
	})
	req := newRequest(t, ModeJudgment, patchFile("src/a.go", "cambio_en_a"), patchFile("src/b.go", "cambio_en_b"))
	res := mustRun(t, newDeps(fake), req)

	judges := fake.byRole(prompts.RoleJuez)
	if len(judges) != 2 {
		t.Fatalf("%d judges ran, want 2", len(judges))
	}
	for _, j := range judges {
		for _, in := range []string{`"bugs-001"`, `"bugs-002"`} {
			if !strings.Contains(j.Prompt, in) {
				t.Errorf("%s must judge %s", j.Name, in)
			}
		}
		for _, out := range []string{`"bugs-003"`, `"bugs-004"`, "nota-del-juez"} {
			if strings.Contains(j.Prompt, out) {
				t.Errorf("%s must not see %s", j.Name, out)
			}
		}
	}

	f1 := findingByID(t, res, "bugs-001")
	if f1.Status != finding.StatusConfirmado || len(f1.Votes) != 2 ||
		f1.Votes[0].Judge != "A" || f1.Votes[0].FindingID != "bugs-001" ||
		f1.Votes[1].Judge != "B" || f1.Votes[1].FindingID != "bugs-001" {
		t.Errorf("bugs-001 = %+v, want confirmado with both votes for it", f1)
	}
	f2 := findingByID(t, res, "bugs-002")
	if f2.Status != finding.StatusDescartadoPorJueces || len(f2.Votes) != 2 || !reflect.DeepEqual(f2.Votes[1], finding.JudgeVote{}) {
		t.Errorf("bugs-002 = %+v, want descartado_por_jueces with a zero vote from B", f2)
	}
	for _, id := range []string{"bugs-003", "bugs-004"} {
		if f := findingByID(t, res, id); len(f.Votes) != 0 {
			t.Errorf("%s was judged: %+v", id, f.Votes)
		}
	}
	if res.Verdict != finding.VerdictPedirCambios {
		t.Errorf("verdict %s, want pedir_cambios", res.Verdict)
	}
}

func TestGroupBFilteredAfterValidation(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs: specialistReply(
			hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "ancla-confirmada"),
			hallazgo("src/c.go", 3, finding.SeverityCritico, finding.GroupA, "ancla-refutada"),
		),
		prompts.RoleReglas: specialistReply(
			hallazgo("src/a.go", 8, finding.SeverityBajo, finding.GroupB, "menor-en-a"),
			hallazgo("src/b.go", 8, finding.SeverityBajo, finding.GroupB, "menor-en-b"),
			hallazgo("src/c.go", 8, finding.SeverityBajo, finding.GroupB, "menor-en-c"),
		),
		"validador_bugs-bugs-002": validatorReply("refutado", "introducido", "src/c.go:2"),
	})
	req := newRequest(t, ModeLigero, patchFile("src/a.go", "x"), patchFile("src/b.go", "y"), patchFile("src/c.go", "z"))
	res := mustRun(t, newDeps(fake), req)

	// The A finding of src/a.go was still a candidate before validation: the
	// B finding next to it survives only because the filter runs afterwards.
	if f := findingByID(t, res, "reglas-001"); f.Status != finding.StatusConfirmado {
		t.Errorf("the B finding next to a confirmed A must stay confirmado, got %s", f.Status)
	}
	// Filtered findings stay in the result, for findings.json, marked.
	for _, id := range []string{"reglas-002", "reglas-003"} {
		if f := findingByID(t, res, id); f.Status != finding.StatusFiltrado || f.Reason != reasonFiltrado {
			t.Errorf("%s: status %s reason %q, want filtrado with %q", id, f.Status, f.Reason, reasonFiltrado)
		}
	}
	if f := findingByID(t, res, "bugs-002"); f.Status != finding.StatusRefutado {
		t.Errorf("refuted findings stay refutado, got %s", f.Status)
	}
	if len(res.Findings) != 5 {
		t.Errorf("%d findings, want all 5 kept", len(res.Findings))
	}
}

func TestGroupBRefilteredAfterJudges(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleSuperiorPlan: planReply(map[string][][]string{"bugs": {{"src/a.go"}}, "reglas": {{"src/a.go"}}}),
		prompts.RoleBugs:         specialistReply(hallazgo("src/a.go", 3, finding.SeverityMedio, finding.GroupA, "ancla")),
		prompts.RoleReglas: specialistReply(
			hallazgo("src/a.go", 8, finding.SeverityBajo, finding.GroupB, "menor"),
			hallazgo("src/a.go", 9, finding.SeverityBajo, finding.GroupB, "menor-refutado"),
		),
		"juez_a": map[string]any{"votos": []finding.JudgeVote{
			{FindingID: "bugs-001", Evidence: []string{"src/a.go:3"}},
			{FindingID: "reglas-002", Evidence: []string{"src/a.go:9"}},
		}},
	})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeJudgment))

	if f := findingByID(t, res, "bugs-001"); f.Status != finding.StatusDescartadoPorJueces {
		t.Fatalf("anchor status %s, want descartado_por_jueces", f.Status)
	}
	if f := findingByID(t, res, "reglas-001"); f.Status != finding.StatusFiltrado || f.Reason != reasonFiltrado {
		t.Errorf("a B finding whose anchor the judges discarded must be filtrado, got %s %q", f.Status, f.Reason)
	}
	if f := findingByID(t, res, "reglas-002"); f.Status != finding.StatusDescartadoPorJueces {
		t.Errorf("a B finding the judges discarded keeps that status, got %s", f.Status)
	}
}

func TestMinorCap(t *testing.T) {
	var hs []map[string]any
	for i := 1; i <= 7; i++ {
		hs = append(hs, hallazgo("src/a.go", i, finding.SeverityBajo, finding.GroupA, fmt.Sprintf("menor-%d", i)))
	}
	fake := newFake(map[string]any{prompts.RoleBugs: specialistReply(hs...)})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))

	if len(res.Findings) != 7 {
		t.Fatalf("%d findings, want 7", len(res.Findings))
	}
	for i, f := range res.Findings {
		want := finding.StatusConfirmado
		if i >= 5 {
			want = finding.StatusMenor
		}
		if f.Status != want {
			t.Errorf("%s status %s, want %s", f.ID, f.Status, want)
		}
	}
	if res.Verdict != finding.VerdictAprobarConComentarios {
		t.Errorf("verdict %s, want aprobar_con_comentarios", res.Verdict)
	}
	s := fake.byName(t, "superior_sintesis").Prompt
	if strings.Count(s, `"estado": "menor"`) != 2 {
		t.Errorf("the synthesis must see the capped statuses:\n%s", s)
	}
}

func TestIncompleteOnFailedSpecialist(t *testing.T) {
	bugsFailed := make(chan struct{})
	fake := newFake(map[string]any{
		prompts.RoleBugs: replyFunc(func(context.Context, agent.Spec) (any, error) {
			close(bugsFailed)
			return nil, errors.New("se venció el tiempo")
		}),
		// reglas finishes only after bugs failed, with a live context.
		prompts.RoleReglas: replyFunc(func(ctx context.Context, _ agent.Spec) (any, error) {
			select {
			case <-bugsFailed:
			case <-time.After(5 * time.Second):
				return nil, errors.New("bugs never ran")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return specialistReply(hallazgo("src/a.go", 4, finding.SeverityMedio, finding.GroupA, "de-reglas")), nil
		}),
	})
	events := make(chan Event, 256)
	res, err := Run(context.Background(), newDeps(fake), newRequest(t, ModeLigero), events)
	if err != nil {
		t.Fatal(err)
	}
	close(events)

	if res.Verdict != finding.VerdictRevisarAMano || !slices.Equal(res.FailedAgents, []string{"bugs"}) {
		t.Errorf("verdict %s failed %v, want revisar_a_mano and [bugs]", res.Verdict, res.FailedAgents)
	}
	if f := findingByID(t, res, "reglas-001"); f.Status != finding.StatusConfirmado {
		t.Errorf("reglas finding = %+v: the other specialist must keep going", f)
	}
	s := fake.byName(t, "superior_sintesis").Prompt
	i := strings.Index(s, "# Revisión incompleta")
	if i < 0 || !strings.Contains(s[i:], "falló el agente bugs (bugs) y no revisó: \"src/a.go\"") {
		t.Errorf("the synthesis must name the failed agent and its files:\n%s", s)
	}
	var failed bool
	for e := range events {
		if e.Kind == AgentFailed && e.Agent == "bugs" && e.Err != nil && e.Stage == StageSpecialists {
			failed = true
		}
	}
	if !failed {
		t.Error("no AgentFailed event for bugs")
	}
}

func TestUnreviewedNonGeneratedMakesIncomplete(t *testing.T) {
	t.Run("codigo", func(t *testing.T) {
		fake := newFake(nil)
		omitted := prb.DiffFile{Path: "src/a.go", Status: "modified", LinesAdded: 900, PatchOmitted: true}
		res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero, omitted))
		if !slices.Equal(res.Unreviewed, []string{"src/a.go"}) || res.Verdict != finding.VerdictRevisarAMano {
			t.Errorf("unreviewed %v verdict %s, want [src/a.go] and revisar_a_mano", res.Unreviewed, res.Verdict)
		}
		s := fake.byName(t, "superior_sintesis").Prompt
		i := strings.Index(s, "# Revisión incompleta")
		if i < 0 || !strings.Contains(s[i:], `"src/a.go" quedó sin revisar`) {
			t.Errorf("the synthesis must name the unreviewed file:\n%s", s)
		}
	})
	t.Run("lockfile", func(t *testing.T) {
		lock := prb.DiffFile{Path: "package-lock.json", Status: "modified", LinesAdded: 900, PatchOmitted: true}
		res := mustRun(t, newDeps(newFake(nil)), newRequest(t, ModeLigero, patchFile("src/b.go", "y"), lock))
		if len(res.Unreviewed) != 0 || !slices.Contains(res.Excluded, "package-lock.json") || res.Verdict != finding.VerdictAprobar {
			t.Errorf("unreviewed %v excluded %v verdict %s, want the lockfile excluded and aprobar", res.Unreviewed, res.Excluded, res.Verdict)
		}
	})
}

func TestExcludedComesFromContextFiles(t *testing.T) {
	req := newRequest(t, ModeLigero, patchFile("src/a.go", "x"), prb.DiffFile{Path: "logo.png", Binary: true})
	req.PR.Files = &prb.Files{Files: []prb.FileStat{
		{Path: "src/a.go", LinesAdded: 1}, {Path: "logo.png"},
		{Path: "package-lock.json", LinesAdded: 300}, {Path: "dist/app.js", LinesAdded: 40},
	}}
	fake := newFake(nil)
	res := mustRun(t, newDeps(fake), req)

	if want := []string{"logo.png", "package-lock.json", "dist/app.js"}; !slices.Equal(res.Excluded, want) {
		t.Errorf("Excluded = %v, want %v", res.Excluded, want)
	}
	p := fake.byName(t, "bugs").Prompt
	i, j := strings.Index(p, "Cambios sensibles"), strings.Index(p, "Excluidos de la revisión")
	if i < 0 || j < i || !strings.Contains(p[i:j], `"package-lock.json"`) {
		t.Errorf("a lockfile change only seen in the diffstat must reach the agents as sensitive:\n%s", p)
	}
}

func TestEventsOrder(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs: specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "x")),
	})
	d := newDeps(fake)
	events := make(chan Event, 256)
	if _, err := Run(context.Background(), d, newRequest(t, ModeLigero), events); err != nil {
		t.Fatal(err)
	}
	close(events)

	type stageEvent struct {
		kind  EventKind
		stage Stage
	}
	var stages []stageEvent
	var current Stage
	open := false
	var activity, validator bool
	for e := range events {
		switch e.Kind {
		case StageStarted, StageDone, StageSkipped:
			stages = append(stages, stageEvent{e.Kind, e.Stage})
			current, open = e.Stage, e.Kind == StageStarted
			if e.Stage == StageValidation && e.Candidates != 1 {
				t.Errorf("validation %v carries %d candidates, want 1", e.Kind, e.Candidates)
			}
		default:
			if !open || e.Stage != current {
				t.Errorf("agent event %+v outside its stage (current %d, open %v)", e, current, open)
			}
		}
		if e.Kind == AgentActivity && e.Agent == "bugs" {
			activity = e.Text == "Leyendo bugs" && e.Role == config.RoleBugs && e.Model == d.Cfg.AssignmentFor(config.RoleBugs).Model
		}
		if e.Kind == AgentStarted && e.Agent == "validador_bugs-bugs-001" {
			validator = e.Stage == StageValidation && e.Role == config.RoleValidadorBugs
		}
	}
	want := []stageEvent{
		{StageStarted, 1}, {StageDone, 1}, {StageStarted, 2}, {StageDone, 2}, {StageStarted, 3}, {StageDone, 3},
		{StageStarted, 4}, {StageDone, 4}, {StageStarted, 5}, {StageDone, 5}, {StageSkipped, 6},
		{StageStarted, 7}, {StageDone, 7},
	}
	if !slices.Equal(stages, want) {
		t.Errorf("stage events = %v, want %v", stages, want)
	}
	if !activity {
		t.Error("the bugs activity was not forwarded with its role and model")
	}
	if !validator {
		t.Error("no AgentStarted for the validator in stage 5")
	}
}

func TestSplitOnlyWhenOversize(t *testing.T) {
	parts := []finding.SplitPart{
		{Title: "Modelo y migración", Files: []string{"src/a.go"}, Lines: 300},
		{Title: "Endpoint", Files: []string{"src/b.go"}, Lines: 200, DependsOn: 1},
	}
	for _, tt := range []struct {
		name  string
		lines int
		want  []finding.SplitPart
	}{
		{"chico", 10, nil},
		{"grande", 500, parts},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := patchFile("src/a.go", "x")
			f.LinesAdded = tt.lines
			fake := newFake(map[string]any{prompts.RoleSuperiorSintesis: synthesisReply(true, parts...)})
			res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero, f))
			if !slices.EqualFunc(res.Split, tt.want, func(a, b finding.SplitPart) bool {
				return a.Title == b.Title && slices.Equal(a.Files, b.Files) && a.Lines == b.Lines && a.DependsOn == b.DependsOn
			}) {
				t.Errorf("Split = %+v, want %+v", res.Split, tt.want)
			}
		})
	}
}

func TestRunRejectsUnknownMode(t *testing.T) {
	fake := newFake(nil)
	if _, err := Run(context.Background(), newDeps(fake), newRequest(t, Mode("rapido")), nil); err == nil {
		t.Error("want an error for an unknown mode")
	}
	if n := len(fake.all()); n != 0 {
		t.Errorf("%d agents ran", n)
	}
}

func TestRunStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fake := newFake(nil)
	if _, err := Run(ctx, newDeps(fake), newRequest(t, ModeLigero), nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if n := len(fake.all()); n != 0 {
		t.Errorf("%d agents ran after cancel", n)
	}
}

func TestPlanCoverageEnforced(t *testing.T) {
	twoFiles := func(t *testing.T) Request {
		return newRequest(t, ModeExhaustivo, patchFile("src/a.go", "cambio_en_a"), patchFile("src/b.go", "cambio_en_b"))
	}
	seesOnly := func(t *testing.T, fake *fakeAgents, name, want, not string) {
		t.Helper()
		p := fake.byName(t, name).Prompt
		if !strings.Contains(p, "+"+want) || strings.Contains(p, not) {
			t.Errorf("%s must see only %s:\n%s", name, want, p)
		}
	}

	t.Run("plan vacío", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleSuperiorPlan: map[string]any{"especialistas": []any{}}})
		res := mustRun(t, newDeps(fake), twoFiles(t))
		for _, name := range []string{"bugs", "seguridad", "reglas", "tests"} {
			p := fake.byName(t, name).Prompt
			if !strings.Contains(p, "+cambio_en_a") || !strings.Contains(p, "+cambio_en_b") {
				t.Errorf("%s must review every file when the plan is empty", name)
			}
		}
		if !slices.Equal(res.FailedAgents, []string{"superior_plan"}) || res.Verdict != finding.VerdictRevisarAMano {
			t.Errorf("failed %v verdict %s, want [superior_plan] and revisar_a_mano", res.FailedAgents, res.Verdict)
		}
		if len(res.Failures) != 1 || res.Failures[0].Reason != "el plan no asignó ningún archivo revisable" {
			t.Errorf("failures = %+v", res.Failures)
		}
	})
	t.Run("archivo omitido", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleSuperiorPlan: planReply(map[string][][]string{
			"bugs": {{"src/a.go"}}, "seguridad": {{"src/a.go"}}, "reglas": {{"src/a.go"}},
		})})
		res := mustRun(t, newDeps(fake), twoFiles(t))
		want := []string{"bugs-1", "bugs-2", "reglas-1", "reglas-2", "resumen", "seguridad", "superior_plan", "superior_sintesis"}
		if got := fake.names(); !slices.Equal(got, want) {
			t.Fatalf("agents = %v, want %v", got, want)
		}
		seesOnly(t, fake, "bugs-2", "cambio_en_b", "cambio_en_a")
		seesOnly(t, fake, "reglas-2", "cambio_en_b", "cambio_en_a")
		if len(res.FailedAgents) != 0 || res.Verdict != finding.VerdictAprobar {
			t.Errorf("failed %v verdict %s: Go's coverage keeps the review complete", res.FailedAgents, res.Verdict)
		}
	})
	t.Run("rutas normalizadas", func(t *testing.T) {
		fake := newFake(map[string]any{prompts.RoleSuperiorPlan: planReply(map[string][][]string{
			"bugs":   {{`"src/a.go"`}, {"./src/b.go"}},
			"reglas": {{" `src/a.go` ", "src/./b.go", "src/zz.go"}},
		})})
		mustRun(t, newDeps(fake), twoFiles(t))
		want := []string{"bugs-1", "bugs-2", "reglas", "resumen", "superior_plan", "superior_sintesis"}
		if got := fake.names(); !slices.Equal(got, want) {
			t.Fatalf("agents = %v, want %v", got, want)
		}
		seesOnly(t, fake, "bugs-1", "cambio_en_a", "cambio_en_b")
		seesOnly(t, fake, "bugs-2", "cambio_en_b", "cambio_en_a")
	})
}

func TestGraveGroupBBecomesA(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs: specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupB, "critico-en-b")),
	})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))

	f := findingByID(t, res, "bugs-001")
	if f.Group != finding.GroupA || f.Status != finding.StatusConfirmado {
		t.Errorf("finding = %+v, want a confirmed Group A", f)
	}
	if res.Verdict != finding.VerdictPedirCambios {
		t.Errorf("verdict %s, want pedir_cambios", res.Verdict)
	}
}

func TestMinorCapPriority(t *testing.T) {
	bajo := func(line int, group finding.Group, origin string) map[string]any {
		h := hallazgo("src/a.go", line, finding.SeverityBajo, group, fmt.Sprintf("bajo-%d", line))
		h["origen"] = origin
		return h
	}
	fiveBajo := func(group finding.Group, origin string) []map[string]any {
		var hs []map[string]any
		for i := 1; i <= 5; i++ {
			hs = append(hs, bajo(10+i, group, origin))
		}
		return hs
	}
	tests := []struct {
		name  string
		bugs  []map[string]any
		menor []string
		kept  []string
	}{
		{"preexistentes no ocupan lugar", fiveBajo(finding.GroupA, "preexistente"), nil,
			[]string{"bugs-001", "bugs-002", "bugs-003", "bugs-004", "bugs-005"}},
		{"incierto después de los del PR", fiveBajo(finding.GroupA, "incierto"), []string{"bugs-005"},
			[]string{"bugs-001", "bugs-004"}},
		{"grupo B después del grupo A", append(
			[]map[string]any{hallazgo("src/a.go", 1, finding.SeverityMedio, finding.GroupA, "ancla")},
			fiveBajo(finding.GroupB, "introducido")...), []string{"bugs-006"},
			[]string{"bugs-002", "bugs-005"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFake(map[string]any{
				prompts.RoleBugs:   specialistReply(tt.bugs...),
				prompts.RoleReglas: specialistReply(bajo(40, finding.GroupA, "introducido")),
			})
			res := mustRun(t, newDeps(fake), newRequest(t, ModeLigero))
			if f := findingByID(t, res, "reglas-001"); f.Status != finding.StatusConfirmado {
				t.Errorf("the PR's Group A bajo must keep its slot, got %s", f.Status)
			}
			for _, id := range tt.menor {
				if f := findingByID(t, res, id); f.Status != finding.StatusMenor {
					t.Errorf("%s status %s, want menor", id, f.Status)
				}
			}
			for _, id := range tt.kept {
				if f := findingByID(t, res, id); f.Status != finding.StatusConfirmado {
					t.Errorf("%s status %s, want confirmado", id, f.Status)
				}
			}
			if res.Verdict != finding.VerdictAprobarConComentarios {
				t.Errorf("verdict %s, want aprobar_con_comentarios", res.Verdict)
			}
		})
	}
}

func TestFailuresDetailed(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleResumen:       &agent.AgentError{Agent: "resumen", Err: fmt.Errorf("claude: %w", context.DeadlineExceeded)},
		prompts.RoleBugs:          errors.New("exit status 1: sin cuota"),
		prompts.RoleSeguridad:     specialistReply(hallazgo("src/a.go", 3, finding.SeverityCritico, finding.GroupA, "inyeccion")),
		prompts.RoleValidadorBugs: &agent.AgentError{Agent: "validador_bugs-seguridad-001", Err: errors.New("se cortó")},
		"juez_a":                  errors.New("juez caído"),
		// A JSON string instead of the synthesis object.
		prompts.RoleSuperiorSintesis: "no es una síntesis",
	})
	res := mustRun(t, newDeps(fake), newRequest(t, ModeJudgment))

	want := []Failure{
		{Agent: "resumen", Role: "resumen", Reason: "se venció el tiempo"},
		{Agent: "bugs", Role: "bugs", Files: []string{"src/a.go"}, Reason: "exit status 1: sin cuota"},
		{Agent: "validador_bugs-seguridad-001", Role: "validador_bugs", Files: []string{"src/a.go"}, Candidate: "seguridad-001", Reason: "se cortó"},
		{Agent: "juez_a", Role: "juez", Reason: "juez caído"},
		{Agent: "superior_sintesis", Role: "superior_sintesis", Reason: "salida inválida"},
	}
	if len(res.Failures) != len(want) {
		t.Fatalf("failures = %+v, want %d", res.Failures, len(want))
	}
	for i, w := range want {
		g := res.Failures[i]
		if g.Agent != w.Agent || g.Role != w.Role || !slices.Equal(g.Files, w.Files) || g.Candidate != w.Candidate ||
			!strings.HasPrefix(g.Reason, w.Reason) {
			t.Errorf("failure %d = %+v, want %+v", i, g, w)
		}
	}
	var names []string
	for _, f := range want {
		names = append(names, f.Agent)
	}
	if !slices.Equal(res.FailedAgents, names) {
		t.Errorf("FailedAgents = %v, want %v", res.FailedAgents, names)
	}
}

func TestContextFilesMissingFromDiffAreUnreviewed(t *testing.T) {
	req := newRequest(t, ModeLigero)
	req.Diff.OmittedFiles = []string{"src/d.go"}
	req.PR.Files = &prb.Files{Files: []prb.FileStat{
		{Path: "src/a.go"}, {Path: "src/c.go"}, {Path: "dist/app.js"}, {Path: "src/d.go"}, {Path: "docs/logo.png"},
	}}
	fake := newFake(nil)
	res := mustRun(t, newDeps(fake), req)

	if want := []string{"src/d.go", "src/c.go"}; !slices.Equal(res.Unreviewed, want) {
		t.Errorf("Unreviewed = %v, want %v", res.Unreviewed, want)
	}
	if res.Verdict != finding.VerdictRevisarAMano {
		t.Errorf("verdict %s, want revisar_a_mano", res.Verdict)
	}
	if s := fake.byName(t, "superior_sintesis").Prompt; !strings.Contains(s, `"src/c.go" quedó sin revisar`) {
		t.Errorf("the synthesis must name the file missing from the diff:\n%s", s)
	}
}

func TestPartialDiffMakesIncomplete(t *testing.T) {
	truncated := patchFile("src/a.go", "cambio_en_a")
	truncated.Truncated, truncated.OmittedLines, truncated.LinesAdded = true, 850, 900
	empty := prb.DiffFile{Path: "src/b.go", Status: "modified", LinesAdded: 3}
	tests := []struct {
		name       string
		req        func(t *testing.T) Request
		unreviewed []string
		gap        string // a line of Result.Gaps, empty when none
		line       string // a line of the synthesis' "Revisión incompleta"
	}{
		{"patch recortado", func(t *testing.T) Request { return newRequest(t, ModeLigero, truncated) },
			[]string{"src/a.go"}, "", `"src/a.go" quedó sin revisar: patch recortado: faltan 850 líneas`},
		{"patch vacío", func(t *testing.T) Request { return newRequest(t, ModeLigero, patchFile("src/a.go", "x"), empty) },
			[]string{"src/b.go"}, "", `"src/b.go" quedó sin revisar: patch vacío aunque cambió líneas`},
		{"contexto sin archivos", func(t *testing.T) Request {
			req := newRequest(t, ModeLigero)
			req.PR.Files = nil
			return req
		}, nil, gapNoFileList, gapNoFileList},
		{"total_files mayor", func(t *testing.T) Request {
			req := newRequest(t, ModeLigero)
			req.Diff.TotalFiles = 3
			return req
		}, nil, "el diff informa 3 archivos y trajo 1", "el diff informa 3 archivos y trajo 1"},
		{"diff recortado sin marca", func(t *testing.T) Request {
			req := newRequest(t, ModeLigero)
			req.Diff.Truncated = true
			return req
		}, nil, gapTruncated, gapTruncated},
		{"diff recortado con el archivo marcado", func(t *testing.T) Request {
			req := newRequest(t, ModeLigero, truncated)
			req.Diff.Truncated = true
			return req
		}, []string{"src/a.go"}, "", `"src/a.go" quedó sin revisar`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFake(nil)
			res := mustRun(t, newDeps(fake), tt.req(t))
			if res.Verdict != finding.VerdictRevisarAMano {
				t.Errorf("verdict %s, want revisar_a_mano", res.Verdict)
			}
			if !slices.Equal(res.Unreviewed, tt.unreviewed) {
				t.Errorf("Unreviewed = %v, want %v", res.Unreviewed, tt.unreviewed)
			}
			if tt.gap == "" && len(res.Gaps) != 0 || tt.gap != "" && !slices.Contains(res.Gaps, tt.gap) {
				t.Errorf("Gaps = %q, want %q", res.Gaps, tt.gap)
			}
			s := fake.byName(t, "superior_sintesis").Prompt
			i := strings.Index(s, "# Revisión incompleta")
			if i < 0 || !strings.Contains(s[i:], tt.line) {
				t.Errorf("the synthesis must be told %q:\n%s", tt.line, s)
			}
		})
	}
}

func TestDiffGapsIgnoreExcludedFiles(t *testing.T) {
	// A truncated lockfile explains the diff's truncated flag, and a lockfile
	// left out by --exclude may count in total_files: neither leaves code unread.
	lock := prb.DiffFile{Path: "package-lock.json", Status: "modified", LinesAdded: 5000, Truncated: true, OmittedLines: 4000}
	req := newRequest(t, ModeLigero, patchFile("src/a.go", "x"), lock)
	req.Diff.Truncated = true
	req.PR.Files.Files = append(req.PR.Files.Files, prb.FileStat{Path: "yarn.lock", LinesAdded: 10})
	req.Diff.TotalFiles = 3
	res := mustRun(t, newDeps(newFake(nil)), req)
	if res.Verdict != finding.VerdictAprobar || len(res.Gaps) != 0 || len(res.Unreviewed) != 0 {
		t.Errorf("verdict %s gaps %q unreviewed %v, want aprobar with no gaps", res.Verdict, res.Gaps, res.Unreviewed)
	}
}

func TestVerdictIgnoresMinorCap(t *testing.T) {
	fake := newFake(map[string]any{
		prompts.RoleBugs: specialistReply(hallazgo("src/a.go", 3, finding.SeverityBajo, finding.GroupA, "menor")),
	})
	d := newDeps(fake)
	d.Cfg.Thresholds.MaxMinorDetails = 0
	res := mustRun(t, d, newRequest(t, ModeLigero))
	if f := findingByID(t, res, "bugs-001"); f.Status != finding.StatusMenor {
		t.Errorf("with no slots the bajo goes to the minor details, got %s", f.Status)
	}
	if res.Verdict != finding.VerdictAprobarConComentarios {
		t.Errorf("verdict %s, want aprobar_con_comentarios: the cap only decides what is listed", res.Verdict)
	}
}
