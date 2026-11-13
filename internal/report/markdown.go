package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/pipeline"
)

// maxReasonRunes caps a failed agent's reason in the footer.
const maxReasonRunes = 200

var verdictNames = map[finding.Verdict]string{
	finding.VerdictAprobar:               "Aprobar",
	finding.VerdictAprobarConComentarios: "Aprobar con comentarios",
	finding.VerdictPedirCambios:          "Pedir cambios",
	finding.VerdictRevisarAMano:          "Revisar a mano",
}

var severityNames = map[finding.Severity]string{
	finding.SeverityCritico: "Crítico",
	finding.SeverityAlto:    "Alto",
	finding.SeverityMedio:   "Medio",
	finding.SeverityBajo:    "Bajo",
}

var severityOrder = map[finding.Severity]int{
	finding.SeverityCritico: 0,
	finding.SeverityAlto:    1,
	finding.SeverityMedio:   2,
	finding.SeverityBajo:    3,
}

// Sections splits the findings by where review.md shows them. Filtered ones
// (Group B without a Group A anchor) are in none: they live in findings.json.
type Sections struct {
	Introduced, Minor, Preexisting, Discarded []finding.Finding
}

func Split(fs []finding.Finding) Sections {
	var s Sections
	for _, f := range fs {
		switch f.Status {
		case finding.StatusRefutado, finding.StatusDescartadoPorJueces:
			s.Discarded = append(s.Discarded, f)
		case finding.StatusFiltrado:
			// only in findings.json
		default:
			// confirmado, sin_confirmar, menor, and anything unvalidated
			// (candidato, unknown): never dropped silently.
			switch {
			case f.Origin == finding.OriginPreexistente:
				s.Preexisting = append(s.Preexisting, f)
			case f.Status == finding.StatusMenor:
				s.Minor = append(s.Minor, f)
			default:
				s.Introduced = append(s.Introduced, f)
			}
		}
	}
	bySeverity := func(a, b finding.Finding) int {
		return cmp.Or(
			cmp.Compare(sevRank(a.Severity), sevRank(b.Severity)),
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
		)
	}
	slices.SortStableFunc(s.Introduced, bySeverity)
	slices.SortStableFunc(s.Minor, bySeverity)
	slices.SortStableFunc(s.Preexisting, bySeverity)
	return s
}

// VerdictLine is the verdict sentence: the verdict name in bold, the
// synthesis phrase (or a phrase built from the counts when the synthesis
// failed) and, whenever the review is incomplete, a clause Go writes after
// the phrase so no phrase can contradict it. Model text is Markdown-escaped.
func VerdictLine(r pipeline.Result) string {
	return verdictSentence(r, func(name string) string { return "**" + name + ".**" }, inline)
}

// VerdictText is the same sentence as VerdictLine in plain text, for the
// terminal and notifications: no bold and no Markdown escaping.
func VerdictText(r pipeline.Result) string {
	return verdictSentence(r, func(name string) string { return name + "." }, func(s string) string { return s })
}

// verdictSentence holds the sentence rules shared by VerdictLine and
// VerdictText; head formats the verdict name and esc the model phrase.
func verdictSentence(r pipeline.Result, head, esc func(string) string) string {
	var b strings.Builder
	name := verdictNames[r.Verdict]
	if name == "" {
		name = string(r.Verdict)
	}
	b.WriteString(head(name))
	clause := incompleteClause(r)
	if r.IncompleteWarning && clause == "" {
		b.WriteString(" Revisión incompleta.")
	}
	phrase := oneLine(r.Phrase)
	if phrase == "" {
		phrase = fallbackPhrase(r)
	}
	b.WriteString(" " + esc(phrase))
	if clause != "" {
		b.WriteString(" " + clause)
	}
	return b.String()
}

// incompleteClause says what the review did not cover, with counts, or ""
// when it covered everything.
func incompleteClause(r pipeline.Result) string {
	failed := map[string]bool{}
	for _, f := range r.Failures {
		failed[f.Agent] = true
	}
	for _, a := range r.FailedAgents {
		failed[a] = true
	}
	var parts []string
	switch n := len(failed); {
	case n == 1:
		parts = append(parts, "1 agente falló")
	case n > 1:
		parts = append(parts, fmt.Sprintf("%d agentes fallaron", n))
	}
	switch n := len(r.Unreviewed); {
	case n == 1:
		parts = append(parts, "1 archivo sin revisar")
	case n > 1:
		parts = append(parts, fmt.Sprintf("%d archivos sin revisar", n))
	}
	if len(r.Gaps) > 0 {
		parts = append(parts, "no se pudo comprobar que el diff esté completo")
	}
	if len(parts) == 0 {
		return ""
	}
	return "Revisión incompleta: " + strings.Join(parts, ", ") + "."
}

