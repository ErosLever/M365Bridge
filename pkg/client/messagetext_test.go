package client

import (
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func TestCarriesAnswerTextAcceptsAPlainAnswer(t *testing.T) {
	msg := map[string]any{"contentOrigin": "DeepLeo", "text": "the answer"}
	if !carriesAnswerText(msg) {
		t.Fatal("a message with no messageType was rejected; that shape is the assistant answer")
	}
}

func TestCarriesAnswerTextRejectsProgress(t *testing.T) {
	msg := map[string]any{"messageType": "Progress", "text": "Searching..."}
	if carriesAnswerText(msg) {
		t.Fatal("a Progress message was accepted as answer text")
	}
}

// Every name in ToolMessageType is a call the backend raises for its own
// built-ins. Ranging over the map keeps the guard in step with it: a type added
// there is covered here without editing this test.
func TestCarriesAnswerTextRejectsEveryBackendToolMessage(t *testing.T) {
	if len(models.ToolMessageType) == 0 {
		t.Fatal("ToolMessageType is empty, so this test proves nothing")
	}
	for messageType := range models.ToolMessageType {
		msg := map[string]any{"messageType": messageType, "text": "backend internals"}
		if carriesAnswerText(msg) {
			t.Errorf("messageType %q was accepted as answer text", messageType)
		}
	}
}

// The guard excludes known backend types instead of admitting only the empty
// messageType. Losing answer text is the worse failure, so an unfamiliar type
// has to pass through.
func TestCarriesAnswerTextAcceptsAnUnknownMessageType(t *testing.T) {
	msg := map[string]any{"messageType": "SomeFutureType", "text": "possibly the answer"}
	if !carriesAnswerText(msg) {
		t.Fatal("an unknown messageType was dropped; the guard must exclude only known backend types")
	}
}

func TestCarriesAnswerTextAcceptsANonStringMessageType(t *testing.T) {
	msg := map[string]any{"messageType": 42, "text": "the answer"}
	if !carriesAnswerText(msg) {
		t.Fatal("a non-string messageType was dropped rather than treated as absent")
	}
}
