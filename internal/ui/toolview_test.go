package ui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/reed/acp-gui/internal/agent"
	"github.com/reed/acp-gui/internal/session"
)

// ACP diff content blocks are the payload pi-acp and codex-acp send for file
// changes. They must become ToolDiffs instead of falling through to rawInput
// JSON, which is what the card used to show.
func TestParseToolCallReadsDiffBlocks(t *testing.T) {
	call := session.ToolCall{
		ToolCallID: "tool-1",
		Title:      "edit a.go",
		Kind:       "edit",
		Status:     "completed",
		Content:    json.RawMessage(`[{"type":"diff","path":"a.go","oldText":"one\ntwo\n","newText":"one\nTWO\n"}]`),
		Locations:  json.RawMessage(`[{"path":"/w/a.go","line":2}]`),
	}
	view := parseToolCall(call)
	if len(view.Diffs) != 1 {
		t.Fatalf("diffs = %+v, want one block", view.Diffs)
	}
	diff := view.Diffs[0]
	if diff.Path != "a.go" || diff.Old != "one\ntwo\n" || diff.New != "one\nTWO\n" {
		t.Errorf("diff = %+v", diff)
	}
	if diff.Operation != "update" || diff.Partial {
		t.Errorf("operation = %q partial = %v, want a whole-file update", diff.Operation, diff.Partial)
	}
	if view.Detail != "" {
		t.Errorf("detail = %q, want the diff to replace the raw fallback", view.Detail)
	}
	if view.Kind != "edit" || len(view.Paths) != 1 || view.Paths[0] != "/w/a.go" {
		t.Errorf("kind = %q paths = %v", view.Kind, view.Paths)
	}
}

func TestParseToolCallKeepsAddedAndDeletedSides(t *testing.T) {
	add := parseToolCall(session.ToolCall{
		Title: "write new.go", Kind: "edit",
		Content: json.RawMessage(`[{"type":"diff","path":"new.go","oldText":null,"newText":"hello\n","_meta":{"kind":"add"}}]`),
	})
	if len(add.Diffs) != 1 || add.Diffs[0].Operation != "add" || add.Diffs[0].Old != "" || add.Diffs[0].New != "hello\n" {
		t.Fatalf("add diff = %+v", add.Diffs)
	}
	if add.Diffs[0].Partial {
		t.Error("an added file is a whole file, not a hunk")
	}
	del := parseToolCall(session.ToolCall{
		Title: "delete gone.go", Kind: "delete",
		Content: json.RawMessage(`[{"type":"diff","path":"gone.go","oldText":"bye\n","newText":"","_meta":{"kind":"delete"}}]`),
	})
	if len(del.Diffs) != 1 || del.Diffs[0].Operation != "delete" || del.Diffs[0].New != "" {
		t.Fatalf("delete diff = %+v", del.Diffs)
	}
}

// codex sends one block per hunk, each with its own old/new text and no
// offsets. Those line numbers are the block's, so they stay hidden.
func TestParseToolCallMarksHunkBlocksPartial(t *testing.T) {
	call := session.ToolCall{
		Title: "Edit files", Kind: "edit",
		Content: json.RawMessage(`[
			{"type":"diff","path":"a.go","oldText":"one\n","newText":"ONE\n","_meta":{"kind":"update"}},
			{"type":"diff","path":"a.go","oldText":"two\n","newText":"TWO\n","_meta":{"kind":"update"}},
			{"type":"diff","path":"b.go","oldText":"three\n","newText":"THREE\n","_meta":{"kind":"update"}}]`),
	}
	view := parseToolCall(call)
	if len(view.Diffs) != 3 {
		t.Fatalf("diffs = %+v, want three hunks", view.Diffs)
	}
	if !view.Diffs[0].Partial || !view.Diffs[1].Partial {
		t.Error("hunks of the same file must not show file line numbers")
	}
	if !view.Diffs[2].Partial {
		t.Error("a single codex hunk still carries no offset")
	}
}

