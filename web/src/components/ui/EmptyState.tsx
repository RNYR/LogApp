import type { ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'

export interface EmptyStateProps {
  title: ReactNode
  description?: ReactNode
  action?: ReactNode
  className?: string
}

export function EmptyState({ title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-2 py-12 text-center', className)}>
      <p className="text-base font-medium text-content">{title}</p>
      {description ? <p className="max-w-md text-sm text-content-muted">{description}</p> : null}
      {action ? <div className="mt-2">{action}</div> : null}
    </div>
  )
}
