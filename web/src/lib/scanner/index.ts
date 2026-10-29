/**
 * Public surface of the Sklad scanner library.
 *
 * - `scanner.ts` — camera CODE128 decoding (native `BarcodeDetector` with a
 *   lazy ZXing fallback), torch control, and audio/haptic feedback.
 * - `hid.ts` — hardware keyboard-wedge capture and manual-entry helpers.
 */

export {
  createScanner,
  createFeedback,
  decodeImageFile,
  normalizeScanValue,
  isSecureContextAvailable,
  hasMediaDevices,
  isBarcodeDetectorSupported,
  isCameraScanSupported,
  describeScannerSupport,
} from './scanner'
export type {
  Scanner,
  ScannerOptions,
  ScannerStatus,
  ScannerError,
  ScannerErrorCode,
  ScanSource,
  ScanResult,
  ScanFeedback,
  ScannerSupport,
  ZoomRange,
} from './scanner'

export { createHidScanner, attachHidScanner, isEditableElement } from './hid'
export type { HidScanner, HidScannerOptions, HidTerminator } from './hid'
