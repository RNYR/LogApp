import type { HTMLAttributes, ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'

export type AlertVariant = 'info' | 'success' | 'warning' | 'error'

export interface AlertProps extends Omit<HTMLAttributes<HTMLDivElement>, 'title'> {
  variant?: AlertVariant
  title?: ReactNode
}

const VARIANT_CLASSES: Record<AlertVariant, string> = {
  info: 'border-stroke bg-info-bg text-info-fg',
  success: 'border-stroke bg-success-bg text-success-fg',
  warning: 'border-stroke bg-warning-bg text-warning-fg',
  error: 'border-stroke bg-danger-bg text-danger-fg',
}

export function Alert({ variant = 'info', title, className, children, ...props }: AlertProps) {
  return (
    <div
      role={variant === 'error' ? 'alert' : 'status'}
      className={cn('rounded-lg border p-3 text-sm', VARIANT_CLASSES[variant], className)}
      {...props}
    >
      {title ? <p className="break-words font-semibold">{title}</p> : null}
      {children ? <div className={cn('min-w-0 break-words', title ? 'mt-1' : undefined)}>{children}</div> : null}
    </div>
  )
}
