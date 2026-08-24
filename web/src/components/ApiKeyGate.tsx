import { useState } from 'react'
import type { AuthMode } from '../api'
import { Formatted, useI18n } from '../i18n'
import { LanguagePicker } from './LanguagePicker'

interface Props {
  /** Decides what the screen asks for; 'none' never reaches this component. */
  mode: AuthMode
  /** Set when a stored credential was offered and refused. */
  rejected: boolean
  onSubmit: (credential: string) => void
}

export function ApiKeyGate({ mode, rejected, onSubmit }: Props) {
  const { t } = useI18n()
  const [value, setValue] = useState('')

  const password = mode === 'password'

  return (
    <div className="gate">
      <form
        className="gate-card"
        onSubmit={(event) => {
          event.preventDefault()
          if (value.trim()) onSubmit(value.trim())
        }}
      >
        <div className="gate-head">
          <h1>{password ? t('gate.passwordTitle') : t('gate.title')}</h1>
          {/* The gate replaces the whole interface, so the language has to be
              changeable from here too. */}
          <LanguagePicker />
        </div>
        <p>
          {password ? (
            <Formatted
              text={t('gate.passwordBody')}
              parts={{ variable: <code>M365_WEB_UI_PASSWORD</code> }}
            />
          ) : (
            <Formatted
              text={t('gate.body')}
              parts={{ keys: <code>M365_API_KEYS</code>, header: <code>Authorization</code> }}
            />
          )}
        </p>
        {rejected && <p className="gate-error">{t('gate.rejected')}</p>}
        <input
          type="password"
          name={password ? 'password' : 'api-key'}
          // A password manager should offer the stored password here and offer
          // nothing for an API key, which it has no business remembering.
          autoComplete={password ? 'current-password' : 'off'}
          autoFocus
          value={value}
          placeholder={password ? t('gate.passwordPlaceholder') : 'sk-...'}
          onChange={(event) => setValue(event.target.value)}
        />
        <button className="primary" type="submit">
          {password ? t('gate.enter') : t('gate.save')}
        </button>
      </form>
    </div>
  )
}
