import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { CompleteReleasesCard } from './CompleteReleasesCard'
import { Toaster } from '@/components/ui/toast'
import type { CompleteReleasesResult, CompleteReleasesStatus } from '@/types/api'

interface Call {
  method: string
  path: string
}

let originalFetch: typeof fetch
let calls: Call[]
let routes: Record<string, (call: number) => Response>

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify({ data }), { status })
}

function failure(status: number, code: string) {
  return new Response(JSON.stringify({ error: { code, message: 'English server text' } }), { status })
}

const inOneDay = () => new Date(Date.now() + 86_400_000).toISOString()

function result(overrides: Partial<CompleteReleasesResult> = {}): CompleteReleasesResult {
  return {
    releases_examined: 168,
    releases_incomplete: 2,
    releases_without_source: 4,
    releases_failed: 3,
    failed_by_code: { PROVIDER_NOT_FOUND: 2, RELEASE_NOT_FOUND: 1 },
    copies: 5,
    bytes: 15_800_000,
    held_tracks: 1830,
    unknown_tracks: 64,
    free_bytes: 123_456_789_012,
    releases: [
      { release_id: 'r1', artist: 'ABBA', title: 'Arrival', total: 12, held: 7, copies: 3, unknown: 2, bytes: 9_000_000 },
      { release_id: 'r2', artist: 'ABBA', title: 'Gold', total: 19, held: 17, copies: 2, unknown: 0, bytes: 6_800_000 },
    ],
    releases_truncated: false,
    ...overrides,
  }
}

function status(overrides: Partial<CompleteReleasesStatus> = {}): CompleteReleasesStatus {
  return { state: 'idle', setting_enabled: true, progress: { done: 0, total: 0 }, ...overrides }
}

function scanned(overrides: Partial<CompleteReleasesResult> = {}, extra: Partial<CompleteReleasesStatus> = {}) {
  return status({
    state: 'scanned',
    progress: { done: 168, total: 168 },
    started_at: '2026-11-22T10:00:00Z',
    finished_at: '2026-11-22T10:05:00Z',
    expires_at: inOneDay(),
    result: result(overrides),
    ...extra,
  })
}

function serve(table: Record<string, (call: number) => Response>) {
  routes = table
}

beforeEach(() => {
  originalFetch = globalThis.fetch
  calls = []
  routes = {}
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === 'string' ? input : input.toString(), 'http://localhost')
    const method = init?.method ?? 'GET'
    const key = `${method} ${url.pathname}`
    calls.push({ method, path: url.pathname })
    const handler = routes[key]
    if (!handler) return failure(500, 'INTERNAL_ERROR')
    return handler(calls.filter((call) => `${call.method} ${call.path}` === key).length)
  }) as unknown as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

const STATUS = 'GET /api/v1/library/complete-releases'

