/**
 * The single place the frontend talks HTTP. Everything goes through request(),
 * so unwrapping the envelope and turning a failure into an ApiError happens
 * once rather than in every component.
 *
 * The backend sends no CORS headers, so the API is always same-origin: nginx
 * proxies /api in the container, the Vite dev server does it locally.
 */

import type { Envelope, ErrorEnvelope, ListMeta } from '@/types/api'

export const API_BASE = '/api/v1'

/**
 * A failed request. The backend's stable error code is kept as-is so callers
 * can react to it; the message is already user-facing text.
 */
export class ApiError extends Error {
  readonly code: string
  readonly status: number
  readonly requestId?: string

  constructor(options: {
    code: string
    message: string
    status: number
    requestId?: string
  }) {
    super(options.message)
    this.name = 'ApiError'
    this.code = options.code
    this.status = options.status
    this.requestId = options.requestId
  }

  /** True when the resource simply does not exist. */
  get isNotFound(): boolean {
    return this.status === 404
  }

  /** True when retrying the same request could plausibly succeed. */
  get isRetryable(): boolean {
    switch (this.code) {
      case 'PROVIDER_UNAVAILABLE':
      case 'PROVIDER_RATE_LIMITED':
      case 'DOWNLOAD_FAILED':
      case 'SHUTTING_DOWN':
        return true
      default:
        return this.status >= 500
    }
  }
}

/** True when a request was aborted rather than failed. */
export function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}

/**
 * Turns anything thrown during a request into the message the UI shows. An
 * ApiError already carries the backend's wording; everything else is a
 * transport failure and gets a sentence of its own.
 */
export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) return error.message
  if (error instanceof Error) {
    return 'Das Backend ist nicht erreichbar.'
  }
  return 'Ein unbekannter Fehler ist aufgetreten.'
}

export interface RequestOptions {
  signal?: AbortSignal
  /** Query parameters; undefined and null entries are dropped. */
  query?: Record<string, string | number | boolean | undefined | null>
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
}

/** Builds a URL below /api/v1, appending the query parameters that have a value. */
export function apiUrl(path: string, query?: RequestOptions['query']): string {
  const url = `${API_BASE}${path}`
  if (!query) return url

  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === '') continue
    params.set(key, String(value))
  }
  const search = params.toString()
  return search ? `${url}?${search}` : url
}

/** A list answer keeps its meta block alongside the items. */
export interface ListResult<T> {
  items: T[]
  meta: ListMeta
}

/**
 * Performs a request and returns the unwrapped payload.
 *
 * A non-2xx answer becomes an ApiError carrying the backend's error code. A
 * body that is not the expected envelope is treated the same way rather than
 * being handed to the UI as undefined.
 */
export async function request<T>(
  path: string,
  options: RequestOptions = {},
): Promise<T> {
  const { envelope, response } = await requestEnvelope<T>(path, options)

  if (!('data' in envelope)) {
    throw new ApiError({
      code: 'INTERNAL_ERROR',
      message: 'Das Backend hat eine unerwartete Antwort gesendet.',
      status: response.status,
    })
  }
  return envelope.data
}

/**
 * Performs a request that answers without a body.
 *
 * 204 No Content is a success with nothing in it, so it must not go through
 * request(): that one insists on a "data" key and would turn a completed
 * deletion into "the backend sent an unexpected answer".
 */
export async function requestVoid(
  path: string,
  options: RequestOptions = {},
): Promise<void> {
  await requestEnvelope<never>(path, options)
}

/** Like request(), but keeps the meta block of a list answer. */
export async function requestList<T>(
  path: string,
  options: RequestOptions = {},
): Promise<ListResult<T>> {
  const { envelope, response } = await requestEnvelope<T[]>(path, options)

  if (!('data' in envelope) || !Array.isArray(envelope.data)) {
    throw new ApiError({
      code: 'INTERNAL_ERROR',
      message: 'Das Backend hat eine unerwartete Liste gesendet.',
      status: response.status,
    })
  }
  return {
    items: envelope.data,
    meta: envelope.meta ?? { count: envelope.data.length },
  }
}

function getCookie(name: string): string | undefined {
  if (typeof document === 'undefined') return undefined
  const match = document.cookie.match(new RegExp('(^|;\\s*)(' + name + ')=([^;]*)'))
  return match && match[3] !== undefined ? decodeURIComponent(match[3]) : undefined
}

async function requestEnvelope<T>(
  path: string,
  options: RequestOptions,
): Promise<{ envelope: Envelope<T>; response: Response }> {
  const { signal, query, method = 'GET', body } = options

  const init: RequestInit = { method, signal, headers: { Accept: 'application/json' } }
  if (method !== 'GET') {
    const csrfToken = getCookie('ytmdl_csrf')
    if (csrfToken) {
      init.headers = { ...init.headers, 'X-CSRF-Token': csrfToken }
    }
  }
  if (body !== undefined) {
    init.headers = { ...init.headers, 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }

  let response: Response
  try {
    response = await fetch(apiUrl(path, query), init)
  } catch (error) {
    // An aborted request is a normal part of navigation and must stay
    // distinguishable from a genuine transport failure.
    if (isAbortError(error)) throw error
    throw new ApiError({
      code: 'INTERNAL_ERROR',
      message: 'Das Backend ist nicht erreichbar.',
      status: 0,
    })
  }

  const payload = await readJson(response)

  if (!response.ok) {
    throw toApiError(payload, response)
  }
  return { envelope: (payload ?? {}) as Envelope<T>, response }
}

/** Reads the body as JSON, tolerating an empty or malformed one. */
async function readJson(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text) as unknown
  } catch {
    return null
  }
}

/** Builds an ApiError from an error envelope, falling back to the status. */
function toApiError(payload: unknown, response: Response): ApiError {
  const detail = (payload as ErrorEnvelope | null)?.error
  if (detail?.code) {
    return new ApiError({
      code: detail.code,
      message: detail.message || 'Die Anfrage ist fehlgeschlagen.',
      status: response.status,
      requestId: detail.request_id,
    })
  }
  return new ApiError({
    code: 'INTERNAL_ERROR',
    message: `Die Anfrage ist mit HTTP ${response.status} fehlgeschlagen.`,
    status: response.status,
  })
}
