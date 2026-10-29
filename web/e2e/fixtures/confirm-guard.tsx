import { createRoot } from 'react-dom/client'
import { ConfirmDialog } from '../../src/components/ui'
import '../../src/index.css'

/**
 * Test-only preview that mounts the real `ConfirmDialog` open with `busy={false}`
 * and counts `onConfirm` invocations on `window`.
 *
 * The Playwright spec activates the same enabled Confirm button twice inside one
 * synchronous `evaluate` call. Because `busy` stays false, React never disables
 * the button, so a second request could only be prevented by the component's
 * `submittedRef` synchronous guard (not by disabled-button or `busyRef`
 * suppression).
 */
declare global {
  interface Window {
    __confirmGuardCalls?: number
  }
}

window.__confirmGuardCalls = 0

createRoot(document.getElementById('root') as HTMLElement).render(
  <ConfirmDialog
    open
    busy={false}
    title="Confirm guard"
    confirmLabel="Confirm"
    cancelLabel="Cancel"
    onConfirm={() => {
      window.__confirmGuardCalls = (window.__confirmGuardCalls ?? 0) + 1
    }}
    onCancel={() => {}}
  >
    Guard body
  </ConfirmDialog>,
)
