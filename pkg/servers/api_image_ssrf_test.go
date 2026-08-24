package servers

import (
	"errors"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

func newImageTestServer() *APIServer {
	return &APIServer{config: &models.Config{
		ImageHostAllowlist: models.DefaultImageHostAllowlist,
	}}
}

func TestHostAllowlistMatching(t *testing.T) {
	allowlist := []string{".officeapps.live.com", "images.example.net"}
	cases := []struct {
		host string
		want bool
	}{
		{"designerappservice.officeapps.live.com", true},
		{"officeapps.live.com", true},
		{"DESIGNERAPPSERVICE.OFFICEAPPS.LIVE.COM", true},
		{"designerappservice.officeapps.live.com.", true},
		{"images.example.net", true},
		// A suffix that only looks like the allowed domain must not match.
		{"evil-officeapps.live.com", false},
		{"officeapps.live.com.attacker.test", false},
		{"attacker.test", false},
		{"sub.images.example.net", false},
	}
	for _, c := range cases {
		if got := hostAllowed(c.host, allowlist); got != c.want {
			t.Fatalf("hostAllowed(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestValidateImageDownloadURLRejectsUntrustedTargets(t *testing.T) {
	api := newImageTestServer()
	cases := []struct {
		name string
		url  string
	}{
		// The download carries the designerapp token, so a foreign host would
		// receive that credential.
		{"foreign host", "https://attacker.example/leak.png"},
		{"plain http", "http://designerappservice.officeapps.live.com/a.png"},
		{"no host", "https:///a.png"},
		{"loopback", "https://localhost/a.png"},
		{"cloud metadata", "https://169.254.169.254/latest/meta-data"},
	}
	for _, c := range cases {
		err := api.validateImageDownloadURL(c.url)
		if err == nil {
			t.Fatalf("%s: validateImageDownloadURL(%q) = nil, want rejection", c.name, c.url)
		}
		if !errors.Is(err, errImageHostNotAllowed) {
			t.Fatalf("%s: error %v does not wrap errImageHostNotAllowed", c.name, err)
		}
	}
}

func TestValidateImageDownloadURLAcceptsAllowlistedHost(t *testing.T) {
	api := newImageTestServer()
	err := api.validateImageDownloadURL("https://designerappservice.officeapps.live.com/i.png?fileToken=x")
	// The host must pass the allowlist. DNS may be unavailable in a sandboxed
	// test environment, so only a resolution failure is tolerated here.
	if err != nil && !strings.Contains(err.Error(), "does not resolve") {
		t.Fatalf("allowlisted host rejected: %v", err)
	}
}

func TestBuildOpenAIImageDataDropsDisallowedHosts(t *testing.T) {
	api := newImageTestServer()
	text := "![a](https://attacker.example/leak.png)\n![b](https://evil-officeapps.live.com/x.png)"

	items := api.buildOpenAIImageData(text, 0, "prompt", "b64_json")
	if len(items) != 0 {
		t.Fatalf("got %d items, want 0; a disallowed URL must not reach the client: %#v", len(items), items)
	}
}

func TestImagePatternIgnoresPlainHTTP(t *testing.T) {
	// http URLs are not downloadable targets, so they must not even be
	// collected as candidates.
	if urlImagePattern.MatchString("![a](http://attacker.example/x.png)") {
		t.Fatal("urlImagePattern matched an http URL")
	}
	if !urlImagePattern.MatchString("![a](https://designerappservice.officeapps.live.com/x.png)") {
		t.Fatal("urlImagePattern did not match an https URL")
	}
}
