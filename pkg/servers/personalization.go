package servers

import (
	"encoding/json"
	"net/http"
)

// M365 Copilot keeps a memory at the account level, and it reaches every turn
// this gateway serves whatever conversation the turn belongs to. Measured
// against the live backend, a brand-new conversation listed content from
// unrelated earlier sessions and applied a style preference stored by one of
// them. Conversation isolation is promised through the session-to-conversation
// mapping, and this setting passes underneath it.
//
// These handlers make the setting visible and changeable. Nothing here runs on
// its own: the value belongs to the operator's real M365 account and applies to
// their web and mobile Copilot too, so turning it off is their decision to make
// and to see, not a side effect of running a gateway.

// personalizationJSON shapes the flags for the wire, in the snake_case every
// other route here uses.
func personalizationJSON(flags map[string]bool) map[string]any {
	body := map[string]any{"object": "personalization"}
	for key, value := range flags {
		body[key] = value
	}
	return body
}

// handlePersonalization reports the account's Copilot memory settings and
// changes the memory switch.
func (api *APIServer) handlePersonalization(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodOptions:
		api.handleCORS(w, r)
	case http.MethodGet:
		api.getPersonalization(w)
	case http.MethodPatch:
		api.patchPersonalization(w, r)
	default:
		api.sendError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func (api *APIServer) getPersonalization(w http.ResponseWriter) {
	flags, err := api.m365Client.GetPersonalizationFlags(api.config.UserOID, api.config.TenantID)
	if err != nil {
		api.sendUpstreamError(w, "personalization read", err)
		return
	}
	api.sendJSON(w, http.StatusOK, personalizationJSON(map[string]bool{
		"memory_enabled":                    flags.MemoryEnabled,
		"insights_from_history_enabled":     flags.InsightsFromHistoryEnabled,
		"custom_instruction_enabled":        flags.CustomInstructionEnabled,
		"graph_content_enabled":             flags.GraphContentEnabled,
		"personalization_allowed_by_tenant": flags.AllowedByTenant,
	}))
}

func (api *APIServer) patchPersonalization(w http.ResponseWriter, r *http.Request) {
	limitRequestBody(w, r, requestBodyMax)
	var request struct {
		MemoryEnabled *bool `json:"memory_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		api.sendRequestBodyError(w, err)
		return
	}
	// A pointer, so a body that names no field is refused rather than read as
	// a request to turn memory off.
	if request.MemoryEnabled == nil {
		api.sendError(w, http.StatusBadRequest, "memory_enabled is required")
		return
	}

	// The tenant decides whether the setting exists at all. Sending the write
	// anyway would fail upstream with a message nobody can act on, so the
	// state of the account is reported instead.
	current, err := api.m365Client.GetPersonalizationFlags(api.config.UserOID, api.config.TenantID)
	if err != nil {
		api.sendUpstreamError(w, "personalization read", err)
		return
	}
	if !current.AllowedByTenant {
		api.sendErrorCode(w, http.StatusConflict, "personalization_disabled_by_tenant",
			"This tenant does not allow Copilot personalization, so the memory setting cannot be changed.")
		return
	}

	flags, err := api.m365Client.SetMemoryEnabled(api.config.UserOID, api.config.TenantID, *request.MemoryEnabled)
	if err != nil {
		api.sendUpstreamError(w, "personalization write", err)
		return
	}
	api.sendJSON(w, http.StatusOK, personalizationJSON(map[string]bool{
		"memory_enabled":                    flags.MemoryEnabled,
		"insights_from_history_enabled":     flags.InsightsFromHistoryEnabled,
		"custom_instruction_enabled":        flags.CustomInstructionEnabled,
		"graph_content_enabled":             flags.GraphContentEnabled,
		"personalization_allowed_by_tenant": flags.AllowedByTenant,
	}))
}
