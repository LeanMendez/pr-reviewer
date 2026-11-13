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
	"regexp"
	"strconv"
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
	if err := os.MkdirAll(dir, 0o700); err != nil {
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
		if err := os.WriteFile(filepath.Join(dir, f.name), f.data, 0o600); err != nil {
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

// reviewDirRe matches pr-<N>-<YYYYMMDD-HHMM>, exactly.
var reviewDirRe = regexp.MustCompile(`^pr-(\d+)-(\d{8}-\d{4})$`)

// latestDir finds the newest finished review of a PR. Only dirs with a
// findings.json count: the engine creates the dir before the agents run, so a
// failed or cancelled review leaves one without it.
func latestDir(root, repo string, pr int) (string, time.Time, bool) {
	entries, err := os.ReadDir(filepath.Join(root, repo))
	if err != nil {
		return "", time.Time{}, false
	}
	var (
		best     string
		bestTime time.Time
		found    bool
	)
	for _, e := range entries {
		m := reviewDirRe.FindStringSubmatch(e.Name())
		if m == nil || !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err != nil || n != pr {
			continue
		}
		at, err := time.ParseInLocation("20060102-1504", m[2], time.Local)
		if err != nil {
			continue
		}
		dir := filepath.Join(root, repo, e.Name())
		if _, err := os.Stat(filepath.Join(dir, "findings.json")); err != nil {
			continue
		}
		if !found || at.After(bestTime) {
			best, bestTime, found = dir, at, true
		}
	}
	return best, bestTime, found
}

// LastReview is the date of the newest finished review of the PR.
func LastReview(root, repo string, pr int) (time.Time, bool) {
	_, at, ok := latestDir(root, repo, pr)
	return at, ok
}

// LoadLatest reads the findings.json of the newest finished review of the PR
// and returns it with the path of that review's review.md.
func LoadLatest(root, repo string, pr int) (pipeline.Result, string, error) {
	dir, _, ok := latestDir(root, repo, pr)
	if !ok {
		return pipeline.Result{}, "", fmt.Errorf("no hay reportes de %s#%d en %s", repo, pr, root)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "findings.json"))
	if err != nil {
		return pipeline.Result{}, "", fmt.Errorf("leer findings.json: %w", err)
	}
	var r pipeline.Result
	if err := json.Unmarshal(raw, &r); err != nil {
		return pipeline.Result{}, "", fmt.Errorf("leer %s: %w", filepath.Join(dir, "findings.json"), err)
	}
	return r, filepath.Join(dir, "review.md"), nil
}
