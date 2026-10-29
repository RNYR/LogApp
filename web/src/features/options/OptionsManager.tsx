import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type LocalizedLabels, type Options } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { useSession } from '../../lib/auth/sessionContext'
import { useLocale } from '../../lib/i18n/useLocale'
import type { Locale } from '../../lib/i18n'
import { Alert, Button, Card, Field, IconButton, Input, LoadingState, Notice as NoticeMessage, PageHeader } from '../../components/ui'
import { cn } from '../../lib/utils/cn'

type OptionsDraft = Options['data']

/** Legacy status palette, offered as one-click swatches. */
const PRESET_COLORS = ['#93c47d', '#8faadc', '#cc99ff', '#ffd966', '#e06666', '#0a99d1']

function cloneOptions(data: OptionsDraft): OptionsDraft {
  return JSON.parse(JSON.stringify(data)) as OptionsDraft
}

function slugify(label: string): string {
  const slug = label
    .normalize('NFD')
    .replace(/[\u0300-\u036f]/g, '')
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 40)
  return slug || 'option'
}

function uniqueValue(base: string, taken: Set<string>): string {
  let value = base
  let suffix = 2
  while (taken.has(value)) {
    value = `${base}-${suffix}`
    suffix += 1
  }
  return value
}

function setLocaleLabel(existing: LocalizedLabels | undefined, locale: Locale, label: string): LocalizedLabels {
  const next: LocalizedLabels = { ...(existing ?? {}) }
  if (locale === 'en') next.en = label
  else if (locale === 'cs') next.cs = label
  else next.it = label
  return next
}

function normalizeHex(value: string): string {
  return /^#[0-9a-fA-F]{6}$/.test(value) ? value : '#000000'
}

function ColorField({ value, onChange, label }: { value: string; onChange: (color: string) => void; label: string }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <input
        type="color"
        value={normalizeHex(value)}
        onChange={(event) => onChange(event.target.value)}
        aria-label={label}
        className="h-8 w-10 cursor-pointer rounded border border-stroke-control bg-panel p-0.5"
      />
      <div className="flex flex-wrap items-center gap-1">
        {PRESET_COLORS.map((color) => {
          const selected = value.toLowerCase() === color
          return (
            <button
              key={color}
              type="button"
              onClick={() => onChange(color)}
              aria-pressed={selected}
              className={cn(
                'h-6 w-6 rounded-full border transition',
                selected ? 'border-action ring-2 ring-action' : 'border-stroke-control hover:scale-110',
              )}
              style={{ backgroundColor: color }}
              aria-label={color}
              title={color}
            />
          )
        })}
      </div>
      <Input
        className="h-8 w-28"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-label={label}
      />
    </div>
  )
}

/**
 * Admin options manager: edit item types and statuses (per-locale labels and
 * status colours), add new options, and remove unused ones.
 *
 * Renames edit the *label* only — the machine `value` never changes, so items
 * keep their references. Deletion of an in-use option is disabled client-side
 * and would also be rejected by the API with `409`.
 */
