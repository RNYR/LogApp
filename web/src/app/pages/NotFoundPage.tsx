import { useTranslation } from 'react-i18next'
import { Link } from 'react-router-dom'
import { EmptyState } from '../../components/ui'

export function NotFoundPage() {
  const { t } = useTranslation()

  return (
    <EmptyState
      title={t('errors.notFound')}
      action={
        <Link
          to="/"
          className="inline-flex h-10 items-center justify-center rounded-md border border-stroke-control bg-panel px-4 text-sm font-medium text-content transition hover:bg-panel-subtle focus:outline-none focus-visible:ring-2 focus-visible:ring-action focus-visible:ring-offset-2"
        >
          {t('inventory.title')}
        </Link>
      }
    />
  )
}
