package ui

import (
	_ "embed"

	mui "github.com/egoist/mygo/ui"
)

// Interface icons, drawn in the text color so they follow light/dark themes.
// Only the icons this view shows are embedded: the component layer is local,
// not an icon library. Sources and licenses: icons/LICENSE.
//
//go:embed icons/message-square.svg
var messageSquareIconSVG []byte

//go:embed icons/messages-square.svg
var messagesSquareIconSVG []byte

//go:embed icons/unplug.svg
var unplugIconSVG []byte

//go:embed icons/sparkles.svg
var sparklesIconSVG []byte

//go:embed icons/git-compare.svg
var gitCompareIconSVG []byte

//go:embed icons/git-branch.svg
var gitBranchIconSVG []byte

//go:embed icons/list-checks.svg
var listChecksIconSVG []byte

//go:embed icons/folder-tree.svg
var folderTreeIconSVG []byte

//go:embed icons/folder.svg
var folderIconSVG []byte

//go:embed icons/folder-open.svg
var folderOpenIconSVG []byte

//go:embed icons/file.svg
var fileIconSVG []byte

//go:embed icons/text.svg
var textIconSVG []byte

//go:embed icons/braces.svg
var bracesIconSVG []byte

//go:embed icons/image.svg
var imageIconSVG []byte

//go:embed icons/terminal.svg
var terminalIconSVG []byte

//go:embed icons/file-plus.svg
var filePlusIconSVG []byte

//go:embed icons/folder-plus.svg
var folderPlusIconSVG []byte

//go:embed icons/refresh-cw.svg
var refreshIconSVG []byte

//go:embed icons/circle-x.svg
var circleXIconSVG []byte

//go:embed icons/code.svg
var codeIconSVG []byte

//go:embed icons/copy.svg
var copyIconSVG []byte

//go:embed icons/check.svg
var checkIconSVG []byte

//go:embed icons/settings.svg
var settingsIconSVG []byte

var (
	messageSquareIcon  = mui.MustParseSVG(messageSquareIconSVG)
	messagesSquareIcon = mui.MustParseSVG(messagesSquareIconSVG)
	unplugIcon         = mui.MustParseSVG(unplugIconSVG)
	sparklesIcon       = mui.MustParseSVG(sparklesIconSVG)
	gitCompareIcon     = mui.MustParseSVG(gitCompareIconSVG)
	gitBranchIcon      = mui.MustParseSVG(gitBranchIconSVG)
	listChecksIcon     = mui.MustParseSVG(listChecksIconSVG)
	folderTreeIcon     = mui.MustParseSVG(folderTreeIconSVG)
	folderIcon         = mui.MustParseSVG(folderIconSVG)
	folderOpenIcon     = mui.MustParseSVG(folderOpenIconSVG)
	fileIcon           = mui.MustParseSVG(fileIconSVG)
	textIcon           = mui.MustParseSVG(textIconSVG)
	bracesIcon         = mui.MustParseSVG(bracesIconSVG)
	imageIcon          = mui.MustParseSVG(imageIconSVG)
	terminalIcon       = mui.MustParseSVG(terminalIconSVG)
	filePlusIcon       = mui.MustParseSVG(filePlusIconSVG)
	folderPlusIcon     = mui.MustParseSVG(folderPlusIconSVG)
	refreshIcon        = mui.MustParseSVG(refreshIconSVG)
	circleXIcon        = mui.MustParseSVG(circleXIconSVG)
	codeIcon           = mui.MustParseSVG(codeIconSVG)
	copyIcon           = mui.MustParseSVG(copyIconSVG)
	checkIcon          = mui.MustParseSVG(checkIconSVG)
	settingsIcon       = mui.MustParseSVG(settingsIconSVG)
)
