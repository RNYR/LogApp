import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'

// Initialise i18next (side effect) before the app renders.
import './lib/i18n'
import './index.css'
import { App } from './App'

const container = document.getElementById('root')
if (!container) {
  throw new Error('Root element #root was not found')
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
