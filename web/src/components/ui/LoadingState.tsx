import type { ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'
import { Spinner } from './Spinner'

export interface LoadingStateProps {
  /** Caller-supplied, translated status text (announced politely). */
  label: ReactNode
  className?: string
}

/**
 * Inline loading indicator with visible status text. `className` controls
 * placement (compact inline vs. centered block).
 */
export function LoadingState({ label, className }: LoadingStateProps) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn('flex items-center justify-center gap-2 py-6 text-sm text-content-muted', className)}
    >
      <Spinner className="h-5 w-5 text-action" />
      <span>{label}</span>
    </div>
  )
}
