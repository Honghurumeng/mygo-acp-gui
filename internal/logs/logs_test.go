package logs

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteAppendsAndCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "logs", "pi.log")
	log, err := Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	if log.Path() != path {
		t.Errorf("path = %q", log.Path())
	}
	if _, err := log.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("second\n")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first\nsecond\n" {
		t.Errorf("contents = %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestWriteRotatesAndKeepsOneBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	log, err := Open(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	if _, err := log.Write([]byte("1234567890")); err != nil { // 10 bytes
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("abcdefghij")); err != nil { // would pass 16
		t.Fatal(err)
	}
	if got := readFile(t, path+".1"); got != "1234567890" {
		t.Errorf("backup = %q", got)
	}
	if got := readFile(t, path); got != "abcdefghij" {
		t.Errorf("current = %q", got)
	}

	if _, err := log.Write([]byte("KLMNOPQRST")); err != nil { // rotates again
		t.Fatal(err)
	}
	if got := readFile(t, path+".1"); got != "abcdefghij" {
		t.Errorf("replaced backup = %q", got)
	}
	if got := readFile(t, path); got != "KLMNOPQRST" {
		t.Errorf("replaced current = %q", got)
	}
	if _, err := os.Stat(path + ".2"); !os.IsNotExist(err) {
		t.Errorf("kept more than one backup: %v", err)
	}
}

func TestOversizedChunkKeepsTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	log, err := Open(path, 32)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	long := strings.Repeat("x", 20) + strings.Repeat("y", 40)
	if _, err := log.Write([]byte(long)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 32 {
		t.Errorf("size = %d, want 32", len(data))
	}
	if !strings.HasSuffix(string(data), strings.Repeat("y", 32)) {
		t.Errorf("kept the wrong tail: %q", data)
	}
}

func TestOpenRotatesExistingOversizedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("old", 20)), 0o600); err != nil {
		t.Fatal(err)
	}
	log, err := Open(path, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.Write([]byte("new\n")); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "new\n" {
		t.Errorf("current = %q", got)
	}
	if got := readFile(t, path+".1"); got != strings.Repeat("old", 20) {
		t.Errorf("backup = %q", got)
	}
}

func TestConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	log, err := Open(path, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := log.Write([]byte("line\n")); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Repeat("line\n", 160); string(data) != want {
		t.Errorf("contents = %q", data)
	}
}

func TestNilAndEmptyLog(t *testing.T) {
	var log *Log
	if _, err := log.Write([]byte("x")); err != nil {
		t.Errorf("nil Write: %v", err)
	}
	if err := log.Close(); err != nil {
		t.Errorf("nil Close: %v", err)
	}
	if log.Path() != "" {
		t.Errorf("nil Path = %q", log.Path())
	}
	if _, err := Open("", 0); err == nil {
		t.Error("empty path was accepted")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
