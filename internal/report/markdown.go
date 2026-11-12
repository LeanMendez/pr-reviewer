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

// sections splits the findings by where review.md shows them. Filtered ones
// (Group B without a Group A anchor) are in none: they live in findings.json.
type sections struct {
	introduced, minor, preexisting, discarded []finding.Finding
}

func split(fs []finding.Finding) sections {
	var s sections
	for _, f := range fs {
		switch f.Status {
		case finding.StatusRefutado, finding.StatusDescartadoPorJueces:
			s.discarded = append(s.discarded, f)
		case finding.StatusConfirmado, finding.StatusSinConfirmar, finding.StatusMenor:
			switch {
			case f.Origin == finding.OriginPreexistente:
				s.preexisting = append(s.preexisting, f)
			case f.Status == finding.StatusMenor:
				s.minor = append(s.minor, f)
			default:
				s.introduced = append(s.introduced, f)
			}
		}
	}
	bySeverity := func(a, b finding.Finding) int {
		return cmp.Or(
			cmp.Compare(severityOrder[a.Severity], severityOrder[b.Severity]),
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
		)
	}
	slices.SortStableFunc(s.introduced, bySeverity)
	slices.SortStableFunc(s.minor, bySeverity)
	slices.SortStableFunc(s.preexisting, bySeverity)
	return s
}

// VerdictLine is the verdict sentence: the verdict name in bold, the
// incomplete-review warning when it applies, and the synthesis phrase (or a
// phrase built from the counts when the synthesis failed).
func VerdictLine(r pipeline.Result) string {
	var b strings.Builder
	name := verdictNames[r.Verdict]
	if name == "" {
		name = string(r.Verdict)
	}
	fmt.Fprintf(&b, "**%s.**", name)
	if r.IncompleteWarning {
		b.WriteString(" Revisión incompleta.")
	}
	phrase := strings.TrimSpace(r.Phrase)
	if phrase == "" {
		phrase = fallbackPhrase(r)
	}
	b.WriteString(" " + phrase)
	return b.String()
}

// fallbackPhrase says what the counts support when the synthesis agent
// failed: how many confirmed problems of the worst severity the PR introduces.
func fallbackPhrase(r pipeline.Result) string {
	counts := map[finding.Severity]int{}
	for _, f := range split(r.Findings).introduced {
		if f.Status == finding.StatusConfirmado && f.Origin.FromPR() {
			counts[f.Severity]++
		}
	}
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
		}
	}
	return lead + " La síntesis no está disponible; revisá los hallazgos abajo."
}

