import type { TFunction } from 'i18next'
import { ApiError } from './errors'

/**
 * Map any thrown error to a localized, user-facing message.
 *
 * The API error envelope carries a machine-readable `code`
 * (`version_conflict`, `validation_failed`, `not_found`, …); unknown errors
 * fall back to a generic server message so nothing internal leaks to the UI.
 */
export function apiErrorMessage(error: unknown, t: TFunction): string {
  if (error instanceof ApiError) {
    switch (error.code) {
      case 'version_conflict':
        return t('errors.versionConflict')
      case 'unauthorized':
        return t('errors.unauthorized')
      case 'forbidden':
        return t('errors.forbidden')
      case 'not_found':
        return t('errors.notFound')
      case 'validation_failed':
        return t('errors.validation')
      case 'rate_limited':
        return t('errors.rateLimited')
      default:
        break
    }

    if (error.isNetworkError) return t('errors.network')
    if (error.isUnauthorized) return t('errors.unauthorized')
    if (error.isForbidden) return t('errors.forbidden')
    if (error.isNotFound) return t('errors.notFound')
    if (error.isValidation) return t('errors.validation')
    if (error.isVersionConflict) return t('errors.versionConflict')
    if (error.isConflict) return t('errors.conflict')
    if (error.isRateLimited) return t('errors.rateLimited')
    return t('errors.server')
  }

  return t('errors.server')
}
