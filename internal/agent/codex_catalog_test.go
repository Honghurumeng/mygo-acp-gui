package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCodexCatalogJSONContainsMetadata(t *testing.T) {
	data, err := CodexCatalogJSON([]CodexCatalogModel{{
		Slug:                  "deepseek/deepseek-v4.1-flash",
		DisplayName:           "DeepSeek V4.1 Flash",
		ContextWindow:         131072,
		ReasoningLevels:       []string{"low", "medium", "high"},
		DefaultReasoningLevel: "medium",
	}})
	if err != nil {
		t.Fatalf("CodexCatalogJSON: %v", err)
	}
	var doc struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(doc.Models) != 1 {
		t.Fatalf("models = %d, want 1", len(doc.Models))
	}
	model := doc.Models[0]
	if model["slug"] != "deepseek/deepseek-v4.1-flash" || model["display_name"] != "DeepSeek V4.1 Flash" {
		t.Errorf("model = %#v", model)
	}
	if model["default_reasoning_level"] != "medium" {
		t.Errorf("default_reasoning_level = %#v", model["default_reasoning_level"])
	}
	if model["context_window"] != float64(131072) || model["max_context_window"] != float64(131072) {
		t.Errorf("context = %#v / %#v", model["context_window"], model["max_context_window"])
	}
	instructions, _ := model["base_instructions"].(string)
	if len(instructions) < 1000 || !strings.Contains(instructions, "You are Codex") {
		t.Errorf("base_instructions looks empty or unexpected: %d chars", len(instructions))
	}
	levels, _ := model["supported_reasoning_levels"].([]any)
	if len(levels) != 3 {
		t.Fatalf("supported_reasoning_levels = %#v", model["supported_reasoning_levels"])
	}
	first, _ := levels[0].(map[string]any)
	if first["effort"] != "low" || first["description"] == "" {
		t.Errorf("first level = %#v", first)
	}
	for _, key := range []string{"shell_type", "visibility", "supported_in_api", "priority", "support_verbosity", "truncation_policy", "experimental_supported_tools"} {
		if _, ok := model[key]; !ok {
			t.Errorf("model is missing %q", key)
		}
	}
}

func TestCodexCatalogJSONWithoutReasoningLevels(t *testing.T) {
	data, err := CodexCatalogJSON([]CodexCatalogModel{{
		Slug:            "demo",
		ReasoningLevels: []string{},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	model := doc.Models[0]
	if _, ok := model["default_reasoning_level"]; ok {
		t.Error("default_reasoning_level should be absent without levels")
	}
	levels, ok := model["supported_reasoning_levels"].([]any)
	if !ok || len(levels) != 0 {
		t.Errorf("supported_reasoning_levels = %#v", model["supported_reasoning_levels"])
	}
}

func TestCodexCatalogRejectsUnknownReasoningLevel(t *testing.T) {
	_, err := CodexCatalogJSON([]CodexCatalogModel{{Slug: "demo", ReasoningLevels: []string{"turbo"}}})
	if err == nil || !strings.Contains(err.Error(), "unknown reasoning level") {
		t.Fatalf("err = %v", err)
	}
}

func TestCodexCatalogServerServesAndUpdates(t *testing.T) {
	server, err := NewCodexCatalogServer()
	if err != nil {
		t.Fatalf("NewCodexCatalogServer: %v", err)
	}
	defer func() { _ = server.Close() }()

	if !strings.HasPrefix(server.URL(), "http://127.0.0.1:") || !strings.HasSuffix(server.URL(), "/models") {
		t.Fatalf("catalog URL = %q", server.URL())
	}
	if err := server.SetModels([]CodexCatalogModel{{Slug: "first"}}); err != nil {
		t.Fatal(err)
	}
	if got := getCatalog(t, server.URL()+"?client_version=0.169.0"); !strings.Contains(got, `"slug":"first"`) {
		t.Errorf("catalog = %s", got)
	}
	if err := server.SetModels([]CodexCatalogModel{{Slug: "second"}}); err != nil {
		t.Fatal(err)
	}
	if got := getCatalog(t, server.URL()); !strings.Contains(got, `"slug":"second"`) {
		t.Errorf("catalog after update = %s", got)
	}

	response, err := http.Post(server.URL(), "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST status = %d, want 405", response.StatusCode)
	}
	response, err = http.Get(strings.TrimSuffix(server.URL(), "/models") + "/other")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown path status = %d, want 404", response.StatusCode)
	}
}

func getCatalog(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET catalog: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET catalog status = %d: %s", response.StatusCode, body)
	}
	return string(body)
}
