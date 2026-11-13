// Package screens holds the Bubble Tea screens of the terminal UI.
package screens

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/eligibility"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// RowsLoadedMsg carries the result of a (re)load of the list.
type RowsLoadedMsg struct {
	Ready, Hidden []listdata.Row
	Err           error
}

// OpenModeMsg asks the router to open the mode picker for Row.
type OpenModeMsg struct{ Row listdata.Row }

// OpenReportMsg asks the router to open the last report of a PR.
type OpenReportMsg struct {
	Repo string
	PR   int
}

// OpenModelsMsg asks the router to open the models screen.
type OpenModelsMsg struct{}

// QuitMsg asks the router to leave the program.
type QuitMsg struct{}

const (
	defaultThreshold = 400
	// Lines of the screen that are not table rows: header, blank, column
	// titles, filter line, hidden line, separator, 5 detail lines, blank, keys.
	fixedLines = 13

	minWidthAuthor = 100 // below this the author column goes away
	minWidthBar    = 80  // below this the size bar goes away

	minWidthModelsHelp = 112 // the key help only lists m when it fits
)

// ListModel is the PR list screen: a table of PRs ready for review and the
// detail of the selected one below it.
type ListModel struct {
	repo string
	load func() tea.Cmd

	ready, hidden []listdata.Row
	loading       bool
	err           error
	loadedAt      time.Time

	cursor, top int
	showHidden  bool
	filter      string
	filtering   bool
	threshold   int

	width, height int
	now           func() time.Time
}

// NewList builds the screen for repo ("workspace/slug"); load starts a load
// whose result arrives as a RowsLoadedMsg.
func NewList(repo string, load func() tea.Cmd) ListModel {
	return ListModel{repo: repo, load: load, loading: true, threshold: defaultThreshold, now: time.Now}
}

// WithThreshold sets the size threshold (lines) of the bar and the warning.
func (m ListModel) WithThreshold(lines int) ListModel {
	if lines > 0 {
		m.threshold = lines
	}
	return m
}

// Init starts the first load.
func (m ListModel) Init() tea.Cmd { return m.startLoad() }

func (m ListModel) startLoad() tea.Cmd {
	if m.load == nil {
		return nil
	}
	return m.load()
}

// Update handles window sizes, loaded rows and key presses.
func (m ListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
	case RowsLoadedMsg:
		m.loading = false
		m.err = msg.Err
		if msg.Err == nil {
			m.ready, m.hidden = msg.Ready, msg.Hidden
			m.loadedAt = m.now()
		}
		m.cursor, m.top = 0, 0
		m.clamp()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m ListModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, emit(QuitMsg{})
	}
	if m.filtering {
		switch k {
		case "enter":
			m.filtering = false
		case "esc":
			m.filtering, m.filter = false, ""
		case "backspace":
			if r := []rune(m.filter); len(r) > 0 {
				m.filter = string(r[:len(r)-1])
			}
		case "up":
			m.cursor--
		case "down":
			m.cursor++
		default:
			if msg.Text != "" {
				m.filter += clean(msg.Text)
			}
		}
		m.clamp()
		return m, nil
	}
	switch k {
	case "up":
		m.cursor--
	case "down":
		m.cursor++
	case "enter":
		if row, ok := m.selected(); ok {
			return m, emit(OpenModeMsg{Row: row})
		}
	case "o":
		if row, ok := m.selected(); ok && row.Reviewed {
			return m, emit(OpenReportMsg{Repo: m.repo, PR: row.Ctx.PullRequest.ID})
		}
	case "/":
		if m.err == nil && !m.loading {
			m.filtering = true
		}
	case "t":
		m.showHidden = !m.showHidden
	case "r":
		m.loading, m.err = true, nil
		return m, m.startLoad()
	case "m":
		return m, emit(OpenModelsMsg{})
	case "q":
		return m, emit(QuitMsg{})
	}
	m.clamp()
	return m, nil
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// entry is a visible row: hidden ones carry their reason.
type entry struct {
	listdata.Row
	hidden bool
}

func (m ListModel) visible() []entry {
	var out []entry
	add := func(rows []listdata.Row, hidden bool) {
		for _, r := range rows {
			if m.matches(r) {
				out = append(out, entry{r, hidden})
			}
		}
	}
	add(m.ready, false)
	if m.showHidden {
		add(m.hidden, true)
	}
	return out
}

