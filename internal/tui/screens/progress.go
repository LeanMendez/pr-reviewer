package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// EventMsg wraps a pipeline event delivered to the screen.
type EventMsg pipeline.Event

// ReviewDoneMsg ends the review: the router sends it when pipeline.Run returns.
type ReviewDoneMsg struct {
	Result pipeline.Result
	Err    error
}

// BackgroundMsg asks the router to keep reviewing PR in the background.
type BackgroundMsg struct{ PR int }

// OpenResultMsg asks the router to open the report of a finished review.
type OpenResultMsg struct{ Result pipeline.Result }

type tickMsg struct{}

const (
	logLines   = 5
	stageNameW = 26
	modelW     = 9
	elapsedW   = 7
	candW      = 13
)

var stageDesc = map[pipeline.Stage]string{
	pipeline.StagePrechecks:   "título con tarea, sin secretos, toca migraciones",
	pipeline.StageRules:       "CLAUDE.md y archivos de reglas, leídos de la rama base",
	pipeline.StagePlan:        "reparte los archivos entre los especialistas",
	pipeline.StageSpecialists: "corren en paralelo",
	pipeline.StageValidation:  "confirma o descarta cada candidato grave",
	pipeline.StageJudges:      "dos jueces, cada uno por su lado",
	pipeline.StageReport:      "lo nuevo del PR, lo que ya existía, cómo partirlo",
}

type stageState struct {
	started, finished time.Time
	done              bool
}

type agentState struct {
	name       string
	stage      pipeline.Stage
	role       config.Role
	model      config.Model
	activity   string
	log        []string
	candidates int
	started    time.Time
	finished   time.Time
	done       bool
	failed     bool
	err        string
}

// ProgressModel is the review progress screen.
type ProgressModel struct {
	row    listdata.Row
	mode   pipeline.Mode
	events <-chan pipeline.Event
	cancel context.CancelFunc

	stages     map[pipeline.Stage]*stageState
	current    pipeline.Stage
	agents     []*agentState
	candidates int

	showLog    bool
	cancelling bool
	finished   bool
	err        error

	width, height int
	now           func() time.Time
}

// NewProgress builds the screen for row; events is the channel the review
// writes to and cancel stops it.
func NewProgress(row listdata.Row, mode pipeline.Mode, events <-chan pipeline.Event, cancel context.CancelFunc) ProgressModel {
	return ProgressModel{
		row: row, mode: mode, events: events, cancel: cancel,
		stages: map[pipeline.Stage]*stageState{}, now: time.Now,
	}
}

// WithClock replaces the clock used for elapsed times (tests).
func (m ProgressModel) WithClock(now func() time.Time) ProgressModel {
	m.now = now
	return m
}

// waitEvent waits for the next event of ch and delivers it as an EventMsg.
// A closed channel yields no message.
func waitEvent(ch <-chan pipeline.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return EventMsg(ev)
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return tickMsg{} })
}

// Init starts listening for events and ticking the clock.
func (m ProgressModel) Init() tea.Cmd { return tea.Batch(waitEvent(m.events), tick()) }

// Update handles events, ticks, the end of the review and key presses.
func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.finished {
			return m, nil
		}
		return m, tick() // only triggers a re-render: elapsed times come from now()
	case EventMsg:
		if m.finished {
			return m, nil
		}
		m.apply(pipeline.Event(msg))
		return m, waitEvent(m.events)
	case ReviewDoneMsg:
		m.finished, m.err = true, msg.Err
		if msg.Err == nil {
			return m, emit(OpenResultMsg{Result: msg.Result})
		}
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m ProgressModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, emit(QuitMsg{})
	case "q":
		if m.finished && m.err != nil {
			return m, emit(BackMsg{})
		}
	case "s":
		if !m.finished {
			return m, emit(BackgroundMsg{PR: m.row.Ctx.PullRequest.ID})
		}
	case "c":
		if !m.finished && !m.cancelling {
			m.cancelling = true
			if m.cancel != nil {
				m.cancel()
			}
		}
	case "l":
		m.showLog = !m.showLog
	}
	return m, nil
}

