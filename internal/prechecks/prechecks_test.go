package prechecks

import (
	"fmt"
	"path"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/finding"
	"github.com/LeanMendez/pr-review/internal/prb"
)

func patch(lines ...string) string {
	return fmt.Sprintf("--- a/x\n+++ b/x\n@@ -1,2 +10,%d @@\n", len(lines)) + strings.Join(lines, "\n") + "\n"
}

func goodPR() prb.PR { return prb.PR{Title: "CU-86b1x2 Arreglo", Description: strings.Repeat("d", 40)} }

func runPatch(p string) Result {
	d := prb.Diff{Files: []prb.DiffFile{{Path: "src/a.go", Status: "modified", Patch: p, LinesAdded: 1}}}
	return Run(goodPR(), d, config.Defaults())
}

func TestSecretsOnAddedLinesOnly(t *testing.T) {
	r := runPatch(patch(" ctx", `+API_KEY = "abcd1234efgh"`))
	if len(r.Findings) != 1 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	f := r.Findings[0]
	if f.Severity != finding.SeverityCritico || f.Status != finding.StatusCandidato || f.Group != finding.GroupA ||
		f.Role != "prechecks" || f.File != "src/a.go" || f.Line != 11 || f.Origin != finding.OriginIntroducido {
		t.Errorf("finding = %+v", f)
	}
	for _, s := range append([]string{f.What, f.Why, f.Fix}, f.Evidence...) {
		if strings.Contains(s, "abcd1234efgh") {
			t.Errorf("secret leaked: %q", s)
		}
	}
	if f.Evidence[0] != "src/a.go:11" {
		t.Errorf("evidence = %v", f.Evidence)
	}
	if r := runPatch(patch(`-API_KEY = "abcd1234efgh"`)); len(r.Findings) != 0 {
		t.Errorf("removed line flagged: %+v", r.Findings)
	}
	if r := runPatch("--- a/x\n+++ b/x\n"); len(r.Findings) != 0 {
		t.Errorf("header flagged: %+v", r.Findings)
	}
	if r := runPatch("+++ b/API_KEY = \"abcd1234efgh\"\n"); len(r.Findings) != 0 {
		t.Errorf("no-hunk flagged: %+v", r.Findings)
	}
}

func TestSecretPatterns(t *testing.T) {
	cases := map[string]string{
		"aws":       "+k := \"AKIA" + strings.Repeat("A", 16) + "\"",
		"private":   "+-----BEGIN OPENSSH PRIVATE KEY-----",
		"assign":    `+password: "hunter2hunter2"`,
		"url":       "+u := \"postgres://admin:s3cr3t@db.local/x\"",
		"github":    "+t=ghp_" + strings.Repeat("a", 36),
		"slack":     "+t=xoxb-123456",
		"atlassian": "+t=ATATT" + strings.Repeat("a", 20),
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			if r := runPatch(patch(line)); len(r.Findings) != 1 {
				t.Errorf("findings = %+v", r.Findings)
			}
		})
	}
	t.Run("env", func(t *testing.T) {
		d := prb.Diff{Files: []prb.DiffFile{{Path: ".env", Status: "added", Patch: patch("+A=1")}}}
		r := Run(goodPR(), d, config.Defaults())
		if len(r.Findings) != 1 || r.Findings[0].Status != finding.StatusCandidato {
			t.Errorf("findings = %+v", r.Findings)
		}
	})
	t.Run("clean", func(t *testing.T) {
		if r := runPatch(patch("+x := 1")); len(r.Findings) != 0 {
			t.Errorf("findings = %+v", r.Findings)
		}
	})
}

func TestProcessFindings(t *testing.T) {
	cfg := config.Defaults()
	r := Run(prb.PR{Title: "Arreglo", Description: strings.Repeat("d", 40)}, prb.Diff{}, cfg)
	if len(r.Findings) != 1 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	f := r.Findings[0]
	if f.Severity != finding.SeverityBajo || f.Status != finding.StatusConfirmado || f.Group != finding.GroupA ||
		f.What != "El título no empieza con el ID de la tarea." {
		t.Errorf("finding = %+v", f)
	}
	big := prb.Diff{Files: []prb.DiffFile{{Path: "a.go", LinesAdded: 401}}}
	r = Run(prb.PR{Title: "CU-1a x", Description: "  \n "}, big, cfg)
	if len(r.Findings) != 1 || r.Findings[0].Severity != finding.SeverityBajo {
		t.Errorf("findings = %+v", r.Findings)
	}
	if r := Run(goodPR(), prb.Diff{}, cfg); len(r.Findings) != 0 {
		t.Errorf("findings = %+v", r.Findings)
	}
}

