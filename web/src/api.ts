import { ApiError } from './types'
import type { M365Conversation, ModelEntry, SessionRecord } from './types'

// The gateway authenticates on a header, never on a cookie, so a cross-site
// form cannot carry the credential and no CSRF token is needed.
let apiKey = ''

export function setApiKey(key: string): void {
  apiKey = key
}

function headers(extra: Record<string, string> = {}): Record<string, string> {
  const result: Record<string, string> = { ...extra }
  if (apiKey) {
    result['Authorization'] = `Bearer ${apiKey}`
  }
  return result
}

/** Turns a failed response into an ApiError carrying the gateway's own code. */
async function toError(res: Response): Promise<ApiError> {
  let code = 'http_error'
  let message = `HTTP ${res.status}`
  try {
    const body = await res.json()
    if (body?.error) {
      code = body.error.code ?? code
      message = body.error.message ?? message
    }
  } catch {
    // A body that is not the documented error shape leaves the status as the
    // only thing worth reporting.
  }
  return new ApiError(res.status, code, message)
}

async function getJSON<T>(path: string): Promise<T> {
  const res = await fetch(path, { headers: headers() })
  if (!res.ok) throw await toError(res)
  return (await res.json()) as T
}

export async function listModels(): Promise<ModelEntry[]> {
  const body = await getJSON<{ data?: ModelEntry[] }>('/v1/models')
  return body.data ?? []
}

export async function listConversations(): Promise<M365Conversation[]> {
  const body = await getJSON<{ conversations?: M365Conversation[] }>('/v1/conversations')
  return body.conversations ?? []
}

export async function listSessions(): Promise<SessionRecord[]> {
  const body = await getJSON<{ data?: SessionRecord[] }>('/v1/sessions')
  return body.data ?? []
}

export interface StoredMessage {
  role: 'user' | 'assistant'
  content: string
  thinking?: string
  model?: string
  created_at: number
}

export async function loadMessages(sessionId: string): Promise<StoredMessage[]> {
  const body = await getJSON<{ data?: StoredMessage[] }>(
    `/v1/sessions/${encodeURIComponent(sessionId)}/messages`,
  )
  return body.data ?? []
}

/** Points a session id at a conversation that already exists upstream. */
export async function bindSession(sessionId: string, conversationId: string): Promise<void> {
  const res = await fetch(`/v1/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'PUT',
    headers: headers({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ conversation_id: conversationId }),
  })
  if (!res.ok) throw await toError(res)
}

/**
 * Deletes the upstream conversation and clears the mapping. localOnly skips the
 * upstream delete, which a deployment without M365 web cookies needs because
 * the upstream delete can never succeed there.
 */
export async function deleteSession(sessionId: string, localOnly: boolean): Promise<void> {
  const query = localOnly ? '?local_only=true' : ''
  const res = await fetch(`/v1/sessions/${encodeURIComponent(sessionId)}${query}`, {
    method: 'DELETE',
    headers: headers(),
  })
  if (!res.ok) throw await toError(res)
}

export async function deleteConversation(conversationId: string): Promise<void> {
  const res = await fetch(`/v1/conversations/${encodeURIComponent(conversationId)}`, {
    method: 'DELETE',
    headers: headers(),
  })
  if (!res.ok) throw await toError(res)
}

export async function renameConversation(conversationId: string, name: string): Promise<void> {
  const res = await fetch(`/v1/conversations/${encodeURIComponent(conversationId)}`, {
    method: 'PATCH',
    headers: headers({ 'Content-Type': 'application/json' }),
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw await toError(res)
}

export interface StreamDelta {
  content?: string
  reasoning?: string
}

/**
 * Streams one turn.
 *
 * The gateway answers with server-sent events, and EventSource cannot POST, so
 * the body is read and framed here. Keepalive comments carry no `data:` line
 * and fall through the parser untouched.
 */
export async function* streamChat(
  sessionId: string,
  model: string,
  content: string,
  signal: AbortSignal,
): AsyncGenerator<StreamDelta> {
  const res = await fetch('/v1/chat/completions', {
    method: 'POST',
    signal,
    headers: headers({
      'Content-Type': 'application/json',
      'X-Session-Id': sessionId,
    }),
    body: JSON.stringify({
      model,
      stream: true,
      messages: [{ role: 'user', content }],
    }),
  })
  if (!res.ok) throw await toError(res)
  if (!res.body) throw new ApiError(res.status, 'no_body', 'The response carried no body')

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''

  while (true) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let boundary = buffer.indexOf('\n\n')
    while (boundary >= 0) {
      const frame = buffer.slice(0, boundary)
      buffer = buffer.slice(boundary + 2)
      const delta = parseFrame(frame)
      if (delta === 'done') return
      if (delta) yield delta
      boundary = buffer.indexOf('\n\n')
    }
  }
}

/** Parses one SSE frame into a delta, 'done' for the terminator, or null. */
function parseFrame(frame: string): StreamDelta | 'done' | null {
  for (const line of frame.split('\n')) {
    if (!line.startsWith('data:')) continue
    const payload = line.slice('data:'.length).trim()
    if (payload === '[DONE]') return 'done'
    if (!payload) continue

    let chunk: {
      error?: { code?: string; message?: string }
      choices?: { delta?: { content?: string; reasoning_content?: string } }[]
    }
    try {
      chunk = JSON.parse(payload)
    } catch {
      // A frame the parser cannot read is dropped rather than shown as text,
      // because rendering it would put transport noise into the answer.
      continue
    }

    // A stream that fails mid-turn reports it in the frame itself; the HTTP
    // status was already sent and cannot be changed.
    if (chunk.error) {
      throw new ApiError(502, chunk.error.code ?? 'stream_error', chunk.error.message ?? 'The stream failed')
    }

    const delta = chunk.choices?.[0]?.delta
    if (!delta) continue
    if (delta.content) return { content: delta.content }
    if (delta.reasoning_content) return { reasoning: delta.reasoning_content }
  }
  return null
}
