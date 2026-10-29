import type { ErrorResponse } from './types'

/**
 * Error thrown by every failed API call.
 *
 * `code` is the machine-readable code from the API error envelope
 * (e.g. `version_conflict`, `validation_failed`, `not_found`); use the
 * `is*` getters to branch without string matching in callers.
 */
export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly details?: string[]
  readonly body?: unknown

  constructor(
    status: number,
    code: string,
    message: string,
    details?: string[],
    body?: unknown,
  ) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.details = details
    this.body = body
  }

  static fromResponse(status: number, body: unknown, statusText: string): ApiError {
    const error = (body as ErrorResponse | undefined)?.error
    if (error && typeof error.code === 'string') {
      return new ApiError(status, error.code, error.message || statusText, error.details, body)
    }
    return new ApiError(status, 'http_error', statusText || `HTTP ${status}`, undefined, body)
  }

  get isNetworkError(): boolean {
    return this.status === 0
  }

  get isUnauthorized(): boolean {
    return this.status === 401
  }

  get isForbidden(): boolean {
    return this.status === 403
  }

  get isNotFound(): boolean {
    return this.status === 404
  }

  get isValidation(): boolean {
    return this.status === 400
  }

  get isConflict(): boolean {
    return this.status === 409
  }

  get isVersionConflict(): boolean {
    return this.status === 409 && this.code === 'version_conflict'
  }

  get isRateLimited(): boolean {
    return this.status === 429
  }
}
