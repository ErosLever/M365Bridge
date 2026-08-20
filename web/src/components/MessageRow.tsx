import { useState } from 'react'
import type { ChatMessage } from '../types'

interface Props {
  message: ChatMessage
  streaming: boolean
}

export function MessageRow({ message, streaming }: Props) {
  const [showThinking, setShowThinking] = useState(false)

  if (message.error) {
    return (
      <div className="message error">
        <span className="who">hata</span>
        <div className="body">{message.error}</div>
      </div>
    )
  }

  return (
    <div className={`message ${message.role}`}>
      <span className="who">{message.role === 'user' ? 'sen' : message.model || 'asistan'}</span>
      {message.thinking && (
        <div className="thinking">
          <button className="thinking-toggle" onClick={() => setShowThinking((open) => !open)}>
            {showThinking ? 'Düşünceyi gizle' : 'Düşünceyi göster'}
          </button>
          {showThinking && <pre className="thinking-body">{message.thinking}</pre>}
        </div>
      )}
      <div className="body">
        {message.content}
        {streaming && !message.content && <span className="cursor">▍</span>}
      </div>
    </div>
  )
}
