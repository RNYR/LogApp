/** Minimal class-name joiner (no runtime dependencies). */
export type ClassValue = string | number | null | false | undefined

export function cn(...values: ClassValue[]): string {
  return values.filter((value): value is string => typeof value === 'string' && value.length > 0).join(' ')
}
