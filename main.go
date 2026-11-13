// ACP GUI is a native desktop client for the Agent Client Protocol.
//
// The window is drawn by MyGo's native UI; agents are started from absolute
// paths over newline-delimited JSON-RPC on stdio (see docs/design).
package main

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	appui "github.com/reed/acp-gui/internal/ui"
)

func main() {
	app := mygo.App
	// Set the name before anything reads the data directory.
	app.SetName("ACP GUI")

	var gui *appui.App
	var win *mygo.Window
	// quitting is set on the main thread with the quit events, so a Dock
	// click while the app quits cannot open a window again.
	var quitting bool

	// openWindow shows the window, or builds a new one after the user closed
	// it: the app keeps running, and the agents keep working, without one.
	openWindow := func() {
		if win != nil && !win.IsDestroyed() {
			win.Show()
			win.Focus()
			return
		}
		window := mygo.NewWindow(mygo.WindowOptions{
			Title:     "ACP GUI",
			Width:     1240,
			Height:    820,
			MinWidth:  900,
			MinHeight: 560,
			Content:   ui.View(gui.View),
			// Reopening the window, and the next launch, keep the size,
			// position and maximized state the user left it in.
			StateKey: "main",
		})
		win = window
		gui.Attach(window)
		// Closing the window closes only the window: sessions and their
		// agent processes keep running in the background, and OnActivate
		// opens a new window on them.
		window.OnClosed(func() { gui.Detach(window) })
	}

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
		dataDir, err := app.Path(mygo.PathUserData)
		if err != nil {
			log.Printf("data: %v", err)
		}
		gui = appui.New(appui.Options{
			Version:   app.Version(),
			Resources: resources,
			DataDir:   dataDir,
		})
		openWindow()
	})

	// Without a window the app stays in the Dock on macOS, and clicking the
	// icon opens a new one. Elsewhere the app would keep running with no way
	// to show itself, so closing the last window quits.
	app.OnWindowAllClosed(func() {
		if runtime.GOOS != "darwin" {
			app.Quit()
		}
	})
	app.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows && gui != nil && !quitting {
			openWindow()
		}
	})

	// Quitting stops the agents: every session's process group is closed.
	app.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	app.OnWillQuit(func(*mygo.QuitEvent) {
		if gui != nil {
			gui.Shutdown()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
