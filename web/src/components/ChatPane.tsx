import { useEffect, useRef, useState } from 'react'
import type { ChatMessage, ModelEntry } from '../types'
import { Formatted, useI18n } from '../i18n'
import { ModelPicker } from './ModelPicker'
import { MessageRow } from './MessageRow'

interface Props {
  messages: ChatMessage[]
  models: ModelEntry[]
  model: string
  sending: boolean
  notice: string
  transcriptsOff: boolean
  title: string
  canImportHistory: boolean
  importing: boolean
  onImportHistory: () => void
  onModel: (model: string) => void
  onSend: (text: string) => void
  onStop: () => void
}

export function ChatPane({
  messages,
  models,
  model,
  sending,
  notice,
  transcriptsOff,
  title,
  canImportHistory,
  importing,
  onImportHistory,
  onModel,
  onSend,
  onStop,
}: Props) {
  const { t } = useI18n()
  const [draft, setDraft] = useState('')
  const bottom = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    bottom.current?.scrollIntoView({ block: 'end' })
  }, [messages])

  function submit() {
    const text = draft.trim()
    if (!text || sending) return
    setDraft('')
    onSend(text)
  }

  return (
    <main className="chat">
      <header className="chat-head">
        <h1 className="chat-title">{title || t('conversation.new')}</h1>
      </header>

      {transcriptsOff && (
        <p className="banner">
          <Formatted
            text={t('chat.transcriptsOff')}
            parts={{ code: <code>M365_ENABLE_WEB_UI=false</code> }}
          />
        </p>
      )}
      {notice && (
        <p className="banner">
          {notice}
          {canImportHistory && (
            <button className="banner-action" onClick={onImportHistory} disabled={importing}>
              {importing ? t('chat.importing') : t('chat.importHistory')}
            </button>
          )}
        </p>
      )}

      <div className="messages">
        {messages.length === 0 && !sending && <p className="empty">{t('chat.empty')}</p>}
        {messages.map((message, index) => (
          <MessageRow key={index} message={message} streaming={sending && index === messages.length - 1} />
        ))}
        <div ref={bottom} />
      </div>

      <div className="composer">
        <textarea
          value={draft}
          placeholder={t('chat.placeholder')}
          rows={3}
          onChange={(event) => setDraft(event.target.value)}
          onKeyDown={(event) => {
            if (event.key === 'Enter' && !event.shiftKey) {
              event.preventDefault()
              submit()
            }
          }}
        />
        <div className="composer-controls">
          <ModelPicker models={models} value={model} onChange={onModel} />
          {sending ? (
            <button className="danger" onClick={onStop}>
              {t('chat.stop')}
            </button>
          ) : (
            <button className="primary" onClick={submit} disabled={!draft.trim()}>
              {t('chat.send')}
            </button>
          )}
        </div>
      </div>
    </main>
  )
}
