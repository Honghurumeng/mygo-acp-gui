package ui

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/reed/acp-gui/internal/session"
)

// ToolDiff is one file change as the agent reported it: an ACP `diff` content
// block, or old/new texts reconstructed from rawInput. OldPath is set for a
// move, Operation is add, update, delete or move, and Partial marks a block
// that covers only a part of the file, so its line numbers are not the file's.
type ToolDiff struct {
	Path      string
	OldPath   string
	Old       string
	New       string
	Operation string
	Partial   bool
}

// DiffStats counts the lines a tool call added and removed.
type DiffStats struct{ Added, Removed int }

// toolCallView is the structured form of one tool call. Fields the agent did
// not send stay empty, and Detail keeps the flat text fallback so a call the
// parser does not understand still shows everything it came with.
type toolCallView struct {
	Kind    string
	Paths   []string
	Diffs   []ToolDiff
	Images  []ToolImage
	Exit    *int
	Cwd     string
	Offset  int
	Limit   int
	Pattern string
	URL     string
	Text    string
	Detail  string
}

// parseToolCall reads the ACP fields of a tool call into the structured view
// the cards render. It never drops payload: text the parser does not turn into
// a diff stays in Detail.
func parseToolCall(call session.ToolCall) toolCallView {
	// Orchestration calls (wait_agent, close_agent, ...) speak about child
	// sessions; their payload is transport JSON, not a result to show.
	if _, ok := subagentControlLabel(call); ok {
		return toolCallView{}
	}
	view := toolCallView{
		Kind:  normalizeToolKind(call.Kind),
		Paths: toolCallPaths(call.Locations),
	}
	text, diffs, images := parseToolContent(call.Content)
	if text == "" {
		text = rawText(call.RawOutput)
	}
	// A command's exit code is appended to its output for clients without a
	// terminal, and sent as rawOutput by codex.
	if code, body := splitToolExitCode(text); code != nil {
		text = body
		view.Exit = code
	}
	if view.Exit == nil {
		view.Exit = rawToolExitCode(call.RawOutput)
	}
	input := parseToolRawInput(call.RawInput)
	view.Cwd = input.Cwd
	view.Offset = input.Offset
	view.Limit = input.Limit
	view.Pattern = input.Pattern
	view.URL = toolCallURL(input.URL, call.Title)
	view.Diffs = diffs
	if len(view.Diffs) == 0 {
		view.Diffs = toolRawInputDiffs(call)
	}
	view.Kind = inferToolKind(view.Kind, call, view.Diffs)
	view.Images = images
	if len(view.Images) == 0 {
		view.Images = toolCallPathImages(view.Kind, view.Paths, input)
	}
	if len(view.Images) == 0 {
		view.Images = toolRawOutputImages(call.RawOutput)
	}
	imageResultJSON := toolImageResultJSON(text)
	if len(view.Images) == 0 && imageResultJSON {
		// Some adapters put the result object's JSON in a text block when they
		// have no text of their own, which is also where a replayed image-only
		// read keeps its picture.
		view.Images = toolRawOutputImages(json.RawMessage(text))
	}
	// One picture can arrive through more than one rawOutput field (pi mirrors
	// an image read into content and structuredContent); draw it once.
	if len(view.Images) > 1 {
		view.Images = dedupeToolImages(view.Images)
	}
	if len(view.Images) > 0 && imageResultJSON && (view.Kind == "read" || view.Kind == "") {
		// pi-acp serializes a tool result that has no text of its own, so an
		// image read arrives as a JSON dump of the image's base64. That is
		// transport metadata, not something to read under the picture.
		text = ""
	}
	// Agent output is terminal text: strip its escapes before anything draws
	// it, and keep the newlines and tabs that carry its layout.
	view.Text = truncate(sanitizeToolText(stripANSI(text)), toolTextLimit)
	view.Detail = view.Text
	// A command's, read's or search's arguments are not its output: the title
	// and header already name the target, so rawInput JSON would only add
	// transport noise. Other tools keep it as their readable payload.
	if view.Detail == "" && len(view.Diffs) == 0 && toolKeepsRawInput(view.Kind) {
		view.Detail = compactJSON(call.RawInput)
	}
	return view
}

