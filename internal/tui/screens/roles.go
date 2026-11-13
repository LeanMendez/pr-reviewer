package screens

import (
	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/prompts"
)

// roleLabel returns the display name and one-line description of a role, as
// the model-configuration screen shows them. Unknown roles keep their id.
func roleLabel(r config.Role) (name, desc string) {
	switch r {
	case config.RoleSuperior:
		return "Agente superior", "arma el plan y la síntesis"
	case config.RoleResumen:
		return "Resumen inicial", "resume el PR y descarta lo trivial"
	case config.RoleBugs:
		return "Bugs y lógica", "busca errores de comportamiento"
	case config.RoleSeguridad:
		return "Seguridad", "datos, permisos, SQL, secretos"
	case config.RoleReglas:
		return "Reglas del repo", "CLAUDE.md y sus archivos"
	case config.RoleTests:
		return "Tests y contratos", "tests faltantes, quién llama a qué"
	case config.RoleValidadorBugs:
		return "Validador de bugs", "intenta descartar bugs y seguridad"
	case config.RoleValidadorReglas:
		return "Validador de reglas", "confirma que la regla aplica"
	case config.RoleJuezA:
		return "Juez A", "real, necesario, solución válida"
	case config.RoleJuezB:
		return "Juez B", "lo mismo, sin ver al juez A"
	}
	return string(r), ""
}

// promptRoleLabel is the display name of the prompt role an agent ran with
// (the role of a pipeline.Failure). Unknown roles keep their id.
func promptRoleLabel(role string) string {
	switch role {
	case prompts.RoleSuperiorPlan:
		return "Plan del agente superior"
	case prompts.RoleSuperiorSintesis:
		return "Síntesis del agente superior"
	case prompts.RoleJuez:
		return "Juez"
	case prompts.RoleResumen, prompts.RoleBugs, prompts.RoleSeguridad, prompts.RoleReglas, prompts.RoleTests,
		prompts.RoleValidadorBugs, prompts.RoleValidadorReglas:
		name, _ := roleLabel(config.Role(role))
		return name
	}
	return clean(role)
}
