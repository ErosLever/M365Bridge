import type { ConversationRow } from '../types'
import { useI18n } from '../i18n'
import { confirmDialog, promptDialog } from '../dialogs'
import { LanguagePicker } from './LanguagePicker'

interface Props {
  rows: ConversationRow[]
  activeId: string
  remoteListFailed: boolean
  /** Null until the account's setting has been read, or when reading it failed. */
  memory: MemoryState | null
  onOpen: (row: ConversationRow) => void
  onNew: () => void
  onDelete: (row: ConversationRow) => void
  onRename: (row: ConversationRow, name: string) => void
  onMemoryChange: (enabled: boolean) => void
}

/** The account's Copilot memory setting as the sidebar needs it. */
export interface MemoryState {
  enabled: boolean
  allowedByTenant: boolean
  /** True while a change is in flight, so the control cannot be moved twice. */
  saving: boolean
}

export function Sidebar({
  rows,
  activeId,
  remoteListFailed,
  memory,
  onOpen,
  onNew,
  onDelete,
  onRename,
  onMemoryChange,
}: Props) {
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
                  onClick={async () => {
                    const name = await promptDialog({
                      title: t('sidebar.rename'),
                      value: row.title,
                      confirmText: t('dialog.save'),
                      cancelText: t('dialog.cancel'),
                      emptyText: t('dialog.nameRequired'),
                    })
                    if (name) onRename(row, name)
                  }}
                >
                  ✎
                </button>
                <button
                  className="icon danger"
                  title={t('sidebar.delete')}
                  onClick={async () => {
                    const confirmed = await confirmDialog({
                      title: t('sidebar.delete'),
                      text: t('sidebar.deleteConfirm', { title }),
                      confirmText: t('dialog.delete'),
                      cancelText: t('dialog.cancel'),
                      danger: true,
                    })
                    if (confirmed) onDelete(row)
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

      {/* The account's Copilot memory reaches every turn this gateway serves,
          whatever conversation it belongs to. It is the operator's own M365
          setting, so it is shown and changed here rather than turned off on
          their behalf. The block stays hidden when the setting could not be
          read, because a control that cannot report the truth is worse than
          none. */}
      {memory && (
        <div className="sidebar-foot">
          <label className="settings-row">
            <input
              type="checkbox"
              checked={memory.enabled}
              disabled={memory.saving || !memory.allowedByTenant}
              onChange={(event) => onMemoryChange(event.target.checked)}
            />
            <span>{t('settings.memory')}</span>
          </label>
          <p className="sidebar-note">
            {memory.allowedByTenant ? t('settings.memoryHint') : t('settings.memoryTenantOff')}
          </p>
        </div>
      )}
    </aside>
  )
}
