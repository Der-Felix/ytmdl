import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { PlayerProvider } from '@/contexts/PlayerContext'
import { DuplicateReviewPanel } from './DuplicateReviewPanel'
import type { LibraryTrack } from '@/types/api'

const tracks = [1, 2, 3].map((n) => ({
  id: `version-${n}`,
  title: 'Same recording',
  artists: ['Artist'],
  album: `Album ${n}`,
  album_artist: 'Artist',
  duration_ms: 180000,
  codec: 'opus',
  bitrate_kbps: 128,
  release_id: `release-${n}`,
  created_at: '',
})) as LibraryTrack[]
const group = {
  key: 'a'.repeat(64),
  fingerprint: 'b'.repeat(64),
  count: 3,
  tracks,
  outcome: '',
  preferred_track_id: '',
}
let calls: { path: string; method: string; body: any }[]
let originalFetch: typeof fetch
let failSave = false
beforeEach(() => {
  localStorage.clear()
  originalFetch = globalThis.fetch
  calls = []
  failSave = false
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), 'http://localhost').pathname
    const method = init?.method ?? 'GET'
    const body = init?.body ? JSON.parse(String(init.body)) : null
    calls.push({ path, method, body })
    if (path.endsWith('/duplicates/review') && method === 'POST') {
      return failSave
        ? new Response(
            JSON.stringify({
              error: { code: 'STALE_REPAIR', message: 'Gruppe hat sich geändert.' },
            }),
            { status: 409 },
          )
        : new Response(null, { status: 204 })
    }
    if (path.endsWith('/duplicates/remove'))
      return new Response(JSON.stringify({ data: { deleted_track_ids: body.remove_track_ids } }))
    return new Response(JSON.stringify({ data: path.endsWith('/duplicates') ? [group] : [] }))
  }) as typeof fetch
})
afterEach(() => {
  globalThis.fetch = originalFetch
})
const writes = () => calls.filter((c) => c.method !== 'GET')
const removals = () => calls.filter((c) => c.path.endsWith('/duplicates/remove'))
async function setup(admin = true) {
  render(
    <PlayerProvider>
      <DuplicateReviewPanel isAdmin={admin} />
    </PlayerProvider>,
  )
  await screen.findByRole('button', { name: 'Diese bevorzugen' })
}

