package servers

import "testing"

func TestFailureLimiterLimitsAfterTheThreshold(t *testing.T) {
	var limiter failureLimiter
	for i := 0; i < authFailureLimit; i++ {
		if limiter.limited("1.2.3.4") {
			t.Fatalf("limited before %d failures were recorded", i)
		}
		limiter.recordFailure("1.2.3.4")
	}
	if !limiter.limited("1.2.3.4") {
		t.Error("not limited after authFailureLimit failures")
	}
}

func TestFailureLimiterTracksKeysIndependently(t *testing.T) {
	var limiter failureLimiter
	for i := 0; i < authFailureLimit; i++ {
		limiter.recordFailure("1.2.3.4")
	}
	if limiter.limited("5.6.7.8") {
		t.Error("one key's failures locked out a different key")
	}
}

func TestFailureLimiterClearResetsTheCount(t *testing.T) {
	var limiter failureLimiter
	for i := 0; i < authFailureLimit; i++ {
		limiter.recordFailure("1.2.3.4")
	}
	limiter.clear("1.2.3.4")
	if limiter.limited("1.2.3.4") {
		t.Error("still limited after clear")
	}
}
