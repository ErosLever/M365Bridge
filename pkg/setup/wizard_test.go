package setup

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSetupFile puts one setup document in a temporary directory.
func writeSetupFile(t *testing.T, refreshToken string) string {
	t.Helper()
	return writeSetupFileWith(t,
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000001",
		refreshToken)
}

// writeSetupFileWith puts one setup document carrying the given identity in a
// temporary directory.
func writeSetupFileWith(t *testing.T, tenant, oid, refreshToken string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.json")
	body, err := json.Marshal(map[string]any{
		"oid":           oid,
		"tenant":        tenant,
		"refresh_token": refreshToken,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// realToken is a value long enough to pass the refresh-token length check, so a
// test about the identity fields fails on the identity and nothing else.
var realToken = "0.AR8A" + strings.Repeat("x", minRefreshTokenLength)

func TestSetupFileRefusesAPlaceholderRefreshToken(t *testing.T) {
	// The example text left in place of a real token was encrypted and stored,
	// and the token endpoint then answered "request is malformed", which reads
	// as a broken install rather than as a value nobody replaced.
	placeholders := map[string]string{
		"turkish example": "sizin-refresh-token-degeriniz",
		"english example": "your-refresh-token-here",
		"short paste":     "0.AAAA",
	}
	for name, token := range placeholders {
		_, _, _, _, err := getConfigFromFile(writeSetupFile(t, token))
		if err == nil {
			t.Fatalf("%s: getConfigFromFile accepted a %d-character token", name, len(token))
		}
		if !strings.Contains(err.Error(), "too short to be a real token") {
			t.Fatalf("%s: error %q does not name the cause", name, err)
		}
	}
}

// The measured failure. A setup file whose tenant read
// 22222222-2222-2222-2222-222222222222 was accepted, written to data/.env, and
// only reported hours later, as an authentication failure on a chat request.
func TestSetupFileRefusesAFillerIdentity(t *testing.T) {
	cases := map[string]struct{ tenant, oid, field, want string }{
		"filler tenant": {
			tenant: "22222222-2222-2222-2222-222222222222",
			oid:    "d6caea73-4091-4392-bcc5-e378e9eb57ea",
			field:  "tenant",
			want:   "filler text",
		},
		"filler oid": {
			tenant: "95d53073-c502-4f19-8ce9-6853f989b388",
			oid:    "00000000-0000-0000-0000-000000000000",
			field:  "oid",
			want:   "filler text",
		},
		"tenant is not a GUID": {
			tenant: "sizin-tenant-degeriniz",
			oid:    "d6caea73-4091-4392-bcc5-e378e9eb57ea",
			field:  "tenant",
			want:   "not a GUID",
		},
		"oid is not a GUID": {
			tenant: "95d53073-c502-4f19-8ce9-6853f989b388",
			oid:    "your-oid",
			field:  "oid",
			want:   "not a GUID",
		},
	}
	for name, tc := range cases {
		_, _, _, _, err := getConfigFromFile(writeSetupFileWith(t, tc.tenant, tc.oid, realToken))
		if err == nil {
			t.Fatalf("%s: getConfigFromFile accepted it", name)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: error %q does not name the cause", name, err)
		}
		if !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("%s: error %q does not name the field", name, err)
		}
	}
}

func TestSetupFileAcceptsARealIdentity(t *testing.T) {
	path := writeSetupFileWith(t,
		"95d53073-c502-4f19-8ce9-6853f989b388",
		"d6caea73-4091-4392-bcc5-e378e9eb57ea",
		realToken)

	if _, _, _, _, err := getConfigFromFile(path); err != nil {
		t.Fatalf("a real identity was refused: %v", err)
	}
}

func TestSetupFileAcceptsATokenSizedValue(t *testing.T) {
	token := realToken

	tenant, oid, parsed, _, err := getConfigFromFile(writeSetupFile(t, token))
	if err != nil {
		t.Fatalf("getConfigFromFile: %v", err)
	}
	if parsed != token {
		t.Fatalf("refresh token changed: got %d chars, want %d", len(parsed), len(token))
	}
	if tenant == "" || oid == "" {
		t.Fatalf("tenant %q or oid %q was lost", tenant, oid)
	}
}

// buildJWT assembles a token in the three-part form Microsoft returns, so the
// claim reader is driven by the shape it meets in production.
func buildJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT","alg":"RS256"}`))
	return header + "." + base64.RawURLEncoding.EncodeToString(body) + ".signature"
}

// The identity written to data/.env comes from here, not from the setup file,
// because the oid never takes part in the token exchange and no other check in
// the wizard can tell a wrong one from a right one.
func TestAccessTokenClaimsReadTheIdentityTheTokenWasIssuedFor(t *testing.T) {
	const wantTID = "95d53073-c502-4f19-8ce9-6853f989b388"
	const wantOID = "d6caea73-4091-4392-bcc5-e378e9eb57ea"
	token := buildJWT(t, map[string]any{
		"tid": wantTID,
		"oid": wantOID,
		"aud": "https://substrate.office.com",
	})

	tid, oid, err := accessTokenClaims(token)
	if err != nil {
		t.Fatalf("accessTokenClaims: %v", err)
	}
	if tid != wantTID {
		t.Errorf("tid = %q, want %q", tid, wantTID)
	}
	if oid != wantOID {
		t.Errorf("oid = %q, want %q", oid, wantOID)
	}
}

func TestAccessTokenClaimsRefuseATokenItCannotRead(t *testing.T) {
	cases := map[string]string{
		"not a JWT":        "an-opaque-token",
		"body is not JSON": "aGVhZGVy.bm90LWpzb24.signature",
		"no identity":      buildJWT(t, map[string]any{"aud": "https://substrate.office.com"}),
		"tid without oid":  buildJWT(t, map[string]any{"tid": "95d53073-c502-4f19-8ce9-6853f989b388"}),
	}
	for name, token := range cases {
		if _, _, err := accessTokenClaims(token); err == nil {
			t.Errorf("%s: accessTokenClaims returned an identity", name)
		}
	}
}
