import type { HTMLAttributes } from 'react'
import { cn } from '../../lib/utils/cn'
import { colorContrastTextClass } from '../../lib/utils/colorContrast'

export interface BadgeProps extends Omit<HTMLAttributes<HTMLSpanElement>, 'color'> {
  /** Optional explicit colour (e.g. a status colour from the API). */
  color?: string | null
}

export function Badge({ color, className, style, children, ...props }: BadgeProps) {
  const hasColor = Boolean(color)
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center rounded-full px-2.5 py-0.5 text-xs font-medium',
        hasColor ? colorContrastTextClass(color) : 'bg-panel-subtle text-content',
        className,
      )}
      style={hasColor ? { backgroundColor: color ?? undefined, ...style } : style}
      {...props}
    >
      <span className="min-w-0 break-words">{children}</span>
    </span>
  )
}
