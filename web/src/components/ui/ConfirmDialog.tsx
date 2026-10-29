import { useEffect, useId, useRef, type ReactNode, type RefObject } from 'react'
import { Button } from './Button'

export interface ConfirmDialogProps {
  open: boolean
  /** A destructive mutation is in flight: Escape is ignored and actions are disabled. */
  busy?: boolean
  title: ReactNode
  children: ReactNode
  confirmLabel: ReactNode
  cancelLabel: ReactNode
  onConfirm: () => void
  onCancel: () => void
  /** Focus Cancel on open and style the confirmation as dangerous. */
  destructive?: boolean
  /** Focus target when the original opener is gone or disabled on close. */
  fallbackFocusRef?: RefObject<HTMLElement>
}

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

/** Focus the first enabled control inside `dialog`, or the dialog itself. */
function focusFirstEnabled(dialog: HTMLElement | null): void {
  if (!dialog) return
  const focusables = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
  if (focusables.length > 0) focusables[0].focus()
  else dialog.focus()
}

/**
 * Controlled confirmation dialog with focus containment/restoration, an
 * Escape policy that is inactive while `busy`, and duplicate-submit guarding.
 *
 * Opening/restoration is keyed only on `open`; changing `busy` or callback
 * identities never recaptures focus from a still-valid control or restores the
 * opener prematurely.
 */
export function ConfirmDialog({
  open,
  busy = false,
  title,
  children,
  confirmLabel,
  cancelLabel,
  onConfirm,
  onCancel,
  destructive = false,
  fallbackFocusRef,
}: ConfirmDialogProps) {
  const titleId = useId()
  const dialogRef = useRef<HTMLDivElement | null>(null)
  const cancelRef = useRef<HTMLButtonElement | null>(null)
  const openerRef = useRef<HTMLElement | null>(null)
  const submittedRef = useRef(false)
  // Cancels any pending post-close restoration watch (opener-removal rescue).
  const restoreRef = useRef<(() => void) | null>(null)

  // Keep the latest busy/callbacks readable from the long-lived listeners
  // without re-running the open/restore effect.
  const busyRef = useRef(busy)
  const onCancelRef = useRef(onCancel)
  const onConfirmRef = useRef(onConfirm)
  busyRef.current = busy
  onCancelRef.current = onCancel
  onConfirmRef.current = onConfirm

  useEffect(() => {
    if (!busy) submittedRef.current = false
  }, [busy])

  // Declared before the busy-rescue effect so the opener is captured from the
  // real trigger before any focus is moved into the dialog.
  useEffect(() => {
    if (!open) return

    // A new dialog lifecycle supersedes any pending post-close restoration.
    restoreRef.current?.()
    restoreRef.current = null

    submittedRef.current = false
    openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null

    if (destructive && cancelRef.current) cancelRef.current.focus()
    else dialogRef.current?.focus()

    function onFocusIn(event: FocusEvent) {
      const dialog = dialogRef.current
      if (!dialog) return
      const target = event.target
      if (target instanceof HTMLElement && dialog.contains(target)) return
      // Focus escaped (programmatic or otherwise): pull it back inside.
      focusFirstEnabled(dialog)
    }

    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') {
        event.preventDefault()
        if (!busyRef.current) onCancelRef.current()
        return
      }
      if (event.key !== 'Tab') return
      const dialog = dialogRef.current
      if (!dialog) return

      const focusables = Array.from(dialog.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
      if (focusables.length === 0) {
        event.preventDefault()
        dialog.focus()
        return
      }
      const first = focusables[0]
      const last = focusables[focusables.length - 1]
      const active = document.activeElement
      const inside = active instanceof HTMLElement && dialog.contains(active)

      if (event.shiftKey) {
        if (!inside || active === first || active === dialog) {
          event.preventDefault()
          last.focus()
        }
      } else if (!inside || active === last || active === dialog) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('focusin', onFocusIn)
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('focusin', onFocusIn)
      document.removeEventListener('keydown', onKeyDown)

      // Restore the opener if it is still usable; otherwise the fallback. The
      // opener may only be removed *after* close (an invalidated list refetch
      // dropping the archived row), so watch for its removal. Rescue is
      // restricted to genuinely lost focus: if the user has deliberately moved
      // elsewhere, the watch is cancelled instead of stealing focus back.
      const opener = openerRef.current
      // eslint-disable-next-line react-hooks/exhaustive-deps
      const fallback = fallbackFocusRef?.current ?? null
      if (opener && opener.isConnected && !opener.hasAttribute('disabled')) {
        opener.focus()
        if (fallback && fallback !== opener) {
          let cancelled = false
          let observer: MutationObserver | null = null
          function cancelRestore() {
            if (cancelled) return
            cancelled = true
            observer?.disconnect()
            document.removeEventListener('focusin', handleExternalFocus, true)
          }
          function handleExternalFocus(event: FocusEvent) {
            const target = event.target
            if (target === opener || target === document.body || target === document.documentElement) return
            cancelRestore()
          }
          observer = new MutationObserver(() => {
            if (opener.isConnected) return
            const active = document.activeElement
            cancelRestore()
            // Only rescue when the removed opener held focus (focus fell to
            // <body>); leave focus untouched if the user moved it elsewhere.
            if (!active || active === document.body || active === opener || opener.contains(active)) {
              fallback.focus()
            }
          })
          observer.observe(document.body, { childList: true, subtree: true })
          document.addEventListener('focusin', handleExternalFocus, true)
          restoreRef.current = cancelRestore
        }
      } else {
        fallback?.focus()
      }
    }
    // Only `open` should (re)capture focus. Callback/busy identity changes must
    // not reopen or prematurely restore.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  // Tie the restoration watch lifetime to the component: cancel it on unmount.
  useEffect(() => () => restoreRef.current?.(), [])

  // Pending state disables every action, which blurs the focused control to
  // <body>. Pull focus back onto the dialog whenever no enabled action holds it.
  useEffect(() => {
    if (!open) return
    const dialog = dialogRef.current
    if (!dialog) return
    const active = document.activeElement
    if (active === dialog) return
    const inside = active instanceof HTMLElement && dialog.contains(active)
    const enabled = active instanceof HTMLElement && active.matches(FOCUSABLE_SELECTOR)
    if (!inside || !enabled) focusFirstEnabled(dialog)
  }, [busy, open])

  if (!open) return null

  function handleConfirm() {
    if (busyRef.current || submittedRef.current) return
    submittedRef.current = true
    onConfirmRef.current()
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !busyRef.current) onCancelRef.current()
      }}
    >
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        tabIndex={-1}
        className="w-full max-w-md rounded-lg border border-stroke bg-panel p-6 text-content shadow-overlay focus:outline-none"
      >
        <h2 id={titleId} className="text-lg font-semibold">
          {title}
        </h2>
        <div className="mt-2 text-sm text-content-muted">{children}</div>
        <div className="mt-5 flex justify-end gap-2">
          <Button
            ref={cancelRef}
            type="button"
            variant="secondary"
            onClick={onCancelRef.current}
            disabled={busy}
          >
            {cancelLabel}
          </Button>
          <Button
            type="button"
            variant={destructive ? 'danger' : 'primary'}
            onClick={handleConfirm}
            loading={busy}
            disabled={busy}
          >
            {confirmLabel}
          </Button>
        </div>
      </div>
    </div>
  )
}
