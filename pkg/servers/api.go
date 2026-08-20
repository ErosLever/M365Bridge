// Package servers provides HTTP API server for M365 Copilot.
// This file implements OpenAI-compatible and Anthropic-compatible API endpoints.
package servers

import (
	"cmp"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/auth"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/codingtools"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/models"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/payload"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/toolcalling"
	"github.com/google/uuid"
	"github.com/pkoukk/tiktoken-go"
)

const (
	// contextCacheDir is the directory for context cache files.
	contextCacheDir = "data/cache"
	// contextCacheMaxSize is the maximum number of in-memory cache entries.
	contextCacheMaxSize = 256
)

// sessionKeyPrefix namespaces the conversation mapping inside the cache. It is
// the only key shape the cache holds.
const sessionKeyPrefix = "session:"

// sessionRecord is the stored form of one session-to-conversation mapping.
//
// The file name is an md5 of the cache key, so the session ID cannot be read
// back from disk. Storing it inside the file is what makes the mapping
// listable at all.
type sessionRecord struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	UpdatedAt      int64  `json:"updated_at"`
}

// ContextCache provides session-based conversation persistence across requests.
type ContextCache struct {
	cacheDir   string
	mu         sync.RWMutex
	mem        map[string]string
	order      []string
	writeFile  func(string, []byte, os.FileMode) error
	removeFile func(string) error
}

// NewContextCache creates a new context cache instance.
func NewContextCache(cacheDir string) *ContextCache {
	os.MkdirAll(cacheDir, 0700)
	return &ContextCache{
		cacheDir:   cacheDir,
		mem:        make(map[string]string),
		writeFile:  os.WriteFile,
		removeFile: os.Remove,
	}
}

// path returns the file path for a cache key.
func (cc *ContextCache) path(key string) string {
	hash := md5.Sum([]byte(key))
	safe := hex.EncodeToString(hash[:])
	return filepath.Join(cc.cacheDir, safe+".json")
}

// Get retrieves a conversation ID by session key.
func (cc *ContextCache) Get(key string) string {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	if val, ok := cc.mem[key]; ok {
		return val
	}

	data, err := os.ReadFile(cc.path(key))
	if err != nil {
		return ""
	}
	convID, ok := decodeCacheEntry(data)
	if !ok {
		return ""
	}

	cc.mem[key] = convID
	cc.order = append(cc.order, key)
	cc.evict()

	return convID
}

// decodeCacheEntry reads either stored shape. Entries written before the
// record format are a bare JSON string, and they must keep working, because
// they are live session mappings.
func decodeCacheEntry(data []byte) (string, bool) {
	var record sessionRecord
	if err := json.Unmarshal(data, &record); err == nil && record.ConversationID != "" {
		return record.ConversationID, true
	}
	var convID string
	if err := json.Unmarshal(data, &convID); err == nil {
		return convID, true
	}
	return "", false
}

// Set stores a conversation ID by session key.
func (cc *ContextCache) Set(key, convID string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.mem[key] = convID
	if idx := indexOf(cc.order, key); idx >= 0 {
		cc.order = append(cc.order[:idx], cc.order[idx+1:]...)
	}
	cc.order = append(cc.order, key)
	cc.evict()

	data, _ := json.Marshal(sessionRecord{
		SessionID:      strings.TrimPrefix(key, sessionKeyPrefix),
		ConversationID: convID,
		UpdatedAt:      time.Now().Unix(),
	})
	_ = cc.writeFile(cc.path(key), data, 0600)
}

// List returns every mapping the cache dir holds, newest first, together with
// the number of entries written before the record format. Those carry no
// session ID and none can be recovered, because the file name is an md5, so
// they are reported as a count rather than dropped silently.
func (cc *ContextCache) List() ([]sessionRecord, int) {
	entries, err := os.ReadDir(cc.cacheDir)
	if err != nil {
		return nil, 0
	}

	var records []sessionRecord
	legacy := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(cc.cacheDir, entry.Name()))
		if err != nil {
			continue
		}
		var record sessionRecord
		if json.Unmarshal(data, &record) != nil || record.SessionID == "" {
			legacy++
			continue
		}
		records = append(records, record)
	}

	slices.SortFunc(records, func(a, b sessionRecord) int {
		return cmp.Compare(b.UpdatedAt, a.UpdatedAt)
	})
	return records, legacy
}

// Lookup returns the stored mapping for one session ID. An entry in the legacy
// format has no timestamp of its own, so the file modification time stands in.
func (cc *ContextCache) Lookup(sid string) (sessionRecord, bool) {
	convID := cc.Get(sessionKeyPrefix + sid)
	if convID == "" {
		return sessionRecord{}, false
	}
	record := sessionRecord{SessionID: sid, ConversationID: convID}

	data, err := os.ReadFile(cc.path(sessionKeyPrefix + sid))
	if err == nil {
		var stored sessionRecord
		if json.Unmarshal(data, &stored) == nil && stored.UpdatedAt > 0 {
			record.UpdatedAt = stored.UpdatedAt
		}
	}
	if record.UpdatedAt == 0 {
		if info, err := os.Stat(cc.path(sessionKeyPrefix + sid)); err == nil {
			record.UpdatedAt = info.ModTime().Unix()
		}
	}
	return record, true
}

// Delete removes a conversation ID from memory and disk.
func (cc *ContextCache) Delete(key string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	delete(cc.mem, key)
	if idx := indexOf(cc.order, key); idx >= 0 {
		cc.order = append(cc.order[:idx], cc.order[idx+1:]...)
	}
	_ = cc.removeFile(cc.path(key))
}

// evict removes oldest entries when cache exceeds max size.
func (cc *ContextCache) evict() {
	for len(cc.order) > contextCacheMaxSize {
		old := cc.order[0]
		cc.order = cc.order[1:]
		delete(cc.mem, old)
	}
}

// indexOf returns the index of a string in a slice, or -1.
func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

// APIServer handles HTTP API requests.
type APIServer struct {
	config       *models.Config
	tokenManager *auth.TokenManager
	m365Client   *client.M365Client
	codeTools    *codingtools.Manager
	ctxCache     *ContextCache
	server       *http.Server
	stopCh       chan struct{}
	mu           sync.RWMutex

	// throttlingMu guards lastThrottling, which holds the most recent quota
	// counters M365 reported. Handlers read it to turn an exhausted quota into
	// a 429 instead of an unexplained empty response.
	throttlingMu   sync.RWMutex
	lastThrottling *client.ThrottlingInfo
}

// noteThrottling records the quota counters carried by a final stream chunk.
// Chunks without counters leave the previous value untouched, because M365
// sends the throttling object on its own update frames rather than on every
// turn.
func (api *APIServer) noteThrottling(info *client.ThrottlingInfo) {
	if info == nil {
		return
	}
	api.throttlingMu.Lock()
	api.lastThrottling = info
	api.throttlingMu.Unlock()
}

// currentThrottling returns the last known quota counters, or nil.
func (api *APIServer) currentThrottling() *client.ThrottlingInfo {
	api.throttlingMu.RLock()
	defer api.throttlingMu.RUnlock()
	return api.lastThrottling
}

// NewAPIServer creates a new API server instance.
func NewAPIServer(config *models.Config, tokenManager *auth.TokenManager) *APIServer {
	return &APIServer{
		config:       config,
		tokenManager: tokenManager,
		ctxCache:     NewContextCache(contextCacheDir),
	}
}

// tokenRefreshInterval is the interval for periodic access token refresh.
const tokenRefreshInterval = 30 * time.Minute

// Start starts the HTTP server on the specified port.
func (api *APIServer) Start(port int) error {
	api.mu.Lock()
	// Initialize request transports and optional local coding tools.
	api.m365Client = client.NewM365Client(api.tokenManager)
	api.m365Client.SetThrottlingObserver(api.noteThrottling)
	api.m365Client.SetWebSearchEnabled(api.config.EnableWebSearch)
	if api.config.EnableCodeTools {
		manager, err := codingtools.New(codingtools.Config{
			Enabled:       true,
			WorkspaceDir:  api.config.WorkspaceDir,
			Timeout:       api.config.CodeToolTimeout,
			MaxOutput:     api.config.CodeToolMaxOutput,
			MaxReadBytes:  api.config.CodeToolMaxReadBytes,
			MaxIterations: api.config.CodeToolMaxIterations,
		})
		if err != nil {
			api.mu.Unlock()
			return fmt.Errorf("initialize coding tools: %w", err)
		}
		api.codeTools = manager
	}
	api.stopCh = make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", api.withAuth(api.handleChatCompletions))
	mux.HandleFunc("/v1/completions", api.withAuth(api.handleCompletions))
	mux.HandleFunc("/v1/responses", api.withAuth(api.handleResponses))
	mux.HandleFunc("/v1/responses/compact", api.withAuth(api.handleResponsesCompact))
	mux.HandleFunc("/v1/messages", api.withAuth(api.handleAnthropicMessages))
	mux.HandleFunc("/v1/messages/count_tokens", api.withAuth(api.handleAnthropicCountTokens))
	mux.HandleFunc("/v1/complete", api.withAuth(api.handleAnthropicComplete))
	mux.HandleFunc("/v1/images/generations", api.withAuth(api.handleImageGenerations))
	mux.HandleFunc("/v1/images/edits", api.withAuth(api.handleImageEdits))
	mux.HandleFunc("/v1/conversations", api.withAuth(api.handleConversations))
	mux.HandleFunc("/v1/conversations/", api.withAuth(api.handleConversation))
	// Session routes expose conversation IDs, so they stay behind the API key
	// middleware.
	mux.HandleFunc("/v1/sessions", api.withAuth(api.handleSessions))
	mux.HandleFunc("/v1/sessions/", api.withAuth(api.handleSession))
	mux.HandleFunc("/v1/models", api.handleModels)
	// Codex probes /v1/health before it sends any chat request and treats a
	// 404 as an unreachable provider. It stays public alongside /v1/models
	// because the probe carries no credential.
	mux.HandleFunc("/v1/health", api.handleV1Health)
	// MCP exposes Copilot as a tool; it stays behind the API key middleware
	// because it drives real upstream turns.
	mux.HandleFunc("/mcp", api.withAuth(api.handleMCP))
	// Quota counters expose account usage, so this route stays behind the API
	// key middleware unlike the public /v1/models and /health routes.
	mux.HandleFunc("/v1/quota", api.withAuth(api.handleQuota))
	mux.HandleFunc("/health", api.handleHealth)

	api.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}
	api.mu.Unlock()

	// Start background token refresher
	go api.runTokenRefresher()

	if len(api.config.APIKeys) > 0 {
		logging.Infof("Starting API server on port %d (API key required, %d key(s) configured)", port, len(api.config.APIKeys))
	} else {
		logging.Infof("Starting API server on port %d (no API key required)", port)
	}
	return api.server.ListenAndServe()
}

// runTokenRefresher periodically refreshes the access token in the background.
// This prevents the first request after token expiry from blocking 1-2 seconds.
// Also refreshes the designerapp broker token to keep the broker refresh token
// alive (broker RT has a 24h lifetime and must be rotated before expiry).
func (api *APIServer) runTokenRefresher() {
	ticker := time.NewTicker(tokenRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-api.stopCh:
			logging.Info("Token refresher stopping")
			return
		case <-ticker.C:
			logging.Debug("Token refresher: starting periodic refresh")
			if _, err := api.tokenManager.Refresh(); err != nil {
				logging.Errorf("Background token refresh failed: %v", err)
			} else {
				logging.Info("Background token refresh succeeded")
			}
			// Refresh designer token to keep broker RT rotated
			if _, err := api.tokenManager.GetDesignerToken(); err != nil {
				logging.Errorf("Background designer token refresh failed: %v", err)
			} else {
				logging.Debug("Background designer token refresh succeeded")
			}
		}
	}
}

// withAuth wraps a handler with API key authentication.
// If no API keys are configured, all requests are allowed (backward compatible).
func (api *APIServer) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logging.Debugf("Request: %s %s %s", r.Method, r.URL.Path, r.RemoteAddr)
		if r.Method == http.MethodOptions {
			next(w, r)
			return
		}
		if len(api.config.APIKeys) > 0 {
			offered := apiKeyCandidates(r)
			if len(offered) == 0 {
				logging.Warnf("Auth: no API key header from %s", r.RemoteAddr)
				api.sendError(w, http.StatusUnauthorized, "Missing API key; send Authorization: Bearer <key> or x-api-key: <key>")
				return
			}
			if !slices.ContainsFunc(offered, api.isValidAPIKey) {
				logging.Warnf("Auth: invalid API key from %s", r.RemoteAddr)
				api.sendError(w, http.StatusUnauthorized, "Invalid API key")
				return
			}
		}
		next(w, r)
	}
}

// apiKeyCandidates returns every credential the client offered, in priority
// order. An Anthropic SDK client sends the key as `x-api-key` under
// ANTHROPIC_API_KEY and as `Authorization: Bearer` under ANTHROPIC_AUTH_TOKEN,
// and Claude Code can send both at once with only one of them valid, so every
// offered credential is checked rather than just the first.
func apiKeyCandidates(r *http.Request) []string {
	var offered []string
	if key := strings.TrimSpace(r.Header.Get("X-API-Key")); key != "" {
		offered = append(offered, key)
	}
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		// A bare token without the scheme is tolerated because some clients
		// send the key unprefixed.
		if key := strings.TrimSpace(trimBearerPrefix(header)); key != "" {
			offered = append(offered, key)
		}
	}
	return offered
}

// trimBearerPrefix removes a case-insensitive "Bearer " scheme prefix.
func trimBearerPrefix(header string) string {
	if len(header) >= 7 && strings.EqualFold(header[:7], "bearer ") {
		return header[7:]
	}
	return header
}

// isValidAPIKey checks if the given token matches any configured API key.
func (api *APIServer) isValidAPIKey(token string) bool {
	return slices.Contains(api.config.APIKeys, token)
}

// extractAPIKey gets the bearer token from the Authorization header.
// Used as a fallback session ID when no explicit session ID is provided.
func (api *APIServer) extractAPIKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
}

// Stop stops the HTTP server and background token refresher.
func (api *APIServer) Stop() error {
	api.mu.Lock()
	defer api.mu.Unlock()

	// Signal background token refresher to stop
	if api.stopCh != nil {
		close(api.stopCh)
		api.stopCh = nil
	}

	if api.server != nil {
		return api.server.Close()
	}
	return nil
}

// handleHealth handles health check requests.
func (api *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleV1Health answers the OpenAI-style health probe. It reports reachability
// only and never touches the upstream, so it stays cheap enough for a client to
// call it before every session.
//
// It returns JSON rather than the plain "OK" of /health, because every other
// /v1 route speaks JSON and a probe that parses the body would choke on text.
func (api *APIServer) handleV1Health(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleModels handles model list requests.
func (api *APIServer) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Advertised context/output hints so harnesses do not pre-truncate prompts
	// or output. M365 enforces its own server-side limits regardless; these are
	// client-facing hints only, overridable via M365_CONTEXT_WINDOW and
	// M365_MAX_OUTPUT_TOKENS.
	contextWindow := api.config.ContextWindowTokens
	maxOutput := api.config.MaxOutputTokens

	// The advertised output budget is only carved out of the window when it is
	// smaller than the window. With the defaults both hints are the same value,
	// so subtracting would advertise an input budget of zero.
	maxInput := contextWindow
	if maxOutput < contextWindow {
		maxInput = contextWindow - maxOutput
	}

	// Several registry keys are aliases for the same model, so the list is keyed
	// by the advertised id and sorted; a map range would otherwise return
	// duplicates in a different order on every request. The keys are kept
	// because reasoning routing is a property of the key, not of the id.
	byID := make(map[string]models.ModelConfig, len(models.ModelRegistry))
	keysByID := make(map[string][]string, len(models.ModelRegistry))
	for key, cfg := range models.ModelRegistry {
		byID[cfg.OpenAIID] = cfg
		keysByID[cfg.OpenAIID] = append(keysByID[cfg.OpenAIID], key)
	}
	ids := slices.Sorted(maps.Keys(byID))

	modelList := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		modelList = append(modelList, modelCatalogEntry(
			id, byID[id], contextWindow, maxInput, maxOutput, supportsReasoningRoute(keysByID[id])))
	}

	response := map[string]any{
		"object":                   "list",
		"data":                     modelList,
		"reasoning_effort_presets": models.ReasoningEffortPresets,

		// Anthropic's Models API paginates its list and its clients read these
		// three fields. The whole registry always fits in one page, so the
		// cursors are the first and last advertised id and there is never more.
		"has_more": false,
		"first_id": firstOrNil(ids),
		"last_id":  lastOrNil(ids),
	}

	api.sendJSON(w, http.StatusOK, response)
}

// supportsReasoningRoute reports whether any registry key for one advertised
// model reaches a reasoning variant. applyReasoningEffort routes `key` to
// `key+"-reasoning"`, so a model without that sibling ignores the effort a
// caller asks for and must not advertise the capability.
func supportsReasoningRoute(keys []string) bool {
	for _, key := range keys {
		if strings.HasSuffix(key, "-reasoning") {
			return true
		}
		if _, ok := models.ModelRegistry[key+"-reasoning"]; ok {
			return true
		}
	}
	return false
}

// firstOrNil returns the first id, or nil for an empty registry. Anthropic
// types the cursor as nullable, so an empty string would be a wrong value
// rather than an absent one.
func firstOrNil(ids []string) any {
	if len(ids) == 0 {
		return nil
	}
	return ids[0]
}

// lastOrNil returns the last id, or nil for an empty registry.
func lastOrNil(ids []string) any {
	if len(ids) == 0 {
		return nil
	}
	return ids[len(ids)-1]
}

// codexBaseInstructions is advertised in the model catalog. Codex builds its
// own request instructions from it; the proxy never forwards it upstream.
const codexBaseInstructions = "You are a helpful AI assistant. When asked to write code, always provide the complete implementation — never truncate, abbreviate, or return only a fragment. Write full, working code with all logic included."

// modelCreated is the advertised creation time. The registry records no real
// release date, so one fixed value is published in both encodings rather than
// two that could disagree.
const modelCreated = 1700000000

// capabilitySupport renders one Anthropic capability leaf.
func capabilitySupport(supported bool) map[string]any {
	return map[string]any{"supported": supported}
}

// anthropicCapabilities describes one model in the shape Anthropic's Models API
// publishes. Every leaf states what this proxy actually does, so a client that
// reads the tree is not told about a feature the proxy never implements.
func anthropicCapabilities(reasoningRoute, thinking bool) map[string]any {
	return map[string]any{
		// No Batch API, no Anthropic citations, no code execution tool, and no
		// context management strategies are implemented by this proxy.
		"batch":          capabilitySupport(false),
		"citations":      capabilitySupport(false),
		"code_execution": capabilitySupport(false),
		"context_management": map[string]any{
			"supported":                false,
			"clear_thinking_20251015":  capabilitySupport(false),
			"clear_tool_uses_20250919": capabilitySupport(false),
			"compact_20260112":         capabilitySupport(false),
		},
		// Effort is honoured only when the model has a reasoning variant to
		// route to; every advertised preset name is otherwise accepted.
		"effort": map[string]any{
			"supported": reasoningRoute,
			"low":       capabilitySupport(reasoningRoute),
			"medium":    capabilitySupport(reasoningRoute),
			"high":      capabilitySupport(reasoningRoute),
			"max":       capabilitySupport(reasoningRoute),
			"xhigh":     capabilitySupport(reasoningRoute),
		},
		"image_input": capabilitySupport(true),
		// PDF content blocks are not converted for the backend, and no strict
		// schema or JSON mode is enforced on a reply.
		"pdf_input":          capabilitySupport(false),
		"structured_outputs": capabilitySupport(false),
		// Thinking is surfaced only for the reasoning tones, which emit
		// ChainOfThoughtSummary; there is no adaptive mode to select.
		"thinking": map[string]any{
			"supported": thinking,
			"types": map[string]any{
				"enabled":  capabilitySupport(thinking),
				"adaptive": capabilitySupport(false),
			},
		},
	}
}

// modelCatalogEntry builds one /v1/models entry.
//
// The entry carries the OpenAI model object and the Anthropic ModelInfo at
// once, because both protocols are served from this one route and each reads
// only the fields it knows. OpenAI requires id, object, created and owned_by;
// Anthropic requires id, type, display_name and created_at. The two field sets
// do not collide, so neither client needs a separate endpoint.
//
// Capability fields appear both at the top level and under `capabilities`
// because OpenAI-compatible clients disagree on where to look, and Codex reads
// a further set of fields that plain OpenAI clients ignore. Anthropic's
// capability tree is merged into the same map; its key names are disjoint from
// the flat OpenAI-style ones, so both survive.
func modelCatalogEntry(id string, cfg models.ModelConfig, contextWindow, maxInput, maxOutput int, reasoningRoute bool) map[string]any {
	modalities := []string{"text", "image"}
	features := []string{"tools", "function_calling", "streaming", "reasoning", "vision"}
	thinking := cfg.Thinking
	capabilities := map[string]any{
		"chat_completions":           true,
		"responses":                  true,
		"streaming":                  true,
		"tools":                      true,
		"supports_tools":             true,
		"tool_calls":                 true,
		"function_calling":           true,
		"supports_function_calling":  true,
		"reasoning":                  true,
		"reasoning_efforts":          models.ReasoningEffortPresets,
		"supported_reasoning_levels": models.ReasoningEffortPresets,
		"reasoning_mode":             "gateway_tone_routing",
		"vision":                     true,
		"supports_vision":            true,
		"modalities":                 modalities,
		"input_modalities":           modalities,
		"output_modalities":          []string{"text"},
		"supported_features":         features,
	}
	// Anthropic's tree uses key names none of the flat entries above take, so
	// merging leaves both readable from the one map.
	maps.Copy(capabilities, anthropicCapabilities(reasoningRoute, thinking))

	return map[string]any{
		// OpenAI model object: id, object, created and owned_by are required,
		// shutdown_date is nullable and nothing is scheduled to retire.
		"id":            id,
		"slug":          id,
		"object":        "model",
		"created":       modelCreated,
		"owned_by":      cfg.OwnerOrDefault(),
		"shutdown_date": nil,

		// Anthropic ModelInfo: type, display_name and created_at complete the
		// object, and max_tokens is its name for the output ceiling.
		"type":       "model",
		"created_at": time.Unix(modelCreated, 0).UTC().Format(time.RFC3339),
		"max_tokens": maxOutput,

		"display_name":      cfg.DisplayNameOrDefault(),
		"description":       "Public model endpoint.",
		"visibility":        "list",
		"supported_in_api":  true,
		"priority":          1,
		"base_instructions": codexBaseInstructions,
		"model_messages":    codexModelMessages(),

		"context_window":                   contextWindow,
		"max_context_window":               contextWindow,
		"effective_context_window_percent": 95,
		"max_input_tokens":                 maxInput,
		"max_output_tokens":                maxOutput,
		"truncation_policy":                map[string]any{"mode": "tokens", "limit": 10000},

		"default_reasoning_level":      "medium",
		"supports_reasoning_summaries": true,
		"default_reasoning_summary":    "none",
		"supported_reasoning_levels":   models.ReasoningEffortPresets,
		"support_verbosity":            true,
		"default_verbosity":            "low",

		// Every model reaches caller-defined tools through the simulated tool
		// calling layer, so tool support is a property of the proxy rather than
		// of the tone.
		"supports_tools":                 true,
		"tool_calls":                     true,
		"function_calling":               true,
		"supports_function_calling":      true,
		"supports_parallel_tool_calls":   true,
		"tool_mode":                      "code_mode_only",
		"shell_type":                     "shell_command",
		"apply_patch_tool_type":          "freeform",
		"web_search_tool_type":           "text_and_image",
		"supports_search_tool":           true,
		"experimental_supported_tools":   []any{},
		"supports_image_detail_original": true,

		"vision":             true,
		"supports_vision":    true,
		"modalities":         modalities,
		"input_modalities":   modalities,
		"output_modalities":  []string{"text"},
		"supported_features": features,

		"additional_speed_tiers":            []string{},
		"service_tiers":                     []any{},
		"availability_nux":                  nil,
		"upgrade":                           nil,
		"include_skills_usage_instructions": false,
		"use_responses_lite":                false,
		"multi_agent_version":               "v2",

		"capabilities": capabilities,
	}
}

// codexModelMessages is the instruction template block Codex expects alongside
// each catalog entry.
func codexModelMessages() map[string]any {
	return map[string]any{
		"instructions_template": codexBaseInstructions,
		"instructions_variables": map[string]string{
			"personality_default":   "",
			"personality_friendly":  "",
			"personality_pragmatic": "",
		},
		"approvals":   nil,
		"auto_review": nil,
	}
}

// upstreamThrottledCode marks a response rejected because the M365
// conversation exhausted its message quota.
const upstreamThrottledCode = "upstream_throttled"

// handleQuota reports the last known M365 conversation quota counters. The
// backend only sends them while a turn is in flight, so the values reflect the
// most recent chat request rather than a live lookup.
func (api *APIServer) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	info := api.currentThrottling()
	if info == nil {
		api.sendJSON(w, http.StatusOK, map[string]any{
			"object":    "quota",
			"available": false,
			"detail":    "no quota counters observed yet; send a chat request first",
		})
		return
	}

	response := map[string]any{
		"object":    "quota",
		"available": true,
		"exhausted": info.Exhausted(),
	}
	if info.NumUserMessages != nil {
		response["used"] = *info.NumUserMessages
	}
	if info.MaxNumUserMessages != nil {
		response["max"] = *info.MaxNumUserMessages
	}
	if info.NumUserMessages != nil && info.MaxNumUserMessages != nil {
		response["headroom"] = *info.MaxNumUserMessages - *info.NumUserMessages
	}
	if len(info.Extra) > 0 {
		response["extra"] = info.Extra
	}
	api.sendJSON(w, http.StatusOK, response)
}

// quotaExhausted reports whether the last observed counters show the
// conversation at its message ceiling.
func (api *APIServer) quotaExhausted() bool {
	return api.currentThrottling().Exhausted()
}

