import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import type { PersonalMix } from '@/lib/api/mixes'
import type { LibraryTrack } from '@/types/api'

let currentTrack: LibraryTrack | null = null
const playTrack = mock((_track: LibraryTrack, _queue?: LibraryTrack[], _index?: number) => {})
const togglePlayPause = mock(() => {})

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack }),
  usePlayerState: () => ({ currentTrack, status: 'paused', queue: currentTrack ? [currentTrack] : [], queueIndex: 0 }),
  usePlayerProgress: () => ({ currentTime: 30, duration: 180 }),
  usePlayerActions: () => ({ playTrack, togglePlayPause, playAlbum: () => {}, addToQueue: () => {} }),
}))

let mixes: PersonalMix[] | Error = []
mock.module('@/lib/api/mixes', () => ({
  personalMixes: async () => {
    if (mixes instanceof Error) throw mixes
    return mixes
  },
  personalMix: async () => ({ tracks: [] }),
}))

const { ListenHome } = await import('./ListenHome')

const track = (n: number) =>
  ({
    id: `track-${n}`,
    title: `Song ${n}`,
    artists: [`Artist ${n}`],
    album: `Album ${n}`,
    album_artist: `Artist ${n}`,
    duration_ms: 180000,
    play_count: 2,
    last_played_at: new Date().toISOString(),
  }) as unknown as LibraryTrack

const mix = (id: string, title: string): PersonalMix => ({
  id,
  kind: 'genre',
  title,
  description: `${title} Beschreibung`,
  genre: 'Rock',
  track_count: 12,
  cover_track_ids: ['track-0'],
  cover_release_ids: ['release-0'],
})

type Answer = unknown[] | 'error'
let history: Answer = []
let releases: Answer = []
let originalFetch: typeof fetch

function respond(answer: Answer, list: boolean) {
  if (answer === 'error') {
    return new Response(JSON.stringify({ error: { code: 'INTERNAL_ERROR', message: 'Datenbank nicht erreichbar.' } }), {
      status: 500,
      headers: { 'Content-Type': 'application/json' },
    })
  }
  const body = list ? { data: answer, meta: { count: answer.length } } : { data: answer }
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

beforeEach(() => {
  currentTrack = null
  mixes = []
  history = []
  releases = []
  playTrack.mockClear()
  togglePlayPause.mockClear()
  originalFetch = globalThis.fetch
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost')
    if (url.pathname === '/api/v1/history') return respond(history, false)
    if (url.pathname === '/api/v1/library/releases') return respond(releases, true)
    return new Response(JSON.stringify({ data: null }), { status: 200, headers: { 'Content-Type': 'application/json' } })
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

const section = (name: string) => screen.queryByRole('heading', { level: 2, name })

describe('Hören start page', () => {
  it('shows every section that has something, in order', async () => {
    currentTrack = track(99)
    mixes = [mix('genre-1', 'Dein Mix 1'), mix('genre-2', 'Dein Mix 2')]
    history = [track(1), track(2)]
    releases = [{ id: 'rel-1', title: 'Neues Album', album_artist: 'Band', cover_url: '' }]
    render(<ListenHome />)

    await screen.findByText('Dein Mix 1')
    await screen.findByText('Song 1')
    await screen.findByText('Neues Album')
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent)
    expect(headings).toEqual(['Weiterhören', 'Deine Mixe', 'Zuletzt gehört', 'Neu in der Mediathek'])
    expect(screen.getByText('Alle Mixe').closest('a')?.getAttribute('href')).toBe('/mixes')
  })

  it('resumes the last session and plays a track from the history', async () => {
    currentTrack = track(99)
    history = [track(1), track(2)]
    render(<ListenHome />)

    fireEvent.click(screen.getByRole('button', { name: 'Weiterhören' }))
    expect(togglePlayPause).toHaveBeenCalledTimes(1)

    fireEvent.click(await screen.findByRole('button', { name: 'Song 2 abspielen' }))
    expect(playTrack).toHaveBeenCalledTimes(1)
    expect(playTrack.mock.calls[0]![0].id).toBe('track-2')
    expect(playTrack.mock.calls[0]![2]).toBe(1)
  })

  it('leaves out the sections that have nothing', async () => {
    history = [track(1)]
    render(<ListenHome />)
    await screen.findByText('Song 1')
    await waitFor(() => expect(section('Deine Mixe')).toBeNull())
    await waitFor(() => expect(section('Neu in der Mediathek')).toBeNull())
    expect(section('Weiterhören')).toBeNull()
    expect(section('Zuletzt gehört')).not.toBeNull()
  })

  it('keeps the other sections when one fails', async () => {
    mixes = new Error('Mixe nicht verfügbar.')
    history = 'error'
    releases = [{ id: 'rel-1', title: 'Neues Album', album_artist: 'Band', cover_url: '' }]
    render(<ListenHome />)

    expect(await screen.findByText('Neues Album')).toBeDefined()
    // Each failure is reported inside its own section.
    await waitFor(() => expect(within(section('Zuletzt gehört')!.closest('section')!).getByRole('alert')).toBeDefined())
    await waitFor(() => expect(within(section('Deine Mixe')!.closest('section')!).getByRole('alert')).toBeDefined())
    expect(within(section('Neu in der Mediathek')!.closest('section')!).queryByRole('alert')).toBeNull()
  })

  it('points a brand-new library to Laden', async () => {
    render(<ListenHome />)
    expect(await screen.findByText('Hier gibt es noch nichts zu hören')).toBeDefined()
    expect(screen.getByRole('link', { name: /Zu Laden/ }).getAttribute('href')).toBe('/search')
    expect(screen.queryAllByRole('heading', { level: 2 })).toHaveLength(0)
  })
})
