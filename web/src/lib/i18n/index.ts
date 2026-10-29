/**
 * i18next runtime for Sklad.
 *
 * Supported locales: Czech (`cs`), English (`en`, fallback/source of truth) and
 * Italian (`it`). The active language is persisted in `localStorage`
 * (`sklad.lang`) and otherwise detected from the browser.
 *
 * Importing this module initialises i18next as a side effect; `main.tsx` imports
 * it once before the app renders.
 */

import i18n from 'i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import { initReactI18next } from 'react-i18next'

import en from '../../locales/en.json'
import cs from '../../locales/cs.json'
import it from '../../locales/it.json'

export const SUPPORTED_LOCALES = ['en', 'cs', 'it'] as const
export type Locale = (typeof SUPPORTED_LOCALES)[number]

/** Fallback and source-of-truth locale (see `docs/contracts` and task 3.d). */
export const DEFAULT_LOCALE: Locale = 'en'

/** `localStorage` key used by the language detector and the switcher. */
export const LOCALE_STORAGE_KEY = 'sklad.lang'

export const resources = {
  en: { translation: en },
  cs: { translation: cs },
  it: { translation: it },
}

void i18n
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources,
    fallbackLng: DEFAULT_LOCALE,
    supportedLngs: [...SUPPORTED_LOCALES],
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    interpolation: {
      // React already escapes interpolated values.
      escapeValue: false,
    },
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: LOCALE_STORAGE_KEY,
      caches: ['localStorage'],
    },
    returnNull: false,
  })

/** Coerce an arbitrary language tag (e.g. `en-US`) to a supported locale. */
export function normalizeLocale(value: string | null | undefined): Locale {
  if (!value) return DEFAULT_LOCALE
  const base = value.toLowerCase().split('-')[0]
  return (SUPPORTED_LOCALES as readonly string[]).includes(base)
    ? (base as Locale)
    : DEFAULT_LOCALE
}

export default i18n
