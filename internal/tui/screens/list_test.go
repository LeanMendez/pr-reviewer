package screens

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/LeanMendez/pr-review/internal/eligibility"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/listdata"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

var now = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

func mkRow(id int, title, author string, lines int, age time.Duration) listdata.Row {
	return listdata.Row{
		Ctx: prb.PRContext{
			PullRequest: prb.PR{ID: id, Title: title, Author: author, UpdatedOn: now.Add(-age),
				Review: prb.Review{Approvals: 1, Reviewers: []prb.Reviewer{{Name: "a"}, {Name: "b"}}}},
			Checks:         &prb.Checks{Overall: "pass", Passed: 3},
			Tasks:          &prb.Tasks{},
			Files:          &prb.Files{Count: 23, LinesAdded: 690, LinesRemoved: 210},
			TaskReferences: []prb.TaskRef{{ID: "CU-86b1k9"}},
		},
		Lines:     lines,
		Suggested: pipeline.ModeExhaustivo,
	}
}

func sampleReady() []listdata.Row {
	return []listdata.Row{
		mkRow(477, "Migrar facturas a nueva tabla", "Lucía G.", 900, 24*time.Hour),
		mkRow(88, "Refresh token con rotación", "Tomás V.", 393, 3*time.Hour),
		mkRow(482, "Reintentos en webhook de pagos", "Martina R.", 252, 48*time.Hour),
		mkRow(1290, "Filtros de fecha en reportes", "Joaquín P.", 130, 5*time.Hour),
		mkRow(1288, "Ajuste de textos en onboarding", "Sofía L.", 43, 24*time.Hour),
	}
}

func sampleHidden() []listdata.Row {
	d := mkRow(500, "Borrador de algo", "Ana B.", 10, time.Hour)
	d.Reason = eligibility.ReasonDraft
	f := mkRow(501, "Arreglo con CI rojo", "Beto C.", 20, 2*time.Hour)
	f.Reason = eligibility.ReasonCIFailing
	return []listdata.Row{d, f}
}

func loaded(t *testing.T, calls *int) ListModel {
	t.Helper()
	m := NewList("equipo/api-pagos", func() tea.Cmd { *calls++; return nil })
	m.now = func() time.Time { return now }
	m.loadedAt = now.Add(-time.Minute)
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: 30})
	return update(m, RowsLoadedMsg{Ready: sampleReady(), Hidden: sampleHidden()})
}

func update(m ListModel, msg tea.Msg) ListModel {
	n, _ := m.Update(msg)
	return n.(ListModel)
}

func send(m ListModel, msg tea.Msg) (ListModel, tea.Msg) {
	n, cmd := m.Update(msg)
	var out tea.Msg
	if cmd != nil {
		out = cmd()
	}
	return n.(ListModel), out
}

func checkWidth(t *testing.T, out string, width int) {
	t.Helper()
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("line %d has width %d > %d: %q", i, w, width, tuitest.Plain(l))
		}
	}
}

func TestListRender100(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	out := tuitest.Plain(m.render(100, 30))
	for _, want := range []string{
		"pr-review", "equipo/api-pagos", "5 listos para revisar", "título", "autor", "tamaño (líneas)", "hace",
		"2 PRs no aparecen: 1 en borrador, 1 con CI fallando.",
		"#477  Migrar facturas a nueva tabla", "CU-86b1k9",
		"CI pasó (3 de 3)", "tareas sin resolver 0", "revisores 1 de 2 aprobó, nadie pidió cambios",
		"pr-review nunca lo revisó",
		"23 archivos, +690 −210.",
		"Es grande: 900 líneas, más del doble de las 400 recomendadas.",
		"Modo sugerido: exhaustivo.",
		"↑↓ moverse", "enter elegir modo", "/ filtrar", "t ver todos", "r actualizar", "q salir",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "repo ") && strings.Contains(strings.Split(out, "\n")[2], "repo") {
		t.Errorf("repo column present:\n%s", out)
	}
	lines := strings.Split(out, "\n")
	hdr := lines[2]
	if !strings.Contains(hdr, "#") {
		t.Errorf("no # column: %q", hdr)
	}
	sel := 0
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimLeft(l, " "), "▌") {
			sel++
			if !strings.Contains(l, "477") {
				t.Errorf("selected row is not 477: %q", l)
			}
		}
	}
	if sel != 1 {
		t.Errorf("selected rows = %d", sel)
	}
	checkWidth(t, m.render(100, 30), 100)
}

