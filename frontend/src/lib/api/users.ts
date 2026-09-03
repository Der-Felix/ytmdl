import { request, requestList, requestVoid, type ListResult, type RequestOptions } from '@/lib/api/client'
import type { CreateUserRequest, UpdateUserStatusRequest, UserSummary } from '@/types/api'

/** Lists all users (Admin only). */
export function listUsers(
  params: { limit?: number; offset?: number } = {},
  options: RequestOptions = {},
): Promise<ListResult<UserSummary>> {
  return requestList<UserSummary>('/users', {
    ...options,
    query: params,
  })
}

/** Creates a new user account (Admin only). */
export function createUser(
  body: CreateUserRequest,
  options: RequestOptions = {},
): Promise<UserSummary> {
  return request<UserSummary>('/users', {
    ...options,
    method: 'POST',
    body,
  })
}

/** Fetches a single user by ID (Admin only). */
export function getUser(id: string, options: RequestOptions = {}): Promise<UserSummary> {
  return request<UserSummary>(`/users/${encodeURIComponent(id)}`, options)
}

/** Updates user role, status or display name (Admin only). */
export function updateUser(
  id: string,
  body: UpdateUserStatusRequest,
  options: RequestOptions = {},
): Promise<UserSummary> {
  return request<UserSummary>(`/users/${encodeURIComponent(id)}`, {
    ...options,
    method: 'PATCH',
    body,
  })
}

/** Resets a user's password and revokes all active sessions (Admin only). */
export function resetPassword(
  id: string,
  password: string,
  options: RequestOptions = {},
): Promise<void> {
  return requestVoid(`/users/${encodeURIComponent(id)}/reset-password`, {
    ...options,
    method: 'POST',
    body: { password },
  })
}

/** Deletes a user account (Admin only). */
export function deleteUser(id: string, options: RequestOptions = {}): Promise<void> {
  return requestVoid(`/users/${encodeURIComponent(id)}`, {
    ...options,
    method: 'DELETE',
  })
}