func (m *ProgressModel) apply(ev pipeline.Event) {
	switch ev.Kind {
	case pipeline.StageStarted:
		m.stages[ev.Stage] = &stageState{started: m.now()}
		m.current = ev.Stage
		if ev.Stage == pipeline.StageValidation {
			m.candidates = ev.Candidates
		}
	case pipeline.StageDone:
		st := m.stages[ev.Stage]
		if st == nil {
			st = &stageState{started: m.now()}
			m.stages[ev.Stage] = st
		}
		st.done, st.finished = true, m.now()
		if ev.Stage == pipeline.StageValidation {
			m.candidates = ev.Candidates
		}
		if m.current == ev.Stage {
			m.current = 0
		}
	case pipeline.AgentStarted:
		m.agents = append(m.agents, &agentState{
			name: ev.Agent, stage: ev.Stage, role: ev.Role, model: ev.Model, started: m.now(),
		})
	case pipeline.AgentActivity:
		if a := m.agent(ev.Agent); a != nil && !a.done {
			a.activity = clean(ev.Text)
			a.log = append(a.log, a.activity)
			if len(a.log) > logLines {
				a.log = a.log[len(a.log)-logLines:]
			}
		}
	case pipeline.AgentDone:
		if a := m.agent(ev.Agent); a != nil {
			a.done, a.finished, a.candidates = true, m.now(), ev.Candidates
			if ev.Stage == pipeline.StageSpecialists {
				m.candidates += ev.Candidates
			}
		}
	case pipeline.AgentFailed:
		if a := m.agent(ev.Agent); a != nil {
			a.done, a.failed, a.finished = true, true, m.now()
			if ev.Err != nil {
				a.err = clean(ev.Err.Error())
			}
		}
	}
}

func (m *ProgressModel) agent(name string) *agentState {
	for _, a := range m.agents {
		if a.name == name {
			return a
		}
	}
	return nil
}

// View renders the screen at the last known size.
func (m ProgressModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ProgressModel) render(width, height int) string {
	pr := m.row.Ctx.PullRequest
	lines := []string{
		theme.Strong.Render(" pr-review   ") + theme.Text.Render(fmt.Sprintf("#%d %s", pr.ID, clean(pr.Title))) +
			theme.Dim.Render("   modo "+string(m.mode)),
		"",
	}
	stages := pipeline.Stages(m.mode)
	doneCount := 0
	agentAt := -1 // index in lines where the agent block goes
	for _, s := range stages {
		st := m.stages[s]
		if st != nil && st.done {
			doneCount++
		}
		lines = append(lines, m.stageLine(s, st, width))
		if st != nil && !st.done && s == m.current {
			agentAt = len(lines)
		}
	}
	lines = append(lines, "")
	lines = append(lines, m.summary(doneCount, len(stages))...)
	lines = append(lines, "")
	switch {
	case m.finished && m.err != nil:
		msg := clean(m.err.Error())
		if m.cancelling || errors.Is(m.err, context.Canceled) {
			msg = "La revisión se canceló."
		}
		lines = append(lines, theme.Accent.Render("  "+msg), "",
			"  "+components.KeyHelp([]components.Key{{Keys: "q", Desc: "volver a la lista"}}))
	case m.cancelling:
		lines = append(lines, theme.Accent.Render("  Cancelando…"))
	default:
		lines = append(lines, "  "+components.KeyHelp(m.keys()))
	}
	if agentAt >= 0 {
		budget := 0 // 0 means no limit
		if height > 0 {
			budget = max(0, height-len(lines))
		}
		block := m.agentLines(width, budget, height > 0)
		lines = append(lines[:agentAt], append(block, lines[agentAt:]...)...)
	}
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}

func (m ProgressModel) keys() []components.Key {
	log := "ver qué hace cada agente"
	if m.showLog {
		log = "ocultar el detalle"
	}
	return []components.Key{
		{Keys: "s", Desc: "seguir en segundo plano"}, {Keys: "l", Desc: log}, {Keys: "c", Desc: "cancelar"},
	}
}

func (m ProgressModel) summary(done, total int) []string {
	n := m.candidates
	word := "candidatos"
	if n == 1 {
		word = "candidato"
	}
	return []string{
		theme.Text.Render(fmt.Sprintf("  %d %s hasta ahora. ", n, word)) +
			theme.Dim.Render("No se muestran hasta que pasen la validación: muchos se van a caer."),
		theme.Dim.Render(fmt.Sprintf("  Esta revisión usa %d agentes en total. Va %d de %d etapas.",
			pipeline.EstimateAgents(m.mode), done, total)),
	}
}

