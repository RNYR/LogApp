import { useCallback, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { DEFAULT_LOCALE, SUPPORTED_LOCALES, normalizeLocale, type Locale } from './index'

export interface LocaleController {
  locale: Locale
  locales: readonly Locale[]
  defaultLocale: Locale
  setLocale: (locale: Locale) => void
}

/** Reads/changes the active i18n locale (persisted by i18next in localStorage). */
export function useLocale(): LocaleController {
  const { i18n } = useTranslation()
  const locale = normalizeLocale(i18n.resolvedLanguage ?? i18n.language)

  const setLocale = useCallback(
    (next: Locale) => {
      void i18n.changeLanguage(next)
    },
    [i18n],
  )

  return useMemo(
    () => ({ locale, locales: SUPPORTED_LOCALES, defaultLocale: DEFAULT_LOCALE, setLocale }),
    [locale, setLocale],
  )
}
