package report

import (
	"strings"
	"testing"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
)

func TestVerdictTextIsPlain(t *testing.T) {
	r := incompleteResult()
	r.Verdict = finding.VerdictPedirCambios
	r.Phrase = "Usa `snake_case` y *negrita* en [x] <b>."
	got := VerdictText(r)
	if !strings.HasPrefix(got, "Pedir cambios. Usa `snake_case` y *negrita* en [x] <b>.") {
		t.Errorf("VerdictText must keep model text raw, got %q", got)
	}
	if strings.Contains(got, "**") {
		t.Errorf("VerdictText has Markdown bold: %q", got)
	}
	if !strings.Contains(got, "Revisión incompleta:") {
		t.Errorf("VerdictText lost the incomplete clause: %q", got)
	}
}

func TestVerdictTextMatchesVerdictLine(t *testing.T) {
	for _, r := range []pipeline.Result{judgmentResult(), incompleteResult(), cleanResult()} {
		for _, phrase := range []string{"", "Una frase."} {
			r.Phrase = phrase
			want := strings.ReplaceAll(VerdictLine(r), "**", "")
			if got := VerdictText(r); got != want {
				t.Errorf("VerdictText = %q, VerdictLine without bold = %q", got, want)
			}
		}
	}
}

func TestVerdictTextFallbackPhrase(t *testing.T) {
	r := judgmentResult()
	r.Phrase = ""
	if got := VerdictText(r); !strings.Contains(got, "La síntesis no está disponible") {
		t.Errorf("fallback phrase missing: %q", got)
	}
}

func TestCommentText(t *testing.T) {
	f := fnd("F1", "a.go", 3, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusConfirmado)
	f.What = "No valida el monto."
	f.Why = "La ruta nueva no pasa por el handler."
	f.Fix = "- old()\n+ new()"
	f.Rule = &finding.RuleRef{File: "docs/reglas/validacion.md", Quote: "validá siempre"}
	got := CommentText(f)
	for _, want := range []string{
		"No valida el monto.", "Por qué pasa: La ruta nueva no pasa por el handler.",
		"Solución, según `docs/reglas/validacion.md`", "```\n- old()\n+ new()\n```",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("CommentText lacks %q:\n%s", want, got)
		}
	}
}

func TestCommentTextSanitizes(t *testing.T) {
	f := fnd("F1", "a.go", 3, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusConfirmado)
	f.What = "# Título *fuerte* [link](http://x) <img src=x>"
	f.Why = "- lista `code`\n> cita"
	f.Fix = "x := 1\n```\nmal()\n```"
	got := CommentText(f)
	for _, bad := range []string{"\n# ", "*fuerte*", "[link]", "\n- lista", "\n> cita"} {
		if strings.Contains(got, bad) || strings.HasPrefix(got, strings.TrimPrefix(bad, "\n")) {
			t.Errorf("CommentText leaves %q live:\n%s", bad, got)
		}
	}
	if strings.Contains(got, "<img") && !strings.Contains(got, `\<img`) {
		t.Errorf("raw HTML left live:\n%s", got)
	}
	// The fix fence must be longer than the backtick run inside the fix.
	if !strings.Contains(got, "````\nx := 1\n```\nmal()\n```\n````") {
		t.Errorf("fix fence can be closed early:\n%s", got)
	}
}

func TestCommentTextWithoutFix(t *testing.T) {
	f := fnd("F1", "a.go", 3, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusConfirmado)
	f.Fix, f.Why = "  ", ""
	got := CommentText(f)
	if strings.Contains(got, "Solución") || strings.Contains(got, "Por qué") {
		t.Errorf("empty why/fix must be left out:\n%s", got)
	}
}

func TestJudgesText(t *testing.T) {
	votes := []finding.JudgeVote{{Judge: "A", Real: true, Worth: true}, {Judge: "B_x", Real: true}, {Judge: "C"}}
	if got, want := JudgesText(votes), "A lo confirma; B_x lo ve menor; C no lo confirma."; got != want {
		t.Errorf("JudgesText = %q, want %q", got, want)
	}
	if got := judges(votes); !strings.Contains(got, `B\_x`) {
		t.Errorf("judges must still escape Markdown: %q", got)
	}
}
