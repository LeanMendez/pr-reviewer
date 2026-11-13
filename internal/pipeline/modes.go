package pipeline

import (
	"fmt"
	"strings"

	"github.com/LeanMendez/pr-review/internal/prechecks"
)

// Mode selects which stages run (spec 6.5).
type Mode string

const (
	ModeLigero     Mode = "ligero"
	ModeExhaustivo Mode = "exhaustivo"
	ModeJudgment   Mode = "judgment"
)

// ParseMode reads a mode name, ignoring case and surrounding spaces.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(strings.ToLower(strings.TrimSpace(s))); m {
	case ModeLigero, ModeExhaustivo, ModeJudgment:
		return m, nil
	}
	return "", fmt.Errorf("modo desconocido %q: usá ligero, exhaustivo o judgment", s)
}

// Suggest proposes exhaustivo for PRs over the size threshold, with
// sensitive changes to migrations, auth or SQL, or touching authentication,
// sessions, tokens, permissions or security code, and ligero otherwise. It
// never proposes judgment: that one is always the user's call.
func Suggest(pre prechecks.Result) Mode {
	if pre.Oversize {
		return ModeExhaustivo
	}
	for _, p := range pre.Sensitive {
		p = strings.ToLower(p)
		if strings.Contains(p, "migra") || strings.Contains(p, "auth") || strings.HasSuffix(p, ".sql") {
			return ModeExhaustivo
		}
	}
	for _, p := range pre.Changed {
		p = strings.ToLower(p)
		for _, w := range accessWords {
			if strings.Contains(p, w) {
				return ModeExhaustivo
			}
		}
	}
	return ModeLigero
}

// accessWords mark a path as access-control code: a folder or file name
// containing one of them suggests exhaustivo.
var accessWords = []string{"auth", "login", "session", "token", "permission", "security"}

// EstimateAgents is the rough agent count shown before a review: ligero runs
// the summary, bugs, reglas and the synthesis; exhaustivo, one agent per
// role except the judges, with the superior twice (plan and synthesis);
// judgment adds the two judges. Extra groups and validators depend on the PR.
func EstimateAgents(m Mode) int {
	switch m {
	case ModeLigero:
		return 4
	case ModeExhaustivo:
		return 9
	case ModeJudgment:
		return 11
	}
	return 0
}

// Stage is one of the seven review stages, numbered as in spec 6.5.
type Stage int

const (
	StagePrechecks Stage = iota + 1
	StageRules
	StagePlan
	StageSpecialists
	StageValidation
	StageJudges
	StageReport
)

var stageNames = [...]string{
	StagePrechecks:   "Chequeos sin IA",
	StageRules:       "Reglas del repo",
	StagePlan:        "Plan",
	StageSpecialists: "Especialistas",
	StageValidation:  "Validación",
	StageJudges:      "Jueces",
	StageReport:      "Reporte",
}

// Name returns the stage name shown to the user, or empty for an unknown stage.
func (s Stage) Name() string {
	if s < StagePrechecks || s > StageReport {
		return ""
	}
	return stageNames[s]
}

// Stages returns the stages a mode runs, in order: every mode but judgment
// skips the judges.
func Stages(m Mode) []Stage {
	switch m {
	case ModeJudgment:
		return []Stage{StagePrechecks, StageRules, StagePlan, StageSpecialists, StageValidation, StageJudges, StageReport}
	case ModeLigero, ModeExhaustivo:
		return []Stage{StagePrechecks, StageRules, StagePlan, StageSpecialists, StageValidation, StageReport}
	}
	return nil
}