func TestListDetailVariants(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m.ready[0].Lines = 100
	m.ready[0].Ctx.Checks = &prb.Checks{Overall: "none"}
	m.ready[0].Ctx.TaskReferences = nil
	m.ready[0].Reviewed = true
	m.ready[0].LastReview = now.Add(-2 * time.Hour)
	out := tuitest.Plain(m.render(100, 30))
	for _, want := range []string{"sin CI", "pr-review lo revisó hace 2 h", "o abrir el último reporte"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"Es grande", "en ClickUp"} {
		if strings.Contains(out, bad) {
			t.Errorf("unexpected %q in:\n%s", bad, out)
		}
	}
}

func TestListNarrow(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	out99 := tuitest.Plain(m.render(99, 30))
	if strings.Contains(strings.Split(out99, "\n")[2], "autor") {
		t.Errorf("autor present at 99:\n%s", out99)
	}
	if !strings.Contains(out99, "·│") {
		t.Errorf("bar missing at 99")
	}
	checkWidth(t, m.render(99, 30), 99)
	out79 := tuitest.Plain(m.render(79, 30))
	if strings.Contains(out79, "│") && strings.Contains(out79, "·│") {
		t.Errorf("bar present at 79:\n%s", out79)
	}
	checkWidth(t, m.render(79, 30), 79)
	checkWidth(t, m.render(60, 30), 60)
}

func TestListHostileText(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m.ready[0].Ctx.PullRequest.Title = "Evil\x1b[31m\ntítulo 🚀🚀🚀 con \r\x00 un montón de texto muy largo que no entra en ninguna columna razonable de la tabla"
	m.ready[0].Ctx.PullRequest.Author = "Au\tthor\nXYZ de nombre larguísimo"
	m.ready[0].Ctx.TaskReferences = []prb.TaskRef{{ID: "CU-1\n\x1b[2Jboom"}}
	for _, w := range []int{100, 80} {
		out := m.render(w, 30)
		checkWidth(t, out, w)
		if strings.Contains(out, "\x1b[31m\n") || strings.Contains(out, "\x1b[2J") || strings.Contains(out, "\x00") || strings.Contains(out, "\r") {
			t.Errorf("control characters leaked: %q", out)
		}
		if got := len(strings.Split(out, "\n")); got > 30 {
			t.Errorf("render has %d lines for height 30", got)
		}
	}
}

