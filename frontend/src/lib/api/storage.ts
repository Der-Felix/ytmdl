/** Storage diagnostics and queue pause/resume controls. */

import { request } from '@/lib/api/client'
import type { StorageStatusResponse } from '@/types/api'

/** GET /storage/status — library and staging health diagnostics. */
export function getStorageStatus(signal?: AbortSignal): Promise<StorageStatusResponse> {
  return request<StorageStatusResponse>('/storage/status', { signal })
}

/** POST /storage/probe — forces an immediate filesystem/guard check. */
export function probeStorage(signal?: AbortSignal): Promise<StorageStatusResponse> {
  return request<StorageStatusResponse>('/storage/probe', {
    method: 'POST',
    signal,
  })
}

/** POST /storage/queue/pause — pauses the background download queue. */
export function pauseStorageQueue(signal?: AbortSignal): Promise<{ paused: boolean }> {
  return request<{ paused: boolean }>('/storage/queue/pause', {
    method: 'POST',
    signal,
  })
}

/** POST /storage/queue/resume — resumes the background download queue. */
export function resumeStorageQueue(signal?: AbortSignal): Promise<{ paused: boolean }> {
  return request<{ paused: boolean }>('/storage/queue/resume', {
    method: 'POST',
    signal,
  })
}