// sendThrottledError reports an exhausted conversation quota as HTTP 429 so
// clients back off instead of retrying against an unexplained empty response.
func (api *APIServer) sendThrottledError(w http.ResponseWriter) {
	info := api.currentThrottling()
	message := "M365 conversation message quota exhausted; start a new session to continue"
	if summary := info.Summary(); summary != "" {
		message = message + " (" + summary + ")"
	}
	api.sendErrorCode(w, http.StatusTooManyRequests, upstreamThrottledCode, message)
}

// upstreamContentBlockedCode marks a reply that is M365's canned content
// refusal rather than an answer.
const upstreamContentBlockedCode = "upstream_content_blocked"

// sendContentBlockedError reports a backend content refusal as HTTP 502.
//
// The refusal reads like an ordinary short answer, so an agent client would
// otherwise accept it and continue on nothing. A distinct status lets the
// client tell "the backend declined this request" apart from "here is the
// answer".
func (api *APIServer) sendContentBlockedError(w http.ResponseWriter, reply string) {
	logging.Warn("upstream content refusal: M365 declined the request instead of answering")
	api.sendErrorCode(w, http.StatusBadGateway, upstreamContentBlockedCode,
		"M365 declined this request: "+strings.TrimSpace(reply))
}

// blockedByContentPolicy reports whether a finished turn is a content refusal
// with nothing else to deliver. A turn that also produced tool calls is real
// work and is left alone.
func blockedByContentPolicy(respText string, toolCalls []client.ToolCall) bool {
	return len(toolCalls) == 0 && toolcalling.IsContentPolicyBlock(respText)
}

// unverifiedCompletionNotice replaces an answer that reports finished work the
// server has no evidence for.
const unverifiedCompletionNotice = "No tool was called in this turn and no tool result exists, so the reported completion is not verified. Nothing was executed."

// withoutUnverifiedCompletionClaim replaces an answer in which the model says
// it carried the work out itself, while the turn emitted no tool call and the
// history holds no tool result to support it. An agent client would otherwise
// accept the report and stop.
//
// The guard is limited to requests that declared tools. The reference this is
// drawn from omits that condition, so a plain chat answer such as "Go was
// created at Google" trips it; here such an answer is never touched.
func withoutUnverifiedCompletionClaim(respText string, hasTools bool, ledger toolcalling.Ledger, toolCalls []client.ToolCall) string {
	return replaceUnverifiedCompletionClaim(respText, hasTools, ledger, len(toolCalls))
}

// replaceUnverifiedCompletionClaim is the count-based form, for the streaming
// paths whose parsed calls are toolcalling.ToolCall rather than client.ToolCall.
func replaceUnverifiedCompletionClaim(respText string, hasTools bool, ledger toolcalling.Ledger, toolCallCount int) string {
	if !unverifiedCompletionClaim(respText, hasTools, ledger, toolCallCount) {
		return respText
	}
	logging.Warn("replacing an unverified completion claim: the turn called no tool and holds no tool result")
	logging.Debugf("unverified completion claim was: %q", respText)
	return unverifiedCompletionNotice
}

// unverifiedCompletionClaim is the condition behind the guard, separated so a
// streaming turn can report it without holding client-shaped tool calls.
func unverifiedCompletionClaim(respText string, hasTools bool, ledger toolcalling.Ledger, toolCallCount int) bool {
	if !hasTools || toolCallCount > 0 || len(ledger.Completed) > 0 {
		return false
	}
	return toolcalling.ClaimsUnverifiedCompletion(respText)
}

// warnOnUnverifiedCompletionClaim reports the same failure where the text has
// already reached the client and can no longer be replaced. Only the Responses
// stream is in that position: it publishes assistant content as it decodes it,
// while the other streaming paths buffer a tool-enabled turn until the parse is
// done and can still replace the answer.
func warnOnUnverifiedCompletionClaim(respText string, hasTools bool, ledger toolcalling.Ledger, toolCallCount int) {
	if unverifiedCompletionClaim(respText, hasTools, ledger, toolCallCount) {
		logging.Warn("unverified completion claim on a streaming turn: the text was already sent to the client and could not be replaced")
		logging.Debugf("unverified completion claim was: %q", respText)
	}
}

// handleConversations lists or creates M365 conversations.
func (api *APIServer) handleConversations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method == http.MethodPost {
		api.createConversation(w, r)
		return
	}
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	conversationClient := client.NewConversationClient(api.tokenManager)
	conversations, err := conversationClient.ListConversations(r.Context())
	if err != nil {
		api.sendConversationError(w, err)
		return
	}
	api.sendJSON(w, http.StatusOK, map[string]any{"conversations": conversations})
}

func (api *APIServer) createConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Message   string `json:"message"`
		Name      string `json:"name"`
		Model     string `json:"model"`
		SessionID string `json:"session_id,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		api.sendError(w, http.StatusBadRequest, "message is required")
		return
	}
	if req.Model == "" {
		req.Model = "gpt5.5-reasoning"
	}
	cfg, ok := api.resolveModel(w, req.Model)
	if !ok {
		return
	}
	messages := []payload.Message{{Role: "user", Content: req.Message}}
	_, _, _, _, conversationID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, "", api.config.UserOID, api.config.TenantID, false)
	if err != nil {
		api.sendError(w, http.StatusBadGateway, "M365 conversation creation failed")
		return
	}
	if conversationID == "" {
		api.sendError(w, http.StatusBadGateway, "M365 conversation creation returned no conversation ID")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		conversationClient := client.NewConversationClient(api.tokenManager)
		if err := conversationClient.RenameConversation(r.Context(), conversationID, strings.TrimSpace(req.Name)); err != nil {
			api.sendConversationError(w, err)
			return
		}
	}
	api.sendJSON(w, http.StatusCreated, map[string]any{"id": conversationID, "name": req.Name})
}

// handleConversation renames or permanently deletes one M365 conversation.
func (api *APIServer) handleConversation(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	conversationID := strings.TrimPrefix(r.URL.Path, "/v1/conversations/")
	if conversationID == "" || strings.Contains(conversationID, "/") {
		api.sendError(w, http.StatusNotFound, "Conversation not found")
		return
	}
	conversationClient := client.NewConversationClient(api.tokenManager)
	switch r.Method {
	case http.MethodPatch:
		var req struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
			api.sendError(w, http.StatusBadRequest, "Invalid request body")
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			api.sendError(w, http.StatusBadRequest, "name is required")
			return
		}
		if err := conversationClient.RenameConversation(r.Context(), conversationID, strings.TrimSpace(req.Name)); err != nil {
			api.sendConversationError(w, err)
			return
		}
		api.sendJSON(w, http.StatusOK, map[string]any{"id": conversationID, "name": strings.TrimSpace(req.Name)})
	case http.MethodDelete:
		if err := conversationClient.DeleteConversation(r.Context(), conversationID); err != nil {
			api.sendConversationError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (api *APIServer) sendConversationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrM365CookiesUnavailable):
		api.sendError(w, http.StatusUnauthorized, "M365 web app cookies are not configured")
	case errors.Is(err, client.ErrConversationAuthentication):
		api.sendError(w, http.StatusUnauthorized, "M365 web app cookies are invalid or expired")
	default:
		logging.Errorf("Conversation management request failed: %v", err)
		api.sendError(w, http.StatusBadGateway, "M365 conversation service request failed")
	}
}

// handleCORS handles CORS preflight requests.
func (api *APIServer) handleCORS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-Id")
	w.WriteHeader(http.StatusOK)
}

// getSessionID extracts session ID from headers or request body.
// Priority: X-Session-Id header > session_id body field > user body field > hash(api_key + first_user_message)
func (api *APIServer) getSessionID(r *http.Request, reqBody map[string]any) string {
	sid := r.Header.Get("X-Session-Id")
	if sid == "" {
		if v, ok := reqBody["session_id"].(string); ok {
			sid = v
		}
	}
	if sid == "" {
		if v, ok := reqBody["user"].(string); ok {
			sid = v
		}
	}
	if sid == "" {
		sid = api.hashSessionID(r, reqBody)
	}
	return sid
}

// hashSessionID derives a session ID from the API key and the first user message.
// When auth is enabled, the hash includes the API key so that different keys
// produce different sessions even with the same first message.
// When auth is disabled, only the first user message is hashed.
func (api *APIServer) hashSessionID(r *http.Request, reqBody map[string]any) string {
	firstMsg := extractFirstUserMessage(reqBody)
	if firstMsg == "" {
		return ""
	}
	apiKey := api.extractAPIKey(r)
	h := md5.Sum([]byte(apiKey + "\x00" + firstMsg))
	return "h:" + hex.EncodeToString(h[:])
}

// extractFirstUserMessage scans the messages array and returns the first user message content.
func extractFirstUserMessage(reqBody map[string]any) string {
	msgs, ok := reqBody["messages"].([]any)
	if !ok || len(msgs) == 0 {
		return ""
	}
	for _, m := range msgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role != "user" {
			continue
		}
		// Content can be a string or an array of content blocks
		switch c := msg["content"].(type) {
		case string:
			if c != "" {
				return c
			}
		case []any:
			for _, block := range c {
				bm, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := bm["type"].(string); t == "text" {
					if txt, _ := bm["text"].(string); txt != "" {
						return txt
					}
				}
			}
		}
	}
	return ""
}

// hashSessionIDFromMessages derives a session ID from the API key and the first user message
// in a typed Message slice. Used by handleChatCompletions which decodes into a struct.
func (api *APIServer) hashSessionIDFromMessages(r *http.Request, messages []payload.Message) string {
	firstMsg := ""
	for _, m := range messages {
		if m.Role == "user" && m.Content != "" {
			firstMsg = m.Content
			break
		}
	}
	if firstMsg == "" {
		return ""
	}
	apiKey := api.extractAPIKey(r)
	h := md5.Sum([]byte(apiKey + "\x00" + firstMsg))
	return "h:" + hex.EncodeToString(h[:])
}

type toolLoopProvider int

const (
	toolLoopOpenAI toolLoopProvider = iota
	toolLoopAnthropic
)

type toolLoopResult struct {
	text           string
	thinking       string
	toolCalls      []client.ToolCall
	finishReason   string
	conversationID string
}

func clientToolCallFromSimulated(call toolcalling.ToolCall) client.ToolCall {
	return client.ToolCall{
		ID:   call.ID,
		Type: "function",
		Function: client.ToolCallFunction{
			Name:      call.Name,
			Namespace: call.Namespace,
			Arguments: string(call.Arguments),
		},
	}
}

func (api *APIServer) prepareCodingTools(tools []toolcalling.ToolDef, anthropic bool) ([]toolcalling.ToolDef, map[string]bool) {
	local := make(map[string]bool)
	if api.codeTools == nil {
		return tools, local
	}
	available := make(map[string]codingtools.Tool)
	for _, schema := range api.codeTools.Tools() {
		available[schema.Name] = schema
	}
	for _, definition := range tools {
		name := toolcalling.ToolName(&definition)
		if _, ok := available[name]; ok {
			local[name] = true
		}
	}
	if !api.config.AutoExposeTools {
		return tools, local
	}
	seen := make(map[string]bool, len(tools))
	for i := range tools {
		seen[toolcalling.ToolName(&tools[i])] = true
	}
	for _, schema := range api.codeTools.Tools() {
		local[schema.Name] = true
		if seen[schema.Name] {
			continue
		}
		definition := toolcalling.ToolDef{Name: schema.Name, Description: schema.Description, InputSchema: schema.InputSchema}
		if !anthropic {
			definition = toolcalling.ToolDef{Type: "function", Function: toolcalling.ToolDefFunc{Name: schema.Name, Description: schema.Description, Parameters: schema.InputSchema}}
		}
		tools = append(tools, definition)
	}
	return tools, local
}

func replaceRequestTools(body []byte, tools []toolcalling.ToolDef) string {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return string(body)
	}
	request["tools"] = tools
	updated, err := json.Marshal(request)
	if err != nil {
		return string(body)
	}
	return string(updated)
}

func (api *APIServer) runToolLoop(r *http.Request, provider toolLoopProvider, messages []payload.Message, cfg models.ModelConfig, convID string, tools []toolcalling.ToolDef, local map[string]bool) (toolLoopResult, error) {
	currentConvID := convID
	seen := make(map[string]bool)
	for iteration := 0; ; iteration++ {
		text, thinking, backendCalls, finishReason, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, currentConvID, api.config.UserOID, api.config.TenantID, len(tools) > 0)
		if err != nil {
			return toolLoopResult{}, err
		}
		if finalConvID != "" {
			currentConvID = finalConvID
		}
		if len(tools) == 0 {
			_, finishReason = withoutBackendToolCalls(backendCalls, finishReason)
			return toolLoopResult{text: text, thinking: thinking, finishReason: finishReason, conversationID: currentConvID}, nil
		}
		contracts := toolcalling.ContractsFor(tools)
		var simulated toolcalling.SimulatedResult
		if provider == toolLoopAnthropic {
			simulated = toolcalling.ParseSimulatedResponseAnthropic(text, toolNamesFromDefs(tools), contracts)
		} else {
			simulated = toolcalling.ParseSimulatedResponse(text, toolNamesFromDefs(tools), contracts)
		}
		simulated = api.repairSimulatedToolCalls(provider, messages, cfg, tools, contracts, text, simulated)
		if !simulated.HasPayload || len(simulated.ToolCalls) == 0 {
			if simulated.HasPayload {
				text, finishReason = simulated.Content, "stop"
			}
			return toolLoopResult{text: text, thinking: thinking, finishReason: finishReason, conversationID: currentConvID}, nil
		}
		var callerCalls []client.ToolCall
		var localCalls []toolcalling.ToolCall
		for _, call := range simulated.ToolCalls {
			converted := clientToolCallFromSimulated(call)
			if local[call.Name] {
				localCalls = append(localCalls, call)
			} else {
				callerCalls = append(callerCalls, converted)
			}
		}
		if len(callerCalls) > 0 {
			return toolLoopResult{thinking: thinking, toolCalls: callerCalls, finishReason: "tool_calls", conversationID: currentConvID}, nil
		}
		if iteration >= api.config.CodeToolMaxIterations-1 {
			return toolLoopResult{}, errors.New("coding tool iteration limit reached")
		}
		var resultParts []string
		for _, call := range localCalls {
			key := call.Name + "\x00" + string(call.Arguments)
			if seen[key] {
				return toolLoopResult{}, fmt.Errorf("duplicate coding tool call %q", call.Name)
			}
			seen[key] = true
			var arguments map[string]any
			if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
				arguments = map[string]any{}
			}
			encoded, err := codingtools.MarshalResult(api.codeTools.Execute(r.Context(), call.Name, arguments))
			if err != nil {
				return toolLoopResult{}, fmt.Errorf("serialize coding tool result: %w", err)
			}
			resultParts = append(resultParts, toolcalling.FormatSimulatedToolResult(call.ID, call.Name, string(encoded)))
		}
		messages = append(messages, payload.Message{Role: "user", Content: strings.Join(resultParts, "\n\n")})
		request := map[string]any{"model": cfg.OpenAIID, "messages": messages, "tools": tools, "stream": false}
		requestJSON, err := json.Marshal(request)
		if err != nil {
			return toolLoopResult{}, fmt.Errorf("serialize coding tool continuation: %w", err)
		}
		if provider == toolLoopAnthropic {
			// The synthetic messages of the built-in loop carry no client tool
			// structure, so there is no ledger to pass here.
			injectSimulatedPromptAnthropic(&messages, string(requestJSON), "auto", "")
		} else {
			injectSimulatedPrompt(&messages, string(requestJSON), "auto", "")
		}
	}
}

// repairSimulatedToolCalls performs a single corrective re-ask when the initial
// simulated parse dropped every tool call for missing required arguments. The
// request envelope already lives in the last message, so the retry re-sends it
// with an appended corrective note on a fresh conversation and re-parses. It
// returns the recovered result when the backend supplies valid tool calls, and
// the original result otherwise so callers keep their existing fallback path.
func (api *APIServer) repairSimulatedToolCalls(provider toolLoopProvider, messages []payload.Message, cfg models.ModelConfig, tools []toolcalling.ToolDef, contracts toolcalling.ToolContracts, rawText string, sim toolcalling.SimulatedResult) toolcalling.SimulatedResult {
	if len(sim.ToolCalls) > 0 || len(messages) == 0 || len(tools) == 0 {
		return sim
	}

	var note string
	narrated := false
	switch {
	case len(sim.DroppedCalls) > 0:
		note = toolcalling.BuildRepairNote(sim.DroppedCalls, contracts)
		logging.Warnf("repairSimulatedToolCalls: re-asking backend, tool calls failed validation: %v", sim.DroppedCalls)
	default:
		// The backend produced no tool call at all. That is only worth a re-ask
		// when the reply denies the tools exist, claims the work already ran
		// elsewhere, or only announces which tool it means to use; an ordinary
		// text answer is a legitimate outcome.
		answer := sim.Content
		if answer == "" {
			answer = rawText
		}
		switch {
		case toolcalling.IsToolRefusal(answer):
			logging.Warn("repairSimulatedToolCalls: re-asking backend, reply denied the declared tools exist")
		case toolcalling.IsSandboxHallucination(answer):
			logging.Warn("repairSimulatedToolCalls: re-asking backend, reply claimed to have run the work itself")
		case toolcalling.IsToolIntentNarration(answer, toolNamesFromDefs(tools)):
			narrated = true
			logging.Warn("repairSimulatedToolCalls: re-asking backend, reply only announced which tool it would use")
		default:
			return sim
		}
		note = toolcalling.BuildNativeToolBanNote()
	}

	retry := make([]payload.Message, len(messages))
	copy(retry, messages)
	last := retry[len(retry)-1]
	last.Content = last.Content + "\n\n" + note
	retry[len(retry)-1] = last

	text, _, _, _, _, err := api.m365Client.ChatConversation(retry, cfg.Tone, cfg.Override, "", api.config.UserOID, api.config.TenantID, true)
	if err != nil {
		logging.Errorf("repairSimulatedToolCalls: retry failed: %v", err)
		return sim
	}

	var retried toolcalling.SimulatedResult
	if provider == toolLoopAnthropic {
		retried = toolcalling.ParseSimulatedResponseAnthropic(text, toolNamesFromDefs(tools), contracts)
	} else {
		retried = toolcalling.ParseSimulatedResponse(text, toolNamesFromDefs(tools), contracts)
	}
	if len(retried.ToolCalls) > 0 {
		logging.Infof("repairSimulatedToolCalls: recovered %d tool call(s) after re-ask", len(retried.ToolCalls))
		return retried
	}
	// An announcement that survived the re-ask is worse than useless to the
	// client: it reads as work in progress that never arrives.
	if narrated {
		logging.Warn("repairSimulatedToolCalls: re-ask stayed an announcement; replacing the answer text")
		sim.Content = toolcalling.ToolNarrationNotice
		sim.FinishReason = "stop"
		sim.HasPayload = true
		return sim
	}
	logging.Warn("repairSimulatedToolCalls: re-ask did not yield valid tool calls; keeping original response")
	return sim
}

// handleChatCompletions handles OpenAI chat completion requests.
func (api *APIServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var req struct {
		Model          string                `json:"model"`
		Messages       []payload.Message     `json:"messages"`
		Stream         bool                  `json:"stream"`
		MaxTokens      int                   `json:"max_tokens"`
		ResponseFormat map[string]any        `json:"response_format"`
		SessionID      string                `json:"session_id"`
		User           string                `json:"user"`
		Tools          []toolcalling.ToolDef `json:"tools"`
		ToolChoice     any                   `json:"tool_choice"`
		StreamOptions  *streamOptions        `json:"stream_options"`
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		logging.Errorf("handleChatCompletions: invalid JSON: %v", err)
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse optional session ID encoded in model name: "gpt5.5:my-session"
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}
	logging.Infof("handleChatCompletions: model=%s stream=%v tools=%d sid=%s", modelKey, req.Stream, len(req.Tools), modelSessionID)

	if err := validateToolResultMessages(req.Messages); err != nil {
		logging.Errorf("handleChatCompletions: %v", err)
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	ledger := buildToolLedger(req.Messages)
	if api.exceededToolRoundLimit(ledger) {
		api.sendToolRoundLimitError(w, ledger)
		return
	}

	// Handle JSON mode
	if req.ResponseFormat != nil {
		if format, ok := req.ResponseFormat["type"].(string); ok && format == "json_object" {
			api.injectJSONMode(&req.Messages)
		}
	}

	preparedTools, localTools := api.prepareCodingTools(req.Tools, false)
	req.Tools = preparedTools
	requestJSON := replaceRequestTools(bodyBytes, req.Tools)
	// The declaration list stays whole in requestJSON so the model sees every
	// capability, but simulation only runs when something is left for the
	// client to execute.
	if len(toolcalling.RouteableTools(req.Tools)) > 0 {
		injectSimulatedPrompt(&req.Messages, requestJSON, toolChoiceString(req.ToolChoice), ledger.EvidenceNote())
	}

	// Resolve session ID and conversation ID
	// Priority: model-name session ID > request body session_id > request body user > X-Session-Id header > hash(api_key + first_user_message)
	sid := modelSessionID
	if sid == "" {
		sid = req.SessionID
	}
	if sid == "" {
		sid = req.User
	}
	if sid == "" {
		sid = r.Header.Get("X-Session-Id")
	}
	if sid == "" {
		sid = api.hashSessionIDFromMessages(r, req.Messages)
	}

	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	// Upload any images found in multimodal content and attach annotations
	api.uploadImagesAndAnnotate(&req.Messages, convID)

	// Determine if client-defined tools are present (for optionsSets stripping)
	hasTools := len(toolcalling.RouteableTools(req.Tools)) > 0

	if len(localTools) > 0 {
		result, err := api.runToolLoop(r, toolLoopOpenAI, req.Messages, cfg, convID, req.Tools, localTools)
		if err != nil {
			api.sendUpstreamError(w, "chat", err)
			return
		}
		api.respondBufferedChat(w, result, req.Messages, cfg, sid, req.MaxTokens, req.Stream, req.Tools, toolChoiceString(req.ToolChoice))
		return
	}
	if req.Stream {
		api.streamChatCompletions(r.Context(), w, req.Messages, cfg, sid, convID, req.MaxTokens, hasTools, req.Tools, toolChoiceString(req.ToolChoice), includeStreamUsage(req.StreamOptions))
	} else {
		api.nonStreamChatCompletions(w, req.Messages, cfg, sid, convID, req.MaxTokens, hasTools, req.Tools, toolChoiceString(req.ToolChoice))
	}
}

// handleCompletions handles OpenAI text completion requests.
func (api *APIServer) handleCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var req struct {
		Model         string                `json:"model"`
		Prompt        string                `json:"prompt"`
		Suffix        string                `json:"suffix"`
		Stream        bool                  `json:"stream"`
		MaxTokens     int                   `json:"max_tokens"`
		Tools         []toolcalling.ToolDef `json:"tools"`
		ToolChoice    any                   `json:"tool_choice"`
		StreamOptions *streamOptions        `json:"stream_options"`
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse optional session ID encoded in model name: "gpt5.5:my-session"
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}

	// Convert FIM to chat format
	messages := api.fimToChat(req.Prompt, req.Suffix)

	// Inject simulated tool prompt if tool calling is enabled
	if len(toolcalling.RouteableTools(req.Tools)) > 0 {
		// A FIM completion carries no tool history, so there is no evidence.
		injectSimulatedPrompt(&messages, string(bodyBytes), toolChoiceString(req.ToolChoice), "")
	}

	// Resolve session ID and conversation ID
	sid := modelSessionID
	if sid == "" {
		sid = api.getSessionID(r, nil)
	}
	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	hasTools := len(toolcalling.RouteableTools(req.Tools)) > 0

	if req.Stream {
		api.streamCompletions(r.Context(), w, messages, cfg, req.MaxTokens, sid, convID, hasTools, req.Tools, toolChoiceString(req.ToolChoice), includeStreamUsage(req.StreamOptions))
	} else {
		api.nonStreamCompletions(w, messages, cfg, req.MaxTokens, sid, convID, hasTools, req.Tools, toolChoiceString(req.ToolChoice))
	}
}

func normalizeAnthropicSystem(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}

	var systemText string
	if err := json.Unmarshal(raw, &systemText); err == nil {
		return systemText, nil
	}

	var blocks []struct {
		Type string
		Text string
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return "", err
	}

	parts := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block.Text != "" {
			parts = append(parts, block.Text)
		}
	}

	return strings.Join(parts, "\n\n"), nil
}

// handleAnthropicCountTokens handles Anthropic token counting requests.
func (api *APIServer) handleAnthropicCountTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		System   json.RawMessage `json:"system"`
		Messages json.RawMessage `json:"messages"`
		Tools    json.RawMessage `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}
	if len(req.Messages) == 0 || string(req.Messages) == "null" {
		api.sendError(w, http.StatusBadRequest, "messages is required")
		return
	}

	countable, err := json.Marshal(struct {
		System   json.RawMessage `json:"system,omitempty"`
		Messages json.RawMessage `json:"messages"`
		Tools    json.RawMessage `json:"tools,omitempty"`
	}{
		System:   req.System,
		Messages: req.Messages,
		Tools:    req.Tools,
	})
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid token input: %v", err))
		return
	}

	api.sendJSON(w, http.StatusOK, map[string]int{"input_tokens": countTokens(string(countable))})
}

