import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import type { PersonalMix, PersonalMixDetail } from '@/lib/api/mixes'
import type { LibraryTrack } from '@/types/api'

const playAlbum = mock((_tracks: LibraryTrack[], _start?: number) => {})
const addToQueue = mock((_tracks: LibraryTrack[] | LibraryTrack) => {})

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: null, status: 'idle' }),
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({ playAlbum, addToQueue, playTrack: () => {}, togglePlayPause: () => {}, playNext: () => {} }),
}))

let mixes: PersonalMix[] = []
let detail: PersonalMixDetail | Error
// When set, personalMix waits for it before answering.
let gate: Promise<void> | null = null
let lastSignal: AbortSignal | undefined
const listCalls: string[] = []
mock.module('@/lib/api/mixes', () => ({
  personalMixes: async () => {
    listCalls.push('list')
    return mixes
  },
  personalMix: async (_id: string, signal?: AbortSignal) => {
    lastSignal = signal
    if (gate) await gate
    if (detail instanceof Error) throw detail
    return detail
  },
}))

const { ForYou, MixDetail } = await import('./ForYou')
const { LISTEN_NAV } = await import('@/components/layout/navigation')

const tracks = Array.from({ length: 12 }, (_, n) => ({
  id: `track-${n}`,
  title: `Song ${n}`,
  artists: [`Artist ${n % 4}`],
  album: `Album ${n}`,
  album_artist: '',
  release_id: `release-${n}`,
  duration_ms: 180000,
  track_number: n + 1,
})) as unknown as LibraryTrack[]

const mix = (id: string, kind: PersonalMix['kind'], title: string, extra: Partial<PersonalMix> = {}): PersonalMix => ({
  id,
  kind,
  title,
  description: `${title} Beschreibung`,
  track_count: 12,
  cover_track_ids: ['track-0', 'track-1', 'track-2', 'track-3'],
  cover_release_ids: ['release-0', 'release-1', 'release-2', 'release-3'],
  ...extra,
})

describe('Für dich', () => {
  beforeEach(() => {
    playAlbum.mockClear()
    addToQueue.mockClear()
    listCalls.length = 0
  })

  it('opens the listening navigation, right before the player', () => {
    const labels = LISTEN_NAV.map((item) => item.label)
    expect(labels[0]).toBe('Für dich')
    expect(labels.indexOf('Player')).toBe(1)
    expect(LISTEN_NAV.find((item) => item.label === 'Für dich')?.href).toBe('/mixes')
  })

  it('renders the mixes as cards and plays one directly', async () => {
    mixes = [mix('genre-1', 'genre', 'Dein Mix 1', { genre: 'Rock' }), mix('on_repeat', 'on_repeat', 'In Dauerschleife')]
    detail = { ...mixes[0]!, tracks }
    render(<ForYou />)
    const card = (await screen.findByText('Dein Mix 1')).closest('article')!
    expect(within(card).getByText('Rock')).toBeTruthy()
    expect(within(card).getByText('12 Titel')).toBeTruthy()
    expect(card.querySelectorAll('img').length).toBe(4)
    expect(within(card).getByRole('link').getAttribute('href')).toBe('/mixes/genre-1')
    expect(screen.getByRole('heading', { name: 'Deine Mixe' })).toBeTruthy()
    expect(screen.getByRole('heading', { name: 'Aus deinem Hören' })).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Dein Mix 1 abspielen' }))
    await waitFor(() => expect(playAlbum).toHaveBeenCalledTimes(1))
    expect(playAlbum.mock.calls[0]![0].map((t) => t.id)).toEqual(tracks.map((t) => t.id))
  })

  it('explains the empty state and links to the library', async () => {
    mixes = []
    render(<ForYou />)
    expect(await screen.findByText('Noch keine Mixe')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Zur Bibliothek' }).getAttribute('href')).toBe('/library')
  })

  it('shows an error when playing a mix fails', async () => {
    mixes = [mix('rediscover', 'rediscover', 'Wiederentdecken')]
    detail = new Error('Mix nicht gefunden.')
    render(<ForYou />)
    fireEvent.click(await screen.findByRole('button', { name: 'Wiederentdecken abspielen' }))
    expect((await screen.findByRole('alert')).textContent).toContain('Mix nicht gefunden.')
    expect(playAlbum).not.toHaveBeenCalled()
  })

  it('does not start a mix that finishes loading after leaving the page', async () => {
    mixes = [mix('genre-1', 'genre', 'Dein Mix 1', { genre: 'Rock' })]
    detail = { ...mixes[0]!, tracks }
    let open = () => {}
    gate = new Promise<void>((resolve) => (open = resolve))
    try {
      const view = render(<ForYou />)
      fireEvent.click(await screen.findByRole('button', { name: 'Dein Mix 1 abspielen' }))
      await waitFor(() => expect(lastSignal).toBeDefined())
      view.unmount()
      expect(lastSignal!.aborted).toBe(true)
      open()
      await new Promise((resolve) => setTimeout(resolve, 0))
      expect(playAlbum).not.toHaveBeenCalled()
    } finally {
      gate = null
      lastSignal = undefined
    }
  })

  it('opens a mix with play, shuffle and queue actions', async () => {
    detail = { ...mix('unheard', 'unheard', 'Noch nicht gehört'), tracks }
    render(<MixDetail id="unheard" />)
    expect(await screen.findByRole('heading', { level: 1, name: 'Noch nicht gehört' })).toBeTruthy()
    expect(screen.getByText('Song 11')).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Abspielen' }))
    expect(playAlbum.mock.calls[0]![0].map((t) => t.id)).toEqual(tracks.map((t) => t.id))
    fireEvent.click(screen.getByRole('button', { name: 'Zufällig' }))
    expect(playAlbum.mock.calls[1]![0].map((t) => t.id).sort()).toEqual(tracks.map((t) => t.id).sort())
    fireEvent.click(screen.getByRole('button', { name: 'Zur Warteschlange' }))
    expect(addToQueue).toHaveBeenCalledTimes(1)
  })
})
