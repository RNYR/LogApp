/**
 * Typed API client for the Sklad backend (`/api/v1`).
 *
 * Request types mirror `openapi/openapi.yaml` (see `./types.ts`). The client:
 *   - sends the session cookie (`credentials: same-origin`),
 *   - attaches the double-submit CSRF header on every mutation,
 *   - unwraps the `{ data }` envelope for single resources,
 *   - throws `ApiError` (with the API `code`) for any non-2xx response,
 *   - notifies subscribers on `401` so the session layer can log out.
 */

import { ApiError } from './errors'
import type {
  Person,
  BulkRequest,
  BulkResponse,
  ChangePasswordRequest,
  DeliveryList,
  ExportFormat,
  Health,
  Item,
  ItemInput,
  ItemList,
  ItemListParams,
  ItemPatch,
  ItemResponse,
  ItemUpdate,
  LoginRequest,
  LoginResponse,
  NotifyRunResponse,
  OkResponse,
  Options,
  OptionsUpdate,
  ResolveRequest,
  ResolveResponse,
  Settings,
  SettingsUpdate,
  User,
  UserInput,
  UserList,
  UserResponse,
  UserUpdate,
  Version,
} from './types'

/** Same-origin API base. Override in tests/dev with `VITE_API_BASE_URL`. */
export const API_BASE_URL: string = import.meta.env.VITE_API_BASE_URL ?? '/api/v1'

/** Double-submit CSRF cookie name (see `docs/contracts/config.md`). */
export const CSRF_COOKIE_NAME = 'sklad_csrf'

// ---------------------------------------------------------------------------
// Session events
// ---------------------------------------------------------------------------

type UnauthorizedHandler = () => void

const unauthorizedHandlers = new Set<UnauthorizedHandler>()

/**
 * Subscribe to `401 Unauthorized` responses. Returns an unsubscribe function
 * suitable for use in a React `useEffect` cleanup.
 */
export function onUnauthorized(handler: UnauthorizedHandler): () => void {
  unauthorizedHandlers.add(handler)
  return () => {
    unauthorizedHandlers.delete(handler)
  }
}

function emitUnauthorized(): void {
  for (const handler of unauthorizedHandlers) {
    try {
      handler()
    } catch {
      // A misbehaving subscriber must not break the request pipeline.
    }
  }
}

// ---------------------------------------------------------------------------
// CSRF token
// ---------------------------------------------------------------------------

let inMemoryCsrfToken: string | null = null

/** Store the CSRF token returned by `POST /auth/login`. */
export function setCsrfToken(token: string | null): void {
  inMemoryCsrfToken = token
}

function readCookie(name: string): string | null {
  if (typeof document === 'undefined') return null
  const prefix = `${name}=`
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim()
    if (trimmed.startsWith(prefix)) {
      return decodeURIComponent(trimmed.slice(prefix.length))
    }
  }
  return null
}

function getCsrfToken(): string | null {
  return readCookie(CSRF_COOKIE_NAME) ?? inMemoryCsrfToken
}

// ---------------------------------------------------------------------------
// Request core
// ---------------------------------------------------------------------------

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  query?: Record<string, unknown>
  body?: unknown
  signal?: AbortSignal
  headers?: Record<string, string>
}

function buildUrl(path: string, query?: Record<string, unknown>): string {
  const url = `${API_BASE_URL}${path}`
  if (!query) return url
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const qs = params.toString()
  return qs ? `${url}?${qs}` : url
}

function parseJson(text: string): unknown {
  try {
    return JSON.parse(text) as unknown
  } catch {
    return undefined
  }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const method = options.method ?? 'GET'
  const isMutation = method !== 'GET'

  const headers: Record<string, string> = { Accept: 'application/json', ...options.headers }
  if (options.body !== undefined) headers['Content-Type'] = 'application/json'
  if (isMutation) {
    const token = getCsrfToken()
    if (token) headers['X-CSRF-Token'] = token
  }

  let response: Response
  try {
    response = await fetch(buildUrl(path, options.query), {
      method,
      headers,
      credentials: 'same-origin',
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal,
    })
  } catch (cause) {
    throw new ApiError(0, 'network_error', 'Network error', undefined, cause)
  }

  if (response.status === 204) {
    return undefined as unknown as T
  }

  const text = await response.text()
  const payload = text ? parseJson(text) : undefined

  if (!response.ok) {
    if (response.status === 401) emitUnauthorized()
    throw ApiError.fromResponse(response.status, payload, response.statusText)
  }

  return payload as T
}

// ---------------------------------------------------------------------------
// Endpoint groups
// ---------------------------------------------------------------------------