// handleAnthropicMessages handles Anthropic messages API requests.
func (api *APIServer) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var req struct {
		Model       string                `json:"model"`
		Messages    []payload.Message     `json:"messages"`
		System      json.RawMessage       `json:"system"`
		MaxTokens   int                   `json:"max_tokens"`
		Stream      bool                  `json:"stream"`
		Temperature float64               `json:"temperature"`
		Tools       []toolcalling.ToolDef `json:"tools"`
		ToolChoice  map[string]any        `json:"tool_choice"`
	}

	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		logging.Errorf("handleAnthropicMessages: invalid JSON: %v", err)
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse optional session ID encoded in model name: "gpt5.5:my-session"
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	// Map Anthropic model to internal model
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}
	logging.Infof("handleAnthropicMessages: model=%s stream=%v tools=%d sid=%s", modelKey, req.Stream, len(req.Tools), modelSessionID)

	if err := validateToolResultMessages(req.Messages); err != nil {
		logging.Errorf("handleAnthropicMessages: %v", err)
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	ledger := buildToolLedger(req.Messages)
	if api.exceededToolRoundLimit(ledger) {
		api.sendToolRoundLimitError(w, ledger)
		return
	}

	// Build chat messages with system prompt prepended. Claude Code can send
	// Anthropic system as either a string or an array of text content blocks.
	systemPrompt, err := normalizeAnthropicSystem(req.System)
	if err != nil {
		logging.Errorf("handleAnthropicMessages: invalid system field: %v", err)
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid system field: %v", err))
		return
	}

	chatMessages := []payload.Message{}
	if systemPrompt != "" {
		chatMessages = append(chatMessages, payload.Message{Role: "system", Content: systemPrompt})
	}
	chatMessages = append(chatMessages, req.Messages...)

	preparedTools, localTools := api.prepareCodingTools(req.Tools, true)
	req.Tools = preparedTools
	requestJSON := replaceRequestTools(bodyBytes, req.Tools)
	if len(toolcalling.RouteableTools(req.Tools)) > 0 {
		injectSimulatedPromptAnthropic(&chatMessages, requestJSON, anthropicToolChoiceString(req.ToolChoice), ledger.EvidenceNote())
	}

	// Resolve session ID and conversation ID
	sid := modelSessionID
	if sid == "" {
		sid = api.getSessionID(r, nil)
	}
	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	// Upload any images found in multimodal content and attach annotations
	api.uploadImagesAndAnnotate(&chatMessages, convID)

	// Determine if client-defined tools are present (for optionsSets stripping)
	hasTools := len(toolcalling.RouteableTools(req.Tools)) > 0

	if len(localTools) > 0 {
		result, err := api.runToolLoop(r, toolLoopAnthropic, chatMessages, cfg, convID, req.Tools, localTools)
		if err != nil {
			api.sendUpstreamError(w, "chat", err)
			return
		}
		api.respondBufferedAnthropic(w, result, chatMessages, req.Model, sid, req.MaxTokens, req.Stream, req.Tools, anthropicToolChoiceEnforcement(req.ToolChoice))
		return
	}
	if req.Stream {
		api.streamAnthropicMessages(r.Context(), w, chatMessages, cfg, req.Model, req.MaxTokens, sid, convID, hasTools, req.Tools, anthropicToolChoiceEnforcement(req.ToolChoice))
	} else {
		api.nonStreamAnthropicMessages(w, chatMessages, cfg, req.Model, req.MaxTokens, sid, convID, hasTools, req.Tools, anthropicToolChoiceEnforcement(req.ToolChoice))
	}
}