// Older payloads carry only rawInput: oldText/newText, an edits array (or the
// string the arguments stream as), or a written file's whole content.
func TestParseToolCallRebuildsDiffFromRawInput(t *testing.T) {
	simple := parseToolCall(session.ToolCall{
		Title: "edit a.go", Kind: "edit",
		RawInput: json.RawMessage(`{"path":"a.go","oldText":"x","newText":"y"}`),
	})
	if len(simple.Diffs) != 1 || simple.Diffs[0].Old != "x" || simple.Diffs[0].New != "y" {
		t.Fatalf("oldText/newText diff = %+v", simple.Diffs)
	}
	if simple.Detail != "" {
		t.Errorf("detail = %q, want the rebuilt diff to replace the JSON", simple.Detail)
	}

	edits := parseToolCall(session.ToolCall{
		Title: "edit a.go", Kind: "edit",
		RawInput: json.RawMessage(`{"path":"a.go","edits":"[{\"oldText\":\"a\",\"newText\":\"b\"},{\"oldText\":\"c\",\"newText\":\"d\"}]"}`),
	})
	if len(edits.Diffs) != 2 {
		t.Fatalf("edits diff = %+v, want two", edits.Diffs)
	}
	if !edits.Diffs[0].Partial || !edits.Diffs[1].Partial {
		t.Error("multi-edit segments must not show file line numbers")
	}

	write := parseToolCall(session.ToolCall{
		Title: "write new.go", Kind: "edit",
		RawInput: json.RawMessage(`{"path":"new.go","content":"hello\n"}`),
	})
	if len(write.Diffs) != 1 || write.Diffs[0].Operation != "add" || write.Diffs[0].New != "hello\n" {
		t.Fatalf("write diff = %+v", write.Diffs)
	}
}