func TestOversize(t *testing.T) {
	cfg := config.Defaults()
	d := prb.Diff{Files: []prb.DiffFile{
		{Path: "a.go", LinesAdded: 200, LinesRemoved: 201},
		{Path: "package-lock.json", LinesAdded: 5000},
	}}
	r := Run(goodPR(), d, cfg)
	if !r.Oversize || r.TotalLines != 401 {
		t.Errorf("oversize=%v total=%d", r.Oversize, r.TotalLines)
	}
	d.Files[0].LinesRemoved = 200
	if r := Run(goodPR(), d, cfg); r.Oversize || r.TotalLines != 400 {
		t.Errorf("oversize=%v total=%d", r.Oversize, r.TotalLines)
	}
	var many prb.Diff
	for i := 0; i < 16; i++ {
		many.Files = append(many.Files, prb.DiffFile{Path: fmt.Sprintf("f%d.go", i)})
	}
	if !Run(goodPR(), many, cfg).Oversize {
		t.Error("16 files should be oversize")
	}
}

func TestSensitive(t *testing.T) {
	for _, p := range []string{"migrations/0042.sql", "Dockerfile", "bitbucket-pipelines.yml", "go.mod", "package.json", "config/app.yaml"} {
		if !IsSensitive(p) {
			t.Errorf("%s should be sensitive", p)
		}
	}
	if IsSensitive("src/a.go") {
		t.Error("src/a.go should not be sensitive")
	}
	d := prb.Diff{Files: []prb.DiffFile{{Path: "go.mod"}, {Path: "src/a.go"}}}
	if r := Run(goodPR(), d, config.Defaults()); !reflect.DeepEqual(r.Sensitive, []string{"go.mod"}) {
		t.Errorf("sensitive = %v", r.Sensitive)
	}
}

func TestExcludedAndUnreviewed(t *testing.T) {
	d := prb.Diff{
		Files: []prb.DiffFile{
			{Path: "package-lock.json"}, {Path: "dist/app.js"}, {Path: "x.min.js"},
			{Path: "big.go", PatchOmitted: true}, {Path: "ok.go"},
		},
		OmittedFiles: []string{"other.go"},
	}
	r := Run(goodPR(), d, config.Defaults())
	if !reflect.DeepEqual(r.Excluded, []string{"package-lock.json", "dist/app.js", "x.min.js"}) {
		t.Errorf("excluded = %v", r.Excluded)
	}
	if !reflect.DeepEqual(r.Unreviewed, []string{"big.go", "other.go"}) {
		t.Errorf("unreviewed = %v", r.Unreviewed)
	}
	for _, p := range []string{"yarn.lock", "go.sum", "vendor/b.go", "node_modules/x.js", "m.js.map", "logo.png"} {
		if !IsGenerated(p) {
			t.Errorf("%s should be generated", p)
		}
	}
	if IsGenerated("src/a.go") || len(ExcludeGlobs) == 0 {
		t.Error("src/a.go generated or no globs")
	}
}

// TestGeneratedAgreesWithExcludeGlobs pins what is generated and checks that
// the globs sent to prb and IsGenerated say the same for every path.
func TestGeneratedAgreesWithExcludeGlobs(t *testing.T) {
	for p, want := range map[string]bool{
		// build output and vendored code only at the repo root
		"dist/app.js": true, "vendor/github.com/x/y.go": true, "node_modules/left-pad/index.js": true,
		"internal/build/deploy.go": false, "build/main.go": false, "cmd/build.go": false,
		"pkg/dist/version.go": false, "a/vendor/b.go": false, "web/node_modules/x.js": false,
		// lockfiles, minified files, source maps and binaries anywhere
		"package-lock.json": true, "web/yarn.lock": true, "services/api/go.sum": true,
		"app.min.js": true, "web/js/app.min.js": true, "web/js/app.js.map": true,
		"logo.png": true, "assets/img/logo.png": true, "docs/manual.pdf": true,
		// hand-written code
		"src/a.go": false, "web/js/app.js": false, "go.mod": false, "distribution/x.go": false,
	} {
		if got := IsGenerated(p); got != want {
			t.Errorf("IsGenerated(%q) = %v, want %v", p, got, want)
		}
		if got := slices.ContainsFunc(ExcludeGlobs, func(g string) bool { return globMatch(g, p) }); got != want {
			t.Errorf("ExcludeGlobs match %q = %v, want %v", p, got, want)
		}
	}
}

