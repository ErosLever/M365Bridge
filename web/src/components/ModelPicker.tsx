import type { ModelEntry } from '../types'
import { useI18n } from '../i18n'

interface Props {
  models: ModelEntry[]
  value: string
  onChange: (model: string) => void
}

export function ModelPicker({ models, value, onChange }: Props) {
  const { t } = useI18n()

  // The gateway answers a model it does not serve with 404 model_not_found, so
  // a value carried over from an older catalog is kept in the list rather than
  // silently replaced.
  const known = models.some((entry) => entry.id === value)

  return (
    <select className="model" value={value} onChange={(event) => onChange(event.target.value)}>
      {!known && <option value={value}>{t('model.unknown', { model: value })}</option>}
      {models.map((entry) => (
        <option key={entry.id} value={entry.id}>
          {entry.display_name || entry.id}
          {entry.capabilities?.thinking?.supported ? t('model.thinks') : ''}
        </option>
      ))}
    </select>
  )
}
