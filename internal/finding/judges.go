package finding

import "strings"

// hasEvidence reports whether the vote carries at least one non-empty evidence reference.
func (v JudgeVote) hasEvidence() bool {
	for _, e := range v.Evidence {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}

// refutes reports whether the vote says the finding is not real, with evidence.
func (v JudgeVote) refutes() bool { return !v.Real && v.hasEvidence() }

// neverMenor reports whether the finding's role is exempt from being demoted
// to "detalles menores" (spec 6.9: security and logic are never silenced as noise).
func neverMenor(role string) bool { return role == "seguridad" || role == "bugs" }

// forFinding returns the vote, or a zero vote when it names another finding.
func forFinding(v JudgeVote, id string) JudgeVote {
	if v.FindingID != "" && v.FindingID != id {
		return JudgeVote{}
	}
	return v
}

// notWorth reports whether the vote says "not worth reporting", with evidence.
func (v JudgeVote) notWorth() bool { return !v.Worth && v.hasEvidence() }

// ApplyJudges applies the two judges' votes to a finding following spec 6.9.
// Votes are always recorded; Status only changes for confirmado/sin_confirmar.
// Votes naming a different finding are recorded but ignored for decisions.
func ApplyJudges(f Finding, a, b JudgeVote) Finding {
	f.Votes = append(append([]JudgeVote(nil), f.Votes...), a, b)

	if f.Status != StatusConfirmado && f.Status != StatusSinConfirmar {
		return f
	}
	a, b = forFinding(a, f.ID), forFinding(b, f.ID)

	if f.Severity.Grave() {
		if a.refutes() && b.refutes() {
			f.Status = StatusDescartadoPorJueces
			return f
		}
	} else {
		if a.refutes() || b.refutes() {
			f.Status = StatusDescartadoPorJueces
			return f
		}
		if a.notWorth() && b.notWorth() && !neverMenor(f.Role) {
			f.Status = StatusMenor
		}
	}

	applyFixVerdict(&f, a, b)
	return f
}

// applyFixVerdict replaces the fix or flags it when a judge says it violates a
// repo rule and cites that rule. The alternative is only taken from the vote
// that cited the rule; an alternative is preferred over flagging.
func applyFixVerdict(f *Finding, votes ...JudgeVote) {
	var first *JudgeVote
	for i, v := range votes {
		if v.FixComplies || strings.TrimSpace(v.RuleCited) == "" {
			continue
		}
		if alt := strings.TrimSpace(v.AlternativeFix); alt != "" {
			f.Fix = alt
			return
		}
		if first == nil {
			first = &votes[i]
		}
	}
	if first == nil {
		return
	}
	f.FixStatus = FixStatusARevisar
	msg := "solución a revisar: viola la regla " + first.RuleCited
	if f.Reason != "" {
		msg = f.Reason + "; " + msg
	}
	f.Reason = msg
}
