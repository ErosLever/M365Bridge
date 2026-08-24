package client

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestUpstreamStatusSurvivesWrapping(t *testing.T) {
	// The HTTP layer sees the error several frames above the dial, so the
	// status has to be readable through whatever wrapped it on the way up.
	wrapped := fmt.Errorf("chat failed: %w", &UpstreamError{
		Op:     "dial",
		Status: 429,
		Err:    errors.New("bad handshake"),
	})

	status, ok := UpstreamStatus(wrapped)
	if !ok {
		t.Fatal("a wrapped upstream error reported no status")
	}
	if status != 429 {
		t.Fatalf("status = %d, want 429", status)
	}
}

func TestUpstreamStatusRejectsAnUnrelatedError(t *testing.T) {
	// A plain error must not be read as "status 0", which would otherwise be
	// indistinguishable from a dial that never reached a response.
	if _, ok := UpstreamStatus(errors.New("context canceled")); ok {
		t.Fatal("an unrelated error reported an upstream status")
	}
}

func TestUpstreamStatusReportsAResponselessDial(t *testing.T) {
	status, ok := UpstreamStatus(&UpstreamError{Op: "dial", Err: errors.New("no route to host")})
	if !ok {
		t.Fatal("a responseless dial reported no upstream status")
	}
	if status != 0 {
		t.Fatalf("status = %d, want 0", status)
	}
}

func TestUpstreamErrorUnwrapsToItsCause(t *testing.T) {
	cause := errors.New("connection reset by peer")
	if !errors.Is(&UpstreamError{Op: "upload", Status: 502, Err: cause}, cause) {
		t.Fatal("the upstream error hid its cause from errors.Is")
	}
}

func TestUpstreamErrorMessageNamesTheOperationAndStatus(t *testing.T) {
	// This text goes to the server log only, but it has to be usable there.
	got := (&UpstreamError{Op: "upload", Status: 403, Err: errors.New("denied")}).Error()
	for _, want := range []string{"upload", "403", "Forbidden", "denied"} {
		if !strings.Contains(got, want) {
			t.Fatalf("error message %q lost %q", got, want)
		}
	}

	got = (&UpstreamError{Op: "dial", Err: errors.New("timeout")}).Error()
	if strings.Contains(got, "status") {
		t.Fatalf("a responseless dial invented a status: %q", got)
	}
}
