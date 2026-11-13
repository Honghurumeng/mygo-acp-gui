package ui

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	mui "github.com/egoist/mygo/ui"
)

const (
	// maxMentionMatches bounds the visible @ list; the index itself keeps
	// more so a narrower query can still find files deep in the workspace.
	maxMentionMatches = 50
	// maxIndexedFiles bounds a workspace walk. A monorepo can hold more
	// files than the composer should ever keep in memory.
	maxIndexedFiles    = 20000
	mentionPanelWidth  = 560
	mentionPanelHeight = 260
)

// FileRef is a workspace file named in a prompt as @workspace/path. It is
// metadata only: the agent reads the file itself, and the composer shows no
// attachment chip for it.
type FileRef struct {
	Name string // workspace-relative, slash-separated
}

// mentionContext finds an @ mention ending at caret. A mention begins at an
// @ that starts a whitespace-delimited word, so mail addresses and
// punctuation inside ordinary text do not open the file list.
func mentionContext(text string, caret int) (start int, query string, ok bool) {
	runes := []rune(text)
	if caret < 0 {
		caret = 0
	}
	if caret > len(runes) {
		caret = len(runes)
	}
	i := caret - 1
	for i >= 0 && !unicode.IsSpace(runes[i]) {
		i--
	}
	start = i + 1
	if start >= caret || runes[start] != '@' {
		return 0, "", false
	}
	return start, string(runes[start+1 : caret]), true
}

// matchMentionFiles scores workspace-relative paths for an @ query. Base
// name matches rank before path matches, and a fuzzy subsequence is the last
// resort, so "@com" finds composer.go without listing every path containing
// those letters first.
func matchMentionFiles(files []string, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	type ranked struct {
		name  string
		score int
	}
	out := make([]ranked, 0, min(len(files), maxMentionMatches))
	for _, file := range files {
		score, ok := mentionScore(file, query)
		if !ok {
			continue
		}
		out = append(out, ranked{name: file, score: score})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score < out[j].score
		}
		if len(out[i].name) != len(out[j].name) {
			return len(out[i].name) < len(out[j].name)
		}
		return out[i].name < out[j].name
	})
	if len(out) > maxMentionMatches {
		out = out[:maxMentionMatches]
	}
	result := make([]string, len(out))
	for i, entry := range out {
		result[i] = entry.name
	}
	return result
}

func mentionScore(file, query string) (int, bool) {
	lower := strings.ToLower(file)
	base := strings.ToLower(path.Base(file))
	if query == "" {
		return 10 + strings.Count(file, "/"), true
	}
	switch {
	case base == query:
		return 0, true
	case strings.HasPrefix(base, query):
		return 1, true
	case strings.HasPrefix(lower, query):
		return 2, true
	case strings.Contains(base, query):
		return 3, true
	case strings.Contains(lower, query):
		return 4, true
	case isSubsequence(query, base):
		return 5, true
	case isSubsequence(query, lower):
		return 6, true
	default:
		return 0, false
	}
}

func isSubsequence(query, text string) bool {
	if query == "" {
		return true
	}
	remaining := []rune(query)
	for _, r := range text {
		if r == remaining[0] {
			remaining = remaining[1:]
			if len(remaining) == 0 {
				return true
			}
		}
	}
	return false
}