// The exit code arrives in the command's text tail (pi-acp) or in rawOutput
// (codex); both are read and the tail line is not shown twice.
func TestParseToolCallReadsExitCodes(t *testing.T) {
	text := parseToolCall(session.ToolCall{
		Title: "go test ./...", Kind: "execute",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"ok  pkg\n\nexit code: 1"}}]`),
	})
	if text.Exit == nil || *text.Exit != 1 {
		t.Fatalf("exit = %v", text.Exit)
	}
	if text.Detail != "ok  pkg" {
		t.Errorf("detail = %q, want the exit line stripped", text.Detail)
	}

	raw := parseToolCall(session.ToolCall{
		Title: "ls", Kind: "execute", Status: "completed",
		Content:   json.RawMessage(`[{"type":"content","content":{"type":"text","text":"a.go\n"}}]`),
		RawOutput: json.RawMessage(`{"exit_code":2}`),
	})
	if raw.Exit == nil || *raw.Exit != 2 {
		t.Fatalf("raw exit = %v", raw.Exit)
	}
	if raw.Detail != "a.go" {
		t.Errorf("raw detail = %q", raw.Detail)
	}
}

// An agent without a kind still gets the right card from its title, and one
// the parser does not know keeps its text instead of losing it.
func TestParseToolCallInfersKindAndKeepsFallback(t *testing.T) {
	if got := parseToolCall(session.ToolCall{Title: "grep \"needle\" in ."}).Kind; got != "search" {
		t.Errorf("kind = %q, want search", got)
	}
	if got := parseToolCall(session.ToolCall{Title: "bash go test"}).Kind; got != "execute" {
		t.Errorf("kind = %q, want execute", got)
	}
	unknown := parseToolCall(session.ToolCall{
		Title:    "wait",
		RawInput: json.RawMessage(`{"seconds":1}`),
	})
	if unknown.Kind != "other" || unknown.Detail != `{"seconds":1}` {
		t.Errorf("unknown view = %+v", unknown)
	}
}

// The presentation step must move a diff out of rawInput JSON and keep later
// status-only updates from clearing it again.
func TestApplyToolPresentationMovesDiffIntoCard(t *testing.T) {
	message := &Message{Title: "edit a.go", Detail: `{"path":"a.go","oldText":"x","newText":"y"}`}
	call := session.ToolCall{
		Title: "edit a.go", Kind: "edit", Status: "completed",
		Content: json.RawMessage(`[{"type":"diff","path":"a.go","oldText":"x\n","newText":"y\n"}]`),
	}
	applyToolPresentation(message, call)
	if message.ToolKind != "edit" || len(message.ToolDiffs) != 1 {
		t.Fatalf("presentation = %+v", message)
	}
	if message.Detail != "" {
		t.Errorf("detail = %q, want the diff to replace the raw JSON", message.Detail)
	}
	if message.ToolStart.IsZero() || message.ToolEnd.IsZero() {
		t.Errorf("runtime not bracketed: start=%v end=%v", message.ToolStart, message.ToolEnd)
	}
	// codex sends a status-only update after the diff: no title, no kind.
	applyToolPresentation(message, session.ToolCall{Status: "completed"})
	if len(message.ToolDiffs) != 1 || message.Detail != "" || message.ToolKind != "edit" {
		t.Errorf("status update regressed presentation: %+v", message)
	}
}

// session/load replays a call as one tool_call that names the kind and its
// arguments, then a tool_call_update that carries only the result. The second
// update must keep the card, instead of turning it into the generic one.
func TestReplayToolCallsKeepTheirKind(t *testing.T) {
	read := &Message{Role: RoleTool, ToolCallID: "t1"}
	applyToolPresentation(read, session.ToolCall{
		ToolCallID: "t1", Title: "read a.go", Kind: "read", Status: "completed",
		RawInput: json.RawMessage(`{"path":"a.go","offset":1,"limit":5}`),
	})
	applyToolPresentation(read, session.ToolCall{
		ToolCallID: "t1", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"package a\n"}}]`),
	})
	if read.ToolKind != "read" {
		t.Errorf("read kind = %q, want read", read.ToolKind)
	}
	if read.Detail != "package a" || read.ToolOffset != 1 || read.ToolLimit != 5 {
		t.Errorf("read presentation = %+v", read)
	}

	edit := &Message{Role: RoleTool, ToolCallID: "t2"}
	applyToolPresentation(edit, session.ToolCall{
		ToolCallID: "t2", Title: "edit a.go", Kind: "edit", Status: "completed",
		RawInput: json.RawMessage(`{"path":"a.go","edits":[{"oldText":"x","newText":"y"}]}`),
	})
	applyToolPresentation(edit, session.ToolCall{
		ToolCallID: "t2", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"edited a.go"}}]`),
	})
	if edit.ToolKind != "edit" || len(edit.ToolDiffs) != 1 {
		t.Errorf("edit presentation = %+v", edit)
	}
}

// Terminal escapes in command output are stripped before the text is drawn,
// and a command's arguments stay out of the body: the title is the command.
func TestParseToolCallStripsEscapesAndCommandArguments(t *testing.T) {
	escaped := parseToolCall(session.ToolCall{
		Title: "go test ./...", Kind: "execute", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"\u001b[31mFAIL\u001b[0m\tpkg\n"}}]`),
	})
	if escaped.Detail != "FAIL\tpkg" {
		t.Errorf("detail = %q, want the escape sequences gone", escaped.Detail)
	}
	pending := parseToolCall(session.ToolCall{
		Title: "ls", Kind: "execute",
		RawInput: json.RawMessage(`{"command":"ls","cwd":"/w"}`),
	})
	if pending.Detail != "" {
		t.Errorf("detail = %q, want no rawInput JSON for a command", pending.Detail)
	}
	if pending.Cwd != "/w" {
		t.Errorf("cwd = %q", pending.Cwd)
	}
	if toolHasBody(&Message{ToolKind: "execute", ToolExit: intPtr(0)}) {
		t.Error("a command with no output should stay a compact row")
	}
}

