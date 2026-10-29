import { useId, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router-dom'
import { api, ApiError } from '../../lib/api'
import { useSession } from '../../lib/auth/sessionContext'
import { Alert, Button, Card, Field, Input, PageHeader } from '../../components/ui'

export function ChangePasswordPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { user, refresh } = useSession()

  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const errorId = useId()
  const errorDescribedBy = error ? errorId : undefined

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)

    if (newPassword.length < 8) {
      setError(t('auth.passwordTooShort'))
      return
    }
    if (newPassword !== confirmPassword) {
      setError(t('auth.passwordMismatch'))
      return
    }

    setSubmitting(true)
    try {
      await api.auth.changePassword({ current_password: currentPassword, new_password: newPassword })
      await refresh()
      navigate('/', { replace: true })
    } catch (cause) {
      if (cause instanceof ApiError && cause.isValidation) setError(t('errors.validation'))
      else if (cause instanceof ApiError && cause.isUnauthorized) setError(t('auth.invalidCredentials'))
      else setError(t('errors.server'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="mx-auto max-w-md">
      <Card className="animate-fade-in">
        <PageHeader title={t('auth.changePassword')} />

        {user?.must_change_password ? (
          <Alert variant="warning" className="mt-4">
            {t('auth.mustChangePassword')}
          </Alert>
        ) : null}

        <form className="mt-5 grid gap-4" onSubmit={(event) => void onSubmit(event)}>
          <Field label={t('auth.currentPassword')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={[describedBy, errorDescribedBy].filter(Boolean).join(' ') || undefined}
                type="password"
                autoComplete="current-password"
                required
                value={currentPassword}
                onChange={(event) => setCurrentPassword(event.target.value)}
              />
            )}
          </Field>

          <Field label={t('auth.newPassword')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={[describedBy, errorDescribedBy].filter(Boolean).join(' ') || undefined}
                type="password"
                autoComplete="new-password"
                required
                minLength={8}
                value={newPassword}
                onChange={(event) => setNewPassword(event.target.value)}
              />
            )}
          </Field>

          <Field label={t('auth.confirmPassword')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={[describedBy, errorDescribedBy].filter(Boolean).join(' ') || undefined}
                type="password"
                autoComplete="new-password"
                required
                value={confirmPassword}
                onChange={(event) => setConfirmPassword(event.target.value)}
              />
            )}
          </Field>

          {error ? (
            <Alert variant="error" id={errorId}>
              {error}
            </Alert>
          ) : null}

          <Button type="submit" loading={submitting}>
            {t('common.save')}
          </Button>
        </form>
      </Card>
    </div>
  )
}
