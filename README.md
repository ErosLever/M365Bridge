# M365Bridge

[![CI](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/ci.yml)
[![Release](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/release.yml/badge.svg)](https://github.com/KilimcininKorOglu/M365Bridge/actions/workflows/release.yml)
[![Version](https://img.shields.io/github/v/release/KilimcininKorOglu/M365Bridge)](https://github.com/KilimcininKorOglu/M365Bridge/releases)
[![Docker](https://img.shields.io/badge/docker-ghcr.io-blue)](https://github.com/KilimcininKorOglu/M365Bridge/pkgs/container/m365bridge)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![OpenAI Compatible](https://img.shields.io/badge/API-OpenAI%20Compatible-412991)](#api-endpoints)
[![Anthropic Compatible](https://img.shields.io/badge/API-Anthropic%20Compatible-D97757?logo=anthropic&logoColor=white)](#api-endpoints)

**English** | **[Türkçe](README.tr.md)**

A Go implementation that converts Microsoft 365 Copilot's WebSocket interface to OpenAI/Anthropic compatible HTTP API.

## Architecture

Your App -> M365Bridge -> substrate.office.com (SignalR) -> M365 Copilot Backend

## Prerequisites

- **Go 1.26+** installed ([download](https://go.dev/dl/)). An older Go from 1.21 on also works: it downloads the 1.26 toolchain on the first build, unless `GOTOOLCHAIN` is set to `local`.
- **git** for cloning this repository
- A **Microsoft 365 Copilot license** (business or enterprise account with Copilot access) tested a copilot chat (basic) account
- A browser logged into [https://m365.cloud.microsoft](https://m365.cloud.microsoft) (for setup wizard token extraction)

## Features

- Text chat with streaming/non-streaming output
- Multimodal image input (OpenAI `image_url` and Anthropic `image` content blocks; PNG, JPEG, GIF, WebP)
- Image generation via  (`/v1/images/generations`, `/v1/images/edits`) with `url` and `b64_json` response formats
- Multi-turn conversation support via ConversationId tracking
- Session isolation (per-session M365 conversations)
- Thinking/reasoning content extraction (`reasoning_content` for OpenAI, `thinking` blocks for Anthropic)
- Simulated tool calling (client-defined tools work on both OpenAI and Anthropic endpoints, streaming and non-streaming)
- OpenAI-compatible API endpoints
- Anthropic-compatible API endpoints (dedicated SSE handlers)
- API key authentication (`M365_API_KEYS` / `M365_API_KEY`)
- max_tokens enforcement across all endpoints (tiktoken BPE)
- CLI interface for interactive use
- Browser interface compiled into the binary (conversation list, streaming chat, model picker)
- Single binary with subcommand routing

## Installation

Three ways to run the service: build it from source, download a pre-built binary, or run the Docker image. All three need the same one-time token setup from the browser.

### Without Docker

Docker is optional. To run the binary directly, on Windows, macOS or Linux:

#### Step 1: Build the binary

```bash
git clone https://github.com/KilimcininKorOglu/M365Bridge
cd M365Bridge
go build -o bin/m365-bridge ./cmd/cli
```

On Windows, name the output `bin/m365-bridge.exe` instead.

#### Step 2: Create the data directory

```bash
mkdir data
```

Every runtime path is relative to the working directory, so **run every command below from the repository root**. Starting the binary from another directory makes it look for `data/` there and report a missing token.

#### Step 3: Get your authentication token

Follow **Step 3** of the Docker section above to run the browser snippet, and **Step 4** if you want the SSO cookies that keep the token renewing past 24 hours.

#### Step 4: Create setup.json and run the wizard

Save the browser output to `data/setup.json` in the format shown in **Step 5** of the Docker section, then:

```bash
./bin/m365-bridge setup-wizard
```

Windows PowerShell:

```powershell
.\bin\m365-bridge.exe setup-wizard
```

The wizard writes `data/.env` and the encrypted credentials under `data/tokens/`.

#### Step 5: Start the server

```bash
./bin/m365-bridge serve --port 8000
```

Windows PowerShell:

```powershell
.\bin\m365-bridge.exe serve --port 8000
```

The API is available at `http://localhost:8000`. The Docker setup maps the container to host port 8230; running directly there is no mapping, so the port you pass is the port you use.

### Pre-built Binaries

Download the latest binary for your platform from [GitHub Releases](https://github.com/KilimcininKorOglu/M365Bridge/releases):

| Platform                    | File                            |
|-----------------------------|---------------------------------|
| Linux amd64                 | `m365-bridge-linux-amd64`       |
| Linux arm64                 | `m365-bridge-linux-arm64`       |
| macOS amd64 (Intel)         | `m365-bridge-darwin-amd64`      |
| macOS arm64 (Apple Silicon) | `m365-bridge-darwin-arm64`      |
| Windows amd64               | `m365-bridge-windows-amd64.exe` |
| Windows arm64               | `m365-bridge-windows-arm64.exe` |

```bash
# Example: Linux amd64
wget https://github.com/KilimcininKorOglu/M365Bridge/releases/latest/download/m365-bridge-linux-amd64
chmod +x m365-bridge-linux-amd64
./m365-bridge-linux-amd64 serve --port 8000
```

### Docker

The easiest way to run M365Bridge is with Docker. The pre-built image is available on GitHub Container Registry.

#### Step 1: Create docker-compose.yml

Create a `docker-compose.yml` file in your project directory:

```yaml
services:
  m365bridge:
    image: ghcr.io/kilimcininkoroglu/m365bridge:latest
    container_name: m365bridge
    ports:
      - "8230:8000"
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

#### Step 2: Start the container

```bash
docker compose up -d
```

The API will be available at `http://localhost:8230`.

#### Step 3: Get your authentication token from the browser

The server needs a refresh token from your Microsoft 365 Copilot session. Extract it as follows:

1. Open [https://m365.cloud.microsoft](https://m365.cloud.microsoft) in your browser and log in
2. Press **F12** to open DevTools, go to **Console**
3. Paste and run the following JavaScript code:

<details>
<summary>Click to expand the JavaScript extraction snippet</summary>

```javascript
(async () => {
// 1. Get oid/tenant from the signed-in account
let oid, tenant;
for (const key of Object.keys(localStorage)) {
  if (!key.includes('active-account-filters')) continue;
  try {
    const val = JSON.parse(localStorage.getItem(key));
    if (val?.homeAccountId?.includes('.')) { [oid, tenant] = val.homeAccountId.split('.'); break; }
  } catch(e) {}
}
if (!oid) {
  const mk = Object.keys(localStorage).find(k => k.startsWith('msal.') && k.includes('|'));
  if (mk) { const p = mk.split('|')[1]; if (p?.includes('.')) [oid, tenant] = p.split('.'); }
}
if (!oid || !tenant) return 'ERROR: No signed-in account found. Log in to m365.cloud.microsoft and run this again.';

// 2. Watch every token exchange for the one this gateway uses
const targetClientID = '4765445b-32c6-49b0-83e6-1d93765276ca';
const origFetch = window.fetch;
let done;
const captured = new Promise(resolve => { done = resolve; });
window.fetch = async function(...args) {
  const resp = await origFetch.apply(this, args);
  const url = typeof args[0] === 'string' ? args[0] : args[0]?.url || '';
  if (url.includes('oauth2/v2.0/token')) {
    try {
      let bodyStr = '';
      const init = args[1];
      if (typeof init?.body === 'string') bodyStr = init.body;
      else if (init?.body instanceof URLSearchParams) bodyStr = init.body.toString();
      else if (init?.body instanceof ArrayBuffer || ArrayBuffer.isView(init?.body)) bodyStr = new TextDecoder().decode(init.body);
      else if (args[0] instanceof Request) bodyStr = await args[0].clone().text();
      // The sign-in exchange puts a broker id in client_id and carries the real
      // target in brk_client_id, so both are accepted.
      const params = new URLSearchParams(bodyStr);
      const isTarget = params.get('client_id') === targetClientID
                    || params.get('brk_client_id') === targetClientID;
      if (isTarget) {
        const data = await resp.clone().json();
        if (data.refresh_token) {
          console.log('===== COPY THE COMPLETE JSON BELOW =====');
          console.log(JSON.stringify({oid, tenant, refresh_token: data.refresh_token}, null, 2));
          done(true);
        }
      }
    } catch(e) {}
  }
  return resp;
};

// 3. Make the app ask for a token
// The page keeps its MSAL instance out of reach of the console, so the refresh
// cannot be requested directly. Moving to another route makes the app request
// one; the original page is restored afterwards.
const startPath = location.pathname;
let moved = false;
for (const href of ['/search', '/library', '/teach', '/chat/all', '/chat']) {
  if (href === startPath) continue;
  const link = document.querySelector('a[href="' + href + '"]');
  if (link) { link.click(); moved = true; break; }
}

// 4. Wait for the exchange, then put everything back
const ok = await Promise.race([captured, new Promise(r => setTimeout(() => r(false), 20000))]);
if (moved) history.back();
window.fetch = origFetch;

return ok
  ? 'Done. Copy the JSON printed above.'
  : 'No token exchange seen; the app is still using a token it refreshed a moment ago. Reload the page and run this again.';
})()
```

</details>

4. Wait a few seconds. The snippet moves to another page and back on its own to make the app request a token, then prints: `===== COPY THE COMPLETE JSON BELOW =====`
5. Copy the JSON output. It will look like this:

```json
{
  "oid": "your-oid",
  "tenant": "your-tenant",
  "refresh_token": "your-refresh-token"
}
```

> **Note:** The snippet cannot read the SSO cookies. They live on `login.microsoftonline.com` rather than on this page, and they are `HttpOnly`, so no script on any page can read them. Step 4 collects them by hand.

#### Step 4 (Recommended): Get SSO cookies

Without these two cookies the setup stops working after 24 hours. Collect them by hand:

Microsoft SPA refresh tokens expire after **24 hours**. Without SSO cookies, you must repeat Step 3 every 24 hours. SSO cookies enable automatic renewal and last weeks/months.

To capture SSO cookies:

1. Open [https://login.microsoftonline.com](https://login.microsoftonline.com) in your browser (this is where the cookies live, not m365.cloud.microsoft)
2. Press **F12** to open DevTools, go to **Application** > **Cookies** > `https://login.microsoftonline.com`
3. Find and copy the values of these two cookies:
   - `ESTSAUTH`
   - `ESTSAUTHPERSISTENT`

#### Step 5: Create setup.json

Create a file at `data/setup.json` with the JSON from Step 3. If you captured SSO cookies manually in Step 4, add them to the `sso_cookies` array:

**Without SSO cookies (must re-run setup every 24 hours):**

```json
{"oid":"your-oid","tenant":"your-tenant","refresh_token":"your-refresh-token"}
```

**With SSO cookies (automatic renewal, recommended):**

```json
{
  "oid": "your-oid",
  "tenant": "your-tenant",
  "refresh_token": "your-refresh-token",
  "sso_cookies": [
    {"name": "ESTSAUTH", "value": "paste-estsauth-value-here"},
    {"name": "ESTSAUTHPERSISTENT", "value": "paste-estsauthpersistent-value-here"}
  ]
}
```

#### Step 6: Run the setup wizard

Run the setup wizard inside the container to encrypt and save your credentials:

```bash
docker exec -it m365bridge ./bin/m365-bridge setup-wizard
```


The wizard will:
- Read `data/setup.json`
- Encrypt the refresh token and SSO cookies with AES-256-GCM
- Save environment variables to `data/.env`
- Verify the token by exchanging it for an access token

On success, the server is ready. The API is available at `http://localhost:8230`.

> **Note:** If you did not capture SSO cookies, the refresh token will expire after 24 hours and the server will stop working. Re-run Steps 3, 5, and 6 to get a new token. With SSO cookies, the server automatically renews tokens when they expire.

#### Alternative: docker run

If you prefer `docker run` instead of Docker Compose:

```bash
docker run -d \
  --name m365bridge \
  -p 8230:8000 \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  ghcr.io/kilimcininkoroglu/m365bridge:latest
```

Then follow Steps 3-6 above.

#### Notes

- The `data/` directory stores tokens, cache, and configuration. It is created automatically on first run.
- Port `8230` (host) maps to port `8000` (container). Change the host port in `docker-compose.yml` or the `-p` flag if needed.
- The container starts with `serve --port 8000` by default.
- To build the image from source instead of using the pre-built one: `docker compose up --build -d`

## Usage

### CLI Flags

| Flag            | Type   | Default | Description                                                                                                                                                                        |
|-----------------|--------|---------|------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `-i`            | bool   | false   | Interactive mode (multi-turn conversation)                                                                                                                                         |
| `--model`       | string | `auto`  | Model to use: `auto`, `quick`, `reasoning`, `gpt5.2`, `gpt5.2-reasoning`, `gpt5.3`, `gpt5.4`, `gpt5.4-reasoning`, `gpt5.5`, `gpt5.5-reasoning`, `gpt5.6-reasoning`, `claude`, `claude-sonnet`, `claude-opus`, `claude-sonnet-4-20250514` |
| `--reasoning`   | bool   | false   | Use reasoning mode                                                                                                                                                                 |
| `--no-stream`   | bool   | false   | Disable streaming, print full response at once                                                                                                                                     |
| `--list-models` | bool   | false   | List all available models and exit                                                                                                                                                 |
| `--version`     | bool   | false   | Show version and exit                                                                                                                                                              |

Positional argument (if no flag consumes it): the query text for single query mode.

### Subcommand: serve

Starts the HTTP API server.

| Flag        | Type | Default | Description           |
|-------------|------|---------|-----------------------|
| `--port`    | int  | 8000    | Port to listen on     |
| `--version` | bool | false   | Show version and exit |

### Subcommand: setup-wizard

Runs the browser-based setup wizard. Reads JSON from file containing `oid`, `tenant`, and `refresh_token`.

| Flag     | Type   | Default           | Description             |
|----------|--------|-------------------|-------------------------|
| `--file` | string | `data/setup.json` | Path to setup JSON file |

Every flag is optional. With none, `serve` listens on port 8000 and `setup-wizard` reads `data/setup.json`.

### Core Environment Variables

Configuration is read from `data/.env`; a process environment variable takes precedence over the file. The setup wizard writes the first two.

| Variable         | Default                                | Description                                                                                          |
|------------------|----------------------------------------|------------------------------------------------------------------------------------------------------|
| `M365_TENANT_ID` | required                               | Directory (tenant) ID. The CLI and the server both exit without it.                                  |
| `M365_USER_OID`  | required                               | Object ID of the signed-in user. The CLI and the server both exit without it.                        |
| `M365_CLIENT_ID` | `4765445b-32c6-49b0-83e6-1d93765276ca` | OAuth client the access tokens are issued to. Change it only for a tenant that blocks the default.   |
| `M365_API_KEYS`  | unset                                  | Comma-separated keys a client must present. Unset leaves every `/v1/*` route and `/mcp` open.        |
| `M365_API_KEY`   | unset                                  | A single key, read only when `M365_API_KEYS` is unset.                                               |
| `TZ`             | system zone                            | Timezone sent with each turn. Without it the zone comes from `/etc/localtime`, then UTC.             |

The feature sections below document the remaining variables next to the behaviour they change. `m365-bridge --help` prints all of them in one list with their current defaults.

### Examples

```bash
# Single query
./bin/m365-bridge "your question"

# Interactive mode
./bin/m365-bridge -i

# Specify model with reasoning
./bin/m365-bridge --model gpt5.5-reasoning "your question"

# Non-streaming
./bin/m365-bridge --no-stream "your question"

# List models
./bin/m365-bridge --list-models

# Start API server
./bin/m365-bridge serve --port 8000

# Run setup wizard with custom file
./bin/m365-bridge setup-wizard --file /path/to/setup.json
```

### API Server

```bash
# Start API server on port 8000
./bin/m365-bridge serve --port 8000

# Test with curl (no auth)
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{"model":"auto","messages":[{"role":"user","content":"Hello"}]}'

# Test with curl (with API key)
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{"model":"gpt5.5","messages":[{"role":"user","content":"Hello"}]}'

# Streaming with session isolation
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -H "X-Session-Id: my-session-1" \
  -d '{"model":"gpt5.5","stream":true,"messages":[{"role":"user","content":"Hello"}]}'
```

### First Run

When you start the server for the first time:

1. The server reads `data/.env` from the current working directory
2. It loads the encrypted refresh token from `data/tokens/rt_90day.txt`
3. It performs a token refresh (exchanges refresh token for an access token). This takes 1-2 seconds
4. On success, you will see: `Starting API server on port 8000`
5. The first request may take slightly longer as it opens a WebSocket connection to `substrate.office.com`

If the refresh token is missing or expired, the server will attempt SSO cookie re-authentication if `data/tokens/sso_cookies.json` exists. If SSO cookies are also missing or expired, the server will fail to start with a token refresh error. Re-run `./bin/m365-bridge setup-wizard` to extract fresh tokens and cookies.

### Session Isolation

Each session maps to a unique M365 conversation. Session ID is resolved in priority order:

1. `sessionID` after the colon in the model name (`model:sessionID`)
2. `previous_response_id` field in request body (`/v1/responses` only)
3. `session_id` field in request body
4. `user` field in request body
5. `X-Session-Id` header
6. `X-Claude-Code-Session-Id` header (Claude Code) or `session-id` header (Codex)
7. `hash(api_key + first_user_message)` (when auth is on) or `hash(first_user_message)` (when auth is off)

Every endpoint resolves the session through this one order. `/v1/completions`, `/v1/messages` and `/v1/complete` used to run a shorter chain that read neither `session_id` nor `user` from the body and never reached the hash, so a request to them named no session at all and each turn opened a new conversation.

Claude Code and Codex each stamp their own session on every request of a session, under a header name neither can be told to change. Step 4 reads those two names, so both clients keep one conversation per session without any configuration. It ranks below the fields above because a client writes that header without being asked, while everything above it is a value the caller set deliberately.

Codex also sends `thread-id` carrying the same value as `session-id`, so reading it would answer only for a request that already carries `session-id`. Its `x-codex-turn-metadata` header is never read: the `installation_id` inside stays the same across every session on one machine, so keying a conversation on it would merge unrelated sessions into one.

The hash fallback covers any other client, as long as its first user message differs.

`GET /v1/sessions` lists the mappings, newest first. Entries written before the mapping carried its session ID cannot be listed, because the cache file name is a hash of the key; they are reported as a `legacy_entries` count and appear in the list after their next turn rewrites them.

`DELETE /v1/sessions/{id}` deletes the upstream M365 conversation and then clears the mapping, so the next turn on that session ID starts a fresh conversation. The mapping is kept when the upstream delete fails, so the request can be retried. Deleting the conversation needs the M365 web cookies in `data/tokens/m365_cookies.json`; add `?local_only=true` to clear only the mapping and leave the conversation in place, which is what a deployment without those cookies needs.

### System Instructions

The M365 backend keeps conversation history itself and receives only the latest turn, so an instruction sent in an earlier message would never reach it. Every `system` message in the request is therefore collected and prefixed to that turn, and kept out of the flattened history, where it would otherwise read as a past conversation line.

`developer` is treated identically. OpenAI renamed the role for its reasoning models and both names remain valid, so a client that sends either reaches the model the same way.

Anthropic's top-level `system` field is accepted as a string or as an array of text blocks, and becomes the same prefixed instruction.

### Python Client (OpenAI SDK)

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:8000/v1",
    api_key="your-api-key",  # required if M365_API_KEYS is set
)
resp = client.chat.completions.create(
    model="gpt5.5",
    messages=[{"role": "user", "content": "Hello"}]
)
print(resp.choices[0].message.content)
```

### Python Client (Anthropic SDK)

```python
from anthropic import Anthropic

client = Anthropic(
    base_url="http://127.0.0.1:8000/v1",
    api_key="your-api-key",  # required if M365_API_KEYS is set
)
resp = client.messages.create(
    model="gpt5.5",
    max_tokens=1024,
    messages=[{"role": "user", "content": "Hello"}]
)
print(resp.content[0].text)
```

### Image Input Example

```python
from openai import OpenAI
import base64

client = OpenAI(
    base_url="http://127.0.0.1:8000/v1",
    api_key="your-api-key",
)

with open("image.png", "rb") as f:
    img_b64 = base64.b64encode(f.read()).decode()

resp = client.chat.completions.create(
    model="gpt5.5",
    messages=[{
        "role": "user",
        "content": [
            {"type": "text", "text": "What is in this image?"},
            {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{img_b64}"}},
        ],
    }],
)
print(resp.choices[0].message.content)
```

## Web Interface

Open the server's root URL in a browser (`http://localhost:8230/` under the shipped Docker setup). The interface is compiled into the binary, so there is no separate asset directory and no second process to run.

It lists conversations in a sidebar, streams answers as they arrive, lets you pick a model from `GET /v1/models`, and creates, renames and deletes conversations.

The page itself is served without an API key, because the screen that asks for the key cannot require one. Every data call it makes goes through the same `withAuth` middleware as any other client. The key is stored in a cookie and sent in the `Authorization` header, never as a cookie the browser attaches on its own, so no cross-site request can carry it.

### What the sidebar shows

Two sources are merged. `GET /v1/conversations` supplies the names and needs M365 web cookies; `GET /v1/sessions` supplies the session ids that make a conversation continuable. A conversation present in both is one row.

Without cookies the first call fails, the sidebar falls back to the local mappings alone and says so. A conversation that only M365 knows is marked and gets a session id bound to it the moment you open it, which is what makes a conversation started on another client continuable here.

### Transcripts

The backend tracks history by conversation ID and never replays it, so the gateway keeps its own record of the turns it carried, one file per session under `data/transcripts`. This is the only place message content reaches disk. Entries per session, bytes per message and files in the store are all bounded.

A conversation started outside this gateway has no record, so its history is empty when you open it. The interface says so and offers to fetch it, which is `GET /v1/conversations/{id}/messages` (see below). Deleting a session deletes its transcript, and so does a turn that produced nothing, since both start a new conversation under that id.

### Configuration

| Variable              | Default | Description                                                                                       |
|-----------------------|---------|---------------------------------------------------------------------------------------------------|
| `M365_ENABLE_WEB_UI`  | `1`     | Serves the interface at `/` and records transcripts. `0`, `false`, `off` or `no` disables both.    |

Turning it off removes the interface (`/` returns 404) and stops the recording, which is what a deployment that only proxies wants. `GET /v1/sessions/{id}/messages` then answers `404 transcripts_disabled`.

### Building the interface

The sources live in `web/` and the build output is committed at `pkg/webui/dist`, because `go:embed` reads it at compile time. Rebuild it after changing anything under `web/`:

```bash
make ui      # builds in a node container and copies the output into pkg/webui/dist
make up      # rebuilds the image and restarts the container
```

## API Endpoints

| Endpoint                         | Description                                            |
|----------------------------------|--------------------------------------------------------|
| `POST /v1/chat/completions`      | OpenAI Chat Completions (streaming + non-streaming)    |
| `POST /v1/completions`           | OpenAI text completion (streaming + non-streaming)     |
| `POST /v1/responses`             | OpenAI Responses API (streaming + non-streaming)       |
| `POST /v1/responses/compact`     | OpenAI Responses Compact API (Codex remote compaction) |
| `POST /v1/messages`              | Anthropic Messages format (dedicated SSE handlers)     |
| `POST /v1/messages/count_tokens` | Anthropic input token counting                         |
| `POST /v1/complete`              | Anthropic Complete (FIM)                               |
| `POST /v1/images/generations`    | OpenAI Images API: generate from text (JSON body)      |
| `POST /v1/images/edits`          | OpenAI Images API: edit existing image (multipart)     |
| `GET /v1/conversations`          | List M365 conversations (requires M365 web cookies)    |
| `POST /v1/conversations`         | Create a conversation with an initial message          |
| `PATCH /v1/conversations/{id}`   | Rename a conversation with `{ "name": "..." }`         |
| `DELETE /v1/conversations/{id}`  | Permanently delete a conversation                      |
| `GET /v1/conversations/{id}/messages` | Read the turns of a conversation held upstream    |
| `GET /v1/models`                 | Model list                                             |
| `GET /v1/quota`                  | Last observed M365 conversation message quota          |
| `GET /v1/sessions`               | List the session to conversation mappings              |
| `GET /v1/sessions/{id}`          | Read one session's conversation ID                     |
| `PUT /v1/sessions/{id}`          | Bind a session to an existing conversation             |
| `GET /v1/sessions/{id}/messages` | Read the recorded turns of a session                   |
| `DELETE /v1/sessions/{id}`       | Delete the conversation and clear the mapping          |
| `POST /mcp`                      | Model Context Protocol server (JSON-RPC 2.0)           |
| `GET /v1/health`                 | Reachability probe for Codex (no auth required)        |
| `GET /health`                    | Health check (no auth required)                        |
| `GET /`                          | Browser interface (no auth required for the page)      |

`PUT /v1/sessions/{id}` takes `{"conversation_id": "..."}` and points a session at a conversation that already exists. The chat path only ever resolves a session to a conversation, so without this a conversation started in the M365 web or mobile client could not be continued through the gateway. Rebinding an existing session is allowed.

`GET /v1/sessions/{id}/messages` returns what the gateway recorded for that session. It answers `404 transcripts_disabled` when `M365_ENABLE_WEB_UI` is off, and an empty list for a conversation that was started elsewhere.

`GET /v1/conversations/{id}/messages` reads the turns of a conversation this gateway never carried. The backend keeps history under the conversation ID and offers no action that returns it, so this recovers it from the conversation page the M365 web client renders, which needs M365 web cookies. It costs a page download and a walk of a serialization this project does not control, so nothing calls it automatically. Add `?session_id=...` to store the result under that session and bind the session to the conversation, which is what the interface does behind its "load history" button; without it the response is returned and nothing is written. A page that carries no readable turn answers `502` rather than an empty conversation, because a caller cannot tell an empty conversation from a failed read.

## Error Responses

Every endpoint reports failures in the OpenAI error shape. `type` is the category a client branches on, `code` is the specific machine-readable reason:

```json
{"error": {"message": "M365 rate limit reached for this chat request; retry after the interval in the Retry-After header", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}
```

`type` is one of `invalid_request_error`, `authentication_error`, `rate_limit_error` or `server_error`. For a request the proxy rejects on its own, `code` is the status slug, for example `bad_request` or `method_not_allowed`.

A failed backend request is classified rather than reported as a generic `500`:

| Status | `code`                     | Cause                                                        |
|--------|----------------------------|--------------------------------------------------------------|
| `401`  | `upstream_auth_failed`     | The stored credentials are missing or could not be refreshed |
| `403`  | `insufficient_permissions` | M365 refused the request for the configured account          |
| `429`  | `rate_limit_exceeded`      | M365 throttled the request; a `Retry-After` header is sent   |
| `429`  | `upstream_throttled`       | The conversation message quota is exhausted                  |
| `409`  | `tool_round_limit`         | One turn drove more tool rounds than `M365_MAX_TOOL_ROUNDS`  |
| `404`  | `model_not_found`          | The requested model is not in `GET /v1/models`                |
| `502`  | `upstream_error`           | M365 rejected the request or was unreachable                 |
| `502`  | `upstream_unavailable`     | The WebSocket handshake failed or the connection dropped     |
| `502`  | `upstream_turn_failed`     | M365 ended the turn without producing an answer              |
| `502`  | `upstream_content_blocked` | M365 declined the request instead of answering it            |
| `503`  | `upstream_unavailable`     | M365 reported itself unavailable                             |
| `504`  | `upstream_timeout`         | M365 did not answer in time                                  |

A failure with no evidence of an upstream cause still reports `500` with `internal_error`, so a bug in the proxy is not presented as a backend outage. Error messages are fixed text: the transport error, including request URLs and credential file paths, stays in the server log.

Once a stream has opened the status is already sent, so the same classification travels in the body. OpenAI-shaped routes put an `error` object on the data line and then `[DONE]`; `/v1/messages` and `/v1/complete` send an `error` event; `/v1/responses` sends `response.failed`. No route writes the failure as assistant content, which a client would otherwise store as the answer.

## Models

All model selection is via the `tone` field sent to the M365 backend. The `Override` field is empty for all models. GPT-5.x models route to the GPT-5 backend. Claude tone values return Claude responses, but M365 does not expose the underlying model identity in SignalR metadata.

| Key                        | Tone              | OpenAI ID         | Thinking? | Backend |
|----------------------------|-------------------|-------------------|-----------|---------|
| `auto`                     | Magic             | gpt-4-auto        | No        | GPT-5   |
| `quick`                    | Chat              | gpt-4-quick       | No        | GPT-5   |
| `reasoning`                | Gpt_5_2_Reasoning | gpt-4-reasoning   | Yes       | GPT-5   |
| `gpt5.2-reasoning`         | Gpt_5_2_Reasoning | gpt-5.2-reasoning | Yes       | GPT-5   |
| `gpt5.4-reasoning`         | Gpt_5_4_Reasoning | gpt-5.4-reasoning | Yes       | GPT-5   |
| `gpt5.2`                   | Gpt_5_2_Chat      | gpt-5.2           | No        | GPT-5   |
| `gpt5.3`                   | Gpt_5_3_Chat      | gpt-5.3           | No        | GPT-5   |
| `gpt5.4`                   | Gpt_5_4_Chat      | gpt-5.4           | No        | GPT-5   |
| `gpt5.5`                   | Gpt_5_5_Chat      | gpt-5.5           | No        | GPT-5   |
| `gpt5.5-reasoning`         | Gpt_5_5_Reasoning | gpt-5.5-reasoning | Yes       | GPT-5   |
| `gpt5.6-reasoning`         | Gpt_5_6_Reasoning | gpt-5.6-reasoning | Yes       | GPT-5   |
| `claude`                   | Claude_Sonnet     | claude-sonnet-4.6 | No        | Claude  |
| `claude-sonnet`            | Claude_Sonnet     | claude-sonnet-4.6 | No        | Claude  |
| `claude-opus`              | Claude_Opus       | claude-opus-4.6   | Yes       | Claude  |
| `claude-sonnet-4-20250514` | Claude_Sonnet     | claude-sonnet-4.6 | No        | Claude  |

### Which model should I use?

| Use case                                  | Model              |
|-------------------------------------------|--------------------|
| General purpose, let backend decide       | `auto`             |
| Fast responses, simple questions          | `quick`            |
| Complex reasoning, multi-step problems    | `reasoning`        |
| GPT-5.2 with deep thinking                | `gpt5.2-reasoning` |
| GPT-5.4 with deep thinking                | `gpt5.4-reasoning` |
| GPT-5.2 chat                              | `gpt5.2`           |
| GPT-5.3 chat                              | `gpt5.3`           |
| GPT-5.4 chat                              | `gpt5.4`           |
| GPT-5.5 chat                              | `gpt5.5`           |
| GPT-5.5 with deep thinking                | `gpt5.5-reasoning` |
| GPT-5.6 with deep thinking (latest)       | `gpt5.6-reasoning` |
| Claude Sonnet 4.6 (Anthropic)             | `claude-sonnet`    |
| Claude Opus 4.6 (Anthropic, most capable) | `claude-opus`      |

A reasoning model produces `reasoning_content` output containing the model's thinking process. OpenAI endpoints expose this as `reasoning_content`; Anthropic endpoints expose it as a `thinking` content block before the `text` block. `claude-opus` produces reasoning content as well; `claude-sonnet` does not. `gpt5.6-reasoning` advertises the capability but has not been observed emitting it. The advertised capability comes from the measured behaviour of each tone rather than from its name.

### Session ID in Model Name

You can embed a session ID directly in the model name using the `:` separator. Claude Code and Codex are already handled by step 4 of [Session Isolation](#session-isolation), so reach for this when you want to name the session yourself, or for a client that sends no session header at all:

```
model: "gpt5.5-reasoning:my-session-001"
```

This is equivalent to setting `X-Session-Id: my-session-001` header or `session_id: "my-session-001"` in the request body. The model key is extracted before the `:` and the session ID is extracted after it.

### External Model Names

A model name this gateway does not serve is answered with `404 model_not_found`, never with another entry. An unknown name used to fall back to the default model, which meant a caller was answered by a tone it never asked for and a model removed from the registry appeared to keep working.

The registry carries the vendor names agent clients send, so `claude-sonnet-4-20250514` resolves; `gpt-4o` and `o1` do not and return `404`. `GET /v1/models` lists every id that is served.

A request that sends no model at all defaults to `gpt5.5-reasoning`, the reasoning tone that is reliable for tool calling, rather than `auto`. This covers an empty `model` field, a missing one, and a bare `:session-id` suffix.

### Advertised Context Window

Each entry in `GET /v1/models` advertises `context_window` and `max_output_tokens` hints so client harnesses do not pre-truncate prompts or output. These are client-facing hints only; M365 enforces its own server-side limits regardless. Both default to `1000000` and are overridable:

| Variable                 | Default   | Description                                            |
|--------------------------|-----------|--------------------------------------------------------|
| `M365_CONTEXT_WINDOW`    | `1000000` | Advertised context window token count in `/v1/models`. |
| `M365_MAX_OUTPUT_TOKENS` | `1000000` | Advertised maximum output token count in `/v1/models`. |

### Model List Fields

`GET /v1/models` lists each model once, keyed by its advertised id and sorted, so aliases such as `claude` and `claude-sonnet` do not appear twice. Each entry carries:

| Field               | Description                                                                                     |
|---------------------|-------------------------------------------------------------------------------------------------|
| `owned_by`          | `anthropic-via-microsoft-365` for the Claude tones, `microsoft-365` for the rest.                 |
| `context_window`    | The advertised window, from `M365_CONTEXT_WINDOW`.                                                |
| `max_output_tokens` | The advertised output budget, from `M365_MAX_OUTPUT_TOKENS`.                                      |
| `max_input_tokens`  | The window minus the output budget, or the full window when the output budget is not smaller.     |
| `supports_tools`    | Always `true`; every model reaches caller-defined tools through the simulated tool calling layer. |

The response also carries `reasoning_effort_presets`, each an `{effort, description}` pair naming an effort value the Responses API accepts.

Each entry additionally carries the model-catalog fields Codex CLI reads, which plain OpenAI clients ignore: `base_instructions`, `model_messages`, `default_reasoning_level`, `apply_patch_tool_type`, `shell_type`, `tool_mode`, `truncation_policy`, `supports_parallel_tool_calls`, and the verbosity and reasoning-summary defaults. Every capability is repeated both at the top level and under `capabilities`, because OpenAI-compatible clients disagree on where to look for it.

#### Both wire formats in one response

The route answers OpenAI and Anthropic clients at once, because both protocols reach the proxy on the same path. Each entry is a valid OpenAI model object and a valid Anthropic `ModelInfo` at the same time; the two field sets do not collide, so each client reads only what it knows.

| Field               | Protocol  | Description                                                              |
|---------------------|-----------|--------------------------------------------------------------------------|
| `object`            | OpenAI    | Always `model`.                                                          |
| `created`           | OpenAI    | Unix seconds.                                                            |
| `owned_by`          | OpenAI    | The vendor behind the tone.                                              |
| `shutdown_date`     | OpenAI    | Always `null`; no model is scheduled to retire.                          |
| `type`              | Anthropic | Always `model`.                                                          |
| `display_name`      | Anthropic | Human-readable name, for example `Claude Sonnet 4.6`.                    |
| `created_at`        | Anthropic | The same instant as `created`, in RFC 3339.                              |
| `max_tokens`        | Anthropic | The output ceiling, matching `max_output_tokens`.                        |

The list itself carries `object` and `data` for OpenAI, and `has_more`, `first_id` and `last_id` for Anthropic. The whole registry fits in one page, so `has_more` is always `false` and the cursors are the first and last advertised id.

`capabilities` holds Anthropic's capability tree alongside the flat OpenAI-style entries: `batch`, `citations`, `code_execution`, `context_management`, `effort`, `image_input`, `pdf_input`, `structured_outputs` and `thinking`, each a `{"supported": bool}` leaf. The values state what the proxy actually does, so most read `false`. `effort` is `true` only for a model that has a `-reasoning` variant to route to, and `thinking` only for a reasoning tone, which is the one that emits chain-of-thought content.

Claude Code discovers gateway models through this route, and it reads only the Anthropic format and only adds ids beginning with `claude` or `anthropic`. The Claude tones therefore keep such ids.

### Conversation Quota

M365 enforces a per-conversation message ceiling and reports the counters on its update frames. Every turn logs them, for example `ConvStream throttling: used=8 max=600 headroom=592`.

`GET /v1/quota` returns the last observed counters. The backend only sends them while a turn is in flight, so the values reflect the most recent chat request rather than a live lookup, and they belong to whichever conversation produced that request:

```json
{"object":"quota","available":true,"exhausted":false,"used":8,"max":600,"headroom":592}
```

Counters the proxy does not recognize are returned under `extra` instead of being dropped. When a request produces an empty upstream response and the last counters show the ceiling was reached, the proxy answers `429` with code `upstream_throttled` rather than a generic empty-response error; start a new session to continue.

### Token Usage

Prompt and completion token counts are estimates produced locally; the M365 backend reports no usage. The encoder is `o200k_base`, the encoding of the GPT-5 class models the backend serves, with `cl100k_base` as the fallback and a character-based estimate when neither vocabulary can be fetched. Every `usage` object names which one produced the numbers:

```json
{"prompt_tokens": 42, "completion_tokens": 17, "total_tokens": 59, "usage_source": "tiktoken_o200k_base_estimate"}
```

`usage_source` is a non-standard field; the standard fields keep their meaning and position. Every endpoint reports usage, streaming and non-streaming alike, including `/v1/complete`, whose own format defines no usage object.

The Anthropic endpoints report the same counts under their own field names, plus two fields the Anthropic format does not define:

```json
{"input_tokens": 42, "output_tokens": 17, "reasoning_tokens": 6, "usage_source": "tiktoken_o200k_base_estimate"}
```

A streaming `/v1/messages` turn splits that object the way the Anthropic wire format does: `message_start` carries the input side, `message_delta` carries the output side, and both name their source. A streaming `/v1/complete` turn reports usage on its final `completion` event, because the earlier events carry deltas.

`/v1/chat/completions` and `/v1/completions` accept the OpenAI `stream_options` object. `{"include_usage": false}` withholds the usage object from a streaming turn. Leaving `stream_options` out keeps the usage object, which differs from OpenAI's own default of `false`: this proxy has always reported usage on every streaming turn and clients here read it. Prompt tokens are counted from the message roles and contents, the serialized tool definitions and the `tool_choice` value, plus a fixed per-message and per-tool framing allowance. The `tool_choice` allowance applies only when the request declared tools. The same turn therefore costs the same on every endpoint.

### Stop Sequences

A stop sequence ends the answer where the caller said it ends. Every chat endpoint accepts one under its own protocol's name:

| Endpoint                | Field           | Shape                        |
|-------------------------|-----------------|------------------------------|
| `/v1/chat/completions`  | `stop`          | A string or an array of them |
| `/v1/completions`       | `stop`          | A string or an array of them |
| `/v1/messages`          | `stop_sequences`| An array of strings          |
| `/v1/complete`          | `stop_sequences`| An array of strings          |

The answer is cut just before the earliest sequence that appears in it, and the sequence itself is removed, so a caller that frames a turn does not read the frame back. With several sequences the answer ends at whichever arrives first, not at whichever was listed first. An empty sequence is ignored rather than matching at offset zero.

The OpenAI endpoints report the ordinary `finish_reason: "stop"`, the same as an answer that ended on its own. The Anthropic endpoints report `stop_reason: "stop_sequence"` and name the sequence that fired: `/v1/messages` in `stop_sequence`, `/v1/complete` in `stop`. Both fields stay `null` when the answer ended on its own, so a client testing for null is not misled by an empty string. `max_tokens` still wins when it is reached first, and the reported reason becomes `max_tokens`.

A streamed answer is cut as it is produced, not afterwards. A sequence can straddle two upstream chunks, so the deltas pass through a writer that holds back the tail which could still complete one, released on a character boundary. A request that sends no stop sequence holds nothing back and receives every chunk as it arrives.

## MCP Server

`POST /mcp` exposes M365 Copilot to Model Context Protocol clients over JSON-RPC 2.0 (protocol revision `2025-06-18`). It supports `initialize`, `tools/list`, `tools/call`, and `ping`; lifecycle notifications are acknowledged with `202` and no body. The route requires an API key when one is configured.

| Tool | Arguments | Description |
|------|-----------|-------------|
| `ask_copilot` | `prompt` (required), `model` | One stateless Copilot turn returning text |
| `describe_image` | `image_url` (required, data URI), `prompt`, `model` | Asks Copilot about an inline image |

```bash
curl -s -X POST http://localhost:8000/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ask_copilot","arguments":{"prompt":"Summarize the CAP theorem"}}}'
```

Copilot is deliberately a leaf in the MCP role. The simulated tool calling used by the `/v1` endpoints is **not** offered through MCP: an MCP client already has a real, schema-enforced tool mechanism, and nesting the prompt-based emulation inside it would create two competing tool loops. Every MCP call is an independent turn with no conversation continuity.

## Tool Calling

M365Bridge supports **simulated tool calling** — client-defined tools (Claude Code's Read/Bash/Write, Codex tools, etc.) work without M365 backend natively supporting them.

### How It Works

1. Client sends a request with `tools` array (OpenAI function definitions or Anthropic tool schemas)
2. M365Bridge embeds the entire request JSON into the prompt sent to M365 Copilot
3. M365 Copilot returns a full response JSON in a ```` ```json ```` block
4. M365Bridge parses the response and extracts tool calls into OpenAI `tool_calls` or Anthropic `tool_use` content blocks
5. Client executes the tool and sends the result back in the next message

This works on both OpenAI (`/v1/chat/completions`) and Anthropic (`/v1/messages`) endpoints, in both streaming and non-streaming modes.

### Example (OpenAI)

```bash
curl http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "messages": [{"role": "user", "content": "Run: echo hello"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "bash",
        "description": "Run a shell command",
        "parameters": {
          "type": "object",
          "properties": {"command": {"type": "string"}},
          "required": ["command"]
        }
      }
    }],
    "tool_choice": "required"
  }'
```

Response:

```json
{
  "choices": [{
    "finish_reason": "tool_calls",
    "message": {
      "role": "assistant",
      "tool_calls": [{
        "id": "call_001",
        "type": "function",
        "function": {
          "name": "bash",
          "arguments": "{\"command\": \"echo hello\"}"
        }
      }]
    }
  }]
}
```

### Example (Anthropic)

```bash
curl http://127.0.0.1:8000/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Run: echo hello"}],
    "tools": [{
      "name": "bash",
      "description": "Run a shell command",
      "input_schema": {
        "type": "object",
        "properties": {"command": {"type": "string"}},
        "required": ["command"]
      }
    }],
    "tool_choice": {"type": "any"}
  }'
```

Response:

```json
{
  "content": [{
    "type": "tool_use",
    "id": "toolu_001",
    "name": "bash",
    "input": {"command": "echo hello"}
  }],
  "stop_reason": "tool_use"
}
```

### Notes

- Tool calling is always enabled — no configuration needed. Requests without `tools` are unaffected.
- Tool call arguments are validated against the declared JSON schema: `type`, `enum`, `required`, nested `properties`, and array `items`. A call that violates the contract is dropped, and the proxy performs a single corrective re-ask carrying the rejection reason so agent clients never receive an unexecutable call. This works best for single-step tool calls; sustained multi-round agent loops (for example Claude Code's `/init` or sub-agent tasks) depend on the M365 backend model's own tool-use reliability and are not guaranteed.
- Under `additionalProperties: false`, arguments the schema does not declare are removed rather than rejected, so one stray field does not cost a round trip.
- `tool_choice` is enforced when the response is parsed, not only asked for in the prompt. Under `"none"` no call is forwarded; when a specific function is pinned, a call to any other tool is dropped and re-asked.
- `parallel_tool_calls: false` (OpenAI, on `/v1/chat/completions` and `/v1/responses`) and `tool_choice.disable_parallel_tool_use: true` (Anthropic, on `/v1/messages`) are enforced the same way: at most one call is forwarded per turn, and the rest are dropped rather than reordered, because the model emits them in the order it wants them run and the next round can ask for the following one. Leaving the field out allows parallel calls, which is the default in both protocols.
- Every tool call id is a fresh `call_<uuid>`. The backend's own ids repeat across turns, which clients reject as duplicates.
- A tool result whose `tool_call_id` (OpenAI), `tool_use_id` (Anthropic), or `call_id` (Responses) is missing, or names a call the same request never declared, is rejected with HTTP 400. A request that declares no tool calls at all skips the id check, so a client that trimmed its history is not blocked.
- When the backend answers a tool request with prose that denies the tools exist, claims to have run the work in its own sandbox, or states that it cannot reach the caller's machine, the proxy re-asks once with an explicit instruction. The phrasings are recognized in English, Chinese and Turkish. An ordinary text answer passes through untouched.
- When M365 Copilot runs its own server-side tools (web search, code interpreter) and returns plain text instead of a simulated JSON payload, the response is returned as a normal text completion with `finish_reason: "stop"`.
- When M365 raises a tool call for one of its own built-ins (`search`, `code_interpreter`, `trigger_plugin`, `invoke_action`), that call is dropped and the turn ends on `stop`. This holds even when the request declares no tools at all: the client never declared those names and cannot execute them, and the answer already carries the search results inline.
- When the backend answers with an unparseable tool-calling envelope, the envelope is withheld instead of being forwarded as the assistant message; an answer that was nothing but envelope becomes a short notice.
- When M365 refuses the request itself rather than answering it, non-streaming endpoints return HTTP 502 with `upstream_content_blocked`, so the refusal is not mistaken for an answer. A streaming turn has already opened its response, so it is logged instead.
- `tool_result` messages (OpenAI) and `tool_use`/`tool_result` content blocks (Anthropic) in conversation history are converted to plain text before being sent to M365, since the M365 backend does not understand tool roles.
- Streaming endpoints buffer the full response before parsing tool calls (tool call JSON may span multiple chunks). While that buffer fills, the stream writes a keepalive frame every ten idle seconds so the connection does not look dead to the client.

### Client-Driven Tool Loops

Agent clients such as Claude Code and Codex drive the tool loop themselves and resend the whole call and result history on every request. The proxy holds no state between those requests, so it rebuilds the evidence of the current user turn from the incoming history. A turn starts at the last user message that carries no tool result, which keeps the Anthropic shape, where every result arrives as a user message, from looking like a new turn.

| Variable               | Default | Description                                                                 |
|------------------------|---------|-----------------------------------------------------------------------------|
| `M365_MAX_TOOL_ROUNDS` | `32`    | Tool rounds one user turn may drive before HTTP 409. Capped at `512`.        |
| `M365_ENABLE_WEB_SEARCH` | `1`   | Declares the M365 `BingWebSearch` built-in on every turn. `0`, `false`, `off` or `no` withholds it. |

- Exceeding the cap returns HTTP 409 with code `tool_round_limit` and reports the round count. HTTP 409 is not a status the Anthropic SDK expects, but an explicit refusal is preferable to answering forever while the client asks for one more round.
- The completed calls and their results are restated in the prompt as final evidence, so the model answers from a result it already has instead of asking for it again. When the same call has failed the same way more than once, the prompt also asks for a change of approach.
- A tool call repeating a name and arguments whose result is already in the turn is dropped on the third identical attempt. The first repeat passes, because reading a file back after writing it or re-running the tests after a change are ordinary. A call demanded through `tool_choice` is always forwarded, and a drop never triggers the corrective re-ask, because re-asking would produce the same call again.
- Each restated result is compacted to a head and a tail around a marker naming the removed size, so a long build log does not grow the prompt on every round of the loop.
- A tool call id declared twice, or answered twice, is rejected with HTTP 400: nothing can tell which call a later result belongs to.
- A reply that only announces which tool it means to use, naming a declared tool in a short sentence without a code fence, is re-asked once. If the retry stays an announcement the answer text is replaced, so the client is not left waiting for a call that never comes.
- A `function_call_progress` input item lets a long-running client tool report intermediate status. It reaches the model as context but never answers the pending call and never starts a new user turn.
- A grammar-constrained tool (`"type": "custom"`, such as Codex code mode's `exec`) takes a raw body rather than JSON arguments. When the backend emits that body unfenced, either as a lone `{"input": "..."}` object or as bare source, it is claimed as a `custom_tool_call` on `/v1/responses` instead of being forwarded as escaped text.
- A client-declared `web_search` tool is never routed back to the client: M365 runs the search itself through its `BingWebSearch` built-in and writes the results into the answer. The declaration stays in the prompt so the model knows the capability exists. When `web_search` is the only declared tool, the request drops out of the simulated tool path entirely and streams as ordinary text.
- When the request declares tools, the turn emits no tool call, and no tool result exists, an answer claiming in the first person to have carried the work out is replaced with a short statement that nothing was verified; the original text is logged at debug level. A third-person statement such as "Go was created at Google" and a long prose answer are never touched. The replacement also applies to the streaming Chat Completions, Messages and Completions endpoints, which buffer a tool-enabled turn until the parse is done. Only `/v1/responses` streaming publishes content as it decodes it, so there the case is logged instead.

## Built-in Coding Tools (Opt-in)

M365Bridge can execute a restricted set of local coding operations on the server. This feature is **disabled by default** and its main gate is `M365_ENABLE_CODE_TOOLS=1`. It is available on OpenAI Chat Completions (`/v1/chat/completions`), Anthropic Messages (`/v1/messages`), and OpenAI Responses (`/v1/responses`).

When enabled, tools explicitly included in a request are recognized and executed locally. `M365_AUTO_EXPOSE_TOOLS=1` also adds all built-in tools to requests automatically; leave it at `0` when clients should select tools explicitly. The server sends local results back to the model and continues until the model returns a final answer, emits a caller-defined tool call, or reaches the iteration limit. Because tool calls and intermediate results must be collected first, requests using built-in tools buffer the complete model response even when `stream: true`, then emit the provider-compatible streaming response.

### Configuration

| Variable                        | Default   | Description                                                                                    |
|---------------------------------|-----------|------------------------------------------------------------------------------------------------|
| `M365_ENABLE_CODE_TOOLS`        | `0`       | Main gate. Set to `1` to enable local tool execution.                                          |
| `M365_AUTO_EXPOSE_TOOLS`        | `0`       | Set to `1` to inject all built-in tool schemas when the client does not provide them.          |
| `M365_WORKSPACE_DIR`            | `.`       | Existing directory that confines file and Git operations.                                      |
| `M365_CODE_TOOL_TIMEOUT`        | `30s`     | Timeout for each command or test execution. Accepts Go duration syntax, such as `10s` or `2m`. |
| `M365_CODE_TOOL_MAX_OUTPUT`     | `1048576` | Maximum captured command output in bytes. Longer output is truncated.                          |
| `M365_CODE_TOOL_MAX_READ_BYTES` | `1048576` | Maximum number of bytes returned by a file read.                                               |
| `M365_CODE_TOOL_MAX_ITERATIONS` | `10`      | Maximum model/tool loop iterations per request.                                                |

Set these variables in `data/.env`. For Docker, `M365_WORKSPACE_DIR` must refer to a directory that already exists inside the container. The provided Compose file mounts only `./data` at `/app/data`; it does not expose a host source workspace.

### Available Tools

| Tool            | Operation                                                        |
|-----------------|------------------------------------------------------------------|
| `list_files`    | List files and directories under a workspace path.               |
| `read_file`     | Read a file, subject to the configured byte limit.               |
| `write_file`    | Create or replace a file inside the workspace.                   |
| `search_files`  | Search workspace file contents.                                  |
| `git_status`    | Show workspace Git status.                                       |
| `git_diff`      | Show workspace Git changes.                                      |
| `git_log`       | Show recent workspace Git history.                               |
| `shell_command` | Run a shell command with the workspace as its working directory. |
| `apply_patch`   | Apply a unified patch inside the workspace.                      |
| `run_tests`     | Run a test command with the configured timeout and output limit. |

### Security Requirements

Enabling these tools turns the API into a remote code and file access surface. **Configure `M365_API_KEYS` or `M365_API_KEY` before enabling them; API key authentication is mandatory for every deployment with coding tools enabled.** Do not expose such a deployment directly to the public internet. Use a least-privilege service account, a dedicated workspace, strict filesystem permissions, network isolation, and container resource limits.

- **OWASP Broken Access Control:** a missing, leaked, or shared API key can let unauthorized callers read, modify, or execute within the mounted workspace. Use unique, rotated keys and enforce authorization at a trusted reverse proxy as well.
- **Command Injection:** `shell_command` and `run_tests` execute model-selected command strings. Treat prompts, repository content, patches, and tool arguments as untrusted input; isolate the process and never provide production credentials.
- **Path Traversal:** file tools confine resolved paths to `M365_WORKSPACE_DIR`, but an overly broad workspace or unsafe mount still exposes sensitive files. Mount only the required project directory and review symlinks and permissions.
- **Sensitive Data Exposure:** tool output and file contents can be returned to the caller and sent to the M365 backend. Keep secrets, tokens, `.env` files, SSH keys, cloud credentials, and customer data outside the workspace.
- **Resource exhaustion:** commands, recursive searches, large files, output, and repeated tool loops can consume CPU, memory, disk, and process capacity. Keep timeout, output, read, and iteration limits conservative and enforce container or OS quotas.

## Responses API

The `/v1/responses` endpoint implements the OpenAI Responses API format. It accepts `input` (string or array of typed items), `instructions`, `max_output_tokens`, `tools`, `reasoning`, and `previous_response_id` for conversation continuity.

### Reasoning Effort

Codex CLI sends `reasoning: {"effort": ..., "summary": ...}`. The accepted effort values are `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`; anything else is rejected with HTTP 400 rather than ignored.

M365 exposes no separate effort dial, so effort steers the only lever that exists: `medium` and above routes the request to the model's reasoning variant when the registry has one, for example `gpt5.5` to `gpt5.5-reasoning`. A model without a variant, or a key that already names one, is left unchanged. `summary` is accepted and not acted on.

### Custom Tools

A tool declared with `"type": "custom"` takes free-form text rather than JSON arguments. Its calls come back as `custom_tool_call` items with the text under `input`, and the matching `custom_tool_call` / `custom_tool_call_output` history items are read back on the next turn.

### Example (non-streaming)

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5",
    "input": "What is 2+2?",
    "session_id": "my-session"
  }'
```

Response:

```json
{
  "id": "resp_...",
  "object": "response",
  "created_at": 1234567890,
  "status": "completed",
  "model": "gpt-5.5",
  "output": [{
    "id": "msg_...",
    "type": "message",
    "status": "completed",
    "role": "assistant",
    "content": [{"type": "output_text", "text": "2+2 equals 4.", "annotations": []}]
  }],
  "output_text": "2+2 equals 4.",
  "usage": {"input_tokens": 5, "output_tokens": 8, "total_tokens": 13}
}
```

### Example (with instructions and input items)

```bash
curl http://127.0.0.1:8000/v1/responses \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "instructions": "You are a concise assistant.",
    "input": [{"role": "user", "content": [{"type": "input_text", "text": "Explain recursion"}]}],
    "stream": true
  }'
```

### Streaming Events

The streaming endpoint emits typed SSE events:

| Event                                    | Description                                                  |
|------------------------------------------|--------------------------------------------------------------|
| `response.created`                       | Response object created (status: in_progress)                |
| `response.in_progress`                   | Response is being generated                                  |
| `response.output_item.added`             | New output item added (message, reasoning, or function_call) |
| `response.content_part.added`            | Content part added to message item                           |
| `response.output_text.delta`             | Text delta                                                   |
| `response.output_text.done`              | Text complete                                                |
| `response.content_part.done`             | Content part complete                                        |
| `response.output_item.done`              | Output item complete                                         |
| `response.reasoning_summary_text.delta`  | Reasoning/thinking delta                                     |
| `response.reasoning_summary_text.done`   | Reasoning complete                                           |
| `response.function_call_arguments.delta` | Tool call arguments delta                                    |
| `response.function_call_arguments.done`  | Tool call arguments complete                                 |
| `response.completed`                     | Full response object (status: completed)                     |
| `response.failed`                        | Error occurred (status: failed)                              |

### Codex Compatibility

Codex CLI opens a provider with two probes before it sends any chat request.

- `GET /v1/health` answers `{"status": "ok"}` without an API key and without touching the upstream. A 404 there makes Codex mark the whole provider unreachable.
- A `POST /v1/responses` whose input carries no text, image, tool call or tool result is answered locally with an empty but well-formed Response, streaming or not. Sending that empty turn upstream cost about twelve seconds and one message of the conversation quota. A request that carries `instructions` is a real turn and still reaches M365.

Every streaming endpoint also writes a keepalive frame after ten idle seconds, because a tool-enabled turn buffers its text until the tool-call parse completes. The OpenAI-shaped routes send an SSE comment, which no client parses as data; `/v1/messages` and `/v1/complete` send the Anthropic `ping` event.

`/v1/chat/completions` and `/v1/completions` also write that comment the moment the stream opens, before the upstream turn starts. Every other streaming route already emits a frame first (`message_start`, `ping`, or `response.created`), so a client never has to tell a slow provider from a dead one.

Two more rules protect a stream whose client went away. Each frame arms a thirty-second write deadline, so a reader that stopped consuming cannot hold the handler and its upstream WebSocket open. A failed keepalive write, or a canceled request context, ends the turn and releases the upstream connection instead of writing into a closed socket.

## Responses Compact API

The `/v1/responses/compact` endpoint implements the OpenAI Responses Compact API for Codex remote compaction. It accepts the same request body as `/v1/responses` (model, input, instructions, tools, stream) and returns a compacted response containing exactly one `compaction` output item.

### How It Works

1. The conversation history (input items) is flattened into a single user message with a compaction prompt
2. The message is sent to M365 Copilot to generate a concise summary
3. The summary is returned wrapped in a `compaction` output item with `encrypted_content` field

### Example (non-streaming)

```bash
curl http://127.0.0.1:8000/v1/responses/compact \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-api-key" \
  -d '{
    "model": "gpt5.5-reasoning",
    "input": [
      {"role": "user", "content": "Fix the auth bug in sso.go"},
      {"role": "assistant", "content": "I added the missing sso_reload parameter."},
      {"role": "user", "content": "Now add logging to the refresh path"}
    ]
  }'
```

Response:

```json
{
  "id": "resp_...",
  "object": "response",
  "status": "completed",
  "output": [{
    "id": "cmp_...",
    "type": "compaction",
    "encrypted_content": "The conversation focused on fixing an SSO auth bug..."
  }]
}
```

### Streaming

Streaming mode emits the same SSE event sequence as `/v1/responses` (`response.created`, `response.in_progress`, `response.output_item.added`, `response.output_item.done`, `response.completed`, `[DONE]`), but the output item has `type: "compaction"` instead of `type: "message"`.

### Notes

- Custom `instructions` in the request body override the default compaction prompt
- The compaction request should use a new session ID (not reuse an existing conversation) for best results

## Project Structure

```
cmd/cli/main.go          # Single entry point, subcommand router
pkg/
  auth/auth.go           # TokenManager, token refresh, AES-encrypted refresh token storage
  auth/sso.go            # SSO cookie-based re-authentication (fallback for 24h token expiry)
  client/client.go       # M365Client, WebSocket (SignalR) communication
  crypto/crypto.go       # AES-256-GCM encryption for refresh tokens
  models/models.go       # Version, ModelRegistry, Config, LoadConfig, LookupModel
  payload/payload.go     # Request payload builders, URL builder, locale/timezone helpers
  servers/
    api.go               # HTTP API server, all endpoints, max_tokens, token counting, session isolation
    cli.go               # CLI server, interactive mode
  setup/wizard.go        # Browser-based setup wizard (JS snippet, token verify, data/.env save)
go.mod                   # Module: github.com/KilimcininKorOglu/M365Bridge, Go 1.22
data/                    # Runtime data (gitignored): tokens/, setup.json, cache/
```

## Dependencies

| Dependency                      | Purpose                                                               |
|---------------------------------|-----------------------------------------------------------------------|
| `github.com/google/uuid`        | UUID generation for SIDs and request IDs                              |
| `github.com/gorilla/websocket`  | WebSocket client for SignalR                                          |
| `github.com/pkoukk/tiktoken-go` | BPE token counting (o200k_base, cl100k_base fallback) for usage and max_tokens enforcement |
| `golang.org/x/net`              | publicsuffix list for SSO cookie jar                                  |

## Security

- Refresh tokens encrypted with AES-256-GCM before storage
- SSO and M365 web cookies encrypted with AES-256-GCM before storage (`data/tokens/sso_cookies.json` and `data/tokens/m365_cookies.json`)
- Legacy plaintext M365 cookie stores are encrypted automatically on first use
- Encryption key stored in `data/tokens/encryption.key`; losing it makes encrypted credentials unreadable and requires rerunning the setup wizard
- Access tokens cached in `data/tokens/token_cache.json` (disk-persisted, ~1h expiry with 60s buffer)
- Background token refresher proactively refreshes access token every 30 minutes in `serve` mode
- SSO cookie auto-renewal silently re-authenticates when refresh token expires (24h SPA limit)
- No credentials stored in code or repository
- `data/` directory is gitignored (contains tokens, cache, setup.json)
- API key authentication protects all `/v1/*` endpoints when configured
- The key is read from `Authorization: Bearer <key>` or `x-api-key: <key>`; when a client offers both, either one being valid is enough

## Image Input Support

The proxy supports multimodal image input via OpenAI and Anthropic API formats:

- **OpenAI**: `content` array with `{"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}` blocks
- **Responses**: `content` array with `{"type": "input_image", "image_url": "data:image/png;base64,..."}` blocks, where the url is a bare string
- **Anthropic**: `content` array with `{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "..."}}` blocks

An `image_url` block accepts the bare string form as well, because clients send it under both block names. A `file_id` reference is not supported; this gateway serves no Files API to resolve one against.

Images are uploaded to the M365 backend via `POST https://substrate.office.com/m365Copilot/UploadFile` and attached to the WebSocket message as `messageAnnotations`. Supported formats: PNG, JPEG, GIF, WebP.

### Remote Image URLs

An OpenAI `image_url` block may also carry a remote `https://` address instead of a data URL. The proxy downloads it before uploading.

No credential is sent on that download, so any public https host is accepted. The request is still checked to keep the proxy from being used to reach addresses inside its own network: plain http, loopback, private, link-local, multicast, carrier-grade NAT and cloud metadata targets are rejected, and the host is re-checked after DNS resolution. A response larger than 20 MB, one whose content type is not an image, or one that fails outright drops that single image rather than the whole request. At most 16 remote images are fetched per turn.

Anthropic `image` blocks carry base64 data directly and are unaffected.

`input_file`, `file`, `input_audio` and `audio` content blocks are dropped with a DEBUG log entry, because the M365 backend accepts image attachments only.

## Image Generation

The proxy exposes M365 Copilot's  image generation as OpenAI Images API endpoints:

- `POST /v1/images/generations` (JSON body): Generate images from a text prompt (no file upload)
- `POST /v1/images/edits` (multipart/form-data): Edit existing image(s) with a text prompt; supports up to 16 images via repeated `image` form fields

Both endpoints accept the following parameters:

| Parameter         | Type   | Default     | Description                                                                                       |
|-------------------|--------|-------------|---------------------------------------------------------------------------------------------------|
| `prompt`          | string | (required)  | The text prompt for image generation/editing                                                      |
| `n`               | int    | 1           | Number of images to generate (M365 generates one per request)                                     |
| `size`            | string | `1024x1024` | Image size hint (appended to prompt as natural language)                                          |
| `quality`         | string | `standard`  | Quality hint (appended to prompt; `standard` is skipped)                                          |
| `style`           | string | `natural`   | Style hint (appended to prompt; `natural` is skipped)                                             |
| `response_format` | string | `url`       | Response format: `url` returns a data URL (base64), `b64_json` returns base64 in a separate field |
| `session_id`      | string | (optional)  | Session ID for conversation continuity                                                            |

### Response Format

- `response_format=url` (default): Downloads the image server-side and returns a `data:image/png;base64,...` data URL. Falls back to the raw `designerapp.officeapps.live.com` URL if the download fails.
- `response_format=b64_json`: Downloads the image server-side using a broker token and returns the image as base64-encoded PNG data in the `b64_json` field.

### Image Host Allowlist

Generated-image URLs are read out of the model's own markdown output, which is untrusted, and the download sends the designerapp access token. The proxy therefore only contacts hosts on an allowlist, requires `https`, and rejects hosts that resolve to loopback, private, link-local, carrier-grade NAT or cloud metadata addresses. A URL that fails these checks is dropped rather than returned to the client.

| Variable | Default | Description |
|----------|---------|-------------|
| `M365_IMAGE_HOST_ALLOWLIST` | `.officeapps.live.com` | Comma-separated hosts that may serve generated images. An entry starting with a dot matches that domain and its subdomains. |

### Image Download Token Flow

When images are generated, the proxy acquires a JWE access token for `designerappservice.officeapps.live.com` via the MSAL.js broker token flow to download the image (used for both `url` and `b64_json` response formats):

1. The broker app (`c0ab8ce9`) acquires a token on behalf of the M365 web app (`4765445b`) with the `designerappservice.officeapps.live.com/.default` scope
2. A broker-compatible refresh token is stored at `data/tokens/rt_broker.txt` (encrypted), rotated automatically by the background token refresher
3. If no broker refresh token exists, one is acquired via SSO cookie broker authorize flow (PKCE + `brk-multihub://outlook.office.com` redirect URI)
4. The JWE token and `fileToken` header are used to download the image from `designerapp.officeapps.live.com`
5. The downloaded image is base64-encoded and returned in the `b64_json` field

### Example

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8230/v1",
    api_key="your-api-key",  # omit if no API key configured
)

resp = client.images.generate(
    model="gpt5.5-reasoning",
    prompt="a serene mountain landscape at sunset",
    n=1,
    response_format="b64_json",
)

# resp.data[0].b64_json contains the base64-encoded PNG
import base64
with open("output.png", "wb") as f:
    f.write(base64.b64decode(resp.data[0].b64_json))
```

## Unimplemented Features

- File upload
- Code interpreter

## Disclaimer

This project is for learning and research purposes only. It explores publicly observable network communication protocols.

By using this project, you confirm that:
- You have legitimate Microsoft 365 Copilot authorization
- It is for personal learning and research, not commercial use
- You understand the risks of using unofficial interfaces
- You accept all consequences

This project does not:
- Crack encryption or bypass authentication
- Access or leak others' data
- Interfere with Microsoft services
- Have any association with Microsoft Corporation

## License

Research Only
