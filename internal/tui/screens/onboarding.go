package screens

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/components"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
)

// toolCheckTimeout bounds each external check so a hung tool cannot freeze
// the setup screen.
const toolCheckTimeout = 10 * time.Second

// OnboardingDeps are the collaborators the first-run setup needs.
type OnboardingDeps struct {
	PRB  prb.Client
	Exec execx.Exec
	Save func(config.Config) error
}

// OnboardingDoneMsg reports that the setup finished and the config was saved.
type OnboardingDoneMsg struct{ Cfg config.Config }

// toolsCheckedMsg carries the result of the prb and claude checks.
type toolsCheckedMsg struct {
	identity  prb.Identity
	authErr   error
	version   string
	claudeErr error
	// The Claude Code session, checked when claude runs: loggedIn, or the
	// error that kept it from being known.
	loggedIn   bool
	sessionErr error
}

type onboardingStep int

const (
	stepTools onboardingStep = iota
	stepWorkspace
	stepModels
	stepCustom
)

// OnboardingModel is the first-run setup: tools check, workspace, models.
type OnboardingModel struct {
	deps  OnboardingDeps
	save  func(config.Config) error
	step  onboardingStep
	input textinput.Model

	checking bool
	checked  toolsCheckedMsg
	wsErr    string
	saveErr  error

	picker PresetPickerModel
	models ModelsModel
	// workspace is the validated value carried into step 3.
	workspace string

	width, height int
}

// NewOnboarding builds the setup screen; it starts on the tools check.
func NewOnboarding(deps OnboardingDeps) OnboardingModel {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "mi-workspace"
	in.CharLimit = 64
	in.SetWidth(32)
	return OnboardingModel{deps: deps, save: deps.Save, input: in, checking: true}
}

// Init starts the tool checks.
func (m OnboardingModel) Init() tea.Cmd { return m.checkTools() }

// checkTools runs `prb auth status`, `claude --version` and, when claude
// runs, `claude auth status` off the Update loop, each with its own timeout.
func (m OnboardingModel) checkTools() tea.Cmd {
	deps := m.deps
	return func() tea.Msg {
		var out toolsCheckedMsg
		ctx, cancel := context.WithTimeout(context.Background(), toolCheckTimeout)
		out.identity, out.authErr = deps.PRB.AuthStatus(ctx)
		cancel()

		ctx, cancel = context.WithTimeout(context.Background(), toolCheckTimeout)
		var buf bytes.Buffer
		out.claudeErr = deps.Exec(ctx, execx.Cmd{Name: "claude", Args: []string{"--version"}, Stdout: &buf})
		cancel()
		if out.claudeErr != nil {
			return out
		}
		first, _, _ := strings.Cut(strings.TrimSpace(buf.String()), "\n")
		out.version = strings.TrimSpace(clean(first))

		ctx, cancel = context.WithTimeout(context.Background(), toolCheckTimeout)
		defer cancel()
		out.loggedIn, out.sessionErr = claudeSession(ctx, deps.Exec)
		return out
	}
}

// claudeSession asks `claude auth status --json` whether Claude Code has a
// session. Without one it exits 1, and its JSON says so too.
func claudeSession(ctx context.Context, ex execx.Exec) (bool, error) {
	var buf bytes.Buffer
	err := ex(ctx, execx.Cmd{Name: "claude", Args: []string{"auth", "status", "--json"}, Stdout: &buf})
	var ee *execx.ExitError
	if err != nil && !errors.As(err, &ee) {
		return false, err
	}
	var st struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal(buf.Bytes(), &st) != nil || st.LoggedIn == nil {
		if err != nil {
			return false, err
		}
		return false, errors.New("claude auth status no dijo si hay sesión")
	}
	return *st.LoggedIn, nil
}

func (m OnboardingModel) authOK() bool {
	return m.checked.authErr == nil && m.checked.identity.Authenticated
}
func (m OnboardingModel) claudeOK() bool {
	return m.checked.claudeErr == nil && m.checked.sessionErr == nil && m.checked.loggedIn
}
func (m OnboardingModel) toolsOK() bool { return !m.checking && m.authOK() && m.claudeOK() }

