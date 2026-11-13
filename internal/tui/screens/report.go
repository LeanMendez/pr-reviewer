package screens

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/report"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/sys"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

const (
	noWrap         = 1 << 20 // width used when the screen size is not known
	ruleWidth      = 96
	maxLocCol      = 40
	maxReasonRunes = 200
)

type copiedMsg struct{ err error }
type openedMsg struct{ err error }

// item is one thing the cursor can stand on: a finding, or the row that
// folds the minor details.
type item struct {
	fold bool
	f    finding.Finding
}

// ReportModel shows a finished review: what the PR introduces, what it
// does not, and everything the review could not check.
type ReportModel struct {
	res     pipeline.Result
	sys     sys.System
	savedAt string
	secs    report.Sections

	cursor    int
	expanded  bool // minor details unfolded
	discarded bool // discarded list shown
	note      string
	top       int  // first visible content line
	free      bool // the window moved away from the cursor: a resize keeps it there

	width, height int
}

// NewReport builds the screen for r; savedAt is the path of the saved
// review.md ("" when nothing was saved).
func NewReport(r pipeline.Result, s sys.System, savedAt string) ReportModel {
	return ReportModel{res: r, sys: s, savedAt: savedAt, secs: report.Split(r.Findings)}
}

// WithNote shows note in the status line until the next key, e.g. a warning
// from the review that produced the report.
func (m ReportModel) WithNote(note string) ReportModel {
	m.note = clean(note)
	return m
}

// Init does nothing: the report is already complete.
func (m ReportModel) Init() tea.Cmd { return nil }

// Update handles key presses, the results of system actions and window sizes.
func (m ReportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.free {
			m.scrollBy(0)
		} else {
			m.follow()
		}
	case copiedMsg:
		switch {
		case msg.err == nil:
			m.note = "Copiado."
		case errors.Is(msg.err, sys.ErrNoClipboard):
			m.note = "No encontré wl-copy, xclip ni xsel para copiar."
		default:
			m.note = "No pude copiar: " + clean(msg.err.Error())
		}
	case openedMsg:
		if msg.err != nil {
			m.note = "No pude abrir el navegador: " + clean(msg.err.Error())
		} else {
			m.note = "Abriendo en el navegador."
		}
	case tea.KeyPressMsg:
		m, cmd = m.key(msg)
	}
	return m, cmd
}

func (m ReportModel) items() []item {
	var out []item
	for _, f := range m.secs.Introduced {
		out = append(out, item{f: f})
	}
	if len(m.secs.Minor) > 0 {
		out = append(out, item{fold: true})
		if m.expanded {
			for _, f := range m.secs.Minor {
				out = append(out, item{f: f})
			}
		}
	}
	return out
}

func (m ReportModel) key(msg tea.KeyPressMsg) (ReportModel, tea.Cmd) {
	m.note = ""
	items := m.items()
	page := max(1, m.bodyHeight(m.height)-1)
	switch msg.String() {
	case "ctrl+c":
		return m, emit(QuitMsg{})
	case "q":
		return m, emit(BackMsg{})
	// Past the first and the last item the arrows scroll the window, so
	// what comes after the findings is reachable with them too.
	case "up":
		if m.cursor > 0 {
			m.cursor--
			m.follow()
		} else {
			m.scrollBy(-1)
		}
	case "down":
		if m.cursor < len(items)-1 {
			m.cursor++
			m.follow()
		} else {
			m.scrollBy(1)
		}
	case "pgup":
		m.scrollBy(-page)
	case "pgdown":
		m.scrollBy(page)
	case "home":
		m.scrollBy(-m.top)
	case "end":
		m.scrollBy(1 << 30)
	case "enter":
		if m.cursor < len(items) && items[m.cursor].fold {
			m.expanded = !m.expanded
			m.follow()
		}
	case "l":
		m.toggleDiscarded()
	case "c":
		if m.cursor >= len(items) || items[m.cursor].fold {
			m.note = "Elegí un hallazgo para copiar."
			break
		}
		s, text := m.sys, report.CommentText(items[m.cursor].f)
		return m, func() tea.Msg { return copiedMsg{err: s.Copy(context.Background(), text)} }
	case "o":
		url := strings.TrimSpace(m.res.PR.URL)
		if url == "" {
			m.note = "Este PR no tiene dirección web para abrir."
			break
		}
		s := m.sys
		return m, func() tea.Msg { return openedMsg{err: s.Open(context.Background(), url)} }
	}
	return m, nil
}

