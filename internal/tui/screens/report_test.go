package screens

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/report"
	"github.com/LeanMendez/pr-review/internal/rules"
	"github.com/LeanMendez/pr-review/internal/tui/sys"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

const prURL = "https://bitbucket.org/acme/api-pagos/pull-requests/477"

type sysCall struct{ name, arg, stdin string }

// fakeSys records what the screen asks the system to do; tools lists the
// installed commands.
func fakeSys(calls *[]sysCall, tools ...string) sys.System {
	return sys.System{
		Exec: execx.Exec(func(_ context.Context, c execx.Cmd) error {
			call := sysCall{name: c.Name}
			if len(c.Args) > 0 {
				call.arg = c.Args[len(c.Args)-1]
			}
			if c.Stdin != nil {
				b, _ := io.ReadAll(c.Stdin)
				call.stdin = string(b)
			}
			*calls = append(*calls, call)
			return nil
		}),
		LookPath: func(n string) (string, error) {
			for _, t := range tools {
				if t == n {
					return n, nil
				}
			}
			return "", exec.ErrNotFound
		},
	}
}

func rf(id, file string, line int, sev finding.Severity, origin finding.Origin, status finding.Status, what string) finding.Finding {
	return finding.Finding{
		ID: id, File: file, Line: line, Severity: sev, Origin: origin, Status: status, What: what,
		Why: "Por qué de " + id + ".", Fix: "- viejo()\n+ nuevo()",
	}
}

func reportResult() pipeline.Result {
	crit := rf("F1", "migraciones/0042_facturas.sql", 18, finding.SeverityCritico, finding.OriginIntroducido,
		finding.StatusConfirmado, "La migración borra la columna total antes de copiar.")
	crit.Rule = &finding.RuleRef{File: "docs/reglas/migraciones.md", Quote: "toda migración se tiene que poder revertir"}
	crit.Votes = []finding.JudgeVote{{Judge: "A", Real: true, Worth: true}, {Judge: "B", Real: true, Worth: true}}
	med := rf("F2", "src/facturas/servicio.go", 142, finding.SeverityMedio, finding.OriginIntroducido,
		finding.StatusSinConfirmar, "CrearFactura acepta montos negativos.")
	unc := rf("F3", "src/x.go", 5, finding.SeverityAlto, finding.OriginIncierto, finding.StatusConfirmado, "No está claro de quién es.")
	minor1 := rf("F4", "a.go", 1, finding.SeverityBajo, finding.OriginIntroducido, finding.StatusMenor, "Nombre poco claro.")
	minor2 := rf("F5", "b.go", 2, finding.SeverityBajo, finding.OriginIntroducido, finding.StatusMenor, "Comentario viejo.")
	pre := rf("F6", "src/facturas/legacy.go", 30, finding.SeverityMedio, finding.OriginPreexistente,
		finding.StatusConfirmado, "Consulta dentro de un loop.")
	refut := rf("F7", "c.go", 3, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusRefutado, "Falso positivo grande.")
	refut.Reason = "el validador no lo reprodujo"
	filt := rf("F8", "d.go", 4, finding.SeverityBajo, finding.OriginIntroducido, finding.StatusFiltrado, "Filtrado de grupo B.")
	return pipeline.Result{
		Repo:    "api-pagos",
		PR:      prb.PR{ID: 477, Title: "Migrar facturas a nueva tabla", URL: prURL, Author: "Lucía G."},
		Mode:    pipeline.ModeJudgment,
		Verdict: finding.VerdictPedirCambios,
		Phrase:  "Un problema crítico que introduce este PR: puede perder datos al migrar.",
		Findings: []finding.Finding{
			minor1, med, filt, crit, pre, refut, unc, minor2,
		},
		Doubts:     []finding.Doubt{{File: "src/y.go", Line: 9, Text: "¿Por qué dos transacciones?"}},
		Split:      []finding.SplitPart{{Title: "Tabla nueva y migración", Files: []string{"migraciones/"}, Lines: 360}, {Title: "Servicio y repositorio", Files: []string{"src/facturas/"}, Lines: 330, DependsOn: 1}},
		Unreviewed: []string{"package-lock.json"},
		RuleFiles:  []rules.RuleFile{{Path: "CLAUDE.md"}, {Path: "docs/reglas/migraciones.md"}},
	}
}

func newReport(r pipeline.Result, tools ...string) (ReportModel, *[]sysCall) {
	var calls []sysCall
	return NewReport(r, fakeSys(&calls, tools...), "~/Documentos/reviews/api-pagos/pr-477/review.md"), &calls
}