// toolKeepsRawInput reports whether a kind's rawInput is the tool's readable
// payload rather than an argument the header already shows.
func toolKeepsRawInput(kind string) bool {
	switch kind {
	case "execute", "read", "search", "fetch", "think":
		return false
	default:
		return true
	}
}

// toolTextLimit caps the text kept for one tool call. Larger output is cut
// once, at parse time, so the list does not carry it on every frame.
const toolTextLimit = 256 << 10

// toolStatusTerminal reports whether a status ends a call's runtime.
func toolStatusTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "complete", "failed", "error", "cancelled", "canceled":
		return true
	}
	return false
}

// toolHasBody reports whether the card has anything to expand. Orchestration
// calls such as wait_agent have neither a diff, a picture nor text, and are
// drawn as one compact event row; a command's exit code stays in that row's
// status.
func toolHasBody(m *Message) bool {
	if m == nil {
		return false
	}
	return len(m.ToolDiffs) > 0 || len(m.ToolImages) > 0 || strings.TrimSpace(m.Detail) != ""
}

// toolContentBlock is the subset of an ACP tool call content block the cards
// read. oldText is a pointer because an added file sends null for it. URI,
// MimeType, Data and Name describe the image and resource blocks a tool can
// return; an image block carries Data, a resource link only its URI.
type toolContentBlock struct {
	Type     string          `json:"type"`
	Path     string          `json:"path"`
	OldPath  string          `json:"oldPath"`
	OldText  *string         `json:"oldText"`
	NewText  *string         `json:"newText"`
	Text     string          `json:"text"`
	URI      string          `json:"uri"`
	Name     string          `json:"name"`
	MimeType string          `json:"mimeType"`
	Data     string          `json:"data"`
	Content  json.RawMessage `json:"content"`
	Meta     struct {
		Kind string `json:"kind"`
	} `json:"_meta"`
}

// parseToolContent splits a content array into its text, diffs and pictures.
// Other block types (terminal, audio) are left to the text fallback.
func parseToolContent(raw json.RawMessage) (string, []ToolDiff, []ToolImage) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil, nil
	}
	var blocks []toolContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return rawText(raw), nil, nil
	}
	var (
		text   strings.Builder
		diffs  []ToolDiff
		images []ToolImage
	)
	for _, block := range blocks {
		if len(images) < maxToolImages {
			images = append(images, toolContentImages(block)...)
			if len(images) > maxToolImages {
				images = images[:maxToolImages]
			}
		}
		switch block.Type {
		case "diff":
			diff := toolContentDiff(block)
			if diff.Path == "" && diff.Old == "" && diff.New == "" {
				continue
			}
			diffs = append(diffs, diff)
		case "content":
			if block.Text != "" {
				text.WriteString(block.Text)
				continue
			}
			text.WriteString(rawText(block.Content))
		case "text":
			text.WriteString(block.Text)
		}
	}
	// One path split into several blocks carries no offsets: the blocks are
	// hunks, so the file's line numbers cannot be shown for them.
	for i := range diffs {
		if countToolDiffPath(diffs, diffs[i].Path) > 1 {
			diffs[i].Partial = true
		}
	}
	return text.String(), diffs, images
}

// toolContentDiff maps one ACP diff block onto ToolDiff. `_meta.kind` is
// codex's private extension: when present it names the operation (add, delete,
// update) and marks an update as a hunk rather than a whole file.
func toolContentDiff(block toolContentBlock) ToolDiff {
	diff := ToolDiff{Path: block.Path, OldPath: block.OldPath}
	if block.OldText != nil {
		diff.Old = *block.OldText
	}
	if block.NewText != nil {
		diff.New = *block.NewText
	}
	switch strings.ToLower(strings.TrimSpace(block.Meta.Kind)) {
	case "add", "create", "created":
		diff.Operation = "add"
	case "delete", "remove", "deleted":
		diff.Operation = "delete"
	case "move", "rename":
		diff.Operation = "move"
	case "update", "modify", "modified":
		diff.Operation = "update"
	}
	if diff.Operation == "" {
		diff.Operation = inferDiffOperation(diff)
	}
	if diff.Operation == "update" && block.Meta.Kind != "" {
		diff.Partial = true
	}
	return diff
}

