import type { ReactNode } from 'react'
import { Alert, type AlertVariant } from './Alert'
import { Button } from './Button'

interface NoticeBase {
  variant?: AlertVariant
  title?: ReactNode
  className?: string
  children: ReactNode
}

interface DismissibleNotice {
  onDismiss: () => void
  dismissLabel: string
}

interface StaticNotice {
  onDismiss?: undefined
  dismissLabel?: undefined
}

export type NoticeProps = NoticeBase & (DismissibleNotice | StaticNotice)

/**
 * Composes `Alert` with an optional, accessible dismiss button. Notice state
 * stays in the feature screen; `dismissLabel` is required when `onDismiss` is
 * supplied so the control always has an accessible name.
 */
export function Notice({ variant = 'info', title, className, children, onDismiss, dismissLabel }: NoticeProps) {
  return (
    <Alert variant={variant} title={title} className={className}>
      {onDismiss ? (
        <div className="flex min-w-0 items-start justify-between gap-4">
          <span className="min-w-0 break-words">{children}</span>
          <Button
            variant="ghost"
            size="sm"
            className="shrink-0 text-xs underline"
            aria-label={dismissLabel}
            onClick={onDismiss}
          >
            {dismissLabel}
          </Button>
        </div>
      ) : (
        children
      )}
    </Alert>
  )
}