func rplain(m ReportModel) string { return tuitest.Plain(m.render(110, 0)) }

// runCmd runs cmd and feeds its message back to the model.
func runCmd(m ReportModel, cmd tea.Cmd) (ReportModel, tea.Msg) {
	if cmd == nil {
		return m, nil
	}
	msg := cmd()
	n, _ := m.Update(msg)
	return n.(ReportModel), msg
}

func rkey(m ReportModel, k string) (ReportModel, tea.Cmd) {
	n, cmd := m.Update(tuitest.Key(k))
	return n.(ReportModel), cmd
}

func indexOrFail(t *testing.T, out, sub string) int {
	t.Helper()
	i := strings.Index(out, sub)
	if i < 0 {
		t.Fatalf("missing %q in:\n%s", sub, out)
	}
	return i
}

func TestReportRender(t *testing.T) {
	m, _ := newReport(reportResult())
	out := rplain(m)
	lines := strings.Split(out, "\n")
	if !strings.Contains(lines[0], "#477 Migrar facturas a nueva tabla") {
		t.Errorf("header line: %q", lines[0])
	}
	var verdict string
	for _, l := range lines {
		if strings.Contains(l, "Pedir cambios.") {
			verdict = l
		}
	}
	if !strings.Contains(verdict, "Pedir cambios. Un problema crítico que introduce este PR: puede perder datos al migrar.") {
		t.Errorf("verdict line: %q", verdict)
	}
	// section order
	prev := -1
	for _, s := range []string{
		"Lo que introduce este PR", "Crítico", "migraciones/0042_facturas.sql:18",
		"La migración borra la columna total antes de copiar.",
		"Por qué pasa: Por qué de F1.",
		`Solución, según docs/reglas/migraciones.md ("toda migración se tiene que poder revertir"):`,
		"- viejo()", "+ nuevo()", "Jueces: A lo confirma; B lo confirma.",
		"2 detalles menores  enter para verlos",
		"Ya existía antes de este PR", "src/facturas/legacy.go:30",
		"Dudas", "src/y.go:9", "¿Por qué dos transacciones?",
		"Cómo partir este PR", "Tabla nueva y migración", "Servicio y repositorio", "Depende de la 1",
		"Descartados: 1 candidato", "Sin revisar: package-lock.json",
		"Reglas usadas: CLAUDE.md, docs/reglas/migraciones.md",
		"Guardado en ~/Documentos/reviews/api-pagos/pr-477/review.md",
	} {
		i := strings.Index(out[max(prev, 0):], s)
		if i < 0 {
			t.Fatalf("%q missing or out of order after offset %d in:\n%s", s, prev, out)
		}
		prev = max(prev, 0) + i
	}
	for _, l := range lines {
		if strings.Contains(l, "Alto") && strings.Contains(l, "src/x.go:5") && !strings.Contains(l, "origen incierto") {
			t.Errorf("uncertain origin not flagged: %q", l)
		}
		if strings.Contains(l, "Medio") && !strings.Contains(l, "sin confirmar") {
			t.Errorf("sin confirmar not flagged: %q", l)
		}
	}
}

