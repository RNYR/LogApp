import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  createFeedback,
  describeScannerSupport,
  hasMediaDevices,
  isBarcodeDetectorSupported,
  isCameraScanSupported,
  isSecureContextAvailable,
  normalizeScanValue,
} from './scanner'
import { attachHidScanner, createHidScanner, isEditableElement } from './hid'

interface FakeTargetHandle {
  target: Document
  fire: (event: KeyboardEvent) => void
  hasKeydownListener: () => boolean
}

function makeFakeTarget(): FakeTargetHandle {
  const listeners = new Map<string, EventListener>()
  const target = {
    addEventListener(type: string, listener: EventListener): void {
      listeners.set(type, listener)
    },
    removeEventListener(type: string): void {
      listeners.delete(type)
    },
  } as unknown as Document
  return {
    target,
    fire: (event) => listeners.get('keydown')?.(event),
    hasKeydownListener: () => listeners.has('keydown'),
  }
}

interface KeyHandle {
  event: KeyboardEvent
  wasPrevented: () => boolean
}

function makeKey(key: string, overrides: Partial<KeyboardEvent> = {}): KeyHandle {
  let prevented = false
  const base = {
    key,
    ctrlKey: false,
    metaKey: false,
    altKey: false,
    target: null,
    preventDefault: (): void => {
      prevented = true
    },
  }
  return {
    event: { ...base, ...overrides } as unknown as KeyboardEvent,
    wasPrevented: () => prevented,
  }
}

const delay = (ms: number): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms))

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('normalizeScanValue', () => {
  it('trims, collapses internal whitespace and uppercases', () => {
    expect(normalizeScanValue('  pv-001 \n')).toBe('PV-001')
    expect(normalizeScanValue('pv   001')).toBe('PV 001')
    expect(normalizeScanValue('PV-999')).toBe('PV-999')
    expect(normalizeScanValue(' pv-1000 ')).toBe('PV-1000')
    expect(normalizeScanValue('')).toBe('')
  })
})

describe('capability probes', () => {
  it('reports nothing supported without a browser environment', () => {
    vi.stubGlobal('window', undefined)
    vi.stubGlobal('navigator', undefined)

    expect(isSecureContextAvailable()).toBe(false)
    expect(hasMediaDevices()).toBe(false)
    expect(isBarcodeDetectorSupported()).toBe(false)
    expect(isCameraScanSupported()).toBe(false)
    expect(describeScannerSupport()).toEqual({
      secureContext: false,
      mediaDevices: false,
      barcodeDetector: false,
    })
  })

  it('reports support when the browser exposes the APIs', () => {
    vi.stubGlobal('window', { isSecureContext: true, BarcodeDetector: class {} })
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: () => Promise.resolve({}) } })

    expect(isSecureContextAvailable()).toBe(true)
    expect(hasMediaDevices()).toBe(true)
    expect(isBarcodeDetectorSupported()).toBe(true)
    expect(isCameraScanSupported()).toBe(true)
    expect(describeScannerSupport()).toEqual({
      secureContext: true,
      mediaDevices: true,
      barcodeDetector: true,
    })
  })

  it('requires a secure context for camera scanning', () => {
    vi.stubGlobal('window', { isSecureContext: false })
    vi.stubGlobal('navigator', { mediaDevices: { getUserMedia: () => Promise.resolve({}) } })

    expect(hasMediaDevices()).toBe(true)
    expect(isCameraScanSupported()).toBe(false)
  })
})

describe('createFeedback', () => {
  it('is a no-op when disabled and never throws', () => {
    const feedback = createFeedback(false)
    expect(feedback.enabled).toBe(false)
    expect(() => feedback.success()).not.toThrow()
    expect(() => feedback.dispose()).not.toThrow()
  })

  it('supports toggling enabled', () => {
    const feedback = createFeedback()
    expect(feedback.enabled).toBe(true)
    feedback.enabled = false
    expect(feedback.enabled).toBe(false)
  })

  it('stays best-effort without WebAudio or vibration', () => {
    vi.stubGlobal('window', {})
    vi.stubGlobal('navigator', {})
    const feedback = createFeedback(true)
    expect(() => feedback.success()).not.toThrow()
  })
})

