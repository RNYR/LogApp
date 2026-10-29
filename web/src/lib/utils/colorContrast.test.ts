import { describe, expect, it } from 'vitest'
import { colorContrastTextClass } from './colorContrast'

/** Independent WCAG 2.1 relative-luminance implementation for verification. */
function luminance(hex: string): number {
  const clean = hex.replace(/^#/, '')
  const channels = [0, 2, 4].map((offset) => Number.parseInt(clean.slice(offset, offset + 2), 16) / 255)
  const linear = channels.map((value) => (value <= 0.03928 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4))
  return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]
}

function contrast(background: string, foreground: string): number {
  const a = luminance(background)
  const b = luminance(foreground)
  const lighter = Math.max(a, b)
  const darker = Math.min(a, b)
  return (lighter + 0.05) / (darker + 0.05)
}

function foregroundFor(className: string): string {
  return className === 'text-content-onColor' ? '#000000' : '#ffffff'
}

describe('colorContrastTextClass', () => {
  it('chooses white text on dark colours', () => {
    expect(colorContrastTextClass('#000000')).toBe('text-content-inverse')
    expect(colorContrastTextClass('#0b6287')).toBe('text-content-inverse')
  })

  it('chooses black text on light colours', () => {
    expect(colorContrastTextClass('#ffffff')).toBe('text-content-onColor')
    expect(colorContrastTextClass('#ffd966')).toBe('text-content-onColor')
  })

  it('picks a legible foreground for the legacy status palette', () => {
    for (const color of ['#93c47d', '#8faadc', '#cc99ff', '#ffd966', '#e06666']) {
      const className = colorContrastTextClass(color)
      expect(className).toBe('text-content-onColor')
      expect(contrast(color, foregroundFor(className))).toBeGreaterThanOrEqual(4.5)
    }
  })

  it('accepts upper-case and shorthand hex', () => {
    expect(colorContrastTextClass('#FFF')).toBe('text-content-onColor')
    expect(colorContrastTextClass('#000')).toBe('text-content-inverse')
    expect(colorContrastTextClass('  #0B6287  ')).toBe('text-content-inverse')
  })

  it('falls back to semantic content without throwing for missing/invalid values', () => {
    expect(colorContrastTextClass(null)).toBe('text-content')
    expect(colorContrastTextClass(undefined)).toBe('text-content')
    expect(colorContrastTextClass('')).toBe('text-content')
    expect(colorContrastTextClass('not-a-colour')).toBe('text-content')
    expect(colorContrastTextClass('#12345')).toBe('text-content')
    expect(colorContrastTextClass('rgba(0,0,0,0.5)')).toBe('text-content')
  })

  it('keeps the selected foreground contrast >= 4.5:1 across a grayscale sweep', () => {
    for (let value = 0; value <= 255; value += 1) {
      const channel = value.toString(16).padStart(2, '0')
      const color = `#${channel}${channel}${channel}`
      const className = colorContrastTextClass(color)
      expect(contrast(color, foregroundFor(className))).toBeGreaterThanOrEqual(4.5)
    }
  })
})
