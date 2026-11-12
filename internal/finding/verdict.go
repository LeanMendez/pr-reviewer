package finding

// Verdict is the final review verdict, computed by fixed rules.
type Verdict string

const (
	VerdictAprobar               Verdict = "aprobar"
	VerdictAprobarConComentarios Verdict = "aprobar_con_comentarios"
	VerdictPedirCambios          Verdict = "pedir_cambios"
	VerdictRevisarAMano          Verdict = "revisar_a_mano"
)

// ComputeVerdict applies spec 6.9. Only Group A findings that are confirmado
// or sin_confirmar and not preexistente count. Medio/bajo with uncertain
// origin do not count. incompleteWarning is true when the verdict is
// pedir_cambios and the review was incomplete.
func ComputeVerdict(fs []Finding, incomplete bool) (v Verdict, incompleteWarning bool) {
	var graveConfirmed, graveDoubtful, minor bool
	for _, f := range fs {
		if f.Group != GroupA || f.Origin == OriginPreexistente {
			continue
		}
		if f.Status != StatusConfirmado && f.Status != StatusSinConfirmar {
			continue
		}
		switch {
		case f.Severity.Grave():
			if f.Status == StatusConfirmado && f.Origin.FromPR() {
				graveConfirmed = true
			} else {
				graveDoubtful = true
			}
		case f.Origin.FromPR():
			minor = true
		}
	}

	switch {
	case graveConfirmed:
		return VerdictPedirCambios, incomplete
	case graveDoubtful || incomplete:
		return VerdictRevisarAMano, false
	case minor:
		return VerdictAprobarConComentarios, false
	default:
		return VerdictAprobar, false
	}
}
