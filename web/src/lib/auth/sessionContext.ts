import { createContext, useContext } from 'react'
import type { User } from '../api/types'

export type SessionStatus = 'loading' | 'authenticated' | 'anonymous'

export interface SessionValue {
  status: SessionStatus
  user: User | null
  /** Authenticate and establish a session (throws `ApiError` on failure). */
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  /** Re-fetch the current user from `GET /auth/me`. */
  refresh: () => Promise<void>
}

export const SessionContext = createContext<SessionValue | null>(null)

export function useSession(): SessionValue {
  const context = useContext(SessionContext)
  if (!context) {
    throw new Error('useSession must be used within a <SessionProvider>')
  }
  return context
}
