// ACP GUI is a native desktop client for the Agent Client Protocol.
//
// The window is drawn by MyGo's native UI; agents are discovered and driven
// over newline-delimited JSON-RPC on stdio (see docs/design).
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// appState is the root state the window shows. Agent and workspace state will
// be added here as the ACP client lands.
type appState struct {
	version string
}

// view builds the window's interface from the state.
func (s *appState) view(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(24).Gap(14).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "ACP GUI").FontSize(28).Bold()
			ui.Text(c, "v"+s.version).FontSize(12).TextColor(t.TextMuted)
		})
		ui.Text(c, "Native client for the Agent Client Protocol").FontSize(15).TextColor(t.TextMuted)

		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Text(c, "No agent connected yet").FontSize(18)
			ui.Text(c, "Agent discovery and the chat view land in the next step.").
				FontSize(13).TextColor(t.TextMuted)
		})
	})
}

func main() {
	app := mygo.App
	// Set the name before anything reads the data directory.
	app.SetName("ACP GUI")

	state := &appState{version: app.Version()}
	if state.version == "" {
		state.version = "dev"
	}

	app.WhenReady(func() {
		app.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		mygo.NewWindow(mygo.WindowOptions{
			Title:     "ACP GUI",
			Width:     980,
			Height:    680,
			MinWidth:  720,
			MinHeight: 480,
			Content:   ui.View(state.view),
		})
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
