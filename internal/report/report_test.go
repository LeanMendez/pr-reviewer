package report

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/rules"
)

var update = flag.Bool("update", false, "rewrite the golden files")

var t0 = time.Date(2026, 11, 12, 4, 12, 0, 0, time.UTC)

func basePR() prb.PR {
	return prb.PR{ID: 477, Title: "Migrar facturas a nueva tabla", URL: "https://bitbucket.org/acme/api-pagos/pull-requests/477", Author: "Lucía G."}
}

func fnd(id, file string, line int, sev finding.Severity, origin finding.Origin, status finding.Status) finding.Finding {
	return finding.Finding{
		ID: id, Role: "bugs", File: file, Line: line, Severity: sev, Origin: origin,
		Group: finding.GroupA, What: "Qué pasa de " + id + ".", Why: "Por qué de " + id + ".",
		Fix: "Arreglo de " + id + ".", Status: status,
	}
}

func judgmentResult() pipeline.Result {
	crit := fnd("F1", "migraciones/0042_facturas.sql", 18, finding.SeverityCritico, finding.OriginIntroducido, finding.StatusConfirmado)
	crit.Rule = &finding.RuleRef{Quote: "toda migración se tiene que poder revertir", File: "docs/reglas/migraciones.md"}
	crit.Votes = []finding.JudgeVote{
		{Judge: "A", FindingID: "F1", Real: true, Worth: true, FixComplies: true},
		{Judge: "B", FindingID: "F1", Real: true, Worth: true, FixComplies: true},
	}
	med := fnd("F2", "src/facturas/servicio.go", 142, finding.SeverityMedio, finding.OriginIntroducido, finding.StatusConfirmado)
	med.Votes = []finding.JudgeVote{
		{Judge: "A", FindingID: "F2", Real: true, Worth: true, FixComplies: true},
		{Judge: "B", FindingID: "F2", Real: true, Worth: false, FixComplies: true},
	}
	unconf := fnd("F3", "src/facturas/pdf.go", 9, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusSinConfirmar)
	uncertain := fnd("F4", "src/facturas/repo.go", 55, finding.SeverityMedio, finding.OriginIncierto, finding.StatusConfirmado)
	minor := fnd("F5", "src/facturas/nombres.go", 3, finding.SeverityBajo, finding.OriginIntroducido, finding.StatusMenor)
	pre := fnd("F6", "src/facturas/legacy.go", 30, finding.SeverityMedio, finding.OriginPreexistente, finding.StatusConfirmado)
	pre.What = "Consulta dentro de un loop (N+1)."
	refuted := fnd("F7", "src/a.go", 1, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusRefutado)
	refuted.Reason = "el validador no encontró la ruta que describe"
	judged := fnd("F8", "src/b.go", 2, finding.SeverityMedio, finding.OriginIntroducido, finding.StatusDescartadoPorJueces)
	judged.Reason = "los dos jueces lo ven irrelevante"
	hidden := fnd("F9", "src/c.go", 3, finding.SeverityBajo, finding.OriginIntroducido, finding.StatusFiltrado)
	hidden.Group = finding.GroupB
	hidden.What = "OCULTO-FILTRADO"
	return pipeline.Result{
		Repo: "api-pagos", PR: basePR(), Mode: pipeline.ModeJudgment,
		Verdict: finding.VerdictPedirCambios, Phrase: "Un problema crítico que introduce este PR: puede perder datos al migrar.",
		Findings: []finding.Finding{crit, med, unconf, uncertain, minor, pre, refuted, judged, hidden},
		Doubts:   []finding.Doubt{{File: "src/facturas/api.go", Line: 12, Text: "¿Esto cambia el contrato que usa el repo de reportes?"}},
		Split: []finding.SplitPart{
			{Title: "Tabla nueva y migración", Files: []string{"migraciones/"}, Lines: 360},
			{Title: "Servicio y repositorio", Files: []string{"src/facturas/"}, Lines: 540, DependsOn: 1},
		},
		Excluded:   []string{"package-lock.json"},
		RuleFiles:  []rules.RuleFile{{Path: "CLAUDE.md", Bytes: 10}, {Path: "docs/reglas/migraciones.md", Bytes: 20}},
		AgentCount: 12, Started: t0, Finished: t0.Add(6*time.Minute + 40*time.Second),
	}
}

