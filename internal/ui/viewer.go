package ui

import (
	"math"
	"path/filepath"
	"strings"

	mui "github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

// viewerMode is what the floating file viewer shows: a diff of two texts, a
// read-only file, or an editor. The zero value is the plain view, so a new
// summary or file opens as its content rather than as a diff.
type viewerMode uint8

const (
	viewerView viewerMode = iota
	viewerEdit
	viewerDiff
)

const (
	// The viewer card is shared by the Git panel's diff, the task summary and
	// the Files panel's file. Its width follows the window until the user
	// drags the card's left edge, after which ViewerWidth is kept in settings.
	viewerMinWidth          = 420
	viewerDefaultMinWidth   = 480
	viewerDefaultMaxWidth   = 900
	viewerSplitWidth        = 700
	viewerDefaultHeightPct  = 86
	viewerCardRadius        = 10
	viewerResizeHandleWidth = 6
)

// viewerSpec describes one document the shared card shows. Callers own the
// mode and the loaded text; the card owns the editor, diff and markdown view
// state and its own width.
type viewerSpec struct {
	// Key names the document for the card's local state. It should change
	// when another file or task is shown in the same popover.
	Key string
	// Title is the bold first line; Subtitle is the muted line below it,
	// usually the path. Tooltip is the title's full text.
	Title    string
	Subtitle string
	Tooltip  string
	// Filename chooses syntax highlighting for the view and edit states.
	Filename string

	// Body is shown by View and Edit. Old and New are compared by Diff.
	Body     string
	Old, New string
	// Markdown renders Body as Markdown in View mode, and Edit edits its
	// source text.
	Markdown bool

	// Modes are shown in this order. Edit is hidden when Save is nil.
	Modes []viewerMode
	Mode  viewerMode
	// OnMode receives a click on a mode button.
	OnMode func(viewerMode)
	// Save writes the editor's text. The caller runs it off the UI thread and
	// reports progress through Saving, SaveErr and Notice.
	Save func(text string)
	// OnClose closes the popover from its header button. The popover's own
	// Escape and outside presses are handled by the caller.
	OnClose func()

	Saving  bool
	SaveErr string
	Notice  string

	Busy     bool
	Err      string
	BusyText string
	// ErrorTitle heads the danger alert, as "Cannot show this diff".
	ErrorTitle string
	// Empty text and title fill the diff's empty state and an empty Markdown
	// view; both have defaults.
	EmptyDiffTitle string
	EmptyDiffText  string
	EmptyViewTitle string
	EmptyViewText  string

	// HeightPercent is the card's height, 86% when zero.
	HeightPercent float32
}

// viewerCardState is the cross-frame state of one document: the editable
// buffer, the read-only state that shares it, the diff built from Old and New,
// and the texts they were built for.
type viewerCardState struct {
	body   string
	font   float32
	editor *editor.State
	diff   *editor.DiffState
	old    string
	new    string
}

// viewerCard draws the floating card that the Git, Tasks and Files panels
// share: a header with mode buttons, save and close, and a body that is a
// diff, a read-only file or an editor. It also adds the left-edge resize
// handle. The caller owns the popover and its open flag.
func (a *App) viewerCard(c *mui.Context, panel, anchor mui.Element, spec viewerSpec) {
	if spec.Mode == viewerEdit && spec.Save == nil {
		spec.Mode = viewerView
	}
	t := c.Theme()
	windowW, _ := c.Size()
	width := a.viewerCardWidth(windowW)
	height := spec.HeightPercent
	if height <= 0 {
		height = viewerDefaultHeightPct
	}
	panel.Width(width).HeightPercent(height).Radius(viewerCardRadius).Clip().
		Background(t.Background).Border(1, t.Border).
		Shadow(0, 12, 32, 0, mui.RGBA(0, 0, 0, 0.24)).
		AttachTo(anchor, mui.AnchorStart, mui.AnchorEnd).
		MarginEnd(10).AttachEdgeMargin(12)
	key := "viewer:" + spec.Key
	if spec.Key == "" {
		key = "viewer:" + spec.Title
	}
	state := mui.Local(panel, key, func() viewerCardState { return viewerCardState{} })
	// Build the editor before the header so its dirty marker is current.
	a.viewerSyncEditor(state, spec)
	panel.Children(func() {
		a.viewerHeader(c, spec, state)
		a.viewerStatus(c, spec)
		switch {
		case spec.Busy:
			centered(c, func() {
				text := spec.BusyText
				if strings.TrimSpace(text) == "" {
					text = T("Loading…")
				}
				busyLine(c, text)
			})
		case spec.Err != "":
			mui.Column(c).Grow(1).Padding(12, 12).Children(func() {
				title := spec.ErrorTitle
				if strings.TrimSpace(title) == "" {
					title = T("Cannot show this file")
				}
				dangerAlert(c, title, spec.Err)
			})
		default:
			a.viewerBody(c, state, spec, width)
		}
		a.viewerResizeHandle(c, panel, windowW)
	})
}

// viewerHeader names the document and holds the mode switch, Save and Close.
func (a *App) viewerHeader(c *mui.Context, spec viewerSpec, state *viewerCardState) {
	t := c.Theme()
	dirty := state.editor != nil && state.editor.Buffer.Modified()
	subtitle := spec.Subtitle
	if dirty {
		if strings.TrimSpace(subtitle) == "" {
			subtitle = T("unsaved changes")
		} else {
			subtitle += " · " + T("unsaved changes")
		}
	}
	mui.Row(c).Height(48).Gap(3).Padding(0, 12).AlignItems(mui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		mui.Column(c).Gap(1).Grow(1).MinWidth(0).Children(func() {
			title := mui.Text(c, spec.Title).FontSize(a.gitFont(11)).
				Bold().SingleLine().MinWidth(0)
			if spec.Tooltip != "" {
				title.Tooltip(spec.Tooltip)
			}
			if subtitle != "" {
				color := t.TextMuted
				if dirty {
					color = t.Warning
				}
				mui.Text(c, subtitle).FontSize(a.gitFont(9)).TextColor(color).
					SingleLine().MinWidth(0)
			}
		})
		for _, mode := range spec.Modes {
			if mode == viewerEdit && spec.Save == nil {
				continue
			}
			if mode != viewerView && mode != viewerEdit && mode != viewerDiff {
				continue
			}
			active := spec.Mode == mode
			b := quietButton(c, viewerModeLabel(mode)).FontSize(a.gitFont(9))
			b.Label(viewerModeAction(mode)).Tooltip(viewerModeTooltip(mode))
			if active {
				b.Background(t.Accent.Alpha(0.14)).TextColor(t.Accent)
			}
			if b.Clicked() && spec.OnMode != nil && !active {
				spec.OnMode(mode)
			}
		}
		if spec.Save != nil && (spec.Mode == viewerEdit || dirty) {
			save := primaryButton(c, T("Save")).FontSize(a.gitFont(9))
			save.Disabled(spec.Saving)
			save.Tooltip(T("Save the file (Cmd/Ctrl+S)"))
			if save.Clicked() && !spec.Saving {
				a.viewerSave(spec, state)
			}
		}
		if quietButton(c, "✕").Label(T("Close")).FontSize(a.gitFont(9)).
			Tooltip(T("Close the viewer")).Clicked() && spec.OnClose != nil {
			spec.OnClose()
		}
	})
}