func (m ListModel) matches(r listdata.Row) bool {
	if m.filter == "" {
		return true
	}
	q := strings.ToLower(m.filter)
	pr := r.Ctx.PullRequest
	return strings.Contains(strings.ToLower(clean(pr.Title)), q) ||
		strings.Contains(strings.ToLower(clean(pr.Author)), q)
}

func (m ListModel) selected() (listdata.Row, bool) {
	v := m.visible()
	if m.cursor < 0 || m.cursor >= len(v) || m.loading || m.err != nil {
		return listdata.Row{}, false
	}
	return v[m.cursor].Row, true
}

func (m *ListModel) clamp() {
	n := len(m.visible())
	if m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scroll()
}

func (m *ListModel) scroll() {
	m.top = scrollTop(m.top, m.cursor, m.tableRows(m.height))
}

// tableRows is how many table rows fit in height; 0 means no limit.
func (m ListModel) tableRows(height int) int {
	if height <= 0 {
		return 0
	}
	return max(1, height-fixedLines)
}

func scrollTop(top, cursor, rows int) int {
	if rows <= 0 {
		return 0
	}
	if cursor < top {
		top = cursor
	}
	if cursor >= top+rows {
		top = cursor - rows + 1
	}
	return max(0, top)
}

// View renders the screen at the last known size.
func (m ListModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m ListModel) render(width, height int) string {
	var lines []string
	lines = append(lines, m.header(width), "")
	switch {
	case m.loading:
		lines = append(lines, theme.Dim.Render(" Cargando PRs de "+clean(m.repo)+"…"))
	case m.err != nil:
		lines = append(lines, m.errorLines()...)
	default:
		lines = append(lines, m.body(width, height)...)
	}
	keys := m.keys(width)
	if height > 0 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, " "+components.KeyHelp(keys))
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}

func (m ListModel) keys(width int) []components.Key {
	switch {
	case m.filtering:
		return []components.Key{{Keys: "enter", Desc: "aplicar"}, {Keys: "esc", Desc: "limpiar"}}
	case m.loading:
		return []components.Key{{Keys: "q", Desc: "salir"}}
	case m.err != nil:
		return []components.Key{{Keys: "r", Desc: "reintentar"}, {Keys: "q", Desc: "salir"}}
	}
	all := "ver todos"
	if m.showHidden {
		all = "solo listos"
	}
	ks := []components.Key{
		{Keys: "↑↓", Desc: "moverse"}, {Keys: "enter", Desc: "elegir modo"}, {Keys: "/", Desc: "filtrar"},
		{Keys: "t", Desc: all}, {Keys: "r", Desc: "actualizar"},
	}
	if width >= minWidthModelsHelp {
		ks = append(ks, components.Key{Keys: "m", Desc: "modelos"})
	}
	return append(ks, components.Key{Keys: "q", Desc: "salir"})
}

func (m ListModel) header(width int) string {
	left := theme.Strong.Render(" pr-review   ") + theme.Dim.Render(clean(m.repo))
	if m.loading || m.err != nil {
		return left
	}
	n := len(m.ready)
	right := fmt.Sprintf("%d listos para revisar", n)
	if n == 1 {
		right = "1 listo para revisar"
	}
	if !m.loadedAt.IsZero() {
		right += "   actualizado hace " + age(m.now().Sub(m.loadedAt))
	}
	pad := width - ansi.StringWidth(left) - ansi.StringWidth(right) - 1
	if pad < 2 {
		pad = 2
	}
	return left + strings.Repeat(" ", pad) + theme.Dim.Render(right)
}

func (m ListModel) errorLines() []string {
	var pe *prb.Error
	if errors.As(m.err, &pe) && pe.Code == "no_credentials" {
		return []string{theme.Text.Render(" No hay sesión de Bitbucket. Corré prb auth login y apretá r.")}
	}
	return []string{
		theme.Text.Render(" " + clean(m.err.Error())),
		theme.Dim.Render(" apretá r para reintentar"),
	}
}

func (m ListModel) body(width, height int) []string {
	var out []string
	v := m.visible()
	switch {
	case len(v) == 0 && m.filter != "":
		out = append(out, theme.Dim.Render(" Ningún PR coincide con el filtro."))
	case len(v) == 0:
		out = append(out, theme.Text.Render(" No hay PRs listos para revisar en "+clean(m.repo)+"."))
	default:
		out = append(out, m.table(v, width, height)...)
	}
	out = append(out, m.filterLine())
	if h := m.hiddenLine(); h != "" {
		out = append(out, h)
	}
	if m.cursor < len(v) && len(v) > 0 {
		out = append(out, m.detail(v[m.cursor].Row, width)...)
	}
	return out
}

