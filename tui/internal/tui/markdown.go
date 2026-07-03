// Markdown rendering for model output, via glamour — the engine behind charm's
// glow. Everything the LLM writes (chat replies, proactive theses, the detail
// pane's thesis and agent read) is markdown-rendered, so headings, lists, code
// and emphasis arrive styled instead of as raw asterisks. Plain transcripts
// (user turns, command echoes) stay plain.
package tui

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// mdState holds the glamour renderers and the render cache. Renderers are bound
// to a wrap width at construction, and the TUI renders model output at two
// widths (chat pane, detail column) that change on resize — hence a small
// per-width pool. The cache matters because refreshChat re-walks the whole
// transcript on every chat event; without it each keystroke-triggered refresh
// would re-render every historical turn.
type mdState struct {
	renderers map[int]*glamour.TermRenderer
	cache     map[string]string
}

// chatStyle adapts glow's stock dark/light style to an embedded pane: the
// document margin and surrounding blank lines are stripped (the pane supplies
// its own padding), everything else — headings, code blocks, lists, quotes —
// stays glow.
func chatStyle(dark bool) ansi.StyleConfig {
	cfg := styles.LightStyleConfig
	if dark {
		cfg = styles.DarkStyleConfig
	}
	margin := uint(0)
	cfg.Document.Margin = &margin
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	return cfg
}

// renderMarkdown renders model-authored markdown to styled terminal output at
// the given wrap width. Any failure falls back to the raw text — a rendering
// hiccup must never eat a trading thesis.
func (m *Model) renderMarkdown(text string, width int) string {
	text = strings.TrimSpace(text)
	if text == "" || width < 8 {
		return text
	}
	if m.md.renderers == nil {
		m.md.renderers = make(map[int]*glamour.TermRenderer)
		m.md.cache = make(map[string]string)
	}

	key := fmt.Sprintf("%d\x00%s", width, text)
	if out, ok := m.md.cache[key]; ok {
		return out
	}

	r, ok := m.md.renderers[width]
	if !ok {
		var err error
		r, err = glamour.NewTermRenderer(
			glamour.WithStyles(chatStyle(m.theme.HasDarkBG)),
			glamour.WithWordWrap(width),
			glamour.WithEmoji(),
		)
		if err != nil {
			return text
		}
		m.md.renderers[width] = r
	}

	out, err := r.Render(text)
	if err != nil {
		return text
	}
	out = strings.Trim(out, "\n")
	m.md.cache[key] = out
	return out
}
