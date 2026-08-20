package servers

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/webui"
)

// The interface is served from "/", which the mux treats as the pattern that
// matches everything no other pattern claims. An unmatched API path therefore
// arrives here too, and answering it with HTML would turn a typo in a route
// into a 200 that no client can parse. apiNamespaces is what keeps that from
// happening.
var apiNamespaces = []string{"/v1/", "/mcp", "/health"}

// webAsset is one embedded file with the validators computed once.
type webAsset struct {
	content     []byte
	etag        string
	contentType string
}

// assetCache holds the embedded files keyed by their request path. The set is
// fixed at compile time, so it is read once and never invalidated.
var (
	assetOnce  sync.Once
	assetsByID map[string]webAsset
)

// loadAssets reads every embedded file and computes its validator.
func loadAssets() {
	assetsByID = make(map[string]webAsset)
	files := webui.Files()
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		contentType := mime.TypeByExtension(path.Ext(name))
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		assetsByID["/"+name] = webAsset{
			content:     content,
			etag:        `"` + hex.EncodeToString(sum[:]) + `"`,
			contentType: contentType,
		}
		return nil
	})
	if err != nil {
		logging.Errorf("webui: cannot read the embedded interface: %v", err)
	}
}

// lookupAsset resolves a request path to an embedded file.
//
// A path that names no file falls back to the document, because the interface
// routes in the browser and a reload on one of its own routes must not 404. A
// path that carries an extension does not fall back, so a missing script is
// reported as missing instead of being answered with HTML.
func lookupAsset(requestPath string) (webAsset, bool) {
	assetOnce.Do(loadAssets)

	clean := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	if clean == "/" {
		clean = "/index.html"
	}
	if asset, ok := assetsByID[clean]; ok {
		return asset, true
	}
	if path.Ext(clean) != "" {
		return webAsset{}, false
	}
	asset, ok := assetsByID["/index.html"]
	return asset, ok
}

// cacheControlFor returns the caching rule for one path.
//
// Vite writes a content hash into every asset file name, so those may be held
// indefinitely; the document names them and must be revalidated every time or
// a deploy would keep serving the previous build.
func cacheControlFor(requestPath string) string {
	if strings.HasPrefix(requestPath, "/assets/") {
		return "public, max-age=31536000, immutable"
	}
	return "no-cache"
}

// handleWebUI serves the browser interface.
//
// The document is served without an API key, because the screen that asks for
// the key cannot itself require one. Every data call the interface makes stays
// behind withAuth.
func (api *APIServer) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if !api.config.EnableWebUI {
		api.sendError(w, http.StatusNotFound, "Not found")
		return
	}
	for _, namespace := range apiNamespaces {
		if strings.HasPrefix(r.URL.Path, namespace) || r.URL.Path == strings.TrimSuffix(namespace, "/") {
			api.sendError(w, http.StatusNotFound, "Not found")
			return
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	asset, ok := lookupAsset(r.URL.Path)
	if !ok {
		api.sendError(w, http.StatusNotFound, "Not found")
		return
	}

	w.Header().Set("ETag", asset.etag)
	w.Header().Set("Cache-Control", cacheControlFor(r.URL.Path))
	if matchesETag(r.Header.Get("If-None-Match"), asset.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", asset.contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(asset.content); err != nil {
		logging.Debugf("webui: write failed for %s: %v", r.URL.Path, err)
	}
}

// matchesETag reports whether an If-None-Match header covers the given tag.
// The header may list several tags, and "*" matches any existing entity.
func matchesETag(header, etag string) bool {
	if header == "" {
		return false
	}
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || candidate == etag {
			return true
		}
		// A cache may revalidate with the weak form of a tag it was given.
		if strings.TrimPrefix(candidate, "W/") == etag {
			return true
		}
	}
	return false
}