// viewerStatus is the one-line saving / error / notice strip under the
// header. It takes no room when there is nothing to say.
func (a *App) viewerStatus(c *mui.Context, spec viewerSpec) {
	t := c.Theme()
	switch {
	case spec.Saving:
		mui.Row(c).Gap(4).AlignItems(mui.Center).Padding(3, 12, 1, 12).Children(func() {
			mui.Spinner(c)
			a.gitCaption(c, T("Saving…"))
		})
	case spec.SaveErr != "":
		mui.Text(c, "⚠ "+singleLine(spec.SaveErr)).FontSize(a.gitFont(9)).
			TextColor(t.Danger).SingleLine().Tooltip(spec.SaveErr).
			Padding(3, 12, 1, 12)
	case spec.Notice != "":
		mui.Text(c, spec.Notice).FontSize(a.gitFont(9)).TextColor(t.Success).
			SingleLine().Padding(3, 12, 1, 12)
	}
}

// viewerBody draws the current mode: a diff, rendered Markdown, an editor or
// a read-only code view.
func (a *App) viewerBody(c *mui.Context, state *viewerCardState, spec viewerSpec, width float32) {
	switch spec.Mode {
	case viewerDiff:
		if spec.Old == spec.New {
			title := spec.EmptyDiffTitle
			if strings.TrimSpace(title) == "" {
				title = T("No changes to show")
			}
			text := spec.EmptyDiffText
			if strings.TrimSpace(text) == "" {
				text = T("Both sides of this file are identical.")
			}
			centered(c, func() {
				emptyState(c, gitCompareIcon, title, text)
			})
			return
		}
		a.viewerDiff(c, state, spec, width)
	case viewerEdit:
		a.viewerEditor(c, state, spec)
	default:
		if spec.Markdown {
			body := spec.Body
			if state.editor != nil && state.editor.Buffer.Modified() {
				// Preview unsaved edits, as the code View does.
				body = state.editor.Buffer.String()
			}
			if strings.TrimSpace(body) == "" {
				title := spec.EmptyViewTitle
				if strings.TrimSpace(title) == "" {
					title = T("File is empty")
				}
				text := spec.EmptyViewText
				if strings.TrimSpace(text) == "" {
					text = T("There is no text to show yet.")
				}
				centered(c, func() {
					emptyState(c, textIcon, title, text)
				})
				return
			}
			mui.Scroll(c).Grow(1).Children(func() {
				mui.Column(c).Padding(14, 18).Children(func() {
					markdown.View(c, body, &a.TaskMD)
				})
			})
			return
		}
		a.viewerEditor(c, state, spec)
	}
}

