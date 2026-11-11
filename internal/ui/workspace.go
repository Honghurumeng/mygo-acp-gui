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
			a.ensureWorkspace(path)
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
				a.ensureWorkspace(paths[0])
				a.WorkspaceCollapsed[paths[0]] = false
				a.persistSettings()
				return
			}
			a.Workspace = paths[0]
			a.WorkspaceError = ""
			a.ensureWorkspace(paths[0])
			a.persistSettings()
		})
	}()
}