// handleAnthropicComplete handles Anthropic complete (FIM) requests.
func (api *APIServer) handleAnthropicComplete(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Model             string   `json:"model"`
		Prompt            string   `json:"prompt"`
		MaxTokensToSample int      `json:"max_tokens_to_sample"`
		Stream            bool     `json:"stream"`
		StopSequences     []string `json:"stop_sequences"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse optional session ID encoded in model name: "gpt5.5:my-session"
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}

	messages := api.fimToChat(req.Prompt, "")

	// Resolve session ID and conversation ID
	sid := modelSessionID
	if sid == "" {
		sid = api.getSessionID(r, nil)
	}
	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	if req.Stream {
		api.streamAnthropicComplete(r.Context(), w, messages, cfg, req.Model, req.MaxTokensToSample, req.StopSequences, sid, convID)
	} else {
		api.nonStreamAnthropicComplete(w, messages, cfg, req.Model, req.MaxTokensToSample, req.StopSequences, sid, convID)
	}
}

// nonStreamAnthropicComplete handles non-streaming Anthropic complete (FIM) requests.
func (api *APIServer) nonStreamAnthropicComplete(w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, model string, maxTokens int, stopSequences []string, sid, convID string) {
	respText, thinking, _, _, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, false)
	if err != nil {
		api.sendUpstreamError(w, "completion", err)
		return
	}

	stopReason := "end_turn"
	for _, s := range stopSequences {
		if strings.Contains(respText, s) {
			stopReason = "stop_sequence"
			break
		}
	}

	// Enforce max_tokens_to_sample on response text
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
			stopReason = "max_tokens"
		}
	}

	// The legacy Complete format defines no usage object. It is reported anyway
	// so a caller reads the same counts here as on every other endpoint; a
	// client written to the original format ignores the extra field.
	response := map[string]any{
		"completion":  respText,
		"stop_reason": stopReason,
		"model":       model,
		"stop":        nil,
		"log_id":      fmt.Sprintf("cmpl_%s", uuid.New().String()),
		"usage":       anthropicUsage(messages, nil, "", respText, thinking),
	}

	api.sendJSON(w, http.StatusOK, response)

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// streamAnthropicComplete streams Anthropic complete (FIM) responses.
// Anthropic Complete streaming uses SSE with event: completion and
// data containing {"type":"completion","completion":"<delta>","stop_reason":null}.
// The final event has stop_reason set and completion empty.
func (api *APIServer) streamAnthropicComplete(ctx context.Context, w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, model string, maxTokens int, stopSequences []string, sid, convID string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	logID := fmt.Sprintf("cmpl_%s", uuid.New().String())

	// Send ping event (Anthropic streaming starts with ping)
	pingData := map[string]any{"type": "ping"}
	pingJSON, _ := json.Marshal(pingData)
	fmt.Fprintf(w, "event: ping\ndata: %s\n\n", pingJSON)
	flusher.Flush()

	ch := api.m365Client.ChatConversationStreamGenContext(ctx, messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, false)

	var fullTextBuilder strings.Builder
	var thinkingText strings.Builder
	truncated := false

	var finalConvID string
	var finalToolCalls []client.ToolCall
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeAnthropicKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			// The classification rather than the transport error, which names
			// request URLs and credential file paths.
			_, code, message := streamErrorFields("complete", chunk.Error)
			errData := map[string]any{
				"type":  "error",
				"error": map[string]any{"type": code, "message": message},
			}
			errJSON, _ := json.Marshal(errData)
			fmt.Fprintf(w, "event: error\ndata: %s\n\n", errJSON)
			flusher.Flush()
			return
		}

		if chunk.IsFinal {
			finalConvID = chunk.ConversationID
			finalToolCalls = chunk.ToolCalls
			break
		}

		// The Complete wire format carries no thinking block, so reasoning is
		// counted for the usage object but never emitted.
		if chunk.Thinking != "" {
			thinkingText.WriteString(chunk.Thinking)
			continue
		}

		// Check max_tokens limit
		if maxTokens > 0 && countTokens(fullTextBuilder.String()) >= maxTokens {
			truncated = true
			for range ch {
			}
			break
		}

		fullTextBuilder.WriteString(chunk.Text)

		// Send completion event with delta text
		compData := map[string]any{
			"type":        "completion",
			"completion":  chunk.Text,
			"stop_reason": nil,
			"model":       model,
			"log_id":      logID,
		}
		compJSON, _ := json.Marshal(compData)
		fmt.Fprintf(w, "event: completion\ndata: %s\n\n", compJSON)
		flusher.Flush()
	}
	_ = finalToolCalls
	fullText := fullTextBuilder.String()

	// Determine stop reason
	stopReason := "end_turn"
	for _, s := range stopSequences {
		if strings.Contains(fullText, s) {
			stopReason = "stop_sequence"
			break
		}
	}
	if truncated {
		stopReason = "max_tokens"
	}

	// Send final completion event with stop_reason
	finalData := map[string]any{
		"type":        "completion",
		"completion":  "",
		"stop_reason": stopReason,
		"model":       model,
		"stop":        nil,
		"log_id":      logID,
		// The intermediate events carry deltas, so usage belongs on the last
		// one. The legacy Complete format defines no such field; it is reported
		// so a caller reads the same counts here as on every other endpoint.
		"usage": anthropicUsage(messages, nil, "", fullText, thinkingText.String()),
	}
	finalJSON, _ := json.Marshal(finalData)
	fmt.Fprintf(w, "event: completion\ndata: %s\n\n", finalJSON)
	flusher.Flush()

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// streamChatCompletions streams chat completion responses in OpenAI format.
func (api *APIServer) streamChatCompletions(ctx context.Context, w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, sid, convID string, maxTokens int, hasTools bool, tools []toolcalling.ToolDef, toolChoice string, includeUsage bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Commit the response headers before the upstream turn starts. The first
	// chunk can take almost ten seconds on a tool-enabled turn, and a client
	// that sees no bytes at all cannot tell a slow provider from a dead one.
	refreshStreamDeadline(w)
	if err := writeSSEKeepalive(w, flusher); err != nil {
		return
	}

	chunkID := fmt.Sprintf("chatcmpl-%s", uuid.New().String())
	openaiModel := cfg.OpenAIID

	ch := api.m365Client.ChatConversationStreamGenContext(ctx, messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)

	hasContent := false
	var fullTextBuilder strings.Builder
	var thinkingText strings.Builder
	var thinkingFilter toolcalling.ThinkingStreamFilter
	truncated := false

	// When tool calling is enabled AND tools are present, buffer all text and
	// parse for tool calls at the end. Tool call blocks may span multiple
	// chunks, so we can't parse incrementally. When no tools are present, stream
	// text directly regardless of the global ToolCalling flag.
	toolCallingEnabled := hasTools

	var finalConvID string
	var finalToolCalls []client.ToolCall
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeSSEKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			api.sendSSEError(w, "chat", chunkID, openaiModel, chunk.Error)
			return
		}

		if chunk.IsFinal {
			finalConvID = chunk.ConversationID
			finalToolCalls = chunk.ToolCalls
			break
		}

		// Send thinking as reasoning_content (OpenAI extended thinking format)
		if chunk.Thinking != "" {
			thinkingText.WriteString(chunk.Thinking)
			if toolCallingEnabled {
				// Live-stream filtered thinking so the transport envelope never
				// leaks, matching the Anthropic streaming path.
				if emit := thinkingFilter.Feed(chunk.Thinking); emit != "" {
					payload := map[string]any{"reasoning_content": emit}
					if !hasContent {
						payload["role"] = "assistant"
						hasContent = true
					}
					api.sendSSEChunk(w, chunkID, openaiModel, payload)
					flusher.Flush()
				}
				continue
			}
			if !hasContent {
				api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
					"role":              "assistant",
					"reasoning_content": chunk.Thinking,
				})
				hasContent = true
			} else {
				api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
					"reasoning_content": chunk.Thinking,
				})
			}
			flusher.Flush()
			continue
		}

		// Check max_tokens limit before sending more content
		if maxTokens > 0 && countTokens(fullTextBuilder.String()) >= maxTokens {
			truncated = true
			// Drain remaining chunks
			for range ch {
			}
			break
		}

		fullTextBuilder.WriteString(chunk.Text)

		// If tool calling is not enabled, stream text directly
		if !toolCallingEnabled {
			if !hasContent {
				api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
					"role":    "assistant",
					"content": chunk.Text,
				})
				hasContent = true
			} else {
				api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
					"content": chunk.Text,
				})
			}
			flusher.Flush()
		}
	}
	fullText := fullTextBuilder.String()

	// Parse simulated tool calls from full text if tool calling is enabled
	var simToolCalls []toolcalling.ToolCall
	if toolCallingEnabled {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponse(fullText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopOpenAI, messages, cfg, tools, contracts, fullText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				simToolCalls = sim.ToolCalls
				fullText = ""
			} else {
				fullText = sim.Content
			}
		} else {
			fullText = toolcalling.WithholdTransportEnvelope(fullText)
			if toolcalling.IsContentPolicyBlock(fullText) {
				// The stream is already open, so the refusal cannot be turned
				// into an HTTP error the way the non-streaming paths do.
				logging.Warn("upstream content refusal on a streaming turn: M365 declined the request instead of answering")
			}
		}
		fullText = replaceUnverifiedCompletionClaim(fullText, hasTools, buildToolLedger(messages), len(simToolCalls))
	}

	if toolCallingEnabled {
		if rem := thinkingFilter.Flush(); rem != "" {
			payload := map[string]any{"reasoning_content": rem}
			if !hasContent {
				payload["role"] = "assistant"
				hasContent = true
			}
			api.sendSSEChunk(w, chunkID, openaiModel, payload)
			flusher.Flush()
		}
	}

	// If tool calling buffered text, send it now as a single chunk
	if toolCallingEnabled && fullText != "" && len(simToolCalls) == 0 {
		if !hasContent {
			api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
				"role":    "assistant",
				"content": fullText,
			})
			hasContent = true
		} else {
			api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
				"content": fullText,
			})
		}
		flusher.Flush()
	}

	// Send tool calls in stream if any (from M365 backend or simulated)
	toolCalls, _ := withoutBackendToolCalls(finalToolCalls, "")

	// Append simulated tool calls
	for _, stc := range simToolCalls {
		toolCalls = append(toolCalls, client.ToolCall{
			ID:       stc.ID,
			Type:     "function",
			Function: client.ToolCallFunction{Name: stc.Name, Namespace: stc.Namespace, Arguments: string(stc.Arguments)},
		})
	}

	if len(toolCalls) > 0 {
		if !hasContent {
			api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
				"role":    "assistant",
				"content": nil,
			})
		}
		for i, tc := range toolCalls {
			api.sendSSEChunk(w, chunkID, openaiModel, map[string]any{
				"tool_calls": []map[string]any{
					{
						"index": i,
						"id":    tc.ID,
						"type":  "function",
						"function": map[string]string{
							"name":      tc.Function.Name,
							"arguments": tc.Function.Arguments,
						},
					},
				},
			})
		}
		flusher.Flush()
	}

	// Send final chunk with usage
	finishReason := "stop"
	if truncated {
		finishReason = "length"
	}
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}
	var usage map[string]any
	if includeUsage {
		promptTok := countPromptTokens(messages, tools, toolChoice)
		completionTok := countTokens(fullText) + outputProtocolTokens
		reasoningTok := countTokens(thinkingText.String())
		usage = map[string]any{
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"reasoning_tokens":  reasoningTok,
			"total_tokens":      promptTok + completionTok + reasoningTok,
			"usage_source":      usageSource(),
		}
	}

	api.sendSSEDone(w, chunkID, openaiModel, finishReason, usage)
	flusher.Flush()

	api.updateChatStreamSession(sid, finalConvID, fullText, thinkingText.String(), toolCalls)
}

func (api *APIServer) updateChatStreamSession(sid, finalConvID, fullText, thinkingText string, toolCalls []client.ToolCall) {
	if sid == "" {
		return
	}

	if strings.TrimSpace(fullText) == "" &&
		strings.TrimSpace(thinkingText) == "" &&
		len(toolCalls) == 0 {
		api.ctxCache.Delete(sessionKeyPrefix + sid)
		return
	}

	if finalConvID != "" {
		api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
	}
}

func (api *APIServer) respondBufferedChat(w http.ResponseWriter, result toolLoopResult, messages []payload.Message, cfg models.ModelConfig, sid string, maxTokens int, stream bool, tools []toolcalling.ToolDef, toolChoice string) {
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(result.text, maxTokens); ok {
			result.text, result.finishReason = truncated, "length"
		}
	}
	if sid != "" && result.conversationID != "" {
		api.ctxCache.Set(sessionKeyPrefix+sid, result.conversationID)
	}
	usage := openAIUsage(messages, tools, toolChoice, result.text, result.thinking)
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		id := fmt.Sprintf("chatcmpl-%s", uuid.New().String())
		if result.text != "" {
			api.sendSSEChunk(w, id, cfg.OpenAIID, map[string]any{"role": "assistant", "content": result.text})
		}
		for i, call := range result.toolCalls {
			api.sendSSEChunk(w, id, cfg.OpenAIID, map[string]any{"tool_calls": []map[string]any{{"index": i, "id": call.ID, "type": "function", "function": map[string]string{"name": call.Function.Name, "arguments": call.Function.Arguments}}}})
		}
		api.sendSSEDone(w, id, cfg.OpenAIID, result.finishReason, usage)
		return
	}
	message := map[string]any{"role": "assistant", "content": result.text}
	if len(result.toolCalls) > 0 {
		message["content"] = nil
		message["tool_calls"] = result.toolCalls
	}
	api.sendJSON(w, http.StatusOK, map[string]any{"id": fmt.Sprintf("chatcmpl-%s", uuid.New().String()), "object": "chat.completion", "created": time.Now().Unix(), "model": cfg.OpenAIID, "choices": []map[string]any{{"index": 0, "message": message, "finish_reason": result.finishReason}}, "usage": usage})
}

// nonStreamChatCompletions handles non-streaming chat completion in OpenAI format.
func (api *APIServer) nonStreamChatCompletions(w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, sid, convID string, maxTokens int, hasTools bool, tools []toolcalling.ToolDef, toolChoice string) {
	respText, thinking, toolCalls, finishReason, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)
	if err != nil {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendUpstreamError(w, "chat", err)
		return
	}

	toolCalls, finishReason = withoutBackendToolCalls(toolCalls, finishReason)
	// The transport thinking filter belongs to simulated mode only, where the
	// envelope travels inside the reasoning text.
	if len(tools) > 0 {
		thinking = chatAnthropicThinkingForOutput(thinking, true)
	}

	// Parse simulated tool calls from response text if tool calling is enabled
	if hasTools {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponse(respText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopOpenAI, messages, cfg, tools, contracts, respText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				finishReason = "tool_calls"
				for _, pc := range sim.ToolCalls {
					toolCalls = append(toolCalls, client.ToolCall{
						ID:       pc.ID,
						Type:     "function",
						Function: client.ToolCallFunction{Name: pc.Name, Namespace: pc.Namespace, Arguments: string(pc.Arguments)},
					})
				}
				respText = ""
			} else {
				respText = sim.Content
				finishReason = "stop"
			}
		} else {
			// M365 did not return a simulated JSON payload (e.g. it ran
			// its own server-side tools and returned plain text). Since
			// we discarded backend-injected toolCalls above, reset the
			// finish reason so we don't report tool_use with no blocks.
			finishReason = "stop"
			respText = toolcalling.WithholdTransportEnvelope(respText)
		}
	}

	if blockedByContentPolicy(respText, toolCalls) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendContentBlockedError(w, respText)
		return
	}
	respText = withoutUnverifiedCompletionClaim(respText, hasTools, buildToolLedger(messages), toolCalls)

	// Enforce max_tokens on response text
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
			finishReason = "length"
		}
	}

	msg := map[string]any{
		"role":    "assistant",
		"content": respText,
	}

	if thinking != "" {
		msg["reasoning_content"] = thinking
	}

	if len(toolCalls) > 0 {
		openaiToolCalls := make([]map[string]any, len(toolCalls))
		for i, tc := range toolCalls {
			openaiToolCalls[i] = map[string]any{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]string{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}
		}
		msg["tool_calls"] = openaiToolCalls
		if respText == "" {
			msg["content"] = nil
		}
	}

	promptTok := countPromptTokens(messages, tools, toolChoice)
	completionTok := countTokens(respText) + outputProtocolTokens
	reasoningTok := countTokens(thinking)
	response := map[string]any{
		"id":      fmt.Sprintf("chatcmpl-%s", uuid.New().String()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   cfg.OpenAIID,
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       msg,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"reasoning_tokens":  reasoningTok,
			"total_tokens":      promptTok + completionTok + reasoningTok,
			"usage_source":      usageSource(),
		},
	}

	api.sendJSON(w, http.StatusOK, response)

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// streamAnthropicMessages streams messages in Anthropic SSE format.
func (api *APIServer) streamAnthropicMessages(ctx context.Context, w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, anthropicModel string, maxTokens int, sid, convID string, hasTools bool, tools []toolcalling.ToolDef, toolChoice string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	msgID := fmt.Sprintf("msg_%s", uuid.New().String())
	// The Anthropic wire format splits usage across message_start and
	// message_delta, so the input side is counted once here and not repeated.
	promptTok := countPromptTokens(messages, tools, toolChoice)

	// Send message_start event
	header := map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"content":       []any{},
			"model":         anthropicModel,
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  promptTok,
				"output_tokens": 0,
				"usage_source":  usageSource(),
			},
		},
	}
	api.sendAnthropicSSE(w, "message_start", header)
	flusher.Flush()

	// Stream content with optional thinking block
	var fullTextBuilder strings.Builder
	var thinkingText strings.Builder
	var thinkingFilter toolcalling.ThinkingStreamFilter
	thinkingClosed := false
	truncated := false
	thinkingBlockOpen := false
	textBlockOpen := false
	blockIndex := 0
	toolCallingEnabled := hasTools
	ch := api.m365Client.ChatConversationStreamGenContext(ctx, messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)

	var finalConvID string
	var finalToolCalls []client.ToolCall
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeAnthropicKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			_, code, message := streamErrorFields("message", chunk.Error)
			errEvent := map[string]any{
				"type": "error",
				"error": map[string]any{
					"type":    code,
					"message": message,
				},
			}
			api.sendAnthropicSSE(w, "error", errEvent)
			flusher.Flush()
			return
		}

		if chunk.IsFinal {
			finalConvID = chunk.ConversationID
			finalToolCalls = chunk.ToolCalls
			break
		}

		// Handle thinking content
		if chunk.Thinking != "" {
			thinkingText.WriteString(chunk.Thinking)
			if toolCallingEnabled {
				// Live-stream thinking through a stateful filter that strips the
				// simulated transport envelope (fenced blocks + meta-prose) so
				// the model's reasoning is visible without exposing the mechanism.
				if emit := thinkingFilter.Feed(chunk.Thinking); emit != "" {
					if !thinkingBlockOpen {
						api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
						thinkingBlockOpen = true
					}
					api.sendAnthropicSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": emit}})
					flusher.Flush()
				}
				continue
			}
			if !thinkingBlockOpen {
				cbStart := map[string]any{
					"type":          "content_block_start",
					"index":         blockIndex,
					"content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""},
				}
				api.sendAnthropicSSE(w, "content_block_start", cbStart)
				thinkingBlockOpen = true
			}
			delta := map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "thinking_delta", "thinking": chunk.Thinking},
			}
			api.sendAnthropicSSE(w, "content_block_delta", delta)
			flusher.Flush()
			continue
		}

		// Transition from thinking to text: flush the filter remainder (tool
		// path) then close any open thinking block before content follows.
		if toolCallingEnabled && !thinkingClosed {
			if rem := thinkingFilter.Flush(); rem != "" {
				if !thinkingBlockOpen {
					api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
					thinkingBlockOpen = true
				}
				api.sendAnthropicSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": rem}})
			}
			thinkingClosed = true
		}
		if thinkingBlockOpen && !textBlockOpen {
			api.sendAnthropicSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
			blockIndex++
			thinkingBlockOpen = false
		}

		// Open text block on first text chunk (only if not buffering for tool calling)
		if !textBlockOpen && !toolCallingEnabled {
			cbStart := map[string]any{
				"type":          "content_block_start",
				"index":         blockIndex,
				"content_block": map[string]any{"type": "text", "text": ""},
			}
			api.sendAnthropicSSE(w, "content_block_start", cbStart)
			textBlockOpen = true
		}

		// Check max_tokens limit before sending more content
		if maxTokens > 0 && countTokens(fullTextBuilder.String()) >= maxTokens {
			truncated = true
			for range ch {
			}
			break
		}

		fullTextBuilder.WriteString(chunk.Text)

		// If tool calling is not enabled, stream text deltas directly
		if !toolCallingEnabled {
			delta := map[string]any{
				"type":  "content_block_delta",
				"index": blockIndex,
				"delta": map[string]any{"type": "text_delta", "text": chunk.Text},
			}
			api.sendAnthropicSSE(w, "content_block_delta", delta)
			flusher.Flush()
		}
	}
	fullText := fullTextBuilder.String()

	// Parse simulated tool calls from full text if tool calling is enabled
	var simToolCalls []toolcalling.ToolCall
	if toolCallingEnabled {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponseAnthropic(fullText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopAnthropic, messages, cfg, tools, contracts, fullText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				simToolCalls = sim.ToolCalls
				fullText = ""
			} else {
				fullText = sim.Content
			}
		} else {
			fullText = toolcalling.WithholdTransportEnvelope(fullText)
			if toolcalling.IsContentPolicyBlock(fullText) {
				// The stream is already open, so the refusal cannot be turned
				// into an HTTP error the way the non-streaming paths do.
				logging.Warn("upstream content refusal on a streaming turn: M365 declined the request instead of answering")
			}
		}
		fullText = replaceUnverifiedCompletionClaim(fullText, hasTools, buildToolLedger(messages), len(simToolCalls))
	}

	// Flush any remaining filtered thinking when the response was thinking-only
	// (no content chunk triggered the in-loop transition) and close its block.
	if toolCallingEnabled && !thinkingClosed {
		if rem := thinkingFilter.Flush(); rem != "" {
			if !thinkingBlockOpen {
				api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": blockIndex, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}})
				thinkingBlockOpen = true
			}
			api.sendAnthropicSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": blockIndex, "delta": map[string]any{"type": "thinking_delta", "thinking": rem}})
		}
		if thinkingBlockOpen {
			api.sendAnthropicSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
			blockIndex++
			thinkingBlockOpen = false
		}
		thinkingClosed = true
	}

	// If tool calling buffered text, send it now as a text block
	if toolCallingEnabled && fullText != "" {
		cbStart := map[string]any{
			"type":          "content_block_start",
			"index":         blockIndex,
			"content_block": map[string]any{"type": "text", "text": ""},
		}
		api.sendAnthropicSSE(w, "content_block_start", cbStart)
		textBlockOpen = true
		delta := map[string]any{
			"type":  "content_block_delta",
			"index": blockIndex,
			"delta": map[string]any{"type": "text_delta", "text": fullText},
		}
		api.sendAnthropicSSE(w, "content_block_delta", delta)
		flusher.Flush()
	}

	// Close any open blocks
	if thinkingBlockOpen {
		api.sendAnthropicSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}
	if textBlockOpen {
		api.sendAnthropicSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": blockIndex})
		blockIndex++
	}

	// Send tool_use content blocks if any (server-side tools from M365 backend or simulated)
	toolCalls, _ := withoutBackendToolCalls(finalToolCalls, "")

	// Append simulated tool calls
	for _, stc := range simToolCalls {
		toolCalls = append(toolCalls, client.ToolCall{
			ID:       stc.ID,
			Type:     "function",
			Function: client.ToolCallFunction{Name: stc.Name, Namespace: stc.Namespace, Arguments: string(stc.Arguments)},
		})
	}

	for _, tc := range toolCalls {
		// Anthropic streaming delivers tool_use input as input_json_delta
		// fragments, not inside content_block_start. SDK clients (e.g. Claude
		// Code) accumulate partial_json and ignore any input in the start
		// event, so the full arguments must be sent as a delta or the client
		// sees an empty input and loops.
		partialJSON := strings.TrimSpace(tc.Function.Arguments)
		if partialJSON == "" {
			partialJSON = "{}"
		}
		api.sendAnthropicSSE(w, "content_block_start", map[string]any{
			"type":  "content_block_start",
			"index": blockIndex,
			"content_block": map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": map[string]any{},
			},
		})
		api.sendAnthropicSSE(w, "content_block_delta", map[string]any{
			"type":  "content_block_delta",
			"index": blockIndex,
			"delta": map[string]any{
				"type":         "input_json_delta",
				"partial_json": partialJSON,
			},
		})
		api.sendAnthropicSSE(w, "content_block_stop", map[string]any{
			"type":  "content_block_stop",
			"index": blockIndex,
		})
		blockIndex++
	}
	flusher.Flush()

	// Send message_delta event
	stopReason := "end_turn"
	if truncated {
		stopReason = "max_tokens"
	}
	if len(toolCalls) > 0 {
		stopReason = "tool_use"
	}
	msgDelta := map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens":    countTokens(fullText) + outputProtocolTokens,
			"reasoning_tokens": countTokens(thinkingText.String()),
			"usage_source":     usageSource(),
		},
	}
	api.sendAnthropicSSE(w, "message_delta", msgDelta)
	flusher.Flush()

	// Send message_stop event
	msgStop := map[string]any{"type": "message_stop"}
	api.sendAnthropicSSE(w, "message_stop", msgStop)
	flusher.Flush()

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

func (api *APIServer) respondBufferedAnthropic(w http.ResponseWriter, result toolLoopResult, messages []payload.Message, model, sid string, maxTokens int, stream bool, tools []toolcalling.ToolDef, toolChoice string) {
	stopReason := "end_turn"
	if len(result.toolCalls) > 0 {
		stopReason = "tool_use"
	}
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(result.text, maxTokens); ok {
			result.text, stopReason = truncated, "max_tokens"
		}
	}
	content := []map[string]any{}
	if result.text != "" {
		content = append(content, map[string]any{"type": "text", "text": result.text})
	}
	for _, call := range result.toolCalls {
		var input any
		if json.Unmarshal([]byte(call.Function.Arguments), &input) != nil {
			input = map[string]any{}
		}
		content = append(content, map[string]any{"type": "tool_use", "id": call.ID, "name": call.Function.Name, "input": input})
	}
	if sid != "" && result.conversationID != "" {
		api.ctxCache.Set(sessionKeyPrefix+sid, result.conversationID)
	}
	usage := anthropicUsage(messages, tools, toolChoice, result.text, result.thinking)
	response := map[string]any{"id": fmt.Sprintf("msg_%s", uuid.New().String()), "type": "message", "role": "assistant", "content": content, "model": model, "stop_reason": stopReason, "stop_sequence": nil, "usage": usage}
	if !stream {
		api.sendJSON(w, http.StatusOK, response)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	api.sendAnthropicSSE(w, "message_start", map[string]any{"type": "message_start", "message": map[string]any{"id": response["id"], "type": "message", "role": "assistant", "content": []any{}, "model": model, "stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": usage["input_tokens"], "output_tokens": 0, "usage_source": usageSource()}}})
	for i, block := range content {
		switch block["type"] {
		case "tool_use":
			// tool_use input must stream as an input_json_delta fragment, not
			// inline in content_block_start; SDK clients accumulate partial_json
			// and otherwise see an empty input and loop forever.
			start := map[string]any{"type": "tool_use", "id": block["id"], "name": block["name"], "input": map[string]any{}}
			api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": start})
			partial, err := json.Marshal(block["input"])
			if err != nil || len(partial) == 0 {
				partial = []byte("{}")
			}
			api.sendAnthropicSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(partial)}})
		case "text":
			api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
			if txt, _ := block["text"].(string); txt != "" {
				api.sendAnthropicSSE(w, "content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": txt}})
			}
		default:
			api.sendAnthropicSSE(w, "content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": block})
		}
		api.sendAnthropicSSE(w, "content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	api.sendAnthropicSSE(w, "message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stopReason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": countTokens(result.text)}})
	api.sendAnthropicSSE(w, "message_stop", map[string]any{"type": "message_stop"})
}

// nonStreamAnthropicMessages handles non-streaming Anthropic messages response.
func (api *APIServer) nonStreamAnthropicMessages(w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, anthropicModel string, maxTokens int, sid, convID string, hasTools bool, tools []toolcalling.ToolDef, toolChoice string) {
	respText, thinking, toolCalls, finishReason, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)
	if err != nil {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendUpstreamError(w, "chat", err)
		return
	}

	toolCalls, finishReason = withoutBackendToolCalls(toolCalls, finishReason)
	// The transport thinking filter belongs to simulated mode only, where the
	// envelope travels inside the reasoning text.
	if len(tools) > 0 {
		thinking = chatAnthropicThinkingForOutput(thinking, true)
	}

	// Parse simulated tool calls from response text if tool calling is enabled
	if hasTools {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponseAnthropic(respText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopAnthropic, messages, cfg, tools, contracts, respText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				finishReason = "tool_calls"
				for _, pc := range sim.ToolCalls {
					toolCalls = append(toolCalls, client.ToolCall{
						ID:       pc.ID,
						Type:     "function",
						Function: client.ToolCallFunction{Name: pc.Name, Namespace: pc.Namespace, Arguments: string(pc.Arguments)},
					})
				}
				respText = ""
			} else {
				respText = sim.Content
				finishReason = "stop"
			}
		} else {
			// M365 did not return a simulated JSON payload (e.g. it ran
			// its own server-side tools and returned plain text). Since
			// we discarded backend-injected toolCalls above, reset the
			// finish reason so we don't report tool_use with no blocks.
			finishReason = "stop"
			respText = toolcalling.WithholdTransportEnvelope(respText)
		}
	}

	stopReason := "end_turn"
	if finishReason == "tool_calls" {
		stopReason = "tool_use"
	}

	if blockedByContentPolicy(respText, toolCalls) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendContentBlockedError(w, respText)
		return
	}
	respText = withoutUnverifiedCompletionClaim(respText, hasTools, buildToolLedger(messages), toolCalls)

	// Enforce max_tokens on response text
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
			stopReason = "max_tokens"
		}
	}

	content := []map[string]any{}
	if thinking != "" {
		content = append(content, map[string]any{"type": "thinking", "thinking": thinking, "signature": ""})
	}
	if respText != "" {
		content = append(content, map[string]any{"type": "text", "text": respText})
	}

	if len(toolCalls) > 0 {
		for _, tc := range toolCalls {
			var input any
			json.Unmarshal([]byte(tc.Function.Arguments), &input)
			if input == nil {
				input = map[string]any{}
			}
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    tc.ID,
				"name":  tc.Function.Name,
				"input": input,
			})
		}
	}

	response := map[string]any{
		"id":            fmt.Sprintf("msg_%s", uuid.New().String()),
		"type":          "message",
		"role":          "assistant",
		"content":       content,
		"model":         anthropicModel,
		"stop_reason":   stopReason,
		"stop_sequence": nil,
		"usage": map[string]any{
			"input_tokens":     countPromptTokens(messages, tools, toolChoice),
			"output_tokens":    countTokens(respText) + outputProtocolTokens,
			"reasoning_tokens": countTokens(thinking),
			"usage_source":     usageSource(),
		},
	}

	api.sendJSON(w, http.StatusOK, response)

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// streamCompletions streams text completion responses in OpenAI text_completion format.
func (api *APIServer) streamCompletions(ctx context.Context, w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, maxTokens int, sid, convID string, hasTools bool, tools []toolcalling.ToolDef, toolChoice string, includeUsage bool) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	// Commit the response headers before the upstream turn starts. The first
	// chunk can take almost ten seconds on a tool-enabled turn, and a client
	// that sees no bytes at all cannot tell a slow provider from a dead one.
	refreshStreamDeadline(w)
	if err := writeSSEKeepalive(w, flusher); err != nil {
		return
	}

	compID := fmt.Sprintf("cmpl-%s", uuid.New().String())
	openaiModel := cfg.OpenAIID

	ch := api.m365Client.ChatConversationStreamGenContext(ctx, messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)

	var fullTextBuilder strings.Builder
	var thinkingBuilder strings.Builder
	truncated := false
	toolCallingEnabled := hasTools

	var finalConvID string
	var finalToolCalls []client.ToolCall
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeSSEKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			// An error object rather than completion text, for the same reason
			// the chat stream carries one: text would be stored as the answer.
			status, code, message := streamErrorFields("completion", chunk.Error)
			errChunk := map[string]any{
				"id":      compID,
				"object":  "text_completion",
				"created": time.Now().Unix(),
				"model":   openaiModel,
				"error": map[string]any{
					"message": message,
					"type":    openAIErrorType(status),
					"code":    code,
				},
			}
			jsonData, _ := json.Marshal(errChunk)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		if chunk.IsFinal {
			finalConvID = chunk.ConversationID
			finalToolCalls = chunk.ToolCalls
			break
		}

		// Accumulate thinking text (not sent as content for text_completion)
		if chunk.Thinking != "" {
			thinkingBuilder.WriteString(chunk.Thinking)
			continue
		}

		// Check max_tokens limit before sending more content
		if maxTokens > 0 && countTokens(fullTextBuilder.String()) >= maxTokens {
			truncated = true
			for range ch {
			}
			break
		}

		fullTextBuilder.WriteString(chunk.Text)

		// If tool calling is not enabled, stream text directly
		if !toolCallingEnabled {
			chunkData := map[string]any{
				"id":      compID,
				"object":  "text_completion",
				"created": time.Now().Unix(),
				"model":   openaiModel,
				"choices": []map[string]any{
					{
						"index":         0,
						"text":          chunk.Text,
						"finish_reason": nil,
						"logprobs":      nil,
					},
				},
			}

			jsonData, _ := json.Marshal(chunkData)
			fmt.Fprintf(w, "data: %s\n\n", jsonData)
			flusher.Flush()
		}
	}
	_ = finalToolCalls
	fullText := fullTextBuilder.String()

	// Parse simulated tool calls from buffered text if tool calling is enabled
	var simToolCalls []toolcalling.ToolCall
	if toolCallingEnabled {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponse(fullText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopOpenAI, messages, cfg, tools, contracts, fullText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				simToolCalls = sim.ToolCalls
				fullText = ""
			} else {
				fullText = sim.Content
			}
		} else {
			fullText = toolcalling.WithholdTransportEnvelope(fullText)
			if toolcalling.IsContentPolicyBlock(fullText) {
				// The stream is already open, so the refusal cannot be turned
				// into an HTTP error the way the non-streaming paths do.
				logging.Warn("upstream content refusal on a streaming turn: M365 declined the request instead of answering")
			}
		}
		fullText = replaceUnverifiedCompletionClaim(fullText, hasTools, buildToolLedger(messages), len(simToolCalls))
	}

	// If tool calling buffered text, send it now as a single chunk
	if toolCallingEnabled && fullText != "" && len(simToolCalls) == 0 {
		chunkData := map[string]any{
			"id":      compID,
			"object":  "text_completion",
			"created": time.Now().Unix(),
			"model":   openaiModel,
			"choices": []map[string]any{
				{
					"index":         0,
					"text":          fullText,
					"finish_reason": nil,
					"logprobs":      nil,
				},
			},
		}
		jsonData, _ := json.Marshal(chunkData)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()
	}

	// Send final done chunk
	finishReason := "stop"
	if truncated {
		finishReason = "length"
	}
	if len(simToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	doneChunk := map[string]any{
		"id":      compID,
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   openaiModel,
		"choices": []map[string]any{
			{
				"index":         0,
				"text":          "",
				"finish_reason": finishReason,
				"logprobs":      nil,
			},
		},
	}
	// This route reported no usage at all, unlike every other streaming
	// endpoint, so the reasoning it accumulated reached no client.
	if includeUsage {
		promptTok := countPromptTokens(messages, tools, toolChoice)
		completionTok := countTokens(fullText) + outputProtocolTokens
		reasoningTok := countTokens(thinkingBuilder.String())
		doneChunk["usage"] = map[string]any{
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"reasoning_tokens":  reasoningTok,
			"total_tokens":      promptTok + completionTok + reasoningTok,
			"usage_source":      usageSource(),
		}
	}
	jsonData, _ := json.Marshal(doneChunk)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// nonStreamCompletions handles non-streaming text completion.
func (api *APIServer) nonStreamCompletions(w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, maxTokens int, sid, convID string, hasTools bool, tools []toolcalling.ToolDef, toolChoice string) {
	respText, thinking, toolCalls, finishReason, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)
	if err != nil {
		api.sendUpstreamError(w, "completion", err)
		return
	}

	toolCalls, finishReason = withoutBackendToolCalls(toolCalls, finishReason)

	// Parse simulated tool calls from response text
	if hasTools {
		contracts := toolcalling.ContractsFor(tools).WithChoice(toolChoice)
		sim := toolcalling.ParseSimulatedResponse(respText, toolNamesFromDefs(tools), contracts)
		sim = api.repairSimulatedToolCalls(toolLoopOpenAI, messages, cfg, tools, contracts, respText, sim)
		sim = dropSettledToolCalls(buildToolLedger(messages), toolChoice, sim)
		if sim.HasPayload {
			if len(sim.ToolCalls) > 0 {
				finishReason = "tool_calls"
				for _, pc := range sim.ToolCalls {
					toolCalls = append(toolCalls, client.ToolCall{
						ID:       pc.ID,
						Type:     "function",
						Function: client.ToolCallFunction{Name: pc.Name, Namespace: pc.Namespace, Arguments: string(pc.Arguments)},
					})
				}
				respText = ""
			} else {
				respText = sim.Content
				finishReason = "stop"
			}
		} else {
			finishReason = "stop"
			respText = toolcalling.WithholdTransportEnvelope(respText)
		}
	}

	if blockedByContentPolicy(respText, toolCalls) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendContentBlockedError(w, respText)
		return
	}
	respText = withoutUnverifiedCompletionClaim(respText, hasTools, buildToolLedger(messages), toolCalls)

	// Enforce max_tokens on response text
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
			finishReason = "length"
		}
	}

	promptTok := countPromptTokens(messages, tools, toolChoice)
	completionTok := countTokens(respText) + outputProtocolTokens
	reasoningTok := countTokens(thinking)

	// Build choices
	choices := []map[string]any{
		{
			"index":         0,
			"text":          respText,
			"finish_reason": finishReason,
			"logprobs":      nil,
		},
	}

	// Add tool calls to response if present (non-standard extension for text_completion)
	response := map[string]any{
		"id":      fmt.Sprintf("cmpl-%s", uuid.New().String()),
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   cfg.OpenAIID,
		"choices": choices,
		"usage": map[string]any{
			"prompt_tokens":     promptTok,
			"completion_tokens": completionTok,
			"reasoning_tokens":  reasoningTok,
			"total_tokens":      promptTok + completionTok + reasoningTok,
			"usage_source":      usageSource(),
		},
	}

	if len(toolCalls) > 0 {
		openaiToolCalls := make([]map[string]any, len(toolCalls))
		for i, tc := range toolCalls {
			openaiToolCalls[i] = map[string]any{
				"index": i,
				"id":    tc.ID,
				"type":  "function",
				"function": map[string]string{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}
		}
		response["tool_calls"] = openaiToolCalls
	}

	api.sendJSON(w, http.StatusOK, response)

	// Cache conversation ID for session continuity
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// sendJSON sends a JSON response.
func (api *APIServer) sendJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(statusCode)

	json.NewEncoder(w).Encode(data)
}

// sendError sends an error response.
func (api *APIServer) sendError(w http.ResponseWriter, statusCode int, message string) {
	api.sendErrorCode(w, statusCode, openAIErrorCode(statusCode), message)
}

// sendErrorCode sends an error body in the OpenAI shape: the category in
// "type", a machine-readable string in "code". Callers with a specific code,
// such as an exhausted quota, pass it here; sendError derives the default one
// from the status.
func (api *APIServer) sendErrorCode(w http.ResponseWriter, statusCode int, code, message string) {
	api.sendJSON(w, statusCode, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(statusCode),
			"code":    code,
		},
	})
}

// sendSSEChunk sends a Server-Sent Events chunk in OpenAI chat.completion.chunk format.
func (api *APIServer) sendSSEChunk(w http.ResponseWriter, chunkID, model string, data map[string]any) {
	chunk := map[string]any{
		"id":      chunkID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         data,
				"finish_reason": nil,
			},
		},
	}

	jsonData, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
}

// sendSSEDone sends the final SSE chunk.
func (api *APIServer) sendSSEDone(w http.ResponseWriter, chunkID, model, finishReason string, usage map[string]any) {
	if finishReason == "" {
		finishReason = "stop"
	}
	chunk := map[string]any{
		"id":      chunkID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]any{
			{
				"index":         0,
				"delta":         map[string]any{},
				"finish_reason": finishReason,
			},
		},
	}

	if usage != nil {
		chunk["usage"] = usage
	}

	jsonData, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

// sendSSEError sends an error via SSE.
// sendSSEError reports a failed turn on an OpenAI-shaped stream.
//
// The failure used to be written as assistant content with finish_reason
// "stop", so a client stored the error text as the model's answer and had no
// way to tell a failure from a reply. OpenAI puts an error object on the data
// line instead, which is what a client checks for. [DONE] still follows, so a
// reader waiting for the terminator does not hang.
func (api *APIServer) sendSSEError(w http.ResponseWriter, op, chunkID, model string, err error) {
	status, code, message := streamErrorFields(op, err)
	body := map[string]any{
		"id":      chunkID,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"error": map[string]any{
			"message": message,
			"type":    openAIErrorType(status),
			"code":    code,
		},
	}

	jsonData, _ := json.Marshal(body)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

// sendAnthropicSSE sends an Anthropic-format SSE event.
func (api *APIServer) sendAnthropicSSE(w http.ResponseWriter, eventType string, data map[string]any) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
}

// uploadImagesAndAnnotate uploads any images found in message Images fields
// to the M365 backend and attaches the resulting docId annotations to the
// last message with images. This enables multimodal image input support.
// Limits for caller-supplied remote images. They bound how much work one
// request can make the proxy do on someone else's behalf.
const (
	remoteImageMaxBytes   = 20 << 20
	remoteImageMaxPerTurn = 16
	remoteImageTimeout    = 30 * time.Second
)

// errRemoteImageRejected marks a caller-supplied image URL the proxy refuses to
// fetch.
var errRemoteImageRejected = errors.New("remote image URL rejected")

// validateRemoteImageURL decides whether a caller-supplied image URL may be
// fetched. No credential travels on this request, so any public https host is
// acceptable; the guard exists to stop the proxy being used to reach addresses
// inside its own network.
func validateRemoteImageURL(rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", errRemoteImageRejected, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q is not https", errRemoteImageRejected, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: URL has no host", errRemoteImageRejected)
	}
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w: %q does not resolve", errRemoteImageRejected, host)
	}
	if slices.ContainsFunc(ips, ipDisallowed) {
		return fmt.Errorf("%w: %q resolves to a non-public address", errRemoteImageRejected, host)
	}
	return nil
}

// fetchRemoteImage downloads a caller-supplied image and returns it as base64
// with its media type. It sends no Authorization header, so a hostile URL
// learns nothing beyond the fact that the proxy fetched it.
func fetchRemoteImage(rawURL string) (base64Data, mediaType string, err error) {
	if err := validateRemoteImageURL(rawURL); err != nil {
		return "", "", err
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("fetch remote image: %w", err)
	}
	// Go's default agent string is rejected outright by several image CDNs,
	// including Wikimedia, so the request identifies the proxy instead.
	req.Header.Set("User-Agent", "M365Bridge/"+models.Version)
	req.Header.Set("Accept", "image/*")

	client := &http.Client{Timeout: remoteImageTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("fetch remote image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("fetch remote image: HTTP %d", resp.StatusCode)
	}

	// One extra byte distinguishes "exactly at the limit" from "truncated".
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteImageMaxBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read remote image: %w", err)
	}
	if len(body) > remoteImageMaxBytes {
		return "", "", fmt.Errorf("%w: larger than %d bytes", errRemoteImageRejected, remoteImageMaxBytes)
	}

	mediaType = resp.Header.Get("Content-Type")
	if semicolon := strings.IndexByte(mediaType, ';'); semicolon >= 0 {
		mediaType = strings.TrimSpace(mediaType[:semicolon])
	}
	if !strings.HasPrefix(mediaType, "image/") {
		return "", "", fmt.Errorf("%w: content type %q is not an image", errRemoteImageRejected, mediaType)
	}
	return base64.StdEncoding.EncodeToString(body), mediaType, nil
}

// resolveRemoteImages fetches every caller-supplied image URL in a message and
// drops the ones that cannot be fetched, so a single bad URL does not fail the
// whole turn.
func resolveRemoteImages(msg *payload.Message) {
	resolved := make([]payload.ImageData, 0, len(msg.Images))
	fetched := 0
	for _, img := range msg.Images {
		if img.RemoteURL == "" {
			resolved = append(resolved, img)
			continue
		}
		if fetched >= remoteImageMaxPerTurn {
			logging.Warnf("resolveRemoteImages: skipping image beyond the per-turn limit of %d", remoteImageMaxPerTurn)
			continue
		}
		fetched++
		data, mediaType, err := fetchRemoteImage(img.RemoteURL)
		if err != nil {
			logging.Errorf("resolveRemoteImages: %v", err)
			continue
		}
		resolved = append(resolved, payload.ImageData{
			Base64:    data,
			MediaType: mediaType,
			FileName:  "upload." + extFromMediaType(mediaType),
		})
	}
	msg.Images = resolved
}

func (api *APIServer) uploadImagesAndAnnotate(messages *[]payload.Message, convID string) {
	// Caller-supplied URLs arrive unfetched, so resolve them before the last
	// message with images is chosen: a message whose only images were rejected
	// must not be treated as an image turn.
	for i := range *messages {
		resolveRemoteImages(&(*messages)[i])
	}

	// Find the last message with images
	lastImgIdx := -1
	for i := range slices.Backward(*messages) {
		if len((*messages)[i].Images) > 0 {
			lastImgIdx = i
			break
		}
	}
	if lastImgIdx < 0 {
		return
	}

	logging.Infof("uploadImagesAndAnnotate: uploading %d images for message[%d] convID=%s", len((*messages)[lastImgIdx].Images), lastImgIdx, convID)

	// Use existing convID or generate a temporary UUID for upload
	uploadConvID := convID
	if uploadConvID == "" {
		uploadConvID = uuid.New().String()
	}

	msg := &(*messages)[lastImgIdx]
	for _, img := range msg.Images {
		result, err := api.m365Client.UploadFile(img.Base64, img.MediaType, img.FileName, uploadConvID, api.config.UserOID, api.config.TenantID)
		if err != nil {
			logging.Errorf("Image upload failed: %v", err)
			continue
		}
		if !result.IsSuccess {
			logging.Warnf("Image upload returned non-success: %+v", result)
			continue
		}

		fileType := strings.TrimPrefix(result.FileType, ".")
		msg.Annotations = append(msg.Annotations, payload.MessageAnnotation{
			ID:                    result.DocID,
			MessageAnnotationType: "ImageFile",
			MessageAnnotationMetadata: map[string]string{
				"@type":          "File",
				"annotationType": "File",
				"fileType":       fileType,
				"fileName":       img.FileName,
			},
		})
	}
}

// injectJSONMode injects JSON mode instructions into messages.
func (api *APIServer) injectJSONMode(messages *[]payload.Message) {
	instruction := "You MUST respond with valid JSON only. Do not include markdown code blocks, explanation, or any text outside the JSON object."

	for i, msg := range *messages {
		if msg.Role == "system" {
			(*messages)[i].Content = msg.Content + "\n" + instruction
			return
		}
	}

	*messages = append([]payload.Message{{Role: "system", Content: instruction}}, *messages...)
}

// sseKeepaliveInterval bounds how long a streaming response may stay silent.
// A tool-enabled turn buffers its text until the parse completes, and a turn on
// a tone that emits no reasoning writes nothing at all until the first content
// chunk arrives; measured at over nine seconds on a live turn. Clients drop a
// stream that delivers no bytes for around thirty seconds.
const sseKeepaliveInterval = 10 * time.Second

// streamOptions carries the OpenAI stream_options object.
type streamOptions struct {
	IncludeUsage *bool `json:"include_usage"`
}

// includeStreamUsage reports whether a streaming turn should carry a usage
// object.
//
// OpenAI defaults include_usage to false. This proxy has always sent usage on
// every streaming chat turn, and clients here read it, so an absent
// stream_options keeps that behaviour. Only an explicit false withholds it.
func includeStreamUsage(opts *streamOptions) bool {
	return opts == nil || opts.IncludeUsage == nil || *opts.IncludeUsage
}

// sseWriteTimeout bounds how long one SSE write may block. Without it a client
// that stopped reading but never closed its socket holds the handler goroutine
// and its upstream WebSocket open for the rest of the turn.
const sseWriteTimeout = 30 * time.Second

// refreshStreamDeadline extends the write deadline of an SSE response.
//
// The error is deliberately ignored: httptest.ResponseRecorder and any wrapped
// writer that hides the underlying connection report ErrNotSupported, and a
// stream must not fail because the deadline could not be armed.
func refreshStreamDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(sseWriteTimeout))
}

// writeSSEKeepalive emits an SSE comment. Every client ignores a comment line,
// so it keeps the connection alive without entering any field contract.
func writeSSEKeepalive(w http.ResponseWriter, flusher http.Flusher) error {
	if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeAnthropicKeepalive emits the ping event the Anthropic wire format
// defines for this purpose. An SSE comment would work too, but the SDK already
// knows ping and dispatches it at any point in the stream.
func writeAnthropicKeepalive(w http.ResponseWriter, flusher http.Flusher) error {
	if _, err := fmt.Fprint(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// nextStreamChunk returns the next upstream chunk, writing a keepalive frame
// through write for every interval that passes while the upstream is silent.
// The second result is false once the channel closes, the request context is
// canceled, or a keepalive write fails; the last two are how a stream to a gone
// client ends while it sits idle.
//
// The keepalive shares the caller's goroutine on purpose: http.ResponseWriter
// tolerates no concurrent writes, so a background ticker goroutine would
// interleave frames with the chunk loop.
func nextStreamChunk(ctx context.Context, ch <-chan client.StreamChunk, keepalive *time.Ticker, w http.ResponseWriter, write func() error) (client.StreamChunk, bool) {
	for {
		select {
		case <-ctx.Done():
			return client.StreamChunk{}, false
		case chunk, ok := <-ch:
			if ok {
				keepalive.Reset(sseKeepaliveInterval)
				refreshStreamDeadline(w)
			}
			return chunk, ok
		case <-keepalive.C:
			refreshStreamDeadline(w)
			if err := write(); err != nil {
				logging.Debugf("nextStreamChunk: keepalive write failed, ending stream: %v", err)
				return client.StreamChunk{}, false
			}
		}
	}
}

// withoutBackendToolCalls drops the tool calls M365 raises for its own built-ins
// (search, code_interpreter, trigger_plugin, invoke_action) and returns the
// finish reason the turn must end on instead.
//
// The client never declared those names, so it cannot execute them. This holds
// whether or not the request carried tools: a plain chat turn that triggers a
// server-side search would otherwise end on a tool_calls finish reason with a
// call the caller has no handler for.
func withoutBackendToolCalls(calls []client.ToolCall, finishReason string) ([]client.ToolCall, string) {
	if len(calls) == 0 {
		return calls, finishReason
	}
	if finishReason == "tool_calls" || finishReason == "tool_use" {
		finishReason = "stop"
	}
	return nil, finishReason
}

// chatAnthropicThinkingForOutput strips the simulated transport envelope from a
// complete thinking string for non-streaming and OpenAI callers. It delegates
// to toolcalling.FilterTransportThinking so every endpoint (streaming and
// non-streaming) applies the exact same filter.
func chatAnthropicThinkingForOutput(thinking string, simulated bool) string {
	if !simulated || thinking == "" {
		return thinking
	}
	return toolcalling.FilterTransportThinking(thinking)
}

// injectSimulatedPrompt replaces the last user message with a simulated-mode
// prompt that embeds the entire OpenAI request JSON and asks M365 Copilot to
// produce a valid chat.completion response in a single ```json block.
func injectSimulatedPrompt(messages *[]payload.Message, requestJSON, toolChoice, evidence string) {
	if len(*messages) == 0 {
		return
	}
	prompt := toolcalling.BuildSimulatedPrompt(requestJSON, true, toolChoice, evidence)
	for i := range slices.Backward(*messages) {
		if (*messages)[i].Role == "user" {
			suffix := ""
			if currentUserMessage := strings.TrimSpace((*messages)[i].Content); currentUserMessage != "" {
				suffix = "\n\nCURRENT USER MESSAGE\n" + currentUserMessage
			}
			(*messages)[i].Content = prompt + suffix
			break
		}
	}
}

// injectSimulatedPromptResponses replaces the converted Responses history with
// one canonical simulation message. The full history remains present exactly
// once inside requestJSON, avoiding duplicated context at the M365 layer.
func injectSimulatedPromptResponses(messages *[]payload.Message, requestJSON, toolChoice, evidence string) {
	prompt := toolcalling.BuildSimulatedPromptResponses(requestJSON, true, toolChoice, evidence)
	canonical := payload.Message{Role: "user", Content: prompt}
	for _, message := range *messages {
		canonical.Images = append(canonical.Images, message.Images...)
		canonical.Annotations = append(canonical.Annotations, message.Annotations...)
	}
	*messages = []payload.Message{canonical}
}

// injectSimulatedPromptAnthropic replaces the last user message with a
// simulated-mode prompt that embeds the entire Anthropic request JSON and asks
// M365 Copilot to produce a valid Anthropic Messages response in a single
// ```json block.
func injectSimulatedPromptAnthropic(messages *[]payload.Message, requestJSON, toolChoice, evidence string) {
	if len(*messages) == 0 {
		return
	}
	prompt := toolcalling.BuildSimulatedPromptAnthropic(requestJSON, true, toolChoice, evidence)
	for i := range slices.Backward(*messages) {
		if (*messages)[i].Role == "user" {
			suffix := ""
			if currentUserMessage := strings.TrimSpace((*messages)[i].Content); currentUserMessage != "" {
				suffix = "\n\nCURRENT USER MESSAGE\n" + currentUserMessage
			}
			(*messages)[i].Content = prompt + suffix
			break
		}
	}
}

// reasoningEffortRank orders the accepted effort values. Anything at medium or
// above is treated as a request to deliberate, which is the only distinction
// M365 can act on.
var reasoningEffortRank = map[string]int{
	"none":    0,
	"minimal": 1,
	"low":     2,
	"medium":  3,
	"high":    4,
	"xhigh":   5,
	"max":     6,
}

// reasoningEffortRequestsDeliberation validates the effort value and reports
// whether it asks for a reasoning tone. An unset block leaves the tone alone.
func reasoningEffortRequestsDeliberation(reasoning *responsesReasoning) (bool, error) {
	if reasoning == nil {
		return false, nil
	}
	effort := strings.ToLower(strings.TrimSpace(reasoning.Effort))
	if effort == "" {
		return false, nil
	}
	rank, ok := reasoningEffortRank[effort]
	if !ok {
		return false, fmt.Errorf(
			"unsupported reasoning effort %q; use %s",
			reasoning.Effort,
			strings.Join(models.ReasoningEffortNames(), ", "),
		)
	}
	return rank >= reasoningEffortRank["medium"], nil
}

// applyReasoningEffort returns the model config to use, redirecting to the
// model key's reasoning variant when the caller asked to deliberate and such a
// variant exists. A model without a variant, or a key that is already one, is
// left untouched: M365 has no separate effort dial, so the tone is the only
// lever.
func applyReasoningEffort(modelKey string, cfg models.ModelConfig, deliberate bool) models.ModelConfig {
	if !deliberate {
		return cfg
	}
	// The requested name is resolved to registry keys first, because a caller
	// that took its id from /v1/models sends the advertised id ("gpt-5.5") and
	// appending the suffix to that never matches the key ("gpt5.5-reasoning").
	// The catalog advertised effort support for the same id, so the request ran
	// on the non-reasoning tone and the caller had no way to notice.
	for _, key := range models.RegistryKeysFor(modelKey) {
		if strings.HasSuffix(key, "-reasoning") {
			return cfg
		}
		// The registry is consulted directly rather than through FindModel,
		// because a model without a reasoning variant must be left alone rather
		// than reported as an unknown model.
		variantKey := key + "-reasoning"
		if variant, ok := models.ModelRegistry[variantKey]; ok {
			logging.Infof("applyReasoningEffort: routing %s to %s", modelKey, variantKey)
			return variant
		}
	}
	return cfg
}

// validateToolResultMessages rejects a tool result that answers nothing. The
// conversation payload flattens results into text, so an id the request never
// declared would silently reach the model as a plausible-looking result and
// desynchronize the client's own tool loop.
//
// When the request declares no tool calls at all, the id cannot be checked
// against anything: a client that trimmed its history to stay under the
// context window legitimately sends results whose calls are no longer present.
// Only a missing id is rejected in that case.
func validateToolResultMessages(messages []payload.Message) error {
	// A repeated id makes the loop ambiguous: neither the client nor this
	// server can tell which call a later result answers.
	known := make(map[string]bool)
	for i := range messages {
		for _, call := range messages[i].ToolCalls {
			if call.ID == "" {
				continue
			}
			if known[call.ID] {
				return fmt.Errorf("tool call id %q is declared more than once in this request", call.ID)
			}
			known[call.ID] = true
		}
	}

	answered := make(map[string]bool)
	for i := range messages {
		if messages[i].Role == "tool" && messages[i].ToolCallID == "" {
			return errors.New(`a message with role "tool" is missing tool_call_id`)
		}
		for _, result := range messages[i].ToolResults {
			if result.ID == "" {
				return errors.New("a tool result is missing the id of the tool call it answers")
			}
			if len(known) > 0 && !known[result.ID] {
				return fmt.Errorf("tool result %q does not answer any tool call in this request", result.ID)
			}
			if answered[result.ID] {
				return fmt.Errorf("tool call %q is answered more than once in this request", result.ID)
			}
			answered[result.ID] = true
		}
	}
	return nil
}

// activeToolMessages returns the messages belonging to the current user turn.
//
// The turn starts at the last user message that carries neither a tool result
// nor a progress note: in the Anthropic shape every tool result arrives as a
// user message, and a progress note is transport metadata, so taking the plain
// last user message would land inside the loop instead of at its start.
// Earlier turns stay out of the round count but remain in the history, where
// they are still evidence.
func activeToolMessages(messages []payload.Message) []payload.Message {
	start := 0
	for i := range messages {
		if messages[i].Role != "user" || len(messages[i].ToolResults) > 0 || messages[i].ToolProgress {
			continue
		}
		start = i
	}
	return messages[start:]
}

// buildToolLedger reconstructs the evidence of the client-driven tool loop from
// the current user turn. The server holds no state across such a loop, so the
// incoming history is the only record of what already ran.
func buildToolLedger(messages []payload.Message) toolcalling.Ledger {
	active := activeToolMessages(messages)

	var calls []toolcalling.LedgerCall
	var results []toolcalling.LedgerResult
	rounds := 0
	for i := range active {
		if len(active[i].ToolCalls) > 0 {
			rounds++
		}
		for _, call := range active[i].ToolCalls {
			calls = append(calls, toolcalling.LedgerCall{
				ID:        call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
		}
		for _, result := range active[i].ToolResults {
			results = append(results, toolcalling.LedgerResult{
				ID:      result.ID,
				Content: result.Content,
			})
		}
	}
	return toolcalling.BuildLedger(calls, results, rounds)
}

// toolChoiceForcesACall reports whether the caller demanded a tool call in this
// turn, either by name or with "required"/"any".
func toolChoiceForcesACall(toolChoice string) bool {
	switch toolChoice {
	case "", "auto", "none":
		return false
	default:
		return true
	}
}

// dropSettledToolCalls removes a tool call the model is issuing for at least
// the third time with the same arguments, whose result is already in this
// turn's history.
//
// A call the caller demanded through tool_choice is forwarded regardless:
// refusing it would contradict the request. The drop is not recorded in
// DroppedCalls either, because that list drives a corrective re-ask and
// re-asking would produce the same call again. When nothing survives, the turn
// becomes a plain answer, which needs substitute text because the parser clears
// the content whenever tool calls are present.
func dropSettledToolCalls(ledger toolcalling.Ledger, toolChoice string, sim toolcalling.SimulatedResult) toolcalling.SimulatedResult {
	if len(sim.ToolCalls) == 0 || toolChoiceForcesACall(toolChoice) {
		return sim
	}
	kept, dropped := ledger.FilterRepeated(sim.ToolCalls)
	if len(dropped) == 0 {
		return sim
	}
	for _, call := range dropped {
		logging.Warnf("dropSettledToolCalls: dropping %q, its result is already in this turn's history", call.Name)
	}
	sim.ToolCalls = kept
	if len(kept) == 0 {
		sim.FinishReason = "stop"
		if strings.TrimSpace(sim.Content) == "" {
			sim.Content = toolcalling.RepeatedCallsNotice
		}
	}
	return sim
}

// toolRoundLimitCode marks a client-driven tool loop that ran past its cap.
const toolRoundLimitCode = "tool_round_limit"

// exceededToolRoundLimit reports whether the client has driven more tool rounds
// within this user turn than the configuration allows.
func (api *APIServer) exceededToolRoundLimit(ledger toolcalling.Ledger) bool {
	limit := api.config.MaxToolRounds
	if limit <= 0 {
		limit = models.DefaultMaxToolRounds
	}
	return ledger.Rounds > limit
}

// sendToolRoundLimitError stops a client-driven tool loop that is not
// converging. HTTP 409 is not a status the Anthropic SDK expects, but an
// explicit refusal is better than answering forever while the client keeps
// asking for one more round.
func (api *APIServer) sendToolRoundLimitError(w http.ResponseWriter, ledger toolcalling.Ledger) {
	limit := api.config.MaxToolRounds
	if limit <= 0 {
		limit = models.DefaultMaxToolRounds
	}
	logging.Warnf(
		"tool round limit reached: rounds=%d limit=%d completed=%d repeatedCall=%v repeatedFailure=%v",
		ledger.Rounds, limit, len(ledger.Completed), ledger.RepeatedCall, ledger.RepeatedFailure,
	)
	message := fmt.Sprintf(
		"tool round limit reached: this turn drove %d tool rounds with %d completed calls, above the limit of %d; raise M365_MAX_TOOL_ROUNDS or start a new turn",
		ledger.Rounds, len(ledger.Completed), limit,
	)
	api.sendErrorCode(w, http.StatusConflict, toolRoundLimitCode, message)
}

// anthropicToolChoiceString normalizes the Anthropic tool_choice field to a
// string ("any", "auto", "tool", or "") for prompt-building purposes.
func anthropicToolChoiceString(toolChoice map[string]any) string {
	if toolChoice == nil {
		return ""
	}
	if t, ok := toolChoice["type"].(string); ok {
		return t
	}
	return ""
}

// anthropicToolChoiceEnforcement resolves the Anthropic tool_choice field to
// the value the parser enforces: "none", "auto", "any", or the name of the one
// tool the caller pinned. It differs from anthropicToolChoiceString, which
// reports the raw type and would turn a pinned tool into the literal name
// "tool".
func anthropicToolChoiceEnforcement(toolChoice map[string]any) string {
	kind := anthropicToolChoiceString(toolChoice)
	if kind != "tool" {
		return kind
	}
	name, _ := toolChoice["name"].(string)
	return name
}

// toolChoiceString normalizes the tool_choice field to a string ("auto",
// "required", "none", or a function name) for prompt-building purposes.
func toolChoiceString(toolChoice any) string {
	if toolChoice == nil {
		return ""
	}
	if s, ok := toolChoice.(string); ok {
		return s
	}
	if m, ok := toolChoice.(map[string]any); ok {
		if fn, ok := m["function"].(map[string]any); ok {
			if name, ok := fn["name"].(string); ok {
				return name
			}
		}
	}
	return ""
}

const simulatedToolCallRequiredCode = "simulated_tool_call_required"
const upstreamEmptyResponseCode = "upstream_empty_response"

var errSimulatedToolCallRequired = errors.New(simulatedToolCallRequiredCode)

type responsesToolPolicy struct {
	simulate         bool
	required         bool
	requiredName     string
	promptChoice     string
	allowedToolNames []string
	tools            []toolcalling.ToolDef
	// ledger carries the evidence of the client-driven tool loop. The
	// Responses input is collapsed into one canonical prompt message before
	// parsing, so the ledger has to travel with the policy instead of being
	// rebuilt from the messages the parser sees.
	ledger toolcalling.Ledger
}

// allows reports whether this policy still permits a call to the named tool.
// allowedToolNames is already narrowed to the pinned tool when tool_choice
// names one, so membership is the whole test.
func (p responsesToolPolicy) allows(name string) bool {
	return slices.Contains(p.allowedToolNames, name)
}

type responsesSimulationResult struct {
	content      string
	toolCalls    []client.ToolCall
	finishReason string
}

func newResponsesToolPolicy(tools []toolcalling.ToolDef, toolChoice any) (responsesToolPolicy, error) {
	allNames := responsesToolNames(tools)
	knownNames := make(map[string]bool, len(tools))
	for _, name := range allNames {
		knownNames[name] = true
	}

	policy := responsesToolPolicy{
		// With only web search declared there is nothing for the client to
		// run, so the request takes the plain answer path.
		simulate:         len(allNames) > 0,
		promptChoice:     "auto",
		allowedToolNames: allNames,
		tools:            tools,
	}

	switch choice := toolChoice.(type) {
	case nil:
		// Responses defaults to auto when tools are present.
	case string:
		normalized := strings.ToLower(strings.TrimSpace(choice))
		switch normalized {
		case "", "auto":
		case "none":
			policy.simulate = false
			policy.promptChoice = "none"
			policy.allowedToolNames = nil
		case "required":
			policy.required = true
			policy.promptChoice = "required"
		default:
			if strings.EqualFold(choice, toolcalling.WebSearchToolName) {
				// The backend performs the search itself, so the pin is
				// satisfied by the answer rather than by a call.
				break
			}
			if !knownNames[choice] {
				return responsesToolPolicy{}, fmt.Errorf("invalid Responses tool_choice %q", choice)
			}
			policy.required = true
			policy.requiredName = choice
			policy.promptChoice = choice
			policy.allowedToolNames = []string{choice}
		}
	case map[string]any:
		name, _ := choice["name"].(string)
		choiceType, _ := choice["type"].(string)
		if name == "" {
			if function, ok := choice["function"].(map[string]any); ok {
				name, _ = function["name"].(string)
			}
		}
		if name == "" && choiceType != "" && choiceType != "function" && choiceType != "custom" {
			name = choiceType
		}
		name = strings.TrimSpace(name)
		if name == "" || !knownNames[name] {
			return responsesToolPolicy{}, fmt.Errorf("invalid Responses named tool_choice %q", name)
		}
		policy.required = true
		policy.requiredName = name
		policy.promptChoice = name
		policy.allowedToolNames = []string{name}
	default:
		return responsesToolPolicy{}, fmt.Errorf("invalid Responses tool_choice type %T", toolChoice)
	}

	if policy.simulate && len(policy.allowedToolNames) == 0 {
		return responsesToolPolicy{}, errors.New("responses tools must include at least one function name")
	}
	if policy.required && !policy.simulate {
		return responsesToolPolicy{}, errors.New("responses tool_choice requires at least one tool")
	}
	return policy, nil
}

func responsesToolName(tool toolcalling.ToolDef) string {
	name := strings.TrimSpace(toolcalling.ToolName(&tool))
	if name == "" && tool.Type != "" && tool.Type != "function" && tool.Type != "custom" {
		name = tool.Type
	}
	return name
}

func responsesToolNames(tools []toolcalling.ToolDef) []string {
	names := make([]string, 0, len(tools))
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		// Web search is a server-side built-in: the backend answers with
		// search results inline and the client has nothing to execute, so a
		// call to it must never be routed even though the declaration stays
		// visible in the prompt.
		if toolcalling.IsWebSearchTool(&tool) {
			continue
		}
		name := responsesToolName(tool)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

func responsesToolKey(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + "/" + name
}

func responsesToolTypes(tools []toolcalling.ToolDef) map[string]string {
	types := make(map[string]string, len(tools))
	for _, tool := range tools {
		name := responsesToolName(tool)
		if name == "" {
			continue
		}
		toolType := tool.Type
		if toolType == "" {
			toolType = "function"
		}
		types[responsesToolKey(tool.Namespace, name)] = toolType
	}
	return types
}

func responsesToolDefsFromRaw(raw any) []toolcalling.ToolDef {
	return responsesToolDefsFromRawNamespace(raw, "")
}

func responsesToolDefsFromRawNamespace(raw any, inheritedNamespace string) []toolcalling.ToolDef {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	var definitions []toolcalling.ToolDef
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolType, _ := tool["type"].(string)
		if toolType == "namespace" {
			namespace, _ := tool["name"].(string)
			if namespace == "" {
				namespace = inheritedNamespace
			}
			definitions = append(
				definitions,
				responsesToolDefsFromRawNamespace(tool["tools"], namespace)...,
			)
			continue
		}
		name, _ := tool["name"].(string)
		if name == "" && toolType != "" && toolType != "function" && toolType != "custom" {
			name = toolType
		}
		if name == "" {
			continue
		}
		namespace, _ := tool["namespace"].(string)
		if namespace == "" {
			namespace = inheritedNamespace
		}
		description, _ := tool["description"].(string)
		definition := toolcalling.ToolDef{
			Type:        toolType,
			Name:        name,
			Namespace:   namespace,
			Description: description,
		}
		if parameters, ok := tool["parameters"].(map[string]any); ok {
			definition.Parameters = parameters
		}
		if inputSchema, ok := tool["input_schema"].(map[string]any); ok {
			definition.InputSchema = inputSchema
		}
		if nestedTools, ok := tool["tools"].([]any); ok {
			definition.Tools = responsesToolDefsFromRawNamespace(nestedTools, namespace)
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func mergeLoadedResponsesTools(input any, tools []toolcalling.ToolDef) []toolcalling.ToolDef {
	items, ok := input.([]any)
	if !ok {
		return tools
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		name := responsesToolName(tool)
		if name != "" {
			seen[responsesToolKey(tool.Namespace, name)] = true
		}
	}
	for _, item := range items {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := record["type"].(string)
		if itemType != "tool_search_output" && itemType != "additional_tools" {
			continue
		}
		for _, tool := range responsesToolDefsFromRaw(record["tools"]) {
			name := responsesToolName(tool)
			key := responsesToolKey(tool.Namespace, name)
			if name == "" || seen[key] {
				continue
			}
			seen[key] = true
			tools = append(tools, tool)
		}
	}
	return tools
}

func buildResponsesToolCallItem(callID string, call client.ToolCall, toolTypes map[string]string, status string) map[string]any {
	toolKey := responsesToolKey(call.Function.Namespace, call.Function.Name)
	if toolTypes[toolKey] == "tool_search" {
		var arguments any
		if err := json.Unmarshal([]byte(call.Function.Arguments), &arguments); err != nil || arguments == nil {
			arguments = map[string]any{"query": call.Function.Arguments}
		}
		return map[string]any{
			"id":        callID,
			"type":      "tool_search_call",
			"execution": "client",
			"status":    status,
			"call_id":   callID,
			"arguments": arguments,
		}
	}
	if toolTypes[toolKey] == "custom" {
		// A custom tool takes free-form input rather than JSON arguments, so it
		// travels as its own item type. The item id is derived from the call id
		// instead of minted fresh, because the streaming path builds the item
		// twice (in_progress then completed) and a client correlates the two by
		// that id.
		item := map[string]any{
			"id":      "ctc_" + strings.TrimPrefix(callID, "call_"),
			"type":    "custom_tool_call",
			"status":  status,
			"call_id": callID,
			"name":    call.Function.Name,
			"input":   "",
		}
		if status == "completed" {
			item["input"] = call.Function.Arguments
		}
		return item
	}
	item := map[string]any{
		"id":      callID,
		"type":    "function_call",
		"status":  status,
		"call_id": callID,
		"name":    call.Function.Name,
	}
	if call.Function.Namespace != "" {
		item["namespace"] = call.Function.Namespace
	}
	if status == "completed" {
		item["arguments"] = call.Function.Arguments
	} else {
		item["arguments"] = ""
	}
	return item
}

func resolveResponsesToolNamespace(
	name string,
	namespace string,
	tools []toolcalling.ToolDef,
) (string, bool) {
	namespaces := make(map[string]bool)
	for _, tool := range tools {
		if responsesToolName(tool) != name {
			continue
		}
		if namespace != "" {
			if tool.Namespace == namespace {
				return namespace, true
			}
			continue
		}
		namespaces[tool.Namespace] = true
	}
	if namespace != "" || len(namespaces) != 1 {
		return "", false
	}
	for candidate := range namespaces {
		return candidate, true
	}
	return "", false
}

func shouldResetResponsesSession(content string, toolCalls []client.ToolCall, err error) bool {
	return err != nil || (strings.TrimSpace(content) == "" && len(toolCalls) == 0)
}

func parseResponsesSimulation(text string, policy responsesToolPolicy) (responsesSimulationResult, error) {
	result := responsesSimulationResult{
		content:      text,
		finishReason: "stop",
	}
	simulated := toolcalling.ParseSimulatedResponseResponses(text, policy.allowedToolNames, toolcalling.ContractsFor(policy.tools))
	// A grammar tool's body arrives unfenced, either as the lone bridge
	// envelope or as bare source. It may be the whole reply, or it may end up
	// as the extracted content of an otherwise valid envelope; either way it
	// would reach the client as escaped source in an assistant message instead
	// of a call. The candidate is whatever text is about to be forwarded.
	if len(simulated.ToolCalls) == 0 {
		candidate := text
		if simulated.HasPayload {
			candidate = simulated.Content
		}
		if call, ok := toolcalling.GrammarBodyCall(candidate, policy.tools, policy.allows); ok {
			logging.Infof("parseResponsesSimulation: claimed an unfenced grammar body as a %q call", call.Name)
			simulated.HasPayload = true
			simulated.FinishReason = "tool_calls"
			simulated.Content = ""
			simulated.ToolCalls = []toolcalling.ToolCall{call}
		}
	}
	if !policy.required {
		simulated = dropSettledToolCalls(policy.ledger, "", simulated)
	}
	if simulated.HasPayload {
		result.content = simulated.Content
		if len(simulated.ToolCalls) > 0 {
			result.finishReason = "tool_calls"
			for _, parsed := range simulated.ToolCalls {
				namespace, ok := resolveResponsesToolNamespace(
					parsed.Name,
					parsed.Namespace,
					policy.tools,
				)
				if !ok {
					continue
				}
				result.toolCalls = append(result.toolCalls, client.ToolCall{
					ID:   parsed.ID,
					Type: "function",
					Function: client.ToolCallFunction{
						Name:      parsed.Name,
						Namespace: namespace,
						Arguments: string(parsed.Arguments),
					},
				})
			}
		}
	}
	if len(result.toolCalls) > 0 && strings.TrimSpace(result.content) == "" {
		result.content = "I'm using the relevant tool now and will continue with its result."
	}

	if policy.required && len(result.toolCalls) == 0 {
		if policy.requiredName != "" {
			return responsesSimulationResult{}, fmt.Errorf("%w: required tool %q was not emitted", errSimulatedToolCallRequired, policy.requiredName)
		}
		return responsesSimulationResult{}, fmt.Errorf("%w: no valid client tool call was emitted", errSimulatedToolCallRequired)
	}
	return result, nil
}

func parseResponsesSimulationWithRetry(
	text string,
	policy responsesToolPolicy,
	requiredRetry func() (string, error),
	emptyRetry func() (string, error),
) (responsesSimulationResult, error) {
	result, err := parseResponsesSimulation(text, policy)
	if err == nil {
		if emptyRetry == nil ||
			!responsesResultEmpty(result.content, result.toolCalls) {
			return result, nil
		}
		retryText, retryErr := emptyRetry()
		if retryErr != nil {
			return responsesSimulationResult{}, fmt.Errorf(
				"empty simulated response retry failed: %w",
				retryErr,
			)
		}
		return parseResponsesSimulation(retryText, policy)
	}
	if requiredRetry == nil ||
		!errors.Is(err, errSimulatedToolCallRequired) {
		return result, err
	}

	for range 2 {
		retryText, retryErr := requiredRetry()
		if retryErr != nil {
			return responsesSimulationResult{}, fmt.Errorf(
				"%w: retry failed: %v",
				errSimulatedToolCallRequired,
				retryErr,
			)
		}
		result, err = parseResponsesSimulation(retryText, policy)
		if err == nil || !errors.Is(err, errSimulatedToolCallRequired) {
			return result, err
		}
	}
	return result, err
}

func responsesSimulationRetryMessages(
	messages []payload.Message,
	policy responsesToolPolicy,
) []payload.Message {
	retryInstruction := "RETRY: The previous result was invalid. "
	if policy.requiredName != "" {
		retryInstruction += fmt.Sprintf(
			"Return exactly one valid tool call named %q inside the required chat-completion JSON envelope. Plain content is invalid.",
			policy.requiredName,
		)
	} else {
		retryInstruction += fmt.Sprintf(
			"Return at least one valid tool call using only these client tools: %s. Plain content is invalid.",
			strings.Join(policy.allowedToolNames, ", "),
		)
	}
	retryInstruction += " Every tool call MUST include all of its schema-required fields, each with a concrete non-empty value; a tool call with a missing or empty required field is invalid."

	retried := append([]payload.Message(nil), messages...)
	for index := range slices.Backward(retried) {
		if retried[index].Role == "user" {
			retried[index].Content += "\n\n" + retryInstruction
			return retried
		}
	}
	return append(retried, payload.Message{
		Role:    "user",
		Content: retryInstruction,
	})
}

func responsesReasoningForOutput(thinking string, simulated bool) string {
	if simulated {
		return ""
	}
	return thinking
}

func responsesResultEmpty(text string, toolCalls []client.ToolCall) bool {
	return strings.TrimSpace(text) == "" && len(toolCalls) == 0
}

var responsesPlainEmptyRetryDelays = []time.Duration{
	10 * time.Second,
	30 * time.Second,
}

func responsesEmptyRetrySchedule(simulateTools bool) []time.Duration {
	if simulateTools {
		return responsesPlainEmptyRetryDelays[:1]
	}
	return responsesPlainEmptyRetryDelays
}

type responsesConversationResult struct {
	text           string
	thinking       string
	toolCalls      []client.ToolCall
	finishReason   string
	conversationID string
}

type responsesConversationCall func(
	context.Context,
	string,
) (responsesConversationResult, error)

func waitForResponsesEmptyRetry(
	ctx context.Context,
	delay time.Duration,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func responsesConversationWithEmptyRetry(
	ctx context.Context,
	initialConversationID string,
	retryDelays []time.Duration,
	onRetry func(),
	call responsesConversationCall,
) (responsesConversationResult, error) {
	conversationID := initialConversationID
	for attempt := 0; ; attempt++ {
		result, err := call(ctx, conversationID)
		if err != nil ||
			!responsesResultEmpty(result.text, result.toolCalls) ||
			attempt >= len(retryDelays) {
			return result, err
		}

		logging.Warnf(
			"Responses upstream completed empty; retrying attempt=%d/%d",
			attempt+2,
			len(retryDelays)+1,
		)
		if onRetry != nil {
			onRetry()
		}
		if err := waitForResponsesEmptyRetry(
			ctx,
			retryDelays[attempt],
		); err != nil {
			return responsesConversationResult{}, err
		}
		conversationID = ""
	}
}

type responsesStreamCall func(
	context.Context,
	string,
) <-chan client.StreamChunk

func responsesStreamWithEmptyRetry(
	ctx context.Context,
	initialConversationID string,
	retryDelays []time.Duration,
	simulatedTransport bool,
	onRetry func(),
	call responsesStreamCall,
) <-chan client.StreamChunk {
	output := make(chan client.StreamChunk)
	go func() {
		defer close(output)
		conversationID := initialConversationID
		emit := func(chunk client.StreamChunk) bool {
			select {
			case output <- chunk:
				return true
			case <-ctx.Done():
				return false
			}
		}

		for attempt := 0; ; attempt++ {
			stream := call(ctx, conversationID)
			sawVisibleChunk := false
			sawFinal := false

			for chunk := range stream {
				if chunk.Error != nil {
					emit(chunk)
					return
				}
				if chunk.IsFinal {
					sawFinal = true
					if sawVisibleChunk || attempt >= len(retryDelays) {
						emit(chunk)
						return
					}
					break
				}
				if simulatedTransport {
					// Simulated prompts can put the transport envelope in the
					// upstream thinking channel. The Responses handler only
					// needs raw text for its safe content extractor.
					chunk.Thinking = ""
				}
				if chunk.Text == "" && chunk.Thinking == "" {
					continue
				}
				sawVisibleChunk = true
				if !emit(chunk) {
					return
				}
			}

			if !sawFinal {
				if ctx.Err() == nil {
					emit(client.StreamChunk{Error: client.ErrConnectionClosed})
				}
				return
			}
			if attempt >= len(retryDelays) {
				return
			}

			logging.Warnf(
				"Responses upstream stream completed empty; retrying attempt=%d/%d",
				attempt+2,
				len(retryDelays)+1,
			)
			if onRetry != nil {
				onRetry()
			}
			if err := waitForResponsesEmptyRetry(
				ctx,
				retryDelays[attempt],
			); err != nil {
				return
			}
			conversationID = ""
		}
	}()
	return output
}

func buildResponsesFailedEvent(
	responseID, model, code, message string,
	sequenceNumber int,
) map[string]any {
	return map[string]any{
		"type":            "response.failed",
		"sequence_number": sequenceNumber,
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "failed",
			"model":  model,
			"error": map[string]any{
				"message": message,
				"type":    "server_error",
				"code":    code,
			},
		},
	}
}

func writeResponsesServerError(w http.ResponseWriter, stream bool, responseID, model, code, message string) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		event := buildResponsesFailedEvent(
			responseID,
			model,
			code,
			message,
			0,
		)
		jsonData, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "server_error",
			"code":    code,
		},
	})
}

func writeResponsesSimulationError(w http.ResponseWriter, stream bool, responseID, model string, err error) {
	writeResponsesServerError(
		w,
		stream,
		responseID,
		model,
		simulatedToolCallRequiredCode,
		err.Error(),
	)
}

func writeResponsesUpstreamEmptyError(w http.ResponseWriter, stream bool, responseID, model string) {
	writeResponsesServerError(
		w,
		stream,
		responseID,
		model,
		upstreamEmptyResponseCode,
		"M365 returned an empty response without a completion message",
	)
}

// parseModelSessionID splits a model string of the form "modelKey:sessionID"
// into its components. If there is no colon, sessionID is empty.
// This allows clients that cannot send custom headers/body fields (e.g. Droid
// CLI) to encode a session ID directly in the model name, e.g.
// "gpt5.5-reasoning:dev-test-session-001".
//
// An empty model key defaults to "gpt5.5-reasoning", the reasoning tone that is
// reliable for tool calling, rather than falling back to the "auto" (Magic)
// tone. This keeps the text endpoints consistent with the conversation and
// image routes, which already default empty models to the same key.
func parseModelSessionID(model string) (modelKey, sessionID string) {
	modelKey, sessionID, found := strings.Cut(model, ":")
	if !found {
		modelKey = model
	}
	if modelKey == "" {
		modelKey = "gpt5.5-reasoning"
	}
	return modelKey, sessionID
}

// toolNamesFromDefs extracts the function names from a slice of tool
// definitions, for filtering M365-invented tool calls (e.g. code_interpreter)
// out of simulated responses.
func toolNamesFromDefs(tools []toolcalling.ToolDef) []string {
	return responsesToolNames(tools)
}

// fimToChat converts FIM (fill-in-the-middle) prompts to chat format.
func (api *APIServer) fimToChat(prompt, suffix string) []payload.Message {
	if suffix != "" {
		return []payload.Message{
			{
				Role:    "user",
				Content: fmt.Sprintf("Complete the middle of the following text naturally.\n\n--- BEGIN TEXT ---\n%s\n--- MIDDLE ---\n%s\n--- END ---\n\nWrite only the middle part that connects the two sections.", prompt, suffix),
			},
		}
	}

	return []payload.Message{
		{
			Role:    "user",
			Content: fmt.Sprintf("Continue writing from this point:\n\n%s", prompt),
		},
	}
}

// tokenEncoder is the tiktoken encoder used for every token count. o200k_base
// is the encoding of the GPT-5 family the backend serves; cl100k_base is kept
// as a fallback because the vocabulary is fetched at first use and the fetch
// can fail.
var tokenEncoder *tiktoken.Tiktoken

// tokenEncodingName names the encoding actually in use, for usage reporting.
var tokenEncodingName string

// Usage source values reported alongside the token counts, so a caller can tell
// a real BPE count from the character estimate that stands in when the
// vocabulary could not be fetched.
const (
	usageSourceHeuristic = "heuristic_character_estimate"
)

func init() {
	for _, name := range []string{"o200k_base", "cl100k_base"} {
		enc, err := tiktoken.GetEncoding(name)
		if err != nil {
			logging.Warnf("token encoding %s unavailable: %v", name, err)
			continue
		}
		tokenEncoder = enc
		tokenEncodingName = name
		break
	}
	if tokenEncoder == nil {
		logging.Warn("no token encoding available, falling back to a character estimate")
	}
}

// usageSource reports how the token counts in a response were produced.
func usageSource() string {
	if tokenEncoder == nil {
		return usageSourceHeuristic
	}
	return "tiktoken_" + tokenEncodingName + "_estimate"
}

// heuristicTokenCount estimates tokens from character classes when no encoding
// is available. Latin text averages roughly four characters per token while
// CJK and other non-ASCII scripts average closer to one, so a single divisor
// would be wrong for one of them.
func heuristicTokenCount(text string) int {
	ascii, other := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		if r <= 0x7f {
			ascii++
		} else {
			other++
		}
	}
	if ascii == 0 && other == 0 {
		return 0
	}
	return max(ascii/4+other, 1)
}

// countTokens returns the real BPE token count using tiktoken.
func countTokens(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	if tokenEncoder != nil {
		return len(tokenEncoder.Encode(text, nil, nil))
	}
	return heuristicTokenCount(text)
}

// Protocol framing costs. The request carries structure the message text does
// not represent: role markers, tool schemas and the priming that starts the
// reply. These are conservative estimates of that framing, not billing figures
// from the backend.
const (
	requestProtocolTokens    = 4
	messageProtocolTokens    = 4
	toolProtocolTokens       = 6
	toolChoiceProtocolTokens = 2
	replyPrimingTokens       = 3
	outputProtocolTokens     = 3
)

// countPromptTokens estimates the prompt cost of a request from its parts.
//
// The previous count ran tiktoken over fmt.Sprint of the message slice, which
// counted Go struct field names and slice punctuation as prompt content. Tools
// and tool_choice were not counted at all even though they travel in the
// request.
func countPromptTokens(messages []payload.Message, tools []toolcalling.ToolDef, toolChoice string) int {
	total := requestProtocolTokens + replyPrimingTokens
	for i := range messages {
		total += messageProtocolTokens + countTokens(messages[i].Role) + countTokens(messages[i].Content)
	}
	for i := range tools {
		total += toolProtocolTokens
		if encoded, err := json.Marshal(tools[i]); err == nil {
			total += countTokens(string(encoded))
		}
	}
	// A tool choice only travels with the tools it selects from. The Responses
	// policy defaults promptChoice to "auto" even for a request that declares
	// none, so billing it unconditionally would charge every toolless turn for
	// framing the backend never received.
	if len(tools) > 0 && strings.TrimSpace(toolChoice) != "" {
		total += toolChoiceProtocolTokens
	}
	return total
}

// openAIUsage builds the usage object for the OpenAI wire format. The buffered
// coding-tool responder has no streaming loop to accumulate counts in, so it
// takes the finished text and counts it the same way the streaming handlers do.
func openAIUsage(messages []payload.Message, tools []toolcalling.ToolDef, toolChoice, answer, thinking string) map[string]any {
	promptTok := countPromptTokens(messages, tools, toolChoice)
	completionTok := countTokens(answer) + outputProtocolTokens
	reasoningTok := countTokens(thinking)
	return map[string]any{
		"prompt_tokens":     promptTok,
		"completion_tokens": completionTok,
		"reasoning_tokens":  reasoningTok,
		"total_tokens":      promptTok + completionTok + reasoningTok,
		"usage_source":      usageSource(),
	}
}

// anthropicUsage builds the usage object for the Anthropic wire format. The
// field names differ from OpenAI's and the format carries no total, but the
// counts behind them are the same.
func anthropicUsage(messages []payload.Message, tools []toolcalling.ToolDef, toolChoice, answer, thinking string) map[string]any {
	return map[string]any{
		"input_tokens":     countPromptTokens(messages, tools, toolChoice),
		"output_tokens":    countTokens(answer) + outputProtocolTokens,
		"reasoning_tokens": countTokens(thinking),
		"usage_source":     usageSource(),
	}
}

// truncateToTokens truncates text to at most maxTokens tokens using tiktoken.
// Returns the truncated text and true if truncation occurred.
func truncateToTokens(text string, maxTokens int) (string, bool) {
	if maxTokens <= 0 {
		return text, false
	}
	if tokenEncoder != nil {
		tokens := tokenEncoder.Encode(text, nil, nil)
		if len(tokens) <= maxTokens {
			return text, false
		}
		return tokenEncoder.Decode(tokens[:maxTokens]), true
	}
	// Without an encoder there is no token boundary to cut on, so the word
	// split stands in; heuristicTokenCount decides whether a cut is needed.
	if heuristicTokenCount(text) <= maxTokens {
		return text, false
	}
	words := strings.Split(text, " ")
	if len(words) <= maxTokens {
		return text, false
	}
	return strings.Join(words[:maxTokens], " "), true
}

func limitResponsesStreamDelta(
	published string,
	delta string,
	maxTokens int,
) (string, string, bool) {
	if delta == "" {
		return "", published, false
	}
	if maxTokens <= 0 ||
		countTokens(published+delta) <= maxTokens {
		return delta, published + delta, false
	}
	remaining := maxTokens - countTokens(published)
	if remaining <= 0 {
		return "", published, true
	}
	limited, _ := truncateToTokens(delta, remaining)
	return limited, published + limited, true
}

// ===================================================================
// OpenAI Responses API (/v1/responses)
// ===================================================================

// responsesRequest is the JSON body for POST /v1/responses.
type responsesRequest struct {
	Model              string                `json:"model"`
	Input              any                   `json:"input"`
	Instructions       string                `json:"instructions"`
	Stream             bool                  `json:"stream"`
	MaxOutputTokens    int                   `json:"max_output_tokens"`
	Tools              []toolcalling.ToolDef `json:"tools"`
	ToolChoice         any                   `json:"tool_choice"`
	Temperature        float64               `json:"temperature"`
	PreviousResponseID string                `json:"previous_response_id"`
	SessionID          string                `json:"session_id"`
	User               string                `json:"user"`
	Metadata           map[string]any        `json:"metadata"`
	Reasoning          *responsesReasoning   `json:"reasoning"`
}

// responsesReasoning is the reasoning block Codex CLI sends. M365 decides how
// much it deliberates through the tone rather than through a knob, so effort
// steers the tone choice and summary is accepted but not acted on.
type responsesReasoning struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary"`
}

