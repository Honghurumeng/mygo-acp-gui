package ui

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/files"
)

const (
	// maxToolImages bounds the pictures one card decodes, matching the
	// composer's attachment limit. A tool that returns a gallery does not
	// decode all of it.
	maxToolImages = 4
	// maxToolImageBytes is the largest picture a card decodes, matching the
	// Files viewer's and the composer's limit.
	maxToolImageBytes = files.MaxImageSize
	// A picture is shown at its own size, shrunk into this box, and small
	// pictures get a box they can be clicked in.
	toolImageMaxWidth  float32 = 260
	toolImageMaxHeight float32 = 200
	toolImageMinSide   float32 = 48
)

var (
	errToolImageBase64      = errors.New("image data is not valid base64")
	errToolImageNoPath      = errors.New("tool reported no image")
	errToolImageNoWorkspace = errors.New("no workspace for this image")
)

// toolImageItem is one decoded picture of a tool call. src is nil when the
// picture could not be read or decoded, and err says why.
type toolImageItem struct {
	src  mui.ImageSource
	data []byte
	name string
	err  string
}

// toolImageCache is the decoded form of a card's ToolImages. key fingerprints
// the source list, so a later update with different pictures starts again
// instead of showing the old ones.
type toolImageCache struct {
	key   string
	busy  bool
	items []toolImageItem
}

// toolImagePanel draws the pictures a tool call returned or looked at. The
// read happens off the UI thread; until it finishes a spinner keeps the card's
// height stable. Clicking a picture opens the same enlarged preview an
// attachment uses.
func (a *App) toolImagePanel(c *mui.Context, m *Message) {
	if m == nil || len(m.ToolImages) == 0 {
		return
	}
	cache := a.ensureToolImages(m)
	if len(cache.items) == 0 {
		if cache.busy {
			busyLine(c, T("Loading image…"))
			return
		}
		mutedText(c, T("Cannot show this image"), a.chatFont(11))
		return
	}
	mui.Row(c).Gap(8).Wrap().AlignItems(mui.Start).Children(func() {
		for i := range cache.items {
			item := cache.items[i]
			c.Key(i)
			if item.src == nil {
				mutedText(c, Tf("Cannot show this image: %s", item.err), a.chatFont(10))
				continue
			}
			width, height := toolImageThumbSize(item.src)
			image := mui.Image(c, item.src).
				Size(width, height).
				Fit(mui.Contain).
				Radius(6).
				Cursor(mui.CursorPointer).
				Label(T("View image in tool call")).
				Tooltip(T("Click to view larger"))
			if image.Clicked() {
				a.openViewerImagePreview(item.name, item.src, item.data)
			}
		}
	})
}

// ensureToolImages returns the card's decoded pictures, starting the read on
// first use. A cache is started once per source list and workspace, so redraws
// while the read runs do not start another.
func (a *App) ensureToolImages(m *Message) *toolImageCache {
	workspace := a.activeFileWorkspace()
	key := toolImageCacheKey(workspace, m.ToolImages)
	if cache := m.toolImageCache; cache != nil && cache.key == key {
		return cache
	}
	cache := &toolImageCache{key: key, busy: true}
	m.toolImageCache = cache
	images := append([]ToolImage(nil), m.ToolImages...)
	go func() {
		items := make([]toolImageItem, 0, len(images))
		for _, image := range images {
			items = append(items, loadToolImage(workspace, image))
		}
		a.Post(func() {
			if m.toolImageCache != cache {
				return
			}
			cache.items = items
			cache.busy = false
		})
	}()
	return cache
}

// toolImageCacheKey identifies the pictures a cache was built from. The data's
// length is enough: a new image that differs only in bytes is rare, and the
// presentation step drops the cache whenever ToolImages changes.
func toolImageCacheKey(workspace string, images []ToolImage) string {
	var b strings.Builder
	b.WriteString(workspace)
	for _, image := range images {
		b.WriteByte(0)
		b.WriteString(image.Path)
		b.WriteByte(0)
		b.WriteString(image.MimeType)
		b.WriteByte(0)
		b.WriteString(image.Name)
		b.WriteByte(0)
		b.WriteString(strconv.Itoa(len(image.Data)))
	}
	return b.String()
}

// loadToolImage reads and decodes one picture. Embedded bytes win over a path:
// they are what the agent actually returned. A path is read for codex's
// view_image and pi's image read, where only the file is reported.
func loadToolImage(workspace string, image ToolImage) toolImageItem {
	name := strings.TrimSpace(image.Name)
	if strings.ContainsAny(name, `/\\`) || filepath.IsAbs(name) {
		name = filepath.Base(name)
	}
	if name == "" && image.Path != "" {
		name = filepath.Base(image.Path)
	}
	if name == "" {
		name = "image.png"
	}
	item := toolImageItem{name: name}
	data, err := readToolImageData(workspace, image)
	if err != nil {
		item.err = toolImageErrorText(err)
		return item
	}
	if len(data) == 0 {
		item.err = T("The image is empty.")
		return item
	}
	if len(data) > maxToolImageBytes {
		item.err = T("The image is too large to show.")
		return item
	}
	item.data = data
	if imageIsSVG(name, image.MimeType) {
		svg, err := parseViewerSVG(data)
		if err != nil {
			item.err = toolImageErrorText(err)
			return item
		}
		item.src = svg
		return item
	}
	bitmap, err := mui.DecodeBitmap(data)
	if err != nil {
		item.err = toolImageErrorText(err)
		return item
	}
	item.src = bitmap
	return item
}

