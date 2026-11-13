package report

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/prb"
)

// maxFragmentLines bounds the diff fragment of AgentText, so one huge hunk
// cannot flood the clipboard.
const maxFragmentLines = 120

// AgentText is a finding with the technical context an AI agent needs to
// diagnose it: the PR with its branches and commits, the location, what
// happens and why, the proposed fix, the rule, the evidence, the judges'
// votes and, when diff (a saved pr.diff) is given, its hunks that touch the
// finding's lines. Unlike CommentText it is meant for agents, not people.
func AgentText(r pipeline.Result, f finding.Finding, diff string) string {
	var b strings.Builder
	b.WriteString("# Hallazgo de pr-review\n\n")
	fmt.Fprintf(&b, "PR: #%d %s\n", r.PR.ID, oneLine(r.PR.Title))
	if u := strings.TrimSpace(r.PR.URL); u != "" {
		fmt.Fprintf(&b, "URL: %s\n", u)
	}
	if r.Repo != "" {
		fmt.Fprintf(&b, "Repo: %s\n", r.Repo)
	}
	fmt.Fprintf(&b, "Rama origen: %s\n", refLine(r.PR.Source))
	fmt.Fprintf(&b, "Rama destino: %s\n", refLine(r.PR.Destination))
	if r.Mode != "" {
		fmt.Fprintf(&b, "Modo de revisión: %s\n", r.Mode)
	}

	b.WriteString("\n## Hallazgo\n\n")
	fmt.Fprintf(&b, "Ubicación: %s\n", agentLocation(f))
	if fn := oneLine(f.Function); fn != "" {
		fmt.Fprintf(&b, "Función: %s\n", fn)
	}
	fmt.Fprintf(&b, "Severidad: %s\nOrigen: %s\nEstado: %s\n", f.Severity, f.Origin, f.Status)
	if f.Role != "" {
		fmt.Fprintf(&b, "Especialista: %s\n", f.Role)
	}
	agentSection(&b, "Qué pasa", f.What)
	agentSection(&b, "Por qué pasa", f.Why)
	if fix := strings.TrimSpace(f.Fix); fix != "" {
		b.WriteString("\n### Solución propuesta\n\n")
		fenced(&b, "", fix)
	}
	if f.Rule != nil {
		fmt.Fprintf(&b, "\n### Regla del repo\n\n%s: \"%s\"\n", oneLine(f.Rule.File), oneLine(f.Rule.Quote))
	}
	if len(f.Evidence) > 0 {
		b.WriteString("\n### Evidencia\n\n")
		for _, e := range f.Evidence {
			fmt.Fprintf(&b, "- %s\n", oneLine(e))
		}
	}
	if len(f.Votes) > 0 {
		b.WriteString("\n### Jueces\n\n")
		for _, v := range f.Votes {
			fmt.Fprintf(&b, "- Juez %s: real=%s, vale la pena=%s, la solución cumple=%s",
				oneLine(v.Judge), yesNo(v.Real), yesNo(v.Worth), yesNo(v.FixComplies))
			if rc := oneLine(v.RuleCited); rc != "" {
				fmt.Fprintf(&b, "; regla citada: %s", rc)
			}
			if alt := oneLine(v.AlternativeFix); alt != "" {
				fmt.Fprintf(&b, "; solución alternativa: %s", alt)
			}
			if len(v.Evidence) > 0 {
				fmt.Fprintf(&b, "; evidencia: %s", oneLine(strings.Join(v.Evidence, ", ")))
			}
			b.WriteString("\n")
		}
	}
	if frag := DiffFragment(diff, f.File, f.Line, f.EndLine); frag != "" {
		b.WriteString("\n### Fragmento del diff\n\n")
		fenced(&b, "diff", frag)
	}
	return b.String()
}

func refLine(r prb.Ref) string {
	switch {
	case r.Branch != "" && r.Commit != "":
		return r.Branch + " @ " + r.Commit
	case r.Branch != "":
		return r.Branch
	case r.Commit != "":
		return "@ " + r.Commit
	}
	return "(desconocida)"
}

func agentLocation(f finding.Finding) string {
	if f.Line > 0 && f.EndLine > f.Line {
		return fmt.Sprintf("%s:%d-%d", oneLine(f.File), f.Line, f.EndLine)
	}
	return fileLine(f.File, f.Line)
}

func agentSection(b *strings.Builder, title, text string) {
	if text = strings.TrimSpace(text); text != "" {
		fmt.Fprintf(b, "\n### %s\n\n%s\n", title, text)
	}
}

// fenced writes text in a code fence longer than any backtick run inside it.
func fenced(b *strings.Builder, lang, text string) {
	fence := strings.Repeat("`", max(3, longestRun(text, '`')+1))
	fmt.Fprintf(b, "%s%s\n%s\n%s\n", fence, lang, strings.TrimRight(text, "\n"), fence)
}

func yesNo(v bool) string {
	if v {
		return "sí"
	}
	return "no"
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// DiffFragment returns the hunks of a unified diff (a saved pr.diff) for
// file that touch the lines line..endLine of the new version, or every hunk
// of the file when line is 0. It returns "" when the file is not in the diff
// or no hunk touches those lines, and cuts the result at maxFragmentLines.
// A section may repeat its header (pr.diff adds one before prb's own).
func DiffFragment(diff, file string, line, endLine int) string {
	if diff == "" || file == "" {
		return ""
	}
	endLine = max(endLine, line)
	var (
		out, hunk []string
		cur       string // file of the current section
		inHunk    bool
		keep      bool
	)
	flush := func() {
		if keep {
			out = append(out, hunk...)
		}
		hunk, keep = nil, false
	}
	for _, l := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			flush()
			inHunk = false
			if i := strings.LastIndex(l, " b/"); i >= 0 {
				cur = l[i+len(" b/"):]
			}
		case strings.HasPrefix(l, "@@"):
			flush()
			inHunk = true
			hunk = []string{l}
			keep = cur == file && (line <= 0 || touches(l, line, endLine))
		case inHunk:
			hunk = append(hunk, l)
		case strings.HasPrefix(l, "+++ b/"):
			cur = strings.TrimPrefix(l, "+++ b/")
		}
	}
	flush()
	if len(out) > maxFragmentLines {
		cut := len(out) - maxFragmentLines
		out = append(out[:maxFragmentLines], fmt.Sprintf("… (fragmento recortado: %d líneas más)", cut))
	}
	return strings.Join(out, "\n")
}

// touches says whether the new-version range of the hunk header h overlaps
// lines from..to. A hunk with no new lines counts as its start line.
func touches(h string, from, to int) bool {
	m := hunkHeader.FindStringSubmatch(h)
	if m == nil {
		return false
	}
	start, _ := strconv.Atoi(m[1])
	count := 1
	if m[2] != "" {
		count, _ = strconv.Atoi(m[2])
	}
	end := start + max(count, 1) - 1
	return start <= to && from <= end
}