func incompleteResult() pipeline.Result {
	return pipeline.Result{
		Repo: "api-pagos", PR: basePR(), Mode: pipeline.ModeExhaustivo,
		Verdict: finding.VerdictRevisarAMano, IncompleteWarning: false,
		Phrase:       "No alcanza para decidir: faltó revisar parte del PR.",
		Findings:     []finding.Finding{fnd("F1", "src/x.go", 4, finding.SeverityMedio, finding.OriginIntroducido, finding.StatusConfirmado)},
		Unreviewed:   []string{"src/enorme.go (parche omitido)"},
		FailedAgents: []string{"seguridad", "validador_bugs-F1"},
		Failures: []pipeline.Failure{
			{Agent: "seguridad", Role: "seguridad", Files: []string{"src/x.go", "src/y.go"}, Reason: "se venció el tiempo"},
			{Agent: "validador_bugs-F1", Role: "validador_bugs", Candidate: "F1", Reason: "salida inválida: JSON roto\nsegunda línea"},
		},
		AgentCount: 5, Started: t0, Finished: t0.Add(3 * time.Minute),
	}
}

func cleanResult() pipeline.Result {
	return pipeline.Result{
		Repo: "api-pagos", PR: basePR(), Mode: pipeline.ModeLigero,
		Verdict: finding.VerdictAprobar, Phrase: "No encontré problemas que introduzca este PR.",
		RuleFiles:  []rules.RuleFile{{Path: "CLAUDE.md", Bytes: 10}},
		AgentCount: 4, Started: t0, Finished: t0.Add(95 * time.Second),
	}
}

func TestMarkdownGolden(t *testing.T) {
	cases := map[string]pipeline.Result{
		"pedir_cambios_judges": judgmentResult(),
		"revisar_a_mano":       incompleteResult(),
		"aprobar":              cleanResult(),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			got := Markdown(r)
			path := filepath.Join("testdata", "review_"+name+".golden.md")
			if *update {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("review.md differs from %s:\n%s", path, got)
			}
		})
	}
}

func TestFilteredOnlyInJSON(t *testing.T) {
	md := Markdown(judgmentResult())
	if strings.Contains(md, "OCULTO-FILTRADO") || strings.Contains(md, "F9") {
		t.Errorf("filtered finding leaked into review.md:\n%s", md)
	}
}

func TestSectionsOmittedWhenEmpty(t *testing.T) {
	md := Markdown(cleanResult())
	for _, h := range []string{"## Lo que introduce", "## Detalles menores", "## Ya existía", "## Dudas", "## Cómo partir"} {
		if strings.Contains(md, h) {
			t.Errorf("empty section %q present:\n%s", h, md)
		}
	}
	if !strings.Contains(md, "## Al pie") {
		t.Error("Al pie missing")
	}
}

func TestVerdictLineNames(t *testing.T) {
	cases := map[finding.Verdict]string{
		finding.VerdictAprobar:               "**Aprobar.**",
		finding.VerdictAprobarConComentarios: "**Aprobar con comentarios.**",
		finding.VerdictPedirCambios:          "**Pedir cambios.**",
		finding.VerdictRevisarAMano:          "**Revisar a mano.**",
	}
	for v, want := range cases {
		r := cleanResult()
		r.Verdict = v
		if got := VerdictLine(r); !strings.HasPrefix(got, want+" ") {
			t.Errorf("%s: got %q, want prefix %q", v, got, want)
		}
	}
}

func TestVerdictLineIncompleteWarning(t *testing.T) {
	r := judgmentResult()
	r.IncompleteWarning = true
	got := VerdictLine(r)
	if !strings.Contains(got, "Revisión incompleta.") {
		t.Errorf("no incomplete warning: %q", got)
	}
	if strings.Contains(VerdictLine(judgmentResult()), "Revisión incompleta.") {
		t.Error("warning shown when not incomplete")
	}
}

func TestVerdictLineFallbackPhrase(t *testing.T) {
	r := judgmentResult()
	r.Phrase = ""
	got := VerdictLine(r)
	if !strings.Contains(got, "1 problema crítico que introduce este PR.") {
		t.Errorf("fallback missing critical count: %q", got)
	}
	if !strings.Contains(got, "La síntesis no está disponible; revisá los hallazgos abajo.") {
		t.Errorf("fallback missing synthesis note: %q", got)
	}
	r = cleanResult()
	r.Phrase = ""
	got = VerdictLine(r)
	if !strings.HasPrefix(got, "**Aprobar.** ") || len(strings.TrimPrefix(got, "**Aprobar.** ")) == 0 {
		t.Errorf("empty sentence: %q", got)
	}
	if !strings.Contains(got, "La síntesis no está disponible") {
		t.Errorf("no synthesis note: %q", got)
	}
}

func TestFailuresInFooter(t *testing.T) {
	md := Markdown(incompleteResult())
	foot := md[strings.Index(md, "## Al pie"):]
	for _, want := range []string{
		"seguridad", "src/x.go, src/y.go", "se venció el tiempo",
		"validador_bugs", "F1", "salida inválida: JSON roto",
	} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer missing %q:\n%s", want, foot)
		}
	}
	if strings.Contains(foot, "segunda línea") {
		t.Error("reason not cut to its first line")
	}
}