func intPtr(v int) *int { return &v }

// A read carries the line range it covers, and a search carries the pattern
// the result lines are highlighted with.
func TestParseToolCallKeepsReadRangeAndPattern(t *testing.T) {
	read := parseToolCall(session.ToolCall{
		Title: "read a.go", Kind: "read",
		RawInput: json.RawMessage(`{"path":"a.go","offset":10,"limit":20}`),
	})
	if read.Offset != 10 || read.Limit != 20 {
		t.Errorf("read range = %d+%d", read.Offset, read.Limit)
	}
	search := parseToolCall(session.ToolCall{
		Title: `grep "needle" in .`, Kind: "search",
		RawInput: json.RawMessage(`{"pattern":"needle","path":"."}`),
	})
	if search.Pattern != "needle" {
		t.Errorf("pattern = %q", search.Pattern)
	}

	matches, files, ok := toolSearchStats("a.go:1:needle\nb.go:2:needle\na.go:9:x\n\n[3 matches limit reached.]")
	if !ok || matches != 3 || files != 2 {
		t.Errorf("search stats = %d matches in %d files (ok=%v)", matches, files, ok)
	}
	if got := toolSearchLabel(1, 1); got != "1 match in 1 file" {
		t.Errorf("singular label = %q", got)
	}
	if got := toolSearchLabel(3, 2); got != "3 matches in 2 files" {
		t.Errorf("plural label = %q", got)
	}
}

// A fetch keeps its URL, from rawInput or from the title, for the header and
// the link in its body.
func TestParseToolCallKeepsFetchURL(t *testing.T) {
	fromInput := parseToolCall(session.ToolCall{
		Title: "fetch docs", Kind: "fetch",
		RawInput: json.RawMessage(`{"url":"https://example.com/docs"}`),
	})
	if fromInput.URL != "https://example.com/docs" {
		t.Errorf("url = %q", fromInput.URL)
	}
	fromTitle := parseToolCall(session.ToolCall{
		Title: "fetch https://example.com/a?b=c", Kind: "fetch",
	})
	if fromTitle.URL != "https://example.com/a?b=c" {
		t.Errorf("title url = %q", fromTitle.URL)
	}
	if got := toolURLHost("https://example.com/a?b=c"); got != "example.com" {
		t.Errorf("host = %q", got)
	}
}

// The copied change reads as a patch, and the Files viewer is only offered a
// path inside the active workspace.
func TestToolDiffTextAndWorkspaceEntry(t *testing.T) {
	app := New(Options{Version: "test", Workspace: t.TempDir(), Agents: []agent.LaunchSpec{}})
	t.Cleanup(func() {
		app.Shutdown()
		app.Flush()
	})
	text := toolDiffText([]ToolDiff{{Path: "a.go", Old: "one\ntwo\n", New: "one\nTWO\n"}})
	for _, want := range []string{"--- a.go", "+++ a.go", "-two", "+TWO"} {
		if !strings.Contains(text, want) {
			t.Errorf("patch misses %q: %q", want, text)
		}
	}
	workspace := app.activeFileWorkspace()
	if workspace == "" {
		t.Skip("no active workspace")
	}
	if entry, ok := app.toolWorkspaceEntry(filepath.Join(workspace, "a.go")); !ok || entry.Path != "a.go" {
		t.Errorf("workspace entry = %+v ok=%v", entry, ok)
	}
	if _, ok := app.toolWorkspaceEntry(filepath.Join(workspace, "..", "outside.go")); ok {
		t.Error("a path outside the workspace must not open")
	}
}

