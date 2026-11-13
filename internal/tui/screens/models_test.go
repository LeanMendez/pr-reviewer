package screens

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func pressKeys(m tea.Model, keys ...string) (tea.Model, tea.Msg) {
	var out tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(tuitest.Key(k))
		out = nil
		if cmd != nil {
			out = cmd()
		}
	}
	return m, out
}

func newModels(save func(config.Config) error) ModelsModel {
	cfg := config.Defaults()
	cfg.Preset = config.PresetBalanceado
	cfg.Models = config.PresetAssignments(config.PresetBalanceado)
	if save == nil {
		save = func(config.Config) error { return nil }
	}
	return NewModels(cfg, save)
}

func goToRole(m tea.Model, r config.Role) tea.Model {
	for i, x := range config.AllRoles() {
		if x == r {
			for j := 0; j < i; j++ {
				m, _ = pressKeys(m, "down")
			}
		}
	}
	return m
}

func TestModelsRender(t *testing.T) {
	out := tuitest.Plain(newModels(nil).render(120, 24))
	last := -1
	for _, r := range config.AllRoles() {
		name, desc := roleLabel(r)
		i := strings.Index(out, name)
		if i < 0 || !strings.Contains(out, desc) || i < last {
			t.Errorf("role %q missing or out of order:\n%s", name, out)
		}
		last = i
	}
	for _, want := range []string{"partiste de Balanceado, con 0 cambios", "opus", "sonnet", "haiku", "alto", "—", "g guardar", "esc salir sin guardar"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
}

func TestModelsWidth(t *testing.T) {
	for _, w := range []int{40, 60, 100} {
		checkWidth(t, newModels(nil).render(w, 24), w)
	}
}

func TestModelsCycleModel(t *testing.T) {
	m, _ := pressKeys(goToRole(newModels(nil), config.RoleSeguridad), "right")
	out := tuitest.Plain(m.(ModelsModel).render(120, 24))
	if !strings.Contains(out, "con 1 cambio") || strings.Contains(out, "1 cambios") {
		t.Errorf("header:\n%s", out)
	}
	found := false
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "Seguridad") {
			found = strings.Contains(l, "‹ sonnet ›") && strings.Contains(l, "cambiado")
		}
	}
	if !found {
		t.Errorf("Seguridad row not changed to sonnet:\n%s", out)
	}
	if got := m.(ModelsModel).current[config.RoleSeguridad].Model; got != config.ModelSonnet {
		t.Errorf("model = %s", got)
	}
	if !strings.Contains(m.(ModelsModel).render(120, 24), "cambiado") {
		t.Error("no cambiado")
	}
}

func TestModelsEffortRespectsModel(t *testing.T) {
	m := goToRole(newModels(nil), config.RoleBugs) // opus/high
	m, _ = pressKeys(m, "right", "right")          // sonnet, haiku
	a := m.(ModelsModel).current[config.RoleBugs]
	if a.Model != config.ModelHaiku || a.Effort != config.EffortDefault || !a.Valid() {
		t.Errorf("after haiku: %+v", a)
	}
	m, _ = pressKeys(m, "e", "e")
	if got := m.(ModelsModel).current[config.RoleBugs]; got != a {
		t.Errorf("e on haiku changed %+v -> %+v", a, got)
	}
	out := tuitest.Plain(m.(ModelsModel).render(120, 24))
	if !strings.Contains(out, "—") {
		t.Errorf("no — shown:\n%s", out)
	}
	m, _ = pressKeys(m, "left")
	m, _ = pressKeys(m, "e")
	if got := m.(ModelsModel).current[config.RoleBugs]; got.Effort != config.EffortLow || !got.Valid() {
		t.Errorf("e on sonnet: %+v", got)
	}
}

func TestModelsPreset(t *testing.T) {
	var m tea.Model = newModels(nil)
	for _, p := range []config.Preset{config.PresetEconomico, config.PresetPotente, config.PresetBalanceado} {
		m, _ = pressKeys(m, "p")
		want := config.PresetAssignments(p)
		for _, r := range config.AllRoles() {
			if got := m.(ModelsModel).current[r]; got != want[r] {
				t.Errorf("%s/%s = %+v, want %+v", p, r, got, want[r])
			}
		}
	}
}

func TestModelsSave(t *testing.T) {
	var saved []config.Config
	save := func(c config.Config) error { saved = append(saved, c); return nil }

	m, msg := pressKeys(newModels(save), "g")
	got, ok := msg.(ModelsSavedMsg)
	if !ok || len(saved) != 1 || saved[0].Preset != config.PresetBalanceado || got.Cfg.Preset != config.PresetBalanceado {
		t.Fatalf("unchanged save: msg=%+v saved=%+v", msg, saved)
	}
	_ = m

	saved = nil
	m, _ = pressKeys(goToRole(newModels(save), config.RoleSeguridad), "right")
	_, msg = pressKeys(m, "g")
	got, ok = msg.(ModelsSavedMsg)
	if !ok || len(saved) != 1 || saved[0].Preset != config.PresetPersonalizado ||
		saved[0].Models[config.RoleSeguridad].Model != config.ModelSonnet || got.Cfg.Preset != config.PresetPersonalizado {
		t.Fatalf("changed save: msg=%+v saved=%+v", msg, saved)
	}
	if saved[0].Thresholds != config.Defaults().Thresholds {
		t.Error("other fields not kept")
	}

	saved = nil
	_, msg = pressKeys(m, "esc")
	if _, ok := msg.(BackMsg); !ok || len(saved) != 0 {
		t.Errorf("esc: msg=%+v saved=%d", msg, len(saved))
	}
}

