// Theme holds the palette and per-pane styles, defined once. Colors adapt to the
// terminal background via lipgloss.LightDark. The palette is a "modern Charm"
// trading look — teal/cyan primary with coral + gold accents — deliberately off
// the default tutorial purple, with gradient titles and borders built from the
// lipgloss v2 blend primitives (Blend1D / BorderForegroundBlend).
package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme is the single source of styling for the whole TUI. In lipgloss v2 colors
// are image/color.Color values produced by lipgloss.Color(...); LightDarkFunc
// picks light/dark variants based on the detected terminal background.
type Theme struct {
	light lipgloss.LightDarkFunc

	// HasDarkBG records the detected terminal background so downstream renderers
	// (the glamour markdown style) can match the lipgloss light/dark adaptation.
	HasDarkBG bool

	// Palette.
	Accent    color.Color // primary (teal)
	AccentAlt color.Color // gradient companion to Accent (cyan)
	Coral     color.Color // secondary accent / alerts
	Gold      color.Color // highlights, tracked markers
	Violet    color.Color // provider / model identity accent
	Subtle    color.Color
	Up        color.Color
	Down      color.Color
	Text      color.Color
	Dim       color.Color
	BarFill   color.Color
	BarTrack  color.Color
	Surface   color.Color // faint panel/selection tint

	// AssetPalette is a generated color grid (Blend1D across vibrant stops) giving
	// each watchlist asset a stable identity color, indexed via AssetColor.
	AssetPalette []color.Color

	// Pane styles.
	Pane        lipgloss.Style // unfocused pane border
	PaneFocused lipgloss.Style // focused pane border (teal→cyan gradient)
	PaneTitle   lipgloss.Style // pane title text (accent, bold)

	// Status bar / footer.
	StatusBar lipgloss.Style
	StatusKey lipgloss.Style // the "HYPERAGENT" badge
	Label     lipgloss.Style // dim annotations and key hints

	// Settings modal chrome.
	TabActive   lipgloss.Style // the selected settings tab
	TabInactive lipgloss.Style
	KeySet      lipgloss.Style // "● configured" API-key state
	KeyUnset    lipgloss.Style // "○ not set" API-key state

	// Mode badges: propose is calm gold, autonomous is hot coral — the riskier
	// state should look riskier at a glance.
	BadgePropose    lipgloss.Style
	BadgeAutonomous lipgloss.Style

	// Markets table.
	TableHeader   lipgloss.Style
	TableSelected lipgloss.Style

	// Thesis section separator.
	ThesisBox lipgloss.Style
}

// titleStops are the two ends of every gradient title/border in the theme.
func (t Theme) titleStops() []color.Color { return []color.Color{t.Accent, t.AccentAlt} }

