package ui

import (
	"errors"
	"path/filepath"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/files"
)

// viewerFileKind says how the floating viewer shows a file's picture.
type viewerFileKind uint8

const (
	viewerNotImage viewerFileKind = iota
	// viewerBitmap is a picture the viewer draws but cannot edit.
	viewerBitmap
	// viewerSVG is a document the viewer draws and edits as source text.
	viewerSVG
)

// viewerFileKindFor classifies the picture formats the viewer draws: PNG,
// JPEG, GIF, WebP and BMP bitmaps, which it only shows, and SVG documents,
// which it shows in View and edits as source in Edit.
func viewerFileKindFor(path string) viewerFileKind {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		return viewerSVG
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
		return viewerBitmap
	}
	return viewerNotImage
}

// viewerFileContent is one file read for the card. Body is the source text,
// empty for a bitmap; Image is the picture drawn in View.
type viewerFileContent struct {
	Body  string
	Image mui.ImageSource
	// Data is the picture's bytes, for the enlarged preview's download.
	Data []byte
	// Editable reports that Body is the file's source text, which Edit can
	// write back.
	Editable bool
}

// loadViewerFile reads one file for the card off the UI thread. A bitmap is
// decoded and cannot be edited; an SVG is parsed for View and kept as source
// for Edit, so an unsaved edit can be seen before it is saved. A document
// MyGo cannot draw opens as its source, where it can be fixed, and one too
// large to edit is still shown as a picture.
func loadViewerFile(workspace, rel string) (viewerFileContent, error) {
	switch viewerFileKindFor(rel) {
	case viewerSVG:
		body, err := files.ReadText(workspace, rel)
		if err == nil {
			content := viewerFileContent{Body: body, Data: []byte(body), Editable: true}
			if svg, _ := parseViewerSVG([]byte(body)); svg != nil {
				content.Image = svg
			}
			return content, nil
		}
		if !errors.Is(err, files.ErrTextTooLarge) {
			return viewerFileContent{}, err
		}
		data, err := files.ReadImage(workspace, rel)
		if err != nil {
			return viewerFileContent{}, err
		}
		svg, err := parseViewerSVG(data)
		if err != nil {
			return viewerFileContent{}, err
		}
		return viewerFileContent{Image: svg, Data: data}, nil
	case viewerBitmap:
		data, err := files.ReadImage(workspace, rel)
		if err != nil {
			return viewerFileContent{}, err
		}
		bitmap, err := mui.DecodeBitmap(data)
		if err != nil {
			return viewerFileContent{}, err
		}
		return viewerFileContent{Image: bitmap, Data: data}, nil
	default:
		body, err := files.ReadText(workspace, rel)
		if err != nil {
			return viewerFileContent{}, err
		}
		return viewerFileContent{Body: body, Editable: true}, nil
	}
}

// parseViewerSVG parses a document the viewer draws. MyGo lays out nothing
// for a document without a size, so that is an error rather than a blank
// picture.
func parseViewerSVG(data []byte) (*mui.SVG, error) {
	svg, err := mui.ParseSVG(data)
	if err != nil {
		return nil, err
	}
	if w, h := svg.Size(); w <= 0 || h <= 0 {
		return nil, errors.New(T("The SVG has no size."))
	}
	return svg, nil
}

// viewerImage draws a picture in View mode: fitted inside the card, at its
// own size when it is smaller. A click opens it enlarged, as a click on an
// attachment does. An SVG being edited is parsed from its unsaved source, so
// View follows Edit before Save.
func (a *App) viewerImage(c *mui.Context, state *viewerCardState, spec viewerSpec) {
	src := spec.Image
	errText := ""
	usingUnsaved := false
	if spec.SVG && state.editor != nil && state.editor.Buffer.Modified() {
		text := state.editor.Buffer.String()
		if text != state.svgSrc || (state.svg == nil && state.svgErr == "") {
			state.svgSrc = text
			state.svg, state.svgErr = nil, ""
			if svg, err := parseViewerSVG([]byte(text)); err != nil {
				state.svgErr = err.Error()
			} else {
				state.svg = svg
			}
		}
		if state.svg != nil {
			src = state.svg
			usingUnsaved = true
		} else {
			errText = state.svgErr
		}
	}
	if errText != "" {
		centered(c, func() {
			dangerAlert(c, T("Cannot show this image"), singleLine(errText))
		})
		return
	}
	if src == nil {
		centered(c, func() {
			emptyState(c, imageIcon, T("No image to show"), T("This file has no picture."))
		})
		return
	}
	t := c.Theme()
	mui.Box(c).Grow(1).FillWidth().Padding(12).Center().
		Background(t.Surface).Children(func() {
		img := mui.Image(c, src).Fit(mui.ScaleDown).FillWidth().FillHeight().
			Label(T("Image")).Cursor(mui.CursorPointer).
			Tooltip(T("Click to view larger"))
		if img.Clicked() && spec.OnOpenImage != nil {
			data := spec.ImageData
			if usingUnsaved {
				// The unsaved document is what shows, so it is what the
				// preview downloads as well.
				data = []byte(state.svgSrc)
			}
			spec.OnOpenImage(src, data)
		}
	})
}
