import { useState } from 'react'
import { Formatted, useI18n } from '../i18n'
import { LanguagePicker } from './LanguagePicker'

interface Props {
  onSubmit: (key: string) => void
}

export function ApiKeyGate({ onSubmit }: Props) {
  const { t } = useI18n()
  const [value, setValue] = useState('')

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
          <h1>{t('gate.title')}</h1>
          {/* The gate replaces the whole interface, so the language has to be
              changeable from here too. */}
          <LanguagePicker />
        </div>
        <p>
          <Formatted
            text={t('gate.body')}
            parts={{ keys: <code>M365_API_KEYS</code>, header: <code>Authorization</code> }}
          />
        </p>
        <input
          type="password"
          autoFocus
          value={value}
          placeholder="sk-..."
          onChange={(event) => setValue(event.target.value)}
        />
        <button className="primary" type="submit">
          {t('gate.save')}
        </button>
      </form>
    </div>
  )
}