// scanWorkspaceFiles lists mentionable files under root. It skips symlinks
// (a link could point outside the workspace), dependency/build directories
// and names that a whitespace-delimited @ token cannot represent.
func scanWorkspaceFiles(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if current == root {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if skipMentionDir(name) {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, current)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.ContainsAny(rel, " \t\r\n@") {
			return nil
		}
		files = append(files, rel)
		if len(files) >= maxIndexedFiles {
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func skipMentionDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".vscode", ".venv", "venv",
		"node_modules", "bower_components", "vendor", "target", "build",
		"dist", "coverage", "__pycache__", ".next", ".nuxt", ".cache":
		return true
	}
	// Hidden directories are mostly tool state. Keep .github so workflows
	// and issue templates remain mentionable.
	return strings.HasPrefix(name, ".") && name != ".github"
}

// ensureMentionIndex starts one background scan per workspace. A mention
// typed before it finishes shows an "Indexing…" row and fills in when the
// scan posts back to the UI thread.
func (a *App) ensureMentionIndex(chat *Chat) {
	if chat == nil || chat.Workspace == "" {
		return
	}
	if chat.fileIndexRoot == chat.Workspace && (chat.fileIndexLoaded || chat.fileIndexLoading) {
		return
	}
	root := chat.Workspace
	chat.fileIndexRoot = root
	chat.fileIndex = nil
	chat.fileIndexErr = ""
	chat.fileIndexLoading = true
	chat.fileIndexLoaded = false
	index := a.fileIndexer
	if index == nil {
		index = scanWorkspaceFiles
	}
	a.cleanup.Add(1)
	go func() {
		defer a.cleanup.Done()
		files, err := index(root)
		a.Post(func() {
			if chat.Closed || chat.Workspace != root {
				return
			}
			chat.fileIndexLoading = false
			chat.fileIndexLoaded = true
			if err != nil {
				chat.fileIndexErr = err.Error()
				return
			}
			chat.fileIndexErr = ""
			chat.fileIndex = files
		})
	}()
}

// syncMention recomputes the @ list from the current text and caret. The
// textarea calls it after each edit, before the next frame is built.
func (a *App) syncMention(chat *Chat, caret int) {
	if chat == nil {
		return
	}
	start, query, ok := mentionContext(chat.Input, caret)
	if !ok || !chat.canType() {
		chat.MentionOpen = false
		chat.MentionQuery = ""
		chat.MentionIndex = 0
		return
	}
	opening := !chat.MentionOpen
	chat.MentionOpen = true
	chat.MentionStart = start
	chat.MentionQuery = query
	matches := matchMentionFiles(chat.fileIndex, query)
	if opening || chat.MentionIndex >= len(matches) {
		chat.MentionIndex = 0
	}
	if chat.MentionIndex < 0 {
		chat.MentionIndex = 0
	}
	a.ensureMentionIndex(chat)
}

// mentionMatches is the current palette content. It returns nil while the
// workspace scan is still running; the view draws a loading row instead.
func (chat *Chat) mentionMatches() []string {
	if chat == nil || !chat.MentionOpen {
		return nil
	}
	return matchMentionFiles(chat.fileIndex, chat.MentionQuery)
}

// acceptMention inserts @relative/path at the mention location and records
// the file for the next prompt. It returns the new caret offset.
func (a *App) acceptMention(chat *Chat, relative string) int {
	if chat == nil || relative == "" {
		return 0
	}
	runes := []rune(chat.Input)
	start := chat.MentionStart
	if start < 0 || start > len(runes) {
		return 0
	}
	end := start + 1 + len([]rune(chat.MentionQuery))
	if end > len(runes) {
		end = len(runes)
	}
	insert := "@" + relative
	// Keep the token separate from whatever follows; an end-of-input
	// mention also gets a trailing space so typing continues naturally.
	if end >= len(runes) || !unicode.IsSpace(runes[end]) {
		insert += " "
	}
	next := string(runes[:start]) + insert + string(runes[end:])
	caret := start + len([]rune(insert))
	chat.Input = next
	chat.MentionOpen = false
	chat.MentionQuery = ""
	chat.MentionIndex = 0
	chat.focusComposer = true
	// Put the caret after the inserted token, not where the query was, so
	// typing continues after the path instead of inside it.
	chat.composerCaret = caret
	a.addFileRef(chat, relative)
	return caret
}

// fileRefsInText keeps the refs whose @path token still appears in the
// prompt text. The text is the only thing sent, so a chip whose token the
// user deleted must not suggest otherwise.
func fileRefsInText(refs []FileRef, text string) []FileRef {
	if len(refs) == 0 {
		return nil
	}
	tokens := make(map[string]bool, len(refs))
	for _, field := range strings.Fields(text) {
		tokens[field] = true
	}
	kept := make([]FileRef, 0, len(refs))
	for _, ref := range refs {
		if ref.Name != "" && tokens["@"+ref.Name] {
			kept = append(kept, ref)
		}
	}
	return kept
}

func (a *App) addFileRef(chat *Chat, relative string) {
	if chat == nil || relative == "" {
		return
	}
	for _, ref := range chat.FileRefs {
		if ref.Name == relative {
			return
		}
	}
	chat.FileRefs = append(chat.FileRefs, FileRef{Name: relative})
}

// mentionPathParts splits a workspace-relative path into the file name a
// row leads with and the directory that follows it muted, so the same base
// name in different packages stays distinguishable at a glance. The
// directory is empty for a file at the workspace root.
func mentionPathParts(relative string) (name, dir string) {
	name = path.Base(relative)
	dir = path.Dir(relative)
	if dir == "." || dir == "/" {
		dir = ""
	}
	return name, dir
}

// mentionPalette draws the @ file list above the composer. Its rows are
// keyboard-selectable through the textarea's OnTextKey handler.
func (a *App) mentionPalette(c *mui.Context, chat *Chat) {
	if !chat.MentionOpen || !chat.canType() {
		return
	}
	matches := chat.mentionMatches()
	t := c.Theme()
	mui.Row(c).Padding(0, 16, 6, 16).Children(func() {
		mui.Column(c).Width(mentionPanelWidth).MaxHeight(mentionPanelHeight).Gap(2).Padding(4).
			Radius(10).Background(t.Background).Border(1, t.Border).
			Children(func() {
				if chat.fileIndexLoading && len(chat.fileIndex) == 0 {
					caption(c, "Indexing workspace files…").Padding(5, 8)
					return
				}
				if chat.fileIndexErr != "" {
					mutedText(c, "Could not index workspace: "+chat.fileIndexErr, 10).Padding(5, 8)
					return
				}
				if len(matches) == 0 {
					label := "No matching files"
					if chat.MentionQuery != "" {
						label = "No files match @" + chat.MentionQuery
					}
					caption(c, label).Padding(5, 8)
					return
				}
				mui.Scroll(c).MaxHeight(mentionPanelHeight - 8).Gap(1).Children(func() {
					for i, relative := range matches {
						i, relative := i, relative
						selected := i == chat.MentionIndex
						item := mui.ButtonBase(c).FillWidth().Gap(8).Padding(4, 8).Radius(6).Justify(mui.Start)
						if selected {
							item.Background(t.SurfaceHover)
						}
						item.Children(func() {
							name, dir := mentionPathParts(relative)
							// The name leads, the directory trails it muted the way
							// the Git panel lists changed files.
							mui.Text(c, truncate(name, 36)).FontSize(10).SingleLine().Shrink(0)
							if dir != "" {
								mutedText(c, truncate(dir, 56), 8).SingleLine()
							}
							mui.Spacer(c)
							if info, err := os.Stat(filepath.Join(chat.Workspace, filepath.FromSlash(relative))); err == nil {
								caption(c, formatBytes(info.Size())).Shrink(0)
							}
						})
						item.Label("Mention " + relative)
						if item.Clicked() {
							a.acceptMention(chat, relative)
						}
					}
				})
			})
	})
}

// formatBytes prints the compact size used by file mention rows.
func formatBytes(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit && exp < len("KMGT")-1; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGT"[exp])
}