func (m ListModel) filterLine() string {
	if m.filtering {
		return theme.Strong.Render(" /") + theme.Text.Render(" "+m.filter) + theme.Accent.Render("▏")
	}
	if m.filter != "" {
		return theme.Dim.Render(" filtro: ") + theme.Text.Render(m.filter)
	}
	return ""
}

func (m ListModel) hiddenLine() string {
	counts := map[eligibility.Reason]int{}
	for _, r := range m.hidden {
		counts[r.Reason]++
	}
	line := eligibility.HiddenLine(counts)
	if line == "" {
		return ""
	}
	hint := " para verlos"
	if m.showHidden {
		hint = " para ocultarlos"
	}
	return theme.Dim.Render(" "+line+"  ") + theme.Strong.Render("t") + theme.Dim.Render(hint)
}

type columns struct{ author, bar bool }

func colsFor(width int) columns {
	return columns{author: width >= minWidthAuthor, bar: width >= minWidthBar}
}

const (
	prefixW = 3
	numW    = 6
	authorW = 12
	linesW  = 4
	ageW    = 4
)

func (c columns) titleWidth(width int) int {
	w := width - prefixW - numW - ageW - 1 // one column of slack
	if c.author {
		w -= authorW
	}
	if c.bar {
		w -= 14 + 1 + linesW + 2 // bar, space, lines, gap
	} else {
		w -= 2 + linesW + 2
	}
	return max(w, 10)
}

func (m ListModel) table(v []entry, width, height int) []string {
	c := colsFor(width)
	tw := c.titleWidth(width)
	hdr := headerRow(c, tw)
	out := []string{theme.Dim.Render(hdr)}

	rows := m.tableRows(height)
	top := scrollTop(m.top, m.cursor, rows)
	end := len(v)
	if rows > 0 && top+rows < end {
		end = top + rows
	}
	for i := top; i < end; i++ {
		out = append(out, m.row(v[i], i == m.cursor, c, tw))
	}
	return out
}

func headerRow(c columns, tw int) string {
	s := pad("", prefixW) + pad("#", numW) + pad("título", tw)
	if c.author {
		s += pad("autor", authorW)
	}
	if c.bar {
		s += pad("tamaño (líneas)", 14+1+linesW+2) // bar + space + lines + gap
	} else {
		s += padLeft("líneas", 2+linesW) + "  "
	}
	return s + padLeft("hace", ageW)
}

func (m ListModel) row(e entry, selected bool, c columns, tw int) string {
	pr := e.Ctx.PullRequest
	prefix := "   "
	if selected {
		prefix = " " + theme.Accent.Render("▌") + " "
	}
	body, num := theme.Text, theme.Text
	switch {
	case e.hidden:
		body, num = theme.Dim, theme.Dim
	case selected:
		body, num = theme.Strong, theme.Strong
	}
	title := clean(pr.Title)
	reason := ""
	if e.hidden {
		reason = " · " + string(e.Reason)
		if rw := ansi.StringWidth(reason); rw < tw-6 {
			title = ansi.Truncate(title, tw-1-rw, "…")
			title += reason
		}
	}
	s := prefix + num.Render(pad(fmt.Sprint(pr.ID), numW)) + body.Render(pad(title, tw))
	if c.author {
		s += theme.Dim.Render(pad(clean(pr.Author), authorW))
	}
	lines := padLeft(fmt.Sprint(e.Lines), linesW)
	if c.bar {
		over := theme.Dim
		if e.Lines > m.threshold {
			over = theme.Accent
		}
		s += components.SizeBar(e.Lines, m.threshold) + over.Render(" "+lines) + "  "
	} else {
		s += theme.Dim.Render("  "+lines) + "  "
		if e.Lines > m.threshold {
			s = prefix + num.Render(pad(fmt.Sprint(pr.ID), numW)) + body.Render(pad(title, tw)) +
				theme.Accent.Render("  "+lines) + "  "
		}
	}
	return s + theme.Dim.Render(padLeft(age(m.now().Sub(pr.UpdatedOn)), ageW))
}

