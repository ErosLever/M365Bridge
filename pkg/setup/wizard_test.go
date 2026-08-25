package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSetupFile puts one setup document in a temporary directory.
func writeSetupFile(t *testing.T, refreshToken string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "setup.json")
	body, err := json.Marshal(map[string]any{
		"oid":           "00000000-0000-0000-0000-000000000001",
		"tenant":        "00000000-0000-0000-0000-000000000002",
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

func TestSetupFileAcceptsATokenSizedValue(t *testing.T) {
	token := "0.AR8A" + strings.Repeat("x", minRefreshTokenLength)

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
