import { forwardRef, type HTMLAttributes, type TableHTMLAttributes, type TdHTMLAttributes, type ThHTMLAttributes } from 'react'
import { cn } from '../../lib/utils/cn'

/**
 * Thin native table wrappers with a contained horizontal scroll container.
 * These are **not** a data-grid abstraction: they forward refs and all native
 * element props (including `aria-sort`) so inline editing and native table
 * structure keep working.
 */

export const TableScroll = forwardRef<HTMLDivElement, HTMLAttributes<HTMLDivElement>>(function TableScroll(
  { className, ...props },
  ref,
) {
  return <div ref={ref} className={cn('relative w-full overflow-x-auto', className)} {...props} />
})

export const Table = forwardRef<HTMLTableElement, TableHTMLAttributes<HTMLTableElement>>(function Table(
  { className, ...props },
  ref,
) {
  return <table ref={ref} className={cn('w-full border-collapse text-sm', className)} {...props} />
})

export const TableHead = forwardRef<HTMLTableSectionElement, HTMLAttributes<HTMLTableSectionElement>>(
  function TableHead({ className, ...props }, ref) {
    return <thead ref={ref} className={cn(className)} {...props} />
  },
)

export const TableHeader = forwardRef<HTMLTableCellElement, ThHTMLAttributes<HTMLTableCellElement>>(
  function TableHeader({ className, ...props }, ref) {
    return (
      <th
        ref={ref}
        className={cn(
          'border-b border-stroke px-3 py-2 text-left text-xs font-semibold uppercase tracking-wide text-content-muted',
          className,
        )}
        {...props}
      />
    )
  },
)

export const TableCell = forwardRef<HTMLTableCellElement, TdHTMLAttributes<HTMLTableCellElement>>(
  function TableCell({ className, ...props }, ref) {
    return <td ref={ref} className={cn('border-b border-stroke px-3 py-2 align-top', className)} {...props} />
  },
)