func (m ListModel) detail(r listdata.Row, width int) []string {
	pr := r.Ctx.PullRequest
	sep := theme.Tick.Render(strings.Repeat("─", max(0, width-2)))
	sep = " " + sep

	left := theme.Strong.Render(" #"+fmt.Sprint(pr.ID)+"  ") + theme.Strong.Render(clean(pr.Title))
	if len(r.Ctx.TaskReferences) > 0 {
		ref := theme.Ref.Render(clean(r.Ctx.TaskReferences[0].ID)) + theme.Dim.Render(" en ClickUp")
		avail := width - ansi.StringWidth(ref) - 3
		left = fit(left, avail)
		gap := max(2, width-ansi.StringWidth(left)-ansi.StringWidth(ref)-1)
		left += strings.Repeat(" ", gap) + ref
	}

	status := theme.Dim.Render(" CI ") + ciText(r.Ctx.Checks) +
		theme.Dim.Render("    tareas sin resolver ") + tasksText(r.Ctx.Tasks) +
		theme.Dim.Render("    revisores ") + theme.Text.Render(reviewersText(pr.Review))

	rev := theme.Dim.Render(" pr-review ")
	if r.Reviewed {
		rev += theme.Text.Render("lo revisó hace "+age(m.now().Sub(r.LastReview))) +
			"    " + theme.Strong.Render("o") + theme.Dim.Render(" abrir el último reporte")
	} else {
		rev += theme.Text.Render("nunca lo revisó")
	}

	size := " " + theme.Text.Render(sizeText(r.Ctx.Files))
	if r.Lines > m.threshold {
		warn := fmt.Sprintf("Es grande: %d líneas, más de las %d recomendadas.", r.Lines, m.threshold)
		if r.Lines > 2*m.threshold {
			warn = fmt.Sprintf("Es grande: %d líneas, más del doble de las %d recomendadas.", r.Lines, m.threshold)
		}
		size += " " + theme.Accent.Render(warn)
	}
	mode := theme.Text.Render(" Modo sugerido: " + string(r.Suggested) + ".")
	return []string{sep, left, status, rev, size, mode}
}

func ciText(c *prb.Checks) string {
	switch {
	case c == nil:
		return theme.Dim.Render("sin datos")
	case c.Overall == "none":
		return theme.Text.Render("sin CI")
	case c.Overall == "pass":
		total := c.Passed + c.Failed + c.Pending + c.Stopped
		if total == 0 {
			return theme.OK.Render("pasó")
		}
		return theme.OK.Render(fmt.Sprintf("pasó (%d de %d)", c.Passed, total))
	case c.Overall == "fail", c.Overall == "stopped":
		return theme.Text.Render("falló")
	case c.Overall == "pending":
		return theme.Text.Render("en curso")
	}
	return theme.Dim.Render("sin datos")
}

func tasksText(t *prb.Tasks) string {
	n := 0
	if t != nil {
		n = t.Unresolved
	}
	if n == 0 {
		return theme.OK.Render("0")
	}
	return theme.Accent.Render(fmt.Sprint(n))
}

func reviewersText(r prb.Review) string {
	total := max(len(r.Reviewers), r.Approvals)
	var s string
	switch {
	case total == 0:
		s = "sin revisores asignados"
	case r.Approvals == 1:
		s = fmt.Sprintf("1 de %d aprobó", total)
	default:
		s = fmt.Sprintf("%d de %d aprobaron", r.Approvals, total)
	}
	switch {
	case r.ChangesRequested == 0:
		s += ", nadie pidió cambios"
	case r.ChangesRequested == 1:
		s += ", 1 pidió cambios"
	default:
		s += fmt.Sprintf(", %d pidieron cambios", r.ChangesRequested)
	}
	return s
}

func sizeText(f *prb.Files) string {
	if f == nil {
		return "Sin datos de archivos."
	}
	return fmt.Sprintf("%d archivos, +%d −%d.", f.Count, f.LinesAdded, f.LinesRemoved)
}

// age renders a duration as "5 min", "3 h" or "2 d".
func age(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d min", max(1, int(d/time.Minute)))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h", int(d/time.Hour))
	}
	return fmt.Sprintf("%d d", int(d/(24*time.Hour)))
}

// clean makes PR-controlled text safe for one terminal line: whitespace
// controls become spaces, other control and invisible format characters
// (except the emoji joiner) are dropped.
func clean(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteRune(' ')
		case unicode.IsControl(r):
		case unicode.Is(unicode.Cf, r) && r != '‍':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pad truncates plain text to w display columns and right-pads it.
func pad(s string, w int) string {
	s = ansi.Truncate(clean(s), w, "…")
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

func padLeft(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return strings.Repeat(" ", max(0, w-ansi.StringWidth(s))) + s
}

// fit cuts a styled line to width display columns.
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
