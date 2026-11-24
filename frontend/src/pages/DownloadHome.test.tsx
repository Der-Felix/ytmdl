import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { render, screen, waitFor, within } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import type { Job, NextUpJob, QueueSummary } from '@/types/api'

const { ACTIVE_LIMIT, DownloadHome } = await import('./DownloadHome')

function job(id: string, status: Job['status']): Job {
  return {
    id,
    type: 'artist',
    status,
    priority: 'normal',
    paused: false,
    label: `Job ${id}`,
    metadata_provider: 'deezer',
    media_provider: 'youtube',
    target_id: `target-${id}`,
    total: 10,
    completed: status === 'completed' ? 10 : 2,
    failed: 0,
    skipped: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  } as Job
}

const next = (id: string): NextUpJob => ({
  job_id: id,
  artist: `Artist ${id}`,
  release: `Release ${id}`,
  open_tracks: 8,
  total_tracks: 10,
})

function summary(overrides: Partial<QueueSummary>): QueueSummary {
  return {
    active_jobs: 2,
    queued_jobs: 1641,
    active_items: 2,
    remaining_items: 9000,
    paused_jobs: 0,
    retry_wait_items: 0,
    completed_last_hour: 10,
    throughput_items_per_hour: 10,
    eta_seconds: null,
    eta_confidence: 'low',
    eta_text: '',
    total_relevant: 0,
    completed_relevant: 0,
    storage_healthy: true,
    current: [],
    next: [],
    ...overrides,
  }
}

const auth = {
  user: {
    id: 'usr-admin',
    username: 'admin',
    display_name: 'Administrator',
    role: 'admin' as const,
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    last_login_at: null,
  },
  loading: false,
  setupRequired: false,
  isAdmin: true,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
  checkSetup: async () => {},
}

let requests: URL[] = []
let jobsByView: Record<string, Job[]> = {}
let queue: QueueSummary = summary({})
let originalFetch: typeof fetch
let originalEventSource: typeof EventSource

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })

beforeEach(() => {
  requests = []
  jobsByView = {}
  queue = summary({})
  originalFetch = globalThis.fetch
  originalEventSource = globalThis.EventSource
  globalThis.EventSource = class {
    close() {}
    addEventListener() {}
    static readonly CLOSED = 2
  } as unknown as typeof EventSource
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost')
    requests.push(url)
    switch (url.pathname) {
      case '/api/v1/jobs': {
        const items = jobsByView[url.searchParams.get('view') ?? ''] ?? []
        return json({ data: items.slice(0, Number(url.searchParams.get('limit'))), meta: { count: items.length, total: items.length } })
      }
      case '/api/v1/jobs/summary':
        return json({ data: queue })
      case '/api/v1/library/stats':
        return json({ data: { total_artists: 1, total_releases: 1, total_tracks: 1 } })
      case '/api/v1/library/releases':
        return json({ data: [], meta: { count: 0 } })
      case '/api/v1/health':
        return json({ data: { status: 'ok', version: '1.9.0', checks: {} } })
      default:
        return json({ data: null })
    }
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
  globalThis.EventSource = originalEventSource
})

function renderPage() {
  return render(
    <AuthContext.Provider value={auth}>
      <DownloadHome />
    </AuthContext.Provider>,
  )
}

describe('Laden start page', () => {
  it('requests only the jobs it shows', async () => {
    renderPage()
    await waitFor(() => expect(requests.filter((url) => url.pathname === '/api/v1/jobs')).toHaveLength(2))
    const params = requests
      .filter((url) => url.pathname === '/api/v1/jobs')
      .map((url) => `${url.searchParams.get('view')}:${url.searchParams.get('limit')}`)
      .sort()
    expect(params).toEqual([`active:${ACTIVE_LIMIT}`, 'done:4'])
  })

  it('caps the active downloads and names the rest of the queue', async () => {
    jobsByView.active = [job('run-1', 'downloading'), job('run-2', 'matching')]
    queue = summary({ next: [next('run-1'), next('q-1'), next('q-2'), next('q-3'), next('q-4'), next('q-5')] })
    renderPage()

    expect(await screen.findByText('Job run-1')).toBeDefined()
    const nextUp = (await screen.findByText('Als Nächstes')).closest('[data-slot="panel"]') as HTMLElement
    // Two running jobs, then the next three of the queue: five in all. The
    // running job that is also first in the queue is listed once.
    expect(within(nextUp).getAllByRole('link').map((link) => link.textContent)).toEqual([
      expect.stringContaining('Release q-1'),
      expect.stringContaining('Release q-2'),
      expect.stringContaining('Release q-3'),
    ])
    expect(screen.queryByText('Release run-1')).toBeNull()
    expect(screen.getByText('1.643 offene Jobs')).toBeDefined()
    const more = screen.getByRole('link', { name: '1.638 weitere Jobs' })
    expect(more.getAttribute('href')).toBe('/downloads')
    expect(screen.getByText('Alle Downloads').closest('a')?.getAttribute('href')).toBe('/downloads')
  })

  it('lists the last finished jobs under Zuletzt geladen', async () => {
    jobsByView.done = [job('done-1', 'completed')]
    renderPage()
    const recent = (await screen.findByRole('heading', { name: 'Zuletzt geladen' })).closest('[data-slot="panel"]') as HTMLElement
    expect(await within(recent).findByText('Job done-1')).toBeDefined()
  })

  it('says so when nothing is running', async () => {
    queue = summary({ active_jobs: 0, queued_jobs: 0 })
    renderPage()
    expect(await screen.findByText('Zurzeit läuft kein Download.')).toBeDefined()
  })
})
