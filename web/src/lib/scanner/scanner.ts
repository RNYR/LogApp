/**
 * Camera CODE128 scanner for Sklad.
 *
 * Decoding strategy (see `docs/contracts/barcode.md`):
 *   1. **Native `BarcodeDetector`** fast path (`formats: ['code_128']`) on
 *      Chromium/Android/desktop where it is available and supports CODE128.
 *   2. **ZXing fallback** (`@zxing/library`) for iOS Safari and any browser
 *      without a usable `BarcodeDetector`. It is loaded lazily so the ZXing
 *      bundle is only fetched when actually needed.
 *
 * Both paths share one `requestAnimationFrame` loop, de-duplicate repeated reads
 * of the same label, and emit audio + haptic feedback on a successful decode.
 *
 * The module is framework-agnostic and safe to import outside a browser (no DOM
 * access happens until `createScanner` / `start`).
 */

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

export type ScannerStatus = 'idle' | 'starting' | 'scanning' | 'stopped' | 'error'

export type ScannerErrorCode =
  | 'insecure_context'
  | 'permission_denied'
  | 'no_camera'
  | 'camera_in_use'
  | 'unsupported'
  | 'unknown'

export interface ScannerError {
  code: ScannerErrorCode
  message: string
  cause?: unknown
}

export type ScanSource = 'barcode-detector' | 'zxing'

export interface ScanResult {
  /** Normalized payload (see `normalizeScanValue`). */
  value: string
  /** Barcode symbology, always `code_128` for this scanner. */
  format: string
  /** Which decoder produced the result. */
  source: ScanSource
  /** `Date.now()` timestamp of the decode. */
  at: number
}

export interface ScannerOptions {
  /** `<video>` element the camera stream is attached to. */
  video: HTMLVideoElement
  /** Called once per newly decoded label (de-duplicated). */
  onResult: (result: ScanResult) => void
  onError?: (error: ScannerError) => void
  onStatus?: (status: ScannerStatus) => void
  /** Formats passed to `BarcodeDetector`. Default `['code_128']`. */
  formats?: string[]
  /** Preferred camera. Default `'environment'` (rear camera on phones). */
  facingMode?: 'environment' | 'user'
  /** Ignore the same value for this long after a decode (ms). Default 1500. */
  dedupeWindowMs?: number
  /** Play a beep + vibration on a successful decode. Default `true`. */
  feedback?: boolean
  /** Turn the torch on when scanning starts (if supported). Default `false`. */
  torch?: boolean
}

export interface Scanner {
  /** Acquire the camera and begin continuous decoding. */
  start(): Promise<void>
  /** Stop the camera, cancel the decode loop and release resources. */
  stop(): void
  /** Toggle the torch; resolves `true` when the change was applied. */
  toggleTorch(on?: boolean): Promise<boolean>
  readonly torchSupported: boolean
  readonly status: ScannerStatus
  /** `true` when decoding is running through the ZXing fallback. */
  readonly usingFallback: boolean
  /** Optical/digital zoom range of the active camera, or `null` if unsupported. */
  readonly zoomRange: ZoomRange | null
  /** Apply a zoom level within `zoomRange`; resolves `true` when applied. */
  setZoom(value: number): Promise<boolean>
}

export interface ZoomRange {
  min: number
  max: number
  step: number
}

// ---------------------------------------------------------------------------
// Normalization (contract: trim, collapse internal whitespace, uppercase)
// ---------------------------------------------------------------------------

export function normalizeScanValue(raw: string): string {
  return raw.replace(/\s+/g, ' ').trim().toUpperCase()
}

// ---------------------------------------------------------------------------
// Capability detection
// ---------------------------------------------------------------------------

interface DetectedBarcode {
  rawValue: string
  format: string
}

interface BarcodeDetectorLike {
  detect(source: CanvasImageSource): Promise<DetectedBarcode[]>
}

interface BarcodeDetectorConstructor {
  new (options?: { formats?: string[] }): BarcodeDetectorLike
  getSupportedFormats?: () => Promise<string[]>
}

export function isSecureContextAvailable(): boolean {
  return typeof window !== 'undefined' && window.isSecureContext === true
}

export function hasMediaDevices(): boolean {
  return (
    typeof navigator !== 'undefined' &&
    typeof navigator.mediaDevices?.getUserMedia === 'function'
  )
}

export function isBarcodeDetectorSupported(): boolean {
  return typeof window !== 'undefined' && 'BarcodeDetector' in window
}

