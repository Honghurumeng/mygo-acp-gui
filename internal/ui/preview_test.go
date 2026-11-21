package ui

import (
	"bytes"
	"encoding/base64"
	"testing"

	mui "github.com/egoist/mygo/ui"
)

// An attachment's preview downloads the bytes the user attached, under the
// name it came with.
func TestPreviewSourceDownloadsAnAttachment(t *testing.T) {
	app, _ := newFakeApp(t)
	png, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := mui.DecodeBitmap(png)
	if err != nil {
		t.Fatal(err)
	}
	app.ImagePreview = &Attachment{
		Name:     "shot.png",
		MimeType: "image/png",
		Data:     base64.StdEncoding.EncodeToString(png),
		Bitmap:   bitmap,
	}
	name, data, ok := app.previewFile()
	if !ok {
		t.Fatal("the attachment preview has nothing to download")
	}
	if name != "shot.png" {
		t.Errorf("download name = %q", name)
	}
	if !bytes.Equal(data, png) {
		t.Errorf("download data differs from the attachment: %d bytes", len(data))
	}
	src, ok := app.previewSource()
	if !ok || src.Width != 1 || src.Height != 1 {
		t.Errorf("preview source = %+v", src)
	}
}
