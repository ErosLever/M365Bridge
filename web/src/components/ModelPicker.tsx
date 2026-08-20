import type { ModelEntry } from '../types'

interface Props {
  models: ModelEntry[]
  value: string
  onChange: (model: string) => void
}

export function ModelPicker({ models, value, onChange }: Props) {
  // The gateway answers a model it does not serve with 404 model_not_found, so
  // a value carried over from an older catalog is kept in the list rather than
  // silently replaced.
  const known = models.some((entry) => entry.id === value)

  return (
    <select className="model" value={value} onChange={(event) => onChange(event.target.value)}>
      {!known && <option value={value}>{value} (listede yok)</option>}
      {models.map((entry) => (
        <option key={entry.id} value={entry.id}>
          {entry.display_name || entry.id}
          {entry.capabilities?.thinking?.supported ? ' · düşünür' : ''}
        </option>
      ))}
    </select>
  )
}
