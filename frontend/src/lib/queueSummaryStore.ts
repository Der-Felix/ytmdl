/**
 * The queue summary (ETA, throughput, active workers, next-up jobs), shared by
 * every view that shows it.
 *
 * Several mounted views need the summary at once — the sidebar badge, the
 * download home and the downloads page. Each loading it on its own turned
 * every job event into three requests, and while downloads run the events
 * arrive several times a second. One store therefore holds the summary for
 * all of them and refetches it at most once per throttle window:
 *
 * - the first subscriber triggers an immediate load;
 * - structural job events request a refetch; the first one in a quiet period
 *   runs at once, later ones collapse into one trailing refetch at the end of
 *   the window, so the final state is always loaded;
 * - progress events patch the worker's percentage without a request;
 * - while the document is hidden nothing is fetched; one refetch follows when
 *   it becomes visible again, if anything changed meanwhile;
 * - a reconnected event stream reloads at once, because events sent while it
 *   was down are lost.
 */

import { isAbortError } from '@/lib/api/client'
import { getQueueSummary } from '@/lib/api/jobs'
import { connectionState, subscribeToConnectionState, subscribeToJobEvents } from '@/lib/sse/jobEvents'
import type { ConnectionState } from '@/lib/sse/jobEvents'
import type { AsyncState } from '@/hooks/useAsync'
import type { JobEvent, QueueSummary } from '@/types/api'

/** Minimum time between two event-driven summary requests. */
export const QUEUE_SUMMARY_THROTTLE_MS = 3000

/** Job events that can change counts, workers or the next-up list. */
const STRUCTURAL_EVENTS = new Set<JobEvent['type']>([
  'job.created',
  'job.status',
  'job.completed',
  'job.failed',
  'job.cancelled',
  'job.paused',
  'job.resumed',
  'job.retried',
  'job.item',
])

export interface QueueSummarySnapshot {
  state: AsyncState<QueueSummary>
  /** True while a user-requested reload is in flight. */
  refreshing: boolean
}

export interface QueueSummaryStoreDeps {
  fetchSummary: (signal: AbortSignal) => Promise<QueueSummary>
  subscribeToEvents: (handler: (event: JobEvent) => void) => () => void
  subscribeToConnection: (handler: (state: ConnectionState) => void) => () => void
  /** The connection state at the moment the store starts. */
  connectionState?: () => ConnectionState
  /** Whether the page is hidden; absent outside the browser. */
  isHidden: () => boolean
  /** Registers a visibility change listener and returns its cleanup. */
  onVisibilityChange: (handler: () => void) => () => void
  now: () => number
  throttleMs?: number
}

export interface QueueSummaryStore {
  subscribe: (listener: () => void) => () => void
  getSnapshot: () => QueueSummarySnapshot
  /** A user-requested reload: runs at once, ignoring the throttle. */
  reload: () => void
}

const INITIAL: QueueSummarySnapshot = { state: { status: 'loading' }, refreshing: false }

