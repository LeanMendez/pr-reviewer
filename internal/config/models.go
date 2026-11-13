package config

import "slices"

// Model is a Claude model alias.
type Model string

const (
	ModelFable  Model = "fable"
	ModelOpus   Model = "opus"
	ModelSonnet Model = "sonnet"
	ModelHaiku  Model = "haiku"
)

// Effort is a reasoning effort level; the empty value is the model default.
type Effort string

const (
	EffortDefault Effort = ""
	EffortLow     Effort = "low"
	EffortMedium  Effort = "medium"
	EffortHigh    Effort = "high"
	EffortXHigh   Effort = "xhigh"
	EffortMax     Effort = "max"
)

// EffortsFor returns the efforts a model supports (rule from gentle-ai):
// haiku only supports the default; fable, opus and sonnet support all six.
// Unknown models support none.
func EffortsFor(m Model) []Effort {
	switch m {
	case ModelHaiku:
		return []Effort{EffortDefault}
	case ModelFable, ModelOpus, ModelSonnet:
		return []Effort{EffortDefault, EffortLow, EffortMedium, EffortHigh, EffortXHigh, EffortMax}
	}
	return nil
}

// Role identifies one agent of the review pipeline.
type Role string

const (
	RoleSuperior        Role = "superior"
	RoleResumen         Role = "resumen"
	RoleBugs            Role = "bugs"
	RoleSeguridad       Role = "seguridad"
	RoleReglas          Role = "reglas"
	RoleTests           Role = "tests"
	RoleValidadorBugs   Role = "validador_bugs"
	RoleValidadorReglas Role = "validador_reglas"
	RoleJuezA           Role = "juez_a"
	RoleJuezB           Role = "juez_b"
)

// AllRoles returns every role in canonical order.
func AllRoles() []Role {
	return []Role{
		RoleSuperior, RoleResumen, RoleBugs, RoleSeguridad, RoleReglas,
		RoleTests, RoleValidadorBugs, RoleValidadorReglas, RoleJuezA, RoleJuezB,
	}
}

// Assignment is the model and effort used by a role.
type Assignment struct {
	Model  Model  `yaml:"modelo"`
	Effort Effort `yaml:"esfuerzo,omitempty"`
}

// Valid reports whether the model is known and supports the effort.
func (a Assignment) Valid() bool {
	return slices.Contains(EffortsFor(a.Model), a.Effort)
}
