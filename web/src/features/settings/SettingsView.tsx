import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { api, type SettingsUpdate } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { useSession } from '../../lib/auth/sessionContext'
import { type Locale } from '../../lib/i18n'
import { Alert, Badge, Button, Card, Field, Input, LoadingState, Notice as NoticeMessage, PageHeader, Select, Table, TableCell, TableHead, TableHeader, TableScroll } from '../../components/ui'

type Notice = { kind: 'success' | 'error'; message: string } | null

function fmtDateTime(value: string | null): string {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

/**
 * Admin settings: Discord webhook, overdue scheduler, default locale, and
 * organisation name — plus the notification test/run-now actions and the
 * delivery log. All mutation endpoints are admin-only server-side.
 */
export function SettingsView() {
  const { t } = useTranslation()
  const { user } = useSession()
  const queryClient = useQueryClient()
  const isAdmin = user?.role === 'admin'

  const [webhook, setWebhook] = useState('')
  const [enabled, setEnabled] = useState(false)
  const [checkHour, setCheckHour] = useState(11)
  const [daysAhead, setDaysAhead] = useState(0)
  const [localeDefault, setLocaleDefault] = useState<Locale>('en')
  const [orgName, setOrgName] = useState('')
  const [dirty, setDirty] = useState(false)
  const [notice, setNotice] = useState<Notice>(null)

  const settingsQuery = useQuery({
    queryKey: ['settings'],
    queryFn: ({ signal }) => api.settings.get(signal),
    enabled: isAdmin,
  })

  const deliveriesQuery = useQuery({
    queryKey: ['notifications'],
    queryFn: ({ signal }) => api.notifications.list(1, 25, signal),
    enabled: isAdmin,
  })

  useEffect(() => {
    const data = settingsQuery.data
    if (!data || dirty) return
    setWebhook('')
    setEnabled(data.discord.enabled)
    setCheckHour(data.scheduler.check_hour)
    setDaysAhead(data.scheduler.days_ahead)
    setLocaleDefault(data.locale.default)
    setOrgName(data.general.organization_name)
  }, [settingsQuery.data, dirty])

  function touch(): void {
    setDirty(true)
    setNotice(null)
  }

  const save = useMutation({
    mutationFn: () => {
      const body: SettingsUpdate = {
        discord: { enabled },
        scheduler: { check_hour: checkHour, days_ahead: daysAhead },
        locale: { default: localeDefault },
        general: { organization_name: orgName },
      }
      const url = webhook.trim()
      if (url) body.discord!.webhook_url = url
      return api.settings.update(body)
    },
    onSuccess: () => {
      setDirty(false)
      setWebhook('')
      setNotice({ kind: 'success', message: t('settings.saved') })
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  const clearWebhook = useMutation({
    mutationFn: () => api.settings.update({ discord: { clear_webhook: true } }),
    onSuccess: () => {
      setWebhook('')
      setNotice({ kind: 'success', message: t('settings.saved') })
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  const testWebhook = useMutation({
    mutationFn: () => api.notifications.test(),
    onSuccess: () => setNotice({ kind: 'success', message: t('settings.testSent') }),
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  const runNow = useMutation({
    mutationFn: () => api.notifications.runNow(),
    onSuccess: () => {
      setNotice({ kind: 'success', message: t('settings.notifyNowDone') })
      void queryClient.invalidateQueries({ queryKey: ['notifications'] })
    },
    onError: (error) => setNotice({ kind: 'error', message: apiErrorMessage(error, t) }),
  })

  if (!isAdmin) {
    return (
      <Alert variant="error" title={t('common.error')}>
        {t('errors.forbidden')}
      </Alert>
    )
  }

  if (settingsQuery.isError) {
    return (
      <Alert variant="error" title={t('common.error')}>
        {apiErrorMessage(settingsQuery.error, t)}
      </Alert>
    )
  }

  if (settingsQuery.isPending) {
    return <LoadingState label={t('common.loading')} className="py-16" />
  }

  const data = settingsQuery.data
  const webhookSet = data?.discord.webhook_set ?? false

  return (
    <div className="space-y-4">
      <PageHeader
        title={t('settings.title')} description={t('shell.settingsHint')}
        actions={
          <Button variant="primary" size="sm" onClick={() => save.mutate()} loading={save.isPending} disabled={!dirty}>
            {t('settings.save')}
          </Button>
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

      <Card className="space-y-4 p-4">
        <div>
          <h2 className="text-base font-semibold text-content">{t('settings.discord')}</h2>
          <p className="text-xs text-content-muted">{t('settings.webhookHint')}</p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Input
            type="url"
            className="max-w-xl"
            value={webhook}
            onChange={(event) => {
              setWebhook(event.target.value)
              touch()
            }}
            placeholder={
              webhookSet && data ? data.discord.webhook_preview : 'https://discord.com/api/webhooks/…'
            }
            aria-label={t('settings.webhook')}
          />
          <Badge className={webhookSet ? '!bg-success-bg !text-success-fg' : '!bg-danger-bg !text-danger-fg'}>
            {webhookSet ? t('settings.webhookSet') : t('settings.webhookNotSet')}
          </Badge>
        </div>

        <label className="flex items-center gap-2 text-sm text-content">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(event) => {
              setEnabled(event.target.checked)
              touch()
            }}
            className="h-4 w-4 accent-action"
          />
          {t('settings.enabled')}
        </label>

        <div className="flex flex-wrap items-center gap-2">
          <Button variant="secondary" size="sm" onClick={() => testWebhook.mutate()} loading={testWebhook.isPending}>
            {t('settings.test')}
          </Button>
          <Button variant="secondary" size="sm" onClick={() => runNow.mutate()} loading={runNow.isPending}>
            {t('settings.notifyNow')}
          </Button>
          {webhookSet ? (
            <Button variant="danger" size="sm" onClick={() => clearWebhook.mutate()} loading={clearWebhook.isPending}>
              {t('common.remove')}
            </Button>
          ) : null}
        </div>
      </Card>

      <Card className="space-y-4 p-4">
        <h2 className="text-base font-semibold text-content">{t('settings.notifications')}</h2>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.checkHour')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                type="number"
                min={0}
                max={23}
                value={checkHour}
                onChange={(event) => {
                  setCheckHour(Number(event.target.value))
                  touch()
                }}
              />
            )}
          </Field>
          <Field label={t('settings.daysAhead')} hint={t('settings.daysAheadHint')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                type="number"
                min={0}
                max={3650}
                value={daysAhead}
                onChange={(event) => {
                  setDaysAhead(Number(event.target.value))
                  touch()
                }}
              />
            )}
          </Field>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <Field label={t('settings.language')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Select
                id={id}
                aria-describedby={describedBy}
                value={localeDefault}
                onChange={(event) => {
                  setLocaleDefault(event.target.value as Locale)
                  touch()
                }}
              >
                <option value="en">{t('languages.en')}</option>
                <option value="cs">{t('languages.cs')}</option>
                <option value="it">{t('languages.it')}</option>
              </Select>
            )}
          </Field>
          <Field label={t('settings.organizationName')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={describedBy}
                value={orgName}
                onChange={(event) => {
                  setOrgName(event.target.value)
                  touch()
                }}
              />
            )}
          </Field>
        </div>
      </Card>

      <Card className="space-y-3 p-4">
        <h2 className="text-base font-semibold text-content">{t('settings.deliveryLog')}</h2>
        {deliveriesQuery.isPending ? (
          <LoadingState label={t('common.loading')} className="justify-start py-2" />
        ) : (deliveriesQuery.data?.data ?? []).length === 0 ? (
          <p className="text-sm text-content-muted">{t('common.noEntries')}</p>
        ) : (
          <TableScroll>
            <Table className="text-left">
              <TableHead>
                <tr>
                  <TableHeader>{t('settings.notifications')}</TableHeader>
                  <TableHeader>{t('common.status')}</TableHeader>
                  <TableHeader>{t('common.note')}</TableHeader>
                  <TableHeader>{t('common.date')}</TableHeader>
                </tr>
              </TableHead>
              <tbody>
                {(deliveriesQuery.data?.data ?? []).map((delivery) => (
                  <tr key={delivery.id}>
                    <TableCell>{delivery.trigger}</TableCell>
                    <TableCell>
                      <Badge
                        className={
                          delivery.status === 'sent'
                            ? '!bg-success-bg !text-success-fg'
                            : delivery.status === 'failed'
                              ? '!bg-danger-bg !text-danger-fg'
                              : '!bg-warning-bg !text-warning-fg'
                        }
                      >
                        {delivery.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="max-w-xs">
                      {delivery.error ?? (delivery.item_count > 0 ? `${delivery.item_count}` : '—')}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">{fmtDateTime(delivery.created_at)}</TableCell>
                  </tr>
                ))}
              </tbody>
            </Table>
          </TableScroll>
        )}
      </Card>
    </div>
  )
}
