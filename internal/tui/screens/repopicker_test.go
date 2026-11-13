package screens

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/prb"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

func sampleRepos() []prb.Repo {
	return []prb.Repo{
		{Slug: "stock", Name: "stock", FullName: "ws/stock"},
		{Slug: "api-pagos", Name: "API de pagos", FullName: "ws/api-pagos"},
		{Slug: "web", Name: "web", FullName: "ws/web"},
	}
}

func pickerKey(m RepoPickerModel, key string) (RepoPickerModel, tea.Msg) {
	n, cmd := m.Update(tuitest.Key(key))
	var out tea.Msg
	if cmd != nil {
		out = cmd()
	}
	return n.(RepoPickerModel), out
}

func loadedPicker(t *testing.T, calls *int, repos []prb.Repo) RepoPickerModel {
	t.Helper()
	m := NewRepoPicker("ws", func() tea.Cmd { *calls++; return nil })
	m.Init()
	n, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	n, _ = n.Update(ReposLoadedMsg{Repos: repos})
	return n.(RepoPickerModel)
}

func TestRepoPickerLoadsAndChooses(t *testing.T) {
	var calls int
	m := NewRepoPicker("ws", func() tea.Cmd { calls++; return nil })
	m.Init()
	if calls != 1 {
		t.Fatalf("Init loaded %d times, want 1", calls)
	}
	if out := tuitest.Plain(m.render(80, 20)); !strings.Contains(out, "Cargando repos de ws…") {
		t.Errorf("loading:\n%s", out)
	}
	m = loadedPicker(t, &calls, sampleRepos())
	out := tuitest.Plain(m.render(80, 20))
	for _, want := range []string{"pr-review", "ws", "3 repos", "Elegí un repo", "api-pagos", "API de pagos", "stock", "web",
		"↑↓ moverse", "enter abrir", "/ filtrar", "r actualizar", "q salir"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Sorted by slug: api-pagos, stock, web.
	if strings.Index(out, "api-pagos") > strings.Index(out, "stock") || strings.Index(out, "stock") > strings.Index(out, "web") {
		t.Errorf("repos not sorted:\n%s", out)
	}
	checkWidth(t, m.render(80, 20), 80)
	if n := len(strings.Split(m.render(80, 20), "\n")); n != 20 {
		t.Errorf("render has %d lines, want 20", n)
	}

	m, _ = pickerKey(m, "down")
	_, msg := pickerKey(m, "enter")
	if got, ok := msg.(RepoChosenMsg); !ok || got.Repo != "ws/stock" {
		t.Errorf("enter emitted %#v, want RepoChosenMsg{ws/stock}", msg)
	}
	if _, msg := pickerKey(m, "q"); msg != (QuitMsg{}) {
		t.Errorf("q emitted %#v", msg)
	}
}

func TestRepoPickerWithoutFullName(t *testing.T) {
	var calls int
	m := loadedPicker(t, &calls, []prb.Repo{{Slug: "stock"}})
	if _, msg := pickerKey(m, "enter"); msg != (RepoChosenMsg{Repo: "ws/stock"}) {
		t.Errorf("enter emitted %#v", msg)
	}
}

func TestRepoPickerFilter(t *testing.T) {
	var calls int
	m := loadedPicker(t, &calls, sampleRepos())
	m, _ = pickerKey(m, "/")
	for _, k := range []string{"p", "a", "g"} {
		m, _ = pickerKey(m, k)
	}
	out := tuitest.Plain(m.render(80, 20))
	if !strings.Contains(out, "/ pag") || !strings.Contains(out, "api-pagos") || strings.Contains(out, "stock") {
		t.Errorf("filtering by pag:\n%s", out)
	}
	m, _ = pickerKey(m, "enter") // applies the filter
	if _, msg := pickerKey(m, "enter"); msg != (RepoChosenMsg{Repo: "ws/api-pagos"}) {
		t.Errorf("enter emitted %#v", msg)
	}
	// The name matches too.
	m, _ = pickerKey(m, "/")
	m, _ = pickerKey(m, "esc")
	m, _ = pickerKey(m, "/")
	for _, k := range []string{"A", "P", "I"} {
		m, _ = pickerKey(m, k)
	}
	if out := tuitest.Plain(m.render(80, 20)); !strings.Contains(out, "api-pagos") || strings.Contains(out, " web") {
		t.Errorf("filtering by name:\n%s", out)
	}
	m, _ = pickerKey(m, "esc")
	for _, k := range []string{"/", "z", "z", "enter"} {
		m, _ = pickerKey(m, k)
	}
	if out := tuitest.Plain(m.render(80, 20)); !strings.Contains(out, "Ningún repo coincide con el filtro.") {
		t.Errorf("no match:\n%s", out)
	}
	if _, msg := pickerKey(m, "enter"); msg != nil {
		t.Errorf("enter with nothing visible emitted %#v", msg)
	}
}

func TestRepoPickerErrors(t *testing.T) {
	var calls int
	m := loadedPicker(t, &calls, nil)
	if out := tuitest.Plain(m.render(80, 20)); !strings.Contains(out, "No hay repos en ws.") {
		t.Errorf("empty:\n%s", out)
	}
	n, _ := m.Update(ReposLoadedMsg{Err: &prb.Error{Code: "no_credentials", Message: "x"}})
	m = n.(RepoPickerModel)
	out := tuitest.Plain(m.render(80, 20))
	if !strings.Contains(out, "No hay sesión de Bitbucket. Corré prb auth login y apretá r.") {
		t.Errorf("no_credentials:\n%s", out)
	}
	n, _ = m.Update(ReposLoadedMsg{Err: fmt.Errorf("sin red")})
	m = n.(RepoPickerModel)
	out = tuitest.Plain(m.render(80, 20))
	if !strings.Contains(out, "sin red") || !strings.Contains(out, "r reintentar") {
		t.Errorf("error:\n%s", out)
	}
	before := calls
	m, _ = pickerKey(m, "r")
	if calls != before+1 {
		t.Errorf("r loaded %d times, want 1", calls-before)
	}
	if out := tuitest.Plain(m.render(80, 20)); !strings.Contains(out, "Cargando repos de ws…") {
		t.Errorf("reloading:\n%s", out)
	}
	if _, msg := pickerKey(m, "enter"); msg != nil {
		t.Errorf("enter while loading emitted %#v", msg)
	}
}

func TestRepoPickerScrolls(t *testing.T) {
	var repos []prb.Repo
	for i := range 30 {
		s := fmt.Sprintf("repo-%02d", i)
		repos = append(repos, prb.Repo{Slug: s, FullName: "ws/" + s})
	}
	var calls int
	m := loadedPicker(t, &calls, repos)
	for range 25 {
		m, _ = pickerKey(m, "down")
	}
	out := tuitest.Plain(m.render(80, 12))
	if n := len(strings.Split(out, "\n")); n != 12 {
		t.Errorf("render has %d lines, want 12:\n%s", n, out)
	}
	if !strings.Contains(out, "▌ repo-25") || strings.Contains(out, "repo-00") {
		t.Errorf("selection not in view:\n%s", out)
	}
	if !strings.Contains(out, "q salir") {
		t.Errorf("key help cut:\n%s", out)
	}
}