func TestFailureReasonTruncated(t *testing.T) {
	r := incompleteResult()
	r.Failures = []pipeline.Failure{{Agent: "bugs", Role: "bugs", Reason: strings.Repeat("x", 500)}}
	md := Markdown(r)
	if strings.Contains(md, strings.Repeat("x", 201)) {
		t.Error("reason longer than 200 characters")
	}
	if !strings.Contains(md, strings.Repeat("x", 199)) {
		t.Error("reason cut too short")
	}
}

func TestUnreviewedShown(t *testing.T) {
	md := Markdown(incompleteResult())
	foot := md[strings.Index(md, "## Al pie"):]
	if !strings.Contains(foot, "src/enorme.go (parche omitido)") {
		t.Errorf("unreviewed missing from footer:\n%s", foot)
	}
}

func TestDiscardedWithReasonInFooter(t *testing.T) {
	md := Markdown(judgmentResult())
	foot := md[strings.Index(md, "## Al pie"):]
	for _, want := range []string{"el validador no encontró la ruta que describe", "los dos jueces lo ven irrelevante"} {
		if !strings.Contains(foot, want) {
			t.Errorf("footer missing %q", want)
		}
	}
	if strings.Contains(md[:strings.Index(md, "## Al pie")], "el validador no encontró") {
		t.Error("discarded finding outside the footer")
	}
}

func TestDirFormat(t *testing.T) {
	at := time.Date(2026, 11, 12, 4, 12, 0, 0, time.UTC)
	got := Dir("/docs/reviews", "stock", 477, at)
	if want := "/docs/reviews/stock/pr-477-20261112-0412"; got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestReportsRoot(t *testing.T) {
	ok := execx.Fake(func(c execx.Cmd) (string, string, int) {
		if c.Name != "xdg-user-dir" || len(c.Args) != 1 || c.Args[0] != "DOCUMENTS" {
			t.Errorf("unexpected command %v", c)
		}
		return "/home/x/Docs\n", "", 0
	})
	if got, want := ReportsRoot(ok), "/home/x/Docs/reviews"; got != want {
		t.Errorf("ReportsRoot = %q, want %q", got, want)
	}

	t.Setenv("HOME", "/home/fallback")
	for name, e := range map[string]execx.Exec{
		"exit":  execx.Fake(func(execx.Cmd) (string, string, int) { return "", "boom", 1 }),
		"empty": execx.Fake(func(execx.Cmd) (string, string, int) { return "\n", "", 0 }),
	} {
		if got, want := ReportsRoot(e), "/home/fallback/Documentos/reviews"; got != want {
			t.Errorf("%s: ReportsRoot = %q, want %q", name, got, want)
		}
	}
	_ = context.Background
}

func TestWriteCreatesFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "api-pagos", "pr-477-20261112-0412")
	r := judgmentResult()
	d := prb.Diff{Files: []prb.DiffFile{
		{Path: "a.go", OldPath: "a.go", Status: "modified", Patch: "@@ -1 +1 @@\n-x\n+y\n"},
		{Path: "b.go", OldPath: "old.go", Status: "renamed", Patch: "@@ -1 +1 @@\n-p\n+q"},
	}}
	if err := Write(dir, r, []byte(`{"ctx":true}`), d); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"context.json", "pr.diff", "findings.json", "review.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "findings.json"))
	var back pipeline.Result
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if back.PR.ID != 477 || len(back.Findings) != len(r.Findings) || back.Verdict != r.Verdict {
		t.Errorf("findings.json round trip lost data: %+v", back)
	}
	ctx, _ := os.ReadFile(filepath.Join(dir, "context.json"))
	if string(ctx) != `{"ctx":true}` {
		t.Errorf("context.json = %q", ctx)
	}
	diff, _ := os.ReadFile(filepath.Join(dir, "pr.diff"))
	want := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-x\n+y\n" +
		"diff --git a/old.go b/b.go\n--- a/old.go\n+++ b/b.go\n@@ -1 +1 @@\n-p\n+q\n"
	if string(diff) != want {
		t.Errorf("pr.diff = %q, want %q", diff, want)
	}
	md, _ := os.ReadFile(filepath.Join(dir, "review.md"))
	if string(md) != Markdown(r) {
		t.Error("review.md differs from Markdown(r)")
	}
}