func readToolImageData(workspace string, image ToolImage) ([]byte, error) {
	if image.Data != "" {
		data, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil {
			return nil, errToolImageBase64
		}
		return data, nil
	}
	path := toolImagePath(image.Path)
	if path == "" {
		return nil, errToolImageNoPath
	}
	if filepath.IsAbs(path) {
		return files.ReadImageFile(path)
	}
	if strings.TrimSpace(workspace) == "" || !filepath.IsAbs(workspace) {
		return nil, errToolImageNoWorkspace
	}
	return files.ReadImage(workspace, path)
}

func imageIsSVG(name, mimeType string) bool {
	return viewerFileKindFor(name) == viewerSVG ||
		strings.EqualFold(strings.TrimSpace(mimeType), "image/svg+xml")
}

// toolImageThumbSize fits a picture into the card's thumbnail box. A tiny
// picture still gets a clickable box; it is drawn contained in it.
func toolImageThumbSize(src mui.ImageSource) (float32, float32) {
	width, height := mui.ImageSize(src)
	if width <= 0 || height <= 0 {
		return 160, 120
	}
	scale := min(toolImageMaxWidth/width, toolImageMaxHeight/height)
	if scale > 1 {
		scale = 1
	}
	width *= scale
	height *= scale
	if width < toolImageMinSide {
		width = toolImageMinSide
	}
	if height < toolImageMinSide {
		height = toolImageMinSide
	}
	return width, height
}

func toolImageErrorText(err error) string {
	switch {
	case errors.Is(err, errToolImageBase64):
		return T("The image data is not valid base64.")
	case errors.Is(err, errToolImageNoPath):
		return T("The tool reported no image.")
	case errors.Is(err, errToolImageNoWorkspace):
		return T("There is no workspace to read this image from.")
	case errors.Is(err, files.ErrImageTooLarge):
		return T("The image is too large to show.")
	case errors.Is(err, files.ErrSymlink):
		return T("The image is a symbolic link.")
	case errors.Is(err, files.ErrOutsideWorkspace):
		return T("The image is outside the workspace.")
	}
	return fileErrorText(err)
}

// ToolImage is a picture a tool call returned or looked at. Data is the ACP
// image block's base64 when the agent embedded it; Path names the file the
// agent viewed when it did not. Both may be set, in which case Data is shown.
type ToolImage struct {
	Path     string
	Name     string
	MimeType string
	Data     string
}

// toolContentImages reads the pictures of one content block. ACP wraps tool
// results in a content block, so both a direct image/resource_link and the
// same block nested under "content" are accepted.
func toolContentImages(block toolContentBlock) []ToolImage {
	if images := imagesFromContentBlock(block); len(images) > 0 || block.Type != "content" || len(block.Content) == 0 {
		return images
	}
	var inner toolContentBlock
	if json.Unmarshal(block.Content, &inner) != nil {
		return nil
	}
	return imagesFromContentBlock(inner)
}

// imagesFromContentBlock maps an image or resource-link block onto ToolImage.
// A resource link only counts when it names a picture: links also describe
// attachments that are not images.
func imagesFromContentBlock(block toolContentBlock) []ToolImage {
	switch block.Type {
	case "image":
		image := ToolImage{
			Path:     toolImagePath(block.URI),
			Name:     block.Name,
			MimeType: block.MimeType,
			Data:     block.Data,
		}
		if image.Data != "" && !toolImageDataFits(image.Data) {
			image.Data = ""
		}
		if image.Data == "" && image.Path == "" {
			return nil
		}
		if image.MimeType != "" && !strings.HasPrefix(strings.ToLower(image.MimeType), "image/") {
			return nil
		}
		return []ToolImage{image}
	case "resource_link", "resource":
		path := toolImagePath(firstNonEmptyString(block.URI, block.Path))
		mime := strings.ToLower(block.MimeType)
		if !toolImageFile(path) && !strings.HasPrefix(mime, "image/") {
			return nil
		}
		if path == "" {
			return nil
		}
		return []ToolImage{{Path: path, Name: block.Name, MimeType: block.MimeType}}
	}
	return nil
}

// toolCallPathImages recognizes a tool call that looked at a picture without
// embedding it: codex's view_image reports only the file's path, and pi's read
// of an image file arrives the same way. Only read/other calls are considered,
// so an edit that happens to name a .png stays a diff.
func toolCallPathImages(kind string, paths []string, input toolRawInput) []ToolImage {
	switch kind {
	case "read", "other", "":
	default:
		return nil
	}
	images := toolImagesFromPaths(paths)
	if len(images) == 0 {
		// locations already hold the absolute path; rawInput is the fallback
		// for an agent that sends only the arguments.
		images = toolImagesFromPaths([]string{input.Path, input.FilePath})
	}
	return images
}