// fallbackPhrase says what the counts support when the synthesis agent
// failed: how many confirmed problems of the worst severity the PR introduces.
func fallbackPhrase(r pipeline.Result) string {
	counts := map[finding.Severity]int{}
	for _, f := range Split(r.Findings).Introduced {
		if f.Status == finding.StatusConfirmado && f.Origin.FromPR() {
			counts[f.Severity]++
		}
	}
	incomplete := incompleteClause(r) != ""
	var lead string
	switch n := counts[finding.SeverityCritico]; {
	case n == 1:
		lead = "1 problema crítico que introduce este PR."
	case n > 1:
		lead = fmt.Sprintf("%d problemas críticos que introduce este PR.", n)
	case counts[finding.SeverityAlto] == 1:
		lead = "1 problema alto que introduce este PR."
	case counts[finding.SeverityAlto] > 1:
		lead = fmt.Sprintf("%d problemas altos que introduce este PR.", counts[finding.SeverityAlto])
	default:
		if n := counts[finding.SeverityMedio] + counts[finding.SeverityBajo]; n == 1 {
			lead = "1 problema menor que introduce este PR."
		} else if n > 1 {
			lead = fmt.Sprintf("%d problemas menores que introduce este PR.", n)
		} else {
			lead = "No hay problemas confirmados que introduzca este PR."
			if incomplete {
				lead = "No hay problemas confirmados, pero la revisión quedó incompleta."
			}
		}
	}
	return lead + " La síntesis no está disponible; revisá los hallazgos abajo."
}

// Markdown renders review.md. Empty sections are left out.
func Markdown(r pipeline.Result) string {
	s := Split(r.Findings)
	var b strings.Builder

	fmt.Fprintf(&b, "# #%d %s\n\n", r.PR.ID, inline(oneLine(r.PR.Title)))
	if r.PR.URL != "" {
		fmt.Fprintf(&b, "%s\n\n", oneLine(r.PR.URL))
	}
	b.WriteString(VerdictLine(r) + "\n")

	if len(s.Introduced) > 0 {
		b.WriteString("\n## Lo que introduce este PR\n")
		for _, f := range s.Introduced {
			writeFinding(&b, f)
		}
	}
	if len(s.Minor) > 0 {
		b.WriteString("\n## Detalles menores y preguntas de diseño\n\n")
		for _, f := range s.Minor {
			fmt.Fprintf(&b, "- %s %s\n", code(loc(f)), inline(oneLine(f.What)))
		}
	}
	if len(s.Preexisting) > 0 {
		b.WriteString("\n## Ya existía antes de este PR\n\n")
		b.WriteString("El PR toca código que ya tenía estos problemas. Sirven de contexto y no cuentan para el veredicto.\n\n")
		for _, f := range s.Preexisting {
			fmt.Fprintf(&b, "- %s %s\n", code(loc(f)), inline(oneLine(f.What)))
		}
	}
	if len(r.Doubts) > 0 {
		b.WriteString("\n## Dudas\n\n")
		for _, d := range r.Doubts {
			fmt.Fprintf(&b, "- %s %s\n", code(fileLine(d.File, d.Line)), inline(oneLine(d.Text)))
		}
	}
	if len(r.Split) > 0 {
		b.WriteString("\n## Cómo partir este PR\n\n")
		for i, p := range r.Split {
			fmt.Fprintf(&b, "%d. %s (%s, %d líneas)", i+1, inline(oneLine(p.Title)), inline(oneLine(strings.Join(p.Files, ", "))), p.Lines)
			if p.DependsOn > 0 {
				fmt.Fprintf(&b, ": depende de la %d, se mergea después", p.DependsOn)
			}
			b.WriteString("\n")
		}
	}
	writeFooter(&b, r, s)
	return b.String()
}

