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
	"github.com/reed/acp-gui/internal/modelconfig"
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
			return session.Info{
				SessionID:     "probe-session",
				ConfigOptions: fresh,
				Modes: []acp.Mode{
					{ID: "default", Name: "Standard"},
					{ID: "plan", Name: "Plan"},
				},
				CurrentModeID: "default",
			}, nil
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
		modes := app.AgentModes["fake"]
		return len(options) == 1 && len(options[0].Options) == 2 &&
			len(modes.Modes) == 2 && modes.CurrentModeID == "default"
	})
	tt.Frame()
	if tt.HasText("stale") {
		t.Errorf("stale provider is still shown; texts: %q", tt.Texts())
	}
	if !tt.HasText("other") {
		t.Errorf("refreshed provider missing; texts: %q", tt.Texts())
	}
	if !tt.HasText("Standard") {
		t.Errorf("probed mode missing from draft chip; texts: %q", tt.Texts())
	}
}

func TestDraftModeChipUsesProbedModes(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	// Discovery caches each agent's modes; injected agents skip it.
	app.AgentModes["fake"] = AgentModeState{
		Modes: []acp.Mode{
			{ID: "default", Name: "Standard"},
			{ID: "plan", Name: "Plan"},
		},
		CurrentModeID: "default",
	}
	tt.Frame()
	if chat.Session != nil {
		t.Fatal("draft started connected")
	}
	if !tt.HasText("Session mode: Standard") {
		t.Fatalf("chip misses the probed mode; texts: %q", tt.Texts())
	}

	if err := tt.Click("Session mode: Standard"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Plan"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingMode; got != "plan" {
		t.Fatalf("pending mode = %q, want plan", got)
	}
	if !tt.HasText("Session mode: Plan") {
		t.Fatalf("chip misses the draft mode pick; texts: %q", tt.Texts())
	}

	// Connecting applies the draft mode to the new session.
	app.Connect()
	waitFor(t, app, tt, func() bool {
		ch := current(app)
		return ch.Connected && ch.PendingMode == "" && ch.Info.CurrentModeID == "plan"
	})
}