func toolImagesFromPaths(paths []string) []ToolImage {
	var images []ToolImage
	seen := make(map[string]bool, len(paths))
	for _, candidate := range paths {
		path := toolImagePath(candidate)
		if path == "" || seen[path] || !toolImageFile(path) {
			continue
		}
		seen[path] = true
		images = append(images, ToolImage{Path: path, Name: filepath.Base(path)})
		if len(images) >= maxToolImages {
			break
		}
	}
	return images
}

// toolImageResultJSON reports whether text is the JSON dump pi-acp makes of a
// tool result that had no text blocks, which is how an image-only result is
// sent. The picture replaces it; the base64 is not worth showing. The shape is
// checked (`content` holding an image block) so a read of a JSON file that
// merely mentions an image is still shown as text.
func toolImageResultJSON(text string) bool {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "{") || !json.Valid([]byte(trimmed)) {
		return false
	}
	var result struct {
		Content []toolContentBlock `json:"content"`
	}
	if json.Unmarshal([]byte(trimmed), &result) != nil {
		return false
	}
	for _, block := range result.Content {
		if block.Type == "image" {
			return true
		}
	}
	return false
}

// toolImageFile reports whether a path names a picture the cards can draw.
func toolImageFile(path string) bool {
	return viewerFileKindFor(path) != viewerNotImage
}

// toolImagePath turns a resource URI into a local path. Only file and plain
// paths are local; a remote link is not something to read from disk.
func toolImagePath(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return ""
	}
	lower := strings.ToLower(uri)
	if strings.HasPrefix(lower, "file://") {
		parsed, err := url.Parse(uri)
		if err != nil {
			return ""
		}
		path := parsed.Path
		if len(path) > 2 && path[0] == '/' && path[2] == ':' {
			path = path[1:] // file:///C:/...
		}
		return path
	}
	if strings.Contains(lower, "://") {
		return ""
	}
	return uri
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// toolRawOutputImages reads pictures from a tool result object: codex puts an
// image generation's picture here when it is not in content, and other agents
// follow the same `{content: [...]}` shape. The walk stops at a picture's own
// data string so base64 is never searched.
func toolRawOutputImages(raw json.RawMessage) []ToolImage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	var images []ToolImage
	collectToolImages(value, &images, 0)
	return images
}

func collectToolImages(value any, images *[]ToolImage, depth int) {
	if len(*images) >= maxToolImages || depth > 8 {
		return
	}
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			collectToolImages(child, images, depth+1)
			if len(*images) >= maxToolImages {
				return
			}
		}
	case map[string]any:
		typeName, _ := item["type"].(string)
		switch typeName {
		case "image":
			image := ToolImage{Name: stringValue(item["name"])}
			image.MimeType = stringValue(item["mimeType"])
			image.Path = toolImagePath(stringValue(item["uri"]))
			if data := stringValue(item["data"]); data != "" && toolImageDataFits(data) {
				image.Data = data
			}
			if image.Data != "" || image.Path != "" {
				*images = append(*images, image)
			}
			return
		case "resource_link", "resource":
			path := toolImagePath(firstNonEmptyString(stringValue(item["uri"]), stringValue(item["path"])))
			mime := strings.ToLower(stringValue(item["mimeType"]))
			if path != "" && (toolImageFile(path) || strings.HasPrefix(mime, "image/")) {
				*images = append(*images, ToolImage{Path: path, Name: stringValue(item["name"]), MimeType: mime})
			}
			return
		}
		for key, child := range item {
			if key == "data" || key == "blob" {
				continue
			}
			collectToolImages(child, images, depth+1)
			if len(*images) >= maxToolImages {
				return
			}
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func toolImageDataFits(data string) bool {
	return base64.StdEncoding.DecodedLen(len(data)) <= maxToolImageBytes
}

// dedupeToolImages drops pictures an agent reported twice. pi's image read
// mirrors the same image into rawOutput.structuredContent as well as
// rawOutput.content, and a result can also repeat a path that its embedded
// copy already names; scanning the whole result would draw it twice. The first
// occurrence wins, so an embedded copy (readable without the workspace) is
// kept over a path-only one.
func dedupeToolImages(images []ToolImage) []ToolImage {
	if len(images) < 2 {
		return images
	}
	seen := make(map[string]bool, len(images))
	kept := images[:0]
	for _, image := range images {
		key := toolImageIdentity(image)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, image)
	}
	return kept
}

// toolImageIdentity is what makes two reported pictures the same one: the
// embedded bytes when the agent sent them, otherwise the file it named.
func toolImageIdentity(image ToolImage) string {
	if image.Data != "" {
		return "data:\x00" + image.Data
	}
	if path := toolImagePath(image.Path); path != "" {
		return "path:\x00" + path
	}
	return ""
}
