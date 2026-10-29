import { useTranslation } from 'react-i18next'
import { Card, PageHeader } from '../../components/ui'

export interface PlaceholderProps {
  titleKey: string
  /** Task id that will replace this placeholder with the real feature. */
  implementsTask?: string
}

/**
 * Temporary route content for features that are scheduled for later frontend
 * tasks (3.c inventory/options, 3.e scan/bulk, 3.f labels, 4.x settings/users).
 */
export function Placeholder({ titleKey, implementsTask }: PlaceholderProps) {
  const { t } = useTranslation()
  return (
    <Card className="animate-fade-in">
      <PageHeader title={t(titleKey)} />
      {implementsTask ? (
        <p className="mt-2 text-xs uppercase tracking-wide text-content-muted">
          {t('common.loading')} · task {implementsTask}
        </p>
      ) : null}
    </Card>
  )
}
