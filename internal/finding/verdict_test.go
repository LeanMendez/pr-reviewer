package finding

import "testing"

func TestComputeVerdict(t *testing.T) {
	f := func(g Group, s Severity, o Origin, st Status) Finding {
		return Finding{Group: g, Severity: s, Origin: o, Status: st}
	}
	tests := []struct {
		name       string
		fs         []Finding
		incomplete bool
		want       Verdict
		warn       bool
	}{
		{"critico confirmado del PR", []Finding{f(GroupA, SeverityCritico, OriginIntroducido, StatusConfirmado)}, false, VerdictPedirCambios, false},
		{"critico confirmado incompleto", []Finding{f(GroupA, SeverityCritico, OriginIntroducido, StatusConfirmado)}, true, VerdictPedirCambios, true},
		{"alto sin confirmar", []Finding{f(GroupA, SeverityAlto, OriginIntroducido, StatusSinConfirmar)}, false, VerdictRevisarAMano, false},
		{"alto confirmado origen incierto", []Finding{f(GroupA, SeverityAlto, OriginIncierto, StatusConfirmado)}, false, VerdictRevisarAMano, false},
		{"solo medio confirmado", []Finding{f(GroupA, SeverityMedio, OriginActivado, StatusConfirmado)}, false, VerdictAprobarConComentarios, false},
		{"nada incompleto", nil, true, VerdictRevisarAMano, false},
		{"medio incompleto", []Finding{f(GroupA, SeverityMedio, OriginActivado, StatusConfirmado)}, true, VerdictRevisarAMano, false},
		{"nada", nil, false, VerdictAprobar, false},
		{"critico preexistente", []Finding{f(GroupA, SeverityCritico, OriginPreexistente, StatusConfirmado)}, false, VerdictAprobar, false},
		{"bajo grupo B", []Finding{f(GroupB, SeverityBajo, OriginIntroducido, StatusConfirmado)}, false, VerdictAprobar, false},
		{"medio origen incierto", []Finding{f(GroupA, SeverityMedio, OriginIncierto, StatusConfirmado)}, false, VerdictAprobar, false},
		{"estados que no cuentan", []Finding{
			f(GroupA, SeverityCritico, OriginIntroducido, StatusMenor),
			f(GroupA, SeverityCritico, OriginIntroducido, StatusRefutado),
			f(GroupA, SeverityCritico, OriginIntroducido, StatusDescartadoPorJueces),
			f(GroupA, SeverityCritico, OriginIntroducido, StatusCandidato),
			f(GroupA, SeverityCritico, OriginIntroducido, StatusFiltrado),
		}, false, VerdictAprobar, false},
		{"confirmado gana a dudoso", []Finding{
			f(GroupA, SeverityAlto, OriginIncierto, StatusSinConfirmar),
			f(GroupA, SeverityCritico, OriginEmpeorado, StatusConfirmado),
		}, false, VerdictPedirCambios, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, warn := ComputeVerdict(tt.fs, tt.incomplete)
			if v != tt.want || warn != tt.warn {
				t.Errorf("got (%q, %v), want (%q, %v)", v, warn, tt.want, tt.warn)
			}
		})
	}
}
