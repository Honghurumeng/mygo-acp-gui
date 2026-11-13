package ui

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mui "github.com/egoist/mygo/ui"
)

func TestMentionContext(t *testing.T) {
	tests := []struct {
		text  string
		caret int
		start int
		query string
		ok    bool
	}{
		{"hello @mai", 10, 6, "mai", true},
		{"@", 1, 0, "", true},
		{"@main.go", 8, 0, "main.go", true},
		{"a@b.com", 7, 0, "", false},
		{"@file and @other", 16, 10, "other", true},
		{"hello", 5, 0, "", false},
	}
	for _, tt := range tests {
		start, query, ok := mentionContext(tt.text, tt.caret)
		if start != tt.start || query != tt.query || ok != tt.ok {
			t.Errorf("mentionContext(%q, %d) = (%d, %q, %v), want (%d, %q, %v)",
				tt.text, tt.caret, start, query, ok, tt.start, tt.query, tt.ok)
		}
	}
}

func TestMatchMentionFilesRanksBaseNames(t *testing.T) {
	files := []string{
		"cmd/other.go",
		"internal/ui/composer.go",
		"internal/ui/composer_test.go",
		"composer.go",
	}
	got := matchMentionFiles(files, "composer.go")
	if len(got) == 0 || got[0] != "composer.go" {
		t.Fatalf("exact base name did not rank first: %v", got)
	}
	got = matchMentionFiles(files, "compose")
	if len(got) != 3 {
		t.Fatalf("base-name prefix matches = %v", got)
	}
	if got = matchMentionFiles(files, "cmt"); len(got) == 0 || !strings.Contains(got[0], "composer_test.go") {
		t.Fatalf("subsequence match = %v", got)
	}
}

func TestMentionPathParts(t *testing.T) {
	tests := []struct {
		relative string
		name     string
		dir      string
	}{
		{"main.go", "main.go", ""},
		{"internal/ui/composer.go", "composer.go", "internal/ui"},
		{"docs/design/git.md", "git.md", "docs/design"},
	}
	for _, tt := range tests {
		name, dir := mentionPathParts(tt.relative)
		if name != tt.name || dir != tt.dir {
			t.Errorf("mentionPathParts(%q) = (%q, %q), want (%q, %q)",
				tt.relative, name, dir, tt.name, tt.dir)
		}
	}
}

func TestFileRefsInText(t *testing.T) {
	refs := []FileRef{{Name: "main.go"}, {Name: "internal/ui/composer.go"}}
	kept := fileRefsInText(refs, "please check @internal/ui/composer.go and @gone.go")
	if len(kept) != 1 || kept[0].Name != "internal/ui/composer.go" {
		t.Fatalf("kept refs = %+v", kept)
	}
	if got := fileRefsInText(refs, "mail@main.go"); len(got) != 0 {
		t.Errorf("an address half matched a mention: %+v", got)
	}
	if got := fileRefsInText(refs, "@main.go"); len(got) != 1 {
		t.Errorf("token at the end of the text was dropped: %+v", got)
	}
}

func TestScanWorkspaceFilesSkipsDependencies(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{
		"main.go",
		"internal/ui/composer.go",
		"node_modules/pkg/index.js",
		"build/out.bin",
		"with space.txt",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	files, err := scanWorkspaceFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"main.go": true, "internal/ui/composer.go": true}
	for _, file := range files {
		if !want[file] {
			t.Errorf("unexpected indexed file %q", file)
		}
		delete(want, file)
	}
	if len(want) != 0 {
		t.Errorf("missing indexed files: %v (got %v)", want, files)
	}
}

func TestPasteImageFromClipboard(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	app.readClipboardImage = func() []byte { return png }

	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboard("")
	tt.Key(mui.Cmd, mui.KeyV)
	waitFor(t, app, tt, func() bool { return len(current(app).Attachments) == 1 })
	chat := current(app)
	if chat.Input != "" {
		t.Errorf("image paste inserted text: %q", chat.Input)
	}
	if chat.Attachments[0].MimeType != "image/png" {
		t.Errorf("mime = %q", chat.Attachments[0].MimeType)
	}

	chat.Input = "look"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "images=1")
	})
}