describe('Duplicate comparison consent and selection', () => {
  it('protects the preferred version and deletes only explicitly confirmed checkboxes', async () => {
    await setup()
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    await screen.findByText(/Vergleich 2 von 2/)
    expect(writes()).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    const dialog = await screen.findByRole('dialog')
    expect(writes()).toHaveLength(1)
    expect(writes()[0]!.body.preferred_track_id).toBe('version-2')
    expect(removals()).toHaveLength(0)
    expect(
      within(dialog).queryByRole('checkbox', { name: 'Album 2 zum Löschen auswählen' }),
    ).toBeNull()
    fireEvent.click(within(dialog).getByRole('checkbox', { name: 'Album 3 zum Löschen auswählen' }))
    fireEvent.click(within(dialog).getByRole('button', { name: '1 Versionen endgültig löschen' }))
    await waitFor(() => expect(removals()).toHaveLength(1))
    expect(removals()[0]!.body.remove_track_ids).toEqual(['version-1'])
    expect(removals()[0]!.body.confirmed).toBe(true)
  })
  it('canceling the deletion dialog keeps every file and the saved preference', async () => {
    await setup()
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByText(/Vergleich 2 von 2/)
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    const dialog = await screen.findByRole('dialog')
    fireEvent.click(within(dialog).getByRole('button', { name: 'Alle Dateien behalten' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(removals()).toHaveLength(0)
    expect(writes()).toHaveLength(1)
  })
  it('supports undo, skip and keeping distinct versions without deletion', async () => {
    await setup(false)
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    await screen.findByText(/Vergleich 2 von 2/)
    fireEvent.click(screen.getByRole('button', { name: 'Auswahl zurück' }))
    expect(screen.getByText(/Vergleich 1 von 2/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Alle Versionen behalten' }))
    await screen.findByText('Alle Versionen bleiben erhalten. Die Gruppe ist erledigt.')
    expect(writes()[0]!.body.outcome).toBe('distinct')
    expect(writes()[0]!.body.preferred_track_id).toBe('')
    expect(removals()).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Neu vergleichen' }))
    await screen.findByRole('button', { name: 'Später prüfen' })
    const count = writes().length
    fireEvent.click(screen.getByRole('button', { name: 'Später prüfen' }))
    expect(writes()).toHaveLength(count)
  })
  it('non-admin preferences never expose a destructive action', async () => {
    await setup(false)
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    await screen.findByText(/Vergleich 2 von 2/)
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    await screen.findByText('Bevorzugte Version gespeichert. Bisher wurde nichts gelöscht.')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(screen.queryByRole('button', { name: /löschen/ })).toBeNull()
    expect(removals()).toHaveLength(0)
  })
  it('stale choice errors cannot open deletion confirmation', async () => {
    await setup()
    failSave = true
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByText(/Vergleich 2 von 2/)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByText('Gruppe hat sich geändert.')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(removals()).toHaveLength(0)
    expect(screen.getByRole('button', { name: 'Gruppe neu laden' })).toBeTruthy()
  })
  it('direct mode deletes only after the final choice, without opening another dialog', async () => {
    await setup()
    const setting = screen.getByRole('switch', { name: 'Vor dem Löschen nachfragen' })
    expect((setting as HTMLInputElement).checked).toBe(true)
    fireEvent.click(setting)
    expect((setting as HTMLInputElement).checked).toBe(false)
    expect(screen.getByText(/Direktmodus: Nach der letzten Auswahl/)).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    await screen.findByText(/Vergleich 2 von 2/)
    expect(removals()).toHaveLength(0)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByText('2 Versionen gelöscht. Die bevorzugte Version bleibt erhalten.')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(removals()).toHaveLength(1)
    expect(removals()[0]!.body.preferred_track_id).toBe('version-2')
    expect(removals()[0]!.body.remove_track_ids).toEqual(['version-1', 'version-3'])
  })
  it('re-enabling confirmation stops automatic deletion', async () => {
    await setup()
    const setting = screen.getByRole('switch', { name: 'Vor dem Löschen nachfragen' })
    fireEvent.click(setting)
    fireEvent.click(setting)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByText(/Vergleich 2 von 2/)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByRole('dialog')
    expect(removals()).toHaveLength(0)
  })
  it('locks confirmation and keep/skip actions while a swipe decision is pending', async () => {
    await setup()
    const setting = screen.getByRole('switch', { name: 'Vor dem Löschen nachfragen' }) as HTMLInputElement
    fireEvent.click(setting)
    fireEvent.click(screen.getByRole('button', { name: 'Diese bevorzugen' }))
    expect(setting.disabled).toBe(true)
    for (const name of ['Alle Versionen behalten', 'Später prüfen', 'Tabelle', 'Neu laden']) {
      const control = screen.getByRole('button', { name, exact: true }) as HTMLButtonElement
      expect(control.disabled).toBe(true)
      fireEvent.click(control)
    }
    expect(writes()).toHaveLength(0)
    await screen.findByText(/Vergleich 2 von 2/)
    expect(setting.disabled).toBe(false)
    fireEvent.click(setting)
    fireEvent.click(screen.getByRole('button', { name: 'Bisherige behalten' }))
    await screen.findByRole('dialog')
    expect(removals()).toHaveLength(0)
  })
  it('rapid repeated choices cannot skip an undecided version', async () => {
    await setup()
    const choose = screen.getByRole('button', { name: 'Diese bevorzugen' })
    fireEvent.click(choose)
    fireEvent.click(choose)
    await screen.findByText(/Vergleich 2 von 2/)
    expect(writes()).toHaveLength(0)
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
