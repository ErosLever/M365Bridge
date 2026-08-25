package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
)

// allowlistedImageHost is an address literal from the documentation range. The
// validator resolves every host it accepts, and a literal resolves without a
// DNS server, so the accepted path stays measurable with no network.
const (
	allowlistedImageHost = "203.0.113.10"
	allowlistedImageURL  = "https://" + allowlistedImageHost + "/i.png?fileToken=x"
)

func newGeneratedImageServer() *APIServer {
	return &APIServer{
		config:    &models.Config{ImageHostAllowlist: []string{allowlistedImageHost}},
		imageRefs: newImageRefStore(),
	}
}

func TestRouteGeneratedImagesRewritesAllowlistedURL(t *testing.T) {
	api := newGeneratedImageServer()

	out := api.routeGeneratedImages("before\n\n![image](" + allowlistedImageURL + ")\n\nafter")

	if strings.Contains(out, allowlistedImageHost) {
		t.Fatalf("the upstream address survived the rewrite: %q", out)
	}
	if !strings.Contains(out, "![image]("+generatedImagePrefix) {
		t.Fatalf("no gateway route in %q", out)
	}
	if !strings.Contains(out, "before") || !strings.Contains(out, "after") {
		t.Fatalf("surrounding text was lost: %q", out)
	}

	id := strings.TrimSuffix(strings.SplitN(out, generatedImagePrefix, 2)[1], ")\n\nafter")
	url, ok := api.imageRefs.get(id)
	if !ok {
		t.Fatalf("reference %q does not resolve", id)
	}
	if url != allowlistedImageURL {
		t.Fatalf("reference resolves to %q, want %q", url, allowlistedImageURL)
	}
}

func TestRouteGeneratedImagesDropsDisallowedHost(t *testing.T) {
	// Handing a model-controlled address back to the client makes the client
	// fetch it, which is why a refused address is removed rather than kept.
	api := newGeneratedImageServer()

	out := api.routeGeneratedImages("text ![image](https://attacker.example/leak.png) end")

	if strings.Contains(out, "attacker.example") {
		t.Fatalf("a disallowed address reached the client: %q", out)
	}
	if !strings.Contains(out, "text") || !strings.Contains(out, "end") {
		t.Fatalf("surrounding text was lost: %q", out)
	}
}

func TestRouteGeneratedImagesLeavesPlainTextAlone(t *testing.T) {
	api := newGeneratedImageServer()
	const answer = "A plain answer with a [link](https://example.com/page) in it."

	if out := api.routeGeneratedImages(answer); out != answer {
		t.Fatalf("plain text changed: %q", out)
	}
	if out := api.routeGeneratedImages(""); out != "" {
		t.Fatalf("empty text changed: %q", out)
	}
}

func TestImageRefStoreExpiresEntry(t *testing.T) {
	store := newImageRefStore()
	now := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { return now }

	id := store.put(allowlistedImageURL)
	if _, ok := store.get(id); !ok {
		t.Fatal("a fresh reference does not resolve")
	}

	now = now.Add(generatedImageTTL)
	if _, ok := store.get(id); ok {
		t.Fatal("an expired reference still resolves")
	}
}

func TestImageRefStoreEvictsOldest(t *testing.T) {
	store := newImageRefStore()
	first := store.put(allowlistedImageURL)
	for range generatedImageMaxRefs {
		store.put(allowlistedImageURL)
	}

	if _, ok := store.get(first); ok {
		t.Fatal("the oldest reference survived the cap")
	}
	if len(store.entries) != generatedImageMaxRefs {
		t.Fatalf("store holds %d entries, want %d", len(store.entries), generatedImageMaxRefs)
	}
}

func TestGeneratedImageRouteAnswersUnknownReference(t *testing.T) {
	api := newGeneratedImageServer()
	cases := map[string]*http.Request{
		"unknown id": httptest.NewRequest(http.MethodGet, generatedImagePrefix+"missing", nil),
		"no id":      httptest.NewRequest(http.MethodGet, generatedImagePrefix, nil),
	}
	for name, request := range cases {
		recorder := httptest.NewRecorder()
		api.handleGeneratedImage(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, want %d", name, recorder.Code, http.StatusNotFound)
		}
	}
}

func TestGeneratedImageRouteRefusesNonGET(t *testing.T) {
	api := newGeneratedImageServer()
	recorder := httptest.NewRecorder()

	api.handleGeneratedImage(recorder, httptest.NewRequest(http.MethodPost, generatedImagePrefix+"x", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want %d", recorder.Code, http.StatusMethodNotAllowed)
	}
}
