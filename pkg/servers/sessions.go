package servers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/client"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
)

// Conversation continuity runs through a session id that the caller either
// sends or the proxy derives. The mapping from that id to the upstream M365
// conversation lived only inside the cache, so a client could neither see
// which sessions existed nor start a session over without knowing the
// conversation id by some other route. These handlers expose it.

// handleSessions lists every session-to-conversation mapping.
func (api *APIServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	records, legacy := api.ctxCache.List()
	data := make([]map[string]any, 0, len(records))
	for _, record := range records {
		data = append(data, sessionJSON(record))
	}
	api.sendJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
		// Entries written before the record format carry no session id, and
		// none can be recovered because the file name is an md5. Reporting the
		// count keeps the omission visible instead of making the list look
		// complete.
		"legacy_entries": legacy,
	})
}

// handleSession reads or clears one session-to-conversation mapping.
func (api *APIServer) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		api.handleCORS(w, r)
		return
	}
	sessionID, sub, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/")
	if sessionID == "" {
		api.sendError(w, http.StatusNotFound, "Session not found")
		return
	}
	if sub != "" {
		if sub != "messages" {
			api.sendError(w, http.StatusNotFound, "Session not found")
			return
		}
		api.handleSessionMessages(w, r, sessionID)
		return
	}

	switch r.Method {
	case http.MethodGet:
		record, ok := api.ctxCache.Lookup(sessionID)
		if !ok {
			api.sendError(w, http.StatusNotFound, "Session not found")
			return
		}
		api.sendJSON(w, http.StatusOK, sessionJSON(record))
	case http.MethodPut:
		api.bindSession(w, r, sessionID)
	case http.MethodDelete:
		api.deleteSession(w, r, sessionID)
	default:
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// handleSessionMessages returns the turns recorded for a session.
//
// The backend never replays a conversation, so this record is the only source
// a client has for redrawing one. It is empty for a conversation that was
// started somewhere other than this gateway, and an empty list says exactly
// that rather than pretending the conversation has no history.
func (api *APIServer) handleSessionMessages(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if api.transcripts == nil {
		api.sendErrorCode(w, http.StatusNotFound, "transcripts_disabled",
			"transcripts are not recorded; set M365_ENABLE_WEB_UI=true to keep them")
		return
	}

	entries := api.transcripts.Load(sessionID)
	data := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{
			"object":     "session.message",
			"role":       entry.Role,
			"content":    entry.Content,
			"created_at": entry.CreatedAt,
		}
		if entry.Model != "" {
			item["model"] = entry.Model
		}
		if entry.Thinking != "" {
			item["thinking"] = entry.Thinking
		}
		data = append(data, item)
	}
	api.sendJSON(w, http.StatusOK, map[string]any{
		"object":     "list",
		"session_id": sessionID,
		"data":       data,
	})
}

// maxConversationIDLength bounds the value a caller may bind. Real ids are far
// shorter, and the value travels upstream on every later turn.
const maxConversationIDLength = 256

// bindSession points a session id at a conversation that already exists.
//
// The chat path resolves a session id to a conversation id, but nothing wrote
// that mapping except a completed turn. A conversation started somewhere else,
// in the M365 web or mobile client, therefore could not be continued through
// this gateway at all. Binding it here is the only way to reach it.
func (api *APIServer) bindSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		ConversationID string `json:"conversation_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		api.sendError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	conversationID := strings.TrimSpace(req.ConversationID)
	if conversationID == "" {
		api.sendError(w, http.StatusBadRequest, "conversation_id is required")
		return
	}
	if len(conversationID) > maxConversationIDLength {
		api.sendError(w, http.StatusBadRequest, "conversation_id is too long")
		return
	}

	api.ctxCache.Set(sessionKeyPrefix+sessionID, conversationID)
	logging.Infof("Bound session %s to conversation %s", sessionID, conversationID)

	record, ok := api.ctxCache.Lookup(sessionID)
	if !ok {
		// Set writes both the in-memory entry and the file, so a miss here
		// means the write did not survive and the caller must not be told the
		// binding holds.
		api.sendError(w, http.StatusInternalServerError, "Failed to store the session mapping")
		return
	}
	api.sendJSON(w, http.StatusOK, sessionJSON(record))
}

// deleteSession clears a mapping so the next turn starts a new conversation.
//
// The upstream conversation is deleted first. If that fails the mapping is
// left in place, so the caller can retry instead of losing the only reference
// to a conversation that still exists. local_only skips the upstream delete,
// which a deployment without M365 web cookies needs, because there the
// upstream delete can never succeed.
func (api *APIServer) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	record, ok := api.ctxCache.Lookup(sessionID)
	if !ok {
		api.sendError(w, http.StatusNotFound, "Session not found")
		return
	}

	localOnly := r.URL.Query().Get("local_only") == "true"
	if !localOnly {
		conversationClient := client.NewConversationClient(api.tokenManager)
		if err := conversationClient.DeleteConversation(r.Context(), record.ConversationID); err != nil {
			api.sendConversationError(w, err)
			return
		}
		logging.Infof("Deleted upstream conversation for session %s", sessionID)
		// More than one session can point at one conversation, and the
		// conversation is gone now. Clearing only the named session would leave
		// its siblings pointing at something that no longer exists.
		api.dropSessionsFor(record.ConversationID)
	}

	api.ctxCache.Delete(sessionKeyPrefix + sessionID)
	api.dropTranscript(sessionID)
	api.sendJSON(w, http.StatusOK, map[string]any{
		"object":                "session.deleted",
		"id":                    sessionID,
		"conversation_id":       record.ConversationID,
		"upstream_conversation": map[string]any{"deleted": !localOnly},
	})
}

// sessionJSON renders one mapping for the wire.
func sessionJSON(record sessionRecord) map[string]any {
	return map[string]any{
		"object":          "session",
		"id":              record.SessionID,
		"conversation_id": record.ConversationID,
		"updated_at":      record.UpdatedAt,
	}
}
