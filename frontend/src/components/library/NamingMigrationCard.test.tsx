import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { render, screen, waitFor } from '@testing-library/react'

import { NamingMigrationCard } from './NamingMigrationCard'

let originalFetch: typeof fetch
let calls: number
let reply: () => Response

const base = {
  from_scheme: 1,
  to_scheme: 2,
  planned: 10,
  moved: 4,
  skipped: 0,
  extras_carried: 1,
  folders_removed: 2,
}

function json(status: Record<string, unknown>) {
  return new Response(JSON.stringify({ data: { ...base, ...status } }))
}

beforeEach(() => {
  originalFetch = globalThis.fetch
  calls = 0
  globalThis.fetch = (async () => {
    calls += 1
    return reply()
  }) as unknown as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('NamingMigrationCard', () => {
  it('shows a running migration and asks again until it is done', async () => {
    reply = () => (calls < 2 ? json({ state: 'running' }) : json({ state: 'completed', moved: 10 }))
    render(<NamingMigrationCard refreshMs={20} />)
    await screen.findByText('Läuft')
    expect(screen.getByText('Dateinamen-Umstellung')).toBeDefined()
    await screen.findByText('Abgeschlossen')
    const seen = calls
    await new Promise((resolve) => setTimeout(resolve, 80))
    expect(calls).toBe(seen)
  })

  it('fetches a completed migration once and shows its counts', async () => {
    reply = () => json({ state: 'completed', planned: 1234, moved: 1234 })
    render(<NamingMigrationCard refreshMs={20} />)
    await screen.findByText('Abgeschlossen')
    expect(screen.getAllByText('1.234').length).toBe(2)
    await new Promise((resolve) => setTimeout(resolve, 80))
    expect(calls).toBe(1)
  })

  it('lists skipped files with a German reason and the path', async () => {
    reply = () =>
      json({
        state: 'incomplete',
        skipped: 1,
        skips: [{ path: 'Artist/Album/01.flac', target: 'Artist/Album/01 - A.flac', reason: 'target_exists' }],
      })
    render(<NamingMigrationCard />)
    await screen.findByText('Unvollständig')
    expect(screen.getByText('Übersprungene Dateien')).toBeDefined()
    expect(screen.getByText('Am Ziel liegt schon eine Datei.')).toBeDefined()
    expect(screen.getByText('Artist/Album/01.flac')).toBeDefined()
  })

  it('shows nothing when the server has no such endpoint', async () => {
    reply = () =>
      new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'nicht gefunden' } }), { status: 404 })
    const { container } = render(<NamingMigrationCard />)
    await waitFor(() => expect(calls).toBe(1))
    await waitFor(() => expect(container.textContent).toBe(''))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('shows one quiet line when no migration was needed', async () => {
    reply = () => json({ state: 'completed', planned: 0, moved: 0, extras_carried: 0, folders_removed: 0 })
    render(<NamingMigrationCard />)
    await screen.findByText(/bereits dem aktuellen Schema/)
    expect(screen.queryByText('Abgeschlossen')).toBeNull()
  })
})