// handleResponses handles OpenAI Responses API requests.
func (api *APIServer) handleResponses(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var req responsesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse model (may contain session ID suffix: "gpt5.5:my-session")
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}

	deliberate, err := reasoningEffortRequestsDeliberation(req.Reasoning)
	if err != nil {
		logging.Errorf("handleResponses: %v", err)
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg = applyReasoningEffort(modelKey, cfg, deliberate)

	req.Tools = mergeLoadedResponsesTools(req.Input, req.Tools)
	preparedTools, localTools := api.prepareCodingTools(req.Tools, false)
	req.Tools = preparedTools
	toolPolicy, err := newResponsesToolPolicy(req.Tools, req.ToolChoice)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	requestJSON := replaceRequestTools(bodyBytes, req.Tools)

	// Convert Responses API input to payload.Message list
	messages := responsesInputToMessages(req.Input)

	// Codex CLI opens a provider with a reachability probe: a POST carrying no
	// input at all. Answer it here rather than sending an empty turn upstream,
	// which costs a round trip and one message of the conversation quota.
	if strings.TrimSpace(req.Instructions) == "" && responsesInputIsEmpty(messages) {
		api.respondResponsesProbe(w, cfg.OpenAIID, req.Stream)
		return
	}

	if err := validateToolResultMessages(messages); err != nil {
		logging.Errorf("handleResponses: %v", err)
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	ledger := buildToolLedger(messages)
	if api.exceededToolRoundLimit(ledger) {
		api.sendToolRoundLimitError(w, ledger)
		return
	}
	// The simulation prompt collapses the input into one message, so the
	// evidence has to travel with the policy to reach the parser.
	toolPolicy.ledger = ledger

	// Prepend instructions as first user message (M365 has no system role)
	if strings.TrimSpace(req.Instructions) != "" && len(messages) > 0 {
		instrMsg := payload.Message{
			Role:    "user",
			Content: "Instructions: " + strings.TrimSpace(req.Instructions),
		}
		messages = append([]payload.Message{instrMsg}, messages...)
	}

	// Inject one Responses-aware simulation prompt unless tool_choice disables
	// client tool use.
	if toolPolicy.simulate {
		injectSimulatedPromptResponses(&messages, requestJSON, toolPolicy.promptChoice, toolPolicy.ledger.EvidenceNote())
	}

	// Resolve session ID
	// Priority: model-name session > previous_response_id > body session_id > body user > header > hash
	sid := modelSessionID
	if sid == "" {
		sid = req.PreviousResponseID
	}
	if sid == "" {
		sid = req.SessionID
	}
	if sid == "" {
		sid = req.User
	}
	if sid == "" {
		sid = r.Header.Get("X-Session-Id")
	}
	if sid == "" {
		sid = api.hashSessionIDFromMessages(r, messages)
	}

	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	// Upload any images found in multimodal content
	api.uploadImagesAndAnnotate(&messages, convID)

	if len(localTools) > 0 {
		result, err := api.runToolLoop(r, toolLoopOpenAI, messages, cfg, convID, req.Tools, localTools)
		if err != nil {
			api.sendUpstreamError(w, "response", err)
			return
		}
		api.respondBufferedResponses(
			w,
			result,
			messages,
			cfg,
			sid,
			req.MaxOutputTokens,
			req.Stream,
			responsesToolTypes(toolPolicy.tools),
			toolPolicy.tools,
			toolPolicy.promptChoice,
		)
		return
	}
	if req.Stream {
		api.streamResponses(
			r.Context(),
			w,
			messages,
			cfg,
			sid,
			convID,
			req.MaxOutputTokens,
			toolPolicy,
		)
	} else {
		api.nonStreamResponses(
			r.Context(),
			w,
			messages,
			cfg,
			sid,
			convID,
			req.MaxOutputTokens,
			toolPolicy,
		)
	}
}

// responsesInputToMessages converts the Responses API input field (string or
// array of input items) to a slice of payload.Message.
func responsesInputToMessages(input any) []payload.Message {
	if input == nil {
		return []payload.Message{{Role: "user", Content: ""}}
	}

	// Simple string input
	if s, ok := input.(string); ok {
		return []payload.Message{{Role: "user", Content: s}}
	}

	// Array input
	arr, ok := input.([]any)
	if !ok {
		return []payload.Message{{Role: "user", Content: ""}}
	}

	var messages []payload.Message
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		itemType, _ := m["type"].(string)

		// Handle function_call_output items (tool results). A custom tool
		// reports its result the same way, only under its own item type and
		// with the free-form field name.
		if itemType == "custom_tool_call_output" {
			callID, _ := m["call_id"].(string)
			output, _ := m["output"].(string)
			messages = append(messages, payload.Message{
				Role: "tool",
				Content: fmt.Sprintf(
					"Authoritative tool result (call_id: %s):\n%s",
					callID,
					output,
				),
				ToolCallID:  callID,
				ToolResults: []payload.ToolResultRecord{{ID: callID, Content: output}},
			})
			continue
		}

		if itemType == "custom_tool_call" {
			name, _ := m["name"].(string)
			input, _ := m["input"].(string)
			callID, _ := m["call_id"].(string)
			messages = append(messages, payload.Message{
				Role:      "assistant",
				Content:   fmt.Sprintf("Tool call: %s(%s)", name, input),
				ToolCalls: []payload.ToolCallRecord{{ID: callID, Name: name, Arguments: input}},
			})
			continue
		}

		// A long-running client tool can report intermediate progress before it
		// has a result. The item is transport metadata: it must reach the model
		// as context but must never satisfy the pending call, or the loop would
		// continue on an unfinished tool.
		if itemType == "function_call_progress" {
			callID, _ := m["call_id"].(string)
			message, _ := m["message"].(string)
			if strings.TrimSpace(callID) == "" || strings.TrimSpace(message) == "" {
				logging.Debugf("responsesInputToMessages: dropping a function_call_progress item without call_id or message")
				continue
			}
			phase, _ := m["phase"].(string)
			if phase == "" {
				phase = "running"
			}
			text := fmt.Sprintf("[Tool Progress (call_id: %s, phase: %s)]\n%s", callID, phase, message)
			if output, _ := m["output"].(string); output != "" {
				text += "\n" + output
			}
			// The role stays "user": a "tool" role would be flattened and
			// counted as a result by the history and the evidence ledger.
			messages = append(messages, payload.Message{Role: "user", Content: text, ToolProgress: true})
			continue
		}

		if itemType == "function_call_output" {
			callID, _ := m["call_id"].(string)
			output, _ := m["output"].(string)
			if output == "" && m["output"] != nil {
				encoded, _ := json.Marshal(m["output"])
				output = string(encoded)
			}
			messages = append(messages, payload.Message{
				Role: "tool",
				Content: fmt.Sprintf(
					"Authoritative tool result (call_id: %s):\n%s",
					callID,
					output,
				),
				ToolCallID:  callID,
				ToolResults: []payload.ToolResultRecord{{ID: callID, Content: output}},
			})
			continue
		}

		// Handle function_call items (assistant tool calls in input history)
		if itemType == "function_call" {
			name, _ := m["name"].(string)
			namespace, _ := m["namespace"].(string)
			args, _ := m["arguments"].(string)
			callID, _ := m["call_id"].(string)
			qualifiedName := name
			if namespace != "" {
				qualifiedName = namespace + "/" + name
			}
			messages = append(messages, payload.Message{
				Role:      "assistant",
				Content:   fmt.Sprintf("Tool call: %s(%s)", qualifiedName, args),
				ToolCalls: []payload.ToolCallRecord{{ID: callID, Name: qualifiedName, Arguments: args}},
			})
			continue
		}

		// Handle reasoning items (skip, M365 generates its own)
		if itemType == "reasoning" {
			continue
		}

		if itemType == "tool_search_call" {
			arguments, _ := json.Marshal(m["arguments"])
			messages = append(messages, payload.Message{
				Role:    "assistant",
				Content: fmt.Sprintf("Tool search call: tool_search(%s)", string(arguments)),
			})
			continue
		}

		if itemType == "tool_search_output" {
			toolsJSON, _ := json.Marshal(m["tools"])
			messages = append(messages, payload.Message{
				Role:    "tool",
				Content: "tool_search_output: preserve these loaded tools with their exact namespace, name, and schema: " + string(toolsJSON),
			})
			continue
		}

		if itemType == "additional_tools" {
			toolsJSON, _ := json.Marshal(m["tools"])
			messages = append(messages, payload.Message{
				Role:    "tool",
				Content: "additional_tools: preserve these callable tools with their exact namespace, name, and schema: " + string(toolsJSON),
			})
			continue
		}

		if itemType == "compaction" {
			summary, _ := m["encrypted_content"].(string)
			if strings.TrimSpace(summary) != "" {
				messages = append(messages, payload.Message{
					Role:    "user",
					Content: "Summary of the earlier conversation:\n" + summary,
				})
			}
			continue
		}

		// Message items (type "message" or items with role)
		role, _ := m["role"].(string)
		if role == "" {
			role = "user"
		}

		content := responsesExtractContent(m["content"])
		messages = append(messages, payload.Message{
			Role:    role,
			Content: content,
		})
	}

	if len(messages) == 0 {
		return []payload.Message{{Role: "user", Content: ""}}
	}
	return messages
}

