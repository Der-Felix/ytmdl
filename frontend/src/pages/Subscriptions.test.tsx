import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { Subscriptions } from './Subscriptions'
import type { Subscription } from '@/types/api'

function subscription(overrides: Partial<Subscription> = {}): Subscription {
  return {
    id: 'sub-1',
    provider: 'deezer',
    artist_source_id: '27',
    artist_name: 'Daft Punk',
    enabled: true,
    auto_download: false,
    last_sync_at: new Date(Date.now() - 3600_000).toISOString(),
    next_sync_at: new Date(Date.now() + 3600_000).toISOString(),
    last_sync_status: 'success',
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    syncing: false,
    ...overrides,
  }
}

interface Call {
  url: string
  method: string
  body: unknown
}

let calls: Call[] = []
let originalFetch: typeof fetch
let originalEventSource: typeof EventSource

type Routes = Record<string, () => { status?: number; body: unknown }>

function stubFetch(routes: Routes): Call[] {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    calls.push({
      url,
      method,
      body: init?.body ? JSON.parse(init.body as string) : undefined,
    })

    const key = `${method} ${url.split('?')[0]}`
    const route = routes[key]
    if (!route) {
      if (originalFetch) return originalFetch(input, init)
      throw new Error(`no route for ${key}`)
    }


    const { status = 200, body } = route()
    return new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as typeof fetch
  return calls
}

function listing(items: Subscription[]): Routes {
  return {
    'GET /api/v1/subscriptions': () => ({
      body: { data: items, meta: { count: items.length } },
    }),
  }
}

beforeEach(() => {
  calls = []
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
})

afterEach(() => {
  globalThis.fetch = originalFetch
  globalThis.EventSource = originalEventSource
})

