package servers

import (
	"sync"
	"time"
)

// authFailureLimit and authFailureWindow bound how many invalid credentials one
// remote address may offer to an authentication route before it is locked out.
// The comparisons behind these routes are already constant-time, so this
// closes the other lever a caller has: an unlimited number of guesses.
const (
	authFailureLimit  = 5
	authFailureWindow = time.Minute
)

type failureRecord struct {
	count int
	since time.Time
}

// failureLimiter counts recent failures per key, typically a remote address.
// The zero value is ready to use.
type failureLimiter struct {
	mu       sync.Mutex
	failures map[string]failureRecord
}

// limited reports whether key has already failed authFailureLimit times
// within the current window.
func (l *failureLimiter) limited(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	failure, exists := l.failures[key]
	if !exists || time.Since(failure.since) >= authFailureWindow {
		if exists {
			delete(l.failures, key)
		}
		return false
	}
	return failure.count >= authFailureLimit
}

// recordFailure counts one more failed attempt from key.
func (l *failureLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.failures == nil {
		l.failures = make(map[string]failureRecord)
	}
	failure := l.failures[key]
	if failure.since.IsZero() || time.Since(failure.since) >= authFailureWindow {
		failure = failureRecord{since: time.Now()}
	}
	failure.count++
	l.failures[key] = failure
}

// clear drops any recorded failures for key. Called after a credential
// succeeds, so a later mistake starts a fresh window instead of inheriting one
// from before the caller proved who it was.
func (l *failureLimiter) clear(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}
