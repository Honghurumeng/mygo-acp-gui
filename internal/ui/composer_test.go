package ui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

func TestModelPanelRefreshesProviderList(t *testing.T) {
	stale := []acp.ConfigOption{{
		ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "stale/old",
		Options: []acp.ConfigOptionValue{{Value: "stale/old", Name: "stale/old"}},
	}}
	fresh := []acp.ConfigOption{{
		ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "demo/small",
		Options: []acp.ConfigOptionValue{
			{Value: "demo/small", Name: "demo/small"},
			{Value: "other/only", Name: "other/only"},
		},
	}}
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
		ProbeConfig: func(context.Context, agent.LaunchSpec) (session.Info, error) {
			return session.Info{SessionID: "probe-session", ConfigOptions: fresh}, nil
		},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.AgentConfigs["fake"] = stale
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()
	if !tt.HasText("stale/old") {
		t.Fatalf("cached provider missing from draft chip; texts: %q", tt.Texts())
	}

	// Opening the panel re-reads the agent's model list; the stale provider
	// must disappear and the freshly advertised one must replace it.
	if err := tt.Click("stale/old"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		options := app.AgentConfigs["fake"]
		return len(options) == 1 && len(options[0].Options) == 2
	})
	tt.Frame()
	if tt.HasText("stale") {
		t.Errorf("stale provider is still shown; texts: %q", tt.Texts())
	}
	if !tt.HasText("other") {
		t.Errorf("refreshed provider missing; texts: %q", tt.Texts())
	}
}

func TestDraftPicksModelAndThinkingBeforeConnect(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	// Discovery caches each agent's config options; injected agents skip it.
	app.AgentConfigs["fake"] = []acp.ConfigOption{
		{
			ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "demo/small",
			Options: []acp.ConfigOptionValue{
				{Value: "demo/small", Name: "demo/small"},
				{Value: "demo/large", Name: "demo/large"},
			},
		},
		{
			ID: "thought_level", Name: "Thinking", Category: "thought_level", Type: "select", CurrentValue: "medium",
			Options: []acp.ConfigOptionValue{
				{Value: "medium", Name: "medium"},
				{Value: "high", Name: "high"},
			},
		},
	}
	tt.Frame()
	if chat.Session != nil {
		t.Fatal("draft started connected")
	}
	if !tt.HasText("demo/small") {
		t.Fatalf("chip misses the probed model; texts: %q", tt.Texts())
	}

	if err := tt.Click("demo/small"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("demo/large"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["model"]; got != "demo/large" {
		t.Fatalf("pending model = %q, want demo/large", got)
	}
	if err := tt.Click("high"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["thought_level"]; got != "high" {
		t.Fatalf("pending thinking = %q, want high", got)
	}

	// Connecting applies the draft picks to the new session.
	app.Connect()
	waitFor(t, app, tt, func() bool {
		ch := current(app)
		if !ch.Connected || ch.PendingConfig != nil {
			return false
		}
		model := ch.Info.ConfigOption("model")
		thinking := ch.Info.ConfigOption("thought_level")
		return model != nil && model.CurrentValue == "demo/large" &&
			thinking != nil && thinking.CurrentValue == "high"
	})
}

func TestFirstMessageConnectsDraft(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	if chat.Session != nil {
		t.Fatal("draft started connected")
	}
	chat.Input = "auto connect"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return current(app).Connected && !current(app).Streaming &&
			lastAssistantHas(app, "收到：auto connect")
	})
	if current(app).Input != "" {
		t.Errorf("input was not cleared after the queued prompt: %q", current(app).Input)
	}
}

func TestWorkspaceConversationPreviewAndCollapse(t *testing.T) {
	app, tt := newFakeApp(t)
	base := current(app).Workspace
	current(app).Title = "First conversation"
	for i := 2; i <= 6; i++ {
		chat := app.newChatTabIn(base)
		chat.Title = fmt.Sprintf("Conversation %d", i)
	}
	app.selectChat(app.Chats[0])
	tt.Frame()

	for i := 2; i <= 5; i++ {
		if !tt.HasText(fmt.Sprintf("Conversation %d", i)) {
			t.Errorf("preview is missing conversation %d; texts: %q", i, tt.Texts())
		}
	}
	if tt.HasText("Conversation 6") {
		t.Fatalf("preview showed more than five conversations; texts: %q", tt.Texts())
	}
	if tt.HasText("Fake history") {
		t.Fatalf("preview showed saved history beyond the limit; texts: %q", tt.Texts())
	}
	label := filepath.Base(base)
	if !tt.HasText("Show 2 more") {
		t.Fatalf("show-more affordance missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Show more in " + label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Conversation 6") || !tt.HasText("Fake history") || !tt.HasText("Show less") {
		t.Fatalf("expanded list missing; texts: %q", tt.Texts())
	}

	// A click on the workspace name folds its conversations.
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Conversation 2") {
		t.Fatalf("workspace did not collapse; texts: %q", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Conversation 2") {
		t.Fatalf("workspace did not expand; texts: %q", tt.Texts())
	}
}

func TestRenameConversation(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	name := chat.name()
	tt.Frame()
	if err := tt.Click("Conversation actions for " + name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Rename"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Rename conversation") {
		t.Fatalf("rename dialog missing; texts: %q", tt.Texts())
	}
	app.RenameText = "Renamed conversation"
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if chat.Title != "Renamed conversation" {
		t.Errorf("title = %q", chat.Title)
	}
	if app.RenameOpen {
		t.Error("rename dialog stayed open")
	}
}

func TestGitBranchFromHead(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/feature/ui\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "internal", "ui")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	chat := &Chat{Workspace: nested}
	if got := chat.gitBranch(); got != "feature/ui" {
		t.Errorf("branch = %q", got)
	}
}

func TestGitBranchFromWorktreeFile(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, "worktrees", "agent")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(dir, "checkout")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".git"), []byte("gitdir: ../worktrees/agent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readGitBranch(workspace); got != "work" {
		t.Errorf("branch = %q", got)
	}
	if got := readGitBranch(t.TempDir()); got != "" {
		t.Errorf("non-repo branch = %q", got)
	}
}