func TestModelsSaveError(t *testing.T) {
	m, msg := pressKeys(newModels(func(config.Config) error { return errors.New("disco lleno") }), "g")
	if msg != nil {
		t.Errorf("emitted %+v on error", msg)
	}
	if out := tuitest.Plain(m.(ModelsModel).render(120, 24)); !strings.Contains(out, "No se pudo guardar: disco lleno") {
		t.Errorf("error not shown:\n%s", out)
	}
}

func TestEffortLabel(t *testing.T) {
	want := map[config.Effort]string{"": "—", "low": "bajo", "medium": "medio", "high": "alto", "xhigh": "muy alto", "max": "máximo"}
	for e, w := range want {
		if got := effortLabel(e); got != w {
			t.Errorf("%q = %q, want %q", e, got, w)
		}
	}
}

func TestPresetPicker(t *testing.T) {
	m := NewPresetPicker(config.Defaults())
	out := tuitest.Plain(m.render(120, 30))
	for _, want := range []string{
		"Balanceado", "Económico", "Potente", "Personalizado", "recomendado",
		"Así queda Balanceado:", "Agente superior", "Juez B", "Resumen inicial",
		"Uso por revisión: ligero 4 agentes, exhaustivo 9, con judgment 11.",
	} {
		if want == "muy alto" {
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	if strings.Count(out, "recomendado") != 1 {
		t.Errorf("want one recomendado:\n%s", out)
	}
	var mm tea.Model = m
	mm, _ = pressKeys(mm, "down")
	if out := tuitest.Plain(mm.(PresetPickerModel).render(120, 30)); !strings.Contains(out, "Así queda Económico:") {
		t.Errorf("preview did not follow cursor:\n%s", out)
	}
	_, msg := pressKeys(mm, "enter")
	if got, ok := msg.(PresetChosenMsg); !ok || got.Preset != config.PresetEconomico {
		t.Errorf("enter = %+v", msg)
	}
	_, msg = pressKeys(mm, "down", "down", "down", "enter")
	if _, ok := msg.(CustomChosenMsg); !ok {
		t.Errorf("enter on Personalizado = %+v", msg)
	}
	_, msg = pressKeys(mm, "esc")
	if _, ok := msg.(BackMsg); !ok {
		t.Errorf("esc = %+v", msg)
	}
}

func TestPresetPickerWidth(t *testing.T) {
	for _, w := range []int{40, 60, 100} {
		checkWidth(t, NewPresetPicker(config.Defaults()).render(w, 30), w)
	}
}

func TestModelsPresetRebases(t *testing.T) {
	var saved []config.Config
	save := func(c config.Config) error { saved = append(saved, c); return nil }

	m, _ := pressKeys(newModels(save), "p", "p") // economico, potente
	out := tuitest.Plain(m.(ModelsModel).render(120, 24))
	if !strings.Contains(out, "partiste de Potente, con 0 cambios") || strings.Contains(out, "cambiado") {
		t.Errorf("after p:\n%s", out)
	}
	_, msg := pressKeys(m, "g")
	got, ok := msg.(ModelsSavedMsg)
	if !ok || len(saved) != 1 || saved[0].Preset != config.PresetPotente || got.Cfg.Preset != config.PresetPotente {
		t.Fatalf("p+g: msg=%+v saved=%+v", msg, saved)
	}

	// one edit after p -> personalizado, count 1
	saved = nil
	m2 := goToRole(m, config.RoleSeguridad)
	m2, _ = pressKeys(m2, "right")
	if out := tuitest.Plain(m2.(ModelsModel).render(120, 24)); !strings.Contains(out, "partiste de Potente, con 1 cambio") {
		t.Errorf("after edit:\n%s", out)
	}
	_, msg = pressKeys(m2, "g")
	if got, ok := msg.(ModelsSavedMsg); !ok || got.Cfg.Preset != config.PresetPersonalizado || saved[0].Preset != config.PresetPersonalizado {
		t.Fatalf("p+edit+g: msg=%+v", msg)
	}

	// away and back clears the marker
	m3, _ := pressKeys(goToRole(newModels(save), config.RoleSeguridad), "right", "left")
	out = tuitest.Plain(m3.(ModelsModel).render(120, 24))
	if strings.Contains(out, "cambiado") || !strings.Contains(out, "con 0 cambios") {
		t.Errorf("away and back:\n%s", out)
	}

	// p then esc does not save
	saved = nil
	_, msg = pressKeys(m, "esc")
	if _, ok := msg.(BackMsg); !ok || len(saved) != 0 {
		t.Errorf("p+esc: msg=%+v saved=%d", msg, len(saved))
	}
}

func TestModelsPresetCycleValid(t *testing.T) {
	var saved []config.Config
	save := func(c config.Config) error { saved = append(saved, c); return nil }
	var m tea.Model = newModels(save)
	for i := 0; i < 4; i++ {
		m, _ = pressKeys(m, "p", "g")
	}
	if len(saved) != 4 {
		t.Fatalf("saved %d", len(saved))
	}
	for _, c := range saved {
		for r, a := range c.Models {
			if !a.Valid() {
				t.Errorf("%s invalid: %+v", r, a)
			}
		}
	}
}

func TestModelsPersonalizadoBase(t *testing.T) {
	cfg := config.Defaults()
	cfg.Preset = config.PresetPersonalizado
	m := NewModels(cfg, func(config.Config) error { return nil })
	if out := tuitest.Plain(m.render(120, 24)); !strings.Contains(out, "partiste de tu configuración, con 0 cambios") {
		t.Errorf("header:\n%s", out)
	}
	_, msg := pressKeys(m, "g")
	if got := msg.(ModelsSavedMsg); got.Cfg.Preset != config.PresetPersonalizado {
		t.Errorf("preset = %s", got.Cfg.Preset)
	}
}
