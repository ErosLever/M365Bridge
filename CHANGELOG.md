# Changelog

All notable changes to M365Bridge will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [1.4.2] - 2026-08-20

### Fixed
- Keep the conversation a built-in coding tool loop created when the loop ends on an error, instead of orphaning it and starting a new one on the next turn
- Compare coding tool calls by signature rather than by raw arguments, so a repeat that differs only in JSON key order or whitespace no longer runs the tool again

## [1.4.1] - 2026-08-20

### Added
- Serve a browser interface at `/` from files embedded in the binary, gated on `M365_ENABLE_WEB_UI`
- Add the chat interface with a conversation sidebar, streaming answers and model selection
- Keep the composer in place while the conversation scrolls and move the model picker into it
- Record the turns of a session under `data/transcripts` and read them back over `GET /v1/sessions/{id}/messages`
- Bind a session to a conversation that already exists with `PUT /v1/sessions/{id}`
- Read a conversation this gateway never carried from the M365 conversation page
- Import an upstream conversation into a session over `GET /v1/conversations/{id}/messages`
- Offer to load the history of a conversation started in the M365 web or mobile client
- Document the whole command surface in `--help`, including both subcommands and the environment variables

### Changed
- Set `ReadHeaderTimeout` and `IdleTimeout` on the HTTP server, and tighten the log file, the written coding-tool file and their directories to owner-only
- State on every remaining static-analysis finding why the code is safe, so a real one cannot hide in the noise
- Cover the coding-tool workspace escape and the written file mode with tests
- Describe the web interface and reading a conversation held upstream in both READMEs
- Ignore the Go build cache directory

### Fixed
- Strip the backend's raw citation markers from answer text on every channel
- Check the error every discarded call returns, and report a failed cache directory instead of proceeding without one
- Restrict a log file an earlier build left readable

## [1.4.0] - 2026-08-20

### Added
- Expose Copilot through a JSON-RPC 2.0 Model Context Protocol server on `/mcp`
- Publish the OpenAI and the Anthropic model schema from a single `/v1/models` route
- Advertise the Codex catalog fields, owner, input budget and tool support in the model list
- Publish three measured tones and state thinking support per model entry
- Accept `max` reasoning effort and describe every preset
- Expose the session to conversation mapping over `/v1/sessions` and `/v1/sessions/{id}`
- Store the session id and a timestamp in the context cache
- Add the `/v1/health` reachability probe
- Surface the M365 conversation quota and report throttling
- Add context window configuration and defaults
- Report usage on the Anthropic Complete endpoint and on the streaming completions route
- Commit stream headers before the upstream turn and keep every SSE stream alive during upstream silence
- Carry the upstream HTTP status out of failed requests
- Report an M365 content refusal as a distinct error
- Add the evidence ledger for client-driven tool loops, and cap the tool rounds one turn may drive
- Put settled tool results into the simulation prompt and stop forwarding a tool call whose result is already settled
- Replace a completion report that no tool result backs
- Accept a progress note for a running client tool
- Validate tool call arguments against their JSON schema and enforce `tool_choice` when parsing the model response
- Reject tool results that answer no declared tool call
- Re-ask when the backend answers a tool request with prose, and claim an unfenced grammar tool body as a call
- Widen the sandbox refusal detector to more phrasings
- Stop routing a declared `web_search` to the client
- Keep tool call structure after text flattening
- Accept the Responses reasoning block and emit custom tools as `custom_tool_call` in Responses output
- Fetch caller-supplied remote image URLs
- Log file and audio content blocks instead of dropping them silently

### Changed
- Answer an empty Responses probe without an upstream turn
- Accumulate streamed text with `strings.Builder`
- Drop values no caller reads
- Restrict generated-image downloads to allowed hosts
- Document the error contract, the tool contract, the session routes, usage reporting, SSE resilience and the setup that does not use Docker

