// The commit history of the Git panel. It replaces CrychicUI's CommitGraph,
// whose fixed 36 DIP rows left too much air and could not expand: here a row
// is 22 DIP, clicking it shows the files that commit changed, and the lanes
// are drawn behind the rows for the same geometry MyGo lays out.

package ui

import (
	"path/filepath"

	mui "github.com/egoist/mygo/ui"
	"github.com/reed/acp-gui/internal/git"
)

const (
	gitGraphRowHeight  = 22
	gitGraphFileHeight = 16
	// gitGraphLaneGutter is the room before the first lane; lanes sit every
	// gitGraphLaneWidth DIPs after it.
	gitGraphLaneGutter = 10
	gitGraphLaneWidth  = 12
)

// gitGraphBody builds the history list: lanes under the rows, one clickable
// row per commit, and the changed files of the open one below it.
func (a *App) gitGraphBody(c *mui.Context) {
	commits := a.Git.Commits
	if len(commits) == 0 {
		mutedText(c, "No commits yet.", 10).Padding(0, 6)
		return
	}
	t := c.Theme()
	lanes := 1
	for _, commit := range commits {
		lanes = max(lanes, commit.Lane+1)
	}
	indent := gitGraphLaneGutter + float32(lanes)*gitGraphLaneWidth

	// tops are the exact offsets MyGo lays the rows out at: fixed row
	// heights plus the open commit's file list, which the lanes must follow.
	tops := make([]float32, len(commits))
	var total float32
	for i, commit := range commits {
		tops[i] = total
		total += gitGraphRowHeight
		if commit.Hash == a.Git.CommitOpen {
			total += a.commitFilesHeight()
		}
	}

	root := mui.Column(c).FillWidth().Padding(0, 8, 0, 0)
	inks := []mui.Color{t.Accent, t.Success, t.Warning, t.Danger}
	index := make(map[string]int, len(commits))
	for i, commit := range commits {
		index[commit.Hash] = i
	}
	// Drawn over the rows: a hover or open fill covers the whole row, lanes
	// and dots stay visible above it, and the whole row takes the click.
	root.DrawOver(func(p *mui.Painter, r mui.Rect) {
		x := func(lane int) float32 {
			return r.X + gitGraphLaneGutter + float32(lane)*gitGraphLaneWidth
		}
		dot := func(i int) float32 { return r.Y + tops[i] + gitGraphRowHeight/2 }
		bottom := r.Y + total
		for i, commit := range commits {
			for _, parent := range commit.Parents {
				j, ok := index[parent]
				to, toLane := bottom, commit.Lane
				if ok {
					to, toLane = dot(j), commits[j].Lane
				}
				path := &mui.Path{}
				path.MoveTo(x(commit.Lane), dot(i))
				if toLane != commit.Lane {
					path.CubeTo(
						x(commit.Lane), dot(i)+gitGraphRowHeight*0.6,
						x(toLane), dot(i)+gitGraphRowHeight*0.4,
						x(toLane), dot(i)+gitGraphRowHeight,
					)
				}
				path.LineTo(x(toLane), to)
				p.StrokePath(path, 2, inks[toLane%len(inks)].Alpha(0.7))
			}
		}
		for i, commit := range commits {
			ink := inks[commit.Lane%len(inks)]
			rad := float32(3)
			if len(commit.Parents) > 1 {
				rad = 4
			}
			p.Fill(mui.Rect{
				X: x(commit.Lane) - rad - 2, Y: dot(i) - rad - 2,
				W: 2*rad + 4, H: 2*rad + 4,
			}, t.Background, 999)
			p.Fill(mui.Rect{
				X: x(commit.Lane) - rad, Y: dot(i) - rad,
				W: 2 * rad, H: 2 * rad,
			}, ink, 999)
		}
	})
	root.Children(func() {
		for _, commit := range commits {
			a.gitGraphCommit(c, commit, indent, inks)
			if commit.Hash == a.Git.CommitOpen {
				a.gitGraphFiles(c, commit.Hash, indent)
			}
		}
	})
}

