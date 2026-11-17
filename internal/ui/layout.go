package ui

import (
	"math"

	mui "github.com/egoist/mygo/ui"
)

// The window's columns share their edges with the conversation: the
// sidebar's right edge and the right panel's left edge both drag, and a
// double click puts the column's default width back. A dragged width is
// remembered in settings.json; the render clamps it again to the room the
// current window has, so a narrow window keeps a usable conversation
// without discarding the width the user chose on a wide one.
const (
	// The built-in width of each column and the bounds a drag may reach.
	// The window share keeps a column from squeezing the conversation out
	// on a small screen, the absolute caps keep the conversation the
	// widest column on a large one.
	sidebarDefaultWidth    = 292
	sidebarMinWidth        = 200
	sidebarMaxWidthCap     = 460
	rightPanelDefaultWidth = 300
	rightPanelMinWidth     = 220
	rightPanelMaxWidthCap  = 560

	// resizeGrip is how wide the drag area over a column's edge is. It is
	// wider than the one DIP border it follows so the pointer can find it
	// without aiming at a line.
	resizeGrip = 6
)

// sidebarPaneWidth is the sidebar's width in DIPs for a window windowW wide:
// the width the user dragged to, clamped to the room this window has.
func (a *App) sidebarPaneWidth(windowW float32) float32 {
	width := a.SidebarWidth
	if width <= 0 {
		width = sidebarDefaultWidth
	}
	return min(max(width, sidebarMinWidth), sidebarMaxWidth(windowW))
}

// sidebarMaxWidth is the widest the sidebar may be in a window windowW wide.
func sidebarMaxWidth(windowW float32) float32 {
	width := float32(sidebarMaxWidthCap)
	if windowW > 0 {
		width = min(width, windowW*0.35)
	}
	return max(width, sidebarMinWidth)
}

// rightPaneWidth is the right column's width in DIPs for a window windowW
// wide. The three tabs share this one width.
func (a *App) rightPaneWidth(windowW float32) float32 {
	width := a.RightPanelWidth
	if width <= 0 {
		width = rightPanelDefaultWidth
	}
	return min(max(width, rightPanelMinWidth), rightPaneMaxWidth(windowW))
}

// rightPaneMaxWidth is the widest the right column may be in a window
// windowW wide. The share of the window leaves the conversation and the
// sidebar room at any size.
func rightPaneMaxWidth(windowW float32) float32 {
	width := float32(rightPanelMaxWidthCap)
	if windowW > 0 {
		width = min(width, windowW*0.45)
	}
	return max(width, rightPanelMinWidth)
}

// normalizeSidebarWidth accepts a persisted width, or returns 0 for the
// built-in width. The render clamps it to the current window.
func normalizeSidebarWidth(width float32) float32 {
	if width < sidebarMinWidth || math.IsNaN(float64(width)) || math.IsInf(float64(width), 0) {
		return 0
	}
	return min(width, sidebarMaxWidthCap)
}

// normalizeRightPanelWidth accepts a persisted width, or returns 0 for the
// built-in width. The render clamps it to the current window.
func normalizeRightPanelWidth(width float32) float32 {
	if width < rightPanelMinWidth || math.IsNaN(float64(width)) || math.IsInf(float64(width), 0) {
		return 0
	}
	return min(width, rightPanelMaxWidthCap)
}

// sidebarResizeHandle is the sidebar's right edge, drawn in the conversation
// on the far side of the border: the sidebar's own last 12 DIPs belong to its
// scroll bar's track, and a grip there would take the wheel and the thumb
// from it. The width is written to settings when the pointer lets go or the
// double click lands, not on every frame of a drag.
func (a *App) sidebarResizeHandle(c *mui.Context) {
	t := c.Theme()
	windowW, _ := c.Size()
	edge := mui.Box(c).Absolute().Left(0).Top(0).Width(resizeGrip).
		FillHeight().Cursor(mui.CursorResizeEW).Role(mui.RoleNone)
	if edge.Hovered() || edge.Dragging() {
		edge.Background(t.Accent.Alpha(0.12))
	}
	if edge.DoubleClicked() {
		a.SidebarWidth = sidebarDefaultWidth
		a.persistSettings()
	}
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		width := a.sidebarPaneWidth(windowW) + dx
		a.SidebarWidth = min(max(width, sidebarMinWidth), sidebarMaxWidth(windowW))
	}
	if _, _, ok := edge.DragEnded(); ok {
		a.persistSettings()
	}
}

// rightPanelResizeHandle is the right column's left edge: dragging it left
// widens the column, since the conversation is on that side.
func (a *App) rightPanelResizeHandle(c *mui.Context) {
	t := c.Theme()
	windowW, _ := c.Size()
	edge := mui.Box(c).Absolute().Left(0).Top(0).Width(resizeGrip).
		FillHeight().Cursor(mui.CursorResizeEW).Role(mui.RoleNone)
	if edge.Hovered() || edge.Dragging() {
		edge.Background(t.Accent.Alpha(0.12))
	}
	if edge.DoubleClicked() {
		a.RightPanelWidth = rightPanelDefaultWidth
		a.persistSettings()
	}
	if dx, _, ok := edge.Dragged(); ok && dx != 0 {
		width := a.rightPaneWidth(windowW) - dx
		a.RightPanelWidth = min(max(width, rightPanelMinWidth), rightPaneMaxWidth(windowW))
	}
	if _, _, ok := edge.DragEnded(); ok {
		a.persistSettings()
	}
}
