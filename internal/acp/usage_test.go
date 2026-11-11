package acp

import (
	"encoding/json"
	"testing"
)

func TestPromptResultTokens(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		ok    bool
		want  PromptUsage
	}{
		{
			name:  "absent",
			usage: "",
			ok:    false,
		},
		{
			name:  "null",
			usage: "null",
			ok:    false,
		},
		{
			name:  "malformed",
			usage: `"not an object"`,
			ok:    false,
		},
		{
			name:  "zero",
			usage: `{"totalTokens":0,"inputTokens":0,"outputTokens":0}`,
			ok:    false,
		},
		{
			name:  "tokens",
			usage: `{"totalTokens":1300,"inputTokens":1200,"outputTokens":100,"thoughtTokens":40,"cachedReadTokens":900}`,
			ok:    true,
			want:  PromptUsage{TotalTokens: 1300, InputTokens: 1200, OutputTokens: 100, ThoughtTokens: 40, CachedReadTokens: 900},
		},
		{
			name:  "total only",
			usage: `{"totalTokens":42}`,
			ok:    true,
			want:  PromptUsage{TotalTokens: 42},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := PromptResult{}
			if test.usage != "" {
				result.Usage = json.RawMessage(test.usage)
			}
			got, ok := result.Tokens()
			if ok != test.ok {
				t.Fatalf("ok = %t, want %t", ok, test.ok)
			}
			if got != test.want {
				t.Errorf("usage = %+v, want %+v", got, test.want)
			}
		})
	}
}