// Update handles checks, sizes and keys, and routes child messages.
func (m OnboardingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.resized(), nil
	case toolsCheckedMsg:
		m.checking = false
		m.checked = msg
		if m.authOK() && strings.TrimSpace(m.input.Value()) == "" {
			m.input.SetValue(strings.TrimSpace(clean(msg.identity.DefaultWorkspace)))
			m.input.CursorEnd()
		}
		return m, nil
	case PresetChosenMsg:
		cfg := m.baseConfig(msg.Preset)
		if err := m.save(cfg); err != nil {
			m.saveErr = err
			return m, nil
		}
		return m, emit(OnboardingDoneMsg{Cfg: cfg})
	case CustomChosenMsg:
		m.models = NewModels(m.baseConfig(config.PresetBalanceado), m.save)
		m.step = stepCustom
		m.saveErr = nil
		return m.resized(), nil
	case ModelsSavedMsg:
		return m, emit(OnboardingDoneMsg{Cfg: msg.Cfg})
	case BackMsg:
		switch m.step {
		case stepCustom:
			m.step = stepModels
		case stepModels:
			m.step = stepWorkspace
			_ = m.input.Focus()
		}
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.PasteMsg:
		if m.step == stepWorkspace {
			m.wsErr = ""
			m.input, _ = m.input.Update(msg)
		}
	}
	return m, nil
}

// resized replays the last window size on the children, so a freshly built
// one renders at the right size.
func (m OnboardingModel) resized() OnboardingModel {
	if m.width == 0 && m.height == 0 {
		return m
	}
	msg := tea.WindowSizeMsg{Width: m.width, Height: m.height}
	pm, _ := m.picker.Update(msg)
	m.picker = pm.(PresetPickerModel)
	if m.step == stepCustom {
		mm, _ := m.models.Update(msg)
		m.models = mm.(ModelsModel)
	}
	return m
}

// baseConfig is the config the user starts from: the defaults plus the
// workspace and the chosen preset.
func (m OnboardingModel) baseConfig(p config.Preset) config.Config {
	cfg := config.Defaults()
	cfg.Workspace = m.workspace
	cfg.Preset = p
	cfg.Models = config.PresetAssignments(p)
	return cfg
}

func (m OnboardingModel) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		return m, emit(QuitMsg{})
	}
	switch m.step {
	case stepTools:
		switch k {
		case "q", "esc":
			return m, emit(QuitMsg{})
		case "r":
			if !m.checking {
				m.checking = true
				return m, m.checkTools()
			}
		case "enter":
			if m.toolsOK() {
				m.step = stepWorkspace
				// The blink command is dropped on purpose: the cursor stays solid.
				_ = m.input.Focus()
				return m, nil
			}
		}
	case stepWorkspace:
		switch k {
		case "esc":
			m.step = stepTools
			m.input.Blur()
		case "enter":
			ws := strings.TrimSpace(m.input.Value())
			switch {
			case ws == "":
				m.wsErr = "Escribí el workspace de Bitbucket."
			case strings.Contains(ws, "/") || strings.IndexFunc(ws, unicode.IsSpace) >= 0:
				m.wsErr = "El workspace es un solo nombre, sin barras ni espacios."
			default:
				m.wsErr, m.workspace = "", ws
				m.picker = NewPresetPicker(m.baseConfig(config.PresetBalanceado))
				m.saveErr = nil
				m.step = stepModels
				m.input.Blur()
				m = m.resized()
			}
		default:
			m.wsErr = ""
			m.input, _ = m.input.Update(msg)
		}
	case stepModels:
		m.saveErr = nil
		pm, cmd := m.picker.Update(msg)
		m.picker = pm.(PresetPickerModel)
		return m, cmd
	case stepCustom:
		mm, cmd := m.models.Update(msg)
		m.models = mm.(ModelsModel)
		return m, cmd
	}
	return m, nil
}

// View renders the screen at the last known size.
func (m OnboardingModel) View() tea.View { return tea.NewView(m.render(m.width, m.height)) }

func (m OnboardingModel) header(n int) string {
	return theme.Strong.Render(" pr-review") + theme.Dim.Render(fmt.Sprintf("   configuración inicial, paso %d de 3", n))
}

