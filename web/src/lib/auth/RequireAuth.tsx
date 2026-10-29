import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { FullPageSpinner } from '../../components/ui'
import { useSession } from './sessionContext'

/**
 * Route guard for the authenticated app shell.
 *
 * - while the session is being restored it renders a full-page spinner,
 * - anonymous users may browse public pages; protected routes redirect to `/login`,
 * - users flagged `must_change_password` are forced to `/change-password`.
 */
export function RequireAuth({ children }: { children: ReactNode }) {
  const { status, user } = useSession()
  const location = useLocation()

  if (status === 'loading') return <FullPageSpinner />

  const publicPaths = ['/', '/scan', '/about']
  if (status === 'anonymous' && !publicPaths.includes(location.pathname)) {
    return <Navigate to="/login" replace state={{ from: `${location.pathname}${location.search}` }} />
  }

  if (location.pathname === '/audit' && user?.role !== 'admin') return <Navigate to="/" replace />

  if (!publicPaths.includes(location.pathname) && user?.role !== 'admin') return <Navigate to="/login" replace />

  if (user?.must_change_password && location.pathname !== '/change-password') {
    return <Navigate to="/change-password" replace />
  }

  return <>{children}</>
}
