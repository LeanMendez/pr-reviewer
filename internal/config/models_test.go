package config

import (
	"slices"
	"testing"
)

func TestEffortsFor(t *testing.T) {
	if got := EffortsFor("haiku"); !slices.Equal(got, []Effort{""}) {
		t.Fatalf("haiku: got %q", got)
	}
	all := []Effort{"", "low", "medium", "high", "xhigh", "max"}
	for _, m := range []Model{"fable", "opus", "sonnet"} {
		if got := EffortsFor(m); !slices.Equal(got, all) {
			t.Fatalf("%s: got %q", m, got)
		}
	}
	if got := EffortsFor("nope"); len(got) != 0 {
		t.Fatalf("unknown model: got %q", got)
	}
}

func TestAssignmentValid(t *testing.T) {
	cases := []struct {
		a    Assignment
		want bool
	}{
		{Assignment{"opus", "high"}, true},
		{Assignment{"haiku", ""}, true},
		{Assignment{"haiku", "high"}, false},
		{Assignment{"nope", ""}, false},
	}
	for _, c := range cases {
		if got := c.a.Valid(); got != c.want {
			t.Errorf("%+v: got %v want %v", c.a, got, c.want)
		}
	}
}

func TestAllRolesOrder(t *testing.T) {
	want := []Role{"superior", "resumen", "bugs", "seguridad", "reglas", "tests", "validador_bugs", "validador_reglas", "juez_a", "juez_b"}
	if got := AllRoles(); !slices.Equal(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestPresetBalanceado(t *testing.T) {
	want := map[Role]Assignment{
		RoleSuperior:        {"opus", "high"},
		RoleResumen:         {"haiku", ""},
		RoleBugs:            {"opus", "high"},
		RoleSeguridad:       {"opus", "medium"},
		RoleReglas:          {"sonnet", "medium"},
		RoleTests:           {"sonnet", "medium"},
		RoleValidadorBugs:   {"opus", "high"},
		RoleValidadorReglas: {"sonnet", "medium"},
		RoleJuezA:           {"opus", "high"},
		RoleJuezB:           {"sonnet", "high"},
	}
	got := PresetAssignments(PresetBalanceado)
	if len(got) != len(want) {
		t.Fatalf("len %d", len(got))
	}
	for r, a := range want {
		if got[r] != a {
			t.Errorf("%s: got %+v want %+v", r, got[r], a)
		}
	}
}

func TestPresetEconomico(t *testing.T) {
	got := PresetAssignments(PresetEconomico)
	for _, r := range AllRoles() {
		a, ok := got[r]
		if !ok || !a.Valid() {
			t.Fatalf("%s: missing or invalid %+v", r, a)
		}
		if a.Model == ModelOpus {
			t.Errorf("%s uses opus", r)
		}
	}
	if got[RoleResumen].Model != ModelHaiku {
		t.Error("resumen should be haiku")
	}
	if got[RoleJuezA] == got[RoleJuezB] {
		t.Error("jueces must differ")
	}
}

func TestPresetPotente(t *testing.T) {
	got := PresetAssignments(PresetPotente)
	for _, r := range AllRoles() {
		a := got[r]
		if !a.Valid() {
			t.Fatalf("%s invalid %+v", r, a)
		}
		if r == RoleResumen {
			continue
		}
		if a.Model != ModelOpus {
			t.Errorf("%s: model %s", r, a.Model)
		}
		if a.Effort != EffortHigh && a.Effort != EffortXHigh && a.Effort != EffortMax {
			t.Errorf("%s: effort %q below high", r, a.Effort)
		}
	}
}

func TestPresetPersonalizadoAndUnknownAreBalanceado(t *testing.T) {
	want := PresetAssignments(PresetBalanceado)
	for _, p := range []Preset{PresetPersonalizado, "x"} {
		got := PresetAssignments(p)
		for r, a := range want {
			if got[r] != a {
				t.Errorf("%s/%s differs", p, r)
			}
		}
	}
}