// responsesExtractContent extracts text from a content field that may be a
// string or an array of content parts (input_text, output_text, text types).
func responsesExtractContent(content any) string {
	if content == nil {
		return ""
	}
	if s, ok := content.(string); ok {
		return s
	}
	arr, ok := content.([]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, part := range arr {
		p, ok := part.(map[string]any)
		if !ok {
			continue
		}
		ptype, _ := p["type"].(string)
		if ptype == "input_text" || ptype == "output_text" || ptype == "text" {
			if text, ok := p["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// responsesInputIsEmpty reports whether a converted Responses input carries no
// work at all: no text, no image, no tool call and no tool result.
//
// responsesInputToMessages returns one empty user message for an empty input,
// so the message count alone cannot tell a probe from a real turn.
func responsesInputIsEmpty(messages []payload.Message) bool {
	for i := range messages {
		m := &messages[i]
		if strings.TrimSpace(m.Content) != "" {
			return false
		}
		if len(m.Images) > 0 || len(m.ToolCalls) > 0 || len(m.ToolResults) > 0 {
			return false
		}
	}
	return true
}

// respondResponsesProbe answers a reachability probe without reaching M365. It
// writes a well-formed but empty Response, streaming or not, and touches no
// session state.
func (api *APIServer) respondResponsesProbe(w http.ResponseWriter, model string, stream bool) {
	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	response := buildResponsesObject(responseID, model, "", "", nil, nil, "stop", 0, 0, 0)

	if !stream {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	sequenceNumber := 0
	sendEvent := func(eventType string, data map[string]any) {
		data["type"] = eventType
		data["sequence_number"] = sequenceNumber
		sequenceNumber++
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()
	}

	// Codex validates the item lifecycle, so the probe emits the envelope
	// events even though it carries no output item.
	sendEvent("response.created", map[string]any{"response": response})
	sendEvent("response.in_progress", map[string]any{"response": response})
	sendEvent("response.completed", map[string]any{"response": response})
}

// buildResponsesObject constructs the non-streaming Responses API response object.
func buildResponsesObject(responseID, model, text, thinking string, toolCalls []client.ToolCall, toolTypes map[string]string, finishReason string, promptTok, completionTok, reasoningTok int) map[string]any {
	status := "completed"
	if finishReason == "length" {
		status = "incomplete"
	}

	output := []map[string]any{}
	outputIndex := 0

	// Add reasoning item if thinking is present
	if thinking != "" {
		reasoningID := fmt.Sprintf("rs_%s", responseID)
		output = append(output, map[string]any{
			"id":     reasoningID,
			"type":   "reasoning",
			"status": "completed",
			"summary": []map[string]any{
				{
					"type": "summary_text",
					"text": thinking,
				},
			},
		})
		outputIndex++
	}

	// Add message item with output_text (only if there is text content)
	if text != "" || len(toolCalls) == 0 {
		msgID := fmt.Sprintf("msg_%s", responseID)
		phase := "final_answer"
		if len(toolCalls) > 0 {
			phase = "commentary"
		}
		output = append(output, map[string]any{
			"id":     msgID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"phase":  phase,
			"content": []map[string]any{
				{
					"type":        "output_text",
					"text":        text,
					"annotations": []any{},
				},
			},
		})
		outputIndex++
	}

	// Add function_call or built-in client tool items after commentary.
	for _, tc := range toolCalls {
		callID := tc.ID
		if callID == "" {
			callID = "call_" + uuid.NewString()
		}
		output = append(output, buildResponsesToolCallItem(callID, tc, toolTypes, "completed"))
		outputIndex++
	}

	resp := map[string]any{
		"id":          responseID,
		"object":      "response",
		"created_at":  time.Now().Unix(),
		"status":      status,
		"model":       model,
		"output":      output,
		"output_text": text,
		"usage": map[string]any{
			"input_tokens":     promptTok,
			"output_tokens":    completionTok,
			"reasoning_tokens": reasoningTok,
			"total_tokens":     promptTok + completionTok + reasoningTok,
			"usage_source":     usageSource(),
		},
	}
	return resp
}

func (api *APIServer) respondBufferedResponses(w http.ResponseWriter, result toolLoopResult, messages []payload.Message, cfg models.ModelConfig, sid string, maxTokens int, stream bool, toolTypes map[string]string, tools []toolcalling.ToolDef, toolChoice string) {
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(result.text, maxTokens); ok {
			result.text, result.finishReason = truncated, "length"
		}
	}
	if sid != "" && result.conversationID != "" {
		api.ctxCache.Set(sessionKeyPrefix+sid, result.conversationID)
	}
	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	response := buildResponsesObject(responseID, cfg.OpenAIID, result.text, result.thinking, result.toolCalls, toolTypes, result.finishReason, countPromptTokens(messages, tools, toolChoice), countTokens(result.text)+outputProtocolTokens, countTokens(result.thinking))
	if !stream {
		api.sendJSON(w, http.StatusOK, response)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []string{"response.created", "response.in_progress"} {
		data, _ := json.Marshal(map[string]any{"type": event, "response": map[string]any{"id": responseID, "object": "response", "status": "in_progress", "model": cfg.OpenAIID}})
		fmt.Fprintf(w, "data: %s\n\n", data)
	}
	completed, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", completed)
}

func (api *APIServer) responsesConversationOnce(
	ctx context.Context,
	messages []payload.Message,
	cfg models.ModelConfig,
	conversationID string,
	simulateTools bool,
) (responsesConversationResult, error) {
	text, thinking, toolCalls, finishReason, finalConversationID, err :=
		api.m365Client.ChatConversationContext(
			ctx,
			messages,
			cfg.Tone,
			cfg.Override,
			conversationID,
			api.config.UserOID,
			api.config.TenantID,
			simulateTools,
		)
	return responsesConversationResult{
		text:           text,
		thinking:       thinking,
		toolCalls:      toolCalls,
		finishReason:   finishReason,
		conversationID: finalConversationID,
	}, err
}

func (api *APIServer) responsesRequestCanceled(
	ctx context.Context,
	sid string,
) bool {
	if ctx.Err() == nil {
		return false
	}
	if sid != "" && api.ctxCache != nil {
		api.ctxCache.Delete(sessionKeyPrefix + sid)
	}
	return true
}

func (api *APIServer) responsesConversation(
	ctx context.Context,
	messages []payload.Message,
	cfg models.ModelConfig,
	conversationID string,
	simulateTools bool,
	onRetry func(),
) (responsesConversationResult, error) {
	return responsesConversationWithEmptyRetry(
		ctx,
		conversationID,
		responsesEmptyRetrySchedule(simulateTools),
		onRetry,
		func(
			callContext context.Context,
			callConversationID string,
		) (responsesConversationResult, error) {
			result, err := api.responsesConversationOnce(
				callContext,
				messages,
				cfg,
				callConversationID,
				simulateTools,
			)
			if simulateTools {
				result.toolCalls = nil
			}
			return result, err
		},
	)
}

// nonStreamResponses handles non-streaming Responses API requests.
func (api *APIServer) nonStreamResponses(
	ctx context.Context,
	w http.ResponseWriter,
	messages []payload.Message,
	cfg models.ModelConfig,
	sid, convID string,
	maxTokens int,
	toolPolicy responsesToolPolicy,
) {
	result, err := api.responsesConversation(
		ctx,
		messages,
		cfg,
		convID,
		toolPolicy.simulate,
		func() {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
		},
	)
	if err != nil {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		if api.responsesRequestCanceled(ctx, sid) {
			return
		}
		api.sendUpstreamError(w, "chat", err)
		return
	}
	respText := result.text
	thinking := result.thinking
	toolCalls := result.toolCalls
	finishReason := result.finishReason
	finalConvID := result.conversationID

	toolCalls, finishReason = withoutBackendToolCalls(toolCalls, finishReason)

	// Parse simulated tool calls from response text
	if toolPolicy.simulate {
		simulated, parseErr := parseResponsesSimulationWithRetry(
			respText,
			toolPolicy,
			func() (string, error) {
				if sid != "" {
					api.ctxCache.Delete(sessionKeyPrefix + sid)
				}
				retryResult, retryErr := api.responsesConversationOnce(
					ctx,
					responsesSimulationRetryMessages(messages, toolPolicy),
					cfg,
					"",
					true,
				)
				if retryErr != nil {
					return "", retryErr
				}
				respText = retryResult.text
				thinking = retryResult.thinking
				toolCalls = retryResult.toolCalls
				finishReason = retryResult.finishReason
				finalConvID = retryResult.conversationID
				return retryResult.text, nil
			},
			func() (string, error) {
				if sid != "" {
					api.ctxCache.Delete(sessionKeyPrefix + sid)
				}
				retryResult, retryErr := api.responsesConversationOnce(
					ctx,
					messages,
					cfg,
					"",
					true,
				)
				if retryErr != nil {
					return "", retryErr
				}
				respText = retryResult.text
				thinking = retryResult.thinking
				toolCalls = nil
				finishReason = retryResult.finishReason
				finalConvID = retryResult.conversationID
				return retryResult.text, nil
			},
		)
		if parseErr != nil {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			if api.responsesRequestCanceled(ctx, sid) {
				return
			}
			if errors.Is(parseErr, errSimulatedToolCallRequired) {
				writeResponsesSimulationError(
					w,
					false,
					"",
					cfg.OpenAIID,
					parseErr,
				)
			} else {
				writeResponsesServerError(
					w,
					false,
					"",
					cfg.OpenAIID,
					"upstream_error",
					parseErr.Error(),
				)
			}
			return
		}
		respText = simulated.content
		toolCalls = simulated.toolCalls
		finishReason = simulated.finishReason
	}
	thinking = responsesReasoningForOutput(thinking, toolPolicy.simulate)
	if blockedByContentPolicy(respText, toolCalls) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendContentBlockedError(w, respText)
		return
	}
	// The Responses input is collapsed into one prompt message, so the
	// evidence comes from the policy rather than from the messages here.
	respText = withoutUnverifiedCompletionClaim(respText, toolPolicy.simulate, toolPolicy.ledger, toolCalls)
	if responsesResultEmpty(respText, toolCalls) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		// An exhausted conversation quota is the one empty-response cause the
		// client can act on, so report it as 429 rather than a generic empty.
		if api.quotaExhausted() {
			api.sendThrottledError(w)
			return
		}
		writeResponsesUpstreamEmptyError(
			w,
			false,
			"",
			cfg.OpenAIID,
		)
		return
	}

	// Enforce max_output_tokens
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
			finishReason = "length"
		}
	}

	promptTok := countPromptTokens(messages, toolPolicy.tools, toolPolicy.promptChoice)
	completionTok := countTokens(respText) + outputProtocolTokens
	reasoningTok := countTokens(thinking)

	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	response := buildResponsesObject(responseID, cfg.OpenAIID, respText, thinking, toolCalls, responsesToolTypes(toolPolicy.tools), finishReason, promptTok, completionTok, reasoningTok)

	api.sendJSON(w, http.StatusOK, response)

	// Cache conversation ID for session continuity
	if sid != "" {
		if shouldResetResponsesSession(respText, toolCalls, nil) {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		} else if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// streamResponses handles streaming Responses API requests.
func (api *APIServer) streamResponses(
	ctx context.Context,
	w http.ResponseWriter,
	messages []payload.Message,
	cfg models.ModelConfig,
	sid, convID string,
	maxTokens int,
	toolPolicy responsesToolPolicy,
) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	openaiModel := cfg.OpenAIID

	// Helper to send a Responses SSE event
	sequenceNumber := 0
	sendEvent := func(eventType string, data map[string]any) {
		data["type"] = eventType
		data["sequence_number"] = sequenceNumber
		sequenceNumber++
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()
	}
	sendFailed := func(code, message string) {
		event := buildResponsesFailedEvent(
			responseID,
			openaiModel,
			code,
			message,
			sequenceNumber,
		)
		sequenceNumber++
		jsonData, _ := json.Marshal(event)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}

	// Send response.created event
	sendEvent("response.created", map[string]any{
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  openaiModel,
		},
	})

	// Send response.in_progress event
	sendEvent("response.in_progress", map[string]any{
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  openaiModel,
		},
	})

	ch := responsesStreamWithEmptyRetry(
		ctx,
		convID,
		responsesEmptyRetrySchedule(toolPolicy.simulate),
		toolPolicy.simulate,
		func() {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
		},
		func(
			callContext context.Context,
			callConversationID string,
		) <-chan client.StreamChunk {
			return api.m365Client.ChatConversationStreamGenContext(
				callContext,
				messages,
				cfg.Tone,
				cfg.Override,
				callConversationID,
				api.config.UserOID,
				api.config.TenantID,
				toolPolicy.simulate,
			)
		},
	)

	var fullTextBuilder strings.Builder
	var thinkingText strings.Builder
	truncated := false

	// When tool calling is enabled, buffer all text and parse at the end
	toolCallingEnabled := toolPolicy.simulate
	var contentExtractor toolcalling.ContentStreamExtractor

	// Track whether we've emitted the message output item
	messageItemEmitted := false
	reasoningItemEmitted := false
	messageOutputIndex := 0
	msgID := fmt.Sprintf("msg_%s", responseID)
	reasoningID := fmt.Sprintf("rs_%s", responseID)
	simulatedPublishedText := ""
	emitSimulatedDelta := func(delta string) {
		delta, published, limited := limitResponsesStreamDelta(
			simulatedPublishedText,
			delta,
			maxTokens,
		)
		simulatedPublishedText = published
		if limited {
			truncated = true
		}
		if delta == "" {
			return
		}
		if !messageItemEmitted {
			outputIdx := 0
			if reasoningItemEmitted {
				outputIdx = 1
			}
			messageOutputIndex = outputIdx
			sendEvent("response.output_item.added", map[string]any{
				"output_index": outputIdx,
				"item": map[string]any{
					"id":      msgID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"phase":   "commentary",
					"content": []any{},
				},
			})
			sendEvent("response.content_part.added", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        "",
					"annotations": []any{},
				},
			})
			messageItemEmitted = true
		}
		sendEvent("response.output_text.delta", map[string]any{
			"item_id":       msgID,
			"output_index":  messageOutputIndex,
			"content_index": 0,
			"delta":         delta,
		})
	}

	// Reasoning is streamed live; under simulated tool calling it passes through
	// thinkingFilter first so the transport envelope never leaks. reasoningEmitted
	// accumulates exactly what was published for the terminal done events.
	var thinkingFilter toolcalling.ThinkingStreamFilter
	var reasoningEmitted strings.Builder
	reasoningFlushed := false
	emitReasoning := func(delta string) {
		if delta == "" {
			return
		}
		reasoningEmitted.WriteString(delta)
		if !reasoningItemEmitted {
			sendEvent("response.output_item.added", map[string]any{
				"output_index": 0,
				"item": map[string]any{
					"id":      reasoningID,
					"type":    "reasoning",
					"status":  "in_progress",
					"summary": []map[string]any{{"type": "summary_text", "text": ""}},
				},
			})
			sendEvent("response.reasoning_summary_part.added", map[string]any{
				"item_id":       reasoningID,
				"output_index":  0,
				"summary_index": 0,
				"part":          map[string]any{"type": "summary_text", "text": ""},
			})
			reasoningItemEmitted = true
		}
		sendEvent("response.reasoning_summary_text.delta", map[string]any{
			"item_id":       reasoningID,
			"output_index":  0,
			"summary_index": 0,
			"delta":         delta,
		})
	}

	var finalConvID string
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeSSEKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			_, code, message := streamErrorFields("responses", chunk.Error)
			sendFailed(code, message)
			return
		}

		if chunk.IsFinal {
			finalConvID = chunk.ConversationID
			break
		}

		// Stream reasoning live. Under simulated tool calling it is filtered so
		// the transport envelope never leaks; otherwise it passes through raw.
		if chunk.Thinking != "" {
			thinkingText.WriteString(chunk.Thinking)
			if toolCallingEnabled {
				emitReasoning(thinkingFilter.Feed(chunk.Thinking))
			} else {
				emitReasoning(chunk.Thinking)
			}
		}

		// Handle text content
		if chunk.Text != "" {
			if toolCallingEnabled {
				// Flush remaining reasoning before content so the reasoning item
				// is fully emitted at output_index 0 ahead of the message item.
				if !reasoningFlushed {
					emitReasoning(thinkingFilter.Flush())
					reasoningFlushed = true
				}
				// Keep the raw transport for final tool-call parsing, while
				// publishing only decoded assistant content.
				fullTextBuilder.WriteString(chunk.Text)
				emitSimulatedDelta(contentExtractor.Feed(chunk.Text))
			} else {
				if !messageItemEmitted {
					// Emit message output item
					outputIdx := 0
					if reasoningItemEmitted {
						outputIdx = 1
					}
					sendEvent("response.output_item.added", map[string]any{
						"output_index": outputIdx,
						"item": map[string]any{
							"id":      msgID,
							"type":    "message",
							"status":  "in_progress",
							"role":    "assistant",
							"phase":   "final_answer",
							"content": []any{},
						},
					})
					sendEvent("response.content_part.added", map[string]any{
						"item_id":       msgID,
						"output_index":  outputIdx,
						"content_index": 0,
						"part": map[string]any{
							"type":        "output_text",
							"text":        "",
							"annotations": []any{},
						},
					})
					messageItemEmitted = true
				}

				// Check max_tokens
				if maxTokens > 0 && countTokens(fullTextBuilder.String()+chunk.Text) > maxTokens {
					remaining := maxTokens - countTokens(fullTextBuilder.String())
					if remaining > 0 {
						delta, _ := truncateToTokens(chunk.Text, remaining)
						if delta != "" {
							fullTextBuilder.WriteString(delta)
							outputIdx := 0
							if reasoningItemEmitted {
								outputIdx = 1
							}
							sendEvent("response.output_text.delta", map[string]any{
								"item_id":       msgID,
								"output_index":  outputIdx,
								"content_index": 0,
								"delta":         delta,
							})
						}
					}
					truncated = true
					// Drain remaining chunks
					go func() {
						for range ch {
						}
					}()
					break
				}

				fullTextBuilder.WriteString(chunk.Text)
				outputIdx := 0
				if reasoningItemEmitted {
					outputIdx = 1
				}
				sendEvent("response.output_text.delta", map[string]any{
					"item_id":       msgID,
					"output_index":  outputIdx,
					"content_index": 0,
					"delta":         chunk.Text,
				})
			}
		}
	}
	fullText := fullTextBuilder.String()
	if api.responsesRequestCanceled(ctx, sid) {
		return
	}

	if !toolCallingEnabled && responsesResultEmpty(fullText, nil) {
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		sendFailed(
			upstreamEmptyResponseCode,
			"The upstream completed without assistant content or a tool call.",
		)
		return
	}

	// Flush any remaining filtered reasoning for the thinking-only case where no
	// content chunk triggered the in-loop flush.
	if toolCallingEnabled && !reasoningFlushed {
		emitReasoning(thinkingFilter.Flush())
		reasoningFlushed = true
	}

	// Finalize reasoning item if emitted. reasoningEmitted holds exactly what was
	// published (raw for non-simulated, filtered under simulated tool calling).
	if reasoningItemEmitted {
		reasoningFinal := reasoningEmitted.String()
		sendEvent("response.reasoning_summary_text.done", map[string]any{
			"item_id":       reasoningID,
			"output_index":  0,
			"summary_index": 0,
			"text":          reasoningFinal,
		})
		sendEvent("response.reasoning_summary_part.done", map[string]any{
			"item_id":       reasoningID,
			"output_index":  0,
			"summary_index": 0,
			"part": map[string]any{
				"type": "summary_text",
				"text": reasoningFinal,
			},
		})
		sendEvent("response.output_item.done", map[string]any{
			"output_index": 0,
			"item": map[string]any{
				"id":     reasoningID,
				"type":   "reasoning",
				"status": "completed",
				"summary": []map[string]any{
					{
						"type": "summary_text",
						"text": reasoningFinal,
					},
				},
			},
		})
	}

	// Handle tool calling: parse buffered text for simulated tool calls
	var toolCalls []client.ToolCall
	finishReason := "stop"

	if toolCallingEnabled {
		initialParseText := contentExtractor.ParseText()
		simulated, parseErr := parseResponsesSimulationWithRetry(
			initialParseText,
			toolPolicy,
			func() (string, error) {
				if sid != "" {
					api.ctxCache.Delete(sessionKeyPrefix + sid)
				}
				retryResult, retryErr := api.responsesConversationOnce(
					ctx,
					responsesSimulationRetryMessages(messages, toolPolicy),
					cfg,
					"",
					true,
				)
				if retryErr != nil {
					return "", retryErr
				}
				fullText = retryResult.text
				finalConvID = retryResult.conversationID
				if !messageItemEmitted {
					contentExtractor = toolcalling.ContentStreamExtractor{}
					contentExtractor.Feed(retryResult.text)
				}
				return retryResult.text, nil
			},
			func() (string, error) {
				if sid != "" {
					api.ctxCache.Delete(sessionKeyPrefix + sid)
				}
				retryResult, retryErr := api.responsesConversationOnce(
					ctx,
					messages,
					cfg,
					"",
					true,
				)
				if retryErr != nil {
					return "", retryErr
				}
				fullText = retryResult.text
				finalConvID = retryResult.conversationID
				if !messageItemEmitted {
					contentExtractor = toolcalling.ContentStreamExtractor{}
					contentExtractor.Feed(retryResult.text)
				}
				return retryResult.text, nil
			},
		)
		if parseErr != nil {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			if api.responsesRequestCanceled(ctx, sid) {
				return
			}
			if errors.Is(parseErr, errSimulatedToolCallRequired) {
				sendFailed(
					simulatedToolCallRequiredCode,
					parseErr.Error(),
				)
			} else {
				sendFailed("upstream_error", parseErr.Error())
			}
			return
		}
		committedContent := contentExtractor.Commit(
			toolPolicy.allowedToolNames,
		)
		emitSimulatedDelta(committedContent)
		if len(simulated.toolCalls) == 0 &&
			(committedContent != "" || simulated.content == "") {
			simulated.content = simulatedPublishedText
		} else if messageItemEmitted {
			simulated.content = simulatedPublishedText
		}
		fullText = simulated.content
		toolCalls = simulated.toolCalls
		finishReason = simulated.finishReason
		// This is the one streaming path that publishes assistant content as it
		// decodes it, so the claim cannot be replaced here.
		warnOnUnverifiedCompletionClaim(fullText, toolPolicy.simulate, toolPolicy.ledger, len(toolCalls))
		if truncated {
			finishReason = "length"
		}
		if responsesResultEmpty(fullText, toolCalls) {
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			sendFailed(
				upstreamEmptyResponseCode,
				"The upstream completed without assistant content or a tool call.",
			)
			return
		}

		// Now emit the buffered text and tool calls as Responses events
		outputIdx := 0
		if reasoningItemEmitted {
			outputIdx = 1
		}
		if messageItemEmitted {
			outputIdx = messageOutputIndex + 1
		}

		if messageItemEmitted {
			phase := "final_answer"
			if len(toolCalls) > 0 {
				phase = "commentary"
			}
			sendEvent("response.output_text.done", map[string]any{
				"item_id":       msgID,
				"output_index":  messageOutputIndex,
				"content_index": 0,
				"text":          fullText,
			})
			sendEvent("response.content_part.done", map[string]any{
				"item_id":       msgID,
				"output_index":  messageOutputIndex,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []any{},
				},
			})
			sendEvent("response.output_item.done", map[string]any{
				"output_index": messageOutputIndex,
				"item": map[string]any{
					"id":     msgID,
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"phase":  phase,
					"content": []map[string]any{
						{
							"type":        "output_text",
							"text":        fullText,
							"annotations": []any{},
						},
					},
				},
			})
		} else if fullText != "" || len(toolCalls) == 0 {
			// Emit buffered text when no incremental content was available.
			// Enforce max_output_tokens
			if maxTokens > 0 {
				if truncated, ok := truncateToTokens(fullText, maxTokens); ok {
					fullText = truncated
					finishReason = "length"
				}
			}
			phase := "final_answer"
			if len(toolCalls) > 0 {
				phase = "commentary"
			}

			sendEvent("response.output_item.added", map[string]any{
				"output_index": outputIdx,
				"item": map[string]any{
					"id":      msgID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"phase":   phase,
					"content": []any{},
				},
			})
			sendEvent("response.content_part.added", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        "",
					"annotations": []any{},
				},
			})
			sendEvent("response.output_text.delta", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"delta":         fullText,
			})
			sendEvent("response.output_text.done", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"text":          fullText,
			})
			sendEvent("response.content_part.done", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []any{},
				},
			})
			sendEvent("response.output_item.done", map[string]any{
				"output_index": outputIdx,
				"item": map[string]any{
					"id":     msgID,
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"phase":  phase,
					"content": []map[string]any{
						{
							"type":        "output_text",
							"text":        fullText,
							"annotations": []any{},
						},
					},
				},
			})
			outputIdx++
		}

		// Emit tool call items after the user-facing commentary.
		toolTypes := responsesToolTypes(toolPolicy.tools)
		for _, tc := range toolCalls {
			callID := tc.ID
			if callID == "" {
				callID = "call_" + uuid.NewString()
			}
			toolKey := responsesToolKey(
				tc.Function.Namespace,
				tc.Function.Name,
			)
			isToolSearch := toolTypes[toolKey] == "tool_search"
			sendEvent("response.output_item.added", map[string]any{
				"output_index": outputIdx,
				"item":         buildResponsesToolCallItem(callID, tc, toolTypes, "in_progress"),
			})
			if !isToolSearch {
				sendEvent("response.function_call_arguments.delta", map[string]any{
					"item_id":      callID,
					"output_index": outputIdx,
					"delta":        tc.Function.Arguments,
				})
				sendEvent("response.function_call_arguments.done", map[string]any{
					"item_id":      callID,
					"output_index": outputIdx,
					"name":         tc.Function.Name,
					"arguments":    tc.Function.Arguments,
				})
			}
			sendEvent("response.output_item.done", map[string]any{
				"output_index": outputIdx,
				"item":         buildResponsesToolCallItem(callID, tc, toolTypes, "completed"),
			})
			outputIdx++
		}
	} else {
		// Non-tool-calling mode: finalize message item if emitted
		if messageItemEmitted {
			outputIdx := 0
			if reasoningItemEmitted {
				outputIdx = 1
			}
			if truncated {
				finishReason = "length"
			}
			sendEvent("response.output_text.done", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"text":          fullText,
			})
			sendEvent("response.content_part.done", map[string]any{
				"item_id":       msgID,
				"output_index":  outputIdx,
				"content_index": 0,
				"part": map[string]any{
					"type":        "output_text",
					"text":        fullText,
					"annotations": []any{},
				},
			})
			sendEvent("response.output_item.done", map[string]any{
				"output_index": outputIdx,
				"item": map[string]any{
					"id":     msgID,
					"type":   "message",
					"status": "completed",
					"role":   "assistant",
					"phase":  "final_answer",
					"content": []map[string]any{
						{
							"type":        "output_text",
							"text":        fullText,
							"annotations": []any{},
						},
					},
				},
			})
		}
	}

	// Build final response object for response.completed
	status := "completed"
	if finishReason == "length" {
		status = "incomplete"
	}

	promptTok := countPromptTokens(messages, toolPolicy.tools, toolPolicy.promptChoice)
	completionTok := countTokens(fullText) + outputProtocolTokens
	reasoningText := thinkingText.String()
	reasoningTok := countTokens(reasoningText)

	finalResponse := buildResponsesObject(responseID, openaiModel, fullText, reasoningText, toolCalls, responsesToolTypes(toolPolicy.tools), finishReason, promptTok, completionTok, reasoningTok)
	finalResponse["status"] = status

	sendEvent("response.completed", map[string]any{
		"response": finalResponse,
	})

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	// Cache conversation ID for session continuity
	if sid != "" {
		if shouldResetResponsesSession(fullText, toolCalls, nil) {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		} else if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}
}

