package finding

import (
	"strings"
	"testing"
)

func vote(real bool, ev ...string) JudgeVote {
	return JudgeVote{Real: real, Worth: true, FixComplies: true, Evidence: ev}
}

func base(sev Severity) Finding {
	return Finding{ID: "f1", Severity: sev, Group: GroupA, Status: StatusConfirmado, Fix: "orig"}
}

func TestApplyJudgesGraveNeedsBothRefutes(t *testing.T) {
	tests := []struct {
		name string
		a, b JudgeVote
		want Status
	}{
		{"one refutes", vote(false, "a.go:1"), vote(true), StatusConfirmado},
		{"both refute with evidence", vote(false, "a.go:1"), vote(false, "b.go:2"), StatusDescartadoPorJueces},
		{"both refute without evidence", vote(false), vote(false, "", " "), StatusConfirmado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApplyJudges(base(SeverityCritico), tt.a, tt.b).Status; got != tt.want {
				t.Errorf("Status = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyJudgesGraveIgnoresWorth(t *testing.T) {
	a, b := vote(true), vote(true)
	a.Worth, b.Worth = false, false
	if got := ApplyJudges(base(SeverityAlto), a, b).Status; got != StatusConfirmado {
		t.Errorf("Status = %q, want confirmado", got)
	}
}

func TestApplyJudgesMinorEitherRefutes(t *testing.T) {
	if got := ApplyJudges(base(SeverityMedio), vote(false, "a.go:1"), vote(true)).Status; got != StatusDescartadoPorJueces {
		t.Errorf("Status = %q, want descartado_por_jueces", got)
	}
}

func TestApplyJudgesMinorBothNotWorth(t *testing.T) {
	a, b := vote(true), vote(true)
	a.Worth, b.Worth = false, false
	if got := ApplyJudges(base(SeverityBajo), a, b).Status; got != StatusMenor {
		t.Errorf("Status = %q, want menor", got)
	}
}

func TestApplyJudgesFixViolatesRule(t *testing.T) {
	violating := vote(true)
	violating.FixComplies = false
	violating.RuleCited = "no usar panic"

	withAlt := violating
	withAlt.AlternativeFix = "x"
	if got := ApplyJudges(base(SeverityAlto), withAlt, vote(true)); got.Fix != "x" {
		t.Errorf("Fix = %q, want x", got.Fix)
	}

	got := ApplyJudges(base(SeverityAlto), violating, vote(true))
	if got.FixStatus != "a_revisar" || !strings.Contains(got.Reason, "no usar panic") || got.Fix != "orig" {
		t.Errorf("got FixStatus=%q Reason=%q Fix=%q", got.FixStatus, got.Reason, got.Fix)
	}

	noRule := vote(true)
	noRule.FixComplies = false
	noRule.AlternativeFix = "x"
	got = ApplyJudges(base(SeverityAlto), noRule, vote(true))
	if got.Fix != "orig" || got.FixStatus != "" || got.Reason != "" {
		t.Errorf("fix changed without cited rule: %+v", got)
	}
}

func TestApplyJudgesKeepsVotes(t *testing.T) {
	a, b := vote(true), vote(true)
	a.Judge, b.Judge = "A", "B"
	got := ApplyJudges(base(SeverityMedio), a, b)
	if len(got.Votes) != 2 || got.Votes[0].Judge != "A" || got.Votes[1].Judge != "B" {
		t.Errorf("Votes = %+v", got.Votes)
	}
}

func TestApplyJudgesOtherStatusesUnchanged(t *testing.T) {
	f := base(SeverityMedio)
	f.Status = StatusRefutado
	got := ApplyJudges(f, vote(false, "a.go:1"), vote(false, "a.go:2"))
	if got.Status != StatusRefutado || len(got.Votes) != 2 {
		t.Errorf("got Status=%q votes=%d", got.Status, len(got.Votes))
	}
}
