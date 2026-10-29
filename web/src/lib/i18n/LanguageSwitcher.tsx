import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../utils/cn'
import { Select } from '../../components/ui'
import type { Locale } from './index'
import { useLocale } from './useLocale'

export interface LanguageSwitcherProps {
  className?: string
}

/** Language selector for `en` / `cs` / `it`; keeps `<html lang>` in sync. */
export function LanguageSwitcher({ className }: LanguageSwitcherProps) {
  const { locale, locales, setLocale } = useLocale()
  const { t } = useTranslation()

  useEffect(() => {
    document.documentElement.lang = locale
  }, [locale])

  return (
    <label className={cn('inline-flex items-center gap-2 text-sm', className)}>
      <span className="sr-only">{t('common.language')}</span>
      <Select
        className="!h-9 !w-auto"
        value={locale}
        aria-label={t('common.language')}
        onChange={(event) => setLocale(event.target.value as Locale)}
      >
        {locales.map((code) => (
          <option key={code} value={code}>
            {t(`languages.${code}`)}
          </option>
        ))}
      </Select>
    </label>
  )
}
