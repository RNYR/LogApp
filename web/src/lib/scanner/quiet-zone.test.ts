import { describe, expect, it } from 'vitest'
import { loadZxingDecoder, quietZoneCuts } from './scanner'
// Gzipped binary PGM, base64 so it loads through Vite without Node APIs.
import lensBarrelB64 from './__fixtures__/lens-barrel-pv-001.pgm.gz.b64?raw'

// CODE128 module pattern for "PV-001" (1 = bar), as in zxing-decode.test.ts.
const PV_001 =
  '11010010000111011101101110101100010011011100100111011001001110110010011100110110110110001100011101011'

/**
 * A label wrapped on a dark part: white tape with only `margin` modules of
 * clear space on each side of the bars, dark object everywhere else.
 */
function cramped(margin: number, moduleSize = 4, width = 800, height = 300) {
  const luminance = new Uint8ClampedArray(width * height).fill(35)
  const tapeWidth = (PV_001.length + 2 * margin) * moduleSize
  const tapeLeft = Math.floor((width - tapeWidth) / 2)
  for (let y = 100; y < 200; y++) {
    for (let x = 0; x < tapeWidth; x++) {
      const module = Math.floor(x / moduleSize) - margin
      const bar = module >= 0 && module < PV_001.length && PV_001[module] === '1'
      luminance[y * width + tapeLeft + x] = bar ? 25 : 225
    }
  }
  return { luminance, width, height }
}

/** Real frame: a PT-E300 label on a lens barrel, greyscale at 1080p scale. */
async function lensBarrel() {
  const gz = Uint8Array.from(atob(lensBarrelB64.replace(/\s+/g, '')), (c) => c.charCodeAt(0))
  const stream = new Blob([gz]).stream().pipeThrough(new DecompressionStream('gzip'))
  const pgm = new Uint8Array(await new Response(stream).arrayBuffer())
  const header = new TextDecoder('latin1').decode(pgm.subarray(0, 32)).match(/^P5\s+(\d+)\s+(\d+)\s+255\s/)
  if (!header) throw new Error('bad fixture')
  const [width, height] = [Number(header[1]), Number(header[2])]
  return { luminance: new Uint8ClampedArray(pgm.subarray(header[0].length)), width, height }
}

const MID_BAND = { center: 0.5, halfHeight: 0.15 }

describe('quietZoneCuts', () => {
  it('cuts through the tape margins on both sides of the bars', () => {
    const { luminance, width, height } = cramped(4)
    const pairs = quietZoneCuts(luminance, width, height, MID_BAND)
    // tape starts at 182; margins are 4 modules of 4 px each side
    expect(pairs).toContainEqual([190, 610])
  })

  it('returns nothing for a flat or barcode-free band', () => {
    const flat = new Uint8ClampedArray(400 * 100).fill(128)
    expect(quietZoneCuts(flat, 400, 100, MID_BAND)).toEqual([])
    const { luminance, width, height } = cramped(4)
    expect(quietZoneCuts(luminance, width, height, { center: 0.15, halfHeight: 0.1 })).toEqual([])
  })
})

describe('quiet-zone assisted decoding', () => {
  it('reads a synthetic label whose margin is too narrow for plain ZXing', async () => {
    const decoder = await loadZxingDecoder()
    const { luminance, width, height } = cramped(3)
    expect(decoder.decode(luminance, width, height)).toBeNull()
    expect(decoder.decodeQuietZone(luminance, width, height, [MID_BAND], 4)).toBe('PV-001')
  })

  it('reads the real lens-barrel label that plain ZXing misses', async () => {
    const decoder = await loadZxingDecoder()
    const { luminance, width, height } = await lensBarrel()
    expect(decoder.decode(luminance, width, height)).toBeNull()
    expect(decoder.decodeQuietZone(luminance, width, height, [MID_BAND], 4)).toBe('PV-001')
  })

  it('respects the try budget', async () => {
    const decoder = await loadZxingDecoder()
    const { luminance, width, height } = await lensBarrel()
    expect(decoder.decodeQuietZone(luminance, width, height, [MID_BAND], 0)).toBeNull()
  })
})
