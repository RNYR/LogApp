import { useId, type ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'

export interface FieldRenderProps {
  id: string
  'aria-describedby'?: string
}

export interface FieldProps {
  label: ReactNode
  /** Explicit control id; a generated id is used when omitted. */
  id?: string
  hint?: ReactNode
  error?: ReactNode
  className?: string
  /** Visually hide the label while keeping it associated (dense tables). */
  srOnlyLabel?: boolean
  children: (props: FieldRenderProps) => ReactNode
}

/**
 * Label/control association plus hint/error descriptions. Owns no form state:
 * the caller renders the control and spreads the provided props onto it.
 */
export function Field({ label, id, hint, error, className, srOnlyLabel = false, children }: FieldProps) {
  const generatedId = useId()
  const controlId = id ?? generatedId
  const hintId = hint ? `${controlId}-hint` : undefined
  const errorId = error ? `${controlId}-error` : undefined
  const describedBy = [hintId, errorId].filter(Boolean).join(' ') || undefined

  return (
    <div className={cn('grid gap-1', className)}>
      <label
        htmlFor={controlId}
        className={cn(
          'text-xs font-medium text-content-muted',
          srOnlyLabel && 'sr-only',
        )}
      >
        {label}
      </label>
      {children({ id: controlId, 'aria-describedby': describedBy })}
      {hint ? (
        <p id={hintId} className="text-xs text-content-muted">
          {hint}
        </p>
      ) : null}
      {error ? (
        <p id={errorId} className="text-xs text-danger-fg">
          {error}
        </p>
      ) : null}
    </div>
  )
}
