package auth

import "testing"

// Bodies the token endpoint returned for each case, trimmed to the fields the
// decision reads.
const (
	expiredTokenBody = `{"error":"invalid_grant","error_description":"AADSTS700084: The refresh token was issued to a single page app (SPA), and therefore has a fixed, limited lifetime of 24:00:00.","error_codes":[700084]}`
	notATokenBody    = `{"error":"invalid_grant","error_description":"AADSTS9002313: Invalid request. Request is malformed or invalid.","error_codes":[9002313]}`
	throttledBody    = `{"error":"temporarily_unavailable","error_description":"AADSTS50196: The server terminated an operation because it encountered a loop.","error_codes":[50196]}`
	interactionBody  = `{"error":"interaction_required","error_description":"AADSTS50076: Due to a configuration change, you must use multi-factor authentication.","error_codes":[50076]}`
)

func TestRefreshTokenRejectedCoversEveryUnusableToken(t *testing.T) {
	// A placeholder in the setup file produced AADSTS9002313, and keying the
	// fallback on AADSTS700084 alone left that install reporting a hard
	// authentication failure while usable SSO cookies sat on disk.
	rejected := map[string]string{
		"expired token": expiredTokenBody,
		"not a token":   notATokenBody,
	}
	for name, body := range rejected {
		if !refreshTokenRejected(body) {
			t.Fatalf("%s: refreshTokenRejected = false, want true", name)
		}
	}

	kept := map[string]string{
		"throttled":            throttledBody,
		"interaction required": interactionBody,
	}
	for name, body := range kept {
		if refreshTokenRejected(body) {
			t.Fatalf("%s: refreshTokenRejected = true, want false; a cookie exchange does not fix it", name)
		}
	}
}

func TestAADSTSCodeNamesTheReason(t *testing.T) {
	cases := map[string]string{
		expiredTokenBody:            "AADSTS700084",
		notATokenBody:               "AADSTS9002313",
		throttledBody:               "AADSTS50196",
		`{"error":"invalid_grant"}`: "no AADSTS code",
	}
	for body, want := range cases {
		if got := aadstsCode(body); got != want {
			t.Fatalf("aadstsCode(%.40q) = %q, want %q", body, got, want)
		}
	}
}