func TestMentionFilePickerAndPrompt(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt := newFakeAppIn(t, workspace)
	app.fileIndexer = func(string) ([]string, error) {
		return []string{"main.go", "internal/ui/composer.go"}, nil
	}
	connect(t, app, tt)

	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@mai")
	waitFor(t, app, tt, func() bool {
		return current(app).MentionOpen && len(current(app).mentionMatches()) == 1
	})
	tt.Frame()
	if !tt.HasText("main.go") {
		t.Fatalf("mention list missing file; texts: %q", tt.Texts())
	}
	if err := tt.Click("Mention main.go"); err != nil {
		t.Fatal(err)
	}
	chat := current(app)
	if chat.Input != "@main.go " {
		t.Fatalf("input = %q", chat.Input)
	}
	if len(chat.FileRefs) != 1 || chat.FileRefs[0].Name != "main.go" {
		t.Fatalf("file refs = %+v", chat.FileRefs)
	}
	if chat.MentionOpen {
		t.Error("mention list stayed open after accepting a file")
	}
	if tt.HasText("Remove main.go") {
		t.Error("an @ file was shown as a composer attachment chip")
	}
	tt.Frame()
	tt.Type("x")
	if chat = current(app); chat.Input != "@main.go x" {
		t.Fatalf("typing after picking a file = %q; the caret should follow the path", chat.Input)
	}
	chat.Input = "@main.go explain"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "files=0 embedded=0")
	})
	if len(current(app).FileRefs) != 0 {
		t.Error("file refs were not cleared after sending")
	}
}

func TestMentionPaletteShowsNameBeforeDirectory(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "internal", "ui", "composer.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package ui\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt := newFakeAppIn(t, workspace)
	app.fileIndexer = func(string) ([]string, error) { return []string{"internal/ui/composer.go"}, nil }
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@composer")
	waitFor(t, app, tt, func() bool {
		return current(app).MentionOpen && len(current(app).mentionMatches()) == 1
	})
	tt.Frame()
	texts := tt.Texts()
	nameAt, dirAt := -1, -1
	for i, text := range texts {
		switch text {
		case "composer.go":
			nameAt = i
		case "internal/ui":
			dirAt = i
		}
	}
	if nameAt < 0 || dirAt < 0 {
		t.Fatalf("mention row lacks name or directory; texts: %q", texts)
	}
	if nameAt > dirAt {
		t.Errorf("directory is listed before the file name; texts: %q", texts)
	}
}

func TestMentionTabAcceptsFirstFile(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "README.md"), []byte("# demo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt := newFakeAppIn(t, workspace)
	app.fileIndexer = func(string) ([]string, error) { return []string{"README.md"}, nil }
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@RE")
	waitFor(t, app, tt, func() bool {
		return current(app).MentionOpen && len(current(app).mentionMatches()) == 1
	})
	tt.Key(0, mui.KeyTab)
	chat := current(app)
	if chat.Input != "@README.md " || len(chat.FileRefs) != 1 {
		t.Fatalf("input = %q, refs = %+v", chat.Input, chat.FileRefs)
	}
	tt.Frame()
	tt.Type("x")
	if chat = current(app); chat.Input != "@README.md x" {
		t.Fatalf("typing after Tab completion = %q; the caret should follow the path", chat.Input)
	}
}

// A file picked before the first connect must survive the draft's
// session/new reset and be sent as part of the queued first prompt.
func TestDraftMentionConnectsAndSends(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, tt := newFakeAppIn(t, workspace)
	app.fileIndexer = func(string) ([]string, error) { return []string{"main.go"}, nil }
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("@mai")
	waitFor(t, app, tt, func() bool {
		return current(app).MentionOpen && len(current(app).mentionMatches()) == 1
	})
	tt.Key(0, mui.KeyTab)
	chat := current(app)
	chat.Input = "@main.go explain"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Connected && !current(app).Streaming && lastAssistantHas(app, "files=0 embedded=0")
	})
}