// globMatch matches a path against a glob where "**" is any number of
// whole segments and every other segment follows path.Match.
func globMatch(pattern, name string) bool {
	var match func(p, n []string) bool
	match = func(p, n []string) bool {
		switch {
		case len(p) == 0:
			return len(n) == 0
		case p[0] == "**":
			for i := 0; i <= len(n); i++ {
				if match(p[1:], n[i:]) {
					return true
				}
			}
			return false
		case len(n) == 0:
			return false
		}
		ok, _ := path.Match(p[0], n[0])
		return ok && match(p[1:], n[1:])
	}
	return match(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func TestPartialPatchesAreUnreviewed(t *testing.T) {
	d := prb.Diff{Files: []prb.DiffFile{
		{Path: "src/trunc.go", Status: "modified", LinesAdded: 900, Truncated: true, OmittedLines: 850, Patch: patch("+x := 1")},
		{Path: "src/cut.go", Status: "modified", LinesAdded: 40, OmittedLines: 12, Patch: patch("+y := 2")},
		{Path: "src/empty.go", Status: "modified", LinesAdded: 3, LinesRemoved: 1},
		{Path: "src/both.go", Status: "modified", LinesAdded: 900, Truncated: true, PatchOmitted: true},
		{Path: "src/renamed.go", OldPath: "src/old.go", Status: "renamed"},
		{Path: "assets/logo.png", Binary: true, LinesAdded: 0},
		{Path: "src/ok.go", Status: "modified", LinesAdded: 1, Patch: patch("+z := 3")},
	}}
	r := Run(goodPR(), d, config.Defaults())
	want := []string{"src/trunc.go", "src/cut.go", "src/empty.go", "src/both.go"}
	if !reflect.DeepEqual(r.Unreviewed, want) {
		t.Errorf("unreviewed = %v, want %v", r.Unreviewed, want)
	}
	for _, tc := range []struct {
		f    prb.DiffFile
		want string
	}{
		{d.Files[0], "patch recortado: faltan 850 líneas"},
		{prb.DiffFile{Truncated: true, Patch: "x"}, "patch recortado"},
		{d.Files[2], "patch vacío aunque cambió líneas"},
		{d.Files[3], "el diff no trajo su patch"},
		{d.Files[4], ""},
		{d.Files[6], ""},
	} {
		if got := PatchGap(tc.f); got != tc.want {
			t.Errorf("PatchGap(%s) = %q, want %q", tc.f.Path, got, tc.want)
		}
	}
}

func TestSecretsScannedInTruncatedPatch(t *testing.T) {
	d := prb.Diff{Files: []prb.DiffFile{{
		Path: "src/a.go", Status: "modified", LinesAdded: 900, Truncated: true, OmittedLines: 850,
		Patch: patch(`+API_KEY = "abcd1234efgh"`),
	}}}
	if r := Run(goodPR(), d, config.Defaults()); len(r.Findings) != 1 {
		t.Errorf("the visible part of a truncated patch must still be scanned: %+v", r.Findings)
	}
}

func TestSummarizeFromFileStats(t *testing.T) {
	stats := []prb.FileStat{
		{Path: "src/a.go", LinesAdded: 300, LinesRemoved: 50},
		{Path: "go.mod", LinesAdded: 3, LinesRemoved: 1},
		{Path: "go.sum", LinesAdded: 900},
		{Path: "migrations/1.sql", LinesAdded: 60},
	}
	var files []prb.DiffFile
	for _, s := range stats {
		files = append(files, prb.DiffFile{Path: s.Path, LinesAdded: s.LinesAdded, LinesRemoved: s.LinesRemoved})
	}
	cfg := config.Defaults()
	run := Run(goodPR(), prb.Diff{Files: files}, cfg)
	sum := Summarize(stats, cfg)
	if run.Oversize != sum.Oversize || run.TotalLines != sum.TotalLines ||
		!reflect.DeepEqual(run.Sensitive, sum.Sensitive) || !reflect.DeepEqual(run.Excluded, sum.Excluded) {
		t.Errorf("run=%+v summarize=%+v", run, sum)
	}
	if !sum.Oversize || sum.TotalLines != 414 {
		t.Errorf("summarize = %+v", sum)
	}
}

func TestInvalidTaskIDPatternFallsBackToDefault(t *testing.T) {
	cfg := config.Defaults()
	cfg.TaskIDPattern = "([unclosed"
	r := Run(prb.PR{Title: "Arreglo", Description: strings.Repeat("d", 40)}, prb.Diff{}, cfg)
	if len(r.Findings) != 1 || r.Findings[0].What != "El título no empieza con el ID de la tarea." {
		t.Errorf("findings = %+v", r.Findings)
	}
	if r := Run(goodPR(), prb.Diff{}, cfg); len(r.Findings) != 0 {
		t.Errorf("findings = %+v", r.Findings)
	}
}

func TestEnvTemplatesAreNotFlagged(t *testing.T) {
	for p, want := range map[string]int{
		".env": 1, ".env.local": 1, "app/.env": 1,
		".env.example": 0, ".env.sample": 0, ".env.template": 0, ".env.dist": 0,
	} {
		d := prb.Diff{Files: []prb.DiffFile{{Path: p, Status: "added", Patch: patch("+A=1")}}}
		if r := Run(goodPR(), d, config.Defaults()); len(r.Findings) != want {
			t.Errorf("%s: findings = %+v, want %d", p, r.Findings, want)
		}
	}
}
