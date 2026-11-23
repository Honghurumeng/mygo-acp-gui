package files

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaxTextSize bounds the text the viewer loads or saves through the Files
// panel. A larger file is an error rather than a truncated preview.
const MaxTextSize = 512 << 10

// MaxImageSize bounds the picture the viewer loads, matching the composer's
// attachments. A larger file is an error rather than a decode that would
// take the window's memory.
const MaxImageSize = 15 << 20

var (
	// ErrTextTooLarge means a file cannot be shown or edited in the viewer.
	ErrTextTooLarge = errors.New("file is too large to view or edit")
	// ErrImageTooLarge means a picture cannot be shown in the viewer.
	ErrImageTooLarge = errors.New("image is too large to view")
	// ErrBinary means a file has NUL bytes and is not text.
	ErrBinary = errors.New("binary file cannot be viewed or edited")
	// ErrSymlink keeps the viewer from following a link out of the workspace.
	ErrSymlink = errors.New("symbolic links cannot be viewed or edited here")
)

// ReadText reads a UTF-8 text file inside the workspace. Binary and oversized
// files are errors, as in the Git preview.
func ReadText(root, rel string) (string, error) {
	path, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", ErrSymlink
	}
	if info.IsDir() {
		return "", fmt.Errorf("%q is a folder", rel)
	}
	if info.Size() > MaxTextSize {
		return "", ErrTextTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", ErrBinary
	}
	if len(data) > MaxTextSize {
		return "", ErrTextTooLarge
	}
	return string(data), nil
}

// ReadImage reads a picture inside the workspace for the viewer. Unlike
// ReadText it accepts binary bytes, and its limit is larger than a text
// file's, but it still refuses a symbolic link so that browsing cannot lead
// out of the workspace.
func ReadImage(root, rel string) ([]byte, error) {
	path, err := Resolve(root, rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSymlink
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%q is a folder", rel)
	}
	if info.Size() > MaxImageSize {
		return nil, ErrImageTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxImageSize {
		return nil, ErrImageTooLarge
	}
	return data, nil
}

// SaveText replaces the contents of an existing text file inside the
// workspace. It preserves the file's permissions, writes a temporary file in
// the same folder and renames it into place, so a failed write leaves the
// original untouched. A file that disappeared since it was opened is not
// recreated.
func SaveText(root, rel, text string) error {
	path, err := Resolve(root, rel)
	if err != nil {
		return err
	}
	if len(text) > MaxTextSize {
		return ErrTextTooLarge
	}
	if strings.ContainsRune(text, 0) {
		return ErrBinary
	}
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%q no longer exists", rel)
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return ErrSymlink
	}
	if info.IsDir() {
		return fmt.Errorf("%q is a folder", rel)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".acp-gui-edit-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	remove := func() {
		_ = tmp.Close()
		_ = os.Remove(name)
	}
	if _, err := tmp.WriteString(text); err != nil {
		remove()
		return err
	}
	if err := tmp.Sync(); err != nil {
		remove()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		remove()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
