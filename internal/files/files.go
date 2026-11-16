// Package files lists and changes paths inside one workspace for the Files
// panel. Every operation is confined to the workspace root: paths come from
// the tree, never from a shell, and a relative path that would leave the root
// is rejected.
package files

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var (
	// ErrOutsideWorkspace means a relative path tried to leave the root.
	ErrOutsideWorkspace = errors.New("path is outside the workspace")
	// ErrInvalidName means a name is not one plain file or folder name.
	ErrInvalidName = errors.New("enter one file or folder name without slashes")
	// ErrExists means the target name is already taken in that folder.
	ErrExists = errors.New("a file or folder with that name already exists")
	// ErrInvalidMove means a folder would be moved into itself or a child.
	ErrInvalidMove = errors.New("a folder cannot be moved into itself")
	// ErrWorkspaceRoot means an operation tried to rename, move or delete the
	// workspace root itself.
	ErrWorkspaceRoot = errors.New("the workspace folder cannot be changed here")
)

// Entry is one visible child of a folder. Path is slash-separated and
// relative to the workspace root so the UI can use it as a map key on every
// platform.
type Entry struct {
	Name string
	Path string
	Dir  bool
}

// CleanName trims surrounding space and accepts one plain file or folder
// name, without path separators, NUL, "." or "..". The git metadata folder is
// reserved.
func CleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", ErrInvalidName
	case name == "." || name == "..":
		return "", ErrInvalidName
	case strings.EqualFold(name, ".git"):
		return "", errors.New(".git is reserved for Git")
	case strings.ContainsAny(name, `/\`):
		return "", ErrInvalidName
	case strings.ContainsRune(name, 0):
		return "", ErrInvalidName
	}
	return name, nil
}

// Resolve returns the absolute path of rel inside root and rejects a relative
// path that escapes it. An empty rel resolves to root.
func Resolve(root, rel string) (string, error) {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("workspace must be an absolute path")
	}
	rel = strings.TrimSpace(rel)
	if filepath.IsAbs(rel) {
		return "", ErrOutsideWorkspace
	}
	target := filepath.Join(root, filepath.FromSlash(rel))
	inside, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	if inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", ErrOutsideWorkspace
	}
	return target, nil
}

// ReadDir lists one folder in workspace root order: directories first, then
// files, each group case-insensitive by name. The git metadata folder is
// skipped. Symlinks are kept as files, so the panel never follows one out of
// the workspace while browsing.
func ReadDir(root, rel string) ([]Entry, error) {
	dir, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(items))
	for _, item := range items {
		name := item.Name()
		if name == ".git" {
			continue
		}
		entries = append(entries, Entry{
			Name: name,
			Path: joinRel(rel, name),
			Dir:  item.IsDir() && item.Type()&os.ModeSymlink == 0,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		left, right := strings.ToLower(entries[i].Name), strings.ToLower(entries[j].Name)
		if left != right {
			return left < right
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// Create makes one file or folder in dir and returns its slash-relative path.
func Create(root, dir, name string, directory bool) (string, error) {
	name, err := CleanName(name)
	if err != nil {
		return "", err
	}
	parent, err := Resolve(root, dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(parent)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("the target is not a folder")
	}
	target := filepath.Join(parent, name)
	if directory {
		if err := os.Mkdir(target, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				return "", ErrExists
			}
			return "", err
		}
	} else {
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return "", ErrExists
			}
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
	}
	return joinRel(dir, name), nil
}

// Rename renames one file or folder inside its own folder and returns the new
// slash-relative path.
func Rename(root, rel, name string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", ErrWorkspaceRoot
	}
	name, err := CleanName(name)
	if err != nil {
		return "", err
	}
	source, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(source); err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(source), name)
	if target == source {
		return filepath.ToSlash(rel), nil
	}
	if err := ensureMissing(target); err != nil {
		return "", err
	}
	if err := os.Rename(source, target); err != nil {
		return "", err
	}
	parent := path.Dir(filepath.ToSlash(rel))
	if parent == "." {
		parent = ""
	}
	return joinRel(parent, name), nil
}

// Move moves one file or folder into destDir and returns its new
// slash-relative path. Moving a folder into itself or one of its children is
// rejected, as is overwriting an existing path.
func Move(root, rel, destDir string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", ErrWorkspaceRoot
	}
	source, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return "", err
	}
	dest, err := Resolve(root, destDir)
	if err != nil {
		return "", err
	}
	destInfo, err := os.Stat(dest)
	if err != nil {
		return "", err
	}
	if !destInfo.IsDir() {
		return "", errors.New("the drop target is not a folder")
	}
	if info.IsDir() {
		prefix := source + string(filepath.Separator)
		if dest == source || strings.HasPrefix(dest, prefix) {
			return "", ErrInvalidMove
		}
	}
	target := filepath.Join(dest, filepath.Base(source))
	if target == source {
		return filepath.ToSlash(rel), nil
	}
	if err := ensureMissing(target); err != nil {
		return "", err
	}
	if err := os.Rename(source, target); err != nil {
		return "", err
	}
	return joinRel(destDir, filepath.Base(source)), nil
}

// Remove deletes one file, symlink or folder (with everything inside it). The
// workspace root cannot be removed through the panel.
func Remove(root, rel string) error {
	if strings.TrimSpace(rel) == "" {
		return ErrWorkspaceRoot
	}
	target, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.RemoveAll(target)
	}
	return os.Remove(target)
}

func ensureMissing(target string) error {
	_, err := os.Lstat(target)
	switch {
	case err == nil:
		return ErrExists
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("check target: %w", err)
	}
}

func joinRel(dir, name string) string {
	dir = strings.Trim(strings.TrimSpace(dir), "/")
	if dir == "" || dir == "." {
		return name
	}
	return path.Join(dir, name)
}
