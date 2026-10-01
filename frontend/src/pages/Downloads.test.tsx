import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'

import { Downloads } from './Downloads'
import { AuthProvider } from '@/hooks/useAuth'
import { isTerminal, section } from '@/lib/api/jobs'
import { navigate } from '@/lib/router'
import type { Job, QueueSummary } from '@/types/api'

function mockJob(id: string, status: Job['status'] = 'queued', paused = false): Job {
  return {
    id,
    type: 'artist',
    status,
    priority: 'normal',
    paused,
    label: `Artist ${id}`,
    metadata_provider: 'deezer',
    media_provider: 'youtube',
    target_id: `target-${id}`,
    options: {
      release_filter: {
        albums: true,
        singles: true,
        eps: true,
        live: false,
        compilations: false,
        remixes: false,
      },
      skip_existing: true,
    },
    total: 10,
    completed: status === 'completed' ? 10 : 0,
    failed: status === 'failed' ? 1 : 0,
    skipped: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }
}

const mockSummary: QueueSummary = {
  total_jobs: 587,
  active_jobs: 3,
  queued_jobs: 572,
  paused_jobs: 32,
  done_jobs: 10,
  failed_jobs: 2,
  active_items: 3,
  remaining_items: 5000,
  retry_wait_items: 0,
  completed_last_hour: 20,
  throughput_items_per_hour: 20,
  eta_seconds: 3600,
  eta_confidence: 'medium',
  eta_text: 'ca. 1 Stunde',
  total_relevant: 5020,
  completed_relevant: 20,
  storage_healthy: true,
  current: [],
  next: [],
}

let originalFetch: typeof fetch
let originalEventSource: typeof EventSource
let streamListeners: Map<string, (event: MessageEvent<string>) => void>