// ===================================================================
// OpenAI Responses Compact API (/v1/responses/compact)
// ===================================================================

// defaultCompactionPrompt is the system instruction sent to M365 Copilot when
// compacting a conversation. It asks the model to produce a concise summary
// that preserves key context for continuation.
const defaultCompactionPrompt = "I need a concise summary of the following conversation between a user and an assistant. Please cover the main topics discussed, any decisions made, code or files mentioned, and what was being worked on. Keep it brief but preserve all important context. Explicitly preserve tool state: which tools were searched for, loaded, or called; their exact namespace and names; the results of those calls; and the user's current objective and next step. Do not describe transport JSON or protocol details; summarize only the actual work."

func responsesCompactionConversationID(string) string {
	return ""
}

// handleResponsesCompact handles POST /v1/responses/compact requests from Codex.
// It sends the conversation history to M365 Copilot with a compaction prompt,
// then returns the summary wrapped in a compaction output item.
func (api *APIServer) handleResponsesCompact(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read request body: %v", err))
		return
	}
	r.Body.Close()

	var req responsesRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Parse model (may contain session ID suffix)
	modelKey, modelSessionID := parseModelSessionID(req.Model)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}

	deliberate, err := reasoningEffortRequestsDeliberation(req.Reasoning)
	if err != nil {
		logging.Errorf("handleResponsesCompact: %v", err)
		api.sendError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg = applyReasoningEffort(modelKey, cfg, deliberate)

	// Convert Responses API input to payload.Message list
	inputMessages := responsesInputToMessages(req.Input)

	// Flatten the conversation history into a single user message with
	// compaction instructions. M365 has no system role and responds to the
	// last user message, so we must merge everything into one message to
	// prevent the model from answering the conversation instead of summarizing it.
	compactionInstr := defaultCompactionPrompt
	instructions := strings.TrimSpace(req.Instructions)
	if instructions != "" {
		compactionInstr = instructions
	}

	var conversationText strings.Builder
	conversationText.WriteString(compactionInstr)
	conversationText.WriteString("\n\n")
	for _, m := range inputMessages {
		fmt.Fprintf(&conversationText, "%s: %s\n", m.Role, m.Content)
	}
	conversationText.WriteString("\nPlease provide the summary now.")

	messages := []payload.Message{
		{Role: "user", Content: conversationText.String()},
	}

	// Resolve session ID (same priority as handleResponses)
	sid := modelSessionID
	if sid == "" {
		sid = req.PreviousResponseID
	}
	if sid == "" {
		sid = req.SessionID
	}
	if sid == "" {
		sid = req.User
	}
	if sid == "" {
		sid = r.Header.Get("X-Session-Id")
	}
	if sid == "" {
		sid = api.hashSessionIDFromMessages(r, messages)
	}

	convID := responsesCompactionConversationID(
		api.ctxCache.Get(sessionKeyPrefix + sid),
	)

	// Upload any images found in multimodal content
	api.uploadImagesAndAnnotate(&messages, convID)

	hasTools := len(toolcalling.RouteableTools(req.Tools)) > 0

	logging.Infof("handleResponsesCompact: model=%s sid=%s convID=%s stream=%t tools=%d", modelKey, sid, convID, req.Stream, len(req.Tools))

	if req.Stream {
		api.streamResponsesCompact(r.Context(), w, messages, cfg, sid, convID, req.MaxOutputTokens, hasTools, req.Tools)
	} else {
		api.nonStreamResponsesCompact(w, messages, cfg, sid, convID, req.MaxOutputTokens, hasTools, req.Tools)
	}
}