// gitGraphCommit is one row, clickable across the lanes too: its left
// padding leaves their gutter empty and the lanes paint over the fill.
func (a *App) gitGraphCommit(c *mui.Context, commit git.LogCommit, indent float32, inks []mui.Color) {
	t := c.Theme()
	open := a.Git.CommitOpen == commit.Hash
	row := mui.ButtonBase(c).FillWidth().Height(gitGraphRowHeight).Gap(5).
		Padding(0, 6, 0, indent).Radius(6).Label(commit.Message)
	if open || row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		mui.Text(c, shortCommitHash(commit.Hash)).Font("monospace").FontSize(8).
			TextColor(t.TextMuted).Shrink(0)
		if ref := graphRef(commit); ref != "" {
			ink := inks[commit.Lane%len(inks)]
			mui.Text(c, ref).FontSize(8).Padding(1, 4).Radius(4).
				Background(ink.Alpha(0.16)).TextColor(ink).Shrink(0)
		}
		mui.Text(c, commit.Message).FontSize(10).Grow(1).MinWidth(0).SingleLine()
		mark := "▸"
		if open {
			mark = "▾"
		}
		mui.Text(c, mark).FontSize(8).TextColor(t.TextMuted).Shrink(0)
	})
	if row.Clicked() {
		a.toggleCommit(commit.Hash)
	}
}

// gitGraphFiles is the open commit's file list, indented past the lanes so
// their lines keep running beside it.
func (a *App) gitGraphFiles(c *mui.Context, hash string, indent float32) {
	t := c.Theme()
	height := a.commitFilesHeight()
	mui.Column(c).FillWidth().Height(height).Padding(2, 8, 2, indent).Children(func() {
		switch {
		case a.Git.CommitFilesBusy:
			a.gitGraphFileLine(c, func() { mutedText(c, "Reading changed files…", 9) })
		case a.Git.CommitFilesErr != "":
			a.gitGraphFileLine(c, func() {
				mui.Text(c, truncate(singleLine(a.Git.CommitFilesErr), 64)).
					FontSize(8).TextColor(t.Danger).SingleLine()
			})
		case len(a.Git.CommitFiles) == 0:
			a.gitGraphFileLine(c, func() { mutedText(c, "No files changed.", 9) })
		default:
			for _, file := range a.Git.CommitFiles {
				a.gitGraphFile(c, hash, file)
			}
		}
	})
}

func (a *App) gitGraphFileLine(c *mui.Context, build func()) {
	mui.Row(c).FillWidth().Height(gitGraphFileHeight).AlignItems(mui.Center).Children(build)
}

// gitGraphFile is one path a commit changed: status letter, name and, after
// it, the directory, as the staged/changes rows show them. The row opens the
// same diff overlay those rows do, for this commit's side.
func (a *App) gitGraphFile(c *mui.Context, hash string, file git.CommitFile) {
	t := c.Theme()
	code, color := commitFileMark(file.Status, t)
	path := file.Path
	name := filepath.Base(path)
	dir := filepath.Dir(path)
	if dir == "." {
		dir = ""
	}
	row := mui.ButtonBase(c).FillWidth().Height(gitGraphFileHeight).Gap(5).
		Justify(mui.Start).AlignItems(mui.Center).Padding(0, 4).Radius(4).
		Label("Commit diff of " + path)
	if row.Hovered() {
		row.Background(t.SurfaceHover)
	}
	row.Children(func() {
		mui.Text(c, code).Font("monospace").FontSize(8).TextColor(color).Shrink(0)
		mui.Text(c, truncate(name, 22)).FontSize(9).SingleLine()
		if dir != "" {
			mutedText(c, truncate(dir, 18), 8).SingleLine()
		}
	})
	if row.Clicked() {
		a.openGitCommitPreview(hash, file)
	}
}

// commitFileMark is the letter and color of a commit file's status.
func commitFileMark(status byte, t *mui.Theme) (string, mui.Color) {
	switch status {
	case 'A':
		return "A", t.Success
	case 'D':
		return "D", t.Danger
	case 'R':
		return "R", t.Accent
	case 'C':
		return "C", t.Accent
	case 'M', 'T':
		return string(status), t.Warning
	case 0:
		return "?", t.TextMuted
	default:
		return string(status), t.TextMuted
	}
}

// commitFilesHeight is the height of the open commit's file list: one line
// while loading, failing or empty, otherwise one per file, plus padding.
func (a *App) commitFilesHeight() float32 {
	n := len(a.Git.CommitFiles)
	if a.Git.CommitFilesBusy || a.Git.CommitFilesErr != "" || n == 0 {
		n = 1
	}
	return float32(n*gitGraphFileHeight + 4)
}

// shortCommitHash is the abbreviated hash the rows show.
func shortCommitHash(hash string) string {
	if len(hash) <= 7 {
		return hash
	}
	return hash[:7]
}

// graphRef shows one ref per commit: the 300 DIP panel has room for the
// current branch, not every decoration.
func graphRef(commit git.LogCommit) string {
	if len(commit.Refs) == 0 {
		return ""
	}
	return shortGraphText(commit.Refs[0], 12)
}
