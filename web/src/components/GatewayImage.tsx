import { useEffect, useState } from 'react'
import { fetchGeneratedImage } from '../api'
import { useI18n } from '../i18n'

/**
 * Shows an image the gateway generated.
 *
 * The route is behind the API key and an `<img>` element carries no header, so
 * the bytes are fetched with the stored credential and shown as a blob.
 *
 * Blobs are kept in a module-level map rather than per component. An answer is
 * re-rendered on every delta while it streams, and a remount would otherwise
 * download the same image again. Nothing is revoked while the page lives; the
 * map holds one entry per image in the open conversation.
 */
const blobs = new Map<string, string>()

type State = { status: 'loading' } | { status: 'ready'; url: string } | { status: 'missing' }

export function GatewayImage({ src, alt }: { src: string; alt: string }) {
  const { t } = useI18n()
  const cached = blobs.get(src)
  const [state, setState] = useState<State>(
    cached ? { status: 'ready', url: cached } : { status: 'loading' },
  )

  useEffect(() => {
    const ready = blobs.get(src)
    if (ready) {
      setState({ status: 'ready', url: ready })
      return
    }

    let live = true
    fetchGeneratedImage(src)
      .then((blob) => {
        // The blob stays in the map even when this component is already gone,
        // because the map is the cache the next mount reads. Revoking it here
        // would leave the map pointing at an address no longer readable.
        const url = URL.createObjectURL(blob)
        blobs.set(src, url)
        if (live) setState({ status: 'ready', url })
      })
      .catch(() => {
        // A reference is lost when the gateway restarts, and the address behind
        // it expires on its own. Both read as a missing image rather than an
        // error, because neither is something the reader can act on.
        if (live) setState({ status: 'missing' })
      })

    return () => {
      live = false
    }
  }, [src])

  if (state.status === 'ready') {
    return <img className="generated-image" src={state.url} alt={alt} />
  }
  return (
    <span className="generated-image-note">
      {state.status === 'loading' ? t('image.loading') : t('image.missing')}
    </span>
  )
}
