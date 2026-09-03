/**
 * The job list, kept current from the event stream.
 *
 * The list is loaded once over REST and then patched from SSE. Events carry
 * only what changed, so anything an event does not mention — the media
 * provider, the timestamps, the release filter — is taken from the job that is
 * already in the list rather than reset. When the stream drops and comes back,
 * the list is reloaded, because events that arrived while it was down are gone
 * for good.
 */

import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from 'react'

import { listJobsWithMeta } from '@/lib/api/jobs'
import {
  connectionState,
  subscribeToConnectionState,
  subscribeToJobEvents,
} from '@/lib/sse/jobEvents'
import type { ConnectionState } from '@/lib/sse/jobEvents'
import { useAsync } from '@/hooks/useAsync'
import type { AsyncState } from '@/hooks/useAsync'
import type { Job, JobEvent, JobPriority, JobStatus, JobType, ListMeta } from '@/types/api'

/** The live connection state of the shared event stream. */
export function useConnectionState(): ConnectionState {
  return useSyncExternalStore(
    subscribeToConnectionState,
    connectionState,
    () => 'offline' as const,
  )
}

/** Registers a handler on the shared event stream for as long as it is mounted. */
export function useJobEvents(handler: (event: JobEvent) => void): void {
  const handlerRef = useRef(handler)
  useEffect(() => {
    handlerRef.current = handler
  })

  useEffect(() => {
    return subscribeToJobEvents((event) => handlerRef.current(event))
  }, [])
}

export interface UseJobsOptions {
  status?: JobStatus
  type?: JobType
  priority?: JobPriority
  limit?: number
  offset?: number
}

export interface JobsResult {
  state: AsyncState<Job[]>
  meta: ListMeta | null
  reload: () => void
  connection: ConnectionState
  setJobs: React.Dispatch<React.SetStateAction<Job[]>>
}

/** Loads the job list and keeps it current. */
export function useJobs(options: UseJobsOptions = {}): JobsResult {
  const limit = options.limit ?? 50
  const offset = options.offset ?? 0
  const status = options.status
  const type = options.type
  const priority = options.priority

  const [meta, setMeta] = useState<ListMeta | null>(null)

  const { state, reload, setData } = useAsync(
    async (signal) => {
      const res = await listJobsWithMeta({
        limit,
        offset,
        status,
        type,
        priority,
        signal,
      })
      setMeta(res.meta)
      return res.items
    },
    [limit, offset, status, type, priority],
  )
  const connection = useConnectionState()

  useJobEvents(
    useCallback(
      (event: JobEvent) => {
        if (!event.job_id) return

        // A newly created job is not in the list yet and cannot be patched
        // into it: only a reload gets its label, providers and options.
        if (event.type === 'job.created') {
          reload()
          return
        }
        setData((jobs) => applyEvent(jobs, event))
      },
      [reload, setData],
    ),
  )

  // Events that arrive while the stream is down are not replayed, so the list
  // is refetched once the connection is back.
  const wasOffline = useRef(false)
  useEffect(() => {
    if (connection === 'open' && wasOffline.current) reload()
    wasOffline.current = connection !== 'open'
  }, [connection, reload])

  const setJobs = useCallback(
    (action: React.SetStateAction<Job[]>) => {
      setData((prev) => (typeof action === 'function' ? action(prev) : action))
    },
    [setData],
  )

  return { state, meta, reload, connection, setJobs }
}

/**
 * Applies one event to the job list. A job the list does not hold is ignored:
 * the reload triggered by job.created is what brings it in.
 */
function applyEvent(jobs: Job[], event: JobEvent): Job[] {
  let changed = false

  const next = jobs.map((job) => {
    if (job.id !== event.job_id) return job
    const patched = patchJob(job, event)
    if (patched !== job) changed = true
    return patched
  })

  return changed ? next : jobs
}

function patchJob(job: Job, event: JobEvent): Job {
  const patch: Partial<Job> = {}

  if (event.status && event.status !== job.status) patch.status = event.status
  if (event.label && event.label !== job.label) patch.label = event.label
  if (event.priority && event.priority !== job.priority) patch.priority = event.priority
  if (typeof event.paused === 'boolean' && event.paused !== job.paused) patch.paused = event.paused

  // job.progress carries current/total; the summary is authoritative where it
  // is present, because it comes straight from the persisted counters.
  if (event.summary) {
    patch.total = event.summary.total
    patch.completed = event.summary.completed
    patch.failed = event.summary.failed
    patch.skipped = event.summary.skipped
  } else if (event.type === 'job.progress' && typeof event.total === 'number') {
    patch.total = event.total
  }

  if (event.type === 'job.failed') {
    if (event.error_code) patch.error_code = event.error_code
    if (event.error_message) patch.error_message = event.error_message
  }

  if (Object.keys(patch).length === 0) return job
  return { ...job, ...patch, updated_at: event.time }
}


/** The item states that mean a worker is busy with that track right now. */
const WORKING_ITEM_STATUSES = new Set(['matching', 'downloading', 'tagging'])

/**
 * The track each running job is working on, keyed by job id.
 *
 * This is progress detail the job record does not carry: the backend reports
 * the current track only on the event stream. It is therefore live-only and
 * disappears when a job ends or the stream drops.
 */
export function useCurrentTracks(): Record<string, string> {
  const [current, setCurrent] = useState<Record<string, string>>({})

  useJobEvents(
    useCallback((event: JobEvent) => {
      const jobId = event.job_id
      if (!jobId) return

      if (event.type === 'job.item') {
        // A finished item would otherwise leave a stale "current" behind.
        if (!event.track || !WORKING_ITEM_STATUSES.has(event.item_status ?? '')) {
          return
        }
        setCurrent((tracks) =>
          tracks[jobId] === event.track
            ? tracks
            : { ...tracks, [jobId]: event.track as string },
        )
        return
      }

      if (
        event.type === 'job.completed' ||
        event.type === 'job.failed' ||
        event.type === 'job.cancelled'
      ) {
        setCurrent((tracks) => {
          if (!(jobId in tracks)) return tracks
          const { [jobId]: _done, ...rest } = tracks
          return rest
        })
      }
    }, []),
  )

  return current
}
