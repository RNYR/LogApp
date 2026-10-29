import { useId, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Link, Navigate, useLocation } from 'react-router-dom'
import { ApiError } from '../../lib/api'
import { useSession } from '../../lib/auth/sessionContext'
import { LanguageSwitcher } from '../../lib/i18n/LanguageSwitcher'
import { Alert, Button, Card, Field, FullPageSpinner, Input } from '../../components/ui'

interface LoginLocationState {
  from?: string
}

/**
 * Public login screen.
 *
 * On success `SessionProvider` flips to `authenticated`; this component then
 * redirects to the page the user originally requested (or `/`). A user flagged
 * `must_change_password` is redirected by `RequireAuth` after login.
 */
export function Login() {
  const { t } = useTranslation()
  const { status, login } = useSession()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const errorId = useId()
  const errorDescribedBy = error ? errorId : undefined

  if (status === 'loading') return <FullPageSpinner />

  if (status === 'authenticated') {
    const from = (location.state as LoginLocationState | null)?.from
    return <Navigate to={from && from !== '/login' ? from : '/'} replace />
  }

  async function onSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(username.trim(), password)
    } catch (cause) {
      if (cause instanceof ApiError && cause.isRateLimited) setError(t('errors.rateLimited'))
      else if (cause instanceof ApiError && cause.isUnauthorized) setError(t('auth.invalidCredentials'))
      else setError(t('errors.server'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4 bg-canvas px-4">
      <div className="flex w-full max-w-sm justify-end">
        <LanguageSwitcher />
      </div>

      <Card className="w-full max-w-sm animate-fade-in">
        <div className="mb-6 text-center">
          <h1 className="text-2xl font-semibold text-content">{t('app.name')}</h1>
          <p className="mt-1 text-sm text-content-muted">{t('auth.editorAdminOnly')}</p>
        </div>

        <form className="grid gap-4" onSubmit={(event) => void onSubmit(event)}>
          <Field label={t('auth.username')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={[describedBy, errorDescribedBy].filter(Boolean).join(' ') || undefined}
                name="username"
                autoComplete="username"
                autoFocus
                required
                value={username}
                onChange={(event) => setUsername(event.target.value)}
              />
            )}
          </Field>

          <Field label={t('auth.password')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <Input
                id={id}
                aria-describedby={[describedBy, errorDescribedBy].filter(Boolean).join(' ') || undefined}
                type="password"
                name="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(event) => setPassword(event.target.value)}
              />
            )}
          </Field>

          {error ? (
            <Alert variant="error" id={errorId}>
              {error}
            </Alert>
          ) : null}

          <Button type="submit" loading={submitting} className="w-full">
            {t('auth.signIn')}
          </Button>
        </form>
      </Card>

      <Link to="/" className="text-sm text-content-muted underline underline-offset-4">{t('auth.continueViewing')}</Link>
    </div>
  )
}