// follow keeps the selected item inside the window, now and on resizes.
func (m *ReportModel) follow() {
	m.free = false
	if m.height <= 0 {
		return
	}
	m.cursor = max(0, min(m.cursor, len(m.items())-1))
	lay := m.layout(m.width)
	m.top = lay.scroll(m.top, m.cursor, m.bodyHeight(m.height))
}

// scrollBy moves the window n lines, leaving the cursor where it is.
func (m *ReportModel) scrollBy(n int) {
	m.top = m.layout(m.width).clamp(m.top+n, m.bodyHeight(m.height))
	m.free = true
}

// toggleDiscarded shows or hides the discarded candidates; shown, they come
// into view. With none, a note says so.
func (m *ReportModel) toggleDiscarded() {
	if len(m.secs.Discarded) == 0 {
		m.note = "Ningún candidato descartado."
		return
	}
	m.discarded = !m.discarded
	if !m.discarded {
		m.scrollBy(0)
		return
	}
	lay := m.layout(m.width)
	m.top = lay.reveal(m.top, lay.disc[0], lay.disc[1], m.bodyHeight(m.height))
	m.free = true
}

// View renders the screen at the last known size.
func (m ReportModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ReportModel) bodyHeight(height int) int {
	h := height - 1 // key help
	if m.note != "" {
		h--
	}
	return max(1, h)
}

func (m ReportModel) render(width, height int) string {
	lay := m.layout(width)
	more := false // content continues below the window
	var out []string
	if height <= 0 {
		out = lay.lines
	} else {
		vh := m.bodyHeight(height)
		top := lay.clamp(m.top, vh)
		more = top+vh < len(lay.lines)
		vp := viewport.New(viewport.WithWidth(max(1, width)), viewport.WithHeight(vh))
		vp.SetContent(strings.Join(lay.lines, "\n"))
		vp.SetYOffset(top)
		view := strings.Split(vp.View(), "\n")
		for len(view) < vh {
			view = append(view, "")
		}
		out = view[:vh]
	}
	if m.note != "" {
		out = append(out[:len(out):len(out)], theme.Accent.Render("  "+m.note))
	}
	out = append(out[:len(out):len(out)], "  "+m.keyHelp(width, more))
	for i, l := range out {
		out[i] = fit(l, width)
	}
	return strings.Join(out, "\n")
}

// keyHelp lists the keys, with pgdn while more content is below the window;
// when they do not fit, the descriptions are shorter.
func (m ReportModel) keyHelp(width int, more bool) string {
	help := func(copyD, openD, backD string) string {
		ks := []components.Key{{Keys: "↑↓", Desc: "moverse"}}
		if more {
			ks = append(ks, components.Key{Keys: "pgdn", Desc: "más abajo"})
		}
		return components.KeyHelp(append(ks,
			components.Key{Keys: "enter", Desc: "expandir"}, components.Key{Keys: "c", Desc: copyD},
			components.Key{Keys: "l", Desc: "descartados"}, components.Key{Keys: "o", Desc: openD},
			components.Key{Keys: "q", Desc: backD}))
	}
	long := help("copiar hallazgo", "abrir en Bitbucket", "volver a la lista")
	if width > 0 && 2+ansi.StringWidth(long) > width {
		return help("copiar", "abrir", "volver")
	}
	return long
}

// layout is the whole report as lines, plus where each item starts and ends
// and where the discarded list is.
type layout struct {
	lines      []string
	start, end []int  // per item, first and last line
	disc       [2]int // first and last line of the discarded list
}

// clamp keeps top inside the content for a window of vh lines.
func (l layout) clamp(top, vh int) int {
	return max(0, min(top, len(l.lines)-vh))
}

// scroll returns the top line that keeps item idx visible in a window of
// vh lines, moving from top as little as possible.
func (l layout) scroll(top, idx, vh int) int {
	if idx >= len(l.start) {
		return l.clamp(top, vh)
	}
	if idx == 0 {
		return 0 // the header and verdict stay in view on the first item
	}
	return l.reveal(top, l.start[idx], l.end[idx], vh)
}

// reveal returns the top line that shows lines start..end in a window of vh
// lines (start first when they do not fit), moving from top as little as
// possible.
func (l layout) reveal(top, start, end, vh int) int {
	top = l.clamp(top, vh)
	if end >= top+vh {
		top = end - vh + 1
	}
	if start < top {
		top = start
	}
	return top
}

type seg struct {
	style lipgloss.Style
	text  string
}

type token struct {
	style lipgloss.Style
	text  string
	space bool // a space goes before it
}