// buildCompactionResponseObject constructs the non-streaming compact response.
// The output contains exactly one compaction item with encrypted_content set
// to the M365 summary text.
func buildCompactionResponseObject(responseID, model, summaryText string, promptTok, completionTok int) map[string]any {
	compactionID := fmt.Sprintf("cmp_%s", responseID)
	output := []map[string]any{
		{
			"id":                compactionID,
			"type":              "compaction",
			"encrypted_content": summaryText,
		},
	}

	return map[string]any{
		"id":         responseID,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"status":     "completed",
		"model":      model,
		"output":     output,
		"usage": map[string]any{
			"input_tokens":  promptTok,
			"output_tokens": completionTok,
			"total_tokens":  promptTok + completionTok,
			"usage_source":  usageSource(),
		},
	}
}

// nonStreamResponsesCompact handles non-streaming compact requests.
func (api *APIServer) nonStreamResponsesCompact(w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, sid, convID string, maxTokens int, hasTools bool, tools []toolcalling.ToolDef) {
	respText, _, _, _, _, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)
	if err != nil {
		logging.Errorf("nonStreamResponsesCompact: chat failed: %v", err)
		if sid != "" {
			api.ctxCache.Delete(sessionKeyPrefix + sid)
		}
		api.sendUpstreamError(w, "compaction", err)
		return
	}

	// In simulated mode, extract plain content
	if hasTools {
		sim := toolcalling.ParseSimulatedResponse(respText, toolNamesFromDefs(tools), toolcalling.ContractsFor(tools))
		if sim.HasPayload {
			respText = sim.Content
		} else {
			respText = toolcalling.WithholdTransportEnvelope(respText)
		}
	}

	// Enforce max_output_tokens
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(respText, maxTokens); ok {
			respText = truncated
		}
	}

	// The compaction request declares no tools, so only message framing counts.
	promptTok := countPromptTokens(messages, nil, "")
	completionTok := countTokens(respText) + outputProtocolTokens

	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	response := buildCompactionResponseObject(responseID, cfg.OpenAIID, respText, promptTok, completionTok)

	api.sendJSON(w, http.StatusOK, response)

	if sid != "" && strings.TrimSpace(respText) != "" {
		api.ctxCache.Delete(sessionKeyPrefix + sid)
	}
}

// streamResponsesCompact handles streaming compact requests.
// It emits a standard Responses SSE stream but replaces the output item
// with a single compaction item containing the summary.
func (api *APIServer) streamResponsesCompact(ctx context.Context, w http.ResponseWriter, messages []payload.Message, cfg models.ModelConfig, sid, convID string, maxTokens int, hasTools bool, tools []toolcalling.ToolDef) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "close")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		api.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	responseID := fmt.Sprintf("resp_%s", uuid.New().String())
	openaiModel := cfg.OpenAIID
	compactionID := fmt.Sprintf("cmp_%s", responseID)

	sendEvent := func(eventType string, data map[string]any) {
		data["type"] = eventType
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", jsonData)
		flusher.Flush()
	}

	// Send response.created event
	sendEvent("response.created", map[string]any{
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  openaiModel,
		},
	})

	// Send response.in_progress event
	sendEvent("response.in_progress", map[string]any{
		"response": map[string]any{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"model":  openaiModel,
		},
	})

	ch := api.m365Client.ChatConversationStreamGenContext(ctx, messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, hasTools)

	var fullTextBuilder strings.Builder

	var finalToolCalls []client.ToolCall
	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()
	for {
		chunk, more := nextStreamChunk(ctx, ch, keepalive, w, func() error { return writeSSEKeepalive(w, flusher) })
		if !more {
			break
		}
		if chunk.Error != nil {
			logging.Errorf("streamResponsesCompact: stream error: %v", chunk.Error)
			if sid != "" {
				api.ctxCache.Delete(sessionKeyPrefix + sid)
			}
			sendEvent("response.failed", map[string]any{
				"response": map[string]any{
					"id":     responseID,
					"object": "response",
					"status": "failed",
					"model":  openaiModel,
					"error":  map[string]any{"message": chunk.Error.Error()},
				},
			})
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
		if chunk.Text != "" {
			fullTextBuilder.WriteString(chunk.Text)
		}
	}
	_ = finalToolCalls
	fullText := fullTextBuilder.String()

	// In simulated mode, extract plain content
	if hasTools {
		sim := toolcalling.ParseSimulatedResponse(fullText, toolNamesFromDefs(tools), toolcalling.ContractsFor(tools))
		if sim.HasPayload {
			fullText = sim.Content
		} else {
			fullText = toolcalling.WithholdTransportEnvelope(fullText)
			if toolcalling.IsContentPolicyBlock(fullText) {
				// The stream is already open, so the refusal cannot be turned
				// into an HTTP error the way the non-streaming paths do.
				logging.Warn("upstream content refusal on a streaming turn: M365 declined the request instead of answering")
			}
		}
	}

	// Enforce max_output_tokens
	if maxTokens > 0 {
		if truncated, ok := truncateToTokens(fullText, maxTokens); ok {
			fullText = truncated
		}
	}

	// Emit the compaction output item
	sendEvent("response.output_item.added", map[string]any{
		"output_index": 0,
		"item": map[string]any{
			"id":   compactionID,
			"type": "compaction",
		},
	})

	sendEvent("response.output_item.done", map[string]any{
		"output_index": 0,
		"item": map[string]any{
			"id":                compactionID,
			"type":              "compaction",
			"encrypted_content": fullText,
		},
	})

	// Build final response object for response.completed
	promptTok := countPromptTokens(messages, nil, "")
	completionTok := countTokens(fullText) + outputProtocolTokens

	finalResponse := buildCompactionResponseObject(responseID, openaiModel, fullText, promptTok, completionTok)

	sendEvent("response.completed", map[string]any{
		"response": finalResponse,
	})

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	if sid != "" && strings.TrimSpace(fullText) != "" {
		api.ctxCache.Delete(sessionKeyPrefix + sid)
	}
}

// imageGenerationRequest represents an OpenAI /v1/images/generations request.
type imageGenerationRequest struct {
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
	Quality        string `json:"quality"`
	Style          string `json:"style"`
	SessionID      string `json:"session_id"`
	User           string `json:"user"`
}

// imageDataItem represents a single image in the OpenAI Images API response.
type imageDataItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// urlImagePattern matches markdown image links with HTTP(S) URLs.
var urlImagePattern = regexp.MustCompile(`!\[[^\]]*\]\((https://[^)]+)\)`)

// handleImageGenerations handles OpenAI /v1/images/generations requests.
// It wraps the prompt as a chat completions request to M365, extracts generated
// image URLs from the response, and returns them in OpenAI Images API format.
func (api *APIServer) handleImageGenerations(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req imageGenerationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logging.Errorf("handleImageGenerations: invalid JSON: %v", err)
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}
	if req.Prompt == "" {
		api.sendError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	logging.Infof("handleImageGenerations: model=%s n=%d size=%s responseFormat=%s", req.Model, req.N, req.Size, req.ResponseFormat)
	if req.N <= 0 {
		req.N = 1
	}

	// Build prompt with size/quality/style hints appended
	fullPrompt := buildImagePromptWithHints(req.Prompt, req.Size, req.Quality, req.Style)

	// Resolve model (default to gpt5.5-reasoning for image generation)
	modelKey := req.Model
	if modelKey == "" {
		modelKey = "gpt5.5-reasoning"
	}
	modelKey, _ = parseModelSessionID(modelKey)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}

	messages := []payload.Message{{Role: "user", Content: fullPrompt}}

	// Image generation is a one-shot operation. Reusing a chat conversation can
	// cause M365 to disengage instead of routing the prompt to image generation.
	respText, _, _, _, _, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, "", api.config.UserOID, api.config.TenantID, false)
	if err != nil {
		api.sendUpstreamError(w, "image generation", err)
		return
	}

	// Extract image URLs from markdown in response text
	dataItems := api.buildOpenAIImageData(respText, req.N, req.Prompt, req.ResponseFormat)
	if len(dataItems) == 0 {
		api.sendError(w, http.StatusInternalServerError, "No images were generated. The model may not have produced an image.")
		return
	}

	api.sendJSON(w, http.StatusOK, map[string]any{
		"created": time.Now().Unix(),
		"data":    dataItems,
	})
}

// handleImageEdits handles OpenAI /v1/images/edits requests.
// It accepts multipart/form-data with an image file, prompt, and optional mask,
// uploads the image to M365, sends the edit prompt, and returns the result.
func (api *APIServer) handleImageEdits(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodPost {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse multipart form
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		logging.Errorf("handleImageEdits: failed to parse multipart form: %v", err)
		api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to parse multipart form: %v", err))
		return
	}

	prompt := r.FormValue("prompt")
	if prompt == "" {
		api.sendError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	modelKey := r.FormValue("model")
	if modelKey == "" {
		modelKey = "gpt5.5-reasoning"
	}
	modelKey, modelSessionID := parseModelSessionID(modelKey)
	cfg, ok := api.resolveModel(w, modelKey)
	if !ok {
		return
	}
	logging.Infof("handleImageEdits: model=%s prompt_len=%d images=%d responseFormat=%s", modelKey, len(prompt), len(r.MultipartForm.File["image"]), r.FormValue("response_format"))

	n := 1
	if nStr := r.FormValue("n"); nStr != "" {
		if v, err := fmtAtoi(nStr); err == nil && v > 0 {
			n = v
		}
	}
	size := r.FormValue("size")
	quality := r.FormValue("quality")
	style := r.FormValue("style")
	responseFormat := r.FormValue("response_format")

	// Read image file(s). OpenAI API supports up to 16 images for GPT image models.
	// Multipart form-data may send "image" as multiple form files.
	imageFiles := r.MultipartForm.File["image"]
	if len(imageFiles) == 0 {
		api.sendError(w, http.StatusBadRequest, "image file is required")
		return
	}

	var images []payload.ImageData
	for i, fh := range imageFiles {
		if i >= 16 {
			break
		}
		f, err := fh.Open()
		if err != nil {
			api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to open image %d: %v", i, err))
			return
		}
		imgBytes, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read image %d: %v", i, err))
			return
		}
		imgB64 := base64.StdEncoding.EncodeToString(imgBytes)
		imgMime := fh.Header.Get("Content-Type")
		if imgMime == "" {
			imgMime = "image/png"
		}
		imgExt := extFromMediaType(imgMime)
		imgName := fmt.Sprintf("edit-%d.%s", i, imgExt)
		images = append(images, payload.ImageData{
			Base64:    imgB64,
			MediaType: imgMime,
			FileName:  imgName,
		})
	}

	// Read optional mask
	var maskB64, maskFileName, maskMimeType string
	if maskFile, maskHeader, err := r.FormFile("mask"); err == nil {
		maskBytes, err := io.ReadAll(maskFile)
		maskFile.Close()
		if err != nil {
			api.sendError(w, http.StatusBadRequest, fmt.Sprintf("Failed to read mask: %v", err))
			return
		}
		maskB64 = base64.StdEncoding.EncodeToString(maskBytes)
		maskMimeType = maskHeader.Header.Get("Content-Type")
		if maskMimeType == "" {
			maskMimeType = "image/png"
		}
		maskFileName = "mask." + extFromMediaType(maskMimeType)
	}

	// Resolve session ID
	sid := modelSessionID
	if sid == "" {
		sid = r.FormValue("session_id")
	}
	if sid == "" {
		sid = r.FormValue("user")
	}
	if sid == "" {
		sid = r.Header.Get("X-Session-Id")
	}
	if sid == "" {
		sid = "img-edit-" + uuid.New().String()[:8]
	}

	var convID string
	if sid != "" {
		convID = api.ctxCache.Get(sessionKeyPrefix + sid)
	}

	// Build prompt with hints
	fullPrompt := buildImagePromptWithHints(prompt, size, quality, style)

	// Build multimodal message with image annotations
	msg := payload.Message{Role: "user", Content: fullPrompt}
	msg.Images = images
	if maskB64 != "" {
		msg.Images = append(msg.Images, payload.ImageData{
			Base64:    maskB64,
			MediaType: maskMimeType,
			FileName:  maskFileName,
		})
	}

	messages := []payload.Message{msg}

	// Upload images and attach annotations
	api.uploadImagesAndAnnotate(&messages, convID)

	respText, _, _, _, finalConvID, err := api.m365Client.ChatConversation(messages, cfg.Tone, cfg.Override, convID, api.config.UserOID, api.config.TenantID, false)
	if err != nil {
		api.sendUpstreamError(w, "image edit", err)
		return
	}

	// Cache conversation ID
	if sid != "" {
		if finalConvID != "" {
			api.ctxCache.Set(sessionKeyPrefix+sid, finalConvID)
		}
	}

	// Extract image URLs from response
	dataItems := api.buildOpenAIImageData(respText, n, prompt, responseFormat)
	if len(dataItems) == 0 {
		api.sendError(w, http.StatusInternalServerError, "No edited images were generated. The model may not have produced an image.")
		return
	}

	api.sendJSON(w, http.StatusOK, map[string]any{
		"created": time.Now().Unix(),
		"data":    dataItems,
	})
}

// buildImagePromptWithHints appends size, quality, and style hints to the prompt
// as natural language, since M365 does not accept these as direct parameters.
func buildImagePromptWithHints(prompt, size, quality, style string) string {
	var hints []string
	if size != "" && size != "1024x1024" {
		hints = append(hints, fmt.Sprintf("size: %s", size))
	}
	if quality != "" && quality != "standard" {
		hints = append(hints, fmt.Sprintf("quality: %s", quality))
	}
	if style != "" && style != "natural" {
		hints = append(hints, fmt.Sprintf("style: %s", style))
	}
	if len(hints) == 0 {
		return prompt
	}
	return fmt.Sprintf("%s\n\nImage specifications: %s", prompt, strings.Join(hints, ", "))
}

// buildOpenAIImageData extracts image URLs from markdown in the response text
// and converts them to OpenAI Images API data items. When responseFormat is
// "b64_json", it downloads each URL and base64-encodes the content. When
// responseFormat is "url", it also downloads the image and returns a
// data:image/png;base64,... data URL (falling back to the raw URL on error)
// since the raw designerapp URL is auth-gated and inaccessible to clients.
func (api *APIServer) buildOpenAIImageData(respText string, n int, revisedPrompt, responseFormat string) []imageDataItem {
	urls := urlImagePattern.FindAllStringSubmatch(respText, -1)
	if len(urls) == 0 {
		return nil
	}

	// Deduplicate URLs
	seen := map[string]bool{}
	var uniqueURLs []string
	for _, match := range urls {
		u := match[1]
		if !seen[u] {
			seen[u] = true
			uniqueURLs = append(uniqueURLs, u)
		}
	}

	if n > 0 && n < len(uniqueURLs) {
		uniqueURLs = uniqueURLs[:n]
	}

	var items []imageDataItem
	for _, u := range uniqueURLs {
		b64, err := api.downloadAndBase64(u)
		if err != nil {
			// A disallowed host is dropped outright. Returning the raw URL
			// would hand a model-controlled address back to the client, which
			// would then fetch it.
			if errors.Is(err, errImageHostNotAllowed) {
				logging.Errorf("Dropping generated image URL: %v", err)
				continue
			}
			// The host is allowed but the transfer failed, so the raw URL is
			// still a safe fallback.
			logging.Errorf("Failed to download image: %v", err)
			items = append(items, imageDataItem{
				URL:           u,
				RevisedPrompt: revisedPrompt,
			})
			continue
		}
		if responseFormat == "b64_json" {
			items = append(items, imageDataItem{
				B64JSON:       b64,
				RevisedPrompt: revisedPrompt,
			})
			continue
		}
		items = append(items, imageDataItem{
			URL:           "data:image/png;base64," + b64,
			RevisedPrompt: revisedPrompt,
		})
	}

	return items
}

// errImageHostNotAllowed reports a generated-image URL the proxy refuses to
// contact.
var errImageHostNotAllowed = errors.New("image URL host is not allowed")

// hostAllowed reports whether host matches an allowlist entry. An entry
// starting with a dot matches that domain and any subdomain of it; any other
// entry must match exactly.
func hostAllowed(host string, allowlist []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, entry := range allowlist {
		if domain, isSuffix := strings.CutPrefix(entry, "."); isSuffix {
			if host == domain || strings.HasSuffix(host, entry) {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

// ipDisallowed reports whether an address must never be contacted. Loopback,
// private, link-local, multicast, unspecified and carrier-grade NAT ranges all
// sit inside the deployment's own network, and 169.254.169.254 is the cloud
// metadata endpoint.
func ipDisallowed(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 100.64.0.0/10 is carrier-grade NAT, which net.IP.IsPrivate misses.
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return false
}

// validateImageDownloadURL decides whether a generated-image URL may be
// fetched. The download sends the designerapp access token, so an attacker who
// can influence the model's output must not be able to redirect that token to
// their own host, nor use the proxy to reach internal addresses. The URL comes
// from model-generated markdown, which is untrusted input.
func (api *APIServer) validateImageDownloadURL(rawURL string) error {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid image URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme %q is not https", errImageHostNotAllowed, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return fmt.Errorf("%w: URL has no host", errImageHostNotAllowed)
	}
	if !hostAllowed(host, api.config.ImageHostAllowlist) {
		return fmt.Errorf("%w: %q", errImageHostNotAllowed, host)
	}

	// Resolve as defence in depth: an allowlisted name that resolves inward
	// still must not be contacted.
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("%w: %q does not resolve", errImageHostNotAllowed, host)
	}
	if slices.ContainsFunc(ips, ipDisallowed) {
		return fmt.Errorf("%w: %q resolves to a non-public address", errImageHostNotAllowed, host)
	}
	return nil
}

// downloadAndBase64 downloads an image from a designerapp URL and returns its
// base64-encoded content. designerapp URLs require a JWE access token (acquired
// via SSO cookies with the M365 web app client_id) and the fileToken query
// parameter sent as a header.
func (api *APIServer) downloadAndBase64(imageURL string) (string, error) {
	if err := api.validateImageDownloadURL(imageURL); err != nil {
		logging.Errorf("downloadAndBase64: refusing download: %v", err)
		return "", err
	}
	logging.Infof("downloadAndBase64: downloading image from %s", imageURL[:min(100, len(imageURL))])
	parsedURL, err := neturl.Parse(imageURL)
	if err != nil {
		logging.Errorf("downloadAndBase64: invalid URL: %v", err)
		return "", fmt.Errorf("invalid image URL: %w", err)
	}

	// Extract fileToken from query params and remove it from the URL
	query := parsedURL.Query()
	fileToken := query.Get("fileToken")
	if fileToken == "" {
		logging.Errorf("downloadAndBase64: no fileToken in URL")
		return "", fmt.Errorf("no fileToken in image URL")
	}
	query.Del("fileToken")
	parsedURL.RawQuery = query.Encode()
	cleanURL := parsedURL.String()

	// Acquire designerapp access token via SSO cookies
	token, err := api.tokenManager.GetDesignerToken()
	if err != nil {
		return "", fmt.Errorf("failed to acquire designer token: %w", err)
	}

	req, err := http.NewRequest("GET", cleanURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create image request: %w", err)
	}
	req.Header.Set("Authorization", token)
	req.Header.Set("filetoken", fileToken)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://m365.cloud.microsoft")
	req.Header.Set("Referer", "https://m365.cloud.microsoft/")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errCode := resp.Header.Get("X-Errorcode")
		failReason := resp.Header.Get("X-Failurereason")
		logging.Errorf("Image download failed: status=%d, x-errorcode=%s, x-failurereason=%s, body=%s",
			resp.StatusCode, errCode, failReason, string(body)[:min(200, len(body))])
		return "", fmt.Errorf("download returned status %d: x-errorcode=%s, x-failurereason=%s", resp.StatusCode, errCode, failReason)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logging.Errorf("downloadAndBase64: failed to read body: %v", err)
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	logging.Infof("downloadAndBase64: success, size=%d bytes", len(body))
	return base64.StdEncoding.EncodeToString(body), nil
}

// fmtAtoi parses an int from a string without importing strconv.
func fmtAtoi(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// extFromMediaType returns the file extension for a given MIME type.
func extFromMediaType(mediaType string) string {
	switch strings.ToLower(mediaType) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/gif":
		return "gif"
	case "image/webp":
		return "webp"
	default:
		return "png"
	}
}
