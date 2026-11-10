// ACP GUI is a native desktop client for the Agent Client Protocol.
//
// The window is drawn by MyGo's native UI; agents are started from absolute
// paths over newline-delimited JSON-RPC on stdio (see docs/design).
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	appui "github.com/reed/acp-gui/internal/ui"
)

func main() {
	app := mygo.App
	// Set the name before anything reads the data directory.
	app.SetName("ACP GUI")

	var gui *appui.App

	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))

		resources, err := app.Path(mygo.PathResources)
		if err != nil {
			log.Printf("resources: %v", err)
		}
		gui = appui.New(appui.Options{
			Version:   app.Version(),
			Resources: resources,
		})

		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "ACP GUI",
			Width:     1060,
			Height:    760,
			MinWidth:  760,
			MinHeight: 520,
			Content:   ui.View(gui.View),
		})
		gui.Attach(win)

		// Closing the window and quitting the app both clean up the agent
		// process group. Session.Close is idempotent, so both may run.
		win.OnClose(func(*mygo.CloseEvent) { gui.Shutdown() })
		app.OnWillQuit(func(*mygo.QuitEvent) { gui.Shutdown() })
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