func TestDraftPicksThinkingBeforeConnect(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	// Discovery caches each agent's config options and modes; injected agents
	// skip it.
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
	if err := tt.Click("high"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["thought_level"]; got != "high" {
		t.Fatalf("pending thinking = %q, want high", got)
	}

	// Connecting applies the draft pick to the new session.
	app.Connect()
	waitFor(t, app, tt, func() bool {
		ch := current(app)
		if !ch.Connected || ch.PendingConfig != nil {
			return false
		}
		thinking := ch.Info.ConfigOption("thought_level")
		return thinking != nil && thinking.CurrentValue == "high"
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

// The action row mixes an icon button, chips and the send/stop button, all
// drawn at different font sizes. They must still share one height, or the
// row looks ragged.
func TestComposerControlsShareHeight(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	chat.Info.Modes = []acp.Mode{{ID: "ask", Name: "Ask"}}
	chat.Info.CurrentModeID = "ask"
	chat.Usage = &acp.Usage{Used: 1, Size: 100}
	app.AgentConfigs["fake"] = []acp.ConfigOption{{
		ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "demo/small",
		Options: []acp.ConfigOptionValue{{Value: "demo/small", Name: "demo/small"}},
	}}
	tt.Frame()

	labels := []string{
		"Attach images",
		"Jump to your message",
		"Session mode: Ask",
		"Agent and model: demo/small",
		"Context usage: 1%",
		"Send",
	}
	want := float32(composerControlHeight)
	for _, label := range labels {
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("%q missing; texts: %q", label, tt.Texts())
		}
		if r.H != want {
			t.Errorf("%q height = %v, want %v", label, r.H, want)
		}
	}

	// While a turn streams, Send becomes Stop in the same slot.
	chat.Streaming = true
	tt.Frame()
	stop, ok := tt.Find("Stop")
	if !ok {
		t.Fatalf("Stop missing; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("Send"); ok {
		t.Fatalf("Send still shown while streaming; texts: %q", tt.Texts())
	}
	if stop.H != want {
		t.Errorf("Stop height = %v, want %v", stop.H, want)
	}
}

// The model chip shows the current agent's mark on its left without
// making the composer's action row taller.
func TestModelChipShowsAgentIconWithoutGrowing(t *testing.T) {
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		DataDir:   t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: agent.PiID, Name: "pi"}},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	tt.Frame()

	chip, ok := tt.Find("Agent and model: pi")
	if !ok {
		t.Fatalf("model chip missing; texts: %q", tt.Texts())
	}
	if chip.H != composerControlHeight {
		t.Errorf("model chip height = %v, want %v", chip.H, composerControlHeight)
	}
	if !tt.HasText("pi agent") {
		t.Errorf("agent icon missing; texts: %q", tt.Texts())
	}
}

// A long model name must widen the chip instead of being cut with an
// ellipsis. When the action row runs out of room, the chip wraps as a whole
// and stays readable rather than shrinking its text.
func TestModelChipShowsFullModelName(t *testing.T) {
	model := "anthropic/claude-sonnet-4-5-20250929-extra-long-model-name"
	app := New(Options{
		Version:   "test",
		Workspace: t.TempDir(),
		DataDir:   t.TempDir(),
		Agents:    []agent.LaunchSpec{{ID: "fake", Name: "fake-agent"}},
	})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	app.AgentConfigs["fake"] = []acp.ConfigOption{
		{
			ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: model,
			Options: []acp.ConfigOptionValue{{Value: model, Name: model}},
		},
		{
			ID: "thought_level", Name: "Thinking", Category: "thought_level", Type: "select", CurrentValue: "high",
			Options: []acp.ConfigOptionValue{{Value: "high", Name: "high"}},
		},
	}
	tt := mui.NewTester(app.View, 1060, 760)
	tt.Frame()

	chip, ok := tt.Find("Agent and model: " + model + " · high")
	if !ok {
		t.Fatalf("model chip missing; texts: %q", tt.Texts())
	}
	if chip.W <= 230 {
		t.Errorf("model chip width = %v, want its full content", chip.W)
	}
	if chip.X < 0 || chip.X+chip.W > 1060 {
		t.Errorf("model chip is outside the window: %+v", chip)
	}
	if _, ok := tt.Find(model); !ok {
		t.Errorf("model label is truncated; texts: %q", tt.Texts())
	}
	if _, ok := tt.Find("· high"); !ok {
		t.Errorf("thinking label is truncated; texts: %q", tt.Texts())
	}
	if chip.H != composerControlHeight {
		t.Errorf("model chip height = %v, want %v", chip.H, composerControlHeight)
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

// The reasoning levels are agent data, not a client-side fixed ladder. A
// connected session must replace the whole thought_level option list from
// the set_config_option response / config_option_update.
func TestModelSwitchRefreshesThinkingOptions(t *testing.T) {
	app, tt := newFakeApp(t)
	connect(t, app, tt)
	tt.Frame()

	if err := tt.Click("demo/small"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("demo/reasoning"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, app, tt, func() bool {
		option := current(app).Info.ConfigOption("thought_level")
		if option == nil || len(option.Options) != 3 {
			return false
		}
		return option.Options[0].Value == "low" &&
			option.Options[1].Value == "high" &&
			option.Options[2].Value == "xhigh"
	})
	tt.Frame()
	// demo/small has low/medium/high; demo/reasoning has low/high/xhigh.
	// The old list must disappear while the panel stays open.
	if tt.HasText("minimal") || tt.HasText("medium") {
		t.Errorf("stale thinking option still rendered; texts: %q", tt.Texts())
	}
	if !tt.HasText("xhigh") {
		t.Errorf("new thinking option missing; texts: %q", tt.Texts())
	}
}

// Before a model has been swept there is no ACP call that says which levels
// it supports. A draft model switch therefore hides the old model's ladder and
// drops its old pick instead of pretending it still applies.
func TestDraftModelSwitchHidesStaleThinking(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	app.AgentConfigs["fake"] = []acp.ConfigOption{
		{
			ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: "demo/small",
			Options: []acp.ConfigOptionValue{
				{Value: "demo/small", Name: "demo/small"},
				{Value: "demo/reasoning", Name: "demo/reasoning"},
			},
		},
		{
			ID: "thought_level", Name: "Thinking", Category: "thought_level", Type: "select", CurrentValue: "medium",
			Options: []acp.ConfigOptionValue{
				{Value: "low", Name: "low"},
				{Value: "medium", Name: "medium"},
				{Value: "high", Name: "high"},
			},
		},
	}
	chat.PendingConfig = map[string]string{"thought_level": "medium"}
	chat.ModelOpen = true
	tt.Frame()

	if err := tt.Click("demo/reasoning"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["model"]; got != "demo/reasoning" {
		t.Fatalf("pending model = %q, want demo/reasoning", got)
	}
	if got, ok := chat.PendingConfig["thought_level"]; ok {
		t.Errorf("old model's thinking pick survived: %q", got)
	}
	thinking := thinkingOptionOf(app.chatConfigOptions(chat))
	if thinking == nil {
		t.Fatal("thinking option missing from draft panel")
	}
	if len(thinking.Options) != 0 || thinking.CurrentValue != "" {
		t.Errorf("stale thinking ladder shown for another model: %+v", thinking)
	}
	if !tt.HasText("Connect to load levels") {
		t.Errorf("draft panel does not explain missing levels; texts: %q", tt.Texts())
	}
	// While the sweep is still running the panel says so instead of
	// pretending the user has to connect.
	app.agentConfigBusy["fake"] = true
	tt.Frame()
	if !tt.HasText("Loading levels") {
		t.Errorf("in-flight sweep is not shown; texts: %q", tt.Texts())
	}
	delete(app.agentConfigBusy, "fake")

	// Connect re-reads the levels for the picked model and clears the draft.
	app.Connect()
	waitFor(t, app, tt, func() bool {
		ch := current(app)
		if !ch.Connected || ch.PendingConfig != nil {
			return false
		}
		model := ch.Info.ConfigOption("model")
		option := ch.Info.ConfigOption("thought_level")
		return model != nil && model.CurrentValue == "demo/reasoning" &&
			option != nil && len(option.Options) == 3 && option.Options[2].Value == "xhigh"
	})
}

// draftConfigOptions is a model + thinking pair shaped like the fake agent's
// session/new answer, used to seed the discovery and per-model caches.
func draftConfigOptions(model string, levels []string, currentLevel string) []acp.ConfigOption {
	choices := make([]acp.ConfigOptionValue, 0, len(levels))
	for _, level := range levels {
		choices = append(choices, acp.ConfigOptionValue{Value: level, Name: level})
	}
	return []acp.ConfigOption{
		{
			ID: "model", Name: "Model", Category: "model", Type: "select", CurrentValue: model,
			Options: []acp.ConfigOptionValue{
				{Value: "demo/small", Name: "demo/small"},
				{Value: "demo/large", Name: "demo/large"},
				{Value: "demo/reasoning", Name: "demo/reasoning"},
			},
		},
		{
			ID: "thought_level", Name: "Thinking", Category: "thought_level", Type: "select", CurrentValue: currentLevel,
			Options: choices,
		},
	}
}

// The startup sweep stores each model's config options. A draft can therefore
// pick another model and its reasoning levels without connecting.
func TestDraftModelUsesCachedThinkingLevels(t *testing.T) {
	app, tt := newFakeApp(t)
	chat := current(app)
	app.AgentConfigs["fake"] = draftConfigOptions("demo/small", []string{"low", "medium", "high"}, "medium")
	app.AgentModelConfigs["fake"] = map[string]modelconfig.Model{
		"demo/reasoning": {ConfigOptions: draftConfigOptions("demo/reasoning", []string{"low", "high", "xhigh"}, "high")},
	}
	chat.PendingConfig = map[string]string{"thought_level": "low"}
	chat.ModelOpen = true
	tt.Frame()

	if err := tt.Click("demo/reasoning"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if got := chat.PendingConfig["model"]; got != "demo/reasoning" {
		t.Fatalf("pending model = %q, want demo/reasoning", got)
	}
	// low exists for both models, so the draft pick survives the switch.
	if got := chat.PendingConfig["thought_level"]; got != "low" {
		t.Errorf("pending thinking = %q, want low", got)
	}
	thinking := thinkingOptionOf(app.chatConfigOptions(chat))
	if thinking == nil || len(thinking.Options) != 3 || thinking.Options[2].Value != "xhigh" {
		t.Fatalf("thinking = %+v", thinking)
	}
	if thinking.CurrentValue != "low" {
		t.Errorf("thinking current = %q, want the draft pick low", thinking.CurrentValue)
	}
	if tt.HasText("medium") {
		t.Errorf("previous model's level still rendered; texts: %q", tt.Texts())
	}
	if !tt.HasText("xhigh") {
		t.Errorf("cached level missing; texts: %q", tt.Texts())
	}
}

// Startup compares the agent's model list with model-config.json: known models
// are not switched again, new ones are read and persisted for the next run.
func TestConfigProbeSweepsOnlyNewModels(t *testing.T) {
	workspace := t.TempDir()
	dataDir := t.TempDir()
	store := modelconfig.Open(filepath.Join(dataDir, modelConfigFileName))
	if err := store.Save(modelconfig.File{
		Version: modelconfig.CurrentVersion,
		Agents: map[string]map[string]modelconfig.Model{
			"fake": {
				"demo/small": {ConfigOptions: draftConfigOptions("demo/small", []string{"low", "medium", "high"}, "medium")},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	spec := agent.LaunchSpec{ID: "fake", Name: "fake-agent"}
	var known map[string]bool
	app := New(Options{
		Version:   "test",
		Workspace: workspace,
		DataDir:   dataDir,
		Agents:    []agent.LaunchSpec{spec},
		ProbeModels: func(_ context.Context, launch agent.LaunchSpec, probe session.ModelProbe) (session.Info, error) {
			if launch.ID != "fake" {
				t.Errorf("probed %q", launch.ID)
			}
			known = probe.Known
			probe.Report("demo/reasoning", draftConfigOptions("demo/reasoning", []string{"low", "high", "xhigh"}, "high"))
			return session.Info{
				SessionID:     "probe-session",
				ConfigOptions: draftConfigOptions("demo/small", []string{"low", "medium", "high"}, "medium"),
				Modes:         []acp.Mode{{ID: "default", Name: "Standard"}},
				CurrentModeID: "default",
			}, nil
		},
	})
	tt := mui.NewTester(app.View, 1060, 760)
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})

	app.probeAgentConfig(spec, true)
	waitFor(t, app, tt, func() bool {
		return len(app.AgentModelConfigs["fake"]["demo/reasoning"].ConfigOptions) > 0
	})
	if !known["demo/small"] {
		t.Errorf("known models = %v, want the persisted demo/small", known)
	}
	if known["demo/reasoning"] {
		t.Errorf("known models = %v, do not include a model that was never swept", known)
	}

	// The sweep persists what it read; the next startup skips both models.
	file, err := store.Load()
	if err != nil {
		t.Fatalf("load cache: %v", err)
	}
	models := file.Agents["fake"]
	if len(models) != 2 {
		t.Fatalf("cache models = %+v", models)
	}
	if entry := models["demo/reasoning"]; entry.ProbedAt == "" || len(entry.ConfigOptions) != 2 {
		t.Errorf("demo/reasoning entry = %+v", entry)
	}

	// The draft panel can now pick the swept model and show its levels.
	chat := current(app)
	chat.ModelOpen = true
	tt.Frame()
	if err := tt.Click("demo/reasoning"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("xhigh") || tt.HasText("medium") {
		t.Errorf("draft panel misses the swept levels; texts: %q", tt.Texts())
	}
}

// End to end: the discovery probe walks the fake agent's model list once and
// writes one entry per model, so the draft panel has every ladder without a
// connect.
func TestProbeCachesEveryModelLevels(t *testing.T) {
	app, tt := newFakeApp(t)
	spec := app.Agents[0]
	app.probeAgentConfig(spec, true)
	waitFor(t, app, tt, func() bool {
		return len(app.AgentModelConfigs["fake"]) == 3
	})

	file, err := app.modelConfig.Load()
	if err != nil {
		t.Fatalf("load cache: %v", err)
	}
	models := file.Agents["fake"]
	if len(models) != 3 {
		t.Fatalf("cached models = %v", models)
	}
	want := map[string][]string{
		"demo/small":     {"low", "medium", "high"},
		"demo/large":     {"off", "minimal", "low", "medium", "high", "xhigh"},
		"demo/reasoning": {"low", "high", "xhigh"},
	}
	for model, levels := range want {
		thinking := acp.ThinkingOption(models[model].ConfigOptions)
		if thinking == nil {
			t.Fatalf("%s has no thinking option: %+v", model, models[model])
		}
		got := make([]string, 0, len(thinking.Options))
		for _, choice := range thinking.Options {
			got = append(got, choice.Value)
		}
		if fmt.Sprint(got) != fmt.Sprint(levels) {
			t.Errorf("%s levels = %v, want %v", model, got, levels)
		}
	}

	// The draft can now switch to a model the initial session/new did not
	// describe, and still see its reasoning levels.
	chat := current(app)
	chat.ModelOpen = true
	tt.Frame()
	if err := tt.Click("demo/reasoning"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("xhigh") || tt.HasText("medium") {
		t.Errorf("draft panel misses the swept levels; texts: %q", tt.Texts())
	}
}
