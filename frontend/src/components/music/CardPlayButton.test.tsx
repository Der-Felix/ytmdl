import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import type { QueueContext } from '@/lib/audio/types'
import type { LibraryArtist, LibraryRelease, LibraryTrack } from '@/types/api'

/*
 * The play button on release and artist cards, and the artist page's
 * "Am meisten gehört" section. The player is replaced by a small fake whose
 * state the tests set, as the other page tests do.
 */

interface FakePlayerState {
  currentTrack: LibraryTrack | null
  queueContext: QueueContext | null
  status: string
}
let playerState: FakePlayerState = { currentTrack: null, queueContext: null, status: 'idle' }
const playAlbum = mock((_tracks: LibraryTrack[], _start?: number, _context?: QueueContext | null) => {})
const playArtist = mock((_tracks: LibraryTrack[], _shuffle?: boolean, _context?: QueueContext | null) => {})
const playTrack = mock((_track: LibraryTrack, _queue?: LibraryTrack[], _index?: number, _context?: QueueContext | null) => {})
const togglePlayPause = mock(() => {})

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => playerState,
  usePlayerState: () => playerState,
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({ playAlbum, playArtist, playTrack, togglePlayPause, playShuffled: () => {}, playNext: () => {}, addToQueue: () => {} }),
}))

const { ReleaseCard } = await import('./ReleaseCard')
const { ArtistCard } = await import('./ArtistCard')
const { ArtistTopTracks } = await import('./ArtistTopTracks')

const track = (n: number, extra: Partial<LibraryTrack> = {}) =>
  ({ id: `t${n}`, title: `Song ${n}`, artists: ['Band'], album: 'Platte', album_artist: 'Band', duration_ms: 200000, track_number: n, disc_number: 1, ...extra }) as unknown as LibraryTrack

const release = { id: 'rel-1', title: 'Leise Räume', provider: 'ytmusic', source_id: 's1', year: 2024, track_count: 3, track_count_in_library: 3, total_size_bytes: 0, created_at: '', cover_url: '' } as unknown as LibraryRelease
const artist = { id: 'art-1', name: 'Band', provider: 'ytmusic', release_count: 1, track_count: 3, total_size_bytes: 0, created_at: '', image_url: '' } as unknown as LibraryArtist

let requests: string[] = []
let routes: Record<string, unknown> = {}
let originalFetch: typeof fetch

beforeEach(() => {
  playerState = { currentTrack: null, queueContext: null, status: 'idle' }
  for (const fn of [playAlbum, playArtist, playTrack, togglePlayPause]) fn.mockClear()
  requests = []
  routes = {}
  originalFetch = globalThis.fetch
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost')
    requests.push(url.pathname + url.search)
    const data = routes[url.pathname]
    if (data === undefined) return new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'nicht da' } }), { status: 404 })
    return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('play button on a release card', () => {
  it('loads the release and plays it in disc and track order without navigating', async () => {
    // Out of order on purpose: the card plays disc 1 first, then track numbers.
    routes['/api/v1/library/releases/rel-1'] = {
      release,
      tracks: [track(2, { disc_number: 2, track_number: 1 }), track(3, { track_number: 2 }), track(1, { track_number: 1 })],
      total_size_bytes: 0,
    }
    const before = window.location.pathname
    render(<ReleaseCard release={release} isLocal />)

    fireEvent.click(screen.getByRole('button', { name: '„Leise Räume“ abspielen' }))
    await waitFor(() => expect(playAlbum).toHaveBeenCalledTimes(1))
    const [tracks, start, context] = playAlbum.mock.calls[0]!
    expect(tracks.map((t) => t.id)).toEqual(['t1', 't3', 't2'])
    expect(start).toBe(0)
    expect(context).toEqual({ kind: 'album', id: 'rel-1', title: 'Leise Räume' })
    expect(requests).toEqual(['/api/v1/library/releases/rel-1'])
    expect(window.location.pathname).toBe(before)
    // The title still opens the release.
    expect(screen.getByRole('link', { name: 'Leise Räume' }).getAttribute('href')).toBe('/library/releases/rel-1')
  })

  it('pauses and resumes while the release is what plays, without loading again', () => {
    playerState = { currentTrack: track(1), queueContext: { kind: 'album', id: 'rel-1', title: 'Leise Räume' }, status: 'playing' }
    const { rerender } = render(<ReleaseCard release={release} isLocal />)

    fireEvent.click(screen.getByRole('button', { name: '„Leise Räume“ pausieren' }))
    expect(togglePlayPause).toHaveBeenCalledTimes(1)

    playerState = { ...playerState, status: 'paused' }
    rerender(<ReleaseCard release={release} isLocal />)
    fireEvent.click(screen.getByRole('button', { name: '„Leise Räume“ fortsetzen' }))
    expect(togglePlayPause).toHaveBeenCalledTimes(2)
    expect(playAlbum).not.toHaveBeenCalled()
    expect(requests).toEqual([])
  })

  it('is a real button in the card, reachable with the keyboard', () => {
    render(<ReleaseCard release={release} isLocal />)
    const button = screen.getByRole('button', { name: '„Leise Räume“ abspielen' })
    expect(button.tagName).toBe('BUTTON')
    expect(button.getAttribute('type')).toBe('button')
    expect(button.getAttribute('tabindex')).toBeNull()
    button.focus()
    expect(document.activeElement).toBe(button)
    // Not nested in the link: a click on it can never follow the link.
    expect(button.closest('a')).toBeNull()
  })

  it('is not offered for a release that is not in the library', () => {
    render(<ReleaseCard release={release} />)
    expect(screen.queryByRole('button', { name: /abspielen/ })).toBeNull()
  })

  it('shows a spinner while loading and stays quiet when nothing comes back', async () => {
    let answer: (value: Response) => void = () => {}
    globalThis.fetch = (() => new Promise<Response>((resolve) => { answer = resolve })) as typeof fetch
    render(<ReleaseCard release={release} isLocal />)
    const button = screen.getByRole('button', { name: '„Leise Räume“ abspielen' })
    fireEvent.click(button)
    await waitFor(() => expect(button.getAttribute('aria-busy')).toBe('true'))
    answer(new Response(JSON.stringify({ data: { release, tracks: [], total_size_bytes: 0 } }), { status: 200 }))
    await waitFor(() => expect(button.getAttribute('aria-busy')).toBeNull())
    expect(playAlbum).not.toHaveBeenCalled()
  })
})

