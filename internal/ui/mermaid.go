package ui

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	mermaid "github.com/zkrebbekx/go-mermaid"
)

const (
	// mermaidCacheLimit bounds the parsed diagrams kept for redraws. A frame
	// rebuilds every visible Markdown view, so a diagram is parsed and drawn
	// once per source, font size and theme rather than once per frame.
	mermaidCacheLimit = 32
	// mermaidThemeName names the palette handed to go-mermaid for this
	// window's theme. go-mermaid keeps palettes in a package map; a mutex
	// around Register + Render keeps two app instances or tests from
	// swapping the palette while one renders.
	mermaidThemeName = "acp-gui"
	// mermaidMaxSource keeps a pathological fence from blocking the UI
	// thread; a diagram this large is better read as source.
	mermaidMaxSource = 256 << 10
)

var mermaidRenderMu sync.Mutex

// mermaidDiagram is a parsed diagram and its intrinsic size in DIPs.
type mermaidDiagram struct {
	svg           *mui.SVG
	width, height float32
}

// mermaidCache keeps the last diagrams as parsed SVGs. It is keyed by the
// source, label size and colors, so changing theme or font does not redraw
// stale ones.
type mermaidCache struct {
	mu      sync.Mutex
	entries map[string]mermaidDiagram
	order   []string
}

func (c *mermaidCache) get(key string) (mermaidDiagram, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	d, ok := c.entries[key]
	return d, ok
}

func (c *mermaidCache) put(key string, d mermaidDiagram) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]mermaidDiagram)
	}
	if _, ok := c.entries[key]; ok {
		c.entries[key] = d
		return
	}
	for len(c.order) >= mermaidCacheLimit {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, old)
	}
	c.entries[key] = d
	c.order = append(c.order, key)
}

// installMermaidHooks gives the chat and the viewer's Markdown views the
// mermaid code-block renderer. It is idempotent and runs before every frame,
// so apps built without New (tests) still draw diagrams.
func (a *App) installMermaidHooks() {
	if a == nil {
		return
	}
	if a.MD.Code == nil {
		a.MD.Code = make(map[string]func(*mui.Context, *markdown.CodeBlockView), 1)
	}
	if _, ok := a.MD.Code["mermaid"]; !ok {
		a.MD.Code["mermaid"] = a.chatMermaidBlock
	}
	if a.TaskMD.Code == nil {
		a.TaskMD.Code = make(map[string]func(*mui.Context, *markdown.CodeBlockView), 1)
	}
	if _, ok := a.TaskMD.Code["mermaid"]; !ok {
		a.TaskMD.Code["mermaid"] = a.viewerMermaidBlock
	}
}

// chatMermaidBlock draws Mermaid blocks in an agent's reply. Labels start
// from the chat body size, a little smaller so a diagram does not dominate
// the conversation.
func (a *App) chatMermaidBlock(c *mui.Context, b *markdown.CodeBlockView) {
	a.mermaidBlock(c, b, a.chatBodyFont(c)*0.9)
}

// viewerMermaidBlock draws Mermaid blocks in the floating viewer and the
// task summary, matching the right panel's font scale.
func (a *App) viewerMermaidBlock(c *mui.Context, b *markdown.CodeBlockView) {
	a.mermaidBlock(c, b, a.gitFont(c.Theme().FontSize))
}

// mermaidBlock draws a closed mermaid fence as a diagram and leaves code for
// a block still streaming in, an empty block, an unsupported kind or a
// source the renderer cannot parse. The source stays one click away through
// the normal code block in those cases.
func (a *App) mermaidBlock(c *mui.Context, b *markdown.CodeBlockView, fontSize float32) {
	if b.Open {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	src := b.Code()
	if strings.TrimSpace(src) == "" || len(src) > mermaidMaxSource {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	d, ok := a.mermaid(src, fontSize, c.Theme())
	if !ok {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	img := mui.Image(c, d.svg).Label(T("Mermaid diagram"))
	if d.width > 0 && d.height > 0 {
		// A column would stretch the image to its width. Give the diagram
		// its natural size, and let the max width shrink a wide one to the
		// Markdown column.
		img.Width(d.width).MaxWidthPercent(100).AspectRatio(d.width / d.height)
	}
}

// mermaid returns a parsed diagram from the cache, drawing it on first use.
func (a *App) mermaid(src string, fontSize float32, t *mui.Theme) (mermaidDiagram, bool) {
	if fontSize <= 0 {
		fontSize = 12
	}
	pal := mermaidPalette(t)
	key := mermaidKey(src, fontSize, pal)
	if d, ok := a.mermaidCache.get(key); ok {
		return d, true
	}
	out, err := renderMermaid(src, fontSize, pal)
	if err != nil {
		return mermaidDiagram{}, false
	}
	svg, err := mui.ParseSVG(out)
	if err != nil {
		return mermaidDiagram{}, false
	}
	w, h := svg.Size()
	d := mermaidDiagram{svg: svg, width: w, height: h}
	a.mermaidCache.put(key, d)
	return d, true
}

// renderMermaid draws with go-mermaid's built-in SVG output. The palette
// follows the window theme, so diagrams read as part of the app rather than
// as pasted pictures.
func renderMermaid(src string, fontSize float32, pal mermaid.Palette) ([]byte, error) {
	mermaidRenderMu.Lock()
	defer mermaidRenderMu.Unlock()
	return mermaid.Render(src,
		mermaid.WithCustomTheme(mermaidThemeName, pal),
		mermaid.WithFont("sans-serif", float64(fontSize)),
		mermaid.WithPadding(12),
	)
}

// mermaidPalette derives a diagram palette from the window theme: nodes in a
// wash of the accent, edges in a muted text color, on the window background.
func mermaidPalette(t *mui.Theme) mermaid.Palette {
	bg := t.Background
	if bg.A == 0 {
		bg = mui.RGB(0xff, 0xff, 0xff)
		if t.Dark {
			bg = mui.RGB(0x1b, 0x1f, 0x24)
		}
	}
	accent := t.Accent
	if accent.A == 0 {
		accent = mui.RGB(0x63, 0x66, 0xf1)
	}
	return mermaid.Palette{
		Background: colorHex(bg),
		NodeFill:   colorHex(mixColor(bg, accent, 0.10)),
		NodeStroke: colorHex(mixColor(bg, accent, 0.62)),
		Text:       colorHex(t.Text),
		Edge:       colorHex(mixColor(bg, t.Text, 0.62)),
	}
}

// mermaidKey is a digest of everything that changes the drawing: the source,
// label size and the six palette entries.
func mermaidKey(src string, fontSize float32, pal mermaid.Palette) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%.2f\x00%s\x00%s\x00%s\x00%s\x00%s",
		src, fontSize, pal.Background, pal.NodeFill, pal.NodeStroke, pal.Text, pal.Edge)))
	return string(sum[:])
}

func colorHex(c mui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

func mixColor(a, b mui.Color, t float32) mui.Color {
	mix := func(x, y uint8) uint8 {
		return uint8(float32(x)*(1-t) + float32(y)*t + 0.5)
	}
	return mui.RGB(mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B))
}
