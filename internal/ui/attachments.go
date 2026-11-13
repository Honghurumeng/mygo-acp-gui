package ui

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
)

const (
	maxAttachments = 4
	maxImageBytes  = 15 << 20
)

func (a *App) chooseImages() {
	chat := a.activeChat()
	if chat == nil || chat.Closed || chat.Connecting || chat.Busy != "" {
		return
	}
	remaining := maxAttachments - len(chat.Attachments)
	if remaining <= 0 {
		a.addError(chat, errAttachmentCount)
		return
	}
	win := a.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:   win,
			Title:    "Attach Images",
			Multiple: true,
			Filters: []mygo.FileFilter{{
				Name:       "Images",
				Extensions: []string{"png", "jpg", "jpeg", "gif", "webp", "bmp"},
			}},
		})
		if err != nil {
			a.Post(func() { a.addError(chat, err) })
			return
		}
		for _, path := range paths {
			if remaining <= 0 {
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				a.Post(func() { a.addError(chat, err) })
				continue
			}
			if info.Size() > maxImageBytes {
				a.Post(func() { a.addError(chat, errImageTooLarge(path)) })
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				a.Post(func() { a.addError(chat, err) })
				continue
			}
			a.loadImageAttachment(chat, filepath.Base(path), data)
			remaining--
		}
	}()
}

// pasteClipboardImage attaches the clipboard's image, if it has one. It
// returns whether the paste was taken: an image paste must not insert the
// empty clipboard text into the composer. Only bytes enter ACP; the app
// never creates a temporary image path or sends a local path as text.
func (a *App) pasteClipboardImage(chat *Chat) bool {
	if chat == nil || a.readClipboardImage == nil {
		return false
	}
	data := a.readClipboardImage()
	if len(data) == 0 {
		return false
	}
	if len(data) > maxImageBytes {
		a.addError(chat, errImageTooLarge("clipboard image"))
		return true
	}
	if !chat.canAttach() {
		if chat.Session != nil && !chat.Info.AgentCapabilities.PromptCapabilities.Image {
			a.addError(chat, attachmentError("This agent does not support image attachments."))
		}
		return true
	}
	if len(chat.Attachments) >= maxAttachments {
		a.addError(chat, errAttachmentCount)
		return true
	}
	a.loadImageAttachment(chat, "pasted-image.png", data)
	return true
}

// loadImageAttachment decodes off the UI thread and appends on it, with the
// attachment limit checked again because decoding may outlive the draft the
// image was pasted into.
func (a *App) loadImageAttachment(chat *Chat, name string, data []byte) {
	if chat == nil || len(data) == 0 {
		return
	}
	if len(data) > maxImageBytes {
		a.addError(chat, errImageTooLarge(name))
		return
	}
	data = append([]byte(nil), data...)
	attachment := Attachment{
		Name:     name,
		MimeType: imageMIME(name, data),
		Data:     base64.StdEncoding.EncodeToString(data),
	}
	go func() {
		bitmap, err := mui.DecodeBitmap(data)
		a.Post(func() {
			if err != nil {
				a.addError(chat, err)
				return
			}
			if chat.Closed || a.chatIndex(chat) < 0 {
				bitmap.ReleasePixels()
				return
			}
			if len(chat.Attachments) >= maxAttachments {
				bitmap.ReleasePixels()
				a.addError(chat, errAttachmentCount)
				return
			}
			attachment.Bitmap = bitmap
			chat.Attachments = append(chat.Attachments, attachment)
		})
	}()
}

func imageMIME(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	if detected := http.DetectContentType(data); strings.HasPrefix(detected, "image/") {
		return detected
	}
	return "application/octet-stream"
}

func (a *App) removeAttachment(chat *Chat, index int) {
	if chat == nil || index < 0 || index >= len(chat.Attachments) {
		return
	}
	chat.Attachments = append(chat.Attachments[:index], chat.Attachments[index+1:]...)
}

// attachmentPicker shows only images. An @ file mention stays plain text in
// the composer and never becomes a chip here, as it is not attached content.
func (a *App) attachmentPicker(c *mui.Context, chat *Chat) {
	if len(chat.Attachments) == 0 {
		return
	}
	mui.Row(c).Gap(8).Wrap().Children(func() {
		for i, attachment := range chat.Attachments {
			i := i
			attachment := attachment
			surfaceCard(c, mui.Row(c).Gap(4).AlignItems(mui.Center).Padding(4, 6)).Children(func() {
				if attachment.Bitmap != nil {
					a.clickableImage(c, attachment, 48, 48, mui.Cover, 5, "View attached image")
				}
				if quietButton(c, "×").Label("Remove image").Clicked() {
					a.removeAttachment(chat, i)
				}
			})
		}
	})
}

// hasImageContent reports whether a message chunk carries an image block.
// Replayed ACP histories use these blocks for images the user sent; text
// chunks alone would make an image-only prompt disappear.
func hasImageContent(blocks []acp.ContentBlock) bool {
	for _, block := range blocks {
		if block.Type == "image" && block.Data != "" {
			return true
		}
	}
	return false
}

