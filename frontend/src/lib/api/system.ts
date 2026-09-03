/** System status, versioning, and update detection API. */

import { request } from '@/lib/api/client'
import type { UpdateStatus } from '@/types/api'

/** GET /system/update — retrieves current or cached update status. */
export function getUpdateStatus(signal?: AbortSignal): Promise<UpdateStatus> {
  return request<UpdateStatus>('/system/update', { signal })
}

/** POST /system/update/check — triggers a fresh update check against GitHub. */
export function checkUpdate(signal?: AbortSignal): Promise<UpdateStatus> {
  return request<UpdateStatus>('/system/update/check', {
    method: 'POST',
    signal,
  })
}
