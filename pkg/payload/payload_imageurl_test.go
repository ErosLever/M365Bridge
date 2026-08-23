package payload

import (
	"encoding/json"
	"testing"
)

// A client may send the url bare under image_url, the way the Responses
// input_image block carries it. Only the object form used to be read.
func TestImageURLAcceptsABareString(t *testing.T) {
	const remote = "https://example.com/cat.png"

	for _, content := range []string{
		`[{"type":"image_url","image_url":"` + remote + `"}]`,
		`[{"type":"image_url","image_url":{"url":"` + remote + `"}}]`,
		`[{"type":"input_image","image_url":"` + remote + `"}]`,
	} {
		var m Message
		if err := json.Unmarshal([]byte(`{"role":"user","content":`+content+`}`), &m); err != nil {
			t.Fatalf("content %s: %v", content, err)
		}
		if len(m.Images) != 1 {
			t.Errorf("content %s gave %d images, want 1", content, len(m.Images))
			continue
		}
		if m.Images[0].RemoteURL != remote {
			t.Errorf("content %s gave url %q", content, m.Images[0].RemoteURL)
		}
	}
}

// A block that names no image must add nothing, so a malformed request does not
// produce an upload of an empty attachment.
func TestImageURLIgnoresAValueThatNamesNoImage(t *testing.T) {
	for _, content := range []string{
		`[{"type":"image_url","image_url":""}]`,
		`[{"type":"image_url","image_url":"not-a-url"}]`,
		`[{"type":"image_url","image_url":7}]`,
		`[{"type":"image_url"}]`,
	} {
		var m Message
		if err := json.Unmarshal([]byte(`{"role":"user","content":`+content+`}`), &m); err != nil {
			t.Fatalf("content %s: %v", content, err)
		}
		if len(m.Images) != 0 {
			t.Errorf("content %s gave %d images, want none", content, len(m.Images))
		}
	}
}
