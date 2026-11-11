// Package logs appends agent diagnostics to per-agent files and rotates them.
//
// Files are mode 0600 because agent stderr may contain sensitive data. A log
// rotates when the file would pass its size limit: the current file becomes
// <name>.1 (replacing the previous backup) and writing continues in a fresh
// file. Only one backup is kept.
package logs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// DefaultMaxBytes is the size limit for one agent log file.
const DefaultMaxBytes int64 = 1 << 20

// Log is one append-only, size-bounded log file. Its methods are safe for
// concurrent use.
type Log struct {
	mu   sync.Mutex
	path string
	max  int64
	file *os.File
	size int64
}

// Open creates the parent directory and opens path for appending. An existing
// oversized file is rotated first.
func Open(path string, max int64) (*Log, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("logs: empty path")
	}
	if max <= 0 {
		max = DefaultMaxBytes
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("logs: create directory: %w", err)
	}
	l := &Log{path: path, max: max}
	if info, err := os.Stat(path); err == nil && info.Size() >= max {
		if err := l.rotateLocked(); err != nil {
			return nil, err
		}
	}
	if err := l.openLocked(); err != nil {
		return nil, err
	}
	return l, nil
}

// Path is the file being written.
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Write appends p, rotating first when it would pass the limit. A chunk larger
// than the limit keeps only its tail.
func (l *Log) Write(p []byte) (int, error) {
	if l == nil {
		return len(p), nil
	}
	if len(p) == 0 {
		return 0, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		if err := l.openLocked(); err != nil {
			return 0, err
		}
	}
	if int64(len(p)) > l.max {
		p = p[int64(len(p))-l.max:]
	}
	if l.size+int64(len(p)) > l.max {
		if err := l.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// Close closes the file. It is safe to call more than once.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// openLocked opens the current file and remembers its size. A failed stat
// leaves the file open and starts counting from zero.
func (l *Log) openLocked() error {
	if l.file != nil {
		return nil
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("logs: open %s: %w", l.path, err)
	}
	l.file = file
	l.size = 0
	if info, err := file.Stat(); err == nil {
		l.size = info.Size()
	}
	return nil
}

// rotateLocked moves the current file aside and opens a fresh one. A missing
// current file is not an error: the next open creates it.
func (l *Log) rotateLocked() error {
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	_ = os.Remove(l.path + ".1")
	if err := os.Rename(l.path, l.path+".1"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("logs: rotate %s: %w", l.path, err)
	}
	l.size = 0
	return l.openLocked()
}
