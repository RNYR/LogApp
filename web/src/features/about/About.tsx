import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { useSession } from '../../lib/auth/sessionContext'
import { useLocale } from '../../lib/i18n/useLocale'
import { Alert, Card, LoadingState, PageHeader } from '../../components/ui'

interface ThirdParty {
  name: string
  license: string
}

const THIRD_PARTY: readonly ThirdParty[] = [
  { name: 'Work Sans', license: 'SIL Open Font License 1.1' },
  { name: 'React', license: 'MIT' },
  { name: 'React Router', license: 'MIT' },
  { name: 'TanStack Query', license: 'MIT' },
  { name: 'Vite', license: 'MIT' },
  { name: 'Tailwind CSS', license: 'MIT' },
  { name: 'i18next', license: 'MIT' },
  { name: '@zxing/library', license: 'Apache-2.0' },
  { name: 'modernc.org/sqlite', license: 'BSD-3-Clause' },
  { name: 'Caddy', license: 'Apache-2.0' },
]

export function About() {
  const { t } = useTranslation()
  const { user } = useSession()
  const isAdmin = user?.role === 'admin'
  const { locale } = useLocale()

  const versionQuery = useQuery({
    queryKey: ['version'],
    queryFn: ({ signal }) => api.system.version(signal),
    staleTime: Infinity,
    retry: false,
  })

  const version = versionQuery.data
  const buildTime = (() => {
    if (!version?.build_time) return '—'
    const parsed = new Date(version.build_time)
    if (Number.isNaN(parsed.getTime())) return version.build_time
    return new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(parsed)
  })()

  if (versionQuery.isPending) return <LoadingState label={t('common.loading')} className="py-16" />

  if (versionQuery.isError) {
    return (
      <Alert variant="error" title={t('common.error')}>
        {apiErrorMessage(versionQuery.error, t)}
      </Alert>
    )
  }

  return (
    <div className="mx-auto max-w-2xl space-y-4">
      <PageHeader title={t('about.title')} description={t('app.tagline')} />

      <Card className="animate-fade-in">
        <p className="text-sm text-content">{t('about.description')}</p>

        <p className="mt-5 rounded-lg bg-info-bg px-3 py-2 text-sm font-medium text-info-fg">
          {t('about.createdBy')}
        </p>

        <dl className="mt-6 grid gap-3 text-sm">
          <div className="flex min-w-0 flex-wrap justify-between gap-4 border-b border-stroke pb-2">
            <dt className="font-medium text-content-muted">{t('common.version')}</dt>
            <dd className="min-w-0 break-words text-right text-content">
              <span className="font-mono">{version?.version ?? '—'}</span>
              {isAdmin && version?.commit ? (
                <span className="ml-2 font-mono text-xs text-content-muted">{version.commit.slice(0, 8)}</span>
              ) : null}
            </dd>
          </div>
          {isAdmin ? <div className="flex min-w-0 flex-wrap justify-between gap-4 border-b border-stroke pb-2">
            <dt className="font-medium text-content-muted">{t('about.buildTime')}</dt>
            <dd className="min-w-0 break-words text-right text-content">{buildTime}</dd>
          </div> : null}
          <div className="flex min-w-0 flex-wrap justify-between gap-4">
            <dt className="font-medium text-content-muted">{t('common.license')}</dt>
            <dd className="min-w-0 break-words text-content">
              MIT · {t('about.license')}
            </dd>
          </div>
        </dl>
      </Card>

      <Card className="animate-fade-in">
        <h2 className="text-lg font-semibold text-content">{t('about.thirdParty')}</h2>
        <ul className="mt-3 grid gap-2 text-sm">
          {THIRD_PARTY.map((dependency) => (
            <li
              key={dependency.name}
              className="flex min-w-0 flex-wrap items-center justify-between gap-4 border-b border-stroke pb-1 last:border-0"
            >
              <span className="min-w-0 break-words text-content">{dependency.name}</span>
              <span className="font-mono text-xs text-content-muted">{dependency.license}</span>
            </li>
          ))}
        </ul>
        <p className="mt-4 text-xs text-content-muted">{t('about.source')}: sklad</p>
      </Card>

      <p className="text-center text-xs text-content-muted">
        {t('footer.madeBy')} · {t('footer.openSource')}
      </p>
    </div>
  )
}