// flow wraps segs into lines of at most width columns: first prefixes the
// first line and rest the others (both already styled). Words longer than a
// line are cut.
func flow(width int, first, rest string, segs ...seg) []string {
	if width <= 0 {
		width = noWrap
	}
	var toks []token
	pending := false // the previous segment ended with a space
	for _, s := range segs {
		text := clean(s.text)
		lead := pending || strings.HasPrefix(text, " ")
		for i, w := range strings.Fields(text) {
			toks = append(toks, token{s.style, w, i > 0 || lead})
		}
		if len(strings.Fields(text)) > 0 {
			pending = strings.HasSuffix(text, " ")
		} else {
			pending = pending || text != ""
		}
	}
	avail := func(prefix string) int { return max(8, width-ansi.StringWidth(prefix)) }
	var out []string
	prefix, line, used := first, "", 0
	flush := func() {
		out = append(out, prefix+line)
		prefix, line, used = rest, "", 0
	}
	for _, t := range toks {
		a := avail(prefix)
		var pieces []string
		if ansi.StringWidth(t.text) > a {
			pieces = strings.Split(ansi.Hardwrap(t.text, a, true), "\n")
		} else {
			pieces = []string{t.text}
		}
		for j, p := range pieces {
			w := ansi.StringWidth(p)
			sp := 0
			if used > 0 && t.space && j == 0 {
				sp = 1
			}
			if used > 0 && used+sp+w > avail(prefix) {
				flush()
				sp = 0
			}
			if sp == 1 {
				line += " "
			}
			line += t.style.Render(p)
			used += sp + w
			if j < len(pieces)-1 {
				flush()
			}
		}
	}
	if line != "" || len(out) == 0 {
		flush()
	}
	return out
}

// codeLines renders pre-formatted text (a fix) one source line per line,
// cutting what does not fit.
func codeLines(width int, indent, text string) []string {
	if width <= 0 {
		width = noWrap
	}
	var out []string
	for _, l := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		l = strings.TrimRight(clean(l), " ")
		style := theme.Text
		switch {
		case strings.HasPrefix(strings.TrimSpace(l), "+"):
			style = theme.OK
		case strings.HasPrefix(strings.TrimSpace(l), "-"):
			style = theme.Dim
		}
		for _, part := range strings.Split(ansi.Hardwrap(l, max(8, width-ansi.StringWidth(indent)), true), "\n") {
			out = append(out, indent+style.Render(part))
		}
	}
	return out
}

