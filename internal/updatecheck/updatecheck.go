// Package updatecheck asks the npm registry for the latest published version
// of the CLIs this app can start.
//
// It only reports that a newer version exists. The app never downloads,
// installs or runs an update, and it never uses the registry to discover
// agents: the package names come from the hand-maintained builtin table.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultRegistry is the npm registry the update check reads. A Checker can
// point elsewhere in tests.
const DefaultRegistry = "https://registry.npmjs.org"

// requestTimeout bounds one package lookup. Lookups run concurrently, so a
// whole check normally takes about this long when the network is slow.
const requestTimeout = 5 * time.Second

// Target is one installed CLI and the npm package that publishes it.
type Target struct {
	ID      string
	Package string
	Current string
}

// Result is the answer for one target. A non-nil Err means the check could
// not tell, which is not a reason to show an update.
type Result struct {
	Current string
	Latest  string
	Update  bool
	Err     error
}

// Checker reads package metadata from an npm registry. The zero value uses
// DefaultRegistry and a client with a 5 second timeout.
type Checker struct {
	RegistryURL string
	Client      *http.Client
}

// Check runs one checker with the default registry.
func Check(ctx context.Context, targets []Target) map[string]Result {
	return Checker{}.Check(ctx, targets)
}

// Check looks up every target concurrently. The returned map is keyed by
// target ID. An empty package, a missing version or an HTTP/JSON failure
// yields a Result with Err set instead of failing the whole batch.
func (c Checker) Check(ctx context.Context, targets []Target) map[string]Result {
	results := make(map[string]Result, len(targets))
	if len(targets) == 0 {
		return results
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	registry := strings.TrimSpace(c.RegistryURL)
	if registry == "" {
		registry = DefaultRegistry
	}

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, target := range targets {
		target := target
		if target.ID == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			latest, err := latestVersion(ctx, client, registry, target.Package)
			result := Result{Current: target.Current, Latest: latest, Err: err}
			if err == nil && CompareVersions(target.Current, latest) < 0 {
				result.Update = true
			}
			mu.Lock()
			results[target.ID] = result
			mu.Unlock()
		}()
	}
	wg.Wait()
	return results
}

// latestVersion reads dist-tag latest for one package.
func latestVersion(ctx context.Context, client *http.Client, registry, pkg string) (string, error) {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return "", errors.New("updatecheck: empty package name")
	}
	endpoint := strings.TrimRight(registry, "/") + "/" + url.PathEscape(pkg) + "/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("updatecheck: %s: %w", pkg, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "acp-gui")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("updatecheck: %s: %w", pkg, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("updatecheck: %s: %s", pkg, resp.Status)
	}

	var body struct {
		Version string `json:"version"`
	}
	// /latest is a small document; the limit keeps a wrong registry or a
	// captive portal from feeding the decoder an unbounded body.
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("updatecheck: %s: decode: %w", pkg, err)
	}
	version := strings.TrimSpace(body.Version)
	if version == "" {
		return "", fmt.Errorf("updatecheck: %s: empty version", pkg)
	}
	return version, nil
}

// CompareVersions compares two semantic versions, returning -1 when a is
// older, 0 when they are equal and 1 when a is newer. A leading v, a missing
// patch component and build metadata are accepted. Pre-release identifiers
// follow semver: numeric identifiers compare numerically, numeric sorts
// before text, and a pre-release sorts before the matching release. If either
// value is not a version, CompareVersions returns 0, so an unknown local
// version never produces an update notice.
func CompareVersions(a, b string) int {
	av, ok := parseVersion(a)
	if !ok {
		return 0
	}
	bv, ok := parseVersion(b)
	if !ok {
		return 0
	}
	if cmp := compareCore(av.core, bv.core); cmp != 0 {
		return cmp
	}
	return comparePrerelease(av.pre, bv.pre)
}

type version struct {
	core []int
	pre  []string
}

func parseVersion(text string) (version, bool) {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(strings.TrimPrefix(text, "v"), "V")
	if text == "" {
		return version{}, false
	}
	if i := strings.IndexByte(text, '+'); i >= 0 {
		text = text[:i]
	}
	var pre []string
	if i := strings.IndexByte(text, '-'); i >= 0 {
		preText := text[i+1:]
		text = text[:i]
		if preText == "" {
			return version{}, false
		}
		pre = strings.Split(preText, ".")
		for _, id := range pre {
			if id == "" {
				return version{}, false
			}
		}
	}
	parts := strings.Split(text, ".")
	if len(parts) == 0 {
		return version{}, false
	}
	core := make([]int, len(parts))
	for i, part := range parts {
		if part == "" {
			return version{}, false
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return version{}, false
		}
		core[i] = n
	}
	return version{core: core, pre: pre}, true
}

func compareCore(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func comparePrerelease(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1 // 1.0.0 is newer than 1.0.0-rc.1
	case len(b) == 0:
		return -1
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] == b[i] {
			continue
		}
		aNum, aOK := numericIdentifier(a[i])
		bNum, bOK := numericIdentifier(b[i])
		switch {
		case aOK && bOK:
			if len(aNum) < len(bNum) {
				return -1
			}
			if len(aNum) > len(bNum) {
				return 1
			}
			if aNum < bNum {
				return -1
			}
			if aNum > bNum {
				return 1
			}
			continue
		case aOK:
			return -1
		case bOK:
			return 1
		default:
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// numericIdentifier trims leading zeroes and reports whether id consists of
// digits only. Comparing the remaining digit strings by length keeps large
// build numbers from overflowing an int.
func numericIdentifier(id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	trimmed := strings.TrimLeft(id, "0")
	if trimmed == "" {
		trimmed = "0"
	}
	return trimmed, true
}
