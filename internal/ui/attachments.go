package ui

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	mui "github.com/egoist/mygo/ui"
)

const (
	maxAttachments = 4
	maxImageBytes  = 15 << 20
)

func (a *App) chooseImages() {
	chat := a.activeChat()
	if chat == nil || chat.Session == nil || chat.Connecting || chat.Busy != "" {
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
			mime := imageMIME(path, data)
			attachment := Attachment{
				Name:     filepath.Base(path),
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(data),
			}
			a.Post(func() {
				if chat.Closed || a.chatIndex(chat) < 0 {
					return
				}
				bitmap, err := mui.DecodeBitmap(data)
				if err != nil {
					a.addError(chat, err)
					return
				}
				attachment.Bitmap = bitmap
				if len(chat.Attachments) >= maxAttachments {
					bitmap.ReleasePixels()
					a.addError(chat, errAttachmentCount)
					return
				}
				chat.Attachments = append(chat.Attachments, attachment)
			})
			remaining--
		}
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

func (a *App) attachmentPicker(c *mui.Context, chat *Chat) {
	if len(chat.Attachments) == 0 {
		return
	}
	t := c.Theme()
	mui.Row(c).Gap(8).Wrap().Children(func() {
		for i, attachment := range chat.Attachments {
			i := i
			attachment := attachment
			mui.Row(c).Gap(6).AlignItems(mui.Center).Padding(4, 6).Radius(8).Background(t.Surface).Children(func() {
				if attachment.Bitmap != nil {
					mui.Image(c, attachment.Bitmap).Size(48, 48).Fit(mui.Cover).Radius(5)
				}
				mui.Column(c).Gap(2).Children(func() {
					mui.Text(c, truncate(attachment.Name, 28)).FontSize(11)
					mui.Text(c, attachment.MimeType).FontSize(9).TextColor(t.TextMuted)
				})
				if mui.Button(c, "×").Label("Remove " + attachment.Name).Clicked() {
					a.removeAttachment(chat, i)
				}
			})
		}
	})
}

func (a *App) messageAttachments(c *mui.Context, attachments []Attachment) {
	if len(attachments) == 0 {
		return
	}
	t := c.Theme()
	mui.Row(c).Gap(6).Wrap().Children(func() {
		for _, attachment := range attachments {
			mui.Column(c).Gap(2).Children(func() {
				if attachment.Bitmap != nil {
					mui.Image(c, attachment.Bitmap).Size(160, 120).Fit(mui.Contain).Radius(6)
				}
				mui.Text(c, truncate(attachment.Name, 32)).FontSize(9).TextColor(t.TextMuted)
			})
		}
	})
}

type attachmentError string

func (e attachmentError) Error() string { return string(e) }

const (
	errAttachmentCount attachmentError = "At most 4 images can be attached."
)

func errImageTooLarge(path string) error {
	return attachmentError("Image is larger than 15 MB: " + filepath.Base(path))
}