func writeFinding(b *strings.Builder, f finding.Finding) {
	fmt.Fprintf(b, "\n### %s %s", sevName(f.Severity), code(loc(f)))
	switch {
	case f.Status != finding.StatusConfirmado && f.Status != finding.StatusSinConfirmar:
		b.WriteString(" · **Sin validar**")
	case f.Status == finding.StatusSinConfirmar:
		b.WriteString(" · **Sin confirmar**")
	case f.Origin == finding.OriginIncierto:
		b.WriteString(" · **Origen incierto**")
	}
	b.WriteString("\n\n")
	b.WriteString(prose(f.What) + "\n")
	if f.Status != finding.StatusConfirmado && f.Status != finding.StatusSinConfirmar {
		b.WriteString("\nNo pasó por la validación: tomalo como una sospecha y revisalo vos.\n")
	}
	if f.Status == finding.StatusSinConfirmar {
		b.WriteString("\nNo se pudo validar: tomalo como una sospecha y revisalo vos.\n")
	}
	if f.Origin == finding.OriginIncierto {
		b.WriteString("\nNo está claro si el PR lo introdujo o ya estaba.\n")
	}
	if strings.TrimSpace(f.Why) != "" {
		fmt.Fprintf(b, "\nPor qué pasa: %s\n", prose(f.Why))
	}
	if strings.TrimSpace(f.Fix) != "" {
		b.WriteString("\n")
		if f.Rule != nil {
			fmt.Fprintf(b, "Solución, según %s (\"%s\"):", code(f.Rule.File), inline(oneLine(f.Rule.Quote)))
		} else {
			b.WriteString("Solución:")
		}
		fix := strings.TrimSpace(f.Fix)
		if strings.Contains(fix, "\n") {
			fence := strings.Repeat("`", max(3, longestRun(fix, '`')+1))
			fmt.Fprintf(b, "\n\n%s\n%s\n%s\n", fence, fix, fence)
		} else {
			fmt.Fprintf(b, " %s\n", inline(fix))
		}
	}
	if f.FixStatus == finding.FixStatusARevisar {
		b.WriteString("\nLos jueces marcaron esta solución para revisar: puede chocar con una regla del repo.\n")
	}
	if len(f.Votes) > 0 {
		fmt.Fprintf(b, "\nJueces: %s\n", judges(f.Votes))
	}
}

// judges summarizes what each judge said, in one sentence.
func judges(votes []finding.JudgeVote) string { return judgesSentence(votes, inline) }

// JudgesText is the same sentence as plain text, for the terminal.
func JudgesText(votes []finding.JudgeVote) string {
	return judgesSentence(votes, func(s string) string { return s })
}

func judgesSentence(votes []finding.JudgeVote, esc func(string) string) string {
	parts := make([]string, 0, len(votes))
	for _, v := range votes {
		var say string
		switch {
		case !v.Real:
			say = "no lo confirma"
		case !v.Worth:
			say = "lo ve menor"
		default:
			say = "lo confirma"
		}
		parts = append(parts, fmt.Sprintf("%s %s", esc(oneLine(v.Judge)), say))
	}
	return strings.Join(parts, "; ") + "."
}

func writeFooter(b *strings.Builder, r pipeline.Result, s Sections) {
	b.WriteString("\n## Al pie\n")
	if len(s.Discarded) > 0 {
		fmt.Fprintf(b, "\nDescartados: %d.\n\n", len(s.Discarded))
		for _, f := range s.Discarded {
			reason := inline(oneLine(f.Reason))
			if reason == "" {
				reason = "sin motivo registrado"
			}
			fmt.Fprintf(b, "- %s %s Motivo: %s\n", code(loc(f)), inline(oneLine(f.What)), reason)
		}
	}
	if len(r.Unreviewed) > 0 {
		b.WriteString("\nSin revisar:\n\n")
		for _, p := range r.Unreviewed {
			fmt.Fprintf(b, "- %s\n", inline(oneLine(p)))
		}
	}
	if len(r.Gaps) > 0 {
		b.WriteString("\nNo se pudo comprobar que el diff esté completo:\n\n")
		for _, g := range r.Gaps {
			fmt.Fprintf(b, "- %s\n", inline(oneLine(g)))
		}
	}
	if len(r.Excluded) > 0 {
		fmt.Fprintf(b, "\nExcluidos de la revisión (generados): %s.\n", inline(oneLine(strings.Join(r.Excluded, ", "))))
	}
	if lines := failureLines(r); len(lines) > 0 {
		b.WriteString("\nAgentes que fallaron (lo que cubrían no se revisó del todo):\n\n")
		for _, l := range lines {
			b.WriteString("- " + l + "\n")
		}
	}
	b.WriteString("\n")
	if len(r.RuleFiles) == 0 {
		b.WriteString("Reglas usadas: ninguna encontrada.\n")
	} else {
		paths := make([]string, len(r.RuleFiles))
		for i, f := range r.RuleFiles {
			paths[i] = inline(oneLine(f.Path))
		}
		fmt.Fprintf(b, "Reglas usadas: %s.\n", strings.Join(paths, ", "))
	}
	fmt.Fprintf(b, "Modo: %s, %d agentes%s.\n", r.Mode, r.AgentCount, duration(r))
}

