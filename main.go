// ACP GUI is a native desktop client for the Agent Client Protocol.
//
// The window is drawn by MyGo's native UI; agents are started from absolute
// paths over newline-delimited JSON-RPC on stdio (see docs/design).
package main

import (
	"log"
	"runtime"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans" // MyGo's own strings in Chinese
	"github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/i18n"
	appui "github.com/reed/acp-gui/internal/ui"
)

func main() {
	app := mygo.App
	// Set the name before anything reads the data directory.
	app.SetName("ACP GUI")
	// Declare the languages the app is written in, so MyGo's own controls,
	// menus and dialogs pick between them with the app's texts (internal/i18n
	// says those). SetLocale then shows the one the settings chose.
	app.SetLanguages(i18n.Languages...)

	// applyLocale gives MyGo's own strings the language the app shows: the
	// setting, or the user's preference when it follows the system.
	applyLocale := func(setting string) {
		if setting == i18n.System {
			app.SetLocale("")
			return
		}
		app.SetLocale(setting)
	}

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
		// Set the native chrome before the first frame so a forced light or
		// dark appearance does not flash the desktop's opposite.
		gui.ApplyAppearance()
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
			Version:            app.Version(),
			Resources:          resources,
			DataDir:            dataDir,
			PreferredLanguages: app.PreferredLanguages(),
			OnLanguage:         applyLocale,
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

	// Clicking a permission or finished-turn notification opens the window;
	// once the app is active, its notifications have served their purpose.
	app.OnNotificationClick(func(string) {
		if gui != nil && !quitting {
			openWindow()
		}
	})
	app.OnDidBecomeActive(func() { mygo.ClearNotifications() })

	// A System language choice follows the user's languages, also when the
	// desktop changes them while the app runs.
	app.OnLocaleChanged(func(string) {
		if gui != nil {
			gui.PreferredLanguagesChanged(app.PreferredLanguages())
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
