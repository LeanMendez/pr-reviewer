package prechecks

import (
	"path"
	"slices"
	"strings"
)

var lockfiles = []string{
	"package-lock.json", "yarn.lock", "pnpm-lock.yaml", "go.sum",
	"composer.lock", "Gemfile.lock", "poetry.lock", "Cargo.lock",
}

var manifests = []string{
	"package.json", "go.mod", "composer.json", "Gemfile", "pyproject.toml",
	"requirements.txt", "Cargo.toml", "pom.xml", "build.gradle", "build.gradle.kts",
}

// generatedRoots hold build output or vendored code only at the repo root:
// the same names deeper in the tree are often hand-written code
// (internal/build, pkg/dist), which must be reviewed.
var generatedRoots = []string{"dist", "vendor", "node_modules"}

var binaryExts = []string{
	".png", ".jpg", ".jpeg", ".gif", ".webp", ".ico", ".pdf", ".zip", ".gz",
	".tar", ".jar", ".exe", ".dll", ".so", ".woff", ".woff2", ".ttf", ".eot", ".mp4",
}

// ExcludeGlobs are the globs passed to `prb pr diff --exclude`. They must
// match exactly the paths IsGenerated reports (prechecks tests pin it).
var ExcludeGlobs = buildExcludeGlobs()

func buildExcludeGlobs() []string {
	var g []string
	for _, l := range lockfiles {
		g = append(g, "**/"+l, l)
	}
	for _, d := range generatedRoots {
		g = append(g, d+"/**")
	}
	g = append(g, "*.min.js", "**/*.min.js", "*.map", "**/*.map")
	for _, e := range binaryExts {
		g = append(g, "*"+e, "**/*"+e)
	}
	return g
}

// IsGenerated reports whether path is a lockfile, minified, source map or
// binary file anywhere, or lives under a root-level dist/, vendor/ or
// node_modules/; such files are excluded from the review.
func IsGenerated(p string) bool {
	p = normalize(p)
	base := path.Base(p)
	for _, l := range lockfiles {
		if base == l {
			return true
		}
	}
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".map") {
		return true
	}
	for _, e := range binaryExts {
		if strings.HasSuffix(lower, e) {
			return true
		}
	}
	root, _, nested := strings.Cut(p, "/")
	return nested && slices.Contains(generatedRoots, root)
}

// IsSensitive reports whether path deserves extra attention from the agents.
func IsSensitive(p string) bool {
	p = normalize(p)
	base := path.Base(p)
	lower := strings.ToLower(base)
	for _, l := range lockfiles {
		if base == l {
			return true
		}
	}
	for _, m := range manifests {
		if base == m {
			return true
		}
	}
	if strings.HasSuffix(lower, ".sql") || strings.HasPrefix(base, "Dockerfile") ||
		base == "bitbucket-pipelines.yml" || strings.Contains(lower, ".env") {
		return true
	}
	if strings.HasPrefix(base, "docker-compose") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
		return true
	}
	if strings.HasPrefix(p, ".github/") {
		return true
	}
	segs := strings.Split(p, "/")
	for _, s := range segs[:len(segs)-1] {
		switch s {
		case "migrations", "migraciones", "config", "permissions":
			return true
		}
	}
	return false
}

func normalize(p string) string { return strings.TrimPrefix(path.Clean(p), "./") }

func isEnvFile(p string) bool {
	b := path.Base(p)
	for _, t := range []string{".example", ".sample", ".template", ".dist"} {
		if strings.HasSuffix(b, ".env"+t) {
			return false
		}
	}
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}
