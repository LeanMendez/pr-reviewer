package pipeline

import (
	"slices"
	"testing"

	"github.com/LeanMendez/pr-review/internal/prechecks"
)

func TestParseMode(t *testing.T) {
	for in, want := range map[string]Mode{
		"ligero": ModeLigero, " Exhaustivo ": ModeExhaustivo, "judgment": ModeJudgment,
	} {
		got, err := ParseMode(in)
		if err != nil || got != want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "rapido", "judgement"} {
		if got, err := ParseMode(in); err == nil {
			t.Errorf("ParseMode(%q) = %q, want an error", in, got)
		}
	}
}

func TestSuggest(t *testing.T) {
	tests := []struct {
		name string
		pre  prechecks.Result
		want Mode
	}{
		{"chico", prechecks.Result{TotalLines: 20}, ModeLigero},
		{"sensible sin riesgo", prechecks.Result{Sensitive: []string{"package.json", "Dockerfile"}}, ModeLigero},
		{"grande", prechecks.Result{Oversize: true}, ModeExhaustivo},
		{"migracion", prechecks.Result{Sensitive: []string{"db/migrations/001_pagos.go"}}, ModeExhaustivo},
		{"migraciones en castellano", prechecks.Result{Sensitive: []string{"migraciones/002.go"}}, ModeExhaustivo},
		{"auth", prechecks.Result{Sensitive: []string{"config/auth.yaml"}}, ModeExhaustivo},
		{"sql", prechecks.Result{Sensitive: []string{"db/schema.sql"}}, ModeExhaustivo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Suggest(tt.pre)
			if got != tt.want {
				t.Errorf("Suggest = %q, want %q", got, tt.want)
			}
			if got == ModeJudgment {
				t.Error("Suggest never proposes judgment")
			}
		})
	}
}

func TestStages(t *testing.T) {
	sinJueces := []Stage{StagePrechecks, StageRules, StagePlan, StageSpecialists, StageValidation, StageReport}
	todas := []Stage{StagePrechecks, StageRules, StagePlan, StageSpecialists, StageValidation, StageJudges, StageReport}
	for m, want := range map[Mode][]Stage{ModeLigero: sinJueces, ModeExhaustivo: sinJueces, ModeJudgment: todas} {
		if got := Stages(m); !slices.Equal(got, want) {
			t.Errorf("Stages(%s) = %v, want %v", m, got, want)
		}
	}
	for i, s := range todas {
		if int(s) != i+1 {
			t.Errorf("stage %d has number %d", i+1, s)
		}
	}

	names := map[Stage]string{
		StagePrechecks: "Chequeos sin IA", StageRules: "Reglas del repo", StagePlan: "Plan",
		StageSpecialists: "Especialistas", StageValidation: "Validación", StageJudges: "Jueces", StageReport: "Reporte",
	}
	for s, want := range names {
		if got := s.Name(); got != want {
			t.Errorf("Stage(%d).Name() = %q, want %q", s, got, want)
		}
	}
	if got := Stage(8).Name(); got != "" {
		t.Errorf("Stage(8).Name() = %q, want empty", got)
	}
}

func TestEstimateAgents(t *testing.T) {
	for m, want := range map[Mode]int{ModeLigero: 4, ModeExhaustivo: 9, ModeJudgment: 11} {
		if got := EstimateAgents(m); got != want {
			t.Errorf("EstimateAgents(%s) = %d, want %d", m, got, want)
		}
	}
}
