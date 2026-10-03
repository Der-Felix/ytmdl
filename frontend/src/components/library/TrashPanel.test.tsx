import { beforeEach, afterEach, describe, it, expect } from 'bun:test'
import {
  render,
  screen,
  fireEvent,
  within,
  waitFor,
} from '@testing-library/react'
import { TrashPanel } from './TrashPanel'
let originalFetch: typeof fetch
let calls: { path: string; method: string; body: any }[]
let failRestore = false
const entry = {
  id: 'trash-fixture',
  track_id: 'track-fixture',
  title: 'Testaufnahme',
  created_at: '2026-10-03T00:00:00Z',
  expires_at: '2026-10-10T00:00:00Z',
  state: 'ready',
}
beforeEach(() => {
  originalFetch = globalThis.fetch
  calls = []
  failRestore = false
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const method = init?.method || 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : null
    calls.push({ path, method, body })
    if (method === 'GET') return new Response(JSON.stringify({ data: [entry] }))
    if (failRestore && path.endsWith('/restore'))
      return new Response(
        JSON.stringify({
          error: {
            code: 'CONFLICT',
            message: 'Vorhandene Datei wird nicht überschrieben.',
          },
        }),
        { status: 409 },
      )
    return new Response(null, { status: 204 })
  }) as typeof fetch
})
afterEach(() => {
  globalThis.fetch = originalFetch
})
describe('Trash restoration and permanent consent', () => {
  it('opens a separate permanent dialog and cancellation performs no write', async () => {
    render(<TrashPanel />)
    await screen.findByText('Testaufnahme')
    fireEvent.click(
      screen.getByRole('button', { name: 'Endgültig löschen', exact: true }),
    )
    const dialog = await screen.findByRole('dialog')
    expect(calls.filter((c) => c.method !== 'GET')).toHaveLength(0)
    fireEvent.click(within(dialog).getByRole('button', { name: 'Abbrechen' }))
    expect(calls.filter((c) => c.method !== 'GET')).toHaveLength(0)
  })
  it('sends permanent consent only after the final dialog button', async () => {
    render(<TrashPanel />)
    await screen.findByText('Testaufnahme')
    fireEvent.click(
      screen.getByRole('button', { name: 'Endgültig löschen', exact: true }),
    )
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(
      within(dialog).getByRole('button', { name: 'Dateien endgültig löschen' }),
    )
    await screen.findByText('Papierkorbeintrag endgültig gelöscht.')
    const writes = calls.filter((c) => c.method !== 'GET')
    expect(writes).toHaveLength(1)
    expect(writes[0]!.path).toBe('/api/v1/library/trash/trash-fixture/purge')
    expect(writes[0]!.body).toEqual({ confirmed: true })
  })
  it('keeps a failed restoration visible and allows retry', async () => {
    failRestore = true
    render(<TrashPanel />)
    await screen.findByText('Testaufnahme')
    fireEvent.click(
      screen.getByRole('button', { name: 'Wiederherstellen', exact: true }),
    )
    await screen.findByRole('alert')
    expect(screen.getByRole('alert').textContent).toContain(
      'nicht überschrieben',
    )
    failRestore = false
    await waitFor(() =>
      expect(
        (
          screen.getByRole('button', {
            name: 'Wiederherstellen',
            exact: true,
          }) as HTMLButtonElement
        ).disabled,
      ).toBe(false),
    )
    fireEvent.click(
      screen.getByRole('button', { name: 'Wiederherstellen', exact: true }),
    )
    await screen.findByText(
      'Titel, Favoriten und Playlist-Zuordnungen wiederhergestellt.',
    )
    expect(calls.filter((c) => c.path.endsWith('/restore'))).toHaveLength(2)
  })
})