describe('play button on an artist card', () => {
  it('plays the artist as the artist page does', async () => {
    routes['/api/v1/library/artists/art-1'] = { artist, releases: [], tracks: [track(1), track(2)], release_count: 1, track_count: 2, total_size_bytes: 0, subscribed: false }
    render(<ArtistCard artist={artist} isLocal />)
    fireEvent.click(screen.getByRole('button', { name: '„Band“ abspielen' }))
    await waitFor(() => expect(playArtist).toHaveBeenCalledTimes(1))
    const [tracks, shuffle, context] = playArtist.mock.calls[0]!
    expect(tracks.map((t) => t.id)).toEqual(['t1', 't2'])
    expect(shuffle).toBe(false)
    expect(context).toEqual({ kind: 'artist', id: 'art-1', title: 'Band' })
    expect(screen.getByRole('link', { name: 'Band' }).getAttribute('href')).toBe('/library/artists/art-1')
  })
})

describe('Am meisten gehört', () => {
  const played = (n: number) => ({ ...track(n), play_count: 20 - n, last_played_at: '2026-11-01T10:00:00Z' })

  it('is left out when there is no listening history for the artist', async () => {
    routes['/api/v1/library/artists/art-1/top-tracks'] = []
    render(<ArtistTopTracks artistId="art-1" artistName="Band" />)
    await waitFor(() => expect(requests).toEqual(['/api/v1/library/artists/art-1/top-tracks?limit=10']))
    expect(screen.queryByRole('heading', { name: 'Am meisten gehört' })).toBeNull()
  })

  it('lists the most played tracks, five at first, and plays from the chosen one', async () => {
    const tracks = Array.from({ length: 7 }, (_, i) => played(i + 1))
    routes['/api/v1/library/artists/art-1/top-tracks'] = tracks
    render(<ArtistTopTracks artistId="art-1" artistName="Band" />)

    const heading = await screen.findByRole('heading', { name: 'Am meisten gehört' })
    const section = heading.closest('section')!
    expect(within(section).getAllByRole('listitem')).toHaveLength(5)
    fireEvent.click(within(section).getByRole('button', { name: 'Mehr anzeigen' }))
    expect(within(section).getAllByRole('listitem')).toHaveLength(7)

    fireEvent.click(within(section).getByRole('button', { name: 'Song 3 abspielen, 17-mal gehört' }))
    expect(playTrack).toHaveBeenCalledTimes(1)
    const [first, queue, index, context] = playTrack.mock.calls[0]!
    expect(first.id).toBe('t3')
    expect(queue!.map((t) => t.id)).toEqual(tracks.map((t) => t.id))
    expect(index).toBe(2)
    expect(context).toEqual({ kind: 'history', id: 'artist-top:art-1', title: 'Am meisten gehört · Band' })
  })
})