describe('CompleteReleasesCard', () => {
  it('offers a scan to an administrator when nothing was scanned yet', async () => {
    serve({
      [STATUS]: () => json(status()),
      'POST /api/v1/library/complete-releases/scan': () =>
        json(status({ state: 'scanning', progress: { done: 0, total: 168 } }), 202),
    })
    render(<CompleteReleasesCard isAdmin refreshMs={10_000} />)

    expect(await screen.findByText('Nicht geprüft')).toBeDefined()
    fireEvent.click(screen.getByRole('button', { name: 'Prüfen' }))

    expect(await screen.findByText('Prüft')).toBeDefined()
    expect(screen.getByText('0 / 168')).toBeDefined()
    expect(screen.getByRole('button', { name: 'Abbrechen' })).toBeDefined()
    expect(calls.some((call) => call.method === 'POST' && call.path.endsWith('/complete-releases/scan'))).toBe(true)
  })

  it('shows a read-only card without actions to a non-administrator', async () => {
    serve({ [STATUS]: () => json(scanned()) })
    render(<CompleteReleasesCard isAdmin={false} />)

    expect(await screen.findByText('Lücken gefunden')).toBeDefined()
    expect(screen.getByText('Prüfen und Auffüllen können nur Administratoren.')).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Auffüllen' })).toBeNull()
    expect(screen.queryByRole('button', { name: /prüfen/i })).toBeNull()
  })

  it('polls a running scan with its progress and stops once it is done', async () => {
    serve({
      [STATUS]: (n) =>
        n < 3 ? json(status({ state: 'scanning', progress: { done: n * 40, total: 168 } })) : json(scanned()),
    })
    render(<CompleteReleasesCard isAdmin refreshMs={20} />)

    expect(await screen.findByText('Prüft')).toBeDefined()
    expect(screen.getByRole('progressbar', { name: 'Fortschritt der Prüfung' })).toBeDefined()
    await screen.findByText('Lücken gefunden')
    // A tick that fired while the last answer was on its way may still land.
    await new Promise((resolve) => setTimeout(resolve, 40))
    const seen = calls.length
    await new Promise((resolve) => setTimeout(resolve, 80))
    expect(calls.length).toBe(seen)
  })

  it('cancels a running scan', async () => {
    serve({
      [STATUS]: () => json(status({ state: 'scanning', progress: { done: 10, total: 168 } })),
      'DELETE /api/v1/library/complete-releases/scan': () =>
        json(status({ state: 'cancelled', error_code: 'JOB_CANCELLED', error: 'The scan was cancelled.' })),
    })
    render(<CompleteReleasesCard isAdmin refreshMs={10_000} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Abbrechen' }))
    expect(await screen.findByText('Die Prüfung wurde abgebrochen.')).toBeDefined()
    expect(screen.getByRole('button', { name: 'Erneut prüfen' })).toBeDefined()
  })

  it('shows the figures, the explanations and the affected releases of a scan', async () => {
    serve({ [STATUS]: () => json(scanned()) })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByText('Lücken gefunden')).toBeDefined()
    expect(screen.getByText('Unvollständige Releases')).toBeDefined()
    expect(screen.getByText('15,8 MB')).toBeDefined()
    expect(screen.getByText('123 GB')).toBeDefined()
    expect(screen.getByText('1.830 Songs liegen schon im richtigen Release.')).toBeDefined()
    expect(screen.getByText('64 Songs fehlen der Bibliothek ganz und werden hierbei nicht geladen.')).toBeDefined()
    expect(
      screen.getByText(
        '3 Releases konnten nicht geprüft werden: 2 × Provider nicht eingerichtet, 1 × beim Provider nicht gefunden.',
      ),
    ).toBeDefined()
    expect(screen.getByText('Arrival')).toBeDefined()
    expect(screen.getByText('7 von 12')).toBeDefined()
    expect(screen.getByText('+3')).toBeDefined()
    expect(screen.getByText('3 Kopien')).toBeDefined()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(screen.queryByText(/Angezeigt werden/)).toBeNull()
    expect(screen.getByRole('button', { name: 'Erneut prüfen' })).toBeDefined()
  })

  it('warns when the copies need more space than is free', async () => {
    serve({ [STATUS]: () => json(scanned({ bytes: 512_000_000, free_bytes: 400_000_000 })) })
    render(<CompleteReleasesCard isAdmin />)

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Der freie Speicher reicht nicht')
    expect(alert.textContent).toContain('512 MB')
    expect(alert.textContent).toContain('400 MB')
  })

  it('says when the list of releases is cut short', async () => {
    serve({ [STATUS]: () => json(scanned({ releases_incomplete: 241, releases_truncated: true })) })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByText('Angezeigt werden 2 von 241 Releases. Auffüllen erfasst alle.')).toBeDefined()
  })

  it('shows a calm state without a fill action when nothing is incomplete', async () => {
    serve({
      [STATUS]: () =>
        json(scanned({ releases_incomplete: 0, copies: 0, bytes: 0, releases: [], releases_failed: 0, failed_by_code: {} })),
    })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByText('Vollständig')).toBeDefined()
    expect(screen.getByText(/Keine Kopien nötig/)).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Auffüllen' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Erneut prüfen' })).toBeDefined()
  })

  it('fills after confirmation and then points to the downloads', async () => {
    serve({
      [STATUS]: (n) => (n === 1 ? json(scanned()) : json(scanned({}, { expires_at: undefined, applied_at: '2026-11-22T10:06:00Z' }))),
      'POST /api/v1/library/complete-releases/apply': () =>
        json({ queued_jobs: 2, already_queued: 1, failed_jobs: 0, copies: 5, bytes: 15_800_000 }),
    })
    render(
      <>
        <CompleteReleasesCard isAdmin />
        <Toaster />
      </>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Auffüllen' }))
    const dialog = await screen.findByRole('dialog')
    expect(dialog.textContent).toContain('Für 2 Releases werden 5 Dateien angelegt, zusammen etwa 15,8 MB.')
    expect(dialog.textContent).toContain('lassen sich dort abbrechen')
    // Nothing is sent before the confirmation.
    expect(calls.some((call) => call.path.endsWith('/apply'))).toBe(false)

    fireEvent.click(within(dialog).getByRole('button', { name: 'Auffüllen' }))

    expect(await screen.findByText('Aufgefüllt')).toBeDefined()
    const card = screen.getByRole('region', { name: /Alben vervollständigen/ })
    expect(within(card).getByText('2 Aufträge unter Downloads eingereiht.')).toBeDefined()
    expect(within(card).getByText('1 Release hatte schon einen offenen Auftrag.')).toBeDefined()
    expect(within(card).getByRole('link', { name: /Zu den Downloads/ }).getAttribute('href')).toBe('/downloads')
    expect((await screen.findAllByText('Auffüllen gestartet')).length).toBeGreaterThan(0)
    expect(calls.filter((call) => call.path.endsWith('/apply')).length).toBe(1)
  })

  it('leads to a new scan when the server refuses the apply as stale', async () => {
    serve({
      [STATUS]: () => json(scanned()),
      'POST /api/v1/library/complete-releases/apply': () => failure(409, 'STALE_REPAIR'),
    })
    render(
      <>
        <CompleteReleasesCard isAdmin />
        <Toaster />
      </>,
    )

    fireEvent.click(await screen.findByRole('button', { name: 'Auffüllen' }))
    fireEvent.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Auffüllen' }))

    expect((await screen.findAllByText('Ergebnis ist nicht mehr gültig')).length).toBeGreaterThan(0)
    expect(await screen.findByText('Veraltet')).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Auffüllen' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Erneut prüfen' })).toBeDefined()
  })

  it('treats a result past its expiry as stale', async () => {
    serve({ [STATUS]: () => json(scanned({}, { expires_at: '2026-01-01T00:00:00Z' })) })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByText('Veraltet')).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Auffüllen' })).toBeNull()
  })

  it('explains a failed scan in German and offers another', async () => {
    serve({
      [STATUS]: () => json(status({ state: 'failed', error_code: 'STORAGE_UNAVAILABLE', error: 'English server text' })),
    })
    render(<CompleteReleasesCard isAdmin />)

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Bibliotheksspeicher nicht erreichbar')
    expect(alert.textContent).not.toContain('English server text')
    expect(screen.getByRole('button', { name: 'Erneut prüfen' })).toBeDefined()
  })

  it('says so when new downloads do not keep releases complete', async () => {
    serve({ [STATUS]: () => json(status({ setting_enabled: false })) })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByText(/„Alben vollständig halten“ ist ausgeschaltet/)).toBeDefined()
  })

  it('shows nothing when the server has no such endpoint', async () => {
    serve({ [STATUS]: () => failure(404, 'NOT_FOUND') })
    const { container } = render(<CompleteReleasesCard isAdmin />)

    await waitFor(() => expect(calls.length).toBe(1))
    await waitFor(() => expect(container.textContent).toBe(''))
  })

  it('offers a retry when the status cannot be loaded', async () => {
    serve({ [STATUS]: () => failure(500, 'INTERNAL_ERROR') })
    render(<CompleteReleasesCard isAdmin />)

    expect(await screen.findByRole('button', { name: /Erneut versuchen/ })).toBeDefined()
  })
})
