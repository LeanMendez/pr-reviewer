// Package report writes a review to disk: review.md for the reader,
// findings.json with everything the pipeline decided, and the PR context and
// diff it was based on.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/LeanMendez/pr-review/internal/execx"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
)

// Dir is the folder of one review: <root>/<repo>/pr-<N>-<YYYYMMDD-HHMM>.
func Dir(reportsRoot, repo string, pr int, at time.Time) string {
	return filepath.Join(reportsRoot, repo, fmt.Sprintf("pr-%d-%s", pr, at.Format("20060102-1504")))
}

// ReportsRoot is <DOCUMENTS>/reviews, where DOCUMENTS comes from
// `xdg-user-dir DOCUMENTS` and falls back to ~/Documentos.
func ReportsRoot(exec execx.Exec) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := exec(ctx, execx.Cmd{Name: "xdg-user-dir", Args: []string{"DOCUMENTS"}, Stdout: &out}); err == nil {
		if docs := strings.TrimSpace(out.String()); docs != "" {
			return filepath.Join(docs, "reviews")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documentos", "reviews")
}

// Write saves the four files of a review into dir, creating it.
func Write(dir string, r pipeline.Result, rawContext []byte, d prb.Diff) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("crear %s: %w", dir, err)
	}
	findings, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("serializar findings.json: %w", err)
	}
	files := []struct {
		name string
		data []byte
	}{
		{"context.json", rawContext},
		{"pr.diff", []byte(unifiedDiff(d))},
		{"findings.json", append(findings, '\n')},
		{"review.md", []byte(Markdown(r))},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o644); err != nil {
			return fmt.Errorf("escribir %s: %w", f.name, err)
		}
	}
	return nil
}

// unifiedDiff concatenates the patches under `diff --git` headers.
func unifiedDiff(d prb.Diff) string {
	var b strings.Builder
	for _, f := range d.Files {
		old := f.OldPath
		if old == "" {
			old = f.Path
		}
		fmt.Fprintf(&b, "diff --git a/%s b/%s\n--- a/%s\n+++ b/%s\n", old, f.Path, old, f.Path)
		if f.Patch != "" {
			b.WriteString(f.Patch)
			if !strings.HasSuffix(f.Patch, "\n") {
				b.WriteByte('\n')
			}
		}
	}
	return b.String()
}
