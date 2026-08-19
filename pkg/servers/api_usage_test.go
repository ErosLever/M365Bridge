package servers

import (
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
)

func TestTokenEncodingPrefersO200kBase(t *testing.T) {
	// The backend serves GPT-5 class models, whose encoding is o200k_base.
	// cl100k_base only stands in when the vocabulary cannot be fetched.
	if tokenEncoder == nil {
		t.Skip("no token encoding available in this environment")
	}
	if tokenEncodingName != "o200k_base" {
		t.Fatalf("encoding = %q, want o200k_base", tokenEncodingName)
	}
	if got := usageSource(); got != "tiktoken_o200k_base_estimate" {
		t.Fatalf("usage source = %q, want the o200k source", got)
	}
}

func TestHeuristicTokenCountSeparatesScripts(t *testing.T) {
	if got := heuristicTokenCount(""); got != 0 {
		t.Fatalf("empty text counted %d tokens", got)
	}
	if got := heuristicTokenCount("   \n\t "); got != 0 {
		t.Fatalf("whitespace counted %d tokens", got)
	}
	// Latin text averages about four characters per token, CJK about one, so
	// the same character count must not produce the same estimate.
	latin := heuristicTokenCount(strings.Repeat("a", 40))
	cjk := heuristicTokenCount(strings.Repeat("字", 40))
	if cjk <= latin {
		t.Fatalf("cjk=%d latin=%d, want the non-ASCII estimate to be higher", cjk, latin)
	}
}

func TestCountPromptTokensCountsMessagesAndTools(t *testing.T) {
	messages := []payload.Message{
		{Role: "user", Content: "what is the weather in Ankara?"},
	}
	tools := []toolcalling.ToolDef{
		{Type: "function", Function: toolcalling.ToolDefFunc{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
		}},
	}

	bare := countPromptTokens(messages, nil, "")
	withTools := countPromptTokens(messages, tools, "")
	withChoice := countPromptTokens(messages, tools, "required")

	if bare <= requestProtocolTokens+replyPrimingTokens {
		t.Fatalf("bare prompt = %d, want the message content counted too", bare)
	}
	if withTools <= bare {
		t.Fatalf("withTools=%d bare=%d, want the tool schema counted", withTools, bare)
	}
	if withChoice != withTools+toolChoiceProtocolTokens {
		t.Fatalf("withChoice=%d, want %d", withChoice, withTools+toolChoiceProtocolTokens)
	}

	twoMessages := countPromptTokens(append(messages, payload.Message{Role: "assistant", Content: "checking"}), nil, "")
	if twoMessages <= bare {
		t.Fatalf("twoMessages=%d bare=%d, want the extra message counted", twoMessages, bare)
	}
}

func TestCountPromptTokensIgnoresGoStructFormatting(t *testing.T) {
	// The old count ran the encoder over fmt.Sprint of the message slice, so
	// Go field names and slice punctuation were billed as prompt content.
	messages := []payload.Message{{Role: "user", Content: "hi"}}
	if got := countPromptTokens(messages, nil, ""); got > 20 {
		t.Fatalf("a two-character prompt counted %d tokens, want only its framing", got)
	}
}