func headings(md string) []string {
	var hs []string
	fence := ""
	for _, l := range strings.Split(md, "\n") {
		if t := strings.TrimLeft(l, "`"); strings.HasPrefix(l, "```") && (fence == "" || (t == "" && len(l) >= len(fence))) {
			if fence == "" {
				fence = l[:len(l)-len(t)]
			} else {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if strings.HasPrefix(l, "## ") || strings.HasPrefix(l, "# ") {
			hs = append(hs, l)
		}
	}
	return hs
}

func TestMarkdownInjectionNeutralized(t *testing.T) {
	clean := judgmentResult()
	evil := judgmentResult()
	evil.PR.Title = "Titulo\n## Al pie\n`rota"
	for i := range evil.Findings {
		f := &evil.Findings[i]
		f.What += "\n## Al pie\n### Crítico `x`\n> cita\n- item\n---"
		f.Why += "\n## Dudas\n# otro"
		f.File += "`\n## Al pie"
		if f.Rule != nil {
			f.Rule.File += "`x"
			f.Rule.Quote += "\" y ` z"
		}
	}
	evil.Findings[0].Fix = "linea\n```\n## Al pie\n```\nfin"
	evil.Unreviewed = []string{"a`b\n## Al pie"}
	evil.Failures = []pipeline.Failure{{Agent: "ag`ent", Role: "bugs", Files: []string{"f`\n## Al pie"}, Reason: "r"}}
	evil.Doubts[0].Text = "duda\n## Al pie"
	md := Markdown(evil)
	want := headings(Markdown(clean))
	// the title line differs in content, but the heading set must keep its shape
	got := headings(md)
	if len(got) != len(want) {
		t.Fatalf("headings changed: got %q want %q\n%s", got, want, md)
	}
	if n := strings.Count(md, "\n### "); n != strings.Count(Markdown(clean), "\n### ") {
		t.Errorf("finding headings changed: %d\n%s", n, md)
	}
	// the fix fence must be longer than the ``` inside it
	if !strings.Contains(md, "````\nlinea\n```\n## Al pie\n```\nfin\n````") {
		t.Errorf("fix fence broken:\n%s", md)
	}
}

func TestEveryNonFilteredFindingShown(t *testing.T) {
	statuses := []finding.Status{
		finding.StatusCandidato, finding.StatusConfirmado, finding.StatusRefutado, finding.StatusSinConfirmar,
		finding.StatusMenor, finding.StatusDescartadoPorJueces, "", "raro",
	}
	for _, st := range statuses {
		for _, origin := range []finding.Origin{finding.OriginIntroducido, finding.OriginPreexistente} {
			r := cleanResult()
			f := fnd("F1", "src/unico.go", 77, finding.SeverityMedio, origin, st)
			r.Findings = []finding.Finding{f}
			if md := Markdown(r); !strings.Contains(md, "src/unico.go:77") {
				t.Errorf("status %q origin %s not shown:\n%s", st, origin, md)
			}
		}
	}
	r := cleanResult()
	r.Findings = []finding.Finding{fnd("F1", "src/unico.go", 77, finding.SeverityMedio, finding.OriginIntroducido, finding.StatusFiltrado)}
	if strings.Contains(Markdown(r), "src/unico.go") {
		t.Error("filtrado shown")
	}
	r.Findings = []finding.Finding{fnd("F1", "src/unico.go", 77, finding.SeverityMedio, finding.OriginIntroducido, finding.StatusCandidato)}
	if md := Markdown(r); !strings.Contains(md, "Sin validar") || !strings.Contains(md, "## Lo que introduce este PR") {
		t.Errorf("candidate not marked:\n%s", md)
	}
}

func TestWritePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r", "pr-1-x")
	if err := Write(dir, cleanResult(), []byte("{}"), prb.Diff{}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", st.Mode().Perm())
	}
	for _, f := range []string{"context.json", "pr.diff", "findings.json", "review.md"} {
		st, _ := os.Stat(filepath.Join(dir, f))
		if st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", f, st.Mode().Perm())
		}
	}
	// an existing directory is not widened
	tight := t.TempDir()
	os.Chmod(tight, 0o700)
	if err := Write(tight, cleanResult(), nil, prb.Diff{}); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(tight); st.Mode().Perm() != 0o700 {
		t.Errorf("existing dir widened to %v", st.Mode().Perm())
	}
}

func TestFallbackSaysIncomplete(t *testing.T) {
	r := cleanResult()
	r.Phrase = ""
	r.Unreviewed = []string{"x.go"}
	got := VerdictLine(r)
	if !strings.Contains(got, "incompleta") {
		t.Errorf("no incomplete note: %q", got)
	}
	if strings.Contains(got, "No hay problemas confirmados que introduzca este PR.") {
		t.Errorf("clean claim without caveat: %q", got)
	}
	r = incompleteResult()
	r.Phrase = ""
	if got := VerdictLine(r); !strings.Contains(got, "incompleta") {
		t.Errorf("failures: no incomplete note: %q", got)
	}
}
