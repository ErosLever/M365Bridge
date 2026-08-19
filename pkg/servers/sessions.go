package servers

import (
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
	sessionID := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		api.sendError(w, http.StatusNotFound, "Session not found")
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
	case http.MethodDelete:
		api.deleteSession(w, r, sessionID)
	default:
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
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
	}

	api.ctxCache.Delete(sessionKeyPrefix + sessionID)
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