export function OptionsManager() {
  const { t } = useTranslation()
  const { locale } = useLocale()
  const { user } = useSession()
  const queryClient = useQueryClient()
  const isAdmin = user?.role === 'admin'

  const [draft, setDraft] = useState<OptionsDraft | null>(null)
  const [dirty, setDirty] = useState(false)
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; message: string } | null>(null)

  const optionsQuery = useQuery({
    queryKey: ['options', locale],
    queryFn: ({ signal }) => api.options.get(locale, signal),
  })

  const usageQuery = useQuery({
    queryKey: ['options-usage'],
    queryFn: async ({ signal }) => {
      // Both active and archived equipment keep option references.
      async function allItems(archived: boolean) {
        const first = await api.items.list({ per_page: 1000, archived }, signal)
        const rows = [...first.data]
        for (let page = 2; page <= (first.meta?.total_pages ?? 1); page += 1) {
          rows.push(...(await api.items.list({ per_page: 1000, archived, page }, signal)).data)
        }
        return rows
      }
      const [active, archived] = await Promise.all([allItems(false), allItems(true)])
      return { data: [...active, ...archived] }
    },
    enabled: isAdmin,
  })

  useEffect(() => {
    if (optionsQuery.data && !dirty) setDraft(cloneOptions(optionsQuery.data))
  }, [optionsQuery.data, dirty])

  const usedTypes = useMemo(
    () =>
      new Set(
        (usageQuery.data?.data ?? [])
          .map((item) => item.type)
          .filter((value): value is string => Boolean(value)),
      ),
    [usageQuery.data],
  )
  const usedStatuses = useMemo(
    () =>
      new Set(
        (usageQuery.data?.data ?? [])
          .map((item) => item.status)
          .filter((value): value is string => Boolean(value)),
      ),
    [usageQuery.data],
  )

  const save = useMutation({
    mutationFn: () => {
      if (!draft) throw new Error('No options to save')
      const types = [...draft.types]
        .sort((a, b) => a.label.localeCompare(b.label, locale))
        .map((type, index) => ({ ...type, sort_order: index }))
      const statuses = draft.statuses.map((status, index) => ({ ...status, sort_order: index }))
      return api.options.update({ types, statuses })
    },
    onSuccess: (data) => {
      setDirty(false)
      setDraft(cloneOptions(data))
      setNotice({ kind: 'success', message: t('options.saved') })
      void queryClient.invalidateQueries({ queryKey: ['options'] })
      void queryClient.invalidateQueries({ queryKey: ['items'] })
    },
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  function touch(): void {
    setDirty(true)
    setNotice(null)
  }

  function addType(): void {
    setDraft((prev) => {
      if (!prev) return prev
      const taken = new Set(prev.types.map((entry) => entry.value))
      const label = t('common.untitled')
      const value = uniqueValue(slugify(label), taken)
      const labels: LocalizedLabels = { en: label, cs: label, it: label }
      return { ...prev, types: [...prev.types, { value, label, labels, sort_order: prev.types.length }] }
    })
    touch()
  }

  function addStatus(): void {
    setDraft((prev) => {
      if (!prev) return prev
      const taken = new Set(prev.statuses.map((entry) => entry.value))
      const label = t('common.untitled')
      const value = uniqueValue(slugify(label), taken)
      const labels: LocalizedLabels = { en: label, cs: label, it: label }
      return {
        ...prev,
        statuses: [...prev.statuses, { value, label, labels, color: PRESET_COLORS[0], sort_order: prev.statuses.length }],
      }
    })
    touch()
  }

  function updateTypeLabel(index: number, label: string): void {
    setDraft((prev) =>
      prev
        ? {
            ...prev,
            types: prev.types.map((entry, i) =>
              i === index ? { ...entry, label, labels: setLocaleLabel(entry.labels, locale, label) } : entry,
            ),
          }
        : prev,
    )
    touch()
  }

  function updateStatusLabel(index: number, label: string): void {
    setDraft((prev) =>
      prev
        ? {
            ...prev,
            statuses: prev.statuses.map((entry, i) =>
              i === index ? { ...entry, label, labels: setLocaleLabel(entry.labels, locale, label) } : entry,
            ),
          }
        : prev,
    )
    touch()
  }

  function updateStatusColor(index: number, color: string): void {
    setDraft((prev) =>
      prev
        ? { ...prev, statuses: prev.statuses.map((entry, i) => (i === index ? { ...entry, color } : entry)) }
        : prev,
    )
    touch()
  }

  function removeType(index: number): void {
    setDraft((prev) => (prev ? { ...prev, types: prev.types.filter((_, i) => i !== index) } : prev))
    touch()
  }

  function removeStatus(index: number): void {
    setDraft((prev) => (prev ? { ...prev, statuses: prev.statuses.filter((_, i) => i !== index) } : prev))
    touch()
  }

  function cancelEdits(): void {
    if (optionsQuery.data) setDraft(cloneOptions(optionsQuery.data))
    setDirty(false)
    setNotice(null)
  }

  if (!isAdmin) {
    return (
      <Alert variant="error" title={t('common.error')}>
        {t('errors.forbidden')}
      </Alert>
    )
  }

  if (optionsQuery.isError) {
    return (
      <Alert variant="error" title={t('common.error')}>
        {apiErrorMessage(optionsQuery.error, t)}
      </Alert>
    )
  }

  if (optionsQuery.isPending || !draft) {
    return <LoadingState label={t('common.loading')} className="py-16" />
  }

  return (
    <div className="options-page space-y-4">
      <PageHeader
        title={t('options.title')} description={t('shell.optionsHint')}
        actions={
          <>
            <Button variant="secondary" size="sm" onClick={cancelEdits} disabled={!dirty || save.isPending}>
              {t('common.cancel')}
            </Button>
            <Button variant="primary" size="sm" onClick={() => save.mutate()} loading={save.isPending} disabled={!dirty}>
              {t('common.save')}
            </Button>
          </>
        }
      />

      {notice ? (
        <NoticeMessage
          variant={notice.kind}
          onDismiss={() => setNotice(null)}
          dismissLabel={t('common.close')}
        >
          {notice.message}
        </NoticeMessage>
      ) : null}

      <Card className="options-section space-y-3 p-5">
        <div>
          <h2 className="text-base font-semibold text-content">{t('options.manageTypes')}</h2>
          <p className="text-xs text-content-muted">{t('options.typeHint')}</p>
        </div>

        <ul className="space-y-2">
          {draft.types.map((type, index) => {
            const inUse = usedTypes.has(type.value)
            return (
              <li key={type.value} className="flex items-center gap-2">
                <Field label={t('options.manageTypes')} srOnlyLabel className="flex-1">
                  {({ id, 'aria-describedby': describedBy }) => (
                    <Input
                      id={id}
                      aria-describedby={describedBy}
                      value={type.label}
                      onChange={(event) => updateTypeLabel(index, event.target.value)}
                    />
                  )}
                </Field>
                <IconButton
                  label={inUse ? t('options.inUse') : t('common.delete')}
                  disabled={inUse || usageQuery.isPending || usageQuery.isError}
                  className="text-content-muted hover:bg-danger-bg hover:text-danger-fg"
                  onClick={() => removeType(index)}
                >
                  ×
                </IconButton>
                {inUse ? <span className="text-xs text-content-muted">{t('options.inUse')}</span> : null}
              </li>
            )
          })}
        </ul>

        <Button variant="secondary" size="sm" onClick={addType}>
          + {t('options.add')}
        </Button>
      </Card>

      <Card className="options-section space-y-3 p-5">
        <div>
          <h2 className="text-base font-semibold text-content">{t('options.manageStatuses')}</h2>
          <p className="text-xs text-content-muted">{t('options.statusColor')}</p>
        </div>

        <ul className="space-y-3">
          {draft.statuses.map((status, index) => {
            const inUse = usedStatuses.has(status.value) || status.value.toLowerCase() === "feasibility"
            return (
              <li key={status.value} className="flex flex-wrap items-center gap-3">
                <Field label={t('options.manageStatuses')} srOnlyLabel className="w-full sm:w-auto sm:max-w-xs">
                  {({ id, 'aria-describedby': describedBy }) => (
                    <Input
                      id={id}
                      aria-describedby={describedBy}
                      value={status.label}
                      disabled={["feasibility", "devel", "sales"].includes(status.value.toLowerCase())}
                      onChange={(event) => updateStatusLabel(index, event.target.value)}
                    />
                  )}
                </Field>
                <ColorField
                  value={status.color}
                  onChange={(color) => updateStatusColor(index, color)}
                  label={t('options.color')}
                />
                <IconButton
                  label={inUse ? t('options.inUse') : t('common.delete')}
                  disabled={inUse || usageQuery.isPending || usageQuery.isError}
                  className="text-content-muted hover:bg-danger-bg hover:text-danger-fg"
                  onClick={() => removeStatus(index)}
                >
                  ×
                </IconButton>
                {inUse ? <span className="text-xs text-content-muted">{t('options.inUse')}</span> : null}
              </li>
            )
          })}
        </ul>

        <Button variant="secondary" size="sm" onClick={addStatus}>
          + {t('options.add')}
        </Button>
      </Card>
    </div>
  )
}
