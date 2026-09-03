import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { SubscribeControl } from './SubscribeControl'
import type { Artist } from '@/types/api'

const artist: Artist = {
  id: 'internal-1',
  name: 'Daft Punk',
  provider: 'deezer',
  source_id: '27',
  source_url: 'https://www.deezer.com/artist/27',
  image_url: '',
}

const storedSubscription = {
  id: 'sub-1',
  provider: 'deezer',
  artist_source_id: '27',
  artist_name: 'Daft Punk',
  enabled: true,
  auto_download: false,
  next_sync_at: new Date(Date.now() + 3600_000).toISOString(),
  last_sync_status: 'pending',
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  syncing: false,
}

/** One recorded request, so a test can assert what the button actually sent. */
interface Call {
  url: string
  method: string
  body: unknown
}

let calls: Call[] = []
let originalFetch: typeof fetch
let originalEventSource: typeof EventSource

/**
 * Routes every request the control can make. Each route is a function so a
 * test can decide per call what comes back.
 */
type Routes = Record<string, () => { status?: number; body: unknown }>

function stubFetch(routes: Routes) {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    calls.push({
      url,
      method,
      body: init?.body ? JSON.parse(init.body as string) : undefined,
    })

    const key = `${method} ${url.split('?')[0]}`
    const route = routes[key] ?? routes[`${method} *`]
    if (!route) {
      if (originalFetch) return originalFetch(input, init)
      throw new Error(`no route for ${key}`)
    }

    const { status = 200, body } = route()
    return new Response(JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as typeof fetch

  return calls
}

beforeEach(() => {
  calls = []
  originalFetch = globalThis.fetch
  originalEventSource = globalThis.EventSource

  // The control subscribes to the shared event stream. happy-dom has no
  // EventSource, and the tests are not about the stream, so it is stubbed out.
  globalThis.EventSource = class {
    close() {}
    addEventListener() {}
    onopen = null
    onerror = null
    readyState = 0
    static readonly CLOSED = 2
  } as unknown as typeof EventSource
})

afterEach(() => {
  globalThis.fetch = originalFetch
  globalThis.EventSource = originalEventSource
})

const notSubscribed: Routes = {
  'GET /api/v1/subscriptions': () => ({ body: { data: [], meta: { count: 0 } } }),
}

const alreadySubscribed: Routes = {
  'GET /api/v1/subscriptions': () => ({
    body: { data: [storedSubscription], meta: { count: 1 } },
  }),
}

describe('SubscribeControl', () => {
  it('shows a loading placeholder while the state is unknown', () => {
    // A request that never settles keeps the control in its loading state.
    globalThis.fetch = (() => new Promise(() => {})) as unknown as typeof fetch

    const { container } = render(<SubscribeControl artist={artist} />)

    expect(screen.queryByRole('button')).toBeNull()
    expect(container.querySelector('[data-slot="skeleton"], .animate-pulse')).not.toBeNull()
  })

  it('offers to subscribe when the artist is not watched', async () => {
    stubFetch(notSubscribed)
    render(<SubscribeControl artist={artist} />)

    const button = await screen.findByRole('button', { name: 'Abonnieren' })
    expect(button).toBeDefined()
    // Nothing to configure until the artist is actually watched.
    expect(screen.queryByRole('checkbox')).toBeNull()
  })

  it('sends the artist on the provider it was found on', async () => {
    const recorded = stubFetch({
      ...notSubscribed,
      'POST /api/v1/subscriptions': () => ({
        status: 201,
        body: { data: storedSubscription },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Abonnieren' }))

    await waitFor(() => {
      expect(recorded.some((call) => call.method === 'POST')).toBe(true)
    })
    const post = recorded.find((call) => call.method === 'POST')
    expect(post?.body).toEqual({
      provider: 'deezer',
      artist_source_id: '27',
      artist_name: 'Daft Punk',
      artist_image_url: '',
    })
  })

  it('shows the subscribed state for a watched artist', async () => {
    stubFetch(alreadySubscribed)
    render(<SubscribeControl artist={artist} />)

    expect(await screen.findByRole('button', { name: /Abonniert/ })).toBeDefined()
    expect(screen.getByRole('button', { name: /Jetzt prüfen/ })).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Abonnieren' })).toBeNull()
  })

  it('starts a check when the sync button is used', async () => {
    const recorded = stubFetch({
      ...alreadySubscribed,
      'POST /api/v1/subscriptions/sub-1/sync': () => ({
        status: 202,
        body: { data: { ...storedSubscription, syncing: true } },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    fireEvent.click(await screen.findByRole('button', { name: /Jetzt prüfen/ }))

    await waitFor(() => {
      expect(
        recorded.some((call) => call.url.endsWith('/subscriptions/sub-1/sync')),
      ).toBe(true)
    })
    // While a run is in flight the button must not offer to start another.
    expect(await screen.findByRole('button', { name: /Wird geprüft/ })).toBeDefined()
  })

  it('switches auto download on through the toggle', async () => {
    const recorded = stubFetch({
      ...alreadySubscribed,
      'PATCH /api/v1/subscriptions/sub-1': () => ({
        body: { data: { ...storedSubscription, auto_download: true } },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    const toggle = await screen.findByRole('checkbox')
    expect(toggle.getAttribute('aria-checked')).toBe('false')

    fireEvent.click(toggle)

    await waitFor(() => {
      expect(recorded.some((call) => call.method === 'PATCH')).toBe(true)
    })
    expect(recorded.find((call) => call.method === 'PATCH')?.body).toEqual({
      auto_download: true,
    })
    await waitFor(() => {
      expect(screen.getByRole('checkbox').getAttribute('aria-checked')).toBe('true')
    })
  })

  it('pauses a subscription without unsubscribing', async () => {
    const recorded = stubFetch({
      ...alreadySubscribed,
      'PATCH /api/v1/subscriptions/sub-1': () => ({
        body: { data: { ...storedSubscription, enabled: false } },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    fireEvent.click(await screen.findByRole('button', { name: /Abonniert/ }))

    await waitFor(() => {
      expect(recorded.find((call) => call.method === 'PATCH')?.body).toEqual({
        enabled: false,
      })
    })
    // Still watched, only paused — the subscribe button must not come back.
    expect(await screen.findByRole('button', { name: /Pausiert/ })).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Abonnieren' })).toBeNull()
  })

  it('reports a failure without losing the button', async () => {
    stubFetch({
      ...notSubscribed,
      'POST /api/v1/subscriptions': () => ({
        status: 502,
        body: {
          error: {
            code: 'PROVIDER_UNAVAILABLE',
            message: 'Deezer hat nicht geantwortet.',
          },
        },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    fireEvent.click(await screen.findByRole('button', { name: 'Abonnieren' }))

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Deezer hat nicht geantwortet.')
    expect(screen.getByRole('button', { name: 'Abonnieren' })).toBeDefined()
  })

  it('reports a failed lookup instead of pretending the artist is unwatched', async () => {
    stubFetch({
      'GET /api/v1/subscriptions': () => ({
        status: 500,
        body: { error: { code: 'INTERNAL_ERROR', message: 'Datenbankfehler.' } },
      }),
    })
    render(<SubscribeControl artist={artist} />)

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Datenbankfehler.')
  })
})
