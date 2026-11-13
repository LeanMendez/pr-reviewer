package screens

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

const authJSON = `{"authenticated":true,"user":"Lean","account_id":"1","default_workspace":"acme"}`

// claudeLoggedIn and claudeLoggedOut are what `claude auth status --json`
// prints, with exit codes 0 and 1.
const (
	claudeLoggedIn  = `{"loggedIn":true,"authMethod":"claude.ai","apiProvider":"firstParty"}`
	claudeLoggedOut = `{"loggedIn":false,"authMethod":"none","apiProvider":"firstParty"}`
)

type obFake struct {
	prbOut, prbErr string
	prbCode        int
	claudeOut      string
	claudeMissing  bool
	claudeCode     int
	// claude auth status --json; "" means logged in.
	claudeAuthOut  string
	claudeAuthCode int
}

func (f obFake) exec() execx.Exec {
	inner := execx.Fake(func(c execx.Cmd) (string, string, int) {
		if c.Name == "claude" && slices.Equal(c.Args, []string{"auth", "status", "--json"}) {
			if f.claudeAuthOut == "" {
				return claudeLoggedIn, "", 0
			}
			return f.claudeAuthOut, "", f.claudeAuthCode
		}
		if c.Name == "claude" {
			return f.claudeOut, "", f.claudeCode
		}
		return f.prbOut, f.prbErr, f.prbCode
	})
	return func(ctx context.Context, c execx.Cmd) error {
		if c.Name == "claude" && f.claudeMissing {
			return errors.New(`exec: "claude": executable file not found in $PATH`)
		}
		return inner(ctx, c)
	}
}

func newOnboarding(f obFake, save func(config.Config) error) OnboardingModel {
	if save == nil {
		save = func(config.Config) error { return nil }
	}
	e := f.exec()
	return NewOnboarding(OnboardingDeps{PRB: prb.Client{Exec: e}, Exec: e, Save: save})
}

