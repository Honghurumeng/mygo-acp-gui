package session

import (
	"context"
	"testing"
	"time"

	"github.com/reed/acp-gui/internal/acp"
)

func levelsOf(option *acp.ConfigOption) []string {
	if option == nil {
		return nil
	}
	out := make([]string, 0, len(option.Options))
	for _, choice := range option.Options {
		out = append(out, choice.Value)
	}
	return out
}

func TestProbeConfigModelsSweepsOnlyUnknownModels(t *testing.T) {
	launch, _ := fakeLaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	reported := make(map[string][]acp.ConfigOption)
	order := make([]string, 0, 3)
	info, err := ProbeConfigModels(ctx, launch, ModelProbe{
		Known: map[string]bool{"demo/small": true},
		Ready: func(state Info) {
			order = append(order, "ready")
			if got := state.ConfigOption("model"); got == nil || got.CurrentValue != "demo/small" {
				t.Errorf("ready model = %+v", got)
			}
		},
		Report: func(model string, options []acp.ConfigOption) {
			order = append(order, model)
			reported[model] = options
		},
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// The returned Info still describes the session/new model, not the last
	// model the sweep switched to.
	if got := info.ConfigOption("thought_level"); got == nil || got.CurrentValue != "medium" {
		t.Errorf("session/new thinking = %+v", got)
	}
	if len(reported) != 2 {
		t.Fatalf("swept models = %v, want demo/large and demo/reasoning", keys(reported))
	}
	if len(order) != 3 || order[0] != "ready" {
		t.Errorf("callbacks = %v, want ready before the sweeps", order)
	}
	large := acp.ThinkingOption(reported["demo/large"])
	if got := levelsOf(large); len(got) != 6 || got[5] != "xhigh" {
		t.Errorf("demo/large levels = %v", got)
	}
	reasoning := acp.ThinkingOption(reported["demo/reasoning"])
	if got := levelsOf(reasoning); len(got) != 3 || got[2] != "xhigh" {
		t.Errorf("demo/reasoning levels = %v", got)
	}
	if model := acp.ModelOption(reported["demo/reasoning"]); model == nil || model.CurrentValue != "demo/reasoning" {
		t.Errorf("reported model option = %+v", model)
	}
}

func TestProbeConfigModelsStopsWhenCacheIsComplete(t *testing.T) {
	launch, _ := fakeLaunch(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	swept := 0
	info, err := ProbeConfigModels(ctx, launch, ModelProbe{
		Known: map[string]bool{"demo/small": true, "demo/large": true, "demo/reasoning": true},
		Report: func(string, []acp.ConfigOption) {
			swept++
		},
	})
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if swept != 0 {
		t.Errorf("swept %d models with a complete cache", swept)
	}
	if got := info.ConfigOption("model"); got == nil || got.CurrentValue != "demo/small" {
		t.Errorf("model = %+v", got)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
