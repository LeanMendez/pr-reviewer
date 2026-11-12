package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func load(t *testing.T, root string, changed []string, lim Limits) RuleSet {
	t.Helper()
	rs, err := Load(root, changed, lim)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return rs
}

func paths(rs RuleSet) []string {
	var out []string
	for _, f := range rs.Files {
		out = append(out, f.Path)
	}
	return out
}

func TestDefaultLimits(t *testing.T) {
	if got, want := DefaultLimits(), (Limits{4, 64 << 10, 256 << 10}); got != want {
		t.Errorf("DefaultLimits() = %+v, want %+v", got, want)
	}
}

func TestLoadRootAndNested(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "raíz")
	write(t, root, "src/CLAUDE.md", "src")
	write(t, root, "src/a/CLAUDE.md", "carpeta a")
	write(t, root, "src/b/CLAUDE.md", "carpeta b")

	rs := load(t, root, []string{"src/a/x.go", "docs/y.md"}, DefaultLimits())

	want := []string{"CLAUDE.md", "src/CLAUDE.md", "src/a/CLAUDE.md"}
	if got := paths(rs); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if strings.Contains(rs.Text, "carpeta b") {
		t.Errorf("src/b/CLAUDE.md loaded: %q", rs.Text)
	}
	if rs.Files[0].Bytes != len("raíz") {
		t.Errorf("Bytes = %d, want %d", rs.Files[0].Bytes, len("raíz"))
	}
	if len(rs.Skipped) != 0 {
		t.Errorf("skipped = %v", rs.Skipped)
	}
}

func TestFollowsReferences(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", strings.Join([]string{
		"Migraciones: @docs/reglas/migraciones.md.",
		"Validación: [ver](docs/reglas/validacion.md#entradas).",
		"Guías: [web](http://example.com/guia.md) y [segura](https://example.com/x.md).",
		"Ejemplo: `@docs/ejemplo.md` no es una referencia.",
		"```python",
		"@app.route('/docs/ejemplo.md')",
		"```",
		"Consultas a @juan o a ana@example.com.",
		"Falta: @docs/nope.md",
	}, "\n"))
	write(t, root, "docs/reglas/migraciones.md", "toda migración se puede revertir; ver [también](otra.md)")
	write(t, root, "docs/reglas/validacion.md", "todo endpoint valida su entrada")
	write(t, root, "docs/reglas/otra.md", "otra regla")
	write(t, root, "docs/ejemplo.md", "no se carga")

	rs := load(t, root, nil, DefaultLimits())

	want := []string{"CLAUDE.md", "docs/reglas/migraciones.md", "docs/reglas/validacion.md", "docs/reglas/otra.md"}
	if got := paths(rs); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	wantSkipped := map[string]string{"docs/nope.md": "no existe"}
	if !reflect.DeepEqual(rs.Skipped, wantSkipped) {
		t.Errorf("skipped = %v, want %v", rs.Skipped, wantSkipped)
	}
}

func TestDepthAndCycles(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "CLAUDE.md", "@n1.md")
		for i := 1; i <= 5; i++ {
			write(t, root, fmt.Sprintf("n%d.md", i), fmt.Sprintf("@n%d.md", i+1))
		}
		rs := load(t, root, nil, DefaultLimits())
		want := []string{"CLAUDE.md", "n1.md", "n2.md", "n3.md"}
		if got := paths(rs); !reflect.DeepEqual(got, want) {
			t.Errorf("files = %v, want %v", got, want)
		}
		if got := rs.Skipped["n4.md"]; got != "supera la profundidad máxima" {
			t.Errorf("skipped[n4.md] = %q (all: %v)", got, rs.Skipped)
		}
	})
	t.Run("cycle", func(t *testing.T) {
		root := t.TempDir()
		write(t, root, "CLAUDE.md", "@a.md")
		write(t, root, "a.md", "regla a @b.md")
		write(t, root, "b.md", "regla b @a.md y @CLAUDE.md")
		rs := load(t, root, nil, DefaultLimits())
		want := []string{"CLAUDE.md", "a.md", "b.md"}
		if got := paths(rs); !reflect.DeepEqual(got, want) {
			t.Errorf("files = %v, want %v", got, want)
		}
		if n := strings.Count(rs.Text, "regla a"); n != 1 {
			t.Errorf("a.md loaded %d times", n)
		}
	})
}

func TestRejectsOutsideBase(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	write(t, dir, "secreto.md", "SECRETO DE AFUERA")
	write(t, dir, "CLAUDE.md", "SECRETO DEL PADRE")
	write(t, root, "CLAUDE.md", "@../../etc/passwd y @docs/link.md y @/etc/hosts")
	write(t, root, "docs/README.md", "")
	if err := os.Symlink(filepath.Join(dir, "secreto.md"), filepath.Join(root, "docs", "link.md")); err != nil {
		t.Fatal(err)
	}

	rs := load(t, root, []string{"../x.go"}, DefaultLimits())

	want := map[string]string{
		"../../etc/passwd": "fuera del repo",
		"docs/link.md":     "fuera del repo",
		"/etc/hosts":       "fuera del repo",
	}
	if !reflect.DeepEqual(rs.Skipped, want) {
		t.Errorf("skipped = %v, want %v", rs.Skipped, want)
	}
	if strings.Contains(rs.Text, "SECRETO") {
		t.Errorf("outside file loaded: %q", rs.Text)
	}
	if got := paths(rs); !reflect.DeepEqual(got, []string{"CLAUDE.md"}) {
		t.Errorf("files = %v", got)
	}
}

func TestSizeLimits(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "@grande.md @b.md @c.md")
	write(t, root, "grande.md", strings.Repeat("x", 101))
	write(t, root, "b.md", strings.Repeat("b", 60))
	write(t, root, "c.md", strings.Repeat("c", 60))

	rs := load(t, root, nil, Limits{MaxDepth: 4, MaxFileBytes: 100, MaxTotalBytes: 100})

	if got := rs.Skipped["grande.md"]; got != "demasiado grande" {
		t.Errorf("skipped[grande.md] = %q", got)
	}
	if got := rs.Skipped["c.md"]; got != "supera el límite total" {
		t.Errorf("skipped[c.md] = %q", got)
	}
	if got := paths(rs); !reflect.DeepEqual(got, []string{"CLAUDE.md", "b.md"}) {
		t.Errorf("files = %v", got)
	}
}

func TestZeroLimitsUseDefaults(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "@a.md")
	write(t, root, "a.md", "regla")
	rs := load(t, root, nil, Limits{})
	if got := paths(rs); !reflect.DeepEqual(got, []string{"CLAUDE.md", "a.md"}) {
		t.Errorf("files = %v", got)
	}
}

func TestTextHasHeaders(t *testing.T) {
	root := t.TempDir()
	write(t, root, "CLAUDE.md", "regla raíz @docs/x.md\n")
	write(t, root, "docs/x.md", "regla de docs\n")

	rs := load(t, root, nil, DefaultLimits())

	for _, c := range []struct{ header, body string }{
		{"### Archivo: CLAUDE.md", "regla raíz"},
		{"### Archivo: docs/x.md", "regla de docs"},
	} {
		h, b := strings.Index(rs.Text, c.header), strings.Index(rs.Text, c.body)
		if h < 0 || b < 0 || h > b {
			t.Errorf("header %q (at %d) must precede %q (at %d) in:\n%s", c.header, h, c.body, b, rs.Text)
		}
	}
}

func TestLoadMissingBase(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "no-existe"), nil, DefaultLimits()); err == nil {
		t.Error("expected an error for a missing base directory")
	}
}