// toolRawInput is the subset of rawInput the cards read. Some agents send only
// oldText/newText, or an edits array, instead of a diff block.
type toolRawInput struct {
	Path     string          `json:"path"`
	FilePath string          `json:"file_path"`
	OldPath  string          `json:"oldPath"`
	OldText  *string         `json:"oldText"`
	NewText  *string         `json:"newText"`
	Content  string          `json:"content"`
	Edits    json.RawMessage `json:"edits"`
	Command  string          `json:"command"`
	Cwd      string          `json:"cwd"`
	Pattern  string          `json:"pattern"`
	Glob     string          `json:"glob"`
	URL      string          `json:"url"`
	Offset   int             `json:"offset"`
	Limit    int             `json:"limit"`
}

func parseToolRawInput(raw json.RawMessage) toolRawInput {
	var input toolRawInput
	if len(raw) == 0 || string(raw) == "null" {
		return input
	}
	if err := json.Unmarshal(raw, &input); err != nil {
		return toolRawInput{}
	}
	return input
}

// toolRawInputDiffs reconstructs a diff from rawInput when no diff block
// arrived: pi's streaming phase and older agents send oldText/newText, an
// edits array, or the whole content of a written file.
func toolRawInputDiffs(call session.ToolCall) []ToolDiff {
	input := parseToolRawInput(call.RawInput)
	path := strings.TrimSpace(input.Path)
	if path == "" {
		path = strings.TrimSpace(input.FilePath)
	}
	var diffs []ToolDiff
	add := func(oldText, newText string) {
		if oldText == "" && newText == "" {
			return
		}
		diff := ToolDiff{Path: path, OldPath: input.OldPath, Old: oldText, New: newText}
		diff.Operation = inferDiffOperation(diff)
		diffs = append(diffs, diff)
	}
	if input.OldText != nil && input.NewText != nil {
		add(*input.OldText, *input.NewText)
	}
	for _, edit := range parseToolEdits(input.Edits) {
		add(edit.Old, edit.New)
	}
	if len(diffs) == 0 && input.Content != "" && isWriteTool(call) {
		add("", input.Content)
	}
	for i := range diffs {
		if countToolDiffPath(diffs, diffs[i].Path) > 1 {
			diffs[i].Partial = true
		}
	}
	return diffs
}

type toolEdit struct{ Old, New string }

// parseToolEdits reads the edits argument, which arrives as an array or as a
// JSON string while the arguments stream in.
func parseToolEdits(raw json.RawMessage) []toolEdit {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		raw = json.RawMessage(text)
	}
	var items []struct {
		OldText   *string `json:"oldText"`
		NewText   *string `json:"newText"`
		OldString *string `json:"old_string"`
		NewString *string `json:"new_string"`
	}
	if json.Unmarshal(raw, &items) != nil {
		return nil
	}
	edits := make([]toolEdit, 0, len(items))
	for _, item := range items {
		var edit toolEdit
		switch {
		case item.OldText != nil && item.NewText != nil:
			edit.Old, edit.New = *item.OldText, *item.NewText
		case item.OldString != nil && item.NewString != nil:
			edit.Old, edit.New = *item.OldString, *item.NewString
		default:
			continue
		}
		edits = append(edits, edit)
	}
	return edits
}

func isWriteTool(call session.ToolCall) bool {
	title := strings.ToLower(strings.TrimSpace(call.Title))
	return strings.HasPrefix(title, "write") || strings.HasPrefix(title, "create")
}

func inferDiffOperation(diff ToolDiff) string {
	switch {
	case diff.OldPath != "" && diff.OldPath != diff.Path:
		return "move"
	case diff.Old == "" && diff.New != "":
		return "add"
	case diff.New == "" && diff.Old != "":
		return "delete"
	default:
		return "update"
	}
}

func countToolDiffPath(diffs []ToolDiff, path string) int {
	if path == "" {
		return 1
	}
	count := 0
	for _, diff := range diffs {
		if diff.Path == path {
			count++
		}
	}
	return count
}