// addMessageImages decodes replayed image blocks off the UI thread. A
// placeholder keeps the images in block order while each bitmap decodes, so
// a slow large image does not reorder the thumbnails. The composer's 4-image
// limit applies per message, not to the whole transcript.
func (a *App) addMessageImages(chat *Chat, msg *Message, blocks []acp.ContentBlock) {
	if chat == nil || msg == nil {
		return
	}
	for _, block := range blocks {
		if block.Type != "image" || block.Data == "" {
			continue
		}
		if len(msg.Attachments) >= maxAttachments {
			break
		}
		data, err := base64.StdEncoding.DecodeString(block.Data)
		if err != nil || len(data) == 0 || len(data) > maxImageBytes {
			continue
		}
		mimeType := block.MimeType
		if mimeType == "" {
			mimeType = imageMIME("image", data)
		}
		index := len(msg.Attachments)
		msg.Attachments = append(msg.Attachments, Attachment{
			Name:     "image",
			MimeType: mimeType,
			Data:     block.Data,
		})
		go func(index int, data []byte) {
			bitmap, err := mui.DecodeBitmap(data)
			if err != nil {
				return
			}
			a.Post(func() {
				if chat.Closed || a.chatIndex(chat) < 0 || !messageInChat(chat, msg) || index >= len(msg.Attachments) {
					bitmap.ReleasePixels()
					return
				}
				msg.Attachments[index].Bitmap = bitmap
			})
		}(index, data)
	}
}

func messageInChat(chat *Chat, msg *Message) bool {
	if chat == nil || msg == nil {
		return false
	}
	for _, candidate := range chat.Messages {
		if candidate == msg {
			return true
		}
	}
	return false
}

func (a *App) messageFiles(c *mui.Context, files []FileRef) {
	if len(files) == 0 {
		return
	}
	mui.Row(c).Gap(6).Wrap().Children(func() {
		for _, ref := range files {
			surfaceCard(c, mui.Row(c).Gap(4).AlignItems(mui.Center).Padding(3, 8)).Children(func() {
				mutedText(c, "@", 10)
				mui.Text(c, truncate(ref.Name, 48)).FontSize(10).SingleLine()
			})
		}
	})
}

func (a *App) messageAttachments(c *mui.Context, attachments []Attachment) {
	if len(attachments) == 0 {
		return
	}
	mui.Row(c).Gap(6).Wrap().Children(func() {
		for _, attachment := range attachments {
			if attachment.Bitmap != nil {
				a.clickableImage(c, attachment, 160, 120, mui.Contain, 6, "View message image")
			}
		}
	})
}

// clickableImage draws a thumbnail and opens the full-size preview when it is
// clicked. Names and MIME types stay out of both places: images are shown as
// pictures, and the preview is the place to inspect them.
func (a *App) clickableImage(c *mui.Context, attachment Attachment, w, h float32, fit mui.Fit, radius float32, label string) mui.Element {
	image := mui.Image(c, attachment.Bitmap).
		Size(w, h).
		Fit(fit).
		Radius(radius).
		Cursor(mui.CursorPointer).
		Label(label).
		Tooltip("Click to view larger")
	if image.Clicked() {
		a.ImagePreview = &attachment
		a.ImageOpen = true
	}
	return image
}

// imagePreviewDialog shows the selected attachment enlarged in a modal. The
// picture is capped at its own size so a small image is not turned into a
// blurry wall, and at the window's room so it always fits.
func (a *App) imagePreviewDialog(c *mui.Context) {
	if !a.ImageOpen || a.ImagePreview == nil || a.ImagePreview.Bitmap == nil {
		return
	}
	bmp := a.ImagePreview.Bitmap
	windowW, windowH := c.Size()
	imageW, imageH := mui.ImageSize(bmp)
	if imageW <= 0 || imageH <= 0 {
		return
	}
	maxW := max(float32(120), windowW-120)
	maxH := max(float32(120), windowH-220)
	scale := min(maxW/imageW, maxH/imageH)
	if scale > 1 {
		scale = 1
	}
	w, h := imageW*scale, imageH*scale
	mui.Modal(c, &a.ImageOpen, func() {
		mui.Column(c).Gap(8).Children(func() {
			mui.Image(c, bmp).Size(w, h).Fit(mui.Contain).Radius(6)
			mui.Row(c).Gap(8).Justify(mui.End).Children(func() {
				if quietButton(c, "Close").Clicked() {
					a.ImageOpen = false
				}
			})
		})
	})
	if !a.ImageOpen {
		a.ImagePreview = nil
	}
}

type attachmentError string

func (e attachmentError) Error() string { return string(e) }

const (
	errAttachmentCount attachmentError = "At most 4 images can be attached."
)

func errImageTooLarge(path string) error {
	return attachmentError("Image is larger than 15 MB: " + filepath.Base(path))
}