// NewTheme builds a theme adapted to whether the terminal background is dark.
func NewTheme(hasDarkBG bool) Theme {
	ld := lipgloss.LightDark(hasDarkBG)
	t := Theme{
		light:     ld,
		HasDarkBG: hasDarkBG,
		Accent:    lipgloss.Color("#2DD4BF"), // teal
		AccentAlt: lipgloss.Color("#38BDF8"), // sky/cyan
		Coral:     lipgloss.Color("#FF6B6B"),
		Gold:      lipgloss.Color("#F5C518"),
		Violet:    lipgloss.Color("#A78BFA"),
		Subtle:    ld(lipgloss.Color("#C8CAC4"), lipgloss.Color("#3A3D3A")),
		Up:        lipgloss.Color("#3DDC97"),
		Down:      lipgloss.Color("#FF5C7A"),
		Text:      ld(lipgloss.Color("#15201E"), lipgloss.Color("#E8EDEC")),
		Dim:       ld(lipgloss.Color("#8A938F"), lipgloss.Color("#6B7572")),
		BarFill:   lipgloss.Color("#2DD4BF"),
		BarTrack:  ld(lipgloss.Color("#DCE0DD"), lipgloss.Color("#242A28")),
		Surface:   ld(lipgloss.Color("#EAF7F4"), lipgloss.Color("#16201E")),
	}

	border := lipgloss.RoundedBorder()

	// Unfocused pane: a quiet, single-color rounded border.
	t.Pane = lipgloss.NewStyle().
		Border(border).
		BorderForeground(t.Subtle).
		Padding(0, 1)
	// Focused pane: a teal→cyan gradient border (the v2 blend primitive), so the
	// active column reads at a glance without a heavy fill.
	t.PaneFocused = lipgloss.NewStyle().
		Border(border).
		BorderForegroundBlend(t.Accent, t.AccentAlt).
		Padding(0, 1)
	t.PaneTitle = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	// Status / footer.
	statusBg := ld(lipgloss.Color("#DCE5E2"), lipgloss.Color("#1B2422"))
	t.StatusBar = lipgloss.NewStyle().Foreground(t.Text).Background(statusBg)
	t.StatusKey = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#06231F")).
		Background(t.Accent).
		Padding(0, 1).
		Bold(true)
	t.Label = lipgloss.NewStyle().Foreground(t.Dim)

	// Markets table.
	t.TableHeader = lipgloss.NewStyle().Foreground(t.Dim).Bold(true)
	t.TableSelected = lipgloss.NewStyle().Foreground(t.Text).Background(t.Surface).Bold(true)

	t.ThesisBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(t.Subtle).
		Foreground(t.Text).
		MarginTop(1)

	// Settings tabs: the active tab is an accent-filled chip, the rest dim text.
	t.TabActive = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#06231F")).
		Background(t.Accent).
		Padding(0, 1).
		Bold(true)
	t.TabInactive = lipgloss.NewStyle().Foreground(t.Dim).Padding(0, 1)

	t.KeySet = lipgloss.NewStyle().Foreground(t.Up)
	t.KeyUnset = lipgloss.NewStyle().Foreground(t.Dim)

	t.BadgePropose = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#231C02")).Background(t.Gold).Padding(0, 1).Bold(true)
	t.BadgeAutonomous = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#2B0A0A")).Background(t.Coral).Padding(0, 1).Bold(true)

	t.AssetPalette = assetGrid()
	return t
}

// ModeBadge renders the execution mode as a colored chip.
func (t Theme) ModeBadge(mode string) string {
	if mode == "autonomous" {
		return t.BadgeAutonomous.Render("AUTO")
	}
	return t.BadgePropose.Render("PROPOSE")
}

// Divider renders a dim horizontal rule of the given width.
func (t Theme) Divider(w int) string {
	if w < 1 {
		w = 1
	}
	return lipgloss.NewStyle().Foreground(t.Subtle).Render(strings.Repeat("─", w))
}

// KeyHints renders a "key description · key description" hint line: keys in the
// accent color, descriptions dim — the inline navigation helper used by every
// overlay footer and the status bar.
func (t Theme) KeyHints(pairs [][2]string) string {
	key := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	sep := t.Label.Render(" · ")
	parts := make([]string, 0, len(pairs))
	for _, p := range pairs {
		parts = append(parts, key.Render(p[0])+" "+t.Label.Render(p[1]))
	}
	return strings.Join(parts, sep)
}

// SignColor returns the up or down color for a signed value.
func (t Theme) SignColor(v float64) color.Color {
	if v < 0 {
		return t.Down
	}
	return t.Up
}

// AssetColor returns a stable identity color for the asset at watchlist index i,
// wrapping the generated palette so any watchlist length is covered.
func (t Theme) AssetColor(i int) color.Color {
	if len(t.AssetPalette) == 0 {
		return t.Accent
	}
	if i < 0 {
		i = -i
	}
	return t.AssetPalette[i%len(t.AssetPalette)]
}

// assetGrid builds the per-asset identity palette by blending between vibrant stops
// (Blend1D) so adjacent assets are visually distinct yet the whole set stays
// harmonious with the teal/coral/gold theme.
func assetGrid() []color.Color {
	stops := []color.Color{
		lipgloss.Color("#2DD4BF"), // teal
		lipgloss.Color("#38BDF8"), // sky
		lipgloss.Color("#A78BFA"), // violet
		lipgloss.Color("#FF6B6B"), // coral
		lipgloss.Color("#F5C518"), // gold
		lipgloss.Color("#3DDC97"), // green
		lipgloss.Color("#2DD4BF"), // wrap back to teal for a seamless cycle
	}
	const perSegment = 4
	var grid []color.Color
	for i := 0; i+1 < len(stops); i++ {
		seg := lipgloss.Blend1D(perSegment+1, stops[i], stops[i+1])
		grid = append(grid, seg[:perSegment]...) // drop the shared endpoint
	}
	return grid
}
