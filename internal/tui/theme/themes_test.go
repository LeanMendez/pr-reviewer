package theme

import (
	"math"
	"reflect"
	"testing"
)

type row struct {
	name, desc                                                string
	bg, text, dim, strong, accent, ok, ref, crit, tick, label string
}

var want = []row{
	{"pr-review", "el tema que diseñamos", "#1E2331", "#D9DCE4", "#838A9E", "#F3F4F8", "#E7B45A", "#86C9A8", "#A99DD8", "#EE7F7A", "#646D85", "#000000"},
	{"claude-code", "claro, crema y ladrillo", "#F4EFE6", "#2B2622", "#746B63", "#14110E", "#AC5435", "#4E7839", "#3E6A8A", "#B3261E", "#9E8763", "#FFFFFF"},
	{"codex", "sobrio, toques verde agua", "#000000", "#D4D4D4", "#7A7A7A", "#FFFFFF", "#3CC4B4", "#5FB39A", "#8FB7B2", "#E5534B", "#5A5A5A", "#000000"},
	{"grok", "minimalista, detalle dorado", "#0B0B0B", "#D9D9D9", "#808080", "#FFFFFF", "#D4A537", "#9CC48A", "#7FA7E8", "#E06C6C", "#5E5E5E", "#000000"},
	{"gemini", "degradado azul, violeta y rosa", "#1E1E2E", "#CDD6F4", "#82879E", "#FFFFFF", "#B58BD6", "#94E2D5", "#89B4FA", "#F38BA8", "#65678D", "#000000"},
	{"nebula", "violeta y naranja", "#0E0520", "#E0D6F0", "#A946F1", "#E0D6F0", "#FF6B35", "#39FF14", "#B388FF", "#FF3D3D", "#7A2BDC", "#000000"},
	{"noir", "negro y crema", "#0D0D0D", "#F5E6C8", "#7A7A7A", "#F5E6C8", "#D4A857", "#7A9E7E", "#8AA6A3", "#BC5E5E", "#5F5F5F", "#000000"},
	{"netrunner", "cian cyberpunk", "#0A0A0A", "#80CBC4", "#008A73", "#80CBC4", "#00E5FF", "#69F0AE", "#00E5FF", "#FF1744", "#5D5D5D", "#000000"},
	{"grimoire", "pergamino y marrón", "#1A120B", "#D4C4A8", "#A27353", "#D4C4A8", "#C9A84C", "#6B8E23", "#5F8A8B", "#C26337", "#825A37", "#000000"},
	{"mission-control", "ámbar sobre azul noche", "#0B1120", "#FFB000", "#557DBE", "#E8E8E8", "#FFB000", "#33FF33", "#88AACC", "#FF3333", "#3060A9", "#000000"},
	{"mythos", "azul profundo y dorado", "#0D1B3D", "#E8EAF6", "#727FC9", "#E8EAF6", "#FFD700", "#66BB6A", "#4DD0E1", "#EF5350", "#4D59DA", "#000000"},
	{"retro86", "Commodore 64", "#40318D", "#ACA6E5", "#ADA4E4", "#ADADAD", "#ACA6E5", "#79BC79", "#70B8B8", "#E19A9A", "#8D80DA", "#000000"},
	{"brutalist", "monocromo puro", "#000000", "#FFFFFF", "#757575", "#FFFFFF", "#FFFFFF", "#FFFFFF", "#AAAAAA", "#FFFFFF", "#5B5B5B", "#000000"},
	{"sensei", "claro, tinta japonesa", "#F5F2EB", "#2C2C2C", "#766D65", "#7E6B3F", "#C41E3A", "#5B7553", "#587372", "#C41E3A", "#9B8A60", "#FFFFFF"},
}

func TestAllThemesOrderAndValues(t *testing.T) {
	got := All()
	if len(got) != len(want) {
		t.Fatalf("len(All()) = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		exp := Theme{Name: w.name, Desc: w.desc, BG: w.bg, Text: w.text, Dim: w.dim, Strong: w.strong,
			Accent: w.accent, OK: w.ok, Ref: w.ref, Crit: w.crit, Tick: w.tick, Label: w.label}
		if w.name == "gemini" {
			exp.Gradient = []string{"#4796E4", "#867CCF", "#C46B82"}
		}
		if !reflect.DeepEqual(g, exp) {
			t.Errorf("theme %d (%s):\n got  %+v\n want %+v", i, w.name, g, exp)
		}
	}
}

