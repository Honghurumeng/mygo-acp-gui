package ui

import "github.com/egoist/mygo"

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
				return
			}
			a.Workspace = paths[0]
			a.WorkspaceError = ""
		})
	}()
}