// pi reports grep/find/ls as other; their titles still name the tool, so they
// get the search card. A quoted path in a title is a fallback when the agent
// sends no locations.
func TestInferSearchFromOtherAndPathFromTitle(t *testing.T) {
	grep := parseToolCall(session.ToolCall{
		Title: `grep "needle" in .`, Kind: "other", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"a.go:1:needle\n"}}]`),
	})
	if grep.Kind != "search" {
		t.Errorf("kind = %q, want search", grep.Kind)
	}
	mcp := parseToolCall(session.ToolCall{Title: "MCP search on docs", Kind: "other"})
	if mcp.Kind != "other" {
		t.Errorf("mcp kind = %q, want other", mcp.Kind)
	}
	if got := toolPathFromTitle("read a.go"); got != "a.go" {
		t.Errorf("title path = %q", got)
	}
	if got := toolPathFromTitle(`Read file 'cmd/app/main.go'`); got != "cmd/app/main.go" {
		t.Errorf("quoted title path = %q", got)
	}
}

// A picture a tool returned or viewed is kept as a ToolImage: embedded base64
// from an ACP image block, a resource link's path, the read's own path, and a
// result object's image array all reach the card instead of being dropped.
func TestParseToolCallReadsImages(t *testing.T) {
	const png = "iVBORw0KGgo="
	embedded := parseToolCall(session.ToolCall{
		Title: "Image generation", Kind: "other", Status: "completed",
		Content: json.RawMessage(`[
			{"type":"content","content":{"type":"text","text":"Revised prompt: a dot"}},
			{"type":"content","content":{"type":"image","data":"` + png + `","mimeType":"image/png","uri":"/w/out.png"}}]`),
	})
	if len(embedded.Images) != 1 {
		t.Fatalf("embedded images = %+v, want one", embedded.Images)
	}
	if image := embedded.Images[0]; image.Data != png || image.Path != "/w/out.png" || image.MimeType != "image/png" {
		t.Errorf("embedded image = %+v", image)
	}
	if embedded.Detail != "Revised prompt: a dot" {
		t.Errorf("detail = %q, want the text beside the image", embedded.Detail)
	}

	viewed := parseToolCall(session.ToolCall{
		Title: "View Image /w/shot.png", Kind: "read", Status: "completed",
		Locations: json.RawMessage(`[{"path":"/w/shot.png"}]`),
		Content: json.RawMessage(`[
			{"type":"content","content":{"type":"resource_link","name":"/w/shot.png","uri":"/w/shot.png"}}]`),
	})
	if len(viewed.Images) != 1 || viewed.Images[0].Path != "/w/shot.png" {
		t.Fatalf("viewed images = %+v", viewed.Images)
	}
	if viewed.Detail != "" {
		t.Errorf("detail = %q, want no text for a path-only view", viewed.Detail)
	}

	fromInput := parseToolCall(session.ToolCall{
		Title: "read photo.jpg", Kind: "read", Status: "completed",
		RawInput: json.RawMessage(`{"path":"photo.jpg"}`),
	})
	if len(fromInput.Images) != 1 || fromInput.Images[0].Path != "photo.jpg" {
		t.Fatalf("rawInput images = %+v", fromInput.Images)
	}

	fromOutput := parseToolCall(session.ToolCall{
		Title: "MCP picture", Kind: "other", Status: "completed",
		RawOutput: json.RawMessage(`{"content":[{"type":"image","data":"` + png + `","mimeType":"image/png"}]}`),
	})
	if len(fromOutput.Images) != 1 || fromOutput.Images[0].Data != png {
		t.Fatalf("rawOutput images = %+v", fromOutput.Images)
	}
}

// pi-acp serializes an image-only result as a JSON dump of its base64. The
// picture replaces that dump, so the card does not show a wall of metadata.
func TestParseToolCallDropsImageResultJSON(t *testing.T) {
	dump, err := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "image", "data": "iVBORw0KGgo=", "mimeType": "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal([]any{
		map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": string(dump)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	view := parseToolCall(session.ToolCall{
		Title: "read shot.png", Kind: "read", Status: "completed",
		Locations: json.RawMessage(`[{"path":"/w/shot.png"}]`),
		Content:   content,
	})
	if len(view.Images) != 1 {
		t.Fatalf("images = %+v", view.Images)
	}
	if view.Detail != "" {
		t.Errorf("detail = %q, want the image JSON dump gone", view.Detail)
	}

	// The result-only update of a replay has no locations; the image is read
	// from the serialized result instead.
	replayed := parseToolCall(session.ToolCall{
		Status:  "completed",
		Content: content,
	})
	if len(replayed.Images) != 1 || replayed.Images[0].Data != "iVBORw0KGgo=" {
		t.Fatalf("replayed images = %+v", replayed.Images)
	}
	if replayed.Detail != "" {
		t.Errorf("replayed detail = %q, want the JSON gone", replayed.Detail)
	}
}

func TestParseToolCallDeduplicatesMirroredImages(t *testing.T) {
	const png = "iVBORw0KGgo="
	// pi's read result repeats the picture in rawOutput.content and
	// rawOutput.structuredContent; the closing update has no locations or
	// rawInput, so the picture is read from rawOutput alone.
	view := parseToolCall(session.ToolCall{
		ToolCallID: "read-image", Title: "read resources/icon.png", Kind: "read", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"Read image file [image/png]"}}]`),
		RawOutput: json.RawMessage(`{
			"content":[{"type":"text","text":"Read image file [image/png]"},
				{"type":"image","data":"` + png + `","mimeType":"image/png"}],
			"structuredContent":{"type":"image","data":"` + png + `","mimeType":"image/png","note":"Read image file [image/png]"}}`),
	})
	if len(view.Images) != 1 {
		t.Fatalf("images = %+v, want the mirrored picture drawn once", view.Images)
	}
	if view.Detail != "Read image file [image/png]" {
		t.Errorf("detail = %q, want the result text kept", view.Detail)
	}

	// Distinct pictures stay distinct: dedupe is by identity, not by count.
	gallery := parseToolCall(session.ToolCall{
		Title: "Image generation", Kind: "other", Status: "completed",
		RawOutput: json.RawMessage(`{"content":[
			{"type":"image","data":"` + png + `","mimeType":"image/png"},
			{"type":"image","data":"iVBORw0KGgoAAA==","mimeType":"image/png"}]}`),
	})
	if len(gallery.Images) != 2 {
		t.Fatalf("gallery images = %+v, want both pictures kept", gallery.Images)
	}
}

