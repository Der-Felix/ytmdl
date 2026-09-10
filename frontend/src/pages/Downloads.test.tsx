import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { cleanup, render, screen, waitFor } from '@testing-library/react'

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
  globalThis.EventSource = class {
    close() {}
    addEventListener() {}
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
        screen.getByText(/not available/i),
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
