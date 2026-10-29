import type { ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  actions?: ReactNode
  className?: string
}

/**
 * Shared page heading row: one `h1`, optional supporting text and wrapping
 * actions. Purely presentational — it owns no state or behaviour.
 */
export function PageHeader({ title, description, actions, className }: PageHeaderProps) {
  return (
    <div className={cn('page-heading flex flex-wrap items-center justify-between gap-3', className)}>
      <div className="min-w-0 max-w-full">
        <h1 className="break-words text-3xl font-medium tracking-tight text-content">{title}</h1>
        {description ? <p className="mt-1 text-sm text-content-muted">{description}</p> : null}
      </div>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
  )
}