// failureLines is one line per failed agent: its role, what it covered and
// why it failed. Agents in FailedAgents without a Failure still show up.
func failureLines(r pipeline.Result) []string {
	var lines []string
	seen := map[string]bool{}
	for _, f := range r.Failures {
		seen[f.Agent] = true
		l := fmt.Sprintf("%s (rol %s)", code(f.Agent), inline(oneLine(f.Role)))
		var covered string
		switch {
		case len(f.Files) > 0:
			covered = inline(oneLine(strings.Join(f.Files, ", ")))
		case f.Candidate != "":
			covered = "candidato " + inline(oneLine(f.Candidate))
		}
		if covered != "" {
			l += ": " + covered
		}
		if reason := truncate(firstLine(f.Reason), maxReasonRunes); reason != "" {
			l += ". Motivo: " + inline(reason)
		}
		lines = append(lines, l)
	}
	for _, name := range r.FailedAgents {
		if !seen[name] {
			lines = append(lines, code(name))
		}
	}
	return lines
}

func duration(r pipeline.Result) string {
	if r.Started.IsZero() || r.Finished.Before(r.Started) {
		return ""
	}
	d := r.Finished.Sub(r.Started).Round(time.Second)
	if m := int(d / time.Minute); m > 0 {
		return fmt.Sprintf(", %d min %d s", m, int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf(", %d s", int(d/time.Second))
}

func loc(f finding.Finding) string { return fileLine(f.File, f.Line) }

func fileLine(file string, line int) string {
	file = oneLine(file)
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func sevRank(s finding.Severity) int {
	if n, ok := severityOrder[s]; ok {
		return n
	}
	return len(severityOrder)
}

func sevName(s finding.Severity) string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return inline(oneLine(string(s)))
}

// code renders s as one inline code span whose delimiter is longer than any
// backtick run inside it, so it cannot be closed early.
func code(s string) string {
	s = oneLine(s)
	d := strings.Repeat("`", longestRun(s, '`')+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return d + s + d
}

// inline neutralizes the Markdown of single-line text: backslashes, so none
// can cancel the escapes, and every character that opens a code span,
// emphasis, a link or raw HTML. One pass, so nothing is escaped twice.
func inline(s string) string { return inlineEscaper.Replace(s) }

var inlineEscaper = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`,
)

// prose cleans free multi-line text: each line loses its indentation, has its
// backticks escaped and, when it would start a heading, quote, list, rule or
// fence, a backslash in front, so it stays a plain paragraph line.
func prose(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, l := range lines {
		l = inline(strings.TrimSpace(l))
		if l != "" && (strings.ContainsRune("#>-*+=~|_", rune(l[0])) || startsOrdered(l)) {
			l = "\\" + l
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

func startsOrdered(l string) bool {
	i := 0
	for i < len(l) && l[i] >= '0' && l[i] <= '9' {
		i++
	}
	return i > 0 && i < len(l) && (l[i] == '.' || l[i] == ')')
}

func longestRun(s string, c byte) int {
	best, cur := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			cur++
			best = max(best, cur)
		} else {
			cur = 0
		}
	}
	return best
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// CommentText is a finding written to paste into a Bitbucket comment: what
// happens, why and the fix in a code block. Model and PR text is sanitized
// like review.md's, since Bitbucket renders it as Markdown.
func CommentText(f finding.Finding) string {
	var b strings.Builder
	b.WriteString(prose(f.What) + "\n")
	if strings.TrimSpace(f.Why) != "" {
		fmt.Fprintf(&b, "\nPor qué pasa: %s\n", prose(f.Why))
	}
	if fix := strings.TrimSpace(f.Fix); fix != "" {
		if f.Rule != nil {
			fmt.Fprintf(&b, "\nSolución, según %s (\"%s\"):\n", code(f.Rule.File), inline(oneLine(f.Rule.Quote)))
		} else {
			b.WriteString("\nSolución:\n")
		}
		fence := strings.Repeat("`", max(3, longestRun(fix, '`')+1))
		fmt.Fprintf(&b, "\n%s\n%s\n%s\n", fence, fix, fence)
	}
	return b.String()
}