### Fixed
- Never forward M365's own tool calls, and keep the backend's own tool messages out of the answer
- Stop a re-encoded snapshot from repeating the answer
- Report a turn the backend ended without an answer, and a turn that ends with no answer and no verdict
- Reject a model this service does not serve instead of folding it into the auto entry
- Serve the reasoning model from a tone that reasons
- Print the CLI model list in a stable order
- Classify upstream failures instead of reporting them all as 500
- Put the error category in `type` and the machine-readable string in `code`
- Drop a streaming turn when its client disconnects, and bound SSE writes to a gone client
- Count tokens with `o200k_base` and report the source
- Count Anthropic prompt tokens like every other endpoint
- Report usage from the buffered coding-tool responders
- Bill tool choice framing only when tools were declared
- Reject a repeated tool call id and make tool call IDs unique
- Bound the tool result text the ledger carries
- Recognize every exit-code wording as a failure
- Keep backend calls suppressed for a `web_search`-only request
- Claim a grammar body wrapped in a valid envelope, and withhold an unparseable transport envelope from the answer
- Replace the unverified claim on the buffered streams too
- Match the backend's observed Turkish refusal wording
- Identify the proxy when fetching a remote image
- Accept the API key from `x-api-key` as well as `Authorization`
- Include the `signature` field on Anthropic thinking blocks
- Serialize refresh token redemption
- Preserve tool schemas and namespaces


## [1.3.7] - 2026-07-14

### Added
- Live-stream filtered reasoning/thinking on the OpenAI chat, OpenAI Responses, and Anthropic streaming endpoints
- Re-ask the backend once when simulated tool calls drop required arguments, applied across all endpoints
- Log the parsed tool-call count in the Anthropic simulated parser for parity with the OpenAI parser

### Changed
- Unify the simulated thinking/transport-envelope filter across all endpoints

### Fixed
- Stream Anthropic `tool_use` input as `input_json_delta` on both the direct and buffered streaming paths so SDK clients accumulate arguments correctly
- Drop simulated tool calls that are missing required arguments before emitting them

## [1.3.6] - 2026-07-12

### Changed
- Move the project to Go 1.26 and adopt standard-library iterators (`slices.Backward`, `strings.SplitSeq`)
- Bump the Docker base images to `golang:1.26-alpine` and `alpine:3.24` and update the `gorilla/websocket` dependency
- Update pinned GitHub Actions to their latest releases via Dependabot

## [1.3.5] - 2026-07-12

### Added
- Harden the OpenAI Responses API for Codex: simulated tool-call retries, namespaced tools, ordered `response.failed` and `[DONE]` terminal events, and client-disconnect cancellation of upstream M365 streams

### Changed
- Modernize the codebase for the gopls modernize analyzer and enforce it as a CI job
- Harden the CI and release supply chain: pin GitHub Actions to commit SHAs, pin Docker base images by digest, add least-privilege workflow permissions, and add Dependabot

### Fixed
- Lowercase Responses tool policy error strings to satisfy Staticcheck
- Retry empty and required-tool Responses completions before failing
- Fix M365 chat routing for education tenants

## [1.3.1] - 2026-07-12

### Added
- Encrypt M365 web cookies with backward-compatible plaintext migration

### Changed
- Simplify the SSO authorization code exchange
- Add a context-window session continuity test
- Add a comprehensive tool-calling architecture guide
- Refine repository ignore rules

### Fixed
- Preserve provider tool names and Responses API call IDs

## [1.3.0] - 2026-07-11

### Added
- Add Claude Fable and GPT-5.6 reasoning model support
- Add opt-in built-in coding tools
- Add M365 conversation management support
- Add the Anthropic token counting endpoint
- Improve SSO extraction and broker redirect handling
- Support Anthropic system content blocks

### Changed
- Apply project-wide Go lint fixes
- Filter setup tokens by client ID
- Clean up API diagnostics

### Fixed
- Summarize broker authorization errors
- Isolate image generation conversations
- Reacquire expired designer broker tokens

## [1.2.2] - 2026-07-08

### Added
- Tool calling support for `/v1/completions` endpoint (simulated tool calls with `tools` field)
- Streaming support for `/v1/complete` endpoint (SSE events: `ping`, `completion` with delta text, final `completion` with `stop_reason`)

### Changed
- Consolidate `StreamChunk` and `ConversationStreamChunk` into a single `StreamChunk` type
- Consolidate `ChatStreamGen` to delegate to `ChatConversationStreamGen` (eliminates ~150 lines of duplicated WebSocket read loop logic)
- Remove shared state from `M365Client` for concurrent requests (per-request state via channel chunks, no mutex needed)
- `ChatConversation` now returns 6 values (added `conversationID` return value)
- `LastConversationID()`, `LastToolCalls()`, `LastThinking()` methods removed from `M365Client`
- CI: use CHANGELOG.md content for GitHub release body

