import { request, requestVoid, type RequestOptions } from '@/lib/api/client'
import type { AuthStatus, LoginRequest, SetupRequest, UserSummary } from '@/types/api'

/** Fetches current system auth status (setup_required, authenticated, user). */
export function getAuthStatus(options: RequestOptions = {}): Promise<AuthStatus> {
  return request<AuthStatus>('/auth/status', options)
}

/** Executes first-run administrator setup. */
export function setupAdmin(body: SetupRequest, options: RequestOptions = {}): Promise<UserSummary> {
  return request<UserSummary>('/auth/setup', {
    ...options,
    method: 'POST',
    body,
  })
}

/** Authenticates a user with username and password. */
export function login(body: LoginRequest, options: RequestOptions = {}): Promise<UserSummary> {
  return request<UserSummary>('/auth/login', {
    ...options,
    method: 'POST',
    body,
  })
}

/** Logs out the current user and invalidates the session. */
export function logout(options: RequestOptions = {}): Promise<void> {
  return requestVoid('/auth/logout', {
    ...options,
    method: 'POST',
  })
}

/** Fetches the currently authenticated user's summary. */
export function getMe(options: RequestOptions = {}): Promise<UserSummary> {
  return request<UserSummary>('/auth/me', options)
}
