package ui

import "github.com/egoist/mygo"

func (a *App) chooseWorkspace() {
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
				if !a.closed.Load() {
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
			a.Workspace = paths[0]
			a.WorkspaceError = ""
		})
	}()
}
