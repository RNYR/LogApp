/**
 * HID keyboard-wedge capture for hardware barcode scanners.
 *
 * USB/Bluetooth scanners behave like keyboards: they "type" the payload and
 * (usually) finish with `Enter`. This module accumulates those keystrokes on the
 * document, resets the buffer when the inter-key gap is too long for a scanner,
 * and emits one value per scan.
 *
 * It deliberately ignores keystrokes that originate in form fields (so normal
 * typing/search still works) unless `ignoreEditableTargets` is disabled, and it
 * can be temporarily paused with `isEnabled` (e.g. while a modal is open).
 *
 * Framework-agnostic; attach it from an effect and call the returned function to
 * detach.
 */

import { normalizeScanValue } from './scanner'

export type HidTerminator = 'enter' | 'tab' | 'none'

export interface HidScannerOptions {
  /** Called once per captured scan, with the normalized payload. */
  onScan: (value: string) => void
  /** Minimum payload length after normalization. Default `3`. */
  minLength?: number
  /** Max gap between keystrokes before the buffer resets (ms). Default `120`. */
  maxInterKeyDelayMs?: number
  /** Key that ends a scan. `'none'` flushes after the inter-key gap. Default `'enter'`. */
  terminator?: HidTerminator
  /** Return `false` to temporarily ignore input. */
  isEnabled?: () => boolean
  /** Element/document to listen on. Default `document`. */
  target?: Document | HTMLElement
  /** Apply `normalizeScanValue` before `onScan`. Default `true`. */
  normalize?: boolean
  /** Ignore keystrokes typed into inputs/textareas/contenteditable. Default `true`. */
  ignoreEditableTargets?: boolean
}

export interface HidScanner {
  start(): void
  stop(): void
  readonly active: boolean
}

const EDITABLE_TAGS = new Set(['INPUT', 'TEXTAREA', 'SELECT'])
const MAX_BUFFER_LENGTH = 64

/** True for `<input>`, `<textarea>`, `<select>` and contenteditable hosts. */
export function isEditableElement(target: EventTarget | null): boolean {
  if (typeof HTMLElement === 'undefined') return false
  if (!(target instanceof HTMLElement)) return false
  if (EDITABLE_TAGS.has(target.tagName)) return true
  return target.isContentEditable
}

export function createHidScanner(options: HidScannerOptions): HidScanner {
  const {
    onScan,
    minLength = 3,
    maxInterKeyDelayMs = 120,
    terminator = 'enter',
    isEnabled,
    target = document,
    normalize = true,
    ignoreEditableTargets = true,
  } = options

  const eventTarget = target as EventTarget

  let buffer = ''
  let lastKeyAt = 0
  let gapTimer: ReturnType<typeof setTimeout> | null = null
  let active = false

  function clearGapTimer(): void {
    if (gapTimer !== null) {
      clearTimeout(gapTimer)
      gapTimer = null
    }
  }

  function reset(): void {
    buffer = ''
    lastKeyAt = 0
    clearGapTimer()
  }

  function flush(): boolean {
    const raw = buffer
    reset()
    const value = normalize ? normalizeScanValue(raw) : raw.trim()
    if (value.length < minLength) return false
    onScan(value)
    return true
  }

  function scheduleGapFlush(): void {
    if (terminator !== 'none') return
    clearGapTimer()
    gapTimer = setTimeout(() => {
      gapTimer = null
      flush()
    }, maxInterKeyDelayMs + 20)
  }

  function onKeyDown(event: KeyboardEvent): void {
    if (!active) return
    if (isEnabled && !isEnabled()) return
    if (event.ctrlKey || event.metaKey || event.altKey) return
    if (ignoreEditableTargets && isEditableElement(event.target)) return

    const now = Date.now()
    if (lastKeyAt !== 0 && now - lastKeyAt > maxInterKeyDelayMs) {
      // Too slow to be a hardware scanner; start a fresh buffer.
      buffer = ''
    }
    lastKeyAt = now

    if (event.key === 'Escape') {
      reset()
      return
    }

    const isEnter = event.key === 'Enter'
    const isTab = event.key === 'Tab'
    const isTerminator =
      terminator === 'enter' ? isEnter : terminator === 'tab' ? isTab : false

    if (isTerminator) {
      if (flush()) event.preventDefault()
      return
    }

    if (event.key.length === 1) {
      buffer += event.key
      if (buffer.length > MAX_BUFFER_LENGTH) reset()
      else scheduleGapFlush()
    }
  }

  const listener = onKeyDown as EventListener

  return {
    start() {
      if (active) return
      active = true
      reset()
      eventTarget.addEventListener('keydown', listener, true)
    },
    stop() {
      if (!active) return
      active = false
      eventTarget.removeEventListener('keydown', listener, true)
      reset()
    },
    get active() {
      return active
    },
  }
}

/**
 * Convenience wrapper: create, start and return a detach function suitable for
 * a `useEffect` cleanup.
 */
export function attachHidScanner(options: HidScannerOptions): () => void {
  const scanner = createHidScanner(options)
  scanner.start()
  return () => scanner.stop()
}
