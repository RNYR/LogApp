/**
 * Pick a guaranteed-contrast foreground for an arbitrary (API-supplied) colour.
 *
 * Status colours come from the backend as data (`item.status_color`, options
 * `statuses[].color`) and are not semantic tokens. Because they can be any valid
 * hex value, neither a fixed dark nor a fixed light foreground is always legible;
 * this helper chooses whichever of black or white has the higher WCAG 2.1
 * contrast ratio against the supplied colour.
 *
 * Unsupported or missing values fall back to the semantic `content` foreground
 * and never throw — this module does not validate or rewrite API payloads.
 */

function parseHex(color: string): [number, number, number] | null {
  const hex = color.trim().replace(/^#/, '')
  if (/^[0-9a-fA-F]{3}$/.test(hex)) {
    return [
      Number.parseInt(hex[0] + hex[0], 16),
      Number.parseInt(hex[1] + hex[1], 16),
      Number.parseInt(hex[2] + hex[2], 16),
    ]
  }
  if (/^[0-9a-fA-F]{6}$/.test(hex)) {
    return [
      Number.parseInt(hex.slice(0, 2), 16),
      Number.parseInt(hex.slice(2, 4), 16),
      Number.parseInt(hex.slice(4, 6), 16),
    ]
  }
  return null
}

/** WCAG 2.1 relative luminance of an sRGB colour. */
function relativeLuminance([red, green, blue]: [number, number, number]): number {
  const channel = (value: number): number => {
    const scaled = value / 255
    return scaled <= 0.03928 ? scaled / 12.92 : ((scaled + 0.055) / 1.055) ** 2.4
  }
  return 0.2126 * channel(red) + 0.7152 * channel(green) + 0.0722 * channel(blue)
}

function contrastRatio(a: number, b: number): number {
  const lighter = Math.max(a, b)
  const darker = Math.min(a, b)
  return (lighter + 0.05) / (darker + 0.05)
}

/**
 * Tailwind text-colour class for text drawn on top of `color`.
 *
 * Returns `text-content-inverse` (white) or `text-content-onColor` (black),
 * whichever has the greater contrast; `text-content` for missing/invalid input.
 */
export function colorContrastTextClass(color: string | null | undefined): string {
  if (!color) return 'text-content'
  const rgb = parseHex(color)
  if (!rgb) return 'text-content'

  const luminance = relativeLuminance(rgb)
  const onWhite = contrastRatio(luminance, 1)
  const onBlack = contrastRatio(luminance, 0)
  return onWhite >= onBlack ? 'text-content-inverse' : 'text-content-onColor'
}
