package servers

import "testing"

// The parser enforces tool_choice by comparing the call name against this
// value, so an Anthropic pinned choice must resolve to the tool's name rather
// than to the literal type "tool".
func TestAnthropicToolChoiceEnforcement(t *testing.T) {
	cases := []struct {
		name   string
		choice map[string]any
		want   string
	}{
		{"absent", nil, ""},
		{"auto", map[string]any{"type": "auto"}, "auto"},
		{"any", map[string]any{"type": "any"}, "any"},
		{"none", map[string]any{"type": "none"}, "none"},
		{"pinned", map[string]any{"type": "tool", "name": "get_weather"}, "get_weather"},
		{"pinned without name", map[string]any{"type": "tool"}, ""},
	}
	for _, c := range cases {
		if got := anthropicToolChoiceEnforcement(c.choice); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// The OpenAI shape carries the pinned name one level deeper, under "function".
func TestToolChoiceStringResolvesPinnedFunction(t *testing.T) {
	cases := []struct {
		name   string
		choice any
		want   string
	}{
		{"absent", nil, ""},
		{"auto", "auto", "auto"},
		{"none", "none", "none"},
		{"required", "required", "required"},
		{
			"pinned",
			map[string]any{"type": "function", "function": map[string]any{"name": "send_email"}},
			"send_email",
		},
	}
	for _, c := range cases {
		if got := toolChoiceString(c.choice); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
