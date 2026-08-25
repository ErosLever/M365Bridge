package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// completionItem decodes a real type 2 frame's item, so the tests read the same
// shape the backend sends rather than a hand-built map.
func completionItem(t *testing.T, raw string) map[string]any {
	t.Helper()
	var item map[string]any
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return item
}

func TestParseTurnResultAcceptsASuccessfulTurn(t *testing.T) {
	item := completionItem(t, `{"conversationId":"c1","turnState":"Completed",
		"result":{"value":"Success","message":"OK.","serviceVersion":"1.0.03518.48495"}}`)
	if failure := parseTurnResult(item); failure != nil {
		t.Fatalf("a successful turn was reported as failed: %v", failure)
	}
}

// The observed failure: a tone the backend accepts but no longer serves ends
// the turn with InternalError and sends no answer message.
func TestParseTurnResultReportsAFailedTurn(t *testing.T) {
	item := completionItem(t, `{"conversationId":"c1","turnState":"Failed",
		"result":{"value":"InternalError","message":"Sorry, I wasn't able to respond to that."}}`)

	failure := parseTurnResult(item)
	if failure == nil {
		t.Fatal("a failed turn was reported as successful")
	}
	if failure.Value != "InternalError" {
		t.Errorf("Value = %q", failure.Value)
	}
	if failure.TurnState != "Failed" {
		t.Errorf("TurnState = %q", failure.TurnState)
	}
	if failure.Message != "Sorry, I wasn't able to respond to that." {
		t.Errorf("Message = %q", failure.Message)
	}
}

// A frame shape this package has not seen must not turn a working turn into an
// error, so an absent result is not a failure.
func TestParseTurnResultIgnoresAnItemWithoutAResult(t *testing.T) {
	item := completionItem(t, `{"conversationId":"c1","turnState":"Completed"}`)
	if failure := parseTurnResult(item); failure != nil {
		t.Fatalf("an item without a result was reported as failed: %v", failure)
	}
}

func TestParseTurnResultIgnoresAResultWithoutAValue(t *testing.T) {
	item := completionItem(t, `{"conversationId":"c1","result":{"message":"something"}}`)
	if failure := parseTurnResult(item); failure != nil {
		t.Fatalf("a result without a value was reported as failed: %v", failure)
	}
}

func TestTurnFailedErrorNamesTheVerdict(t *testing.T) {
	err := &TurnFailedError{Value: "InternalError", TurnState: "Failed", Message: "Sorry."}
	got := err.Error()
	for _, want := range []string{"InternalError", "Failed", "Sorry."} {
		if !strings.Contains(got, want) {
			t.Errorf("Error() = %q, missing %q", got, want)
		}
	}
}

func TestTurnFailureSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("chat: %w", &TurnFailedError{Value: "InternalError"})
	failure, ok := TurnFailure(wrapped)
	if !ok {
		t.Fatal("TurnFailure missed a wrapped turn failure")
	}
	if failure.Value != "InternalError" {
		t.Fatalf("Value = %q", failure.Value)
	}
}

func TestTurnFailureRejectsAnUnrelatedError(t *testing.T) {
	if _, ok := TurnFailure(errors.New("connection reset")); ok {
		t.Fatal("TurnFailure claimed an unrelated error")
	}
}

// An empty turn is the second way a tone fails: no answer, no tool call and no
// verdict frame. The check must not fire on a turn that produced either one,
// because that would turn a working answer into an error.
func TestEmptyTurnDetectsATurnWithNothingToReturn(t *testing.T) {
	var empty answerAccumulator
	if !empty.emptyTurn(nil) {
		t.Error("a turn with no text and no tool call was reported as usable")
	}

	var answered answerAccumulator
	answered.appendAnswer("an answer")
	if answered.emptyTurn(nil) {
		t.Error("a turn that produced text was reported as empty")
	}

	var called answerAccumulator
	if called.emptyTurn([]ToolCall{{Function: ToolCallFunction{Name: "search"}}}) {
		t.Error("a turn that produced a tool call was reported as empty")
	}

	// A turn that answered with an image and no words still produced something.
	var pictured answerAccumulator
	pictured.inject(len("\n\n![image](https://example.test/a.png)\n\n"))
	if pictured.emptyTurn(nil) {
		t.Error("a turn that produced only an image was reported as empty")
	}
}
