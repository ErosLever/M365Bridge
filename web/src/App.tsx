import { useCallback, useEffect, useRef, useState } from 'react'
import * as api from './api'
import { ApiError } from './types'
import type { ChatMessage, ConversationRow, ModelEntry } from './types'
import { apiKeyCookie, modelCookie, readCookie, writeCookie } from './cookies'
import { ApiKeyGate } from './components/ApiKeyGate'
import { ChatPane } from './components/ChatPane'
import { Sidebar } from './components/Sidebar'

/** How many session-only rows get a title fetched from their transcript. */
const titleHydrationLimit = 30

function newSessionId(): string {
  return `ui-${crypto.randomUUID()}`
}

function firstLine(text: string): string {
  const line = text.trim().split('\n', 1)[0] ?? ''
  return line.length > 60 ? `${line.slice(0, 60)}…` : line
}

export function App() {
  const [apiKey, setApiKeyState] = useState(() => readCookie(apiKeyCookie))
  const [authRequired, setAuthRequired] = useState(false)

  const [models, setModels] = useState<ModelEntry[]>([])
  // The default is the id GET /v1/models advertises, not the registry key that
  // also resolves to it: the picker matches on the advertised id and would
  // otherwise show the stored value as one the catalog does not carry.
  const [model, setModel] = useState(() => readCookie(modelCookie) || 'gpt-5.5-reasoning')

  const [rows, setRows] = useState<ConversationRow[]>([])
  const [activeId, setActiveId] = useState('')
  const [messages, setMessages] = useState<ChatMessage[]>([])

  const [remoteListFailed, setRemoteListFailed] = useState(false)
  const [transcriptsOff, setTranscriptsOff] = useState(false)
  const [notice, setNotice] = useState('')
  const [sending, setSending] = useState(false)
  // Set when the open conversation exists upstream but has no transcript here.
  // Reading it costs a page download and a walk of a serialization this project
  // does not control, so it waits for the user to ask.
  const [importable, setImportable] = useState<{ sessionId: string; conversationId: string } | null>(null)
  const [importing, setImporting] = useState(false)

  const abortRef = useRef<AbortController | null>(null)

  api.setApiKey(apiKey)

  const report = useCallback((err: unknown) => {
    if (err instanceof ApiError) {
      if (err.status === 401) {
        setAuthRequired(true)
        setNotice('Bu gateway bir API anahtarı istiyor.')
        return
      }
      setNotice(`${err.code}: ${err.message}`)
      return
    }
    if (err instanceof DOMException && err.name === 'AbortError') return
    setNotice(String(err))
  }, [])

  /**
   * Fills in titles for rows the M365 list did not name. The gateway stores no
   * title of its own, so the first thing the user said stands in for one.
   */
  const hydrateTitles = useCallback(async (merged: ConversationRow[]) => {
    const unnamed = merged.filter((row) => !row.title && row.sessionId).slice(0, titleHydrationLimit)
    for (const row of unnamed) {
      try {
        const stored = await api.loadMessages(row.sessionId)
        const first = stored.find((m) => m.role === 'user')
        if (!first) continue
        setRows((previous) =>
          previous.map((candidate) =>
            candidate.sessionId === row.sessionId && !candidate.title
              ? { ...candidate, title: firstLine(first.content) }
              : candidate,
          ),
        )
      } catch (err) {
        if (err instanceof ApiError && err.code === 'transcripts_disabled') {
          setTranscriptsOff(true)
          return
        }
        // One unreadable transcript must not stop the rest.
      }
    }
  }, [])

  /**
   * Builds the sidebar from both sources. The M365 list carries the names, the
   * local mappings carry the session ids, and a conversation present in both is
   * one row.
   */
  const refreshRows = useCallback(async () => {
    let sessions: Awaited<ReturnType<typeof api.listSessions>> = []
    try {
      sessions = await api.listSessions()
    } catch (err) {
      report(err)
      return
    }
    const sessionByConversation = new Map(sessions.map((s) => [s.conversation_id, s]))

    let merged: ConversationRow[] = []
    try {
      const conversations = await api.listConversations()
      setRemoteListFailed(false)
      merged = conversations
        .filter((c) => !c.isMessageless)
        .map((c) => {
          const session = sessionByConversation.get(c.conversationId)
          if (session) sessionByConversation.delete(c.conversationId)
          return {
            sessionId: session?.id ?? '',
            conversationId: c.conversationId,
            title: c.chatName?.trim() || 'Adsız konuşma',
            updatedAt: c.updateTimeUtc ?? session?.updated_at ?? 0,
            remoteOnly: !session,
          }
        })
    } catch {
      // Listing M365 conversations needs browser cookies. Without them the
      // local mappings are the whole sidebar, and saying so beats an empty one.
      setRemoteListFailed(true)
    }

    for (const session of sessionByConversation.values()) {
      merged.push({
        sessionId: session.id,
        conversationId: session.conversation_id,
        title: '',
        updatedAt: session.updated_at,
        remoteOnly: false,
      })
    }

    merged.sort((a, b) => b.updatedAt - a.updatedAt)
    setRows((previous) => {
      // A conversation that has not had its first turn yet exists only here, so
      // it is carried over. Once its first turn creates the upstream
      // conversation the merged list describes the same thing, and keeping the
      // local copy too would show one conversation as two rows.
      const known = new Set(merged.map((row) => row.sessionId))
      const unsent = previous.filter((row) => !row.conversationId && !known.has(row.sessionId))
      return [...unsent, ...merged]
    })
    void hydrateTitles(merged)
  }, [hydrateTitles, report])

  useEffect(() => {
    api.listModels().then(setModels).catch(report)
  }, [report])

  useEffect(() => {
    void refreshRows()
  }, [refreshRows])

  const openRow = useCallback(
    async (row: ConversationRow) => {
      setNotice('')
      setImportable(null)
      let sessionId = row.sessionId
      if (!sessionId) {
        // The conversation exists upstream but nothing here points at it, so it
        // gets a session id before it can be continued.
        sessionId = newSessionId()
        try {
          await api.bindSession(sessionId, row.conversationId)
        } catch (err) {
          report(err)
          return
        }
        setRows((previous) =>
          previous.map((candidate) =>
            candidate.conversationId === row.conversationId
              ? { ...candidate, sessionId, remoteOnly: false }
              : candidate,
          ),
        )
      }
      setActiveId(sessionId)

      try {
        const stored = await api.loadMessages(sessionId)
        setMessages(
          stored.map((entry) => ({
            role: entry.role,
            content: entry.content,
            thinking: entry.thinking,
            model: entry.model,
            createdAt: entry.created_at,
          })),
        )
        if (stored.length === 0 && row.conversationId) {
          setImportable({ sessionId, conversationId: row.conversationId })
          setNotice(
            'Bu konuşma bu gateway dışında başlamış. Buradan devam edebilirsin, ya da geçmişini M365 sayfasından yükleyebilirsin.',
          )
        }
      } catch (err) {
        if (err instanceof ApiError && err.code === 'transcripts_disabled') {
          setTranscriptsOff(true)
          setMessages([])
          return
        }
        report(err)
      }
    },
    [report],
  )

  /** Pulls the history of the open conversation from M365 and stores it here. */
  const importHistory = useCallback(async () => {
    if (!importable || importing) return
    setImporting(true)
    setNotice('')
    try {
      const stored = await api.importHistory(importable.conversationId, importable.sessionId)
      setMessages(
        stored.map((entry) => ({
          role: entry.role,
          content: entry.content,
          thinking: entry.thinking,
          model: entry.model,
          createdAt: entry.created_at,
        })),
      )
      setImportable(null)
      void refreshRows()
    } catch (err) {
      report(err)
    } finally {
      setImporting(false)
    }
  }, [importable, importing, refreshRows, report])

  const startNew = useCallback(() => {
    const sessionId = newSessionId()
    setRows((previous) => [
      { sessionId, conversationId: '', title: 'Yeni konuşma', updatedAt: Date.now() / 1000, remoteOnly: false },
      ...previous,
    ])
    setActiveId(sessionId)
    setMessages([])
    setNotice('')
    setImportable(null)
  }, [])

  const removeRow = useCallback(
    async (row: ConversationRow) => {
      // A conversation with no upstream id was never sent, so there is nothing
      // to delete anywhere but here.
      if (row.conversationId && row.sessionId) {
        try {
          await api.deleteSession(row.sessionId, remoteListFailed)
        } catch (err) {
          report(err)
          return
        }
      } else if (row.conversationId) {
        try {
          await api.deleteConversation(row.conversationId)
        } catch (err) {
          report(err)
          return
        }
      }
      setRows((previous) => previous.filter((candidate) => candidate !== row))
      if (row.sessionId === activeId) {
        setActiveId('')
        setMessages([])
      }
      void refreshRows()
    },
    [activeId, refreshRows, remoteListFailed, report],
  )

  const renameRow = useCallback(
    async (row: ConversationRow, name: string) => {
      if (!row.conversationId) {
        setNotice('Bu konuşma henüz gönderilmedi, adlandırmak için önce bir mesaj yaz.')
        return
      }
      try {
        await api.renameConversation(row.conversationId, name)
      } catch (err) {
        report(err)
        return
      }
      setRows((previous) =>
        previous.map((candidate) => (candidate === row ? { ...candidate, title: name } : candidate)),
      )
    },
    [report],
  )

  const send = useCallback(
    async (text: string) => {
      let sessionId = activeId
      if (!sessionId) {
        sessionId = newSessionId()
        setRows((previous) => [
          { sessionId, conversationId: '', title: firstLine(text), updatedAt: Date.now() / 1000, remoteOnly: false },
          ...previous,
        ])
        setActiveId(sessionId)
      }

      setNotice('')
      setSending(true)
      setMessages((previous) => [
        ...previous,
        { role: 'user', content: text },
        { role: 'assistant', content: '', model },
      ])

      const controller = new AbortController()
      abortRef.current = controller

      try {
        for await (const delta of api.streamChat(sessionId, model, text, controller.signal)) {
          setMessages((previous) => {
            const next = [...previous]
            const last = next[next.length - 1]
            if (!last || last.role !== 'assistant') return previous
            next[next.length - 1] = {
              ...last,
              content: delta.content ? last.content + delta.content : last.content,
              thinking: delta.reasoning ? (last.thinking ?? '') + delta.reasoning : last.thinking,
            }
            return next
          })
        }
      } catch (err) {
        const message =
          err instanceof ApiError ? `${err.code}: ${err.message}` : String(err)
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          setMessages((previous) => {
            const next = [...previous]
            const last = next[next.length - 1]
            if (last && last.role === 'assistant' && !last.content) {
              next[next.length - 1] = { ...last, error: message }
              return next
            }
            return previous
          })
        }
        report(err)
      } finally {
        abortRef.current = null
        setSending(false)
        // The first turn is what creates the upstream conversation, so the
        // sidebar only learns its id once the turn is over.
        void refreshRows()
      }
    },
    [activeId, model, refreshRows, report],
  )

  const stop = useCallback(() => {
    abortRef.current?.abort()
  }, [])

  const chooseModel = useCallback((next: string) => {
    setModel(next)
    writeCookie(modelCookie, next)
  }, [])

  const saveApiKey = useCallback((key: string) => {
    setApiKeyState(key)
    api.setApiKey(key)
    writeCookie(apiKeyCookie, key)
    setAuthRequired(false)
    setNotice('')
  }, [])

  // Shown whenever the gateway refused the credential, including the case where
  // a key is already stored: a stored key that no longer works has to be
  // replaceable, not a lockout.
  if (authRequired) {
    return <ApiKeyGate onSubmit={saveApiKey} />
  }

  // A row that M365 lists but nothing here has bound carries an empty session
  // id, so an empty activeId would match the first such row and title the pane
  // after a conversation the user never opened.
  const activeRow = activeId ? rows.find((row) => row.sessionId === activeId) : undefined

  return (
    <div className="layout">
      <Sidebar
        rows={rows}
        activeId={activeId}
        remoteListFailed={remoteListFailed}
        onOpen={openRow}
        onNew={startNew}
        onDelete={removeRow}
        onRename={renameRow}
      />
      <ChatPane
        messages={messages}
        models={models}
        model={model}
        sending={sending}
        notice={notice}
        transcriptsOff={transcriptsOff}
        title={activeRow?.title ?? ''}
        canImportHistory={importable !== null}
        importing={importing}
        onImportHistory={importHistory}
        onModel={chooseModel}
        onSend={send}
        onStop={stop}
      />
    </div>
  )
}
