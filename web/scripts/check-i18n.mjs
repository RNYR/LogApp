#!/usr/bin/env node
/**
 * i18n catalog parity check (dependency-free).
 *
 * `src/locales/en.json` is the source of truth. This script fails when the
 * `cs`/`it` catalogs do not mirror it exactly:
 *
 *   - identical key sets (no missing / no extra keys),
 *   - identical `{{placeholder}}` sets per key (e.g. `{{count}}`),
 *   - no empty translations.
 *
 * Run with `--verbose` for key-order and untranslated-value information.
 *
 *   node scripts/check-i18n.mjs
 *   npm run check:i18n
 */

import { readFileSync } from 'node:fs'
import { dirname, join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

const VERBOSE = process.argv.includes('--verbose')
const root = join(dirname(fileURLToPath(import.meta.url)), '..')
const localesDir = join(root, 'src', 'locales')
const SOURCE = 'en'
const TARGETS = ['cs', 'it']
const PLACEHOLDER = /\{\{\s*([A-Za-z0-9_]+)\s*\}\}/g

/** @param {string} name */
function loadLocale(name) {
  const file = join(localesDir, `${name}.json`)
  try {
    return JSON.parse(readFileSync(file, 'utf8'))
  } catch (error) {
    throw new Error(`Cannot read ${relative(root, file)}: ${error instanceof Error ? error.message : String(error)}`)
  }
}

/** Flatten a nested catalog into `dotted.path -> value`. */
function flatten(node, prefix = '', out = new Map()) {
  for (const [key, value] of Object.entries(node)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
      flatten(value, path, out)
    } else {
      out.set(path, value)
    }
  }
  return out
}

/** Sorted placeholder names found in a string value. */
function placeholders(value) {
  if (typeof value !== 'string') return []
  return [...value.matchAll(PLACEHOLDER)].map((match) => match[1]).sort()
}

function main() {
  const source = flatten(loadLocale(SOURCE))
  const sourceKeys = [...source.keys()]
  let failures = 0

  for (const target of TARGETS) {
    const catalog = flatten(loadLocale(target))
    const targetKeys = [...catalog.keys()]

    const missing = sourceKeys.filter((key) => !catalog.has(key))
    const extra = targetKeys.filter((key) => !source.has(key))

    const placeholderIssues = []
    const empty = []
    const untranslated = []

    for (const [key, value] of source) {
      if (!catalog.has(key)) continue
      const translated = catalog.get(key)

      if (typeof translated === 'string' && translated.trim() === '') empty.push(key)

      const expected = placeholders(value).join(',')
      const actual = placeholders(translated).join(',')
      if (expected !== actual) {
        placeholderIssues.push(`${key} (expected [${expected}], got [${actual}])`)
      }

      if (
        VERBOSE &&
        typeof value === 'string' &&
        translated === value &&
        /[A-Za-z]/.test(value)
      ) {
        untranslated.push(key)
      }
    }

    const ok =
      missing.length === 0 &&
      extra.length === 0 &&
      placeholderIssues.length === 0 &&
      empty.length === 0

    console.log(`${ok ? '✓' : '✗'} ${target}.json — ${targetKeys.length} keys`)

    if (missing.length) {
      failures += 1
      console.log(`    missing: ${missing.slice(0, 12).join(', ')}${missing.length > 12 ? ` … (+${missing.length - 12})` : ''}`)
    }
    if (extra.length) {
      failures += 1
      console.log(`    extra: ${extra.slice(0, 12).join(', ')}${extra.length > 12 ? ` … (+${extra.length - 12})` : ''}`)
    }
    if (placeholderIssues.length) {
      failures += 1
      console.log(`    placeholder mismatches: ${placeholderIssues.slice(0, 8).join('; ')}${placeholderIssues.length > 8 ? ' …' : ''}`)
    }
    if (empty.length) {
      failures += 1
      console.log(`    empty translations: ${empty.slice(0, 12).join(', ')}${empty.length > 12 ? ' …' : ''}`)
    }

    if (VERBOSE) {
      if (missing.length === 0 && targetKeys.join('\n') !== sourceKeys.join('\n')) {
        console.log('    note: key order differs from en.json (harmless)')
      }
      if (untranslated.length) {
        console.log(`    note: ${untranslated.length} value(s) identical to en (brand/units/IDs are fine)`)
      }
    }
  }

  if (failures > 0) {
    console.error(`\ni18n check failed (${failures} problem group(s)).`)
    process.exit(1)
  }
  console.log(`\ni18n catalogs are consistent (${sourceKeys.length} keys, ${SOURCE} -> ${TARGETS.join(', ')}).`)
}

try {
  main()
} catch (error) {
  console.error(`✗ ${error instanceof Error ? error.message : String(error)}`)
  process.exit(1)
}
