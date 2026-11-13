package theme

import (
	"fmt"
	"math"
	"strconv"
)

func parseHex(c string) (r, g, b float64) {
	if len(c) != 7 || c[0] != '#' {
		return 0, 0, 0
	}
	v, err := strconv.ParseUint(c[1:], 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return float64(v >> 16 & 0xFF), float64(v >> 8 & 0xFF), float64(v & 0xFF)
}

func luminance(c string) float64 {
	r, g, b := parseHex(c)
	lin := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// Contrast returns the WCAG 2 contrast ratio between two #RRGGBB colors.
func Contrast(a, b string) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Lerp interpolates linearly in RGB across evenly spaced stops, with x in [0,1].
func Lerp(stops []string, x float64) string {
	switch len(stops) {
	case 0:
		return ""
	case 1:
		return stops[0]
	}
	x = math.Min(1, math.Max(0, x))
	pos := x * float64(len(stops)-1)
	i := int(pos)
	if i >= len(stops)-1 {
		i = len(stops) - 2
	}
	f := pos - float64(i)
	r1, g1, b1 := parseHex(stops[i])
	r2, g2, b2 := parseHex(stops[i+1])
	mix := func(a, b float64) int { return int(math.Round(a + (b-a)*f)) }
	return fmt.Sprintf("#%02X%02X%02X", mix(r1, r2), mix(g1, g2), mix(b1, b2))
}

// GradientAt returns the gradient color at x, adjusted if needed so it reaches
// min contrast against bg. Only HSL lightness changes, in 0.5% steps: lighter
// on dark backgrounds, darker on light ones. If even the extreme lightness
// falls short, it returns white (dark bg) or black (light bg).
func GradientAt(stops []string, x float64, bg string, min float64) string {
	c := Lerp(stops, x)
	if Contrast(c, bg) >= min {
		return c
	}
	dark := luminance(bg) < 0.18
	r, g, b := parseHex(c)
	h, sat, l := rgbToHSL(r/255, g/255, b/255)
	for step := 1; step <= 200; step++ {
		if dark {
			l += 0.005
		} else {
			l -= 0.005
		}
		l = math.Min(1, math.Max(0, l))
		out := hslToHex(h, sat, l)
		if Contrast(out, bg) >= min {
			return out
		}
		if l == 0 || l == 1 {
			break
		}
	}
	if dark {
		return "#FFFFFF"
	}
	return "#000000"
}

func rgbToHSL(r, g, b float64) (h, s, l float64) {
	mx, mn := math.Max(r, math.Max(g, b)), math.Min(r, math.Min(g, b))
	l = (mx + mn) / 2
	d := mx - mn
	if d == 0 {
		return 0, 0, l
	}
	s = d / (1 - math.Abs(2*l-1))
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
	return h, s, l
}

func hslToHex(h, s, l float64) string {
	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	to := func(v float64) int { return int(math.Round((v + m) * 255)) }
	return fmt.Sprintf("#%02X%02X%02X", to(r), to(g), to(b))
}
