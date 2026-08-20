import type { ConversationRow } from '../types'

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
  return (
    <aside className="sidebar">
      <div className="sidebar-head">
        <span className="brand">M365Bridge</span>
        <button className="primary" onClick={onNew}>
          Yeni konuşma
        </button>
      </div>

      {remoteListFailed && (
        <p className="sidebar-note">
          M365 konuşma listesi okunamadı, bu yüzden yalnızca bu gateway üzerinden açılan konuşmalar
          görünüyor. Listenin tamamı için M365 web cookie&apos;leri gerekiyor.
        </p>
      )}

      <ul className="conversations">
        {rows.map((row) => (
          <li
            key={row.sessionId || row.conversationId}
            className={row.sessionId && row.sessionId === activeId ? 'row active' : 'row'}
          >
            <button className="row-open" onClick={() => onOpen(row)} title={row.title || 'Başlıksız'}>
              <span className="row-title">{row.title || 'Başlıksız'}</span>
              {row.remoteOnly && <span className="row-badge">M365</span>}
            </button>
            <span className="row-actions">
              <button
                className="icon"
                title="Yeniden adlandır"
                onClick={() => {
                  const name = window.prompt('Yeni ad', row.title)
                  if (name && name.trim()) onRename(row, name.trim())
                }}
              >
                ✎
              </button>
              <button
                className="icon danger"
                title="Sil"
                onClick={() => {
                  if (window.confirm(`"${row.title || 'Başlıksız'}" silinsin mi?`)) onDelete(row)
                }}
              >
                ×
              </button>
            </span>
          </li>
        ))}
        {rows.length === 0 && <li className="empty">Henüz konuşma yok.</li>}
      </ul>
    </aside>
  )
}
