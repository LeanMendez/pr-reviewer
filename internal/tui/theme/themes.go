package theme

// DefaultName is the theme used when none is configured.
const DefaultName = "pr-review"

// Theme is a color palette; every color is #RRGGBB. Label is the text color
// of the "Crítico" tag drawn on top of Crit.
type Theme struct {
	Name, Desc                                                string
	BG, Text, Dim, Strong, Accent, OK, Ref, Crit, Tick, Label string
	Gradient                                                  []string
}

var themes = []Theme{
	{Name: "pr-review", Desc: "el tema que diseñamos", BG: "#1E2331", Text: "#D9DCE4", Dim: "#838A9E", Strong: "#F3F4F8",
		Accent: "#E7B45A", OK: "#86C9A8", Ref: "#A99DD8", Crit: "#EE7F7A", Tick: "#646D85", Label: "#000000"},
	{Name: "claude-code", Desc: "claro, crema y ladrillo", BG: "#F4EFE6", Text: "#2B2622", Dim: "#746B63", Strong: "#14110E",
		Accent: "#AC5435", OK: "#4E7839", Ref: "#3E6A8A", Crit: "#B3261E", Tick: "#9E8763", Label: "#FFFFFF"},
	{Name: "codex", Desc: "sobrio, toques verde agua", BG: "#000000", Text: "#D4D4D4", Dim: "#7A7A7A", Strong: "#FFFFFF",
		Accent: "#3CC4B4", OK: "#5FB39A", Ref: "#8FB7B2", Crit: "#E5534B", Tick: "#5A5A5A", Label: "#000000"},
	{Name: "grok", Desc: "minimalista, detalle dorado", BG: "#0B0B0B", Text: "#D9D9D9", Dim: "#808080", Strong: "#FFFFFF",
		Accent: "#D4A537", OK: "#9CC48A", Ref: "#7FA7E8", Crit: "#E06C6C", Tick: "#5E5E5E", Label: "#000000"},
	{Name: "gemini", Desc: "degradado azul, violeta y rosa", BG: "#1E1E2E", Text: "#CDD6F4", Dim: "#82879E", Strong: "#FFFFFF",
		Accent: "#B58BD6", OK: "#94E2D5", Ref: "#89B4FA", Crit: "#F38BA8", Tick: "#65678D", Label: "#000000",
		Gradient: []string{"#4796E4", "#867CCF", "#C46B82"}},
	// The nine palettes below come from github.com/basicScandal/claude-skins
	// (skins/, commit efa1dca), MIT License, Copyright (c) 2026 basicScandal.
	// Colors were adjusted for contrast; see THIRD_PARTY_NOTICES.md for the
	// full license text.
	{Name: "nebula", Desc: "violeta y naranja", BG: "#0E0520", Text: "#E0D6F0", Dim: "#A946F1", Strong: "#E0D6F0",
		Accent: "#FF6B35", OK: "#39FF14", Ref: "#B388FF", Crit: "#FF3D3D", Tick: "#7A2BDC", Label: "#000000"},
	{Name: "noir", Desc: "negro y crema", BG: "#0D0D0D", Text: "#F5E6C8", Dim: "#7A7A7A", Strong: "#F5E6C8",
		Accent: "#D4A857", OK: "#7A9E7E", Ref: "#8AA6A3", Crit: "#BC5E5E", Tick: "#5F5F5F", Label: "#000000"},
	{Name: "netrunner", Desc: "cian cyberpunk", BG: "#0A0A0A", Text: "#80CBC4", Dim: "#008A73", Strong: "#80CBC4",
		Accent: "#00E5FF", OK: "#69F0AE", Ref: "#00E5FF", Crit: "#FF1744", Tick: "#5D5D5D", Label: "#000000"},
	{Name: "grimoire", Desc: "pergamino y marrón", BG: "#1A120B", Text: "#D4C4A8", Dim: "#A27353", Strong: "#D4C4A8",
		Accent: "#C9A84C", OK: "#6B8E23", Ref: "#5F8A8B", Crit: "#C26337", Tick: "#825A37", Label: "#000000"},
	{Name: "mission-control", Desc: "ámbar sobre azul noche", BG: "#0B1120", Text: "#FFB000", Dim: "#557DBE", Strong: "#E8E8E8",
		Accent: "#FFB000", OK: "#33FF33", Ref: "#88AACC", Crit: "#FF3333", Tick: "#3060A9", Label: "#000000"},
	{Name: "mythos", Desc: "azul profundo y dorado", BG: "#0D1B3D", Text: "#E8EAF6", Dim: "#727FC9", Strong: "#E8EAF6",
		Accent: "#FFD700", OK: "#66BB6A", Ref: "#4DD0E1", Crit: "#EF5350", Tick: "#4D59DA", Label: "#000000"},
	{Name: "retro86", Desc: "Commodore 64", BG: "#40318D", Text: "#ACA6E5", Dim: "#ADA4E4", Strong: "#ADADAD",
		Accent: "#ACA6E5", OK: "#79BC79", Ref: "#70B8B8", Crit: "#E19A9A", Tick: "#8D80DA", Label: "#000000"},
	{Name: "brutalist", Desc: "monocromo puro", BG: "#000000", Text: "#FFFFFF", Dim: "#757575", Strong: "#FFFFFF",
		Accent: "#FFFFFF", OK: "#FFFFFF", Ref: "#AAAAAA", Crit: "#FFFFFF", Tick: "#5B5B5B", Label: "#000000"},
	{Name: "sensei", Desc: "claro, tinta japonesa", BG: "#F5F2EB", Text: "#2C2C2C", Dim: "#766D65", Strong: "#7E6B3F",
		Accent: "#C41E3A", OK: "#5B7553", Ref: "#587372", Crit: "#C41E3A", Tick: "#9B8A60", Label: "#FFFFFF"},
}

// All returns the 14 themes in presentation order.
func All() []Theme { return append([]Theme(nil), themes...) }

// ByName finds a theme by name.
func ByName(name string) (Theme, bool) {
	for _, t := range themes {
		if t.Name == name {
			return t, true
		}
	}
	return Theme{}, false
}
