// Shared looks for views built by hand. MyGo has no style sheets: a widget is
// a function that styles its elements once and is called wherever it shows, so
// the helpers here keep the repeated paddings, colors and sizes in one place.
// Layout and content stay with the caller; these only encode the project's
// small visual vocabulary.

package ui

import mui "github.com/egoist/mygo/ui"

// mutedText is secondary text in the theme's muted color. It returns the
// element so callers add their own size, Selectable, Grow or Tooltip.
func mutedText(c *mui.Context, text string, size float32) mui.Element {
	return mui.Text(c, text).FontSize(size).TextColor(c.Theme().TextMuted)
}

// caption is a one-line 9px muted label: counts, statuses and micro hints.
func caption(c *mui.Context, text string) mui.Element {
	return mutedText(c, text, 9).SingleLine()
}

// sectionLabel is the bold 9px heading of a group of rows or options.
// Callers add padding, Grow or a count beside it.
func sectionLabel(c *mui.Context, text string) mui.Element {
	return mui.Text(c, text).FontSize(9).Bold().TextColor(c.Theme().TextMuted)
}

// surfaceCard applies the panel look shared by chat cards and attachment
// rows: rounded corners on the theme's surface, no border. Callers keep their
// own layout (gap, padding, direction) and add children afterwards.
func surfaceCard(c *mui.Context, e mui.Element) mui.Element {
	return e.Radius(8).Background(c.Theme().Surface)
}

// sunkenSurface is the icon rail's background: the panel surface one step
// deeper, so the rail reads as a groove beside the right column instead of
// continuing it. It is derived rather than stored in every palette: mixing
// toward black deepens all theme families in both appearances, where a
// palette's own background can sit on either side of its surface. Dark
// surfaces have a smaller range below them, so they take a larger share to
// stay visible next to the panel.
func sunkenSurface(t *mui.Theme) mui.Color {
	amount := float32(0.07)
	if t.Dark {
		amount = 0.18
	}
	return t.Surface.Mix(mui.Color{A: 255}, amount)
}

// buttonBase is the shape every button shares, taken from the sidebar's ⋯
// action: a compact row with 8 DIP corners and no border. It sets the label
// of the control for assistive technology and tests; the caller adds the text
// or icon and the fill.
func buttonBase(c *mui.Context, label string) mui.Element {
	b := mui.ButtonBase(c).Padding(4, 6).Radius(8)
	if label != "" {
		b.Label(label)
	}
	return b
}

// quietButton is the project's default button: the ⋯ action's flat pill on
// the window background, with a hover and pressed fill and no border.
func quietButton(c *mui.Context, label string) mui.Element {
	t := c.Theme()
	b := buttonBase(c, label)
	fill := t.Background
	switch {
	case b.Pressed():
		fill = t.SurfacePressed
	case b.Hovered():
		fill = t.SurfaceHover
	}
	b.Background(fill).TextColor(t.Text)
	b.Children(func() { mui.Text(c, label) })
	return b
}

// primaryButton is the main action's button: the same shape filled with the
// accent, gray in a window not drawn as active as MyGo's own primary button.
func primaryButton(c *mui.Context, label string) mui.Element {
	t := c.Theme()
	fill, fg := t.Accent, t.AccentText
	hover, pressed := t.AccentHover, t.AccentPressed
	if !c.WindowActive() && t.InactiveAccent != t.Accent {
		fill, fg = t.Surface, t.Text
		hover, pressed = t.SurfaceHover, t.SurfacePressed
	}
	b := buttonBase(c, label)
	switch {
	case b.Pressed():
		fill = pressed
	case b.Hovered():
		fill = hover
	}
	b.Background(fill).TextColor(fg)
	b.Children(func() { mui.Text(c, label) })
	return b
}

// dangerButton is a destructive action: the quiet shape with the theme's
// danger color for its text, flat like every other button.
func dangerButton(c *mui.Context, label string) mui.Element {
	return quietButton(c, label).TextColor(c.Theme().Danger)
}

// chipBase is the clickable pill used in the composer for the model, mode,
// usage and branch chips: the buttons' shape, a little tighter.
func chipBase(c *mui.Context) mui.Element {
	t := c.Theme()
	chip := buttonBase(c, "").Padding(3, 8).Gap(3)
	fill := t.Background
	switch {
	case chip.Pressed():
		fill = t.SurfacePressed
	case chip.Hovered():
		fill = t.SurfaceHover
	}
	return chip.Background(fill)
}

// composerControlHeight is the base height of every control in the
// composer's action row. The row mixes an icon, chips and text buttons at
// different font sizes; one scaled height keeps their edges lined up.
const composerControlHeight = 22

// menuTrigger is the "⋯" row action that opens a popover. The label is for
// assistive technology and tests, the background keeps the row's own fill,
// and size lets the row's text scale move the dots with it.
func menuTrigger(c *mui.Context, label string, background mui.Color, size float32) mui.Element {
	menu := buttonBase(c, label).Background(background)
	menu.Children(func() {
		mutedText(c, "⋯", size)
	})
	return menu
}