func TestComposerHeightClamp(t *testing.T) {
	if got := clampComposer(20); got != minComposerHeight {
		t.Errorf("low = %v", got)
	}
	if got := clampComposer(1000); got != maxComposerHeight {
		t.Errorf("high = %v", got)
	}
	if got := clampComposer(200); got != 200 {
		t.Errorf("middle = %v", got)
	}
}

// Opening the slash-command list above the composer must not rebuild the
// textarea under a new element id: the composer is keyed so focus and the
// next typed rune stay with it.
func TestCommandPaletteKeepsComposerFocus(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("/")
	tt.Frame()
	if !tt.Focused("Message") {
		t.Fatalf("command palette took focus; texts: %q", tt.Texts())
	}
	tt.Type("p")
	if got := current(app).Input; got != "/p" {
		t.Fatalf("input after palette opened = %q", got)
	}
}

// A text paste must still insert text when the clipboard also offers an
// image representation; only image-only pasteboard content becomes an
// attachment.
func TestPasteTextIsNotAnImage(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	app.readClipboardImage = func() []byte {
		t.Error("clipboard image was read for a text paste")
		return nil
	}
	tt.Frame()
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.SetClipboard("hello from clipboard")
	tt.Key(mui.Cmd, mui.KeyV)
	tt.Frame()
	if got := current(app).Input; got != "hello from clipboard" {
		t.Fatalf("input = %q", got)
	}
	if len(current(app).Attachments) != 0 {
		t.Fatal("text paste became an image attachment")
	}
}

// Images show as pictures, not filenames or MIME labels, and clicking one
// opens the large preview from both the composer and the transcript.
func TestImagePreviewFromComposerAndMessage(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	png, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := mui.DecodeBitmap(png)
	if err != nil {
		t.Fatal(err)
	}
	chat := current(app)
	chat.Attachments = []Attachment{{
		Name:     "pasted-image.png",
		MimeType: "image/png",
		Data:     pngBase64,
		Bitmap:   bitmap,
	}}
	tt.Frame()
	if tt.HasText("pasted-image.png") || tt.HasText("image/png") {
		t.Fatalf("composer showed image metadata; texts: %q", tt.Texts())
	}
	if err := tt.Click("View attached image"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || app.ImagePreview == nil {
		t.Fatal("composer image did not open the preview")
	}
	if !tt.HasText("Close") {
		t.Fatalf("preview dialog missing; texts: %q", tt.Texts())
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if app.ImageOpen || app.ImagePreview != nil {
		t.Fatal("preview did not close")
	}

	chat.Input = "look"
	tt.Frame()
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		return !current(app).Streaming && lastAssistantHas(app, "images=1")
	})
	if tt.HasText("pasted-image.png") || tt.HasText("image/png") {
		t.Fatalf("transcript showed image metadata; texts: %q", tt.Texts())
	}
	if err := tt.Click("View message image"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !app.ImageOpen || app.ImagePreview == nil || app.ImagePreview.Name != "pasted-image.png" {
		t.Fatal("message image did not open the preview")
	}
}
