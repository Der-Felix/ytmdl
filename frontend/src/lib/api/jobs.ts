/** Download jobs: creating them, listing them, cancelling them. */

import { request, requestList } from '@/lib/api/client'
import type {
  ArtistDownloadRequest,
  Job,
  JobDetail,
  JobItem,
  JobPriority,
  JobStatus,
  JobType,
  ListMeta,
  QueueSummary,
  ReleaseDownloadRequest,
  TrackDownloadRequest,
} from '@/types/api'
import { TERMINAL_JOB_STATUSES } from '@/types/api'

/** POST /downloads/artist */
export function downloadArtist(
  body: ArtistDownloadRequest,
  signal?: AbortSignal,
): Promise<Job> {
  return request<Job>('/downloads/artist', { method: 'POST', body, signal })
}

/** POST /downloads/release */
export function downloadRelease(
  body: ReleaseDownloadRequest,
  signal?: AbortSignal,
): Promise<Job> {
  return request<Job>('/downloads/release', { method: 'POST', body, signal })
}

/** POST /downloads/track */
export function downloadTrack(
  body: TrackDownloadRequest,
  signal?: AbortSignal,
): Promise<Job> {
  return request<Job>('/downloads/track', { method: 'POST', body, signal })
}

export interface ListJobsOptions {
  status?: JobStatus
  type?: JobType
  priority?: JobPriority
  limit?: number
  offset?: number
  signal?: AbortSignal
}

export interface ListJobsResult {
  items: Job[]
  meta: ListMeta
}

