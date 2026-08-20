import { useEffect, useRef, useState } from 'react'
import type { ChatMessage, ModelEntry } from '../types'
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
  onModel,
  onSend,
  onStop,
}: Props) {
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
        <h1 className="chat-title">{title || 'Yeni konuşma'}</h1>
      </header>

      {transcriptsOff && (
        <p className="banner">
          Transcript kaydı kapalı (<code>M365_ENABLE_WEB_UI=false</code>), bu yüzden geçmiş mesajlar
          yeniden çizilemiyor.
        </p>
      )}
      {notice && <p className="banner">{notice}</p>}

      <div className="messages">
        {messages.length === 0 && !sending && (
          <p className="empty">Bir mesaj yazarak başla.</p>
        )}
        {messages.map((message, index) => (
          <MessageRow key={index} message={message} streaming={sending && index === messages.length - 1} />
        ))}
        <div ref={bottom} />
      </div>

      <div className="composer">
        <textarea
          value={draft}
          placeholder="Mesaj yaz. Enter gönderir, Shift+Enter satır ekler."
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
              Durdur
            </button>
          ) : (
            <button className="primary" onClick={submit} disabled={!draft.trim()}>
              Gönder
            </button>
          )}
        </div>
      </div>
    </main>
  )
}
