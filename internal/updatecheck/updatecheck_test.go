package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.5.2", "1.5.2", 0},
		{"v1.5.2", "1.5.2", 0},
		{"1.5", "1.5.0", 0},
		{"1.5.2+build.7", "1.5.2", 0},
		{"1.5.2", "1.6.0", -1},
		{"1.6.0", "1.5.2", 1},
		{"1.5.2-beta.1", "1.5.2", -1},
		{"1.5.2", "1.5.2-beta.1", 1},
		{"1.5.2-beta.2", "1.5.2-beta.10", -1},
		{"1.5.2-beta.10", "1.5.2-beta.2", 1},
		{"1.5.2-alpha", "1.5.2-beta", -1},
		{"1.5.2-1", "1.5.2-alpha", -1},
		{"not-a-version", "1.0.0", 0},
		{"1.0.0", "", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCheckReadsLatestVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/plain/latest", "/@scope/pkg/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"2.0.0"}`))
		case "/same/latest":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
		case "/missing/latest":
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected request path %q", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	results := (Checker{RegistryURL: server.URL}).Check(context.Background(), []Target{
		{ID: "plain", Package: "plain", Current: "1.0.0"},
		{ID: "scoped", Package: "@scope/pkg", Current: "1.0.0"},
		{ID: "same", Package: "same", Current: "1.0.0"},
		{ID: "missing", Package: "missing", Current: "1.0.0"},
	})

	if got := results["plain"]; got.Err != nil || !got.Update || got.Latest != "2.0.0" {
		t.Errorf("plain = %+v", got)
	}
	if got := results["scoped"]; got.Err != nil || !got.Update || got.Latest != "2.0.0" {
		t.Errorf("scoped = %+v", got)
	}
	if got := results["same"]; got.Err != nil || got.Update || got.Latest != "1.0.0" {
		t.Errorf("same = %+v", got)
	}
	if got := results["missing"]; got.Err == nil || got.Update {
		t.Errorf("missing = %+v, want an error and no update", got)
	}
}

func TestCheckStopsWithContext(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	results := (Checker{RegistryURL: server.URL}).Check(ctx, []Target{
		{ID: "slow", Package: "slow", Current: "1.0.0"},
	})
	if got := results["slow"]; got.Err == nil {
		t.Fatalf("slow = %+v, want a context error", got)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("check took %s after context cancellation", elapsed)
	}
}