func (m OnboardingModel) render(width, height int) string {
	switch m.step {
	case stepWorkspace:
		return m.renderWorkspace(width, height)
	case stepModels:
		out := m.picker.render(width, height)
		if m.saveErr != nil {
			lines := strings.Split(out, "\n")
			if n := len(lines); n >= 2 {
				lines[n-2] = fit(theme.Accent.Render("  No se pudo guardar: "+clean(m.saveErr.Error())), width)
			}
			out = strings.Join(lines, "\n")
		}
		return out
	case stepCustom:
		return m.models.render(width, height)
	}
	return m.renderTools(width, height)
}

func (m OnboardingModel) renderTools(width, height int) string {
	lines := []string{
		m.header(1),
		"",
		theme.Strong.Render("  Antes de empezar, compruebo que tengas lo necesario."),
		"",
	}
	keys := []components.Key{{Keys: "r", Desc: "volver a comprobar"}, {Keys: "q", Desc: "salir"}}
	if m.checking {
		lines = append(lines, theme.Dim.Render("  Comprobando prb y Claude Code…"))
		keys = []components.Key{{Keys: "q", Desc: "salir"}}
	} else {
		lines = append(lines, m.authLines()...)
		lines = append(lines, m.claudeLines()...)
		if m.toolsOK() {
			keys = append([]components.Key{{Keys: "enter", Desc: "seguir"}}, keys...)
		}
	}
	lines = append(lines, "", "  "+components.KeyHelp(keys))
	return renderLines(lines, width, height)
}

func (m OnboardingModel) authLines() []string {
	tick := theme.OK.Render("  ✓ ")
	warn := theme.Accent.Render("  ✗ ")
	if m.authOK() {
		return []string{tick + theme.Text.Render("prb: sesión iniciada como "+clean(m.checked.identity.User))}
	}
	var pe *prb.Error
	if m.checked.authErr == nil || (errors.As(m.checked.authErr, &pe) && pe.Code == "no_credentials") {
		return []string{warn + theme.Text.Render("prb: no hay sesión iniciada."), theme.Dim.Render("    Corré prb auth login y apretá r.")}
	}
	return []string{
		warn + theme.Text.Render("prb: no pude comprobar la sesión."),
		theme.Dim.Render("    " + clean(m.checked.authErr.Error())),
		theme.Dim.Render("    Revisá que prb esté instalado y apretá r."),
	}
}

func (m OnboardingModel) claudeLines() []string {
	warn := theme.Accent.Render("  ✗ ")
	if m.checked.claudeErr == nil {
		v := m.checked.version
		if v == "" {
			v = "instalado"
		}
		lines := []string{theme.OK.Render("  ✓ ") + theme.Text.Render("Claude Code: "+v)}
		switch {
		case m.checked.sessionErr != nil:
			lines = append(lines,
				warn+theme.Text.Render("Claude Code: no pude comprobar la sesión."),
				theme.Dim.Render("    "+clean(m.checked.sessionErr.Error())),
				theme.Dim.Render("    Revisá que Claude Code esté actualizado y apretá r."))
		case !m.checked.loggedIn:
			lines = append(lines,
				warn+theme.Text.Render("Claude Code no tiene sesión."),
				theme.Dim.Render("    Corré claude y iniciá sesión, después apretá r."))
		}
		return lines
	}
	var ee *execx.ExitError
	if errors.As(m.checked.claudeErr, &ee) {
		return []string{
			warn + theme.Text.Render(fmt.Sprintf("Claude Code: claude --version falló (código %d).", ee.Code)),
			theme.Dim.Render("    Revisá la instalación y apretá r."),
		}
	}
	return []string{warn + theme.Text.Render("No encontré claude. Instalá Claude Code."), theme.Dim.Render("    Después apretá r.")}
}

func (m OnboardingModel) renderWorkspace(width, height int) string {
	lines := []string{
		m.header(2),
		"",
		theme.Strong.Render("  Escribí tu workspace de Bitbucket."),
		theme.Dim.Render("  Es el nombre que aparece en la dirección: bitbucket.org/<workspace>/…"),
		"",
		theme.Dim.Render("  workspace  ") + m.input.View(),
	}
	if m.wsErr != "" {
		lines = append(lines, "", theme.Accent.Render("  "+m.wsErr))
	} else {
		lines = append(lines, "", "")
	}
	lines = append(lines, "", "  "+components.KeyHelp([]components.Key{
		{Keys: "enter", Desc: "seguir"}, {Keys: "esc", Desc: "volver al paso anterior"},
	}))
	return renderLines(lines, width, height)
}
