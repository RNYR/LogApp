import { forwardRef, type ReactNode } from 'react'
import { cn } from '../../lib/utils/cn'
import { Button, type ButtonProps } from './Button'

export interface IconButtonProps extends Omit<ButtonProps, 'children' | 'aria-label' | 'size'> {
  /** Required accessible name (also the default `title`). */
  label: string
  children: ReactNode
}

/**
 * Glyph action composes `Button` and enforces an accessible name plus a
 * >=44px hit area. Glyph children are decorative; the label provides the name.
 *
 * The `!` important modifiers are required because `cn` only joins classes and
 * `Button`'s size utilities (`h-8`, `px-3`) would otherwise win on specificity
 * ties. The action-colored outline is restored explicitly because `Button`
 * suppresses the global focus outline in favour of a ring.
 */
export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { label, title, variant = 'ghost', className, children, ...props },
  ref,
) {
  return (
    <Button
      ref={ref}
      variant={variant}
      aria-label={label}
      title={title ?? label}
      className={cn(
        '!h-11 !w-11 !p-0 shrink-0',
        'focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-action',
        className,
      )}
      {...props}
    >
      <span aria-hidden="true">{children}</span>
    </Button>
  )
})
