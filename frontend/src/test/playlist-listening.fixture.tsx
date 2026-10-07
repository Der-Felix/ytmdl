import { afterEach, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { AuthProvider } from '@/contexts/AuthContext'
import { PlayerProvider } from '@/contexts/PlayerContext'
import { Playlists } from '@/pages/Playlists'
import { PlaylistDetail } from '@/pages/PlaylistDetail'
import { PlaylistArtwork } from '@/components/music/PlaylistArtwork'
import { playlistPreview } from '@/lib/playlist-artwork'

const originalFetch = globalThis.fetch
afterEach(() => { globalThis.fetch = originalFetch })
const tracks = [1, 2, 3, 4, 5].map((n) => ({ id: `track-${n}`, title: `Song ${n}`, artists: [`Artist ${n}`], album: `Album ${n}`, release_id: `release-${n}`, duration_ms: 120000, position: n }))
const playlists = Array.from({ length: 25 }, (_, n) => ({ id: `list-${n}`, name: `Playlist ${n}`, description: '', track_count: 5, duration_ms: 600000, updated_at: new Date().toISOString() }))

it('builds a bounded collage with distinct same-origin artwork and an empty fallback', () => {
  expect(playlistPreview([...tracks.slice(0, 1), { ...tracks[1]!, release_id: 'release-1' }, ...tracks.slice(2)]).map(t => t.id)).toEqual(['track-1', 'track-3', 'track-4', 'track-5'])
  const { rerender } = render(<PlaylistArtwork name="Fixture" tracks={tracks} />)
  const cover = screen.getByLabelText('Cover der Playlist Fixture')
  expect(cover.querySelectorAll('img').length).toBe(4)
  expect(cover.querySelector('img')?.getAttribute('src')).toBe('/api/v1/library/tracks/track-1/artwork')
  rerender(<PlaylistArtwork name="Fixture" tracks={[]} />)
  expect(screen.getByLabelText('Cover der Playlist Fixture').querySelectorAll('img').length).toBe(0)
})

it('bounds preview loading to a page, filters playlists and exposes play failures', async () => {
  const detailReads: string[] = []
  let fail = false
  globalThis.fetch = (async (input) => {
    const url = String(input)
    if (url.endsWith('/playlists')) return new Response(JSON.stringify({ data: playlists }))
    detailReads.push(url)
    if (fail) return new Response(JSON.stringify({ error: { code: 'PROVIDER_UNAVAILABLE', message: 'Playlist konnte nicht geladen werden.' } }), { status: 503 })
    return new Response(JSON.stringify({ data: { ...playlists[0], tracks } }))
  }) as typeof fetch
  render(<PlayerProvider><Playlists /></PlayerProvider>)
  await screen.findByText('Playlist 0')
  await waitFor(() => expect(detailReads.length).toBe(12))
  expect(screen.queryByText('Playlist 24')).toBeNull()
  fireEvent.change(screen.getByLabelText('Playlists durchsuchen'), { target: { value: 'Playlist 24' } })
  await screen.findByText('Playlist 24')
  await waitFor(() => expect(detailReads.some(url => url.endsWith('/list-24'))).toBe(true))
  fail = true
  fireEvent.click(screen.getByRole('button', { name: 'Playlist 24 abspielen' }))
  expect((await screen.findByRole('alert')).textContent).toContain('Playlist konnte nicht geladen werden.')
})

it('keeps original positions when filtering and requires confirmation before removing membership', async () => {
  const writes: unknown[] = []
  globalThis.fetch = (async (input, init) => {
    const url = String(input)
    if (init?.method === 'PUT') {
      writes.push(JSON.parse(init.body as string))
      return new Response(JSON.stringify({ error: { code: 'STALE_REPAIR', message: 'Reihenfolge hat sich geändert.' } }), { status: 409 })
    }
    if (init?.method === 'DELETE') throw new Error('Removal must not happen before confirmation')
    const data = url.endsWith('/auth/status') ? { authenticated: true, user: { id: 'fixture', username: 'fixture', role: 'user' } } : { ...playlists[0], tracks }
    return new Response(JSON.stringify({ data }))
  }) as typeof fetch
  render(<AuthProvider><PlayerProvider><PlaylistDetail id="list-0" /></PlayerProvider></AuthProvider>)
  await screen.findByLabelText('In dieser Playlist suchen')
  fireEvent.change(screen.getByLabelText('In dieser Playlist suchen'), { target: { value: 'Song 2' } })
  fireEvent.click(screen.getByRole('button', { name: 'Aktionen für Song 2' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Nach oben verschieben' }))
  await waitFor(() => expect(writes).toEqual([{ track_ids: ['track-2', 'track-1', 'track-3', 'track-4', 'track-5'] }]))
  expect((await screen.findByRole('alert')).textContent).toContain('Reihenfolge hat sich geändert.')
  fireEvent.click(screen.getByRole('button', { name: 'Aktionen für Song 2' }))
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Aus Playlist entfernen' }))
  const dialog = await screen.findByRole('dialog')
  expect(within(dialog).getByText(/Nur der Eintrag/)).toBeTruthy()
  fireEvent.click(within(dialog).getByRole('button', { name: 'Abbrechen' }))
  expect(writes.length).toBe(1)
})