func (m ProgressModel) stageLine(s pipeline.Stage, st *stageState, width int) string {
	dw := 60
	if width > 0 {
		dw = max(10, width-stageNameW-elapsedW-9)
	}
	glyph, name, desc := theme.Dim.Render("· "), theme.Dim, theme.Dim
	elapsed := ""
	switch {
	case st != nil && st.done:
		glyph, name = theme.OK.Render("✓ "), theme.Strong
		elapsed = fmtElapsed(st.finished.Sub(st.started))
	case st != nil:
		glyph, name, desc = theme.Accent.Render("● "), theme.Strong, theme.Text
		elapsed = fmtElapsed(m.now().Sub(st.started))
	}
	d := stageDesc[s]
	if s == pipeline.StageValidation && st != nil && !st.done {
		d = fmt.Sprintf("confirma o descarta cada candidato grave (%d)", m.candidates)
	}
	return "  " + theme.Dim.Render(fmt.Sprintf("%-3d", int(s))) + glyph + name.Render(pad(s.Name(), stageNameW)) +
		desc.Render(pad(d, dw)) + theme.Dim.Render(padLeft(elapsed, elapsedW))
}

// agentLabel is the lowercase role name; a validator also names the candidate
// it checks (its agent name is "<role>-<candidate id>").
func agentLabel(a *agentState) string {
	name, _ := roleLabel(a.role)
	label := strings.ToLower(name)
	if a.role == config.RoleValidadorBugs || a.role == config.RoleValidadorReglas {
		if id := strings.TrimPrefix(clean(a.name), string(a.role)+"-"); id != "" && id != clean(a.name) {
			label += " · " + id
		}
	}
	return label
}

// agentLines renders the agents of the running stage in at most budget lines
// (0 means no limit). Running agents come first; what does not fit becomes
// a "y N más" line.
func (m ProgressModel) agentLines(width, budget int, limited bool) []string {
	var shown []*agentState
	for _, a := range m.agents {
		if a.stage == m.current && !a.done {
			shown = append(shown, a)
		}
	}
	for _, a := range m.agents {
		if a.stage == m.current && a.done {
			shown = append(shown, a)
		}
	}
	roleCol := 18
	for _, a := range shown {
		roleCol = max(roleCol, min(34, ansi.StringWidth(agentLabel(a))+1))
	}
	specialists := m.current == pipeline.StageSpecialists
	cw := 0
	if specialists {
		cw = candW + 3
	}
	aw := 40
	if width > 0 {
		aw = max(10, width-8-2-roleCol-modelW-elapsedW-cw-1)
	}
	var out []string
	for i, a := range shown {
		entry := m.agentEntry(a, roleCol, aw, specialists)
		if limited {
			left := len(shown) - i - 1
			need := len(entry)
			if left > 0 {
				need++ // room for the "y N más" line
			}
			if len(out)+need > budget {
				if len(out)+1 <= budget {
					out = append(out, theme.Dim.Render(fmt.Sprintf("        y %d más", len(shown)-i)))
				}
				return out
			}
		}
		out = append(out, entry...)
	}
	return out
}

func (m ProgressModel) agentEntry(a *agentState, roleCol, aw int, specialists bool) []string {
	const indent = "        "
	glyph, activity, style := theme.Accent.Render("● "), a.activity, theme.Dim
	end := m.now()
	switch {
	case a.failed:
		glyph, style = theme.Accent.Render("✗ "), theme.Accent
		activity = "falló"
		if a.err != "" {
			activity += ": " + a.err
		}
		end = a.finished
	case a.done:
		glyph, activity = theme.OK.Render("✓ "), "terminó"
		end = a.finished
	case activity == "":
		activity = "arrancando"
	}
	line := indent + glyph +
		theme.Text.Render(pad(agentLabel(a), roleCol)) +
		theme.Ref.Render(pad(string(a.model), modelW)) +
		style.Render(pad(activity, aw)) +
		theme.Dim.Render(padLeft(fmtElapsed(end.Sub(a.started)), elapsedW))
	if specialists {
		cand := ""
		if a.done && !a.failed {
			switch a.candidates {
			case 0:
				cand = "0 candidatos"
			case 1:
				cand = "1 candidato"
			default:
				cand = fmt.Sprintf("%d candidatos", a.candidates)
			}
		}
		line += "   " + theme.Dim.Render(pad(cand, candW))
	}
	out := []string{line}
	if m.showLog {
		if len(a.log) == 0 {
			out = append(out, theme.Tick.Render(indent+"    (sin actividad registrada)"))
		}
		for _, l := range a.log {
			out = append(out, theme.Dim.Render(indent+"    · "+l))
		}
	}
	return out
}

// fmtElapsed renders a duration as "12 s" or "1m 14s".
func fmtElapsed(d time.Duration) string {
	s := max(0, int(d.Seconds()))
	if s < 60 {
		return fmt.Sprintf("%d s", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}
