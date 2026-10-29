import { forwardRef, type SelectHTMLAttributes } from 'react'
import { cn } from '../../lib/utils/cn'

export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  invalid?: boolean
}

export const Select = forwardRef<HTMLSelectElement, SelectProps>(function Select(
  { className, invalid = false, children, ...props },
  ref,
) {
  return (
    <select
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        'sklad-select h-10 w-full min-w-0 rounded-md border bg-panel pl-3 pr-10 text-sm text-content',
        'focus:outline-none focus:ring-2 focus:ring-offset-2',
        invalid
          ? 'border-danger-fg focus:border-danger-fg focus:ring-danger-fg'
          : 'border-stroke-control focus:border-action focus:ring-action',
        className,
      )}
      {...props}
    >
      {children}
    </select>
  )
})
