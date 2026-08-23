import { languages, useI18n } from '../i18n'

export function LanguagePicker() {
  const { lang, setLang, t } = useI18n()

  return (
    <select
      className="language"
      value={lang}
      aria-label={t('sidebar.language')}
      title={t('sidebar.language')}
      onChange={(event) => setLang(event.target.value)}
    >
      {languages.map((entry) => (
        <option key={entry.code} value={entry.code}>
          {entry.label}
        </option>
      ))}
    </select>
  )
}