// Markdown renders review.md. Empty sections are left out.
func Markdown(r pipeline.Result) string {
	s := split(r.Findings)
	var b strings.Builder

	fmt.Fprintf(&b, "# #%d %s\n\n", r.PR.ID, r.PR.Title)
	if r.PR.URL != "" {
		fmt.Fprintf(&b, "%s\n\n", r.PR.URL)
	}
	b.WriteString(VerdictLine(r) + "\n")

	if len(s.introduced) > 0 {
		b.WriteString("\n## Lo que introduce este PR\n")
		for _, f := range s.introduced {
			writeFinding(&b, f)
		}
	}
	if len(s.minor) > 0 {
		b.WriteString("\n## Detalles menores y preguntas de diseño\n\n")
		for _, f := range s.minor {
			fmt.Fprintf(&b, "- `%s` %s\n", loc(f), oneLine(f.What))
		}
	}
	if len(s.preexisting) > 0 {
		b.WriteString("\n## Ya existía antes de este PR\n\n")
		b.WriteString("El PR toca código que ya tenía estos problemas. Sirven de contexto y no cuentan para el veredicto.\n\n")
		for _, f := range s.preexisting {
			fmt.Fprintf(&b, "- `%s` %s\n", loc(f), oneLine(f.What))
		}
	}
	if len(r.Doubts) > 0 {
		b.WriteString("\n## Dudas\n\n")
		for _, d := range r.Doubts {
			fmt.Fprintf(&b, "- `%s` %s\n", fileLine(d.File, d.Line), oneLine(d.Text))
		}
	}
	if len(r.Split) > 0 {
		b.WriteString("\n## Cómo partir este PR\n\n")
		for i, p := range r.Split {
			fmt.Fprintf(&b, "%d. %s (%s, %d líneas)", i+1, p.Title, strings.Join(p.Files, ", "), p.Lines)
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
	fmt.Fprintf(b, "\n### %s `%s`", severityNames[f.Severity], loc(f))
	switch {
	case f.Status == finding.StatusSinConfirmar:
		b.WriteString(" · **Sin confirmar**")
	case f.Origin == finding.OriginIncierto:
		b.WriteString(" · **Origen incierto**")
	}
	b.WriteString("\n\n")
	b.WriteString(strings.TrimSpace(f.What) + "\n")
	if f.Status == finding.StatusSinConfirmar {
		b.WriteString("\nNo se pudo validar: tomalo como una sospecha y revisalo vos.\n")
	}
	if f.Origin == finding.OriginIncierto {
		b.WriteString("\nNo está claro si el PR lo introdujo o ya estaba.\n")
	}
	if strings.TrimSpace(f.Why) != "" {
		fmt.Fprintf(b, "\nPor qué pasa: %s\n", strings.TrimSpace(f.Why))
	}
	if strings.TrimSpace(f.Fix) != "" {
		b.WriteString("\n")
		if f.Rule != nil {
			fmt.Fprintf(b, "Solución, según `%s` (\"%s\"):", f.Rule.File, f.Rule.Quote)
		} else {
			b.WriteString("Solución:")
		}
		fix := strings.TrimSpace(f.Fix)
		if strings.Contains(fix, "\n") {
			fmt.Fprintf(b, "\n\n```\n%s\n```\n", fix)
		} else {
			fmt.Fprintf(b, " %s\n", fix)
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
func judges(votes []finding.JudgeVote) string {
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
		parts = append(parts, fmt.Sprintf("%s %s", v.Judge, say))
	}
	return strings.Join(parts, "; ") + "."
}

func writeFooter(b *strings.Builder, r pipeline.Result, s sections) {
	b.WriteString("\n## Al pie\n")
	if len(s.discarded) > 0 {
		fmt.Fprintf(b, "\nDescartados: %d.\n\n", len(s.discarded))
		for _, f := range s.discarded {
			reason := oneLine(f.Reason)
			if reason == "" {
				reason = "sin motivo registrado"
			}
			fmt.Fprintf(b, "- `%s` %s Motivo: %s\n", loc(f), oneLine(f.What), reason)
		}
	}
	if len(r.Unreviewed) > 0 {
		b.WriteString("\nSin revisar:\n\n")
		for _, p := range r.Unreviewed {
			fmt.Fprintf(b, "- %s\n", p)
		}
	}
	if len(r.Excluded) > 0 {
		fmt.Fprintf(b, "\nExcluidos de la revisión (generados): %s.\n", strings.Join(r.Excluded, ", "))
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
			paths[i] = f.Path
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
		l := fmt.Sprintf("`%s` (rol %s)", f.Agent, f.Role)
		var covered string
		switch {
		case len(f.Files) > 0:
			covered = strings.Join(f.Files, ", ")
		case f.Candidate != "":
			covered = "candidato " + f.Candidate
		}
		if covered != "" {
			l += ": " + covered
		}
		if reason := truncate(firstLine(f.Reason), maxReasonRunes); reason != "" {
			l += ". Motivo: " + reason
		}
		lines = append(lines, l)
	}
	for _, name := range r.FailedAgents {
		if !seen[name] {
			lines = append(lines, fmt.Sprintf("`%s`", name))
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
	if line > 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}
	return file
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(line)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
