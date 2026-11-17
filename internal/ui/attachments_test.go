package ui

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

const testPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func testPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(testPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestImageMIME(t *testing.T) {
	png := testPNG(t)
	cases := []struct {
		name string
		data []byte
		want string
	}{
		{"photo.PNG", nil, "image/png"},
		{"photo.jpg", nil, "image/jpeg"},
		{"photo.JPEG", nil, "image/jpeg"},
		{"animation.gif", nil, "image/gif"},
		{"photo.webp", nil, "image/webp"},
		{"photo.bmp", nil, "image/bmp"},
		{"pasted-image.png", png, "image/png"},
		{"no-extension", png, "image/png"},
		{"notes.txt", []byte("plain text"), "application/octet-stream"},
	}
	for _, tc := range cases {
		if got := imageMIME(tc.name, tc.data); got != tc.want {
			t.Errorf("imageMIME(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAttachmentLimitRemovalAndCapabilities(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir(), Agents: []agent.LaunchSpec{}})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := app.activeChat()
	if chat == nil {
		t.Fatal("no draft chat")
	}
	png := testPNG(t)

	// Decoding appends the attachment off the UI thread and keeps the bytes.
	app.loadImageAttachment(chat, "pixel.png", png)
	waitFor(t, app, tt, func() bool { return len(chat.Attachments) == 1 })
	if chat.Attachments[0].MimeType != "image/png" || chat.Attachments[0].Bitmap == nil {
		t.Fatalf("attachment = %+v", chat.Attachments[0])
	}
	app.removeAttachment(chat, 0)
	if len(chat.Attachments) != 0 {
		t.Fatalf("removeAttachment left %d attachments", len(chat.Attachments))
	}

	// A count limit rejects the fifth image without decoding it.
	chat.Attachments = []Attachment{
		{Name: "1.png", MimeType: "image/png"},
		{Name: "2.png", MimeType: "image/png"},
		{Name: "3.png", MimeType: "image/png"},
		{Name: "4.png", MimeType: "image/png"},
	}
	app.readClipboardImage = func() []byte { return png }
	if !app.pasteClipboardImage(chat) {
		t.Fatal("full clipboard paste was not consumed")
	}
	if !chatHasError(chat, "At most 4 images") {
		t.Errorf("count limit error missing: %+v", chat.Messages)
	}
	chat.Attachments = nil

	// Oversized bytes are rejected before a bitmap is allocated.
	app.readClipboardImage = func() []byte { return make([]byte, maxImageBytes+1) }
	if !app.pasteClipboardImage(chat) {
		t.Fatal("oversized clipboard paste was not consumed")
	}
	if !chatHasError(chat, "larger than 15 MB") {
		t.Errorf("size limit error missing: %+v", chat.Messages)
	}

	// A connected agent without image prompt support refuses the paste.
	chat.Session = &session.Session{}
	chat.Info.SessionID = "session"
	chat.Info.AgentCapabilities.PromptCapabilities.Image = false
	app.readClipboardImage = func() []byte { return png }
	if !app.pasteClipboardImage(chat) {
		t.Fatal("unsupported clipboard paste was not consumed")
	}
	if !chatHasError(chat, "does not support image") {
		t.Errorf("capability error missing: %+v", chat.Messages)
	}
	chat.Session = nil

	// loadImageAttachment also enforces the byte limit for file-picker paths.
	app.loadImageAttachment(chat, "huge.png", make([]byte, maxImageBytes+1))
	if !chatHasError(chat, "Image is larger than 15 MB: huge.png") {
		t.Errorf("file size limit error missing: %+v", chat.Messages)
	}
}

func chatHasError(chat *Chat, want string) bool {
	for _, message := range chat.Messages {
		if message.Role == RoleError && strings.Contains(message.Text, want) {
			return true
		}
	}
	return false
}

func TestChooseImagesAttachesAndReportsOversize(t *testing.T) {
	workspace := t.TempDir()
	pngPath := filepath.Join(workspace, "pixel.png")
	if err := os.WriteFile(pngPath, testPNG(t), 0o600); err != nil {
		t.Fatal(err)
	}
	bigPath := filepath.Join(workspace, "huge.png")
	if err := os.WriteFile(bigPath, make([]byte, maxImageBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		Agents:    []agent.LaunchSpec{},
		OpenImages: func() ([]string, error) {
			return []string{pngPath, bigPath}, nil
		},
	})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := app.activeChat()
	app.chooseImages()
	waitFor(t, app, tt, func() bool {
		return len(chat.Attachments) == 1 && chatHasError(chat, "Image is larger than 15 MB: huge.png")
	})
	if chat.Attachments[0].MimeType != "image/png" {
		t.Errorf("attachment = %+v", chat.Attachments[0])
	}
}

func TestChooseImagesReportsPickerError(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{},
		OpenImages: func() ([]string, error) {
			return nil, errors.New("dialog unavailable")
		},
	})
	tt := mui.NewTester(app.View, 1200, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	chat := app.activeChat()
	app.chooseImages()
	waitFor(t, app, tt, func() bool { return chatHasError(chat, "dialog unavailable") })
}
