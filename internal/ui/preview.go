package ui

import (
	"encoding/base64"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
)

const (
	// The enlarged preview's zoom: 1 shows the picture fitted to the
	// window. A wheel zooms by previewZoomWheel per DIP it scrolls, about
	// 10% a notch, and the buttons by previewZoomStep.
	previewZoomMin   = 0.25
	previewZoomMax   = 6
	previewZoomWheel = 0.0024
	previewZoomStep  = 1.25

	// previewSharpStep is the step between the scales a gesture draws the SVG
	// at: a step or two, not a new picture every frame, which is what makes
	// zooming stutter. previewSharpMax bounds the picture MyGo makes, as it
	// clamps a raster over 4096 pixels a side to nothing: 2048 DIPs, holding
	// a 2x display's pixels under that.
	previewSharpStep = 1.25
	previewSharpMax  = 2048
	// previewSharpDelay is how long the zoom rests before the SVG is drawn
	// again at the zoom shown, sharp.
	previewSharpDelay = 180 * time.Millisecond
)

// imageView is the enlarged preview's view of its picture: the scale, 1
// showing it fitted to the window, and where the picture's top-left corner
// sits in the box that clips it, in DIPs from the box's corner.
//
// Sharp is the scale the SVG is drawn at, close to Zoom so that a step or
// two of the wheel redraw it rather than every frame; it equals Zoom once
// the zoom rests, so the picture ends up sharp. Zooming shows the picture
// drawn at Sharp, scaled by Zoom/Sharp, which is cheap.
type imageView struct {
	Zoom  float32
	Sharp float32
	X, Y  float32
	// pending is when the zoom last changed, timing the sharpening.
	pending time.Time
}

// fit shows the whole picture again.
func (v *imageView) fit() {
	*v = imageView{Zoom: 1, Sharp: 1}
}

// previewSharpLimit returns the largest scale the picture can be drawn at
// within the room it has: MyGo refuses pictures over 4096 pixels a side, so
// a 2x display allows 2048 DIPs.
func previewSharpLimit(boxW, boxH float32) float32 {
	side := max(boxW, boxH)
	if side <= 0 {
		return previewZoomMax
	}
	return previewSharpMax / side
}

// previewRasterZoom returns the scale to draw the SVG at for the zoom shown:
// the nearest step of previewSharpStep, within what the box can have drawn.
// A gesture changes it a few times, at most, rather than on every frame.
func previewRasterZoom(zoom, boxW, boxH float32) float32 {
	if zoom <= 0 {
		return 1
	}
	step := float32(math.Pow(previewSharpStep,
		math.Round(math.Log(float64(zoom))/math.Log(previewSharpStep))))
	return max(min(step, previewSharpLimit(boxW, boxH)), 0.05)
}

// openImagePreview shows the enlarged preview, fitted to the window again and
// without the last download's result.
func (a *App) openImagePreview() {
	a.ImageView.fit()
	a.ImageNotice, a.ImageNoticeErr = "", false
	a.ImageOpen = true
}

// previewSource is what the enlarged preview shows: an attachment's bitmap or
// a Mermaid diagram's SVG, and its intrinsic size in DIPs.
type previewSource struct {
	Image         mui.ImageSource
	Width, Height float32
}

// previewSource returns the picture the enlarged preview shows: the Mermaid
// diagram a click opened, or the attachment one did.
func (a *App) previewSource() (previewSource, bool) {
	if d := a.DiagramPreview; d != nil && d.svg != nil && d.width > 0 && d.height > 0 {
		return previewSource{Image: d.svg, Width: d.width, Height: d.height}, true
	}
	attachment := a.ImagePreview
	if attachment == nil || attachment.Bitmap == nil {
		return previewSource{}, false
	}
	w, h := mui.ImageSize(attachment.Bitmap)
	if w <= 0 || h <= 0 {
		return previewSource{}, false
	}
	return previewSource{Image: attachment.Bitmap, Width: w, Height: h}, true
}