// viewerDiff keeps one DiffState while the same sides show, and moves between
// split and unified as the card is resized. The scroll position stays.
func (a *App) viewerDiff(c *mui.Context, state *viewerCardState, spec viewerSpec, width float32) {
	layout := editor.DiffUnified
	if viewerSplit(width) {
		layout = editor.DiffSplit
	}
	fontSize := a.gitFont(11)
	if state.diff == nil || state.old != spec.Old || state.new != spec.New || state.font != fontSize {
		oldBuf := editor.NewBuffer(spec.Old)
		newBuf := editor.NewBuffer(spec.New)
		if lang := editor.LanguageFor(spec.Filename); lang != nil {
			oldBuf.SetLanguage(lang)
			newBuf.SetLanguage(lang)
		}
		state.diff = &editor.DiffState{
			Old:        oldBuf,
			New:        newBuf,
			Layout:     layout,
			Wrap:       true,
			Font:       mui.Font{Size: fontSize},
			LineHeight: 1.4,
		}
		state.old, state.new, state.font = spec.Old, spec.New, fontSize
	}
	state.diff.Layout = layout
	editor.DiffView(c, state.diff).Grow(1).FillWidth()
}

// viewerSyncEditor creates the editor buffer for a document and follows the
// caller's text when it changes. A dirty buffer is never overwritten by an
// external update; a saved update whose text matches the buffer just marks it
// saved, keeping the undo history.
func (a *App) viewerSyncEditor(state *viewerCardState, spec viewerSpec) {
	if state.editor == nil {
		buf := editor.NewBuffer(spec.Body)
		if lang := editor.LanguageFor(spec.Filename); lang != nil {
			buf.SetLanguage(lang)
		}
		state.editor = &editor.State{
			Buffer:      buf,
			SoftWrap:    true,
			LineHeight:  1.4,
			NoFolding:   true,
			Placeholder: T("Empty file"),
		}
		state.body = spec.Body
		return
	}
	if state.body == spec.Body {
		return
	}
	buf := state.editor.Buffer
	switch {
	case buf.String() == lineEndingsLF(spec.Body):
		buf.MarkSaved()
	case !buf.Modified():
		buf.Reset(spec.Body)
	}
	state.body = spec.Body
}

