package servers

import (
	"strings"
	"testing"
)

const testPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="

// The Responses path uploads whatever a message carries in Images, but the
// converter used to flatten content to text, so an image reached the upload
// step as nothing at all.
func TestResponsesInputCarriesAnInlineImage(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "what is in this image?"},
				map[string]any{"type": "input_image", "image_url": testPNGDataURL},
			},
		},
	})

	if len(messages) != 1 {
		t.Fatalf("messages = %d, want one user message", len(messages))
	}
	if len(messages[0].Images) != 1 {
		t.Fatalf("images = %#v, want the inline image", messages[0].Images)
	}
	if messages[0].Images[0].Base64 == "" {
		t.Fatalf("image carries no data: %#v", messages[0].Images[0])
	}
	if messages[0].Images[0].MediaType != "image/png" {
		t.Fatalf("media type = %q", messages[0].Images[0].MediaType)
	}
	if !strings.Contains(messages[0].Content, "what is in this image?") {
		t.Fatalf("text was lost: %q", messages[0].Content)
	}
}

// A remote url is resolved later by the server layer, which enforces the host
// allowlist; the converter only has to keep it.
func TestResponsesInputCarriesARemoteImageURL(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_image", "image_url": "https://example.com/cat.png"},
			},
		},
	})

	if len(messages) != 1 || len(messages[0].Images) != 1 {
		t.Fatalf("messages = %#v, want one image", messages)
	}
	if messages[0].Images[0].RemoteURL != "https://example.com/cat.png" {
		t.Fatalf("remote url = %q", messages[0].Images[0].RemoteURL)
	}
}

// An image-only turn carries work, so the probe path must not answer it
// without reaching M365.
func TestResponsesImageOnlyInputIsNotAProbe(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_image", "image_url": testPNGDataURL},
			},
		},
	})

	if responsesInputIsEmpty(messages) {
		t.Fatal("an image-only input was read as an empty probe")
	}
}

func TestResponsesInputKeepsPlainStringContent(t *testing.T) {
	messages := responsesInputToMessages([]any{
		map[string]any{"role": "user", "content": "plain text"},
	})

	if len(messages) != 1 || messages[0].Content != "plain text" || messages[0].Role != "user" {
		t.Fatalf("messages = %#v", messages)
	}
}
