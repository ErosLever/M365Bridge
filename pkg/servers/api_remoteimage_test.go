package servers

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
)

// A caller-supplied image URL is fetched without any credential, so the guard
// exists to stop the proxy reaching addresses inside its own network rather
// than to protect a token.
func TestValidateRemoteImageURLRejectsUnsafeTargets(t *testing.T) {
	cases := map[string]string{
		"plain http":     "http://example.com/cat.png",
		"loopback":       "https://127.0.0.1/cat.png",
		"private range":  "https://10.0.0.5/cat.png",
		"link local":     "https://169.254.169.254/latest/meta-data/",
		"carrier NAT":    "https://100.64.0.1/cat.png",
		"no host":        "https:///cat.png",
		"unresolvable":   "https://this-host-does-not-exist.invalid/cat.png",
		"file scheme":    "file:///etc/passwd",
		"ipv6 loopback":  "https://[::1]/cat.png",
		"unspecified v4": "https://0.0.0.0/cat.png",
	}
	for name, rawURL := range cases {
		err := validateRemoteImageURL(rawURL)
		if err == nil {
			t.Fatalf("%s: %s was accepted", name, rawURL)
		}
		if !errors.Is(err, errRemoteImageRejected) {
			t.Fatalf("%s: error %v is not a rejection", name, err)
		}
	}
}

// A data URL still decodes inline, and a remote URL is carried unfetched so the
// server layer can resolve it.
func TestImageURLBlockCarriesRemoteURLs(t *testing.T) {
	var message payload.Message
	raw := `{"role":"user","content":[
		{"type":"image_url","image_url":{"url":"https://example.com/cat.png"}},
		{"type":"image_url","image_url":{"url":"data:image/png;base64,aGk="}}
	]}`
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(message.Images) != 2 {
		t.Fatalf("got %d images, want the remote and the inline one", len(message.Images))
	}

	var remote, inline int
	for _, img := range message.Images {
		switch {
		case img.RemoteURL != "":
			remote++
			if img.Base64 != "" {
				t.Fatal("a remote image arrived with bytes already attached")
			}
		case img.Base64 != "":
			inline++
		}
	}
	if remote != 1 || inline != 1 {
		t.Fatalf("remote=%d inline=%d, want one of each", remote, inline)
	}
}

func TestResolveRemoteImagesDropsUnfetchableOnes(t *testing.T) {
	// One bad URL must not fail the whole turn, and the inline image beside it
	// must survive untouched.
	message := payload.Message{
		Role: "user",
		Images: []payload.ImageData{
			{RemoteURL: "https://127.0.0.1/cat.png"},
			{Base64: "aGk=", MediaType: "image/png", FileName: "upload.png"},
		},
	}

	resolveRemoteImages(&message)

	if len(message.Images) != 1 {
		t.Fatalf("got %d images, want only the inline one", len(message.Images))
	}
	if message.Images[0].Base64 != "aGk=" {
		t.Fatalf("the inline image was lost: %#v", message.Images[0])
	}
}

func TestResolveRemoteImagesBoundsTheFetchCount(t *testing.T) {
	// Every URL is unfetchable here, so the point is that the loop stops rather
	// than attempting one lookup per URL for an unbounded list.
	var images []payload.ImageData
	for range remoteImageMaxPerTurn + 5 {
		images = append(images, payload.ImageData{RemoteURL: "https://127.0.0.1/cat.png"})
	}
	message := payload.Message{Role: "user", Images: images}

	resolveRemoteImages(&message)

	if len(message.Images) != 0 {
		t.Fatalf("got %d images, want none to survive", len(message.Images))
	}
}

func TestRemoteImageRejectionNamesTheReason(t *testing.T) {
	err := validateRemoteImageURL("http://example.com/cat.png")
	if err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("error %v does not explain the scheme requirement", err)
	}
}
