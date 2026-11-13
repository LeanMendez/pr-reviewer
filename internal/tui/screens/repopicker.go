package screens

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// ReposLoadedMsg carries the repos of the workspace.
type ReposLoadedMsg struct {
	Repos []prb.Repo
	Err   error
}

// RepoChosenMsg reports the repo picked, as "workspace/slug".
type RepoChosenMsg struct{ Repo string }

// Lines of the picker that are not repo rows: header, blank, title, blank,
// filter line, key help.
const pickerFixedLines = 6

const repoSlugW = 32

// RepoPickerModel lists the repos of the workspace to pick one; it opens
// when pr-review gets no repo and is not run inside a Bitbucket checkout.
type RepoPickerModel struct {
	workspace string
	load      func() tea.Cmd

	repos   []prb.Repo
	loading bool
	err     error

	cursor, top int
	filter      filterInput

	width, height int
}

// NewRepoPicker builds the picker for workspace; load starts a load whose
// result arrives as a ReposLoadedMsg.
func NewRepoPicker(workspace string, load func() tea.Cmd) RepoPickerModel {
	return RepoPickerModel{workspace: workspace, load: load, loading: true}
}

// Init starts the first load.
func (m RepoPickerModel) Init() tea.Cmd { return m.startLoad() }

func (m RepoPickerModel) startLoad() tea.Cmd {
	if m.load == nil {
		return nil
	}
	return m.load()
}

// Update handles window sizes, loaded repos and key presses.
func (m RepoPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
	case ReposLoadedMsg:
		m.loading, m.err = false, msg.Err
		if msg.Err == nil {
			m.repos = slices.Clone(msg.Repos)
			slices.SortStableFunc(m.repos, func(a, b prb.Repo) int {
				return strings.Compare(strings.ToLower(a.Slug), strings.ToLower(b.Slug))
			})
		}
		m.cursor, m.top = 0, 0
		m.clamp()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m RepoPickerModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, emit(QuitMsg{})
	}
	if m.filter.active {
		m.cursor += m.filter.key(msg)
		m.clamp()
		return m, nil
	}
	switch k {
	case "up":
		m.cursor--
	case "down":
		m.cursor++
	case "enter":
		if r, ok := m.selected(); ok {
			return m, emit(RepoChosenMsg{Repo: m.fullName(r)})
		}
	case "/":
		if !m.loading && m.err == nil {
			m.filter.start()
		}
	case "r":
		m.loading, m.err = true, nil
		return m, m.startLoad()
	case "q":
		return m, emit(QuitMsg{})
	}
	m.clamp()
	return m, nil
}

// fullName is "workspace/slug", from prb's full name when it has one.
func (m RepoPickerModel) fullName(r prb.Repo) string {
	if strings.Contains(r.FullName, "/") {
		return r.FullName
	}
	return m.workspace + "/" + r.Slug
}

func (m RepoPickerModel) visible() []prb.Repo {
	if m.filter.text == "" {
		return m.repos
	}
	var out []prb.Repo
	for _, r := range m.repos {
		if m.filter.matches(r.Slug) || m.filter.matches(r.Name) {
			out = append(out, r)
		}
	}
	return out
}

func (m RepoPickerModel) selected() (prb.Repo, bool) {
	v := m.visible()
	if m.loading || m.err != nil || m.cursor < 0 || m.cursor >= len(v) {
		return prb.Repo{}, false
	}
	return v[m.cursor], true
}

func (m *RepoPickerModel) clamp() {
	n := len(m.visible())
	m.cursor = max(0, min(m.cursor, n-1))
	m.top = scrollTop(m.top, m.cursor, m.rows(m.height))
}

// rows is how many repo rows fit in height; 0 means no limit.
func (m RepoPickerModel) rows(height int) int {
	if height <= 0 {
		return 0
	}
	return max(1, height-pickerFixedLines)
}

// View renders the screen at the last known size.
func (m RepoPickerModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m RepoPickerModel) render(width, height int) string {
	lines := []string{m.header(width), ""}
	switch {
	case m.loading:
		lines = append(lines, theme.Dim.Render(" Cargando repos de "+clean(m.workspace)+"…"))
	case m.err != nil:
		lines = append(lines, m.errorLines()...)
	default:
		lines = append(lines, m.body(height)...)
	}
	if height > 0 {
		for len(lines) < height-1 {
			lines = append(lines, "")
		}
	} else {
		lines = append(lines, "")
	}
	lines = append(lines, " "+components.KeyHelp(m.keys()))
	if height > 0 && len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, l := range lines {
		lines[i] = fit(l, width)
	}
	return strings.Join(lines, "\n")
}

func (m RepoPickerModel) header(width int) string {
	left := " " + theme.Brand("pr-review") + "   " + theme.Dim.Render(clean(m.workspace))
	if m.loading || m.err != nil {
		return left
	}
	right := fmt.Sprintf("%d repos", len(m.repos))
	if len(m.repos) == 1 {
		right = "1 repo"
	}
	gap := max(2, width-ansi.StringWidth(left)-ansi.StringWidth(right)-1)
	return left + strings.Repeat(" ", gap) + theme.Dim.Render(right)
}

func (m RepoPickerModel) errorLines() []string {
	var pe *prb.Error
	if errors.As(m.err, &pe) && pe.Code == "no_credentials" {
		return []string{theme.Text.Render(" No hay sesión de Bitbucket. Corré prb auth login y apretá r.")}
	}
	return []string{
		theme.Text.Render(" " + clean(m.err.Error())),
		theme.Dim.Render(" apretá r para reintentar"),
	}
}

func (m RepoPickerModel) body(height int) []string {
	out := []string{theme.Text.Render(" Elegí un repo"), ""}
	v := m.visible()
	switch {
	case len(m.repos) == 0:
		out = append(out, theme.Text.Render(" No hay repos en "+clean(m.workspace)+"."))
	case len(v) == 0:
		out = append(out, theme.Dim.Render(" Ningún repo coincide con el filtro."))
	default:
		rows := m.rows(height)
		top := scrollTop(m.top, m.cursor, rows)
		end := len(v)
		if rows > 0 && top+rows < end {
			end = top + rows
		}
		for i := top; i < end; i++ {
			out = append(out, m.row(v[i], i == m.cursor))
		}
	}
	if f := m.filter.line(); f != "" {
		out = append(out, f)
	}
	return out
}

func (m RepoPickerModel) row(r prb.Repo, selected bool) string {
	prefix, style := "   ", theme.Text
	if selected {
		prefix, style = " "+theme.Marker()+" ", theme.Strong
	}
	s := prefix + style.Render(pad(r.Slug, repoSlugW))
	if name := clean(r.Name); name != "" && name != clean(r.Slug) {
		s += theme.Dim.Render(name)
	}
	return s
}

func (m RepoPickerModel) keys() []components.Key {
	switch {
	case m.filter.active:
		return []components.Key{{Keys: "enter", Desc: "aplicar"}, {Keys: "esc", Desc: "limpiar"}}
	case m.loading:
		return []components.Key{{Keys: "q", Desc: "salir"}}
	case m.err != nil:
		return []components.Key{{Keys: "r", Desc: "reintentar"}, {Keys: "q", Desc: "salir"}}
	}
	return []components.Key{
		{Keys: "↑↓", Desc: "moverse"}, {Keys: "enter", Desc: "abrir"}, {Keys: "/", Desc: "filtrar"},
		{Keys: "r", Desc: "actualizar"}, {Keys: "q", Desc: "salir"},
	}
}
