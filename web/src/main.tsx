import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { I18nProvider } from './i18n'
import './styles.css'

const container = document.getElementById('root')
if (!container) {
  throw new Error('the document has no #root element to mount into')
}

createRoot(container).render(
  <StrictMode>
    <I18nProvider>
      <App />
    </I18nProvider>
  </StrictMode>,
)