func loc(file string, line int) string {
	file = clean(file)
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

type row struct{ loc, text string }

// rowLines renders "location  text" rows, aligned in a column when the
// locations fit and wrapped inline otherwise.
func rowLines(width int, indent string, rows []row, locStyle, textStyle lipgloss.Style) []string {
	lw := 0
	for _, r := range rows {
		lw = max(lw, ansi.StringWidth(r.loc))
	}
	if lw > maxLocCol || (width > 0 && lw+ansi.StringWidth(indent)+12 > width) {
		lw = 0
	}
	var out []string
	for _, r := range rows {
		if lw > 0 {
			first := indent + locStyle.Render(r.loc+strings.Repeat(" ", lw-ansi.StringWidth(r.loc))) + "  "
			rest := strings.Repeat(" ", ansi.StringWidth(first))
			out = append(out, flow(width, first, rest, seg{textStyle, r.text})...)
			continue
		}
		out = append(out, flow(width, indent, indent+"  ", seg{locStyle, r.loc}, seg{textStyle, " " + r.text})...)
	}
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func (m ReportModel) layout(width int) layout {
	var lay layout
	add := func(ls ...string) { lay.lines = append(lay.lines, ls...) }
	rule := func() {
		n := ruleWidth
		if width > 0 {
			n = min(n, max(4, width-4))
		}
		add(theme.Tick.Render("  " + strings.Repeat("─", n)))
	}
	heading := func(title string, count int) {
		add(theme.Strong.Render("  "+title) + theme.Dim.Render(fmt.Sprintf("  %d", count)))
	}
	r := m.res

	// header and verdict
	meta := []string{}
	for _, s := range []string{r.Repo, r.PR.Author, string(r.Mode)} {
		if s = clean(s); s != "" {
			meta = append(meta, s)
		}
	}
	add(theme.Strong.Render(" pr-review   ")+theme.Text.Render(fmt.Sprintf("#%d %s", r.PR.ID, clean(r.PR.Title)))+
		theme.Dim.Render("   "+strings.Join(meta, ", ")), "")
	name, rest, _ := strings.Cut(report.VerdictText(r), ". ")
	add(flow(width, "  ", "  ", seg{theme.Strong, name + "."}, seg{theme.Text, " " + rest})...)
	add("")

	// what the PR introduces
	items := m.items()
	cursor := max(0, min(m.cursor, len(items)-1))
	lay.start = make([]int, len(items))
	lay.end = make([]int, len(items))
	mark := func(i int) string {
		if i == cursor {
			return " " + theme.Accent.Render("▌") + " "
		}
		return "   "
	}
	rule()
	heading("Lo que introduce este PR", len(m.secs.Introduced))
	if len(m.secs.Introduced) == 0 {
		add(theme.Dim.Render("  Ningún hallazgo nuevo."))
	}
	idx := 0
	for _, f := range m.secs.Introduced {
		add("")
		lay.start[idx] = len(lay.lines)
		add(m.findingLines(width, mark(idx), f)...)
		lay.end[idx] = len(lay.lines) - 1
		idx++
	}
	if len(m.secs.Minor) > 0 {
		add("")
		lay.start[idx], lay.end[idx] = len(lay.lines), len(lay.lines)
		n := len(m.secs.Minor)
		verb := "verl"
		if m.expanded {
			verb = "plegarl"
		}
		verb += "o"
		if n > 1 {
			verb += "s"
		}
		add(mark(idx) + theme.Dim.Render(plural(n, "detalle menor", "detalles menores")+"  ") +
			theme.Strong.Render("enter") + theme.Dim.Render(" para "+verb))
		idx++
		if m.expanded {
			for _, f := range m.secs.Minor {
				lay.start[idx] = len(lay.lines)
				first := mark(idx) + theme.Ref.Render(loc(f.File, f.Line)) + "  "
				add(flow(width, first, "     ", seg{theme.Text, f.What})...)
				lay.end[idx] = len(lay.lines) - 1
				idx++
			}
		}
	}

	if len(m.secs.Preexisting) > 0 {
		add("")
		rule()
		heading("Ya existía antes de este PR", len(m.secs.Preexisting))
		add(flow(width, "  ", "  ", seg{theme.Dim, "El PR toca código que ya tenía estos problemas. Sirven de contexto y no cuentan para el veredicto."})...)
		add("")
		var rows []row
		for _, f := range m.secs.Preexisting {
			rows = append(rows, row{loc(f.File, f.Line), f.What})
		}
		add(rowLines(width, "  ", rows, theme.Dim, theme.Dim)...)
	}
	if len(r.Doubts) > 0 {
		add("")
		rule()
		heading("Dudas", len(r.Doubts))
		add("")
		var rows []row
		for _, d := range r.Doubts {
			rows = append(rows, row{loc(d.File, d.Line), d.Text})
		}
		add(rowLines(width, "  ", rows, theme.Ref, theme.Text)...)
	}
	if len(r.Split) > 0 {
		add("")
		rule()
		heading("Cómo partir este PR", len(r.Split))
		add("")
		for i, p := range r.Split {
			first := "  " + theme.Dim.Render(fmt.Sprintf("%d  ", i+1))
			add(flow(width, first, "     ", seg{theme.Text, p.Title},
				seg{theme.Dim, fmt.Sprintf(" (%s, %d líneas)", strings.Join(p.Files, ", "), p.Lines)})...)
			if p.DependsOn > 0 {
				add(flow(width, "     ", "     ", seg{theme.Dim, fmt.Sprintf("Depende de la %d: se mergea después.", p.DependsOn)})...)
			}
		}
	}

	add("")
	rule()
	foot, disc := m.footer(width)
	lay.disc = [2]int{len(lay.lines), len(lay.lines) + max(0, disc-1)}
	add(foot...)
	return lay
}

// findingLines renders one finding of the introduced section.
func (m ReportModel) findingLines(width int, gutter string, f finding.Finding) []string {
	const body = "   "
	head := gutter + theme.SeverityLabel(f.Severity) + "  " + theme.Ref.Render(loc(f.File, f.Line))
	switch {
	case f.Status != finding.StatusConfirmado && f.Status != finding.StatusSinConfirmar:
		head += theme.Accent.Render("  sin validar")
	case f.Status == finding.StatusSinConfirmar:
		head += theme.Accent.Render("  sin confirmar")
	case f.Origin == finding.OriginIncierto:
		head += theme.Accent.Render("  origen incierto")
	}
	out := []string{head}
	out = append(out, flow(width, body, body, seg{theme.Strong, f.What})...)
	if strings.TrimSpace(f.Why) != "" {
		out = append(out, flow(width, body, body, seg{theme.Dim, "Por qué pasa: "}, seg{theme.Text, f.Why})...)
	}
	if fix := strings.TrimSpace(f.Fix); fix != "" {
		label := []seg{{theme.Dim, "Solución:"}}
		if f.Rule != nil {
			label = []seg{{theme.Dim, "Solución, según "}, {theme.Ref, f.Rule.File},
				{theme.Dim, ` ("` + f.Rule.Quote + `"):`}}
		}
		blockish := strings.Contains(fix, "\n") || strings.HasPrefix(fix, "+") || strings.HasPrefix(fix, "-")
		if blockish {
			out = append(out, flow(width, body, body, label...)...)
			out = append(out, codeLines(width, body+"  ", fix)...)
		} else {
			out = append(out, flow(width, body, body, append(label, seg{theme.Text, " " + fix})...)...)
		}
	}
	if f.FixStatus == finding.FixStatusARevisar {
		out = append(out, flow(width, body, body,
			seg{theme.Accent, "Los jueces marcaron esta solución para revisar: puede chocar con una regla del repo."})...)
	}
	if len(f.Votes) > 0 {
		out = append(out, flow(width, body, body, seg{theme.Dim, "Jueces: "}, seg{theme.Text, report.JudgesText(f.Votes)})...)
	}
	return out
}

// footer says what was not shown or not checked; disc is how many of its
// first lines are the discarded list.
func (m ReportModel) footer(width int) (out []string, disc int) {
	r := m.res
	dim := func(s string) { out = append(out, flow(width, "  ", "  ", seg{theme.Dim, s})...) }
	warn := func(prefix, s string) {
		out = append(out, flow(width, prefix, strings.Repeat(" ", ansi.StringWidth(prefix)), seg{theme.Accent, s})...)
	}

	if n := len(m.secs.Discarded); n > 0 {
		hint := "l para ver por qué"
		if m.discarded {
			hint = "l para ocultarlos"
		}
		out = append(out, theme.Dim.Render("  Descartados: "+plural(n, "candidato", "candidatos")+".  ")+
			theme.Strong.Render("l")+theme.Dim.Render(strings.TrimPrefix(hint, "l")))
		if m.discarded {
			var rows []row
			for _, f := range m.secs.Discarded {
				reason := clean(f.Reason)
				if strings.TrimSpace(reason) == "" {
					reason = "sin motivo registrado"
				}
				rows = append(rows, row{loc(f.File, f.Line), f.What + " Motivo: " + reason})
			}
			out = append(out, rowLines(width, "    ", rows, theme.Ref, theme.Dim)...)
		}
	}
	disc = len(out)
	if len(r.Unreviewed) > 0 {
		warn("  ", "Sin revisar: "+strings.Join(r.Unreviewed, ", ")+".")
	}
	if len(r.Excluded) > 0 {
		dim("Excluidos de la revisión (generados): " + strings.Join(r.Excluded, ", ") + ".")
	}
	if len(r.Gaps) > 0 {
		warn("  ", "No se pudo comprobar que el diff esté completo:")
		for _, g := range r.Gaps {
			warn("    · ", g)
		}
	}
	if lines := m.failureLines(); len(lines) > 0 {
		warn("  ", "Agentes que fallaron (lo que cubrían no se revisó del todo):")
		for _, l := range lines {
			warn("    ✗ ", l)
		}
	}
	if len(r.RuleFiles) == 0 {
		dim("Reglas usadas: ninguna encontrada.")
	} else {
		paths := make([]string, len(r.RuleFiles))
		for i, f := range r.RuleFiles {
			paths[i] = f.Path
		}
		dim("Reglas usadas: " + strings.Join(paths, ", ") + ".")
	}
	if m.savedAt != "" {
		dim("Guardado en " + m.savedAt)
	}
	return out, disc
}

// failureLines is one line per failed agent: role, what it covered and why
// it failed. Agents named in FailedAgents without a Failure still show up.
func (m ReportModel) failureLines() []string {
	var lines []string
	seen := map[string]bool{}
	for _, f := range m.res.Failures {
		seen[f.Agent] = true
		name, _ := roleLabel(config.Role(f.Role))
		l := name
		switch {
		case len(f.Files) > 0:
			l += ": " + strings.Join(f.Files, ", ")
		case f.Candidate != "":
			l += ": candidato " + f.Candidate
		}
		first, _, _ := strings.Cut(strings.TrimSpace(f.Reason), "\n")
		if reason := ansi.Truncate(clean(first), maxReasonRunes, "…"); strings.TrimSpace(reason) != "" {
			l += ". Motivo: " + reason
		}
		lines = append(lines, l)
	}
	for _, a := range m.res.FailedAgents {
		if !seen[a] {
			lines = append(lines, a)
		}
	}
	return lines
}