// previewFile returns the file a download writes: the SVG the diagram was
// drawn as, or the bytes the attachment came with, and a name for it.
func (a *App) previewFile() (name string, data []byte, ok bool) {
	if d := a.DiagramPreview; d != nil && len(d.source) > 0 {
		return "diagram.svg", d.source, true
	}
	attachment := a.ImagePreview
	if attachment == nil || attachment.Data == "" {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(attachment.Data)
	if err != nil || len(data) == 0 {
		return "", nil, false
	}
	name = attachment.Name
	if name == "" {
		name = "image.png"
	}
	return name, data, true
}

// canDownloadPreview reports whether a download has something to write,
// without decoding the attachment's bytes on every frame.
func (a *App) canDownloadPreview() bool {
	if d := a.DiagramPreview; d != nil {
		return len(d.source) > 0
	}
	return a.ImagePreview != nil && a.ImagePreview.Data != ""
}

// imagePreviewDialog shows the selected picture enlarged in a modal: an
// attachment's bitmap, or a Mermaid diagram's SVG opened by clicking the
// diagram. The picture is capped at its own size so a small image is not
// turned into a blurry wall, and at the window's room so it always fits. The
// wheel zooms in on the point under the pointer, the pointer drags the
// enlarged picture around, and the row below downloads it or closes the
// modal.
func (a *App) imagePreviewDialog(c *mui.Context) {
	if !a.ImageOpen {
		return
	}
	src, ok := a.previewSource()
	if !ok {
		return
	}
	windowW, windowH := c.Size()
	maxW := max(float32(120), windowW-120)
	maxH := max(float32(120), windowH-220)
	scale := min(maxW/src.Width, maxH/src.Height)
	if scale > 1 {
		scale = 1
	}
	boxW, boxH := src.Width*scale, src.Height*scale
	if a.ImageView.Zoom <= 0 || a.ImageView.Sharp <= 0 {
		a.ImageView.fit()
	}
	// DialogBase rather than Modal: keying the backdrop keeps the preview's
	// state when another overlay comes or goes, whose place would otherwise
	// change the preview's ids and cancel the press a click begins with.
	t := c.Theme()
	mui.DialogBase(c, &a.ImageOpen, func(backdrop, panel mui.Element) {
		backdrop.Key("image-preview-backdrop").Background(mui.RGBA(0, 0, 0, 0.4))
		panel.Key("image-preview").
			Padding(t.Space(5)).Gap(t.Space(3)).Radius(t.Space(2.5)).Background(t.Background).
			MaxWidth(windowW-t.Space(10)).MaxHeight(windowH-t.Space(10)).
			Shadow(0, 10, 30, 0, mui.RGBA(0, 0, 0, 0.3))
		panel.Children(func() {
			a.previewView(c, src, boxW, boxH)
			a.previewToolbar(c, boxW, boxH)
			if a.ImageNotice != "" {
				col := t.TextMuted
				if a.ImageNoticeErr {
					col = t.Danger
				}
				mui.Text(c, a.ImageNotice).FontSize(10).TextColor(col)
			}
		})
	})
	if !a.ImageOpen {
		a.ImagePreview = nil
		a.DiagramPreview = nil
		a.ImageNotice, a.ImageNoticeErr = "", false
	}
}

// previewView is the box the picture shows in: it clips the picture, takes
// the wheel and the pinch that zoom it, and the pointer that drags it around
// while it is larger than the box. The box keeps its size as the picture
// zooms, so its label, the pointer and tests stay on what shows.
func (a *App) previewView(c *mui.Context, src previewSource, boxW, boxH float32) {
	view := &a.ImageView
	area := mui.Box(c).Size(boxW, boxH).Clip().Role(mui.RoleImage).Label(T("Image preview"))
	if view.Zoom > 1 {
		cursor := mui.CursorGrab
		if area.Dragging() {
			cursor = mui.CursorGrabbing
		}
		area.Cursor(cursor)
	}
	if dx, dy, ok := area.Dragged(); ok && (dx != 0 || dy != 0) {
		view.X += dx
		view.Y += dy
	}
	area.HandleInput(func(ev mui.InputEvent) bool {
		switch ev.Kind {
		case mui.InputScroll:
			if ev.DY == 0 {
				return false
			}
			if ev.Momentum {
				// The fingers left; a fling must not keep zooming.
				return true
			}
			step := float32(math.Exp(-float64(ev.DY) * previewZoomWheel))
			a.zoomPreview(step, boxW, boxH, ev.X, ev.Y)
			return true
		case mui.InputZoom:
			a.zoomPreview(ev.Scale, boxW, boxH, ev.X, ev.Y)
			return true
		}
		return false
	})
	area.Children(func() {
		a.previewPicture(c, src, boxW, boxH)
	})
}

// previewPicture draws the picture at the scale it was last drawn at, scaled
// to the zoom shown, and places it in the box: centered while it is smaller,
// and otherwise no further than its edges. It places the picture every frame,
// so a resize, a drag or a zoom that the buttons made keeps it within the box.
func (a *App) previewPicture(c *mui.Context, src previewSource, boxW, boxH float32) {
	view := &a.ImageView
	a.sharpenPreview(c, boxW, boxH)
	view.Sharp = min(view.Sharp, previewSharpLimit(boxW, boxH))
	w, h := boxW*view.Zoom, boxH*view.Zoom
	if w <= boxW {
		view.X = (boxW - w) / 2
	} else {
		view.X = min(0, max(boxW-w, view.X))
	}
	if h <= boxH {
		view.Y = (boxH - h) / 2
	} else {
		view.Y = min(0, max(boxH-h, view.Y))
	}
	f := view.Zoom / view.Sharp
	mui.Image(c, src.Image).Size(boxW*view.Sharp, boxH*view.Sharp).Fit(mui.Contain).Radius(6).
		Absolute().Left(view.X).Top(view.Y).
		TransformOrigin(0, 0).Scale(f, f)
}

// sharpenPreview draws the picture again at the zoom shown once it rests. A
// gesture shows it drawn at a nearby step, which moves cheaply; this makes
// the still picture exact.
func (a *App) sharpenPreview(c *mui.Context, boxW, boxH float32) {
	view := &a.ImageView
	if view.pending.IsZero() {
		return
	}
	if wait := previewSharpDelay - time.Since(view.pending); wait > 0 {
		c.After(wait)
		return
	}
	view.pending = time.Time{}
	view.Sharp = min(view.Zoom, previewSharpLimit(boxW, boxH))
}

// zoomPreview scales the preview by factor around the point at (cx, cy) in
// the box, so that the picture's point under the pointer stays under it.
func (a *App) zoomPreview(factor, boxW, boxH, cx, cy float32) {
	view := &a.ImageView
	if factor <= 0 || boxW <= 0 || boxH <= 0 || view.Zoom <= 0 {
		return
	}
	zoom := min(max(view.Zoom*factor, previewZoomMin), previewZoomMax)
	if zoom == view.Zoom {
		return
	}
	// The picture's fraction at the pointer stays where the pointer is.
	fx := (cx - view.X) / (boxW * view.Zoom)
	fy := (cy - view.Y) / (boxH * view.Zoom)
	view.Zoom = zoom
	view.X = cx - fx*boxW*zoom
	view.Y = cy - fy*boxH*zoom
	view.Sharp = previewRasterZoom(zoom, boxW, boxH)
	view.pending = time.Now()
}

// previewToolbar is the modal's action row: the zoom buttons and the current
// percentage, which resets it, then Download and Close.
func (a *App) previewToolbar(c *mui.Context, boxW, boxH float32) {
	view := &a.ImageView
	mui.Row(c).Gap(6).AlignItems(mui.Center).Justify(mui.End).Children(func() {
		if previewGlyphButton(c, "−", T("Zoom out")) {
			a.zoomPreview(1/previewZoomStep, boxW, boxH, boxW/2, boxH/2)
		}
		percent := Tf("%d%%", int(math.Round(float64(view.Zoom*100))))
		if quietButton(c, percent).Tooltip(T("Reset zoom")).Clicked() {
			view.fit()
		}
		if previewGlyphButton(c, "+", T("Zoom in")) {
			a.zoomPreview(previewZoomStep, boxW, boxH, boxW/2, boxH/2)
		}
		if a.canDownloadPreview() && quietButton(c, T("Download")).Clicked() {
			a.downloadPreview()
		}
		if quietButton(c, T("Close")).Clicked() {
			a.ImageOpen = false
		}
	})
}

// previewGlyphButton is a compact action of the preview: the quiet button's
// look at the toolbar's size, with its glyph as text and a full label for
// assistive technology and tests.
func previewGlyphButton(c *mui.Context, glyph, label string) bool {
	t := c.Theme()
	b := buttonBase(c, label).Size(26, 22).Center().Tooltip(label)
	fill := t.Background
	switch {
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	b.Background(fill).TextColor(t.Text)
	b.Children(func() { mui.Text(c, glyph) })
	return b.Clicked()
}

// downloadPreview saves the picture to the file the user picks. The native
// dialog blocks until it is answered, so it runs off the UI thread; the
// result shows under the toolbar.
func (a *App) downloadPreview() {
	name, data, ok := a.previewFile()
	if !ok {
		return
	}
	a.ImageNotice, a.ImageNoticeErr = "", false
	win := a.window()
	go func() {
		opts := mygo.SaveDialogOptions{
			Parent:      win,
			Title:       T("Save image"),
			DefaultPath: name,
			ButtonLabel: T("Save"),
		}
		if ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), "."); ext != "" {
			opts.Filters = []mygo.FileFilter{{Name: strings.ToUpper(ext), Extensions: []string{ext}}}
		}
		path, err := mygo.Dialog.Save(opts)
		if err == nil && path != "" {
			err = os.WriteFile(path, data, 0o644)
		}
		switch {
		case err != nil:
			a.Post(func() {
				a.ImageNotice, a.ImageNoticeErr = Tf("Could not save: %s", err), true
			})
		case path != "":
			a.Post(func() { a.ImageNotice = Tf("Saved to %s", path) })
		}
	}()
}
