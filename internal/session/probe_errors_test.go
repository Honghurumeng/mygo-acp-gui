package session

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
	"github.com/reed/acp-gui/internal/agent"
)

func shellLaunch(t *testing.T, script string) agent.LaunchSpec {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell probe fixture")
	}
	return agent.LaunchSpec{ID: "shell", Name: "shell", Command: "/bin/sh", Args: []string{"-c", script}}
}

func TestProbeRejectsNonACPOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := Probe(ctx, shellLaunch(t, "echo not-json; exit 1"))
	if err == nil {
		t.Fatal("non-ACP stdout was accepted")
	}
	if !strings.Contains(err.Error(), "not clean ACP JSON") {
		t.Fatalf("error = %v, want a non-ACP classification", err)
	}
}

func TestProbeReportsImmediateExitAndStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	_, err := Probe(ctx, shellLaunch(t, "echo boom >&2; exit 3"))
	if err == nil {
		t.Fatal("immediately exiting agent was accepted")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error = %v, want the stderr tail", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("immediate exit took %s", elapsed)
	}
}

func TestProbeHonorsContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Probe(ctx, shellLaunch(t, "while :; do :; done"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("probe timeout took %s", elapsed)
	}
}

func TestStartReportsAuthRequired(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	launch, workspace := fakeLaunch(t)
	launch.Env = append(append([]string(nil), launch.Env...), "FAKE_AGENT_NO_SESSION=1")

	_, err := Start(ctx, Config{Launch: launch, Workspace: workspace})
	if err == nil {
		t.Fatal("session/new -32000 was accepted")
	}
	if !IsAuthRequired(err) {
		t.Fatalf("error = %v, want auth required", err)
	}
	var auth *AuthRequiredError
	if !errors.As(err, &auth) {
		t.Fatalf("error does not wrap AuthRequiredError: %T", err)
	}
	if len(auth.Methods) == 0 || auth.Methods[0].Name != "Fake login" {
		t.Errorf("auth methods = %+v", auth.Methods)
	}
	if !strings.Contains(auth.Error(), "Fake login") {
		t.Errorf("auth message = %q", auth.Error())
	}
}

func TestAuthErrorDetection(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"rpc code", &acp.RPCError{Code: -32000, Message: "session/new failed"}, true},
		{"rpc auth", &acp.RPCError{Code: -1, Message: "Authentication required"}, true},
		{"rpc login", &acp.RPCError{Code: -1, Message: "please login"}, true},
		{"wrapped code", fmt.Errorf("session/new: %w", &acp.RPCError{Code: -32000}), true},
		{"plain auth", errors.New("auth required"), true},
		{"other", &acp.RPCError{Code: -32602, Message: "bad params"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAuthError(tc.err); got != tc.want {
				t.Errorf("isAuthError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}

	rpc := &acp.RPCError{Code: -32000, Message: "authentication required"}
	auth := &AuthRequiredError{
		Methods: []acp.AuthMethod{{ID: "fake-login", Name: "Fake login", Description: "Run it"}},
		Err:     rpc,
	}
	if !IsAuthRequired(auth) {
		t.Fatal("IsAuthRequired missed the wrapped error")
	}
	if !errors.Is(auth, rpc) {
		t.Fatal("AuthRequiredError does not unwrap its cause")
	}
	if !strings.Contains(auth.Error(), "Fake login") || !strings.Contains(auth.Error(), "authentication required") {
		t.Errorf("message = %q", auth.Error())
	}
	if IsAuthRequired(errors.New("other")) {
		t.Error("unrelated error was treated as auth")
	}
}