/** `true` when the browser can open a camera at all (HTTPS + getUserMedia). */
export function isCameraScanSupported(): boolean {
  return isSecureContextAvailable() && hasMediaDevices()
}

export interface ScannerSupport {
  secureContext: boolean
  mediaDevices: boolean
  barcodeDetector: boolean
}

/** Diagnostics for the UI (i18n keys map from these flags). */
export function describeScannerSupport(): ScannerSupport {
  return {
    secureContext: isSecureContextAvailable(),
    mediaDevices: hasMediaDevices(),
    barcodeDetector: isBarcodeDetectorSupported(),
  }
}

function getBarcodeDetectorConstructor(): BarcodeDetectorConstructor | undefined {
  if (typeof window === 'undefined') return undefined
  return (window as unknown as { BarcodeDetector?: BarcodeDetectorConstructor }).BarcodeDetector
}

// ---------------------------------------------------------------------------
// Feedback (audio + haptic)
// ---------------------------------------------------------------------------

export interface ScanFeedback {
  enabled: boolean
  success(): void
  dispose(): void
}

/**
 * Small WebAudio "beep" + vibration helper. The `AudioContext` is created
 * lazily (and must be resumed inside a user gesture on iOS).
 */
export function createFeedback(enabled = true): ScanFeedback {
  let audio: AudioContext | null = null

  const beep = (): void => {
    try {
      const Ctor =
        typeof window === 'undefined'
          ? undefined
          : window.AudioContext ??
            (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
      if (!Ctor) return
      audio ??= new Ctor()
      if (audio.state === 'suspended') void audio.resume()

      const now = audio.currentTime
      const oscillator = audio.createOscillator()
      const gain = audio.createGain()
      oscillator.type = 'sine'
      oscillator.frequency.value = 880
      gain.gain.setValueAtTime(0.0001, now)
      gain.gain.exponentialRampToValueAtTime(0.15, now + 0.01)
      gain.gain.exponentialRampToValueAtTime(0.0001, now + 0.12)
      oscillator.connect(gain)
      gain.connect(audio.destination)
      oscillator.start(now)
      oscillator.stop(now + 0.14)
    } catch {
      // Feedback is best-effort; never break scanning because of it.
    }
  }

  const vibrate = (): void => {
    try {
      navigator.vibrate?.(40)
    } catch {
      // Ignore unsupported vibration.
    }
  }

  return {
    get enabled() {
      return enabled
    },
    set enabled(value: boolean) {
      enabled = value
    },
    success() {
      if (!enabled) return
      beep()
      vibrate()
    },
    dispose() {
      if (audio) {
        void audio.close()
        audio = null
      }
    },
  }
}

// ---------------------------------------------------------------------------
// Error mapping
// ---------------------------------------------------------------------------

function errorName(cause: unknown): string | undefined {
  if (cause instanceof DOMException) return cause.name
  if (cause && typeof cause === 'object' && 'name' in cause) {
    const name = (cause as { name?: unknown }).name
    return typeof name === 'string' ? name : undefined
  }
  return undefined
}

function toScannerError(cause: unknown): ScannerError {
  const message = cause instanceof Error ? cause.message : 'Camera error'
  switch (errorName(cause)) {
    case 'NotAllowedError':
    case 'SecurityError':
      return { code: 'permission_denied', message, cause }
    case 'NotFoundError':
    case 'OverconstrainedError':
      return { code: 'no_camera', message, cause }
    case 'NotReadableError':
    case 'AbortError':
      return { code: 'camera_in_use', message, cause }
    default:
      return { code: 'unknown', message, cause }
  }
}

// ---------------------------------------------------------------------------
// ZXing fallback (lazy)
// ---------------------------------------------------------------------------

interface ZxingDecoder {
  decode(context: CanvasRenderingContext2D, width: number, height: number): string | null
}

/**
 * Convert canvas RGBA pixels into the one-byte-per-pixel luminance plane that
 * ZXing's `RGBLuminanceSource` expects.
 *
 * Passing `ImageData.data` straight through is wrong: ZXing treats any 8-bit
 * array as already-grey pixels, so a 4-bytes-per-pixel RGBA buffer is read as
 * garbage and nothing ever decodes. That broke the ZXing fallback used by
 * iOS Safari (no `BarcodeDetector`), while desktop Chrome kept working via
 * the native detector.
 */
export function rgbaToLuminance(rgba: Uint8ClampedArray, width: number, height: number): Uint8ClampedArray {
  const size = width * height
  const out = new Uint8ClampedArray(size)
  for (let i = 0, p = 0; i < size; i++, p += 4) {
    // Integer BT.601 approximation: (77 R + 150 G + 29 B) / 256.
    out[i] = (rgba[p] * 77 + rgba[p + 1] * 150 + rgba[p + 2] * 29) >> 8
  }
  return out
}

/**
 * The part of the camera frame that is actually visible in a `<video>` styled
 * with `object-fit: cover`. Decoding only this region matches what the user
 * aims at and keeps the ZXing work per frame small on phones (a portrait
 * iPhone stream shown in a 16:9 box hides roughly two thirds of the frame).
 */
export function visibleVideoRegion(
  videoWidth: number,
  videoHeight: number,
  boxWidth: number,
  boxHeight: number,
): { x: number; y: number; width: number; height: number } {
  if (!boxWidth || !boxHeight || !videoWidth || !videoHeight) {
    return { x: 0, y: 0, width: videoWidth, height: videoHeight }
  }
  const videoAspect = videoWidth / videoHeight
  const boxAspect = boxWidth / boxHeight
  if (videoAspect > boxAspect) {
    const width = Math.round(videoHeight * boxAspect)
    return { x: Math.round((videoWidth - width) / 2), y: 0, width, height: videoHeight }
  }
  const height = Math.round(videoWidth / boxAspect)
  return { x: 0, y: Math.round((videoHeight - height) / 2), width: videoWidth, height }
}

async function loadZxingDecoder(): Promise<ZxingDecoder> {
  const {
    MultiFormatReader,
    RGBLuminanceSource,
    BinaryBitmap,
    HybridBinarizer,
    GlobalHistogramBinarizer,
    DecodeHintType,
    BarcodeFormat,
    NotFoundException,
  } = await import('@zxing/library')

  const hints = new Map()
  hints.set(DecodeHintType.POSSIBLE_FORMATS, [BarcodeFormat.CODE_128])
  hints.set(DecodeHintType.TRY_HARDER, true)

  const reader = new MultiFormatReader()
  reader.setHints(hints)

  function attempt(bitmap: InstanceType<typeof BinaryBitmap>): string | null {
    try {
      return reader.decodeWithState(bitmap).getText()
    } catch (error) {
      // NotFoundException is the normal "nothing in this frame" case.
      if (error instanceof NotFoundException) return null
      return null
    } finally {
      reader.reset()
    }
  }

  return {
    decode(context, width, height) {
      const image = context.getImageData(0, 0, width, height)
      const source = new RGBLuminanceSource(rgbaToLuminance(image.data, width, height), width, height)
      // The local (hybrid) binarizer copes with uneven light; the global one
      // is ZXing's recommendation for 1D codes and wins on soft, low-contrast
      // frames. Try both before giving up on a frame.
      return (
        attempt(new BinaryBitmap(new HybridBinarizer(source))) ??
        attempt(new BinaryBitmap(new GlobalHistogramBinarizer(source)))
      )
    },
  }
}

// ---------------------------------------------------------------------------
// Torch helpers (the `torch` capability is not in the standard TS lib types)
// ---------------------------------------------------------------------------

interface TorchCapability {
  torch?: boolean
}

interface TorchConstraint {
  torch?: boolean
}

// ---------------------------------------------------------------------------
// Scanner factory
// ---------------------------------------------------------------------------

/** Minimum time between decoded frames (~11 fps): enough for a QR-grade loop. */
const FRAME_INTERVAL_MS = 90

export function createScanner(options: ScannerOptions): Scanner {
  const {
    video,
    onResult,
    onError,
    onStatus,
    formats = ['code_128'],
    facingMode = 'environment',
    dedupeWindowMs = 1500,
    feedback: feedbackEnabled = true,
    torch: torchInitially = false,
  } = options

  let status: ScannerStatus = 'idle'
  let stream: MediaStream | null = null
  let rafId: number | null = null
  let detector: BarcodeDetectorLike | null = null
  let zxing: ZxingDecoder | null = null
  let usingFallback = false
  let running = false
  let detectBusy = false
  let lastFrameAt = 0
  let lastValue = ''
  let lastValueAt = 0
  let torchOn = false
  let nativeErrors = 0
  let decodeCostMs = 0
  const feedback = createFeedback(feedbackEnabled)

  const canvas = document.createElement('canvas')
  const canvasContext = canvas.getContext('2d', { willReadFrequently: true })

  function setStatus(next: ScannerStatus): void {
    if (status === next) return
    status = next
    onStatus?.(next)
  }

  function fail(error: ScannerError): void {
    setStatus('error')
    onError?.(error)
  }

  function emit(value: string, source: ScanSource): void {
    if (!running || !value) return
    const now = Date.now()
    if (value === lastValue && now - lastValueAt < dedupeWindowMs) return
    lastValue = value
    lastValueAt = now
    feedback.success()
    onResult({ value, format: 'code_128', source, at: now })
  }

  function videoTrack(): MediaStreamTrack | null {
    return stream?.getVideoTracks()[0] ?? null
  }

  async function prepareDecoder(): Promise<void> {
    const Ctor = getBarcodeDetectorConstructor()
    if (Ctor) {
      try {
        if (typeof Ctor.getSupportedFormats === 'function') {
          const supported = await Ctor.getSupportedFormats()
          if (!supported.includes('code_128')) throw new Error('code_128 unsupported')
        }
        detector = new Ctor({ formats })
        usingFallback = false
        return
      } catch {
        detector = null
      }
    }
    zxing = await loadZxingDecoder()
    usingFallback = true
  }

  async function detectNative(): Promise<void> {
    if (!detector || detectBusy) return
    detectBusy = true
    try {
      const results = await detector.detect(video)
      const first = results.find((entry) => entry.rawValue)
      if (first && running) emit(normalizeScanValue(first.rawValue), 'barcode-detector')
      nativeErrors = 0
    } catch {
      // Transient detector errors (e.g. frame not ready) are ignored, but a
      // detector that keeps failing (seen on browsers that expose an
      // incomplete BarcodeDetector) is replaced by the ZXing fallback.
      nativeErrors += 1
      if (nativeErrors >= 15 && running) {
        detector = null
        void loadZxingDecoder().then((decoder) => {
          if (!running) return
          zxing = decoder
          usingFallback = true
        })
      }
    } finally {
      detectBusy = false
    }
  }

  function decodeZxing(): void {
    if (!zxing || !canvasContext) return
    if (!video.videoWidth || !video.videoHeight) return
    const region = visibleVideoRegion(video.videoWidth, video.videoHeight, video.clientWidth, video.clientHeight)
    const { width, height } = region
    if (!width || !height) return
    if (canvas.width !== width) canvas.width = width
    if (canvas.height !== height) canvas.height = height
    const started = performance.now()
    canvasContext.drawImage(video, region.x, region.y, width, height, 0, 0, width, height)
    const value = zxing.decode(canvasContext, width, height)
    decodeCostMs = performance.now() - started
    if (value) emit(normalizeScanValue(value), 'zxing')
  }

  function scheduleFrame(): void {
    if (!running) return
    rafId = window.requestAnimationFrame(onFrame)
  }

  function onFrame(timestamp: number): void {
    rafId = null
    if (!running) return

    const ready =
      video.readyState >= 2 && video.videoWidth > 0 && video.videoHeight > 0

    // ZXing runs on the main thread: leave at least as much idle time as the
    // last decode took so slower phones stay responsive.
    const interval = Math.max(FRAME_INTERVAL_MS, decodeCostMs * 2)
    if (ready && timestamp - lastFrameAt >= interval) {
      lastFrameAt = timestamp
      if (detector) void detectNative()
      else if (zxing) decodeZxing()
    }

    scheduleFrame()
  }

  async function toggleTorch(on?: boolean): Promise<boolean> {
    const track = videoTrack()
    if (!track) return false
    const capabilities = track.getCapabilities?.() as unknown as TorchCapability | undefined
    if (!capabilities?.torch) return false

    const next = on ?? !torchOn
    try {
      await track.applyConstraints({
        advanced: [{ torch: next } as unknown as TorchConstraint as MediaTrackConstraintSet],
      })
      torchOn = next
      return true
    } catch {
      return false
    }
  }

  function zoomRange(): ZoomRange | null {
    const capabilities = videoTrack()?.getCapabilities?.() as unknown as
      | { zoom?: { min?: number; max?: number; step?: number } }
      | undefined
    const zoom = capabilities?.zoom
    if (!zoom || typeof zoom.max !== 'number' || typeof zoom.min !== 'number' || zoom.max <= zoom.min) return null
    return { min: zoom.min, max: zoom.max, step: zoom.step || 0.1 }
  }

  async function setZoom(value: number): Promise<boolean> {
    const track = videoTrack()
    const range = zoomRange()
    if (!track || !range) return false
    const zoom = Math.min(range.max, Math.max(range.min, value))
    try {
      await track.applyConstraints({ advanced: [{ zoom } as unknown as MediaTrackConstraintSet] })
      return true
    } catch {
      return false
    }
  }

  async function enableContinuousFocus(): Promise<void> {
    const track = videoTrack()
    const capabilities = track?.getCapabilities?.() as unknown as { focusMode?: string[] } | undefined
    if (!track || !capabilities?.focusMode?.includes('continuous')) return
    try {
      await track.applyConstraints({
        advanced: [{ focusMode: 'continuous' } as unknown as MediaTrackConstraintSet],
      })
    } catch {
      // Best effort: keep the default focus behaviour.
    }
  }

  async function start(): Promise<void> {
    if (running) return

    if (!isSecureContextAvailable()) {
      fail({
        code: 'insecure_context',
        message: 'Camera access requires a secure (HTTPS) context',
      })
      return
    }
    if (!hasMediaDevices()) {
      fail({ code: 'unsupported', message: 'MediaDevices.getUserMedia is unavailable' })
      return
    }

    setStatus('starting')

    try {
      stream = await navigator.mediaDevices.getUserMedia({
        video: {
          facingMode: { ideal: facingMode },
          // Small PT-E300 labels (6-12 mm tape) need the extra pixels.
          width: { ideal: 1920 },
          height: { ideal: 1080 },
        },
        audio: false,
      })
    } catch (cause) {
      fail(toScannerError(cause))
      return
    }

    await enableContinuousFocus()

    video.setAttribute('playsinline', 'true')
    video.muted = true
    video.playsInline = true
    video.srcObject = stream

    try {
      await video.play()
    } catch {
      // Some browsers reject the play promise; frames are retried by the loop.
    }

    try {
      await prepareDecoder()
    } catch (cause) {
      fail({ code: 'unsupported', message: 'Barcode decoder failed to initialise', cause })
      stopStream()
      return
    }

    running = true
    lastValue = ''
    lastValueAt = 0
    if (torchInitially) await toggleTorch(true)
    setStatus('scanning')
    scheduleFrame()
  }

  function stopStream(): void {
    if (stream) {
      for (const track of stream.getTracks()) track.stop()
      stream = null
    }
    if (video.srcObject) video.srcObject = null
  }

  function stop(): void {
    running = false
    if (rafId !== null) {
      window.cancelAnimationFrame(rafId)
      rafId = null
    }
    detector = null
    zxing = null
    usingFallback = false
    detectBusy = false
    torchOn = false
    nativeErrors = 0
    decodeCostMs = 0
    stopStream()
    feedback.dispose()
    if (status !== 'error') setStatus('stopped')
  }

  return {
    start,
    stop,
    toggleTorch,
    get torchSupported() {
      const capabilities = videoTrack()?.getCapabilities?.() as unknown as
        | TorchCapability
        | undefined
      return Boolean(capabilities?.torch)
    },
    get status() {
      return status
    },
    get usingFallback() {
      return usingFallback
    },
    get zoomRange() {
      return zoomRange()
    },
    setZoom,
  }
}

// ---------------------------------------------------------------------------
// Still-photo decoding (iOS fallback)
// ---------------------------------------------------------------------------

/**
 * Decode a CODE128 label from a photo (e.g. `<input type="file" capture>`).
 *
 * Browser camera streams on iPhone are limited to ~1080p and focus poorly up
 * close, while the native camera app takes a sharp, 12 MP still. The photo is
 * tried at several sizes: large for thin bars, smaller to shrink focus blur.
 */
export async function decodeImageFile(file: Blob): Promise<string | null> {
  const bitmap = await createImageBitmap(file)
  try {
    const NativeDetector = getBarcodeDetectorConstructor()
    if (NativeDetector) {
      try {
        const found = await new NativeDetector({ formats: ['code_128'] }).detect(bitmap)
        const value = found.find((entry) => entry.rawValue)?.rawValue
        if (value) return normalizeScanValue(value)
      } catch {
        // Fall through to ZXing.
      }
    }
    const decoder = await loadZxingDecoder()
    const longest = Math.max(bitmap.width, bitmap.height)
    let lastWidth = 0
    for (const target of [2560, 1600, 1000]) {
      const scale = Math.min(1, target / longest)
      const width = Math.round(bitmap.width * scale)
      const height = Math.round(bitmap.height * scale)
      if (width === lastWidth) continue
      lastWidth = width
      const canvas = document.createElement('canvas')
      canvas.width = width
      canvas.height = height
      const context = canvas.getContext('2d', { willReadFrequently: true })
      if (!context) return null
      context.drawImage(bitmap, 0, 0, width, height)
      const value = decoder.decode(context, width, height)
      if (value) return normalizeScanValue(value)
    }
    return null
  } finally {
    bitmap.close?.()
  }
}
