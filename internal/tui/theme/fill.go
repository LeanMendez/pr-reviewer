package theme

import (
	"image/color"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// fillParser is reused by every Fill: like the theme variables, Fill only runs
// on the Bubble Tea goroutine (or in tests), so one parser is enough.
var fillParser = ansi.NewParser()

// Fill paints the active theme background on every cell of a screen. Each
// line, measured in visible cells, is cut or padded with background spaces to
// exactly width; background lines are added up to height; and the background
// is set again at the start of each line and after every SGR sequence that
// drops it (a reset or a default background), so unstyled text gets it too.
// Explicit backgrounds, like the "Crítico" label's, are kept. One trailing
// newline is ignored and lines beyond height are left to the renderer. A width
// or height of 0 or less (size still unknown) leaves that dimension as is.
func Fill(content string, width, height int) string {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	p := fillParser
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		if width > 0 {
			l = ansi.Truncate(l, width, "")
		}
		b.WriteString(bgSeq)
		withBackground(&b, l, p)
		if pad := width - ansi.StringWidth(l); pad > 0 {
			b.WriteString(ansi.ResetStyle + bgSeq + strings.Repeat(" ", pad))
		}
		b.WriteString(ansi.ResetStyle)
	}
	return b.String()
}

// withBackground copies line to b, writing the theme background after every
// SGR sequence that leaves the terminal default background.
func withBackground(b *strings.Builder, line string, p *ansi.Parser) {
	for {
		i := strings.IndexByte(line, ansi.ESC)
		if i < 0 {
			b.WriteString(line)
			return
		}
		b.WriteString(line[:i])
		seq, _, n, _ := ansi.DecodeSequence(line[i:], ansi.NormalState, p)
		b.WriteString(seq)
		if strings.HasPrefix(seq, "\x1b[") && ansi.Cmd(p.Command()) == 'm' && dropsBackground(p.Params()) {
			b.WriteString(bgSeq)
		}
		line = line[i+n:]
	}
}

// dropsBackground reports whether an SGR sequence ends with the terminal
// default background: a reset (no parameters or 0) or 49 that no later
// parameter replaces with a background of its own.
func dropsBackground(params ansi.Params) bool {
	if len(params) == 0 {
		return true
	}
	drops := false
	for i := 0; i < len(params); i++ {
		switch v := params[i].Param(0); {
		case v == 0 || v == 49:
			drops = true
		case v >= 40 && v <= 47, v >= 100 && v <= 107:
			drops = false
		case v == 38 || v == 48 || v == 58:
			if v == 48 {
				drops = false
			}
			var c color.Color // skip the color's own parameters
			i += max(ansi.ReadStyleColor(params[i:], &c), 1) - 1
		}
	}
	return drops
}
