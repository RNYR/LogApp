import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { api, onUnauthorized } from '../api'
import type { User } from '../api/types'
import { SessionContext, type SessionStatus, type SessionValue } from './sessionContext'

/**
 * Restores the session on mount (`GET /auth/me`), exposes login/logout, and
 * flips to `anonymous` whenever the API returns `401`.
 */
export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient()
  const [status, setStatus] = useState<SessionStatus>('loading')
  const [user, setUser] = useState<User | null>(null)

  const refresh = useCallback(async () => {
    try {
      const current = await api.auth.me()
      setUser(current)
      setStatus('authenticated')
    } catch {
      setUser(null)
      setStatus('anonymous')
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => onUnauthorized(() => {
    setUser(null)
    setStatus('anonymous')
  }), [])

  const login = useCallback(async (username: string, password: string) => {
    const authenticated = await api.auth.login({ username, password })
    await queryClient.cancelQueries()
    queryClient.clear()
    setUser(authenticated.user)
    setStatus('authenticated')
  }, [queryClient])

  const logout = useCallback(async () => {
    try {
      await api.auth.logout()
    } catch {
      // A failed logout must still clear the local session.
    } finally {
      await queryClient.cancelQueries()
      queryClient.clear()
      setUser(null)
      setStatus('anonymous')
    }
  }, [queryClient])

  const value = useMemo<SessionValue>(
    () => ({ status, user, login, logout, refresh }),
    [status, user, login, logout, refresh],
  )

  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>
}