// Later updates must not lose the picture a replay's opening call reported,
// and the serialized result of the closing update must not become its body.
func TestApplyToolPresentationKeepsImages(t *testing.T) {
	message := &Message{Role: RoleTool, ToolCallID: "image-1"}
	applyToolPresentation(message, session.ToolCall{
		ToolCallID: "image-1", Title: "read shot.png", Kind: "read", Status: "completed",
		RawInput: json.RawMessage(`{"path":"shot.png"}`),
	})
	if len(message.ToolImages) != 1 || message.ToolImages[0].Path != "shot.png" {
		t.Fatalf("opening images = %+v", message.ToolImages)
	}
	applyToolPresentation(message, session.ToolCall{ToolCallID: "image-1", Status: "completed"})
	if len(message.ToolImages) != 1 {
		t.Fatalf("status update dropped the image: %+v", message.ToolImages)
	}

	applyToolPresentation(message, session.ToolCall{
		ToolCallID: "image-1", Status: "completed",
		Content: json.RawMessage(`[{"type":"content","content":{"type":"text","text":"{\n  \"content\": [\n    {\n      \"type\": \"image\",\n      \"data\": \"iVBORw0KGgo=\"\n    }\n  ]\n}"}}]`),
	})
	if len(message.ToolImages) != 1 || message.ToolImages[0].Data != "iVBORw0KGgo=" {
		t.Fatalf("closing images = %+v", message.ToolImages)
	}
	if message.Detail != "" {
		t.Errorf("detail = %q, want the result JSON dropped", message.Detail)
	}
}
