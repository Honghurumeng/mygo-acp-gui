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
	if a.Session == nil || a.Connecting || a.SessionBusy != "" {
		return
	}
	remaining := maxAttachments - len(a.Attachments)
	if remaining <= 0 {
		a.addError(errAttachmentCount)
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
			a.Post(func() { a.addError(err) })
			return
		}
		for _, path := range paths {
			if remaining <= 0 {
				break
			}
			info, err := os.Stat(path)
			if err != nil {
				a.Post(func() { a.addError(err) })
				continue
			}
			if info.Size() > maxImageBytes {
				a.Post(func() { a.addError(errImageTooLarge(path)) })
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				a.Post(func() { a.addError(err) })
				continue
			}
			mime := imageMIME(path, data)
			attachment := Attachment{
				Name:     filepath.Base(path),
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(data),
			}
			a.Post(func() {
				bitmap, err := mui.DecodeBitmap(data)
				if err != nil {
					a.addError(err)
					return
				}
				attachment.Bitmap = bitmap
				if len(a.Attachments) >= maxAttachments {
					bitmap.ReleasePixels()
					a.addError(errAttachmentCount)
					return
				}
				a.Attachments = append(a.Attachments, attachment)
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

func (a *App) removeAttachment(index int) {
	if index < 0 || index >= len(a.Attachments) {
		return
	}
	a.Attachments = append(a.Attachments[:index], a.Attachments[index+1:]...)
}

func (a *App) attachmentPicker(c *mui.Context) {
	if len(a.Attachments) == 0 {
		return
	}
	t := c.Theme()
	mui.Row(c).Gap(8).Wrap().Children(func() {
		for i, attachment := range a.Attachments {
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
					a.removeAttachment(i)
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
