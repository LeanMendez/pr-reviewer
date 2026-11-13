package prechecks

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/prb"
)

type secretPattern struct {
	name string
	re   *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{"clave de acceso de AWS", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"clave privada", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"asignación de credencial", regexp.MustCompile(`(?i)(api[_-]?key|secret|token|passw(or)?d)\s*[:=]\s*['"][^'"]{8,}['"]`)},
	{"URL con credenciales", regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)},
	{"token de GitHub", regexp.MustCompile(`ghp_[A-Za-z0-9]{36}`)},
	{"token de Slack", regexp.MustCompile(`xox[baprs]-`)},
	{"token de Atlassian", regexp.MustCompile(`ATATT[A-Za-z0-9_\-=]{20,}`)},
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// scanSecrets returns one candidate finding per added line that matches a
// secret pattern. The matched value is never copied into the finding.
func scanSecrets(f prb.DiffFile) []finding.Finding {
	var out []finding.Finding
	if f.Status == "added" && isEnvFile(f.Path) {
		out = append(out, secretFinding(f.Path, 1, "archivo .env"))
	}
	if f.Binary || f.PatchOmitted {
		return out
	}
	line, inHunk := 0, false
	for _, l := range strings.Split(f.Patch, "\n") {
		if m := hunkRe.FindStringSubmatch(l); m != nil {
			line, _ = strconv.Atoi(m[1])
			inHunk = true
			continue
		}
		if !inHunk || l == "" {
			continue
		}
		switch l[0] {
		case '+':
			for _, p := range secretPatterns {
				if p.re.MatchString(l[1:]) {
					out = append(out, secretFinding(f.Path, line, p.name))
					break
				}
			}
			line++
		case ' ':
			line++
		}
	}
	return out
}

func secretFinding(file string, line int, kind string) finding.Finding {
	return finding.Finding{
		Role:     "prechecks",
		File:     file,
		Line:     line,
		Severity: finding.SeverityCritico,
		Origin:   finding.OriginIntroducido,
		Group:    finding.GroupA,
		What:     "Posible secreto en una línea agregada (" + kind + ").",
		Why:      "Un secreto en el repositorio queda en el historial y lo puede leer cualquiera con acceso.",
		Fix:      "Rotá la credencial, sacala del código y cargala desde variables de entorno o un gestor de secretos.",
		Evidence: []string{file + ":" + strconv.Itoa(line)},
		Status:   finding.StatusCandidato,
	}
}