function stubFetch(jobs: Job[], summary: QueueSummary | null, totalCount = 587) {
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString()
    const parsed = new URL(url, 'http://localhost')

    if (parsed.pathname === '/api/v1/jobs/summary') {
      if (!summary) {
        return new Response(
          JSON.stringify({
            error: { code: 'INTERNAL_ERROR', message: 'not available' },
          }),
          {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          },
        )
      }
      return new Response(JSON.stringify({ data: summary }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }

    if (parsed.pathname === '/api/v1/jobs') {
      return new Response(
        JSON.stringify({
          data: jobs,
          meta: {
            total: totalCount,
            limit: 20,
            offset: 0,
          },
        }),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      )
    }

    if (parsed.pathname === '/api/v1/auth/me') {
      return new Response(
        JSON.stringify({
          id: 'admin_1',
          username: 'sysadmin',
          role: 'admin',
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }

    if (parsed.pathname === '/api/v1/auth/status') {
      return new Response(
        JSON.stringify({
          setup_required: false,
          authenticated: true,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }

    return new Response(JSON.stringify({}), { status: 200 })
  }) as typeof fetch
}

function setTestURL(to: string) {
  const fullUrl = to.startsWith('http') ? to : `http://localhost${to}`
  if ((window as any).happyDOM?.setURL) {
    ;(window as any).happyDOM.setURL(fullUrl)
  }
  navigate(to, { replace: true })
}

beforeEach(() => {
  originalFetch = globalThis.fetch
  originalEventSource = globalThis.EventSource
  streamListeners = new Map()
  globalThis.EventSource = class {
    close() {}
    addEventListener(type: string, listener: (event: MessageEvent<string>) => void) { streamListeners.set(type, listener) }
    onopen = null
    onerror = null
    readyState = 0
    static readonly CLOSED = 2
  } as unknown as typeof EventSource
  setTestURL('/downloads')
})

afterEach(() => {
  cleanup()
  globalThis.fetch = originalFetch
  globalThis.EventSource = originalEventSource
})

describe('Downloads page tab counts', () => {
  it('loads a linked job directly even when it is not on the first list page', async () => {
    stubFetch([], mockSummary)
    const mocked = globalThis.fetch
    const paths: string[] = []
    globalThis.fetch = (async (input, init) => {
      const path = new URL(String(input), 'http://localhost').pathname
      paths.push(path)
      if (path === '/api/v1/jobs/older-job') return new Response(JSON.stringify({ data: { job: mockJob('older-job', 'completed'), items: [] } }), { headers: { 'Content-Type': 'application/json' } })
      return mocked(input, init)
    }) as typeof fetch
    render(<AuthProvider><Downloads jobId="older-job" /></AuthProvider>)
    expect(await screen.findByText('Artist older-job')).toBeDefined()
    expect(paths).toContain('/api/v1/jobs/older-job')
    expect(paths).not.toContain('/api/v1/jobs')
  })

  it('refreshes filtered membership when an unseen job completes through SSE', async () => {
    stubFetch([], mockSummary, 0)
    const mocked = globalThis.fetch
    let completed = false
    let requests = 0
    let finishRefresh: (() => void) | undefined
    globalThis.fetch = (async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname === '/api/v1/jobs') {
        requests++
        if (completed) await new Promise<void>((resolve) => { finishRefresh = resolve })
        const data = [mockJob('already-done', 'completed'), ...(completed ? [mockJob('newly-done', 'completed')] : [])]
        return new Response(JSON.stringify({ data, meta: { total: data.length, count: data.length } }), { headers: { 'Content-Type': 'application/json' } })
      }
      return mocked(input, init)
    }) as typeof fetch
    setTestURL('/downloads?view=done')
    render(<AuthProvider><Downloads /></AuthProvider>)
    await waitFor(() => expect(requests).toBe(1))
    expect(await screen.findByText('Artist already-done')).toBeDefined()
    completed = true
    act(() => streamListeners.get('job.completed')!(new MessageEvent('job.completed', { data: JSON.stringify({ type: 'job.completed', job_id: 'newly-done', status: 'completed' }) })))
    await waitFor(() => expect(finishRefresh).toBeDefined())
    expect(screen.getByText('Artist already-done')).toBeDefined()
    await act(async () => finishRefresh!())
    expect(await screen.findByText('Artist newly-done')).toBeDefined()
    expect(requests).toBe(2)
  })

  it('does not label a filtered total as the global all count when the summary is unavailable', async () => {
    stubFetch([mockJob('failure', 'failed')], null, 7)
    setTestURL('/downloads?view=failed')
    render(<AuthProvider><Downloads /></AuthProvider>)
    expect(await screen.findByText('Artist failure')).toBeDefined()
    expect(screen.getByRole('button', { name: /^Alle/ }).textContent).toBe('Alle')
    expect(screen.getByText('7 Aufträge in dieser Ansicht')).toBeDefined()
  })

  it('requests the selected group and very-high priority before pagination', async () => {
    stubFetch([], mockSummary, 0)
    const mocked = globalThis.fetch
    const queries: URLSearchParams[] = []
    globalThis.fetch = (async (input, init) => {
      const url = new URL(String(input), 'http://localhost')
      if (url.pathname === '/api/v1/jobs') queries.push(url.searchParams)
      return mocked(input, init)
    }) as typeof fetch
    setTestURL('/downloads?view=failed&priority=very_high&page=2')
    render(<AuthProvider><Downloads /></AuthProvider>)
    await waitFor(() => expect(queries.length).toBeGreaterThan(0))
    expect(queries.at(-1)!.get('view')).toBe('failed')
    expect(queries.at(-1)!.get('priority')).toBe('very_high')
    expect(queries.at(-1)!.get('offset')).toBe('20')
    expect((screen.getByRole('combobox', { name: 'Priorität:' }) as HTMLSelectElement).value).toBe('very_high')
    fireEvent.click(screen.getByRole('button', { name: /Aktiv/ }))
    await waitFor(() => expect(queries.at(-1)!.get('view')).toBe('active'))
    expect(queries.at(-1)!.get('offset')).toBe('0')
  })

  it('uses global summary counts for tab badges instead of paginated slice length', async () => {
    const page1Jobs: Job[] = Array.from({ length: 20 }, (_, i) =>
      mockJob(`job-${i + 1}`, 'queued', false),
    )

    stubFetch(page1Jobs, mockSummary, 587)

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    await waitFor(() => {
      const allTab = screen.getByRole('button', { name: /Alle/ })
      const activeTab = screen.getByRole('button', { name: /Aktiv/ })
      const queuedTab = screen.getByRole('button', { name: /Warteschlange/ })
      const pausedTab = screen.getByRole('button', { name: /Pausiert/ })
      const doneTab = screen.getByRole('button', { name: /Fertig/ })
      const failedTab = screen.getByRole('button', { name: /Fehlgeschlagen/ })

      expect(allTab.textContent).toContain('587')
      expect(activeTab.textContent).toContain('3')
      // Crucial: page 1 has 20 queued jobs, but global count is 572 (Model A partition: 3 + 572 + 10 + 2 = 587)
      expect(queuedTab.textContent).toContain('572')
      expect(queuedTab.textContent).not.toContain('20')
      // Page 1 has 0 paused jobs, but global count is 32 (actionable overlay)
      expect(pausedTab.textContent).toContain('32')
      expect(doneTab.textContent).toContain('10')
      expect(failedTab.textContent).toContain('2')
    })
  })

  it('preserves global badge counts across page navigation', async () => {
    const page2Jobs: Job[] = Array.from({ length: 20 }, (_, i) =>
      mockJob(`job-${i + 21}`, 'queued', false),
    )

    stubFetch(page2Jobs, mockSummary, 587)
    setTestURL('/downloads?page=2')

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    await waitFor(() => {
      const queuedTab = screen.getByRole('button', { name: /Warteschlange/ })
      expect(queuedTab.textContent).toContain('572')
    })
  })

  it('handles unavailable summary cleanly without crashing', async () => {
    stubFetch([], null, 0)

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /Alle/ })).toBeDefined()
    })

    const allTab = screen.getByRole('button', { name: /Alle/ })
    expect(allTab.textContent).toBe('Alle')
  })

  it('omits status tab badges when summary is unavailable even if jobs are present on the page', async () => {
    const pageJobs: Job[] = Array.from({ length: 20 }, (_, i) =>
      mockJob(`job-${i + 1}`, 'queued', false),
    )
    stubFetch(pageJobs, null, 0)

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    await waitFor(() => {
      const queuedTab = screen.getByRole('button', { name: /Warteschlange/ })
      const activeTab = screen.getByRole('button', { name: /Aktiv/ })
      // Crucial: page has 20 queued jobs, but without global summary,
      // no misleading slice badge "20" is shown.
      expect(queuedTab.textContent).toBe('Warteschlange')
      expect(activeTab.textContent).toBe('Aktiv')
    })
  })

  it('verifies Model A partition arithmetic on global summary counts', () => {
    // Partition invariant: Active + Queued + Done + Failed == Total
    const partitionSum =
      mockSummary.active_jobs +
      mockSummary.queued_jobs +
      mockSummary.done_jobs +
      mockSummary.failed_jobs
    expect(partitionSum).toBe(mockSummary.total_jobs)
    // Paused is actionable overlay, not part of the partition sum
    expect(mockSummary.paused_jobs).toBe(32)
  })

  it('filters paused jobs strictly to actionable non-terminal jobs', () => {
    const actionablePaused = mockJob('actionable-paused', 'queued', true)
    const historicalCancelledPaused = mockJob('historical-cancelled', 'cancelled', true)
    const historicalCompletedPaused = mockJob('historical-completed', 'completed', true)
    const historicalFailedPaused = mockJob('historical-failed', 'failed', true)
    const runnableQueued = mockJob('runnable', 'queued', false)

    const jobs = [
      actionablePaused,
      historicalCancelledPaused,
      historicalCompletedPaused,
      historicalFailedPaused,
      runnableQueued,
    ]

    // Downloads.tsx paused tab predicate: j.paused && !isTerminal(j)
    const pausedTabPredicate = (j: Job) => j.paused && !isTerminal(j)
    const pausedJobs = jobs.filter(pausedTabPredicate)

    expect(pausedJobs).toEqual([actionablePaused])
    expect(isTerminal(historicalCancelledPaused)).toBe(true)
    expect(isTerminal(historicalCompletedPaused)).toBe(true)
    expect(isTerminal(historicalFailedPaused)).toBe(true)
    expect(isTerminal(actionablePaused)).toBe(false)
    expect(isTerminal(runnableQueued)).toBe(false)

    // Warteschlange tab predicate: section(j) === 'queued'
    expect(section(actionablePaused)).toBe('queued')
    expect(section(runnableQueued)).toBe('queued')
    // Historical cancelled and completed jobs belong to 'done', failed to 'failed'
    expect(section(historicalCancelledPaused)).toBe('done')
    expect(section(historicalCompletedPaused)).toBe('done')
    expect(section(historicalFailedPaused)).toBe('failed')
  })

  it('renders ErrorState with retry when initial queue summary load fails', async () => {
    stubFetch([], null, 0)

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    await waitFor(() => {
      expect(
        screen.getByText('Unerwarteter Fehler'),
      ).toBeDefined()
    })
  })

  it('preserves last known queue summary and displays warning banner when background refresh fails', async () => {
    let summaryResponse: QueueSummary | null = mockSummary
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      const parsed = new URL(url, 'http://localhost')

      if (parsed.pathname === '/api/v1/jobs/summary') {
        if (!summaryResponse) {
          return new Response(
            JSON.stringify({
              error: { code: 'INTERNAL_ERROR', message: 'connection lost' },
            }),
            {
              status: 500,
              headers: { 'Content-Type': 'application/json' },
            },
          )
        }
        return new Response(JSON.stringify({ data: summaryResponse }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }

      if (parsed.pathname === '/api/v1/jobs') {
        return new Response(
          JSON.stringify({
            data: [],
            meta: { total: 0, limit: 20, offset: 0 },
          }),
          {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          },
        )
      }

      if (parsed.pathname === '/api/v1/auth/me') {
        return new Response(
          JSON.stringify({ id: 'admin_1', username: 'sysadmin', role: 'admin' }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }

      if (parsed.pathname === '/api/v1/auth/status') {
        return new Response(
          JSON.stringify({ setup_required: false, authenticated: true }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }

      return new Response(JSON.stringify({}), { status: 200 })
    }) as typeof fetch

    render(
      <AuthProvider>
        <Downloads />
      </AuthProvider>,
    )

    // Initially, summary is rendered
    await waitFor(() => {
      expect(screen.getByLabelText('Warteschlangen-Status und Vorschau')).toBeDefined()
    })

    // Now summary fails on subsequent reload
    summaryResponse = null

    // Click "Aktualisieren" button
    const refreshBtn = screen.getByRole('button', { name: /Aktualisieren/ })
    refreshBtn.click()

    // Verify: Cards are still mounted (not unmounted into skeleton), and warning banner appears
    await waitFor(() => {
      expect(screen.getByLabelText('Warteschlangen-Status und Vorschau')).toBeDefined()
      expect(
        screen.getByText(/Hintergrundaktualisierung fehlgeschlagen/i),
      ).toBeDefined()
    })
  })
})
