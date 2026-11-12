// Package prechecks runs the deterministic (non-AI) checks of stage 1.
package prechecks

import (
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
			r.TotalLines += f.LinesAdded + f.LinesRemoved
			r.Findings = append(r.Findings, scanSecrets(f)...)
			if f.PatchOmitted {
				r.Unreviewed = append(r.Unreviewed, f.Path)
				seen[f.Path] = true
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
		r.Unreviewed = append(r.Unreviewed, p)
	}
	r.Oversize = oversize(r.TotalLines, files, cfg)
	r.Findings = append(r.Findings, processFindings(pr, r.Oversize, cfg)...)
	return r
}

// Summarize fills Oversize, TotalLines, Sensitive and Excluded from a
// diffstat, without the diff.
func Summarize(files []prb.FileStat, cfg config.Config) Result {
	var r Result
	n := 0
	for _, f := range files {
		if IsGenerated(f.Path) {
			r.Excluded = append(r.Excluded, f.Path)
		} else {
			n++
			r.TotalLines += f.LinesAdded + f.LinesRemoved
		}
		if IsSensitive(f.Path) {
			r.Sensitive = append(r.Sensitive, f.Path)
		}
	}
	r.Oversize = oversize(r.TotalLines, n, cfg)
	return r
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
	if oversize && nonSpace(pr.Description) < minDescriptionChars {
		out = append(out, processFinding(
			"El PR es grande y tiene la descripción vacía o casi vacía.",
			"Un PR grande sin contexto es difícil de revisar y de entender después.",
			"Completá la descripción con qué cambia, por qué y cómo probarlo, o dividí el PR.",
			"descripción del PR"))
	}
	return out
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
