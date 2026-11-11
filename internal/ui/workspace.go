package ui

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// addWorkspace asks for a folder and adds it to the sidebar. It does not
// create a conversation: the workspace's + button does that.
func (a *App) addWorkspace() {
	win := a.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            win,
			Title:             "Add Workspace",
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil {
			a.Post(func() { a.WorkspaceError = err.Error() })
			return
		}
		if len(paths) == 0 {
			return
		}
		a.Post(func() {
			if a.closed.Load() {
				return
			}
			path := filepath.Clean(strings.TrimSpace(paths[0]))
			a.showWorkspace(path)
			a.Workspace = path
			a.WorkspaceError = ""
			a.WorkspaceCollapsed[path] = false
			a.persistSettings()
		})
	}()
}

func (a *App) chooseWorkspace() {
	chat := a.activeChat()
	win := a.window()
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:            win,
			Title:             "Choose Workspace",
			Directory:         true,
			CreateDirectories: true,
		})
		if err != nil {
			a.Post(func() {
				if a.closed.Load() {
					return
				}
				if chat != nil && a.chatIndex(chat) >= 0 {
					chat.WorkspaceError = err.Error()
				} else {
					a.WorkspaceError = err.Error()
				}
			})
			return
		}
		if len(paths) == 0 {
			return
		}
		a.Post(func() {
			if a.closed.Load() {
				return
			}
			if chat != nil && a.chatIndex(chat) >= 0 {
				chat.Workspace = paths[0]
				chat.WorkspaceError = ""
				a.showWorkspace(paths[0])
				a.WorkspaceCollapsed[paths[0]] = false
				a.persistSettings()
				return
			}
			a.Workspace = paths[0]
			a.WorkspaceError = ""
			a.showWorkspace(paths[0])
			a.persistSettings()
		})
	}()
}

// workspaceRemoved reports whether the user soft-deleted a sidebar group. The
// lookup tries the cleaned path and its symlink-resolved form, so a record
// survives a folder that is temporarily unavailable.
func (a *App) workspaceRemoved(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if a.RemovedWorkspaces[canonicalWorkspace(path)] {
		return true
	}
	return a.RemovedWorkspaces[filepath.Clean(path)]
}

func (a *App) markWorkspaceRemoved(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	if a.RemovedWorkspaces == nil {
		a.RemovedWorkspaces = make(map[string]bool)
	}
	a.RemovedWorkspaces[filepath.Clean(path)] = true
	if key := canonicalWorkspace(path); key != "" {
		a.RemovedWorkspaces[key] = true
	}
}

// removeWorkspace soft-deletes a sidebar group. Open tabs, saved sessions and
// the folder itself are untouched: only the group is hidden and kept in
// settings for a later restore.
func (a *App) removeWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	// Keep the path in the sidebar list so the record survives a restart and
	// "Show removed" can still draw it.
	a.ensureWorkspace(path)
	a.markWorkspaceRemoved(path)
	a.WorkspaceMenuOpen = ""
	if sameWorkspace(a.Workspace, path) {
		if visible := a.firstVisibleWorkspace(); visible != "" {
			a.Workspace = visible
		}
	}
	a.persistSettings()
}

// restoreWorkspace un-deletes a soft-deleted group.
func (a *App) restoreWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	delete(a.RemovedWorkspaces, filepath.Clean(path))
	delete(a.RemovedWorkspaces, canonicalWorkspace(path))
	a.ensureWorkspace(path)
	a.WorkspaceMenuOpen = ""
	a.persistSettings()
}

// showWorkspace adds a folder the user picked explicitly and clears a
// previous soft delete, so picking a removed folder brings its group back.
func (a *App) showWorkspace(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}
	delete(a.RemovedWorkspaces, filepath.Clean(path))
	delete(a.RemovedWorkspaces, canonicalWorkspace(path))
	a.ensureWorkspace(path)
}

// shownWorkspaces is the sidebar list: removed groups are hidden unless the
// user is reviewing them in order to restore one.
func (a *App) shownWorkspaces() []string {
	out := make([]string, 0, len(a.Workspaces))
	for _, workspace := range a.Workspaces {
		if a.workspaceRemoved(workspace) && !a.ShowRemovedWorkspaces {
			continue
		}
		out = append(out, workspace)
	}
	return out
}

func (a *App) firstVisibleWorkspace() string {
	for _, workspace := range a.Workspaces {
		if !a.workspaceRemoved(workspace) {
			return workspace
		}
	}
	return ""
}

func (a *App) removedWorkspaceCount() int {
	count := 0
	for _, workspace := range a.Workspaces {
		if a.workspaceRemoved(workspace) {
			count++
		}
	}
	return count
}

// removedWorkspaceList is the persisted soft-delete record, in sidebar order.
func (a *App) removedWorkspaceList() []string {
	var out []string
	for _, workspace := range a.Workspaces {
		if a.workspaceRemoved(workspace) {
			out = append(out, workspace)
		}
	}
	return out
}