func TestListKeys(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m = update(m, tuitest.Key("down"))
	if !strings.HasPrefix(strings.TrimLeft(strings.Split(tuitest.Plain(m.render(100, 30)), "\n")[4], " "), "▌ 88") {
		t.Errorf("down did not move selection:\n%s", tuitest.Plain(m.render(100, 30)))
	}
	_, msg := send(m, tuitest.Key("enter"))
	open, ok := msg.(OpenModeMsg)
	if !ok || open.Row.Ctx.PullRequest.ID != 88 {
		t.Errorf("enter msg = %#v", msg)
	}
	m = update(m, tuitest.Key("up"))
	m = update(m, tuitest.Key("up")) // clamp at the top
	if m.cursor != 0 {
		t.Errorf("cursor = %d", m.cursor)
	}

	// t shows the hidden rows with their reason.
	m = update(m, tuitest.Key("t"))
	out := tuitest.Plain(m.render(100, 30))
	for _, want := range []string{"Borrador de algo", "en borrador", "Arreglo con CI rojo", "con CI fallando"} {
		if !strings.Contains(out, want) {
			t.Errorf("t: missing %q in:\n%s", want, out)
		}
	}
	m = update(m, tuitest.Key("t"))
	if strings.Contains(tuitest.Plain(m.render(100, 30)), "Borrador de algo") {
		t.Error("second t did not hide again")
	}

	// / filters by title or author.
	m = update(m, tuitest.Key("/"))
	for _, k := range "joa" {
		m = update(m, tuitest.Key(string(k)))
	}
	out = tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "Filtros de fecha") || strings.Contains(out, "Refresh token") {
		t.Errorf("filter by author failed:\n%s", out)
	}
	m = update(m, tuitest.Key("backspace"))
	m = update(m, tuitest.Key("backspace"))
	m = update(m, tuitest.Key("backspace"))
	for _, k := range "TOKEN" {
		m = update(m, tuitest.Key(string(k)))
	}
	out = tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "Refresh token") || strings.Contains(out, "Filtros de fecha") {
		t.Errorf("filter by title failed:\n%s", out)
	}
	// While typing, q is text, not quit.
	m, msg = send(m, tuitest.Key("q"))
	if msg != nil {
		t.Errorf("q while filtering emitted %#v", msg)
	}
	m = update(m, tuitest.Key("backspace")) // drop the typed q
	m = update(m, tuitest.Key("enter"))     // leaves filter mode, keeps the text
	_, msg = send(m, tuitest.Key("enter"))
	if open, ok := msg.(OpenModeMsg); !ok || open.Row.Ctx.PullRequest.ID != 88 {
		t.Errorf("enter on the filtered row = %#v", msg)
	}
	m = update(m, tuitest.Key("/"))
	m = update(m, tuitest.Key("esc"))
	out = tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "Filtros de fecha") {
		t.Errorf("esc did not clear the filter:\n%s", out)
	}

	_, msg = send(m, tuitest.Key("m"))
	if _, ok := msg.(OpenModelsMsg); !ok {
		t.Errorf("m msg = %#v", msg)
	}
	_, msg = send(m, tuitest.Key("q"))
	if _, ok := msg.(QuitMsg); !ok {
		t.Errorf("q msg = %#v", msg)
	}
	before := calls
	m, _ = send(m, tuitest.Key("r"))
	if calls != before+1 {
		t.Errorf("r did not call load: %d -> %d", before, calls)
	}
	if !strings.Contains(tuitest.Plain(m.render(100, 30)), "Cargando") {
		t.Error("no loading indicator after r")
	}
}

func TestListEnterOnReviewed(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	_, msg := send(m, tuitest.Key("o"))
	if msg != nil {
		t.Errorf("o on an unreviewed PR emitted %#v", msg)
	}
	m.ready[0].Reviewed = true
	m.ready[0].LastReview = now.Add(-2 * time.Hour)
	_, msg = send(m, tuitest.Key("enter"))
	if open, ok := msg.(OpenModeMsg); !ok || open.Row.Ctx.PullRequest.ID != 477 {
		t.Errorf("enter msg = %#v", msg)
	}
	_, msg = send(m, tuitest.Key("o"))
	rep, ok := msg.(OpenReportMsg)
	if !ok || rep.PR != 477 || rep.Repo != "equipo/api-pagos" {
		t.Errorf("o msg = %#v", msg)
	}
	if out := tuitest.Plain(m.render(100, 30)); !strings.Contains(out, "o abrir el último reporte") {
		t.Errorf("missing help:\n%s", out)
	}
}

