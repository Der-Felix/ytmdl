import { afterEach, beforeEach, describe, expect, it, jest } from 'bun:test'

import { createQueueSummaryStore } from './queueSummaryStore'
import type { ConnectionState } from '@/lib/sse/jobEvents'
import type { JobEvent, QueueSummary } from '@/types/api'

function summary(overrides: Partial<QueueSummary> = {}): QueueSummary {
  return {
    active_jobs: 1,
    queued_jobs: 0,
    current: [{ item_id: 7, progress_percent: 10 }],
    next: [],
    ...overrides,
  } as unknown as QueueSummary
}

/** Settles pending promise callbacks (fetch resolution and its finally). */
async function flush(): Promise<void> {
  for (let i = 0; i < 5; i++) await Promise.resolve()
}

function setup() {
  let clock = 0
  let hidden = false
  let eventHandler: ((event: JobEvent) => void) | null = null
  let connectionHandler: ((state: ConnectionState) => void) | null = null
  let visibilityHandler: (() => void) | null = null
  const fetches: Array<{ resolve: (s: QueueSummary) => void; reject: (e: unknown) => void }> = []

  const store = createQueueSummaryStore({
    fetchSummary: () =>
      new Promise<QueueSummary>((resolve, reject) => {
        fetches.push({ resolve, reject })
      }),
    subscribeToEvents: (handler) => {
      eventHandler = handler
      return () => {
        eventHandler = null
      }
    },
    subscribeToConnection: (handler) => {
      connectionHandler = handler
      return () => {
        connectionHandler = null
      }
    },
    isHidden: () => hidden,
    onVisibilityChange: (handler) => {
      visibilityHandler = handler
      return () => {
        visibilityHandler = null
      }
    },
    now: () => clock,
    throttleMs: 3000,
  })

  return {
    store,
    fetches,
    advance(ms: number) {
      clock += ms
      jest.advanceTimersByTime(ms)
    },
    emit(event: Partial<JobEvent> & { type: JobEvent['type'] }) {
      eventHandler?.({ time: '2026-11-25T00:00:00Z', ...event } as JobEvent)
    },
    connection(state: ConnectionState) {
      connectionHandler?.(state)
    },
    setHidden(value: boolean) {
      hidden = value
      visibilityHandler?.()
    },
    async resolveLast(data: QueueSummary = summary()) {
      fetches[fetches.length - 1].resolve(data)
      await flush()
    },
    get subscribed() {
      return eventHandler !== null
    },
  }
}

describe('queueSummaryStore', () => {
  beforeEach(() => {
    jest.useFakeTimers()
  })
  afterEach(() => {
    jest.useRealTimers()
  })

  it('loads once at once and serves every subscriber from one request', async () => {
    const t = setup()
    const a = t.store.subscribe(() => {})
    const b = t.store.subscribe(() => {})
    const c = t.store.subscribe(() => {})
    expect(t.fetches).toHaveLength(1)
    await t.resolveLast(summary({ active_jobs: 3 }))
    expect(t.store.getSnapshot().state).toMatchObject({ status: 'success', data: { active_jobs: 3 } })
    a()
    b()
    c()
    expect(t.subscribed).toBe(false)
  })

  it('throttles a burst of events to one leading and one trailing refetch', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    expect(t.fetches).toHaveLength(1)

    // Ten events per second for two seconds right after the first load.
    for (let i = 0; i < 20; i++) {
      t.emit({ type: 'job.item', job_id: 'j' })
      t.advance(100)
    }
    // Still inside the window of the first load: nothing yet.
    expect(t.fetches).toHaveLength(1)

    t.advance(1000)
    expect(t.fetches).toHaveLength(2)
    await t.resolveLast()

    // No further events: no further requests.
    t.advance(10_000)
    expect(t.fetches).toHaveLength(2)
    off()
  })

  it('keeps at most one request per window while events keep arriving', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()

    for (let i = 0; i < 100; i++) {
      t.emit({ type: 'job.status', job_id: 'j' })
      t.advance(100)
      if (t.fetches.length > 1) await t.resolveLast()
    }
    // 10 s of events: the first load plus one refetch per 3 s window.
    expect(t.fetches.length).toBeLessThanOrEqual(5)
    expect(t.fetches.length).toBeGreaterThanOrEqual(3)
    off()
  })

  it('loads the final state when an event arrives during a running request', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.advance(5000)

    t.emit({ type: 'job.completed', job_id: 'j' })
    expect(t.fetches).toHaveLength(2)
    // The job finishes while that request is still out.
    t.emit({ type: 'job.created', job_id: 'k' })
    await t.resolveLast(summary({ active_jobs: 1 }))

    t.advance(3000)
    expect(t.fetches).toHaveLength(3)
    await t.resolveLast(summary({ active_jobs: 2 }))
    expect(t.store.getSnapshot().state).toMatchObject({ data: { active_jobs: 2 } })
    off()
  })

  it('patches worker progress without a request', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.emit({ type: 'job.progress', job_id: 'j', item_id: 7, download_percent: 55 })
    expect(t.fetches).toHaveLength(1)
    const state = t.store.getSnapshot().state
    expect(state.status === 'success' && state.data.current?.[0].progress_percent).toBe(55)
    off()
  })

  it('does not fetch while hidden and refetches once when visible again', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.setHidden(true)
    for (let i = 0; i < 10; i++) {
      t.emit({ type: 'job.item', job_id: 'j' })
      t.advance(1000)
    }
    expect(t.fetches).toHaveLength(1)

    t.setHidden(false)
    expect(t.fetches).toHaveLength(2)
    await t.resolveLast()
    t.advance(10_000)
    expect(t.fetches).toHaveLength(2)
    off()
  })

  it('stays quiet on becoming visible when nothing changed', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.setHidden(true)
    t.advance(10_000)
    t.setHidden(false)
    expect(t.fetches).toHaveLength(1)
    off()
  })

  it('reloads at once when the event stream reconnects', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.connection('offline')
    t.connection('connecting')
    expect(t.fetches).toHaveLength(1)
    t.connection('open')
    expect(t.fetches).toHaveLength(2)
    off()
  })

  it('reports a manual reload as refreshing and keeps the data meanwhile', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.store.reload()
    expect(t.fetches).toHaveLength(2)
    expect(t.store.getSnapshot()).toMatchObject({ refreshing: true, state: { status: 'success' } })
    await t.resolveLast()
    expect(t.store.getSnapshot().refreshing).toBe(false)
    off()
  })

  it('keeps the summary when a background refresh fails', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast(summary({ active_jobs: 4 }))
    t.advance(5000)
    t.emit({ type: 'job.status', job_id: 'j' })
    t.fetches[1].reject(new Error('boom'))
    await flush()
    expect(t.store.getSnapshot().state).toMatchObject({ status: 'success', data: { active_jobs: 4 } })
    off()
  })

  it('starts over after the last subscriber left', async () => {
    const t = setup()
    const off = t.store.subscribe(() => {})
    await t.resolveLast()
    t.emit({ type: 'job.item', job_id: 'j' })
    off()
    t.advance(10_000)
    expect(t.fetches).toHaveLength(1)
    expect(t.store.getSnapshot().state.status).toBe('loading')

    const again = t.store.subscribe(() => {})
    expect(t.fetches).toHaveLength(2)
    again()
  })
})