export function createQueueSummaryStore(deps: QueueSummaryStoreDeps): QueueSummaryStore {
  const throttleMs = deps.throttleMs ?? QUEUE_SUMMARY_THROTTLE_MS
  const listeners = new Set<() => void>()

  let snapshot: QueueSummarySnapshot = INITIAL
  let controller: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | null = null
  let lastFetchAt = Number.NEGATIVE_INFINITY
  /** A refetch was requested while one was running or the page was hidden. */
  let dirty = false
  let wasOpen = false
  let wasOffline = false
  let teardown: Array<() => void> = []

  function emit(next: QueueSummarySnapshot): void {
    snapshot = next
    for (const listener of listeners) listener()
  }

  function clearTimer(): void {
    if (timer !== null) {
      clearTimeout(timer)
      timer = null
    }
  }

  function fetchNow(manual: boolean): void {
    clearTimer()
    controller?.abort()
    const own = new AbortController()
    controller = own
    dirty = false
    lastFetchAt = deps.now()
    if (manual) emit({ ...snapshot, refreshing: true })

    deps
      .fetchSummary(own.signal)
      .then((data) => {
        if (controller !== own) return
        controller = null
        emit({ state: { status: 'success', data }, refreshing: false })
      })
      .catch((error: unknown) => {
        if (controller !== own || isAbortError(error)) return
        controller = null
        // A failed background refresh keeps the summary on screen; only a
        // failed first load or a failed explicit reload is shown as an error.
        if (snapshot.state.status === 'success' && !manual) {
          emit({ ...snapshot, refreshing: false })
        } else {
          emit({ state: { status: 'error', error }, refreshing: false })
        }
      })
      .finally(() => {
        if (dirty && listeners.size > 0) requestRefetch()
      })
  }

  /** Asks for a refetch under the throttle; requests in a burst collapse. */
  function requestRefetch(): void {
    if (deps.isHidden() || controller !== null) {
      dirty = true
      return
    }
    if (timer !== null) return
    const wait = lastFetchAt + throttleMs - deps.now()
    if (wait <= 0) {
      fetchNow(false)
      return
    }
    timer = setTimeout(() => {
      timer = null
      if (deps.isHidden()) {
        dirty = true
        return
      }
      fetchNow(false)
    }, wait)
  }

  function handleEvent(event: JobEvent): void {
    // Fast path: the worker's percentage comes straight from the event.
    if (event.type === 'job.progress' && event.item_id !== undefined && event.download_percent !== undefined) {
      const state = snapshot.state
      if (state.status !== 'success' || !state.data.current) return
      let changed = false
      const current = state.data.current.map((worker) => {
        if (worker.item_id !== event.item_id) return worker
        const progress = event.download_percent ?? worker.progress_percent
        if (progress === worker.progress_percent) return worker
        changed = true
        return { ...worker, progress_percent: progress }
      })
      if (changed) emit({ ...snapshot, state: { status: 'success', data: { ...state.data, current } } })
      return
    }
    if (STRUCTURAL_EVENTS.has(event.type)) requestRefetch()
  }

  function handleConnection(state: ConnectionState): void {
    // Only a stream that was open and dropped can have lost events; the
    // first open after the start is already covered by the first load.
    if (state === 'open') {
      if (wasOffline) fetchNow(false)
      wasOpen = true
      wasOffline = false
    } else if (wasOpen) {
      wasOffline = true
    }
  }

  function handleVisibility(): void {
    if (!deps.isHidden() && dirty && controller === null) requestRefetch()
  }

  function start(): void {
    wasOpen = deps.connectionState?.() === 'open'
    wasOffline = false
    teardown = [
      deps.subscribeToEvents(handleEvent),
      deps.subscribeToConnection(handleConnection),
      deps.onVisibilityChange(handleVisibility),
    ]
    fetchNow(false)
  }

  function stop(): void {
    for (const cleanup of teardown) cleanup()
    teardown = []
    clearTimer()
    controller?.abort()
    controller = null
    dirty = false
    lastFetchAt = Number.NEGATIVE_INFINITY
    // Nothing is shown without a subscriber; the next one (perhaps another
    // user after signing in again) starts from a fresh load.
    snapshot = INITIAL
  }

  return {
    subscribe(listener) {
      listeners.add(listener)
      if (listeners.size === 1) start()
      return () => {
        listeners.delete(listener)
        if (listeners.size === 0) stop()
      }
    },
    getSnapshot: () => snapshot,
    reload: () => {
      if (listeners.size > 0) fetchNow(true)
    },
  }
}

/** The application's one queue summary store. */
export const queueSummaryStore = createQueueSummaryStore({
  fetchSummary: (signal) => getQueueSummary(signal),
  subscribeToEvents: subscribeToJobEvents,
  subscribeToConnection: subscribeToConnectionState,
  connectionState,
  isHidden: () => typeof document !== 'undefined' && document.visibilityState === 'hidden',
  onVisibilityChange: (handler) => {
    if (typeof document === 'undefined') return () => {}
    document.addEventListener('visibilitychange', handler)
    return () => document.removeEventListener('visibilitychange', handler)
  },
  now: () => Date.now(),
})
