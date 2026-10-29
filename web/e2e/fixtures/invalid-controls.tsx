import { createRoot } from 'react-dom/client'
import { Input, Select } from '../../src/components/ui'
import '../../src/index.css'

/**
 * Test-only preview that mounts the real `Input`/`Select` components in their
 * `invalid` state. No screen passes `invalid`, so this fixture is the browser
 * rendering surface for the shared danger validation styling; the Playwright
 * spec asserts the computed border/ring geometry against it.
 */
createRoot(document.getElementById('root') as HTMLElement).render(
  <main style={{ padding: 24, display: 'grid', gap: 12, maxWidth: 320 }}>
    <Input invalid defaultValue="invalid input" aria-label="invalid input" />
    <Input type="date" invalid defaultValue="2026-01-01" aria-label="invalid date" />
    <Select invalid defaultValue="value" aria-label="invalid select">
      <option value="value">value</option>
    </Select>
  </main>,
)