export const api = {
 personnel: {
 list: async (): Promise<Person[]> => (await request<{data:Person[]}>('/personnel')).data,
 create: async (body: {name:string}): Promise<Person> => (await request<{data:Person}>('/personnel',{method:'POST',body})).data,
 update: async (id:number, body:{name?:string;active?:boolean;version:number}): Promise<Person> => (await request<{data:Person}>(`/personnel/${id}`,{method:'PATCH',body})).data,
 },
  system: {
    health: (signal?: AbortSignal) => request<Health>('/health', { signal }),
    version: (signal?: AbortSignal) => request<Version>('/version', { signal }),
  },

  auth: {
    /** Authenticate; also stores the returned CSRF token for later mutations. */
    async login(body: LoginRequest): Promise<LoginResponse['data']> {
      const res = await request<LoginResponse>('/auth/login', { method: 'POST', body })
      setCsrfToken(res.data.csrf_token)
      return res.data
    },
    logout: () => request<void>('/auth/logout', { method: 'POST' }),
    me: async (signal?: AbortSignal): Promise<User> => {
      const res = await request<UserResponse>('/auth/me', { signal })
      return res.data
    },
    changePassword: (body: ChangePasswordRequest) =>
      request<OkResponse>('/auth/change-password', { method: 'POST', body }),
  },

  users: {
    list: async (signal?: AbortSignal): Promise<User[]> => {
      const res = await request<UserList>('/users', { signal })
      return res.data
    },
    create: async (body: UserInput): Promise<User> => {
      const res = await request<UserResponse>('/users', { method: 'POST', body })
      return res.data
    },
    update: async (userId: number, body: UserUpdate): Promise<User> => {
      const res = await request<UserResponse>(`/users/${userId}`, { method: 'PATCH', body })
      return res.data
    },
    deactivate: (userId: number) =>
      request<OkResponse>(`/users/${userId}`, { method: 'DELETE' }),
  },

  options: {
    get: async (lang?: string, signal?: AbortSignal): Promise<Options['data']> => {
      const res = await request<Options>('/options', { query: { lang }, signal })
      return res.data
    },
    update: async (body: OptionsUpdate): Promise<Options['data']> => {
      const res = await request<Options>('/options', { method: 'PUT', body })
      return res.data
    },
  },

  items: {
    list: (params: ItemListParams = {}, signal?: AbortSignal) =>
      request<ItemList>('/items', { query: params as Record<string, unknown>, signal }),
    get: async (itemId: number, signal?: AbortSignal): Promise<Item> => {
      const res = await request<ItemResponse>(`/items/${itemId}`, { signal })
      return res.data
    },
    create: async (body: ItemInput): Promise<Item> => {
      const res = await request<ItemResponse>('/items', { method: 'POST', body })
      return res.data
    },
    /** Full replace (all fields); requires the current `version`. */
    replace: async (itemId: number, body: ItemUpdate): Promise<Item> => {
      const res = await request<ItemResponse>(`/items/${itemId}`, { method: 'PUT', body })
      return res.data
    },
    /** Partial update; requires the current `version`. */
    patch: async (itemId: number, body: ItemPatch): Promise<Item> => {
      const res = await request<ItemResponse>(`/items/${itemId}`, { method: 'PATCH', body })
      return res.data
    },
    archive: (itemId: number) => request<OkResponse>(`/items/${itemId}`, { method: 'DELETE' }),
    resolve: async (codes: string[]): Promise<ResolveResponse['data']> => {
      const body: ResolveRequest = { codes }
      const res = await request<ResolveResponse>('/items/resolve', { method: 'POST', body })
      return res.data
    },
    bulk: async (body: BulkRequest): Promise<BulkResponse['data']> => {
      const res = await request<BulkResponse>('/items/bulk', { method: 'POST', body })
      return res.data
    },
  },

  export: {
    /** Build a download URL (use with an `<a>` or `window.location.assign`). */
    url: (format: ExportFormat = 'json', archived = false): string =>
      buildUrl('/export', { format, archived }),
    download: async (format: ExportFormat = 'json', archived = false): Promise<Blob> => {
      const response = await fetch(buildUrl('/export', { format, archived }), {
        credentials: 'same-origin',
        headers: { Accept: format === 'csv' ? 'text/csv' : 'application/json' },
      })
      if (!response.ok) {
        const text = await response.text()
        if (response.status === 401) emitUnauthorized()
        throw ApiError.fromResponse(response.status, text ? parseJson(text) : undefined, response.statusText)
      }
      return response.blob()
    },
  },

  settings: {
    get: async (signal?: AbortSignal): Promise<Settings['data']> => {
      const res = await request<Settings>('/settings', { signal })
      return res.data
    },
    update: async (body: SettingsUpdate): Promise<Settings['data']> => {
      const res = await request<Settings>('/settings', { method: 'PUT', body })
      return res.data
    },
  },

  notifications: {
    list: (page = 1, perPage = 20, signal?: AbortSignal) =>
      request<DeliveryList>('/notifications', { query: { page, per_page: perPage }, signal }),
    test: () => request<OkResponse>('/notifications/test', { method: 'POST' }),
    runNow: () => request<NotifyRunResponse>('/notifications/run-now', { method: 'POST' }),
  },
} as const

export type Api = typeof api