describe('isEditableElement', () => {
  it('returns false without a DOM or for non-elements', () => {
    vi.stubGlobal('HTMLElement', undefined)
    expect(isEditableElement(null)).toBe(false)
    expect(isEditableElement({} as unknown as EventTarget)).toBe(false)
  })

  it('recognises form fields and contenteditable hosts', () => {
    class FakeElement {
      constructor(
        public tagName: string,
        public isContentEditable = false,
      ) {}
    }
    vi.stubGlobal('HTMLElement', FakeElement)

    expect(isEditableElement(new FakeElement('INPUT') as unknown as EventTarget)).toBe(true)
    expect(isEditableElement(new FakeElement('TEXTAREA') as unknown as EventTarget)).toBe(true)
    expect(isEditableElement(new FakeElement('SELECT') as unknown as EventTarget)).toBe(true)
    expect(isEditableElement(new FakeElement('DIV') as unknown as EventTarget)).toBe(false)
    expect(isEditableElement(new FakeElement('DIV', true) as unknown as EventTarget)).toBe(true)
  })
})

describe('createHidScanner', () => {
  it('captures a fast scan terminated by Enter and normalizes it', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({ onScan: (value) => scans.push(value), target: handle.target })
    scanner.start()
    expect(scanner.active).toBe(true)

    for (const character of 'pv-001') handle.fire(makeKey(character).event)
    const enter = makeKey('Enter')
    handle.fire(enter.event)

    expect(scans).toEqual(['PV-001'])
    expect(enter.wasPrevented()).toBe(true)
    scanner.stop()
  })

  it('ignores buffers shorter than minLength without preventing default', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({ onScan: (value) => scans.push(value), target: handle.target })
    scanner.start()

    handle.fire(makeKey('A').event)
    const enter = makeKey('Enter')
    handle.fire(enter.event)

    expect(scans).toEqual([])
    expect(enter.wasPrevented()).toBe(false)
    scanner.stop()
  })

  it('resets the buffer after a slow inter-key gap', async () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({
      onScan: (value) => scans.push(value),
      target: handle.target,
      maxInterKeyDelayMs: 40,
    })
    scanner.start()

    for (const character of 'ABC') handle.fire(makeKey(character).event)
    await delay(80)
    for (const character of 'XYZ') handle.fire(makeKey(character).event)
    handle.fire(makeKey('Enter').event)

    expect(scans).toEqual(['XYZ'])
    scanner.stop()
  })

  it('supports the Tab terminator', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({
      onScan: (value) => scans.push(value),
      target: handle.target,
      terminator: 'tab',
    })
    scanner.start()

    for (const character of 'pv-002') handle.fire(makeKey(character).event)
    const tab = makeKey('Tab')
    handle.fire(tab.event)

    expect(scans).toEqual(['PV-002'])
    expect(tab.wasPrevented()).toBe(true)
    scanner.stop()
  })

  it('honours the isEnabled gate', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    let enabled = false
    const scanner = createHidScanner({
      onScan: (value) => scans.push(value),
      target: handle.target,
      isEnabled: () => enabled,
    })
    scanner.start()

    for (const character of 'pv-003') handle.fire(makeKey(character).event)
    handle.fire(makeKey('Enter').event)
    expect(scans).toEqual([])

    enabled = true
    for (const character of 'pv-004') handle.fire(makeKey(character).event)
    handle.fire(makeKey('Enter').event)
    expect(scans).toEqual(['PV-004'])
    scanner.stop()
  })

  it('resets the buffer on Escape', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({ onScan: (value) => scans.push(value), target: handle.target })
    scanner.start()

    for (const character of 'AB') handle.fire(makeKey(character).event)
    handle.fire(makeKey('Escape').event)
    for (const character of 'CDE') handle.fire(makeKey(character).event)
    handle.fire(makeKey('Enter').event)

    expect(scans).toEqual(['CDE'])
    scanner.stop()
  })

  it('detaches the listener on stop and via attachHidScanner cleanup', () => {
    const handle = makeFakeTarget()
    const scanner = createHidScanner({ onScan: () => undefined, target: handle.target })
    scanner.start()
    expect(handle.hasKeydownListener()).toBe(true)
    scanner.stop()
    expect(scanner.active).toBe(false)
    expect(handle.hasKeydownListener()).toBe(false)

    const attached = makeFakeTarget()
    const detach = attachHidScanner({ onScan: () => undefined, target: attached.target })
    expect(attached.hasKeydownListener()).toBe(true)
    detach()
    expect(attached.hasKeydownListener()).toBe(false)
  })

  it('caps runaway buffers instead of emitting them whole', () => {
    const handle = makeFakeTarget()
    const scans: string[] = []
    const scanner = createHidScanner({ onScan: (value) => scans.push(value), target: handle.target })
    scanner.start()

    for (let index = 0; index < 70; index += 1) handle.fire(makeKey('A').event)
    handle.fire(makeKey('Enter').event)

    expect(scans.every((scan) => scan.length <= 64)).toBe(true)
    scanner.stop()
  })
})
