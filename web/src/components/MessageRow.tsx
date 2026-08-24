import { useState } from 'react'
import type { ChatMessage } from '../types'
import { useI18n } from '../i18n'
import { Markdown } from './Markdown'

interface Props {
  message: ChatMessage
  streaming: boolean
}

export function MessageRow({ message, streaming }: Props) {
  const { t } = useI18n()
  const [showThinking, setShowThinking] = useState(false)

  if (message.error) {
    return (
      <div className="message error">
        <span className="who">{t('message.error')}</span>
        <div className="body">{message.error}</div>
      </div>
    )
  }

  return (
    <div className={`message ${message.role}`}>
      <span className="who">
        {message.role === 'user' ? t('message.you') : message.model || t('message.assistant')}
      </span>
      {message.thinking && (
        <div className="thinking">
          <button className="thinking-toggle" onClick={() => setShowThinking((open) => !open)}>
            {showThinking ? t('message.hideThinking') : t('message.showThinking')}
          </button>
          {/* The backend writes the reasoning in markdown too, so a step title
              arrives as **bold** and reads as asterisks when shown raw. */}
          {showThinking && (
            <div className="thinking-body">
              <Markdown text={message.thinking} />
            </div>
          )}
        </div>
      )}
      {/* Only an answer is markdown. What the user typed is shown exactly as
          they typed it, because an asterisk in a question is an asterisk. */}
      <div className="body">
        {message.role === 'assistant' ? <Markdown text={message.content} /> : message.content}
        {streaming && !message.content && <span className="cursor">▍</span>}
      </div>
    </div>
  )
}
