//go:build smoke

package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/prompts"
)

func smokeRunner() Runner {
	return Runner{Exec: execx.Real(), Bin: "claude", Timeout: 3 * time.Minute, LogDir: os.TempDir()}
}

func TestRealClaudeSmoke(t *testing.T) {
	t.Run("simple", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "probe.txt"), []byte("hola-mundo-42"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := smokeRunner().Run(context.Background(), Spec{
			Name: "smoke-simple", Assignment: config.Assignment{Model: "haiku"},
			System: "Sos un lector de archivos. Respondé solo con el esquema pedido.",
			Prompt: "Leé probe.txt en el directorio actual y devolvé su contenido exacto en content.",
			Schema: `{"type":"object","properties":{"content":{"type":"string"}},"required":["content"]}`,
			Dir:    dir,
		}, func(a Activity) { t.Logf("activity: %s", a.Text) })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("output: %s", out)
		var v struct{ Content string }
		if err := json.Unmarshal(out, &v); err != nil || v.Content == "" {
			t.Fatalf("bad output %s: %v", out, err)
		}
	})
	t.Run("superior_sintesis_schema", func(t *testing.T) {
		out, err := smokeRunner().Run(context.Background(), Spec{
			Name: "smoke-sintesis", Assignment: config.Assignment{Model: "haiku"},
			System: prompts.System("superior_sintesis"),
			Prompt: "Es una prueba de conectividad. No leas nada. Devolvé en frase un saludo corto y division.necesaria=false.",
			Schema: prompts.Schema("superior_sintesis"),
			Dir:    t.TempDir(),
		}, func(a Activity) { t.Logf("activity: %s", a.Text) })
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("output: %s", out)
	})
}
