package payload

import (
	"encoding/json"
	"strings"
	"testing"
)

// OpenAI renamed the system role to developer for its reasoning models. A
// developer message used to reach neither the system prefix nor the skip list,
// so its instructions were lost on the path that sends only the latest turn.
func TestIsSystemRole(t *testing.T) {
	for _, c := range []struct {
		role string
		want bool
	}{
		{"system", true},
		{"developer", true},
		{"System", true},
		{" developer ", true},
		{"user", false},
		{"assistant", false},
		{"tool", false},
		{"", false},
	} {
		if got := IsSystemRole(c.role); got != c.want {
			t.Errorf("IsSystemRole(%q) = %v, want %v", c.role, got, c.want)
		}
	}
}

func instructionCarryingPayload(t *testing.T, role string) string {
	t.Helper()

	body, err := BuildConversationPayload(
		"sid", "uuid",
		[]Message{
			{Role: role, Content: "ALWAYS-ANSWER-BANANA"},
			{Role: "user", Content: "What is the capital of France?"},
		},
		false, "Magic", "", false, false, false, nil,
	)
	if err != nil {
		t.Fatalf("BuildConversationPayload: %v", err)
	}
	return body
}

func TestDeveloperInstructionsReachTheTurn(t *testing.T) {
	body := instructionCarryingPayload(t, "developer")

	if !strings.Contains(body, "ALWAYS-ANSWER-BANANA") {
		t.Error("a developer message did not reach the payload")
	}
	// The system prefix is what carries an instruction, because the backend
	// receives only the latest turn.
	system := instructionCarryingPayload(t, "system")
	if messageText(t, body) != messageText(t, system) {
		t.Errorf("developer turn text = %q, want the same as the system turn %q",
			messageText(t, body), messageText(t, system))
	}
}

// The flattened history labels each earlier turn by role. An instruction is not
// a turn, so it must not appear there for either name.
func TestDeveloperMessageStaysOutOfTheFlattenedHistory(t *testing.T) {
	messages := []Message{
		{Role: "developer", Content: "ALWAYS-ANSWER-BANANA"},
		{Role: "user", Content: "first question"},
		{Role: "assistant", Content: "first answer"},
		{Role: "user", Content: "second question"},
	}

	got := conversationTextForM365(messages, true)

	if strings.Contains(got, "DEVELOPER: ") {
		t.Errorf("the developer message was flattened as a conversation turn:\n%s", got)
	}
	if !strings.Contains(got, "USER: first question") {
		t.Errorf("the earlier turn is missing from the history:\n%s", got)
	}
}

// messageText reads the text the payload sends as the current turn.
func messageText(t *testing.T, body string) string {
	t.Helper()

	var decoded struct {
		Arguments []struct {
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
		} `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if len(decoded.Arguments) == 0 {
		t.Fatal("payload carries no arguments")
	}
	return decoded.Arguments[0].Message.Text
}