/** GET /jobs */
export async function listJobs(
  options: ListJobsOptions = {},
): Promise<Job[]> {
  const result = await requestList<Job>('/jobs', {
    query: {
      status: options.status,
      type: options.type,
      priority: options.priority,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
  return result.items
}

/** GET /jobs with metadata (total count for pagination) */
export function listJobsWithMeta(
  options: ListJobsOptions = {},
): Promise<ListJobsResult> {
  return requestList<Job>('/jobs', {
    query: {
      status: options.status,
      type: options.type,
      priority: options.priority,
      limit: options.limit,
      offset: options.offset,
    },
    signal: options.signal,
  })
}

/** GET /jobs/{id} — the job with its items and summary. */
export function getJob(id: string, signal?: AbortSignal): Promise<JobDetail> {
  return request<JobDetail>(`/jobs/${encodeURIComponent(id)}`, { signal })
}

/** GET /jobs/summary — live queue summary and ETA */
export function getQueueSummary(signal?: AbortSignal): Promise<QueueSummary> {
  return request<QueueSummary>('/jobs/summary', { signal })
}

/** PATCH /jobs/{id} — update job priority and/or pause state. */
export function updateJob(
  id: string,
  body: { priority?: JobPriority; paused?: boolean },
  signal?: AbortSignal,
): Promise<Job> {
  return request<Job>(`/jobs/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body,
    signal,
  })
}

/** POST /jobs/{id}/pause */
export function pauseJob(id: string, signal?: AbortSignal): Promise<Job> {
  return request<Job>(`/jobs/${encodeURIComponent(id)}/pause`, {
    method: 'POST',
    signal,
  })
}

/** POST /jobs/{id}/resume */
export function resumeJob(id: string, signal?: AbortSignal): Promise<Job> {
  return request<Job>(`/jobs/${encodeURIComponent(id)}/resume`, {
    method: 'POST',
    signal,
  })
}

/** POST /jobs/{id}/retry-failed */
export function retryFailedJob(
  id: string,
  signal?: AbortSignal,
): Promise<{ job: Job; retried: number; skipped: number }> {
  return request<{ job: Job; retried: number; skipped: number }>(
    `/jobs/${encodeURIComponent(id)}/retry-failed`,
    {
      method: 'POST',
      signal,
    },
  )
}

/** POST /jobs/{job_id}/items/{item_id}/retry */
export function retryJobItem(
  jobId: string,
  itemId: string,
  signal?: AbortSignal,
): Promise<JobItem> {
  return request<JobItem>(
    `/jobs/${encodeURIComponent(jobId)}/items/${encodeURIComponent(itemId)}/retry`,
    {
      method: 'POST',
      signal,
    },
  )
}

/** DELETE /jobs/{id} — cancels the job; its history stays readable. */
export function cancelJob(id: string, signal?: AbortSignal): Promise<Job> {
  return request<Job>(`/jobs/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    signal,
  })
}

/** DELETE /jobs/history — safely purges old job history (Admin only). */
export function deleteJobHistory(
  olderThanDays?: number,
  statuses?: string[],
  signal?: AbortSignal,
): Promise<{ deleted_jobs: number; deleted_items: number }> {
  return request<{ deleted_jobs: number; deleted_items: number }>(
    '/jobs/history',
    {
      method: 'DELETE',
      query: {
        older_than_days: olderThanDays,
        statuses: statuses ? statuses.join(',') : undefined,
      },
      signal,
    },
  )
}


/* ------------------------------------------------------------------ helpers */

/** True once the job has reached a final state. */
export function isTerminal(job: Job): boolean {
  return TERMINAL_JOB_STATUSES.includes(job.status)
}

/** True while the job is queued or being worked on. */
export function isActive(job: Job): boolean {
  return !isTerminal(job)
}

/** Items that reached a final state — completed, failed or skipped. */
export function processed(job: Job): number {
  return job.completed + job.failed + job.skipped
}

/**
 * Progress in percent, or null while the total is still unknown.
 *
 * The total only exists once the catalogue has been resolved, so a job in a
 * resolving state has no meaningful percentage and must not be shown as 0 %.
 */
export function progressPercent(job: Job): number | null {
  if (job.total <= 0) return null
  return Math.min(100, Math.round((processed(job) / job.total) * 100))
}

/** The German label of a job status. */
export const JOB_STATUS_LABELS: Record<JobStatus, string> = {
  queued: 'In Warteschlange',
  resolving_artist: 'Künstler wird aufgelöst',
  resolving_releases: 'Releases werden aufgelöst',
  resolving_tracks: 'Tracks werden aufgelöst',
  deduplicating: 'Duplikate werden entfernt',
  matching: 'Quellen werden gesucht',
  downloading: 'Wird heruntergeladen',
  tagging: 'Tags werden geschrieben',
  finalizing: 'Wird finalisiert',
  retry_wait: 'Wartet auf Wiederholung',
  waiting_for_storage: 'Wartet auf Speicher',
  waiting_for_space: 'Wartet auf Speicherplatz',
  completed: 'Abgeschlossen',
  failed: 'Fehlgeschlagen',
  cancelled: 'Abgebrochen',
}

/** The German label of an item status. */
export const ITEM_STATUS_LABELS: Record<string, string> = {
  pending: 'Ausstehend',
  matching: 'Quellensuche',
  downloading: 'Download',
  tagging: 'Tagging',
  finalizing: 'Finalisierung',
  retry_wait: 'Wiederholung geplant',
  waiting_for_storage: 'Wartet auf Speicher',
  waiting_for_space: 'Wartet auf Speicherplatz',
  completed: 'Abgeschlossen',
  failed: 'Fehlgeschlagen',
  skipped: 'Übersprungen',
  cancelled: 'Abgebrochen',
}

/** The German label of a job type. */
export const JOB_TYPE_LABELS: Record<JobType, string> = {
  artist: 'Diskografie',
  release: 'Release',
  track: 'Track',
}

/** The German label of a job priority. */
export const JOB_PRIORITY_LABELS: Record<JobPriority, string> = {
  low: 'Niedrig',
  normal: 'Normal',
  high: 'Hoch',
  very_high: 'Sehr hoch',
}

/**
 * How a job counts towards the sections of the downloads page.
 *
 * A job that completed with failures is deliberately still "abgeschlossen":
 * individual track failures must not present the whole job as a failure. Only
 * a job whose own status is failed belongs in the failure section.
 */
export function section(job: Job): 'active' | 'queued' | 'done' | 'failed' {
  if (job.status === 'queued') return 'queued'
  if (job.status === 'failed') return 'failed'
  if (isTerminal(job)) return 'done'
  return 'active'
}

