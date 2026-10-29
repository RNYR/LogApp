import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Link, useSearchParams } from 'react-router-dom'
import { api, type Item } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { Alert, Card, EmptyState, Field, Input, LoadingState, PageHeader } from '../../components/ui'

export function LabelView() {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const presetTag = params.get('tag') ?? ''
  const [search, setSearch] = useState(presetTag)
  const [query, setQuery] = useState(presetTag)
  const [selected, setSelected] = useState<Item | null>(null)
  useEffect(() => {
    const timer = setTimeout(() => setQuery(search.trim()), 250)
    return () => clearTimeout(timer)
  }, [search])
  const itemsQuery = useQuery({
    queryKey: ['label-items', query],
    queryFn: ({ signal }) => api.items.list({ q: query, per_page: 20, archived: false }, signal),
    enabled: query.length > 0,
  })
  const results = itemsQuery.data?.data ?? []
  useEffect(() => {
    if (selected || !presetTag || !itemsQuery.data) return
    const exact = itemsQuery.data.data.find(item => item.asset_tag === presetTag)
    if (exact) setSelected(exact)
  }, [presetTag, itemsQuery.data, selected])
  return <div className="space-y-4">
    <PageHeader title={t('labels.title')} description={t('shell.labelsHint')} />
    <div className="grid gap-4 lg:grid-cols-2">
      <Card className="min-w-0 space-y-4 p-5">
          <Field label={t('common.search')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                value={search}
                placeholder={t('inventory.searchPlaceholder')}
                onChange={(event) => setSearch(event.target.value)}
              />
            )}
          </Field>

          {itemsQuery.isPending && query.length > 0 ? (
            <LoadingState label={t('common.loading')} className="justify-start py-2" />
          ) : null}

          {itemsQuery.isError ? (
            <Alert variant="error" title={t('common.error')}>
              {apiErrorMessage(itemsQuery.error, t)}
            </Alert>
          ) : null}

          {itemsQuery.isSuccess && query.length > 0 && results.length === 0 ? (
            <EmptyState title={t('inventory.emptyFiltered')} />
          ) : null}

          {results.length > 0 ? (
            <ul className="max-h-56 divide-y divide-stroke overflow-y-auto">
              {results.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    className="flex w-full min-w-0 items-center gap-3 px-1 py-2 text-left hover:bg-panel-subtle"
                    onClick={() => {
                      setSelected(item)
                      setSearch(item.asset_tag)
                    }}
                  >
                    <span className="shrink-0 font-mono text-xs text-content-muted">{item.asset_tag}</span>
                    <span className="min-w-0 flex-1 truncate text-sm text-content">
                      {item.name || t('common.untitled')}
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          ) : null}


        {selected ? <section aria-label={t('labels.assetId')} className="space-y-3 rounded-lg border border-stroke bg-panel-subtle p-4">
          <h2 className="text-sm font-medium text-content-muted">{t('labels.assetId')}</h2>
          <p className="break-all font-mono text-3xl font-semibold text-content">{selected.asset_tag}</p>
          <p className="break-words text-content">{selected.name || t('common.untitled')}</p>
          <p className="text-sm text-content-muted">{t('labels.exactTag')}</p>
        </section> : <EmptyState title={t('labels.noItem')} />}
      </Card>
      <Card className="min-w-0 space-y-4 p-5">
        <h2 className="text-lg font-semibold text-content">Brother PT-E300</h2>
        <p className="text-sm text-content-muted">{t('labels.manualOnly')}</p>
        <ol className="list-decimal space-y-3 pl-5 text-sm text-content">
          <li>{t('labels.step1')}</li>
          <li>{t('labels.step2')}</li>
          <li>{t('labels.step3')}</li>
          <li>{t('labels.step4')}</li>
        </ol>
        <Link to="/scan" className="inline-block font-medium text-content underline underline-offset-4">{t('labels.verifyScan')}</Link>
      </Card>
    </div>
  </div>
}
