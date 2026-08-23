import type { ConversationRow } from '../types'
import { useI18n } from '../i18n'
import { LanguagePicker } from './LanguagePicker'

interface Props {
  rows: ConversationRow[]
  activeId: string
  remoteListFailed: boolean
  onOpen: (row: ConversationRow) => void
  onNew: () => void
  onDelete: (row: ConversationRow) => void
  onRename: (row: ConversationRow, name: string) => void
}

export function Sidebar({ rows, activeId, remoteListFailed, onOpen, onNew, onDelete, onRename }: Props) {
  const { t } = useI18n()

  return (
    <aside className="sidebar">
      <div className="sidebar-head">
        <div className="sidebar-top">
          <span className="brand">M365Bridge</span>
          <LanguagePicker />
        </div>
        <button className="primary" onClick={onNew}>
          {t('sidebar.newConversation')}
        </button>
      </div>

      {remoteListFailed && <p className="sidebar-note">{t('sidebar.remoteListFailed')}</p>}

      <ul className="conversations">
        {rows.map((row) => {
          const title = row.title || t('conversation.untitled')
          return (
            <li
              key={row.sessionId || row.conversationId}
              className={row.sessionId && row.sessionId === activeId ? 'row active' : 'row'}
            >
              <button className="row-open" onClick={() => onOpen(row)} title={title}>
                <span className="row-title">{title}</span>
                {row.remoteOnly && <span className="row-badge">M365</span>}
              </button>
              <span className="row-actions">
                <button
                  className="icon"
                  title={t('sidebar.rename')}
                  onClick={() => {
                    const name = window.prompt(t('sidebar.renamePrompt'), row.title)
                    if (name && name.trim()) onRename(row, name.trim())
                  }}
                >
                  ✎
                </button>
                <button
                  className="icon danger"
                  title={t('sidebar.delete')}
                  onClick={() => {
                    if (window.confirm(t('sidebar.deleteConfirm', { title }))) onDelete(row)
                  }}
                >
                  ×
                </button>
              </span>
            </li>
          )
        })}
        {rows.length === 0 && <li className="empty">{t('sidebar.empty')}</li>}
      </ul>
    </aside>
  )
}
