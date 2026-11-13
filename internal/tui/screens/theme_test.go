package screens

import (
	"strings"
	"testing"

	"github.com/LeanMendez/pr-review/internal/config"
	"github.com/LeanMendez/pr-review/internal/pipeline"
	"github.com/LeanMendez/pr-review/internal/tui/theme"
	"github.com/LeanMendez/pr-review/internal/tui/tuitest"
)

// TestBrandAndMarkerFollowTheme renders every screen and checks that the
// " pr-review" header uses theme.Brand and the selection marker theme.Marker:
// gemini's gradient, or Strong and Accent in a theme without one.
func TestBrandAndMarkerFollowTheme(t *testing.T) {
	t.Cleanup(func() {
		def, _ := theme.ByName(theme.DefaultName)
		theme.Use(def)
	})
	screens := []struct {
		name   string
		render func() string
		marker bool
	}{
		{"list", func() string { var n int; return loaded(t, &n).render(100, 30) }, true},
		{"repopicker", func() string { var n int; return loadedPicker(t, &n, sampleRepos()).render(80, 20) }, true},
		{"mode", func() string { return newMode().render(100, 24) }, true},
		{"models", func() string { return newModels(nil).render(120, 24) }, true},
		{"progress", func() string { m, _ := newProgress(pipeline.ModeLigero, nil); return m.render(120, 0) }, false},
		{"report", func() string { m, _ := newReport(reportResult()); return m.render(110, 0) }, true},
		{"onboarding", func() string { return newOnboarding(obFake{}, nil).render(100, 30) }, false},
		{"presetpicker", func() string { return NewPresetPicker(config.Defaults()).render(100, 30) }, true},
	}
	for _, tc := range []struct {
		theme string
		check func(t *testing.T, th theme.Theme, brand []tuitest.Cell, marker tuitest.Cell)
	}{
		{"gemini", func(t *testing.T, th theme.Theme, brand []tuitest.Cell, marker tuitest.Cell) {
			colors := map[string]bool{}
			for _, c := range brand {
				colors[c.FG] = true
			}
			if len(colors) < 2 {
				t.Errorf("brand has %d foreground colors, want a gradient: %+v", len(colors), brand)
			}
			if mid := theme.Lerp(th.Gradient, 0.5); marker.Text != "" && marker.FG != mid {
				t.Errorf("marker fg %s, want the gradient's middle %s", marker.FG, mid)
			}
		}},
		{"codex", func(t *testing.T, th theme.Theme, brand []tuitest.Cell, marker tuitest.Cell) {
			for _, c := range brand {
				if c.FG != th.Strong {
					t.Errorf("brand cell %+v, want Strong %s", c, th.Strong)
				}
			}
			if marker.Text != "" && marker.FG != th.Accent {
				t.Errorf("marker fg %s, want Accent %s", marker.FG, th.Accent)
			}
		}},
	} {
		th, _ := theme.ByName(tc.theme)
		theme.Use(th)
		for _, s := range screens {
			t.Run(tc.theme+"/"+s.name, func(t *testing.T) {
				lines := strings.Split(s.render(), "\n")
				var brand []tuitest.Cell
				for _, l := range lines {
					if strings.HasPrefix(tuitest.Plain(l), " pr-review") {
						brand = tuitest.Cells(l)[1:10]
						break
					}
				}
				if brand == nil {
					t.Fatal("no \" pr-review\" header")
				}
				var marker tuitest.Cell
				for _, l := range lines {
					if c := firstVisible(tuitest.Cells(l)); c.Text == "▌" {
						marker = c
						break
					}
				}
				if s.marker && marker.Text == "" {
					t.Fatal("no selection marker")
				}
				tc.check(t, th, brand, marker)
			})
		}
	}
}

// firstVisible returns the first cell that is not a space.
func firstVisible(cells []tuitest.Cell) tuitest.Cell {
	for _, c := range cells {
		if c.Text != " " {
			return c
		}
	}
	return tuitest.Cell{}
}
