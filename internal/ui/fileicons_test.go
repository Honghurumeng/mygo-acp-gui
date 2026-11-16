package ui

import (
	"testing"

	mui "github.com/egoist/mygo/ui"
)

func TestFileIconForType(t *testing.T) {
	tests := []struct {
		name string
		want *mui.SVG
	}{
		{"main.go", codeIcon},
		{"app.tsx", codeIcon},
		{"style.css", codeIcon},
		{"index.html", codeIcon},
		{"README.md", textIcon},
		{"notes.txt", textIcon},
		{"data.json", bracesIcon},
		{"tsconfig.jsonc", bracesIcon},
		{"logo.svg", imageIcon},
		{"shot.PNG", imageIcon},
		{"build.sh", terminalIcon},
		{"init.zsh", terminalIcon},
		{"go.mod", settingsIcon},
		{"pnpm-lock.yaml", settingsIcon},
		{"Makefile", settingsIcon},
		{"Dockerfile", settingsIcon},
		{".gitignore", settingsIcon},
		{".env.local", settingsIcon},
		{"LICENSE", textIcon},
		{"Makefile.bak", fileIcon},
		{"archive.xyz", fileIcon},
		{"binary", fileIcon},
		{"", fileIcon},
	}
	for _, test := range tests {
		if got := fileIconFor(test.name); got != test.want {
			t.Errorf("fileIconFor(%q) = %p, want %p", test.name, got, test.want)
		}
	}
}
