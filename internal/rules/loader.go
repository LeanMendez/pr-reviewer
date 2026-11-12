// Package rules loads a repo's review rules (CLAUDE.md files and the files
// they reference) from the destination branch checkout.
package rules

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// RuleFile is a loaded rules file, relative to the base directory.
type RuleFile struct {
	Path  string
	Bytes int
}

// RuleSet is the loaded rules: the files, the ones skipped (path → reason)
// and their concatenated text with a header per file.
type RuleSet struct {
	Files   []RuleFile
	Skipped map[string]string
	Text    string
}

// Limits bound how far references are followed and how much is loaded.
// MaxDepth counts files: the CLAUDE.md files are depth 1, what they
// reference is depth 2, and so on. Zero fields take the default.
type Limits struct{ MaxDepth, MaxFileBytes, MaxTotalBytes int }

// DefaultLimits returns the default loader limits.
func DefaultLimits() Limits {
	return Limits{MaxDepth: 4, MaxFileBytes: 64 << 10, MaxTotalBytes: 256 << 10}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxDepth <= 0 {
		l.MaxDepth = d.MaxDepth
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = d.MaxTotalBytes
	}
	return l
}

const (
	rulesFile        = "CLAUDE.md"
	reasonOutside    = "fuera del repo"
	reasonTooBig     = "demasiado grande"
	reasonTotal      = "supera el límite total"
	reasonDepth      = "supera la profundidad máxima"
	reasonMissing    = "no existe"
	reasonNotFile    = "no es un archivo"
	reasonUnreadable = "no se pudo leer"
)

var (
	// refRe matches an @path import (at line start or after a space) or the
	// target of a markdown link.
	refRe      = regexp.MustCompile(`(?:^|\s)@(\S+)|\[[^\]]*\]\(\s*<?([^)\s>]+)>?[^)]*\)`)
	codeSpanRe = regexp.MustCompile("`[^`]*`")
	schemeRe   = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	docExts    = map[string]bool{".md": true, ".markdown": true, ".mdx": true, ".txt": true}
)

// pending is a file to visit: a slash path relative to the base directory
// (absolute or starting with "../" when it points outside).
type pending struct {
	rel      string
	depth    int
	optional bool // a CLAUDE.md probed by folder: silently ignored when missing
}

type loader struct {
	root  string
	lim   Limits
	seen  map[string]bool
	total int
	set   RuleSet
	text  strings.Builder
}

// Load reads, from baseDir, the root CLAUDE.md, the CLAUDE.md of every folder
// on the way to each changed path, and the files they reference through
// @path imports and markdown links to documents, breadth first. Anything
// outside baseDir (also through symlinks), too deep or too big is recorded in
// Skipped with its reason.
func Load(baseDir string, changed []string, lim Limits) (RuleSet, error) {
	root, err := filepath.Abs(baseDir)
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		return RuleSet{}, fmt.Errorf("leyendo las reglas de %s: %w", baseDir, err)
	}
	l := &loader{root: root, lim: lim.withDefaults(), seen: map[string]bool{}, set: RuleSet{Skipped: map[string]string{}}}
	queue := entries(changed)
	for len(queue) > 0 {
		next := l.visit(queue[0])
		queue = append(queue[1:], next...)
	}
	l.set.Text = l.text.String()
	return l.set, nil
}

// entries returns the root CLAUDE.md and one per folder of the changed
// paths, parents first. Paths pointing outside the repo are ignored.
func entries(changed []string) []pending {
	dirs := map[string]bool{}
	for _, c := range changed {
		c = path.Clean(filepath.ToSlash(c))
		if outside(c) {
			continue
		}
		for d := path.Dir(c); d != "."; d = path.Dir(d) {
			dirs[d] = true
		}
	}
	out := []pending{{rel: rulesFile, depth: 1, optional: true}}
	for _, d := range slices.Sorted(maps.Keys(dirs)) {
		out = append(out, pending{rel: d + "/" + rulesFile, depth: 1, optional: true})
	}
	return out
}

