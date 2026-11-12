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

// ApplyJudges applies the two judges' votes to a finding following spec 6.9.
// Votes are always recorded; Status only changes for confirmado/sin_confirmar.
func ApplyJudges(f Finding, a, b JudgeVote) Finding {
	f.Votes = append(append([]JudgeVote(nil), f.Votes...), a, b)

	if f.Status != StatusConfirmado && f.Status != StatusSinConfirmar {
		return f
	}

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
		if !a.Worth && !b.Worth {
			f.Status = StatusMenor
		}
	}

	applyFixVerdict(&f, a, b)
	return f
}

// applyFixVerdict replaces the fix or flags it when a judge says it violates a
// repo rule and cites that rule.
func applyFixVerdict(f *Finding, votes ...JudgeVote) {
	var rule, alt string
	for _, v := range votes {
		if v.FixComplies || strings.TrimSpace(v.RuleCited) == "" {
			continue
		}
		if rule == "" {
			rule = v.RuleCited
		}
		if alt == "" {
			alt = strings.TrimSpace(v.AlternativeFix)
		}
	}
	if rule == "" {
		return
	}
	if alt != "" {
		f.Fix = alt
		return
	}
	f.FixStatus = FixStatusARevisar
	msg := "solución a revisar: viola la regla " + rule
	if f.Reason != "" {
		msg = f.Reason + "; " + msg
	}
	f.Reason = msg
}