describe('Subscriptions page', () => {
  it('announces the loading state', () => {
    globalThis.fetch = (() => new Promise(() => {})) as unknown as typeof fetch

    render(<Subscriptions />)

    const region = screen.getByRole('status')
    expect(region.getAttribute('aria-busy')).toBe('true')
    expect(region.textContent).toContain('Abonnements werden geladen')
  })

  it('lists the watched artists with their state', async () => {
    stubFetch(
      listing([
        subscription(),
        subscription({
          id: 'sub-2',
          artist_source_id: '99',
          artist_name: 'Kevin MacLeod',
          provider: 'ytmusic',
          auto_download: true,
          enabled: false,
          last_sync_status: 'partial',
        }),
      ]),
    )

    render(<Subscriptions />)

    expect(await screen.findByText('Daft Punk')).toBeDefined()
    expect(screen.getByText('Kevin MacLeod')).toBeDefined()
    expect(screen.getByText('deezer')).toBeDefined()
    expect(screen.getByText('ytmusic')).toBeDefined()
    expect(screen.getByText('Erfolgreich')).toBeDefined()
    expect(screen.getByText('Teilweise')).toBeDefined()
    expect(screen.getByText('Pausiert')).toBeDefined()
    expect(screen.getByText('Auto-Download')).toBeDefined()
  })

  it('points at Discover when nothing is watched yet', async () => {
    stubFetch(listing([]))

    render(<Subscriptions />)

    expect(await screen.findByText('Noch keine Abonnements')).toBeDefined()
    expect(screen.getByRole('link', { name: 'Künstler entdecken' })).toBeDefined()
  })

  it('shows a failed listing with a retry', async () => {
    stubFetch({
      'GET /api/v1/subscriptions': () => ({
        status: 500,
        body: { error: { code: 'INTERNAL_ERROR', message: 'Datenbankfehler.' } },
      }),
    })

    render(<Subscriptions />)

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Datenbankfehler.')
    expect(screen.getByRole('button', { name: /Erneut versuchen/ })).toBeDefined()
  })

  it('starts a check for one subscription', async () => {
    const recorded = stubFetch({
      ...listing([subscription()]),
      'POST /api/v1/subscriptions/sub-1/sync': () => ({
        status: 202,
        body: { data: subscription({ syncing: true }) },
      }),
    })

    render(<Subscriptions />)
    fireEvent.click(await screen.findByRole('button', { name: /Jetzt prüfen/ }))

    await waitFor(() => {
      expect(
        recorded.some((call) => call.url.endsWith('/subscriptions/sub-1/sync')),
      ).toBe(true)
    })
    expect(await screen.findByText('Wird geprüft')).toBeDefined()
  })

  it('switches auto download on', async () => {
    const recorded = stubFetch({
      ...listing([subscription()]),
      'PATCH /api/v1/subscriptions/sub-1': () => ({
        body: { data: subscription({ auto_download: true }) },
      }),
    })

    render(<Subscriptions />)
    fireEvent.click(await screen.findByRole('checkbox'))

    await waitFor(() => {
      expect(recorded.find((call) => call.method === 'PATCH')?.body).toEqual({
        auto_download: true,
      })
    })
    expect(await screen.findByText('Auto-Download')).toBeDefined()
  })

  it('pauses and resumes a subscription', async () => {
    let enabled = true
    const recorded = stubFetch({
      'GET /api/v1/subscriptions': () => ({
        body: { data: [subscription({ enabled })], meta: { count: 1 } },
      }),
      'PATCH /api/v1/subscriptions/sub-1': () => {
        enabled = !enabled
        return { body: { data: subscription({ enabled }) } }
      },
    })

    render(<Subscriptions />)
    fireEvent.click(await screen.findByRole('button', { name: 'Pausieren' }))

    await waitFor(() => {
      expect(recorded.find((call) => call.method === 'PATCH')?.body).toEqual({
        enabled: false,
      })
    })
    expect(await screen.findByRole('button', { name: 'Fortsetzen' })).toBeDefined()
  })

  it('asks before removing a subscription, then removes it', async () => {
    let removed = false
    const recorded = stubFetch({
      'GET /api/v1/subscriptions': () => ({
        body: {
          data: removed ? [] : [subscription()],
          meta: { count: removed ? 0 : 1 },
        },
      }),
      // 204 No Content, exactly as the backend answers it.
      'DELETE /api/v1/subscriptions/sub-1': () => {
        removed = true
        return { status: 204, body: null }
      },
    })

    render(<Subscriptions />)

    // The first click only arms the confirmation; nothing is sent yet.
    fireEvent.click(
      await screen.findByRole('button', { name: 'Daft Punk entfernen' }),
    )
    expect(recorded.some((call) => call.method === 'DELETE')).toBe(false)

    fireEvent.click(
      screen.getByRole('button', { name: 'Daft Punk wirklich entfernen' }),
    )

    await waitFor(() => {
      expect(recorded.some((call) => call.method === 'DELETE')).toBe(true)
    })

    // A body-less success is still a success: the row goes and no failure is
    // reported. Asserting only that the request was sent is what let a client
    // that rejected 204 pass this test before.
    expect(await screen.findByText('Noch keine Abonnements')).toBeDefined()
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('keeps a failed action next to the row it belongs to', async () => {
    stubFetch({
      ...listing([subscription()]),
      'POST /api/v1/subscriptions/sub-1/sync': () => ({
        status: 409,
        body: {
          error: {
            code: 'ALREADY_EXISTS',
            message: 'Eine Synchronisation läuft bereits.',
          },
        },
      }),
    })

    render(<Subscriptions />)
    fireEvent.click(await screen.findByRole('button', { name: /Jetzt prüfen/ }))

    const alert = await screen.findByRole('alert')
    expect(alert.textContent).toContain('Eine Synchronisation läuft bereits.')
    // The row survives the failure.
    expect(screen.getByText('Daft Punk')).toBeDefined()
  })

  it('shows the stored error of a failed run', async () => {
    stubFetch(
      listing([
        subscription({
          last_sync_status: 'failed',
          last_error: 'Deezer hat nicht geantwortet.',
        }),
      ]),
    )

    render(<Subscriptions />)

    expect(await screen.findByText('Deezer hat nicht geantwortet.')).toBeDefined()
    expect(screen.getByText('Fehlgeschlagen')).toBeDefined()
  })
})

describe('Subscriptions header', () => {
  it('uses the German plural of "Künstler", which is "Künstler"', async () => {
    stubFetch(listing([subscription(), subscription({ id: 'sub-2' })]))

    render(<Subscriptions />)

    expect(
      await screen.findByText('2 Künstler werden auf neue Veröffentlichungen geprüft.'),
    ).toBeDefined()
  })

  it('uses the singular verb for a single subscription', async () => {
    stubFetch(listing([subscription()]))

    render(<Subscriptions />)

    expect(
      await screen.findByText('1 Künstler wird auf neue Veröffentlichungen geprüft.'),
    ).toBeDefined()
  })
})

describe('sync completion', () => {
  it('refreshes the finished row without blanking the list', async () => {
    // The page listens on the shared stream, so the test needs a real handle
    // on the EventSource it opens.
    let dispatch: ((type: string, data: unknown) => void) | null = null

    globalThis.EventSource = class {
      private listeners = new Map<string, ((e: MessageEvent) => void)[]>()
      constructor() {
        dispatch = (type, data) => {
          for (const fn of this.listeners.get(type) ?? []) {
            fn(new MessageEvent(type, { data: JSON.stringify(data) }))
          }
        }
      }
      addEventListener(type: string, fn: (e: MessageEvent) => void) {
        this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
      }
      close() {}
      onopen = null
      onerror = null
      readyState = 1
      static readonly CLOSED = 2
    } as unknown as typeof EventSource

    const recorded = stubFetch({
      ...listing([subscription({ syncing: true }), subscription({ id: 'sub-2' })]),
      'GET /api/v1/subscriptions/sub-1': () => ({
        body: {
          data: { subscription: subscription({ last_sync_status: 'partial' }) },
        },
      }),
    })

    render(<Subscriptions />)
    expect(await screen.findByText('Wird geprüft')).toBeDefined()

    dispatch?.('subscription.sync.completed', {
      type: 'subscription.sync.completed',
      time: new Date().toISOString(),
      subscription_id: 'sub-1',
    })

    // The finished row picks up its new status...
    expect(await screen.findByText('Teilweise')).toBeDefined()

    // ...through a single-row fetch, not a full listing reload, so the rest of
    // the list is never put back into its loading state.
    await waitFor(() => {
      expect(
        recorded.some((call) => call.url.includes('/subscriptions/sub-1')),
      ).toBe(true)
    })
    const listings = recorded.filter(
      (call) => call.method === 'GET' && /\/subscriptions(\?|$)/.test(call.url),
    )
    expect(listings.length).toBe(1)
    expect(screen.queryByRole('status')).toBeNull()
  })
})