// visit loads one file and returns the references it makes.
func (l *loader) visit(p pending) []pending {
	if l.seen[p.rel] {
		return nil
	}
	l.seen[p.rel] = true
	if outside(p.rel) {
		return l.skip(p.rel, reasonOutside)
	}
	if p.depth > l.lim.MaxDepth {
		return l.skip(p.rel, reasonDepth)
	}
	real, err := filepath.EvalSymlinks(filepath.Join(l.root, filepath.FromSlash(p.rel)))
	if errors.Is(err, fs.ErrNotExist) {
		if p.optional {
			return nil
		}
		return l.skip(p.rel, reasonMissing)
	}
	if err != nil {
		return l.skip(p.rel, reasonUnreadable)
	}
	if !l.within(real) {
		return l.skip(p.rel, reasonOutside)
	}
	info, err := os.Stat(real)
	switch {
	case err != nil:
		return l.skip(p.rel, reasonUnreadable)
	case !info.Mode().IsRegular():
		if p.optional {
			return nil
		}
		return l.skip(p.rel, reasonNotFile)
	case info.Size() > int64(l.lim.MaxFileBytes):
		return l.skip(p.rel, reasonTooBig)
	case int64(l.total)+info.Size() > int64(l.lim.MaxTotalBytes):
		return l.skip(p.rel, reasonTotal)
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return l.skip(p.rel, reasonUnreadable)
	}
	l.total += len(data)
	l.set.Files = append(l.set.Files, RuleFile{Path: p.rel, Bytes: len(data)})
	if l.text.Len() > 0 {
		l.text.WriteString("\n\n")
	}
	fmt.Fprintf(&l.text, "### Archivo: %s\n\n%s", p.rel, strings.TrimRight(string(data), "\n"))

	var next []pending
	for _, ref := range references(string(data)) {
		next = append(next, pending{rel: resolve(path.Dir(p.rel), ref), depth: p.depth + 1})
	}
	return next
}

func (l *loader) skip(rel, reason string) []pending {
	l.set.Skipped[rel] = reason
	return nil
}

func (l *loader) within(real string) bool {
	rel, err := filepath.Rel(l.root, real)
	return err == nil && !outside(filepath.ToSlash(rel))
}

func outside(rel string) bool {
	return path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") ||
		rel == "~" || strings.HasPrefix(rel, "~/")
}

// resolve makes a reference relative to the base directory. Absolute and
// home references are kept as written, so they are rejected as outside.
func resolve(dir, ref string) string {
	if path.IsAbs(ref) || ref == "~" || strings.HasPrefix(ref, "~/") {
		return ref
	}
	return path.Join(dir, ref)
}

// references returns the @path imports and the markdown links to documents in
// text, in order, ignoring code blocks and code spans.
func references(text string) []string {
	var out []string
	for _, line := range proseLines(text) {
		for _, m := range refRe.FindAllStringSubmatch(codeSpanRe.ReplaceAllString(line, ""), -1) {
			if m[1] != "" {
				// An import must look like a path, so "@juan" is not one.
				if ref := strings.TrimRight(m[1], `.,;:!?)]}"'`); strings.ContainsAny(ref, "./") {
					out = append(out, ref)
				}
			} else if ref, ok := linkTarget(m[2]); ok {
				out = append(out, ref)
			}
		}
	}
	return out
}

// linkTarget keeps local links to documents, without fragment or query.
func linkTarget(t string) (string, bool) {
	if schemeRe.MatchString(t) || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
		return "", false
	}
	if i := strings.IndexAny(t, "#?"); i >= 0 {
		t = t[:i]
	}
	return t, docExts[strings.ToLower(path.Ext(t))]
}

// proseLines returns the lines of text outside fenced code blocks.
func proseLines(text string) []string {
	var out []string
	fence := ""
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
		case strings.HasPrefix(t, "```"):
			fence = "```"
		case strings.HasPrefix(t, "~~~"):
			fence = "~~~"
		default:
			out = append(out, line)
		}
	}
	return out
}