### Fixed
- Make `/v1/models` endpoint public without auth requirement
- Merge system prompts into last message for M365 backend (system messages in earlier positions were silently dropped in multi-message conversations)

## [1.2.1] - 2026-07-07

### Added
- OpenAI Responses Compact API endpoint (`/v1/responses/compact`) for Codex remote compaction (streaming + non-streaming)

### Changed
- Documentation: add `/v1/responses/compact` endpoint docs to README, README.tr, AGENTS.md, and CLAUDE.md

## [1.2.0] - 2026-07-07

### Added
- Structured logging system (`pkg/logging`) with dual-writer (stdout + `data/proxy.log`) and leveled logging (DEBUG/INFO/WARN/ERROR/FATAL)
- OpenAI Images API endpoints (`/v1/images/generations`, `/v1/images/edits`) wrapping M365 DALL-E image generation
- Image generation support with server-side image download for both `url` and `b64_json` response formats
- Multiple image upload support for image edits (up to 16 images via repeated `image` form fields)
- OpenAI Responses API endpoint (`/v1/responses`)
- Generated image URL extraction from M365 WebSocket responses with markdown image link emission

### Fixed
- Client: extract generated image URLs from M365 WebSocket Progress messages (`contentOrigin: "ImageGeneration"`)

### Changed
- Documentation: fix model table formatting

## [1.1.0] - 2026-07-06

### Added
- Simulated tool calling mode for client-defined tools (OpenAI and Anthropic endpoints, streaming and non-streaming)
- Native Anthropic simulated mode with dedicated SSE handlers (`BuildSimulatedPromptAnthropic`/`ParseSimulatedResponseAnthropic`)
- Shell-routing for agentic coding loops (Claude Code, Droid CLI, Codex)
- Claude model support: `claude`, `claude-sonnet`, `claude-opus`, `claude-sonnet-4-20250514` (verified via tone test, routes to real Anthropic Claude Sonnet/Opus 4.6)
- Session ID embedded in model name via `:` separator (e.g. `gpt5.5-reasoning:my-session-001`)

### Changed
- Removed global `ToolCalling` configuration (`M365_TOOL_CALLING` env var and `Config.ToolCalling` field); tool calling is always enabled, `len(req.Tools) > 0` is the only gate
- Removed tool calling mode configuration (`M365_TOOL_CALLING_MODE` env var and `Config.ToolCallingMode` field); simulated mode is the only mode
- Removed fenced code block tool calling mode and all related functions (`ParseToolCalls`, `buildToolInstruction`, `injectToolDefs`, anti-confabulation retry logic)
- Strengthened and clarified tool use system instructions
- Updated documentation for tool calling and session isolation

## [1.0.3] - 2026-07-05

### Added
- SSO cookie-based re-authentication as fallback when 24h refresh token expires (AADSTS700084)
- SSO cookie capture during setup-wizard via `sso_cookies` field in setup.json
- Setup and token renewal process improvements

### Changed
- Docker setup documentation improved with single step-by-step flow

### Fixed
- SSO re-authentication reliability improvements (sso_reload=True, response_mode=fragment, correct redirect_uri, Origin header for SPA token exchange)

## [1.0.2] - 2026-07-05

### Changed
- Repository recreated to reset contributor history

## [1.0.1] - 2026-07-05

### Added
- Docker support with multi-stage Dockerfile and docker-compose.yml
- GitHub Actions CI workflow (cross-platform build for linux/darwin/windows amd64+arm64)
- GitHub Actions release workflow (6 platform binaries + multi-arch Docker image push to ghcr.io)
- Pre-built binary downloads from GitHub Releases
- .dockerignore for optimized Docker build context
- Version update skill for automated version bumping
- Prerequisites section and first-run expectations in README
- Model selection guide in README
- Anthropic SDK and image input Python examples in README
- .env example format in README

### Changed
- Project renamed from m365-copilot2api to M365Bridge
- Go module path changed to github.com/KilimcininKorOglu/M365Bridge
- Binary output moved to bin/ directory
- Encryption key storage moved from ~/.m365-copilot/ to data/tokens/
- .env file location moved from project root to data/.env
- Setup wizard output messages updated to use ./bin/m365-bridge paths
- Version field changed from const to var for ldflags override
- Version output changed from "M365 Copilot CLI" to "M365Bridge"
- README badges and Docker pull instructions added
- .gitignore updated for new project structure
