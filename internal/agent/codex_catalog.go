package agent

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// codexBaseInstructions is the system prompt embedded in every generated
// catalog entry. Codex requires base_instructions (or a model_messages
// template) for each model; without it the whole catalog is rejected. The
// text is Codex's own coding prompt (Apache-2.0, from the Codex repository),
// so a custom provider still gets the agent behavior Codex expects.
//
//go:embed codex_prompt.txt
var codexBaseInstructions string

// DefaultCodexCatalogContextWindow is only used when a caller leaves the
// context window empty. Settings normally supplies the user's value.
const DefaultCodexCatalogContextWindow = 200000

// codexReasoningDescriptions keeps the labels Codex shows in its reasoning
// menu close to the built-in ones.
var codexReasoningDescriptions = map[string]string{
	"none":    "No extra reasoning",
	"minimal": "Minimal reasoning",
	"low":     "Fast responses with lighter reasoning",
	"medium":  "Balances speed and reasoning depth for everyday tasks",
	"high":    "Greater reasoning depth for complex problems",
	"xhigh":   "Extra high reasoning depth for complex problems",
	"max":     "Maximum reasoning depth for the hardest problems",
	"ultra":   "Maximum reasoning depth with extra time",
}

// CodexCatalogModel is one model advertised to Codex by the generated
// catalog. Codex only sends the slug to the provider; the rest is the
// metadata it needs for context tracking, compaction and reasoning effort.
type CodexCatalogModel struct {
	Slug                  string
	DisplayName           string
	Description           string
	ContextWindow         int
	MaxContextWindow      int
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

func (m CodexCatalogModel) normalized() (CodexCatalogModel, error) {
	m.Slug = strings.TrimSpace(m.Slug)
	if m.Slug == "" {
		return m, fmt.Errorf("codex: catalog model slug is empty")
	}
	m.DisplayName = strings.TrimSpace(m.DisplayName)
	if m.DisplayName == "" {
		m.DisplayName = m.Slug
	}
	m.Description = strings.TrimSpace(m.Description)
	if m.Description == "" {
		m.Description = "Model served through a custom provider."
	}
	if m.ContextWindow <= 0 {
		m.ContextWindow = DefaultCodexCatalogContextWindow
	}
	if m.MaxContextWindow < m.ContextWindow {
		m.MaxContextWindow = m.ContextWindow
	}
	levels := make([]string, 0, len(m.ReasoningLevels))
	seen := make(map[string]bool, len(m.ReasoningLevels))
	for _, level := range m.ReasoningLevels {
		level = strings.ToLower(strings.TrimSpace(level))
		if level == "" || seen[level] {
			continue
		}
		if _, ok := codexReasoningDescriptions[level]; !ok {
			return m, fmt.Errorf("codex: unknown reasoning level %q", level)
		}
		seen[level] = true
		levels = append(levels, level)
	}
	m.ReasoningLevels = levels
	if len(levels) == 0 {
		m.DefaultReasoningLevel = ""
		return m, nil
	}
	fallback := strings.ToLower(strings.TrimSpace(m.DefaultReasoningLevel))
	if !seen[fallback] {
		fallback = levels[0]
	}
	m.DefaultReasoningLevel = fallback
	return m, nil
}

// entry is the Codex ModelInfo object. Only the fields needed by Codex
// 0.169 are emitted; optional fields such as default_verbosity and
// effective_context_window_percent already have defaults.
func (m CodexCatalogModel) entry() map[string]any {
	levels := make([]map[string]string, 0, len(m.ReasoningLevels))
	for _, level := range m.ReasoningLevels {
		levels = append(levels, map[string]string{
			"effort":      level,
			"description": codexReasoningDescriptions[level],
		})
	}
	entry := map[string]any{
		"slug":                         m.Slug,
		"display_name":                 m.DisplayName,
		"description":                  m.Description,
		"shell_type":                   "shell_command",
		"visibility":                   "list",
		"supported_in_api":             true,
		"priority":                     0,
		"support_verbosity":            false,
		"truncation_policy":            map[string]any{"mode": "tokens", "limit": 10000},
		"experimental_supported_tools": []string{},
		"base_instructions":            strings.TrimSpace(codexBaseInstructions),
		"context_window":               m.ContextWindow,
		"max_context_window":           m.MaxContextWindow,
		"supported_reasoning_levels":   levels,
	}
	if m.DefaultReasoningLevel != "" {
		entry["default_reasoning_level"] = m.DefaultReasoningLevel
	}
	return entry
}

// CodexCatalogJSON builds the {"models":[...]} document Codex expects from
// model_catalog_url. Codex appends ?client_version=..., so the URL is just
// the document address.
func CodexCatalogJSON(models []CodexCatalogModel) ([]byte, error) {
	entries := make([]map[string]any, 0, len(models))
	for i, model := range models {
		normalized, err := model.normalized()
		if err != nil {
			return nil, fmt.Errorf("codex: catalog model %d: %w", i, err)
		}
		entries = append(entries, normalized.entry())
	}
	data, err := json.Marshal(map[string]any{"models": entries})
	if err != nil {
		return nil, fmt.Errorf("codex: encode model catalog: %w", err)
	}
	return data, nil
}

// CodexCatalogServer serves a generated Codex model catalog on loopback.
// codex-acp passes model_catalog_url to codex app-server, which fetches the
// document when the app-server starts, so the server must outlive sessions.
type CodexCatalogServer struct {
	listener net.Listener
	server   *http.Server

	mu   sync.RWMutex
	body []byte
}

// NewCodexCatalogServer listens on a random loopback port. Call SetModels
// before a Codex session starts; URL is stable for the app's lifetime.
func NewCodexCatalogServer() (*CodexCatalogServer, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("codex: listen for model catalog: %w", err)
	}
	s := &CodexCatalogServer{listener: listener}
	mux := http.NewServeMux()
	mux.HandleFunc("/models", s.handleModels)
	s.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		_ = s.server.Serve(listener)
	}()
	return s, nil
}

// URL is the model_catalog_url value to put in the provider definition.
func (s *CodexCatalogServer) URL() string {
	return "http://" + s.listener.Addr().String() + "/models"
}

// SetModels replaces the catalog document. Codex refetches it for every new
// app-server, so an update applies to new sessions without restarting the
// GUI.
func (s *CodexCatalogServer) SetModels(models []CodexCatalogModel) error {
	body, err := CodexCatalogJSON(models)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.body = body
	s.mu.Unlock()
	return nil
}

func (s *CodexCatalogServer) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	body := s.body
	s.mu.RUnlock()
	if len(body) == 0 {
		http.Error(w, "model catalog not ready", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// Close stops the listener. It is safe to call more than once.
func (s *CodexCatalogServer) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	return s.server.Close()
}
