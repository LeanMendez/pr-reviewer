// Package prechecks runs the deterministic (non-AI) checks of stage 1.
package prechecks

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/prb"
)

const minDescriptionChars = 30

// Result is the outcome of the deterministic checks.
type Result struct {
	Findings   []finding.Finding
	Sensitive  []string
	Excluded   []string
	Unreviewed []string
	Changed    []string // every changed path that is not generated
	Oversize   bool
	TotalLines int
}

// Run applies every check to the PR and its diff.
func Run(pr prb.PR, d prb.Diff, cfg config.Config) Result {
	var r Result
	files := 0
	seen := map[string]bool{}
	for _, f := range d.Files {
		if IsGenerated(f.Path) || f.Binary {
			r.Excluded = append(r.Excluded, f.Path)
		} else {
			files++
			r.Changed = append(r.Changed, f.Path)
			r.TotalLines += f.LinesAdded + f.LinesRemoved
			r.Findings = append(r.Findings, scanSecrets(f)...)
			if PatchGap(f) != "" {
				r.Unreviewed = append(r.Unreviewed, f.Path)
			}
		}
		if IsSensitive(f.Path) {
			r.Sensitive = append(r.Sensitive, f.Path)
		}
		seen[f.Path] = true
	}
	for _, p := range d.OmittedFiles {
		if seen[p] {
			continue
		}
		seen[p] = true
		if IsSensitive(p) {
			r.Sensitive = append(r.Sensitive, p)
		}
		if IsGenerated(p) {
			r.Excluded = append(r.Excluded, p)
			continue
		}
		files++
		r.Changed = append(r.Changed, p)
		r.Unreviewed = append(r.Unreviewed, p)
	}
	r.Oversize = oversize(r.TotalLines, files, cfg)
	r.Findings = append(r.Findings, processFindings(pr, r.Oversize, cfg)...)
	return r
}

// Summarize fills Oversize, TotalLines, Sensitive, Excluded and Changed from
// a diffstat, without the diff.
func Summarize(files []prb.FileStat, cfg config.Config) Result {
	var r Result
	n := 0
	for _, f := range files {
		if IsGenerated(f.Path) {
			r.Excluded = append(r.Excluded, f.Path)
		} else {
			n++
			r.TotalLines += f.LinesAdded + f.LinesRemoved
			r.Changed = append(r.Changed, f.Path)
		}
		if IsSensitive(f.Path) {
			r.Sensitive = append(r.Sensitive, f.Path)
		}
	}
	r.Oversize = oversize(r.TotalLines, n, cfg)
	return r
}

// PatchGap says why the diff does not carry f's whole patch, or "" when it
// does: prb omitted or cut it, or sent it empty although lines changed. A
// file with a gap was not fully reviewed. Binary files never have a patch.
func PatchGap(f prb.DiffFile) string {
	switch {
	case f.Binary:
		return ""
	case f.PatchOmitted:
		return "el diff no trajo su patch"
	case f.OmittedLines > 0:
		return fmt.Sprintf("patch recortado: faltan %d líneas", f.OmittedLines)
	case f.Truncated:
		return "patch recortado"
	case f.Patch == "" && f.LinesAdded+f.LinesRemoved > 0:
		return "patch vacío aunque cambió líneas"
	}
	return ""
}

func oversize(lines, files int, cfg config.Config) bool {
	return lines > cfg.Thresholds.MaxLines || files > cfg.Thresholds.MaxFiles
}

func processFindings(pr prb.PR, oversize bool, cfg config.Config) []finding.Finding {
	var out []finding.Finding
	re, err := regexp.Compile(cfg.TaskIDPattern)
	if err != nil || cfg.TaskIDPattern == "" {
		re = regexp.MustCompile(config.Defaults().TaskIDPattern)
	}
	if !re.MatchString(strings.TrimSpace(pr.Title)) {
		out = append(out, processFinding(
			"El título no empieza con el ID de la tarea.",
			"Sin el ID no se puede enlazar el PR con su tarea ni rastrear el cambio.",
			"Renombrá el PR para que empiece con el ID de la tarea, por ejemplo `CU-123 Descripción`.",
			"título del PR: "+pr.Title))
	}
	if oversize {
		out = append(out, OversizeFindings(pr)...)
	}
	return out
}

// OversizeFindings are the process findings of a PR over the size
// threshold: a missing or minimal description. Run adds them; a caller that
// learns the PR is oversize only from the diffstat adds them itself.
func OversizeFindings(pr prb.PR) []finding.Finding {
	if nonSpace(pr.Description) >= minDescriptionChars {
		return nil
	}
	return []finding.Finding{processFinding(
		"El PR es grande y tiene la descripción vacía o casi vacía.",
		"Un PR grande sin contexto es difícil de revisar y de entender después.",
		"Completá la descripción con qué cambia, por qué y cómo probarlo, o dividí el PR.",
		"descripción del PR")}
}

func processFinding(what, why, fix, evidence string) finding.Finding {
	return finding.Finding{
		Role:     "prechecks",
		Severity: finding.SeverityBajo,
		Origin:   finding.OriginIntroducido,
		Group:    finding.GroupA,
		What:     what,
		Why:      why,
		Fix:      fix,
		Evidence: []string{evidence},
		Status:   finding.StatusConfirmado,
	}
}

func nonSpace(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
