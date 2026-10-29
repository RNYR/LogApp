/**
 * TypeScript mirror of the Sklad REST API contract.
 *
 * These types are derived 1:1 from `openapi/openapi.yaml`
 * (OpenAPI 3.0.3, `/api/v1`). They are the build-safe, hand-synced mirror used
 * by `client.ts`; once dependencies are installed the same shapes can be
 * regenerated with:
 *
 *     npm run gen:api   # openapi-typescript ../openapi/openapi.yaml
 *
 * Keep the two in sync: the OpenAPI document is the source of truth.
 */

export type Role = 'admin' | 'editor'
export type Locale = 'en' | 'cs' | 'it'
export type SortField =
  | 'asset_tag'
  | 'name'
  | 'sn'
  | 'type'
  | 'status'
  | 'sent_date'
  | 'return_date'
  | 'note'
  | 'updated_at'
export type SortOrder = 'asc' | 'desc'
export type ExportFormat = 'json' | 'csv'
export type NotificationTrigger = 'overdue_daily' | 'manual' | 'test'
export type NotificationStatus = 'pending' | 'sent' | 'failed'

// ---------------------------------------------------------------- envelopes --

/** Machine-readable error payload: `{ "error": { code, message, details } }`. */
export interface ErrorResponse {
  error: {
    code: string
    message: string
    details?: string[]
  }
}

export interface OkResponse {
  data: { ok: boolean }
}

export interface Meta {
  page: number
  per_page: number
  total: number
  total_pages: number
}

// -------------------------------------------------------------------- system --

export interface Health {
  status: string
  version: string
  time: string
}

export interface Version {
  version: string
  commit: string
  build_time: string
}

// ---------------------------------------------------------------------- auth --

export interface User {
  id: number
  username: string
  display_name: string
  email: string
  role: Role
  must_change_password: boolean
  active: boolean
  last_login_at: string | null
  created_at: string
  updated_at: string
}

export interface UserResponse {
  data: User
}

export interface UserList {
  data: User[]
}

export interface UserInput {
  username: 'editor'
  password: string
  display_name?: string
  email?: string
  role: 'editor'
}

export interface UserUpdate {
  display_name?: string
  email?: string
  role?: 'admin' | 'editor'
  active?: boolean
  password?: string
}

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  data: {
    user: User
    csrf_token: string
  }
}

export interface ChangePasswordRequest {
  current_password: string
  new_password: string
}

// ------------------------------------------------------------------- options --

export interface LocalizedLabels {
  en?: string
  cs?: string
  it?: string
}

export interface TypeOption {
  value: string
  label: string
  labels?: LocalizedLabels
  sort_order?: number
}

export interface StatusOption {
  value: string
  label: string
  labels?: LocalizedLabels
  color: string
  sort_order?: number
}

export interface Options {
  data: {
    types: TypeOption[]
    statuses: StatusOption[]
  }
}

export interface OptionsUpdate {
  types: TypeOption[]
  statuses: StatusOption[]
  renames?: {
    types?: Record<string, string>
    statuses?: Record<string, string>
  }
}

// --------------------------------------------------------------------- items --

export interface Person { id: number; name: string; active: boolean; is_customer: boolean; version: number }

export interface Item {
  personnel_id?: number | null
  personnel_name?: string | null
  id: number
  asset_tag: string
  name: string
  sn: string
  type: string | null
  type_label: string | null
  status: string | null
  status_label: string | null
  status_color: string | null
  sent_date: string | null
  return_date: string | null
  note: string
  version: number
  archived: boolean
  label_printed_at: string | null
  created_at: string
  updated_at: string
  created_by: string | null
  updated_by: string | null
}

export interface ItemResponse {
  data: Item
}

export interface ItemList {
  data: Item[]
  meta?: Meta
}

export interface ItemInput {
  personnel_id?: number
  asset_tag?: string
  name?: string
  sn?: string
  type?: string
  status?: string
  sent_date?: string | null
  return_date?: string | null
  note?: string
}

/** Full replace (`PUT /items/{itemId}`). `version` is required. */
export interface ItemUpdate {
  personnel_id?: number
  version: number
  name?: string
  sn?: string
  type?: string
  status?: string
  sent_date?: string | null
  return_date?: string | null
  note?: string
}

/** Partial update (`PATCH /items/{itemId}`). `version` is required. */
export type ItemPatch = ItemUpdate

export type ItemListParams = {
  q?: string
  type?: string
  status?: string
  archived?: boolean
  overdue?: boolean
  sort?: SortField
  order?: SortOrder
  page?: number
  per_page?: number
}

export interface ResolveRequest {
  codes: string[]
}

export interface ResolveResponse {
  data: {
    matched: Item[]
    unknown: string[]
  }
}

export interface BulkItemRef {
  asset_tag: string
  version: number
}

export interface BulkPatch {
  personnel_id?: number
  status?: string
  note?: string
  sent_date?: string
  return_date?: string
  clear_sent_date?: boolean
  clear_return_date?: boolean
}

export interface BulkRequest {
  items: BulkItemRef[]
  patch: BulkPatch
}

export interface BulkResponse {
  data: {
    updated: number
    updated_items: Item[]
  }
}

// ------------------------------------------------------------------ settings --

export interface Settings {
  data: {
    discord: {
      webhook_set: boolean
      webhook_preview: string
      enabled: boolean
    }
    scheduler: {
      check_hour: number
      days_ahead: number
    }
    locale: {
      default: Locale
    }
    general: {
      organization_name: string
    }
  }
}

export interface SettingsUpdate {
  discord?: {
    /** Empty string keeps the existing webhook. */
    webhook_url?: string
    /** Remove the stored webhook (additive contract field). */
    clear_webhook?: boolean
    enabled?: boolean
  }
  scheduler?: {
    check_hour?: number
    days_ahead?: number
  }
  locale?: {
    default?: Locale
  }
  general?: {
    organization_name?: string
  }
}

// ------------------------------------------------------------- notifications --

export interface NotificationDelivery {
  id: number
  channel_id: number
  trigger: NotificationTrigger
  status: NotificationStatus
  item_count: number
  error: string | null
  created_at: string
  sent_at: string | null
}

export interface DeliveryList {
  data: NotificationDelivery[]
  meta?: Meta
}

export interface NotifyRunResponse {
  data: {
    due_count: number
    sent: boolean
    delivery_id: number | null
  }
}