// drive feeds msg to m and keeps feeding the messages its commands return,
// until a command yields nothing or a terminal message.
func drive(m tea.Model, msg tea.Msg) (tea.Model, tea.Msg) {
	for msg != nil {
		switch msg.(type) {
		case OnboardingDoneMsg, QuitMsg:
			return m, msg
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		msg = nil
		if cmd != nil {
			msg = cmd()
		}
	}
	return m, nil
}

func ready(t *testing.T) tea.Model {
	t.Helper()
	om := newOnboarding(obFake{prbOut: authJSON, claudeOut: "2.1.0 (Claude Code)\n"}, nil)
	m, _ := drive(om, om.Init()())
	return m
}

func view(m tea.Model) string { return tuitest.Plain(m.(OnboardingModel).render(100, 30)) }

func TestOnboardingToolsOK(t *testing.T) {
	m := ready(t)
	out := view(m)
	for _, want := range []string{"paso 1 de 3", "prb: sesión iniciada como Lean", "Claude Code: 2.1.0 (Claude Code)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	m, _ = drive(m, tuitest.Key("enter"))
	if out := view(m); !strings.Contains(out, "paso 2 de 3") {
		t.Errorf("enter did not advance:\n%s", out)
	}
}

func TestOnboardingToolsMissing(t *testing.T) {
	f := obFake{prbErr: "no credentials\n", prbCode: 4, claudeMissing: true}
	m := newOnboarding(f, nil)
	var tm tea.Model
	tm, _ = drive(m, m.Init()())
	out := view(tm)
	for _, want := range []string{"Corré prb auth login y apretá r.", "No encontré claude. Instalá Claude Code."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	tm, _ = drive(tm, tuitest.Key("enter"))
	if out := view(tm); !strings.Contains(out, "paso 1 de 3") {
		t.Errorf("advanced with missing tools:\n%s", out)
	}
}

func TestOnboardingOneToolMissingBlocks(t *testing.T) {
	m := newOnboarding(obFake{prbOut: authJSON, claudeMissing: true}, nil)
	tm, _ := drive(m, m.Init()())
	tm, _ = drive(tm, tuitest.Key("enter"))
	if out := view(tm); !strings.Contains(out, "paso 1 de 3") || !strings.Contains(out, "prb: sesión iniciada como Lean") {
		t.Errorf("unexpected:\n%s", out)
	}
}

func TestOnboardingClaudeWithoutSession(t *testing.T) {
	f := obFake{prbOut: authJSON, claudeOut: "2.1.0 (Claude Code)\n", claudeAuthOut: claudeLoggedOut, claudeAuthCode: 1}
	m := newOnboarding(f, nil)
	tm, _ := drive(m, m.Init()())
	out := view(tm)
	for _, want := range []string{
		"prb: sesión iniciada como Lean", "Claude Code: 2.1.0 (Claude Code)",
		"Claude Code no tiene sesión.", "Corré claude y iniciá sesión, después apretá r.",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "enter seguir") {
		t.Errorf("offers to go on without a Claude session:\n%s", out)
	}
	tm, _ = drive(tm, tuitest.Key("enter"))
	if out := view(tm); !strings.Contains(out, "paso 1 de 3") {
		t.Errorf("advanced without a Claude session:\n%s", out)
	}

	// after logging in, r finds the session
	om := tm.(OnboardingModel)
	f.claudeAuthOut, f.claudeAuthCode = "", 0
	om.deps.Exec = f.exec()
	tm, _ = drive(om, tuitest.Key("r"))
	if out := view(tm); strings.Contains(out, "no tiene sesión") || !strings.Contains(out, "enter seguir") {
		t.Errorf("after r:\n%s", out)
	}
}

func TestOnboardingClaudeSessionUnknown(t *testing.T) {
	f := obFake{prbOut: authJSON, claudeOut: "2.1.0 (Claude Code)\n", claudeAuthOut: "error: unknown command 'auth'", claudeAuthCode: 1}
	m := newOnboarding(f, nil)
	tm, _ := drive(m, m.Init()())
	out := view(tm)
	for _, want := range []string{"Claude Code: 2.1.0 (Claude Code)", "Claude Code: no pude comprobar la sesión."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	tm, _ = drive(tm, tuitest.Key("enter"))
	if out := view(tm); !strings.Contains(out, "paso 1 de 3") {
		t.Errorf("advanced without knowing the Claude session:\n%s", out)
	}
}

func TestOnboardingRetry(t *testing.T) {
	calls := 0
	e := execx.Fake(func(c execx.Cmd) (string, string, int) {
		if c.Name == "claude" && len(c.Args) > 0 && c.Args[0] == "auth" {
			return claudeLoggedIn, "", 0
		}
		if c.Name == "claude" {
			return "2.1.0", "", 0
		}
		calls++
		if calls == 1 {
			return "", "no credentials", 4
		}
		return authJSON, "", 0
	})
	m := NewOnboarding(OnboardingDeps{PRB: prb.Client{Exec: e}, Exec: e, Save: func(config.Config) error { return nil }})
	tm, _ := drive(m, m.Init()())
	if out := view(tm); !strings.Contains(out, "Corré prb auth login") {
		t.Fatalf("first check:\n%s", out)
	}
	tm, _ = drive(tm, tuitest.Key("r"))
	if out := view(tm); !strings.Contains(out, "sesión iniciada como Lean") {
		t.Errorf("after r:\n%s", out)
	}
}

func TestOnboardingCleansExternalText(t *testing.T) {
	m := newOnboarding(obFake{prbOut: `{"authenticated":true,"user":"Le\u001b[31man","default_workspace":"acme"}`, claudeOut: "2.1.0\x1b[2J\n"}, nil)
	tm, _ := drive(m, m.Init()())
	out := view(tm)
	if strings.ContainsRune(out, 0x1b) || !strings.Contains(out, "Le[31man") {
		t.Errorf("not cleaned: %q", out)
	}
}

func TestOnboardingWorkspace(t *testing.T) {
	m := ready(t)
	m, _ = drive(m, tuitest.Key("enter"))
	if out := view(m); !strings.Contains(out, "paso 2 de 3") || !strings.Contains(out, "acme") {
		t.Fatalf("default workspace not preloaded:\n%s", out)
	}
	for i := 0; i < 4; i++ {
		m, _ = drive(m, tuitest.Key("backspace"))
	}
	m, _ = drive(m, tuitest.Key("enter"))
	out := view(m)
	if !strings.Contains(out, "Escribí el workspace de Bitbucket.") || !strings.Contains(out, "paso 2 de 3") {
		t.Errorf("empty workspace:\n%s", out)
	}
}

func TestOnboardingWorkspaceRejectsSlugsWithSlashOrSpace(t *testing.T) {
	for _, bad := range []string{"a/b", "a b"} {
		m := ready(t)
		m, _ = drive(m, tuitest.Key("enter"))
		om := m.(OnboardingModel)
		om.input.SetValue(bad)
		m, _ = drive(om, tuitest.Key("enter"))
		out := view(m)
		if !strings.Contains(out, "El workspace es un solo nombre, sin barras ni espacios.") || !strings.Contains(out, "paso 2 de 3") {
			t.Errorf("%q:\n%s", bad, out)
		}
	}
}

func TestOnboardingWorkspaceIsTrimmed(t *testing.T) {
	var saved config.Config
	m := ready(t)
	m, _ = drive(m, tuitest.Key("enter"))
	om := m.(OnboardingModel)
	om.input.SetValue("  zeta  ")
	om.save = func(c config.Config) error { saved = c; return nil }
	m, _ = drive(om, tuitest.Key("enter"))
	_, msg := drive(m, tuitest.Key("enter"))
	if _, ok := msg.(OnboardingDoneMsg); !ok || saved.Workspace != "zeta" {
		t.Errorf("msg=%v saved=%+v", msg, saved)
	}
}

func toModels(t *testing.T, save func(config.Config) error) tea.Model {
	t.Helper()
	om := newOnboarding(obFake{prbOut: authJSON, claudeOut: "2.1.0"}, save)
	m, _ := drive(om, om.Init()())
	m, _ = drive(m, tuitest.Key("enter"))
	m, _ = drive(m, tuitest.Key("enter"))
	return m
}

func TestOnboardingModels(t *testing.T) {
	var saved []config.Config
	save := func(c config.Config) error { saved = append(saved, c); return nil }
	m := toModels(t, save)
	if out := view(m); !strings.Contains(out, "paso 3 de 3") || !strings.Contains(out, "Balanceado") {
		t.Fatalf("step 3:\n%s", out)
	}
	_, msg := drive(m, tuitest.Key("enter"))
	done, ok := msg.(OnboardingDoneMsg)
	if !ok || len(saved) != 1 {
		t.Fatalf("msg=%v saved=%d", msg, len(saved))
	}
	if saved[0].Workspace != "acme" || saved[0].Preset != config.PresetBalanceado || saved[0].Models[config.RoleBugs] != config.PresetAssignments(config.PresetBalanceado)[config.RoleBugs] {
		t.Errorf("saved = %+v", saved[0])
	}
	if done.Cfg.Workspace != "acme" {
		t.Errorf("done = %+v", done)
	}
}

func TestOnboardingSaveError(t *testing.T) {
	m := toModels(t, func(config.Config) error { return errors.New("disco lleno") })
	m, msg := drive(m, tuitest.Key("enter"))
	if _, ok := msg.(OnboardingDoneMsg); ok {
		t.Fatal("finished despite save error")
	}
	if out := view(m); !strings.Contains(out, "No se pudo guardar: disco lleno") {
		t.Errorf("no error shown:\n%s", out)
	}
}

func TestOnboardingCustom(t *testing.T) {
	var saved []config.Config
	m := toModels(t, func(c config.Config) error { saved = append(saved, c); return nil })
	m, _ = drive(m, tuitest.Key("down"))
	m, _ = drive(m, tuitest.Key("down"))
	m, _ = drive(m, tuitest.Key("down"))
	m, _ = drive(m, tuitest.Key("enter"))
	if out := view(m); !strings.Contains(out, "agente") || !strings.Contains(out, "esfuerzo") {
		t.Fatalf("models table not shown:\n%s", out)
	}
	m, _ = drive(m, tuitest.Key("right"))
	_, msg := drive(m, tuitest.Key("g"))
	if _, ok := msg.(OnboardingDoneMsg); !ok || len(saved) != 1 {
		t.Fatalf("msg=%v saved=%d", msg, len(saved))
	}
	if saved[0].Workspace != "acme" || saved[0].Preset != config.PresetPersonalizado {
		t.Errorf("saved = %+v", saved[0])
	}
}

func TestOnboardingEscGoesBack(t *testing.T) {
	m := toModels(t, nil)
	m, _ = drive(m, tuitest.Key("esc"))
	if out := view(m); !strings.Contains(out, "paso 2 de 3") {
		t.Fatalf("esc from step 3:\n%s", out)
	}
	m, _ = drive(m, tuitest.Key("esc"))
	if out := view(m); !strings.Contains(out, "paso 1 de 3") {
		t.Errorf("esc from step 2:\n%s", out)
	}
}

func TestOnboardingEscFromCustomGoesToPicker(t *testing.T) {
	m := toModels(t, nil)
	for _, k := range []string{"down", "down", "down", "enter", "esc"} {
		m, _ = drive(m, tuitest.Key(k))
	}
	if out := view(m); !strings.Contains(out, "paso 3 de 3") {
		t.Errorf("esc from table:\n%s", out)
	}
}

func TestOnboardingQuit(t *testing.T) {
	m := ready(t)
	_, msg := drive(m, tuitest.Key("q"))
	if _, ok := msg.(QuitMsg); !ok {
		t.Errorf("q = %v", msg)
	}
}
