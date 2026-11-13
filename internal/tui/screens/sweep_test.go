package screens

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

// TestEveryScreenIsReadableInEveryTheme renders each screen state through
// theme.Fill under all 14 themes and checks every cell: the theme background
// (or the Crítico label's), an explicit foreground, no reverse video, and a
// contrast of 4.5:1 for text (3:1 for separators and bars drawn in the tick color, and for gradient bars).
func TestEveryScreenIsReadableInEveryTheme(t *testing.T) {
	t.Cleanup(func() { d, _ := theme.ByName(theme.DefaultName); theme.Use(d) })
	type scr struct {
		name string
		w, h int
		fn   func() string
	}
	k := func(m tea.Model, keys ...string) tea.Model {
		for _, s := range keys {
			m, _ = m.Update(tuitest.Key(s))
		}
		return m
	}
	progressAgents := func(log bool) string {
		m, _ := newProgress(pipeline.ModeExhaustivo, nil)
		m = feed(m, pipeline.Event{Kind: pipeline.StageStarted, Stage: 1}, pipeline.Event{Kind: pipeline.StageDone, Stage: 1},
			pipeline.Event{Kind: pipeline.StageStarted, Stage: pipeline.StageSpecialists},
			pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "bugs", Role: config.RoleBugs, Model: config.ModelOpus},
			pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "seguridad", Role: config.RoleSeguridad, Model: config.ModelSonnet},
			pipeline.Event{Kind: pipeline.AgentStarted, Stage: pipeline.StageSpecialists, Agent: "reglas", Role: config.RoleReglas, Model: config.ModelSonnet},
			pipeline.Event{Kind: pipeline.AgentActivity, Stage: pipeline.StageSpecialists, Agent: "bugs", Text: "leyendo src/a.go"},
			pipeline.Event{Kind: pipeline.AgentDone, Stage: pipeline.StageSpecialists, Agent: "reglas", Candidates: 2},
			pipeline.Event{Kind: pipeline.AgentFailed, Stage: pipeline.StageSpecialists, Agent: "seguridad", Err: errors.New("se cortó")})
		if log {
			m, _ = press(m, "l")
		}
		return m.render(120, 40)
	}
	screens := []scr{
		{"list", 100, 30, func() string { var n int; return loaded(t, &n).render(100, 30) }},
		{"list-narrow", 60, 30, func() string { var n int; return loaded(t, &n).render(60, 30) }},
		{"list-hidden", 120, 30, func() string {
			var n int
			return k(loaded(t, &n), "t", "down", "down", "down", "down", "down").(ListModel).render(120, 30)
		}},
		{"list-filter", 100, 30, func() string { var n int; return k(loaded(t, &n), "/", "f", "a").(ListModel).render(100, 30) }},
		{"list-filter-none", 100, 30, func() string {
			var n int
			return k(loaded(t, &n), "/", "z", "z", "enter").(ListModel).render(100, 30)
		}},
		{"list-note", 100, 30, func() string {
			var n int
			return loaded(t, &n).WithNote("No conozco el tema x; uso pr-review.").render(100, 30)
		}},
		{"list-err", 100, 30, func() string {
			var n int
			return update(loaded(t, &n), RowsLoadedMsg{Err: errors.New("boom")}).render(100, 30)
		}},
		{"list-loading", 100, 30, func() string { return NewList("a/b", func(int, map[int]bool) tea.Cmd { return nil }).render(100, 30) }},
		{"repopicker", 80, 20, func() string { var n int; return loadedPicker(t, &n, sampleRepos()).render(80, 20) }},
		{"repopicker-filter", 80, 20, func() string {
			var n int
			return k(loadedPicker(t, &n, sampleRepos()), "/", "a").(RepoPickerModel).render(80, 20)
		}},
		{"mode", 100, 24, func() string { return newMode().render(100, 24) }},
		{"mode-narrow", 50, 24, func() string { return newMode().render(50, 24) }},
		{"models", 120, 24, func() string { return newModels(nil).render(120, 24) }},
		{"models-changed", 120, 24, func() string {
			return k(newModels(nil), "down", "right", "down", "e").(ModelsModel).render(120, 24)
		}},
		{"progress", 120, 40, func() string { m, _ := newProgress(pipeline.ModeLigero, nil); return m.render(120, 40) }},
		{"progress-agents", 120, 40, func() string { return progressAgents(false) }},
		{"progress-log", 120, 40, func() string { return progressAgents(true) }},
		{"report", 110, 80, func() string { m, _ := newReport(reportResult()); return m.render(110, 80) }},
		{"report-exp", 110, 120, func() string {
			m, _ := newReport(reportResult())
			return k(m, "down", "down", "down", "enter", "l").(ReportModel).render(110, 120)
		}},
		{"onboarding", 100, 30, func() string { return newOnboarding(obFake{}, nil).render(100, 30) }},
		{"presetpicker", 100, 30, func() string { return NewPresetPicker(config.Defaults()).render(100, 30) }},
		{"settings", 100, 30, func() string { return NewSettings().render(100, 30) }},
		{"themes", 100, 40, func() string { return NewThemes("x").render(100, 40) }},
		{"themes-light-preview", 100, 40, func() string { return NewThemes("claude-code").render(100, 40) }},
		{"themes-gemini-preview", 100, 40, func() string { return NewThemes("gemini").render(100, 40) }},
		{"themes-err", 100, 12, func() string { return NewThemes("x").WithSaveError(errors.New("disco lleno")).render(100, 12) }},
	}
	// The picker's preview is painted in the previewed theme, so its cells
	// are judged against the theme whose background they carry.
	byBG := map[string][]theme.Theme{}
	for _, th := range theme.All() {
		byBG[strings.ToUpper(th.BG)] = append(byBG[strings.ToUpper(th.BG)], th)
	}
	for _, th := range theme.All() {
		theme.Use(th)
		problems := map[string]int{}
		for _, s := range screens {
			lines := strings.Split(theme.Fill(s.fn(), s.w, s.h), "\n")
			if len(lines) != s.h {
				problems[fmt.Sprintf("%s: %d lines, want %d", s.name, len(lines), s.h)]++
			}
			for y, l := range lines {
				w := 0
				for _, c := range tuitest.Cells(l) {
					w += c.Width
					if c.BG == "" {
						problems[fmt.Sprintf("%s: default background on %q", s.name, c.Text)]++
						continue
					}
					if strings.TrimSpace(c.Text) == "" {
						if _, ok := byBG[strings.ToUpper(c.BG)]; !ok && !isCrit(c.BG) {
							problems[fmt.Sprintf("%s: blank cell on background %s", s.name, c.BG)]++
						}
						continue
					}
					if c.FG == "" {
						problems[fmt.Sprintf("%s: default foreground on %q", s.name, c.Text)]++
						continue
					}
					if c.Reverse {
						problems[fmt.Sprintf("%s: reverse video on %q", s.name, c.Text)]++
					}
					cellThemes := byBG[strings.ToUpper(c.BG)]
					if len(cellThemes) == 0 && !isCrit(c.BG) {
						problems[fmt.Sprintf("%s: unknown background %s", s.name, c.BG)]++
					}
					if !strings.HasPrefix(s.name, "themes") && !strings.EqualFold(c.BG, th.BG) && !strings.EqualFold(c.BG, th.Crit) {
						problems[fmt.Sprintf("%s: foreign background %s", s.name, c.BG)]++
					}
					min := 4.5
					for _, ct := range cellThemes {
						if (strings.EqualFold(c.FG, ct.Tick) && strings.ContainsAny(c.Text, "·│─▌█▉▊▋▍▎▏")) || (len(ct.Gradient) > 0 && strings.ContainsAny(c.Text, "█▉▊▋▌▍▎▏")) {
							min = 3
						}
					}
					if cr := theme.Contrast(c.FG, c.BG); cr < min-0.005 {
						problems[fmt.Sprintf("%s: contrast %.2f < %.1f, fg %s on %s, e.g. %q at line %d", s.name, cr, min, c.FG, c.BG, c.Text, y)]++
					}
				}
				if w != s.w {
					problems[fmt.Sprintf("%s: line %d is %d cells wide, want %d", s.name, y, w, s.w)]++
				}
			}
		}
		keys := make([]string, 0, len(problems))
		for p := range problems {
			keys = append(keys, p)
		}
		sort.Strings(keys)
		for _, p := range keys {
			t.Errorf("[%s] %s (x%d)", th.Name, p, problems[p])
		}
	}
}

// isCrit reports whether bg is some theme's Crítico label background.
func isCrit(bg string) bool {
	for _, th := range theme.All() {
		if strings.EqualFold(bg, th.Crit) {
			return true
		}
	}
	return false
}
