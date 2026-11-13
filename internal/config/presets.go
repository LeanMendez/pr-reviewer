package config

// Preset names a ready-made set of role assignments.
type Preset string

const (
	PresetBalanceado    Preset = "balanceado"
	PresetEconomico     Preset = "economico"
	PresetPotente       Preset = "potente"
	PresetPersonalizado Preset = "personalizado"
)

// PresetAssignments returns a fresh assignment map for the preset.
// "personalizado" and unknown presets start from balanceado.
func PresetAssignments(p Preset) map[Role]Assignment {
	switch p {
	case PresetEconomico:
		return map[Role]Assignment{
			RoleSuperior:        {ModelSonnet, EffortMedium},
			RoleResumen:         {ModelHaiku, EffortDefault},
			RoleBugs:            {ModelSonnet, EffortMedium},
			RoleSeguridad:       {ModelSonnet, EffortMedium},
			RoleReglas:          {ModelSonnet, EffortLow},
			RoleTests:           {ModelSonnet, EffortLow},
			RoleValidadorBugs:   {ModelSonnet, EffortMedium},
			RoleValidadorReglas: {ModelSonnet, EffortLow},
			RoleJuezA:           {ModelSonnet, EffortHigh},
			RoleJuezB:           {ModelSonnet, EffortMedium},
		}
	case PresetPotente:
		return map[Role]Assignment{
			RoleSuperior:        {ModelOpus, EffortXHigh},
			RoleResumen:         {ModelHaiku, EffortDefault},
			RoleBugs:            {ModelOpus, EffortXHigh},
			RoleSeguridad:       {ModelOpus, EffortXHigh},
			RoleReglas:          {ModelOpus, EffortHigh},
			RoleTests:           {ModelOpus, EffortHigh},
			RoleValidadorBugs:   {ModelOpus, EffortXHigh},
			RoleValidadorReglas: {ModelOpus, EffortHigh},
			RoleJuezA:           {ModelOpus, EffortMax},
			RoleJuezB:           {ModelOpus, EffortHigh},
		}
	default:
		return map[Role]Assignment{
			RoleSuperior:        {ModelOpus, EffortHigh},
			RoleResumen:         {ModelHaiku, EffortDefault},
			RoleBugs:            {ModelOpus, EffortHigh},
			RoleSeguridad:       {ModelOpus, EffortMedium},
			RoleReglas:          {ModelSonnet, EffortMedium},
			RoleTests:           {ModelSonnet, EffortMedium},
			RoleValidadorBugs:   {ModelOpus, EffortHigh},
			RoleValidadorReglas: {ModelSonnet, EffortMedium},
			RoleJuezA:           {ModelOpus, EffortHigh},
			RoleJuezB:           {ModelSonnet, EffortHigh},
		}
	}
}