func TestThemesAreAccessible(t *testing.T) {
	check := func(th, token, fg, bg string, min float64) {
		t.Helper()
		if c := Contrast(fg, bg); c < min {
			t.Errorf("%s %s %s on %s: %.2f < %.1f", th, token, fg, bg, c, min)
		}
	}
	for _, th := range All() {
		for tok, c := range map[string]string{"text": th.Text, "dim": th.Dim, "strong": th.Strong,
			"accent": th.Accent, "ok": th.OK, "ref": th.Ref, "crit": th.Crit} {
			check(th.Name, tok, c, th.BG, 4.5)
		}
		check(th.Name, "tick", th.Tick, th.BG, 3)
		check(th.Name, "label", th.Label, th.Crit, 4.5)
		if th.Gradient != nil {
			for i := 0; i <= 8; i++ {
				x := float64(i) / 8
				check(th.Name, "gradient name", GradientAt(th.Gradient, x, th.BG, 4.5), th.BG, 4.5)
				check(th.Name, "gradient bar", GradientAt(th.Gradient, x, th.BG, 3.0), th.BG, 3)
			}
		}
	}
}

func TestContrastKnownValues(t *testing.T) {
	if c := Contrast("#000000", "#FFFFFF"); math.Abs(c-21) > 0.01 {
		t.Errorf("black/white = %.3f, want 21", c)
	}
	if c := Contrast("#777777", "#FFFFFF"); math.Abs(c-4.48) > 0.01 {
		t.Errorf("777/white = %.3f, want 4.48", c)
	}
	if a, b := Contrast("#123456", "#ABCDEF"), Contrast("#ABCDEF", "#123456"); a != b {
		t.Errorf("not symmetric: %v vs %v", a, b)
	}
}

func TestLerp(t *testing.T) {
	s := []string{"#000000", "#FFFFFF"}
	for x, want := range map[float64]string{0: "#000000", 1: "#FFFFFF", 0.5: "#808080"} {
		if got := Lerp(s, x); got != want {
			t.Errorf("Lerp(%v) = %s, want %s", x, got, want)
		}
	}
	g := []string{"#4796E4", "#867CCF", "#C46B82"}
	if Lerp(g, 0.5) != "#867CCF" || Lerp(g, 1) != "#C46B82" {
		t.Errorf("multi-stop Lerp wrong: %s %s", Lerp(g, 0.5), Lerp(g, 1))
	}
}

func TestByName(t *testing.T) {
	if th, ok := ByName("gemini"); !ok || th.Name != "gemini" {
		t.Errorf("ByName(gemini) = %v, %v", th, ok)
	}
	if _, ok := ByName("nope"); ok {
		t.Error("ByName(nope) ok")
	}
	if _, ok := ByName(DefaultName); !ok {
		t.Error("DefaultName not found")
	}
}

func hueOf(c string) float64 {
	r, g, b := parseHex(c)
	r, g, b = r/255, g/255, b/255
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	d := mx - mn
	if d == 0 {
		return 0
	}
	var h float64
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return h
}

func TestGradientAtKeepsPassingColors(t *testing.T) {
	g, _ := ByName("gemini")
	for _, x := range []float64{0, 0.5, 1} {
		if got, want := GradientAt(g.Gradient, x, g.BG, 4.5), Lerp(g.Gradient, x); got != want {
			t.Errorf("x=%v: GradientAt = %s, want %s", x, got, want)
		}
	}
}

func TestGradientAtRaisesDips(t *testing.T) {
	g, _ := ByName("gemini")
	raw := Lerp(g.Gradient, 6.0/8)
	got := GradientAt(g.Gradient, 6.0/8, g.BG, 4.5)
	if got == raw {
		t.Fatalf("expected adjustment of %s", raw)
	}
	if c := Contrast(got, g.BG); c < 4.5 {
		t.Errorf("contrast %.2f < 4.5", c)
	}
	if d := math.Abs(hueOf(got) - hueOf(raw)); d > 1 && d < 359 {
		t.Errorf("hue moved %.1f degrees (%s -> %s)", d, raw, got)
	}
}
