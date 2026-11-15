package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionWritesAgentLog(t *testing.T) {
	dataDir := t.TempDir()
	app, tt := newSettingsApp(t, dataDir, t.TempDir())
	connect(t, app, tt)
	path := filepath.Join(dataDir, "logs", "fake.log")
	waitFor(t, app, tt, func() bool {
		data, err := os.ReadFile(path)
		return err == nil && strings.Contains(string(data), "fake-agent ready")
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	for _, want := range []string{"=== ", "command:", "fake-agent ready"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("log missing %q:\n%s", want, data)
		}
	}
}