// normalizeToolKind lowercases the ACP kind and folds agent spellings onto the
// kinds the cards know.
func normalizeToolKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "edit", "write", "modify", "patch":
		return "edit"
	case "delete", "remove":
		return "delete"
	case "move", "rename":
		return "move"
	case "read", "search", "execute", "think", "fetch", "switch_mode", "other":
		return strings.ToLower(strings.TrimSpace(kind))
	case "":
		return ""
	default:
		return "other"
	}
}

// inferToolKind fills the kind of an agent that does not send one, and refines
// "other": pi reports grep, find and ls as other, and their titles still name
// the tool. The title's verb is the strongest signal; rawInput only breaks
// ties.
func inferToolKind(kind string, call session.ToolCall, diffs []ToolDiff) string {
	if kind != "" && kind != "other" {
		return kind
	}
	if len(diffs) > 0 {
		return "edit"
	}
	switch firstWordLower(call.Title) {
	case "read", "cat":
		return "read"
	case "edit", "write", "patch", "apply_patch":
		return "edit"
	case "grep", "find", "search", "ls", "glob":
		return "search"
	case "bash", "sh", "shell", "exec", "run":
		return "execute"
	case "fetch", "curl", "http", "webfetch":
		return "fetch"
	case "think", "thinking", "reason", "reasoning":
		return "think"
	}
	input := parseToolRawInput(call.RawInput)
	switch {
	case input.Command != "":
		return "execute"
	case input.OldText != nil || input.NewText != nil || len(input.Edits) > 0:
		return "edit"
	case input.Pattern != "" || input.Glob != "":
		return "search"
	case input.URL != "":
		return "fetch"
	}
	if kind != "" {
		return kind
	}
	// A status-and-result update names neither the tool nor its arguments. It
	// is how pi-acp closes a live call and how it replays every historic one,
	// so it carries no evidence: report no kind instead of "other", which
	// would overwrite the kind the opening tool_call declared.
	if strings.TrimSpace(call.Title) == "" && len(call.RawInput) == 0 {
		return ""
	}
	return "other"
}

// toolCallURL is the target of a fetch: rawInput.url when the agent sends it,
// else the first URL-looking word of the title.
func toolCallURL(raw, title string) string {
	if raw = strings.TrimSpace(raw); raw != "" {
		return raw
	}
	for _, field := range strings.Fields(title) {
		field = strings.Trim(field, "'\"`()<>,")
		if strings.HasPrefix(field, "http://") || strings.HasPrefix(field, "https://") {
			return field
		}
	}
	return ""
}

func firstWordLower(text string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	if len(fields) == 0 {
		return ""
	}
	return strings.Trim(fields[0], ":'\"`")
}

// toolCallPaths reads the paths of ACP locations. The protocol sends an array;
// a single object is tolerated.
func toolCallPaths(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var items []struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &items) != nil {
		var one struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(raw, &one) != nil {
			return nil
		}
		items = append(items, one)
	}
	paths := make([]string, 0, len(items))
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		path := strings.TrimSpace(item.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

var toolExitTail = regexp.MustCompile(`(?m)(?:\n|^)exit code: (-?\d+)\s*$`)

// splitToolExitCode removes the exit line pi appends to a command's output and
// returns the code.
func splitToolExitCode(text string) (*int, string) {
	match := toolExitTail.FindStringSubmatchIndex(text)
	if match == nil {
		return nil, text
	}
	code, err := strconv.Atoi(text[match[2]:match[3]])
	if err != nil {
		return nil, text
	}
	return &code, strings.TrimRight(text[:match[0]], "\n")
}

// rawToolExitCode reads codex's `rawOutput: {"exit_code": N}`.
func rawToolExitCode(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var output struct {
		ExitCode  *int `json:"exit_code"`
		ExitCode2 *int `json:"exitCode"`
	}
	if json.Unmarshal(raw, &output) != nil {
		return nil
	}
	if output.ExitCode != nil {
		return output.ExitCode
	}
	return output.ExitCode2
}
