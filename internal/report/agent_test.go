package report

import (
	"strings"
	"testing"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
)

// agentDiff is a pr.diff as Write saves it: our header plus prb's patch,
// which carries its own header.
func agentDiff() string {
	return unifiedDiff(prb.Diff{Files: []prb.DiffFile{
		{Path: "migraciones/0042_facturas.sql", Patch: "diff --git a/migraciones/0042_facturas.sql b/migraciones/0042_facturas.sql\n" +
			"--- a/migraciones/0042_facturas.sql\n+++ b/migraciones/0042_facturas.sql\n" +
			"@@ -10,4 +10,5 @@ CREATE TABLE facturas\n ctx\n-ALTER TABLE facturas DROP COLUMN total;\n" +
			"+ALTER TABLE facturas ADD COLUMN monto int;\n+UPDATE facturas SET monto = total;\n ctx2\n" +
			"@@ -200,2 +201,3 @@\n fin\n+-- LEJANO\n fin2\n"},
		{Path: "src/otro.go", Patch: "diff --git a/src/otro.go b/src/otro.go\n--- a/src/otro.go\n+++ b/src/otro.go\n" +
			"@@ -18,2 +18,3 @@\n a\n+OTRO_ARCHIVO\n b\n"},
		{Path: "src/borrado.go", Patch: "diff --git a/src/borrado.go b/src/borrado.go\ndeleted file mode 100644\n" +
			"--- a/src/borrado.go\n+++ /dev/null\n@@ -1,2 +0,0 @@\n-BORRADO_1\n-BORRADO_2\n"},
	}})
}

func agentFinding() finding.Finding {
	return finding.Finding{
		ID: "F1", Role: "bugs", File: "migraciones/0042_facturas.sql", Line: 12, EndLine: 13, Function: "migrar",
		Severity: finding.SeverityCritico, Origin: finding.OriginIntroducido, Status: finding.StatusConfirmado,
		What: "La migración borra la columna total antes de copiar.", Why: "El DROP corre antes del UPDATE.",
		Fix:      "- ALTER TABLE facturas DROP COLUMN total;\n+ ALTER TABLE facturas ADD COLUMN monto int;",
		Evidence: []string{"migraciones/0042_facturas.sql:12", "base:migraciones/0041_facturas.sql:3"},
		Rule:     &finding.RuleRef{File: "docs/reglas/migraciones.md", Quote: "toda migración se tiene que poder revertir"},
		Votes:    []finding.JudgeVote{{Judge: "A", Real: true, Worth: true, AlternativeFix: "migrar en dos pasos"}},
	}
}

func agentResult() pipeline.Result {
	return pipeline.Result{
		Repo: "acme/api-pagos", Mode: pipeline.ModeJudgment,
		PR: prb.PR{ID: 477, Title: "Migrar facturas", URL: "https://bitbucket.org/acme/api-pagos/pull-requests/477",
			Source: prb.Ref{Branch: "feat/facturas", Commit: "abc123"}, Destination: prb.Ref{Branch: "main", Commit: "def456"}},
	}
}

func TestAgentTextCarriesTechnicalContext(t *testing.T) {
	f := agentFinding()
	got := AgentText(agentResult(), f, agentDiff())
	for _, want := range []string{
		"#477 Migrar facturas", "https://bitbucket.org/acme/api-pagos/pull-requests/477", "acme/api-pagos",
		"feat/facturas @ abc123", "main @ def456", "judgment",
		"migraciones/0042_facturas.sql:12-13", "migrar", "bugs",
		string(f.Severity), string(f.Origin), string(f.Status),
		f.What, f.Why, f.Fix,
		"docs/reglas/migraciones.md", "toda migración se tiene que poder revertir",
		"migraciones/0042_facturas.sql:12", "base:migraciones/0041_facturas.sql:3",
		"migrar en dos pasos",
		"@@ -10,4 +10,5 @@", "+ALTER TABLE facturas ADD COLUMN monto int;",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AgentText lacks %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"LEJANO", "OTRO_ARCHIVO"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("AgentText has %q from a hunk that does not touch the finding:\n%s", unwanted, got)
		}
	}
}

func TestAgentTextWithoutDiff(t *testing.T) {
	got := AgentText(agentResult(), agentFinding(), "")
	if !strings.Contains(got, "La migración borra la columna total antes de copiar.") {
		t.Fatalf("finding missing:\n%s", got)
	}
	if strings.Contains(got, "@@") {
		t.Errorf("no diff given, yet a fragment appears:\n%s", got)
	}
}

func TestDiffFragment(t *testing.T) {
	diff := agentDiff()
	tests := []struct {
		name           string
		file           string
		line, endLine  int
		want, unwanted []string
	}{
		{"hunk with the line", "migraciones/0042_facturas.sql", 12, 0,
			[]string{"@@ -10,4 +10,5 @@", "+UPDATE facturas SET monto = total;"}, []string{"LEJANO", "OTRO_ARCHIVO"}},
		{"range reaching a second hunk", "migraciones/0042_facturas.sql", 14, 202,
			[]string{"@@ -10,4 +10,5 @@", "+-- LEJANO"}, []string{"OTRO_ARCHIVO"}},
		{"no line takes the whole file", "migraciones/0042_facturas.sql", 0, 0,
			[]string{"@@ -10,4 +10,5 @@", "@@ -200,2 +201,3 @@"}, []string{"OTRO_ARCHIVO"}},
		{"other file", "src/otro.go", 18, 0, []string{"+OTRO_ARCHIVO"}, []string{"LEJANO", "monto"}},
		{"deleted file", "src/borrado.go", 0, 0, []string{"-BORRADO_1"}, []string{"OTRO_ARCHIVO"}},
		{"line outside every hunk", "migraciones/0042_facturas.sql", 100, 0, nil, []string{"@@"}},
		{"file not in the diff", "src/nada.go", 1, 0, nil, []string{"@@"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffFragment(diff, tt.file, tt.line, tt.endLine)
			if tt.want == nil && got != "" {
				t.Errorf("want no fragment, got:\n%s", got)
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("fragment lacks %q:\n%s", w, got)
				}
			}
			for _, u := range tt.unwanted {
				if strings.Contains(got, u) {
					t.Errorf("fragment has %q:\n%s", u, got)
				}
			}
		})
	}
}

func TestDiffFragmentIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/big.go b/big.go\n--- a/big.go\n+++ b/big.go\n@@ -0,0 +1,500 @@\n")
	for i := 0; i < 500; i++ {
		b.WriteString("+linea\n")
	}
	got := DiffFragment(b.String(), "big.go", 0, 0)
	if n := strings.Count(got, "\n"); n > maxFragmentLines+2 {
		t.Errorf("fragment has %d lines, want at most %d plus the cut note", n, maxFragmentLines)
	}
	if !strings.Contains(got, "recortado") {
		t.Errorf("a cut fragment must say so:\n%s", got)
	}
}