func TestReportVerdictPlainText(t *testing.T) {
	r := reportResult()
	r.Verdict = finding.VerdictRevisarAMano
	r.Phrase = "Mirá `esto` *ahora*."
	out := rplain(func() ReportModel { m, _ := newReport(r); return m }())
	if !strings.Contains(out, "Revisar a mano. Mirá `esto` *ahora*.") {
		t.Errorf("verdict must be plain text without Markdown escapes:\n%s", out)
	}
	if strings.Contains(out, "**") || strings.Contains(out, `\`) {
		t.Errorf("Markdown leaked into the terminal:\n%s", out)
	}
}

func TestReportSections(t *testing.T) {
	m, _ := newReport(reportResult())
	out := rplain(m)
	if strings.Contains(out, "Filtrado de grupo B.") {
		t.Errorf("filtrado must never be shown:\n%s", out)
	}
	if strings.Contains(out, "Falso positivo grande.") {
		t.Errorf("refutado only in the discarded view:\n%s", out)
	}
	if strings.Contains(out, "Nombre poco claro.") {
		t.Errorf("menor must be folded:\n%s", out)
	}
	if !strings.Contains(out, "Consulta dentro de un loop.") {
		t.Errorf("preexistente missing:\n%s", out)
	}
	// preexistente is not in the introduced section
	if i, j := indexOrFail(t, out, "Consulta dentro de un loop."), indexOrFail(t, out, "Ya existía antes de este PR"); i < j {
		t.Errorf("preexistente listed before its section")
	}
	// candidato and unknown statuses show as "sin validar"
	r := reportResult()
	r.Findings = append(r.Findings,
		rf("F9", "e.go", 5, finding.SeverityAlto, finding.OriginIntroducido, finding.StatusCandidato, "Candidato sin pasar."),
		rf("F10", "f.go", 6, finding.SeverityAlto, finding.OriginIntroducido, finding.Status("raro"), "Estado raro."))
	m2, _ := newReport(r)
	out2 := rplain(m2)
	for _, what := range []string{"e.go:5", "f.go:6"} {
		found := false
		for _, l := range strings.Split(out2, "\n") {
			if strings.Contains(l, what) {
				found = true
				if !strings.Contains(l, "sin validar") {
					t.Errorf("%s not flagged sin validar: %q", what, l)
				}
			}
		}
		if !found {
			t.Errorf("%s not shown", what)
		}
	}
}

func TestReportMatchesReviewMarkdownClassification(t *testing.T) {
	// Every finding review.md lists outside the footer must be on screen
	// (after expanding the minor ones) and nothing else may be.
	r := reportResult()
	m, _ := newReport(r)
	m, _ = rkey(m, "down") // F1 -> F3? just move; then expand folded details
	for i := 0; i < 5; i++ {
		m, _ = rkey(m, "down")
	}
	m, _ = rkey(m, "enter")
	out := rplain(m)
	for _, f := range r.Findings {
		shown := strings.Contains(out, f.What)
		want := f.Status != finding.StatusFiltrado && f.Status != finding.StatusRefutado && f.Status != finding.StatusDescartadoPorJueces
		if shown != want {
			t.Errorf("finding %s (%s) shown=%v want=%v:\n%s", f.ID, f.Status, shown, want, out)
		}
	}
}

func TestReportOnlyCriticalIsRed(t *testing.T) {
	const redSeq = "38;2;238;127;122"
	m, _ := newReport(reportResult())
	for name, out := range map[string]string{"default": m.render(110, 0)} {
		n := 0
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, redSeq) {
				n++
				if !strings.Contains(tuitest.Plain(l), "Crítico") {
					t.Errorf("%s: red outside a Crítico label: %q", name, tuitest.Plain(l))
				}
			}
		}
		if n != 1 {
			t.Errorf("%s: red lines = %d, want 1", name, n)
		}
	}
	// no critical findings: no red at all, even with failures and gaps
	r := reportResult()
	r.Findings = r.Findings[1:2]
	r.Gaps = []string{"falta un archivo"}
	r.Failures = []pipeline.Failure{{Agent: "bugs-1", Role: "bugs", Files: []string{"a.go"}, Reason: "boom"}}
	m2, _ := newReport(r)
	m2, _ = rkey(m2, "l")
	if strings.Contains(m2.render(110, 0), redSeq) {
		t.Errorf("red used without a critical finding")
	}
}

func TestReportKeys(t *testing.T) {
	m, calls := newReport(reportResult(), "wl-copy", "xdg-open")

	// enter on the folded details expands them
	if strings.Contains(rplain(m), "Nombre poco claro.") {
		t.Fatal("details should start folded")
	}
	m1, _ := rkey(m, "down")
	m1, _ = rkey(m1, "down")
	m1, _ = rkey(m1, "down") // F1, F3, F2 -> fold row
	m1, _ = rkey(m1, "enter")
	out := rplain(m1)
	for _, want := range []string{"Nombre poco claro.", "Comentario viejo.", "enter para plegarlos"} {
		if !strings.Contains(out, want) {
			t.Errorf("expanded view lacks %q:\n%s", want, out)
		}
	}
	m1, _ = rkey(m1, "enter")
	if strings.Contains(rplain(m1), "Nombre poco claro.") {
		t.Errorf("second enter must fold again")
	}

	// c copies CommentText of the selected finding (the critical one first)
	mc, cmd := rkey(m, "c")
	mc, _ = runCmd(mc, cmd)
	want := report.CommentText(reportResult().Findings[3])
	if len(*calls) != 1 || (*calls)[0].name != "wl-copy" || (*calls)[0].stdin != want {
		t.Errorf("copy calls = %+v, want stdin %q", *calls, want)
	}
	if !strings.Contains(rplain(mc), "Copiado.") {
		t.Errorf("no Copiado. note:\n%s", rplain(mc))
	}
	// the note goes away with the next key
	mc, _ = rkey(mc, "down")
	if strings.Contains(rplain(mc), "Copiado.") {
		t.Errorf("note should clear on the next key")
	}

	// l toggles the discarded list with the reason
	ml, _ := rkey(m, "l")
	out = rplain(ml)
	for _, want := range []string{"Falso positivo grande.", "c.go:3", "el validador no lo reprodujo"} {
		if !strings.Contains(out, want) {
			t.Errorf("discarded view lacks %q:\n%s", want, out)
		}
	}
	ml, _ = rkey(ml, "l")
	if strings.Contains(rplain(ml), "Falso positivo grande.") {
		t.Errorf("l again must hide the discarded list")
	}

	// o opens the PR URL
	*calls = nil
	mo, cmd := rkey(m, "o")
	runCmd(mo, cmd)
	if len(*calls) != 1 || (*calls)[0].name != "xdg-open" || (*calls)[0].arg != prURL {
		t.Errorf("open calls = %+v", *calls)
	}

	// q goes back, ctrl+c quits
	if _, cmd := rkey(m, "q"); cmd == nil {
		t.Fatal("q returned no command")
	} else if _, ok := cmd().(BackMsg); !ok {
		t.Errorf("q did not emit BackMsg")
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); cmd == nil {
		t.Fatal("ctrl+c returned no command")
	} else if _, ok := cmd().(QuitMsg); !ok {
		t.Errorf("ctrl+c did not emit QuitMsg")
	}
}

func TestReportCopyOnFoldedRowDoesNothing(t *testing.T) {
	m, calls := newReport(reportResult(), "wl-copy")
	for i := 0; i < 3; i++ {
		m, _ = rkey(m, "down")
	}
	m, cmd := rkey(m, "c")
	m, _ = runCmd(m, cmd)
	if len(*calls) != 0 {
		t.Errorf("copy ran on the fold row: %+v", *calls)
	}
}

func TestReportNoClipboard(t *testing.T) {
	m, _ := newReport(reportResult()) // no clipboard tools installed
	m, cmd := rkey(m, "c")
	m, _ = runCmd(m, cmd)
	if out := rplain(m); !strings.Contains(out, "No encontré wl-copy, xclip ni xsel para copiar.") {
		t.Errorf("missing no-clipboard note:\n%s", out)
	}
}

func TestReportCopyFailure(t *testing.T) {
	m, _ := newReport(reportResult(), "wl-copy")
	m.sys.Exec = func(context.Context, execx.Cmd) error { return errors.New("boom") }
	m, cmd := rkey(m, "c")
	m, _ = runCmd(m, cmd)
	if out := rplain(m); !strings.Contains(out, "No pude copiar:") || strings.Contains(out, "Copiado.") {
		t.Errorf("copy failure not reported:\n%s", out)
	}
}

func TestReportOpenWithoutURL(t *testing.T) {
	r := reportResult()
	r.PR.URL = ""
	m, calls := newReport(r, "xdg-open")
	m, cmd := rkey(m, "o")
	m, _ = runCmd(m, cmd)
	if len(*calls) != 0 || !strings.Contains(rplain(m), "no tiene dirección") {
		t.Errorf("o without URL: calls=%+v\n%s", *calls, rplain(m))
	}
}

func TestReportFooterShowsGapsAndFailures(t *testing.T) {
	r := reportResult()
	r.Gaps = []string{"el diff de a.go quedó cortado"}
	r.Excluded = []string{"yarn.lock"}
	r.Failures = []pipeline.Failure{
		{Agent: "bugs-1", Role: "bugs", Files: []string{"a.go", "b.go"}, Reason: "se venció el tiempo\nsegunda línea"},
		{Agent: "validador_bugs-F3", Role: "validador_bugs", Candidate: "F3", Reason: "salida inválida"},
	}
	r.FailedAgents = []string{"bugs-1", "validador_bugs-F3", "juez-1"}
	m, _ := newReport(r)
	out := rplain(m)
	for _, want := range []string{
		"No se pudo comprobar que el diff esté completo", "el diff de a.go quedó cortado",
		"Excluidos de la revisión (generados): yarn.lock",
		"Bugs y lógica", "a.go, b.go", "se venció el tiempo",
		"Validador de bugs", "candidato F3", "salida inválida",
		"juez-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("footer lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "segunda línea") {
		t.Errorf("only the first line of a reason is shown:\n%s", out)
	}
	if !strings.Contains(out, "Revisión incompleta:") {
		t.Errorf("verdict lacks the incomplete clause:\n%s", out)
	}
}

func TestReportNothingToShow(t *testing.T) {
	r := pipeline.Result{PR: prb.PR{ID: 1, Title: "t"}, Verdict: finding.VerdictAprobar}
	m := NewReport(r, sys.System{}, "")
	out := rplain(m)
	for _, want := range []string{"Aprobar.", "Reglas usadas: ninguna encontrada."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Descartados") || strings.Contains(out, "Guardado en") {
		t.Errorf("empty sections must not show:\n%s", out)
	}
	// keys on an empty report do not panic
	for _, k := range []string{"up", "down", "enter", "c", "l", "o"} {
		m, _ = rkey(m, k)
	}
}

func hostileResult() pipeline.Result {
	long := strings.Repeat("palabralargaquenocabe", 12)
	f := rf("F1", "src/"+long+".go", 7, finding.SeverityCritico, finding.OriginIntroducido, finding.StatusConfirmado,
		"Texto con \x1b[31mescape\x1b[0m, \x07campana y "+long+" y 日本語日本語日本語日本語日本語")
	f.Why = "porque\ttab\r\notra línea " + long
	f.Fix = "+ " + long + "\n\x1b[2Jborrar()"
	f.Rule = &finding.RuleRef{File: long + ".md", Quote: long}
	f.Votes = []finding.JudgeVote{{Judge: "A\x1b[1m", Real: true, Worth: true}}
	r := reportResult()
	r.Findings = []finding.Finding{f}
	r.Phrase = "Frase \x1b[31mroja\x1b[0m " + long
	r.PR.Title = "Título\x1b]0;pwn\x07 " + long
	r.Doubts = []finding.Doubt{{File: long, Line: 1, Text: "duda \x1b[5m" + long}}
	r.Split = []finding.SplitPart{{Title: long, Files: []string{long, long}, Lines: 5}}
	r.Unreviewed = []string{long}
	r.Gaps = []string{"gap \x1b[1m" + long}
	r.Failures = []pipeline.Failure{{Agent: "x", Role: "bugs", Files: []string{long}, Reason: long}}
	return r
}

func TestReportTerminalSafety(t *testing.T) {
	for _, w := range []int{40, 80, 110} {
		m, _ := newReport(hostileResult())
		m, _ = rkey(m, "l")
		out := m.render(w, 0)
		checkWidth(t, out, w)
		plain := tuitest.Plain(out)
		if strings.ContainsFunc(strings.ReplaceAll(plain, "\n", ""), unicode.IsControl) {
			t.Errorf("width %d: control characters reach the terminal: %q", w, plain)
		}
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, "\x1b[31m") && !strings.Contains(l, "Crítico") {
				// theme styles only; model escapes must have been stripped
				t.Errorf("width %d: model escape sequence survived: %q", w, l)
			}
		}
	}
}

func TestReportScrollsToSelection(t *testing.T) {
	r := reportResult()
	r.Findings = nil
	for i := 0; i < 8; i++ {
		r.Findings = append(r.Findings, rf("G"+string(rune('a'+i)), "f"+string(rune('a'+i))+".go", i+1,
			finding.SeverityMedio, finding.OriginIntroducido, finding.StatusConfirmado, "Hallazgo número "+string(rune('a'+i))+"."))
	}
	m, _ := newReport(r)
	const h = 14
	for i := 0; i < 7; i++ {
		m, _ = rkey(m, "down")
		out := m.render(100, h)
		if n := len(strings.Split(out, "\n")); n != h {
			t.Fatalf("render height = %d, want %d", n, h)
		}
		if p := tuitest.Plain(out); !strings.Contains(p, "Hallazgo número "+string(rune('a'+i+1))+".") {
			t.Fatalf("selected finding %d not visible:\n%s", i+1, p)
		}
		if !strings.Contains(tuitest.Plain(out), "q volver") {
			t.Fatalf("key help not pinned at the bottom:\n%s", tuitest.Plain(out))
		}
	}
	// and back up to the first
	for i := 0; i < 7; i++ {
		m, _ = rkey(m, "up")
	}
	if p := tuitest.Plain(m.render(100, h)); !strings.Contains(p, "Hallazgo número a.") {
		t.Errorf("first finding not visible after scrolling back:\n%s", p)
	}
}

func TestReportCursorMarker(t *testing.T) {
	m, _ := newReport(reportResult())
	mark := func(m ReportModel) string {
		for _, l := range strings.Split(rplain(m), "\n") {
			if strings.Contains(l, "▌") && strings.Contains(l, ":") {
				return l
			}
		}
		return ""
	}
	if l := mark(m); !strings.Contains(l, "migraciones/0042_facturas.sql:18") {
		t.Errorf("first marker on %q", l)
	}
	m, _ = rkey(m, "down")
	if l := mark(m); !strings.Contains(l, "src/x.go:5") { // alto goes before medio
		t.Errorf("second marker on %q", l)
	}
}
