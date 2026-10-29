import { forwardRef, type InputHTMLAttributes } from 'react'
import { cn } from '../../lib/utils/cn'

export interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  invalid?: boolean
}

export const Input = forwardRef<HTMLInputElement, InputProps>(function Input(
  { className, invalid = false, ...props },
  ref,
) {
  return (
    <input
      ref={ref}
      aria-invalid={invalid || undefined}
      className={cn(
        'h-10 w-full rounded-md border bg-panel px-3 text-sm text-content',
        'placeholder:text-content-muted focus:outline-none focus:ring-2 focus:ring-offset-2',
        invalid
          ? 'border-danger-fg focus:border-danger-fg focus:ring-danger-fg'
          : 'border-stroke-control focus:border-action focus:ring-action',
        className,
      )}
      {...props}
    />
  )
})