func TestListError(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m = update(m, RowsLoadedMsg{Err: &prb.Error{Code: "no_credentials", Message: "x"}})
	out := tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "No hay sesión de Bitbucket. Corré prb auth login y apretá r.") {
		t.Errorf("no_credentials:\n%s", out)
	}
	m = update(m, RowsLoadedMsg{Err: &prb.Error{Code: "rate_limited", Message: "demasiados pedidos"}})
	out = tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "demasiados pedidos") || !strings.Contains(out, "apretá r para reintentar") {
		t.Errorf("other error:\n%s", out)
	}
	m = update(m, RowsLoadedMsg{Err: errTest("boom")})
	if out = tuitest.Plain(m.render(100, 30)); !strings.Contains(out, "boom") {
		t.Errorf("plain error:\n%s", out)
	}
	_, msg := send(m, tuitest.Key("q"))
	if _, ok := msg.(QuitMsg); !ok {
		t.Errorf("q on error = %#v", msg)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestListEmpty(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m = update(m, RowsLoadedMsg{Hidden: sampleHidden()})
	out := tuitest.Plain(m.render(100, 30))
	for _, want := range []string{"No hay PRs listos para revisar en equipo/api-pagos.", "2 PRs no aparecen"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	_, msg := send(m, tuitest.Key("enter"))
	if msg != nil {
		t.Errorf("enter on empty list emitted %#v", msg)
	}
}

func TestListScrollsToSelection(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	var many []listdata.Row
	for i := 0; i < 40; i++ {
		many = append(many, mkRow(1000+i, "PR número "+strings.Repeat("x", i%5), "Autor", 100, time.Hour))
	}
	m = update(m, RowsLoadedMsg{Ready: many})
	for i := 0; i < 30; i++ {
		m = update(m, tuitest.Key("down"))
	}
	out := tuitest.Plain(m.render(100, 24))
	if got := len(strings.Split(out, "\n")); got > 24 {
		t.Errorf("render has %d lines for height 24", got)
	}
	if !strings.Contains(out, "▌ 1030") {
		t.Errorf("selected row not visible:\n%s", out)
	}
	if !strings.Contains(out, "↑↓ moverse") {
		t.Errorf("footer lost:\n%s", out)
	}
}

func TestListKeyHelpFits(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	for _, w := range []int{100, 112} {
		out := tuitest.Plain(m.render(w, 30))
		if !strings.Contains(out, "q salir") || strings.Contains(out, "…") {
			t.Errorf("width %d: key help cut:\n%s", w, out)
		}
	}
	if out := tuitest.Plain(m.render(112, 30)); !strings.Contains(out, "m modelos") {
		t.Errorf("m modelos missing at 112:\n%s", out)
	}
}

func TestListMarkReviewed(t *testing.T) {
	var calls int
	m := loaded(t, &calls)
	m = m.MarkReviewed(477, now)
	out := tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "pr-review lo revisó hace 1 min") || !strings.Contains(out, "o abrir el último reporte") {
		t.Errorf("477 not marked:\n%s", out)
	}
	_, msg := send(m, tuitest.Key("o"))
	if rep, ok := msg.(OpenReportMsg); !ok || rep.PR != 477 {
		t.Errorf("o msg = %#v", msg)
	}
	m = update(m, tuitest.Key("down"))
	if out := tuitest.Plain(m.render(100, 30)); !strings.Contains(out, "nunca lo revisó") {
		t.Errorf("88 should stay unreviewed:\n%s", out)
	}
}

func TestListWithNote(t *testing.T) {
	var calls int
	m := loaded(t, &calls).WithNote("No pude abrir el reporte: roto")
	out := tuitest.Plain(m.render(100, 30))
	if !strings.Contains(out, "No pude abrir el reporte: roto") {
		t.Errorf("note not shown:\n%s", out)
	}
	if n := len(strings.Split(out, "\n")); n != 30 {
		t.Errorf("render has %d lines, want 30", n)
	}
	m = update(m, tuitest.Key("down"))
	if out := tuitest.Plain(m.render(100, 30)); strings.Contains(out, "No pude abrir") {
		t.Errorf("the note should go away on the next key:\n%s", out)
	}
}
