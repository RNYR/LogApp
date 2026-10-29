import { describe, expect, it } from 'vitest'
import {
  BarcodeFormat,
  BinaryBitmap,
  DecodeHintType,
  HybridBinarizer,
  MultiFormatReader,
  RGBLuminanceSource,
} from '@zxing/library'
import { rgbaToLuminance, visibleVideoRegion } from './scanner'

// CODE128 module pattern for "PV-001" (1 = bar), generated with python-barcode.
const PV_001 =
  '11010010000111011101101110101100010011011100100111011001001110110010011100110110110110001100011101011'

/** Render the pattern as an RGBA frame like a camera canvas would produce. */
function renderFrame(pattern: string, moduleSize = 3, width = 640, height = 240): Uint8ClampedArray {
  const rgba = new Uint8ClampedArray(width * height * 4).fill(255)
  const left = Math.floor((width - pattern.length * moduleSize) / 2)
  for (let y = 60; y < height - 60; y++) {
    for (let m = 0; m < pattern.length; m++) {
      if (pattern[m] !== '1') continue
      for (let dx = 0; dx < moduleSize; dx++) {
        const p = (y * width + left + m * moduleSize + dx) * 4
        rgba[p] = 20
        rgba[p + 1] = 30
        rgba[p + 2] = 40
      }
    }
  }
  return rgba
}

function decode(luminance: Uint8ClampedArray, width: number, height: number): string | null {
  const hints = new Map()
  hints.set(DecodeHintType.POSSIBLE_FORMATS, [BarcodeFormat.CODE_128])
  hints.set(DecodeHintType.TRY_HARDER, true)
  try {
    const bitmap = new BinaryBitmap(new HybridBinarizer(new RGBLuminanceSource(luminance, width, height)))
    return new MultiFormatReader().decode(bitmap, hints).getText()
  } catch {
    return null
  }
}

describe('ZXing fallback decoding (iOS Safari path)', () => {
  it('decodes a CODE128 label after converting RGBA to luminance', () => {
    const rgba = renderFrame(PV_001)
    expect(decode(rgbaToLuminance(rgba, 640, 240), 640, 240)).toBe('PV-001')
  })

  it('cannot decode when raw RGBA is passed as luminance (the original bug)', () => {
    const rgba = renderFrame(PV_001)
    expect(decode(rgba, 640, 240)).toBeNull()
  })

  it('weights green most, like BT.601', () => {
    const lum = rgbaToLuminance(new Uint8ClampedArray([255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255]), 3, 1)
    expect(Array.from(lum)).toEqual([76, 149, 28])
  })
})

describe('visibleVideoRegion', () => {
  it('crops a portrait phone stream shown in a 16:9 box to the visible band', () => {
    expect(visibleVideoRegion(1080, 1920, 320, 180)).toEqual({ x: 0, y: 656, width: 1080, height: 608 })
  })

  it('crops the sides of a wide stream shown in a narrower box', () => {
    expect(visibleVideoRegion(1920, 1080, 400, 300)).toEqual({ x: 240, y: 0, width: 1440, height: 1080 })
  })

  it('falls back to the full frame when the element has no size', () => {
    expect(visibleVideoRegion(1280, 720, 0, 0)).toEqual({ x: 0, y: 0, width: 1280, height: 720 })
  })
})
