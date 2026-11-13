package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestValidateWorkspace(t *testing.T) {
	dir := t.TempDir()
	got, err := ValidateWorkspace(dir)
	if err != nil {
		t.Fatalf("valid workspace: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("workspace = %q, want %q", got, filepath.Clean(dir))
	}

	if _, err := ValidateWorkspace("relative/dir"); err == nil {
		t.Error("relative workspace was accepted")
	}
	if _, err := ValidateWorkspace(filepath.Join(dir, "missing")); err == nil {
		t.Error("missing workspace was accepted")
	}
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateWorkspace(file); err == nil {
		t.Error("file workspace was accepted")
	}
}

func TestPiLaunchSpec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executables use a shebang")
	}
	root := t.TempDir()
	bundleDir := filepath.Join(root, "agents", "pi-acp")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(bundleDir, "pi-acp.bundle.mjs")
	if err := os.WriteFile(bundle, []byte("// bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "VERSION"), []byte("0.4.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bin := t.TempDir()
	node := fakeExecutable(t, bin, "node")
	pi := fakeExecutable(t, bin, "pi")
	env := Environment{
		Env:         []string{"PATH=/usr/bin:/bin"},
		NodePath:    node,
		PiPath:      pi,
		NodeMajor:   22,
		NodeVersion: "v22.20.0",
		PiVersion:   "1.5.2",
	}

	spec, err := PiLaunchSpec(root, env)
	if err != nil {
		t.Fatalf("PiLaunchSpec: %v", err)
	}
	if spec.Command != node {
		t.Errorf("command = %q, want %q", spec.Command, node)
	}
	if len(spec.Args) != 1 || spec.Args[0] != bundle {
		t.Errorf("args = %q, want the bundle", spec.Args)
	}
	if spec.Version != "1.5.2" {
		t.Errorf("version = %q, want the pi CLI version", spec.Version)
	}
	path := envValue(spec.Env, "PATH")
	for _, dir := range []string{bin, filepath.Dir(node), filepath.Dir(pi)} {
		if !strings.Contains(path, dir) {
			t.Errorf("PATH %q does not contain %q", path, dir)
		}
	}

	env.NodeMajor = 20
	if _, err := PiLaunchSpec(root, env); err == nil {
		t.Error("old Node was accepted")
	}
	env.NodeMajor = 22
	env.PiPath = ""
	if _, err := PiLaunchSpec(root, env); err == nil {
		t.Error("missing pi was accepted")
	}
}

func TestCommandCodeLaunchSpec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executables use a shebang")
	}
	bin := t.TempDir()
	commandCode := fakeExecutable(t, bin, "command-code")
	env := Environment{
		Env:                []string{"PATH=/usr/bin:/bin"},
		NodePath:           fakeExecutable(t, bin, "node"),
		NodeMajor:          22,
		CommandCodePath:    commandCode,
		CommandCodeVersion: "1.88.5",
	}

	spec, err := CommandCodeLaunchSpec(env)
	if err != nil {
		t.Fatalf("CommandCodeLaunchSpec: %v", err)
	}
	if spec.ID != CommandCodeID {
		t.Errorf("id = %q", spec.ID)
	}
	if spec.Name != "Command Code" {
		t.Errorf("name = %q", spec.Name)
	}
	if spec.Command != commandCode {
		t.Errorf("command = %q, want %q", spec.Command, commandCode)
	}
	if len(spec.Args) != 1 || spec.Args[0] != "acp" {
		t.Errorf("args = %q, want [acp]", spec.Args)
	}
	if spec.Source != SourceBuiltin {
		t.Errorf("source = %q", spec.Source)
	}
	if spec.Version != "1.88.5" {
		t.Errorf("version = %q", spec.Version)
	}
	if spec.LoginHint != "command-code login" {
		t.Errorf("login hint = %q", spec.LoginHint)
	}
	if !strings.Contains(envValue(spec.Env, "PATH"), bin) {
		t.Errorf("PATH %q does not contain %q", envValue(spec.Env, "PATH"), bin)
	}

	env.CommandCodePath = ""
	if _, err := CommandCodeLaunchSpec(env); err == nil {
		t.Error("missing command-code was accepted")
	}
}

func TestBuiltinLaunchResultsKeepsAvailableAgents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake executables use a shebang")
	}
	root := t.TempDir()
	bundleDir := filepath.Join(root, "agents", "pi-acp")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "pi-acp.bundle.mjs"), []byte("// bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	env := Environment{
		Env:                []string{"PATH=/usr/bin:/bin"},
		NodePath:           fakeExecutable(t, bin, "node"),
		PiPath:             fakeExecutable(t, bin, "pi"),
		NodeMajor:          22,
		CommandCodePath:    fakeExecutable(t, bin, "command-code"),
		CommandCodeVersion: "1.88.5",
	}

	results := BuiltinLaunchResults(root, env)
	if len(results) != 2 {
		t.Fatalf("results = %+v, want pi and command-code", results)
	}
	for _, result := range results {
		if result.Err != nil {
			t.Errorf("%s: %v", result.Name, result.Err)
		}
		if result.Name == "" || result.InstallHint == "" {
			t.Errorf("missing metadata: %+v", result.BuiltinAgent)
		}
	}
	if results[0].ID != PiID || results[1].ID != CommandCodeID {
		t.Errorf("order = %q, %q", results[0].ID, results[1].ID)
	}

	// A broken pi must not hide a working Command Code install.
	env.PiPath = ""
	results = BuiltinLaunchResults(root, env)
	if len(results) != 2 {
		t.Fatalf("results = %+v, want both rows", results)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "pi") {
		t.Errorf("pi error = %v", results[0].Err)
	}
	if results[1].Err != nil || results[1].Spec.ID != CommandCodeID {
		t.Errorf("command-code = %+v", results[1])
	}
}

func TestResolveEnvironmentLoginShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login-shell resolution is Unix-only")
	}
	bin := t.TempDir()
	fakeExecutable(t, bin, "node")
	fakeExecutable(t, bin, "pi")
	fakeExecutable(t, bin, "command-code")
	shell := filepath.Join(t.TempDir(), "fake-shell")
	script := "#!/bin/sh\nexec /bin/sh -c \"$2\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := ResolveEnvironment(ctx, ResolveOptions{
		Shell:   shell,
		Env:     []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()},
		Timeout: 2 * time.Second,
	})
	if env.NodePath != filepath.Join(bin, "node") {
		t.Errorf("node = %q, want fake node; warnings=%v", env.NodePath, env.Warnings)
	}
	if env.PiPath != filepath.Join(bin, "pi") {
		t.Errorf("pi = %q, want fake pi; warnings=%v", env.PiPath, env.Warnings)
	}
	if env.NodeMajor != 22 {
		t.Errorf("node major = %d (%s)", env.NodeMajor, env.NodeVersion)
	}
	if env.PiVersion != "9.9.9" {
		t.Errorf("pi version = %q", env.PiVersion)
	}
	if env.CommandCodePath != filepath.Join(bin, "command-code") {
		t.Errorf("command-code = %q, want fake command-code; warnings=%v", env.CommandCodePath, env.Warnings)
	}
	if env.CommandCodeVersion != "1.88.5" {
		t.Errorf("command-code version = %q", env.CommandCodeVersion)
	}
	if !strings.Contains(envValue(env.Env, "PATH"), bin) {
		t.Errorf("resolved PATH lost the fake bin: %q", envValue(env.Env, "PATH"))
	}

	rel := ResolveEnvironment(ctx, ResolveOptions{
		Shell:    shell,
		Env:      []string{"PATH=" + bin + ":/usr/bin:/bin"},
		NodePath: "./node",
		Timeout:  2 * time.Second,
	})
	if rel.NodePath != filepath.Join(bin, "node") {
		t.Errorf("invalid override discarded the shell result: %q; warnings=%v", rel.NodePath, rel.Warnings)
	}
	if len(rel.Warnings) == 0 {
		t.Error("relative node override produced no warning")
	}
}

func fakeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	version := "v22.1.0"
	if name == "pi" {
		version = "9.9.9"
	}
	if name == "command-code" {
		version = "1.88.5"
	}
	script := "#!/bin/sh\necho " + version + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveEnvironmentInteractiveFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("login-shell resolution is Unix-only")
	}
	nodeBin := t.TempDir()
	piBin := t.TempDir()
	fakeExecutable(t, nodeBin, "node")
	fakeExecutable(t, piBin, "pi")
	shell := filepath.Join(t.TempDir(), "fake-shell")
	script := `#!/bin/sh
if [ "$1" = "-ilc" ]; then
  PATH="` + nodeBin + `:` + piBin + `:/usr/bin:/bin"
  export PATH
fi
exec /bin/sh -c "$2"
`
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := ResolveEnvironment(ctx, ResolveOptions{
		Shell:   shell,
		Env:     []string{"PATH=" + nodeBin + ":/usr/bin:/bin"},
		Timeout: 2 * time.Second,
	})
	if env.NodePath != filepath.Join(nodeBin, "node") {
		t.Errorf("node = %q", env.NodePath)
	}
	if env.PiPath != filepath.Join(piBin, "pi") {
		t.Errorf("pi = %q, want interactive fallback; warnings=%v", env.PiPath, env.Warnings)
	}
	if !strings.Contains(envValue(env.Env, "PATH"), piBin) {
		t.Errorf("interactive PATH was not kept: %q", envValue(env.Env, "PATH"))
	}
}

func TestResolveEnvironmentNPMGlobalFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake npm uses a shebang")
	}
	bin := t.TempDir()
	prefix := t.TempDir()
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, filepath.Join(prefix, "bin", "pi"), "#!/bin/sh\necho 9.9.9\n")
	fakeExecutable(t, bin, "node")
	npm := filepath.Join(bin, "npm")
	writeExecutable(t, npm, "#!/bin/sh\nprintf '%s\\n' '"+prefix+"'\n")
	shell := filepath.Join(t.TempDir(), "fake-shell")
	writeExecutable(t, shell, "#!/bin/sh\nexec /bin/sh -c \"$2\"\n")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := ResolveEnvironment(ctx, ResolveOptions{
		Shell:   shell,
		Env:     []string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir()},
		Timeout: 2 * time.Second,
	})
	if env.PiPath != filepath.Join(prefix, "bin", "pi") {
		t.Errorf("pi = %q, want npm global fallback; warnings=%v", env.PiPath, env.Warnings)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
