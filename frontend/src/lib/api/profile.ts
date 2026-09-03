import { request, requestVoid, type RequestOptions } from '@/lib/api/client'
import type {
  ChangePasswordRequest,
  SessionSummary,
  UpdateProfileRequest,
  UserSummary,
} from '@/types/api'

/** Fetches current user profile. */
export function getProfile(options: RequestOptions = {}): Promise<UserSummary> {
  return request<UserSummary>('/profile', options)
}

/** Updates user display name. */
export function updateProfile(
  body: UpdateProfileRequest,
  options: RequestOptions = {},
): Promise<UserSummary> {
  return request<UserSummary>('/profile', {
    ...options,
    method: 'PATCH',
    body,
  })
}

/** Changes current user password and revokes other sessions. */
export function changePassword(
  body: ChangePasswordRequest,
  options: RequestOptions = {},
): Promise<void> {
  return requestVoid('/profile/password', {
    ...options,
    method: 'POST',
    body,
  })
}

/** Lists active sessions for current user. */
export function listSessions(options: RequestOptions = {}): Promise<SessionSummary[]> {
  return request<SessionSummary[]>('/profile/sessions', options)
}

/** Revokes a specific session. */
export function revokeSession(id: string, options: RequestOptions = {}): Promise<void> {
  return requestVoid(`/profile/sessions/${encodeURIComponent(id)}`, {
    ...options,
    method: 'DELETE',
  })
}

/** Revokes all other sessions except current. */
export function revokeOtherSessions(options: RequestOptions = {}): Promise<void> {
  return requestVoid('/profile/sessions/revoke-others', {
    ...options,
    method: 'POST',
  })
}