// lineEndingsLF matches the text an editor buffer holds: NewBuffer and Reset
// keep CRLF when saving but store and return LF internally.
func lineEndingsLF(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

// viewerEditor draws the shared editor read-only for View and editable for
// Edit. The same State is reused so switching modes keeps the scroll position
// and unsaved text.
func (a *App) viewerEditor(c *mui.Context, state *viewerCardState, spec viewerSpec) {
	a.viewerSyncEditor(state, spec)
	fontSize := a.gitFont(11)
	st := state.editor
	st.ReadOnly = spec.Mode != viewerEdit
	st.Font = mui.Font{Size: fontSize}
	st.LineHeight = 1.4
	st.SoftWrap = true
	st.LineNumbers = editor.LineNumbersOn
	st.Label = spec.Title
	st.OnInput = func(ev mui.InputEvent) bool {
		if spec.Mode != viewerEdit || spec.Save == nil || ev.Kind != mui.InputKeyDown ||
			ev.Key != mui.KeyS || ev.Mods != mui.Cmd {
			return false
		}
		a.viewerSave(spec, state)
		return true
	}
	editor.View(c, st).Grow(1).FillWidth()
}

// viewerSave sends the editor's current text to the caller's save function.
func (a *App) viewerSave(spec viewerSpec, state *viewerCardState) {
	if spec.Save == nil || spec.Saving || state.editor == nil {
		return
	}
	// WriteTo keeps the buffer's original CRLF or LF line endings.
	var text strings.Builder
	if _, err := state.editor.Buffer.WriteTo(&text); err != nil {
		return
	}
	spec.Save(text.String())
}

// viewerModeLabel is the word on a mode button.
func viewerModeLabel(mode viewerMode) string {
	switch mode {
	case viewerDiff:
		return T("Diff")
	case viewerEdit:
		return T("Edit")
	default:
		return T("View")
	}
}

// viewerModeAction is the button's accessible name, which tests click. It
// stays a sentence so it does not depend on the visible label.
func viewerModeAction(mode viewerMode) string {
	switch mode {
	case viewerDiff:
		return T("Show diff")
	case viewerEdit:
		return T("Edit file")
	default:
		return T("Show file content")
	}
}

func viewerModeTooltip(mode viewerMode) string {
	switch mode {
	case viewerDiff:
		return T("Show the changes between the two sides")
	case viewerEdit:
		return T("Edit the text and save it")
	default:
		return T("Read the file without editing it")
	}
}

// viewerCardWidth is the card's width in DIPs for a window windowW wide: the
// width the user dragged to, clamped to the room left of the right panel, or
// a share of that room when the user has not resized it.
func (a *App) viewerCardWidth(windowW float32) float32 {
	maxWidth := viewerMaxWidth(windowW)
	if a.ViewerWidth > 0 {
		return min(max(a.ViewerWidth, viewerMinWidth), maxWidth)
	}
	room := max(windowW-gitPanelWidth, 0)
	return min(max(room*0.62, viewerDefaultMinWidth), min(viewerDefaultMaxWidth, maxWidth))
}

// viewerMaxWidth is the widest card that still leaves the conversation and
// the right panel visible.
func viewerMaxWidth(windowW float32) float32 {
	return max(viewerMinWidth, windowW-gitPanelWidth-rightRailWidth-24)
}

// viewerSplit reports whether the card is wide enough for side-by-side diff.
func viewerSplit(width float32) bool { return width >= viewerSplitWidth }

// isMarkdownPath reports whether the viewer should render the file's text as
// Markdown in View mode.
func isMarkdownPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// refreshAfterFileSave makes the hidden right panels reread a workspace after
// the viewer writes a file. The visible panel updates at once; the others are
// only marked stale so opening them later reads the new state.
func (a *App) refreshAfterFileSave(workspace string) {
	if a.closed.Load() || workspace == "" {
		return
	}
	if sameWorkspace(a.Git.Workspace, workspace) {
		if a.GitPanelOpen && a.RightTab == rightTabGit {
			if !a.Git.Loading && a.Git.Busy == "" {
				a.startGitLoad(a.Git.Workspace, false)
			}
		} else {
			a.Git.Checked = false
		}
	}
	if sameWorkspace(a.Files.Workspace, workspace) {
		if a.GitPanelOpen && a.RightTab == rightTabFiles {
			if a.Files.Loading || a.Files.Busy != "" {
				a.Files.Checked = false
			} else {
				a.refreshFilesOnFocus()
			}
		} else {
			a.Files.Checked = false
		}
	}
}

// normalizeViewerWidth accepts a persisted width, or returns 0 for the
// responsive default. The render clamps it to the current window.
func normalizeViewerWidth(width float32) float32 {
	if width < viewerMinWidth || math.IsNaN(float64(width)) || math.IsInf(float64(width), 0) {
		return 0
	}
	return width
}

// viewerResizeHandle is the card's left edge: dragging it widens or narrows
// the card, and the width is remembered after the pointer lets go.
func (a *App) viewerResizeHandle(c *mui.Context, panel mui.Element, windowW float32) {
	t := c.Theme()
	edge := mui.Box(c).Absolute().Left(0).Top(0).Width(viewerResizeHandleWidth).
		FillHeight().Cursor(mui.CursorResizeEW).Role(mui.RoleNone)
	if edge.Hovered() || edge.Dragging() {
		edge.Background(t.Accent.Alpha(0.10))
	}
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		width := a.viewerCardWidth(windowW) - dx
		a.ViewerWidth = min(max(width, viewerMinWidth), viewerMaxWidth(windowW))
	}
	if _, _, ok := edge.DragEnded(); ok {
		a.persistSettings()
	}
}
