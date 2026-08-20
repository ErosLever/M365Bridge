import { useState } from 'react'

interface Props {
  onSubmit: (key: string) => void
}

export function ApiKeyGate({ onSubmit }: Props) {
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
        <h1>API anahtarı</h1>
        <p>
          Bu gateway <code>M365_API_KEYS</code> ile yapılandırılmış. Anahtar bu tarayıcıda bir
          cookie&apos;de saklanır ve her istekte <code>Authorization</code> başlığıyla gönderilir.
        </p>
        <input
          type="password"
          autoFocus
          value={value}
          placeholder="sk-..."
          onChange={(event) => setValue(event.target.value)}
        />
        <button className="primary" type="submit">
          Kaydet
        </button>
      </form>
    </div>
  )
}
