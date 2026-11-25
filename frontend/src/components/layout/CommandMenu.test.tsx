import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import type { LibrarySearchResults, LibraryTrack } from '@/types/api'
import type { Playlist } from '@/types/playlist'
import { CommandMenu } from './CommandMenu'

let loadedTrack: unknown = null
let played: { track: LibraryTrack; queue?: LibraryTrack[]; index?: number }[] = []
let queued: LibraryTrack[] = []

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: loadedTrack, status: "idle", shuffle: true, repeatMode: "off" }),
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({
    togglePlayPause: () => {},
    next: () => {},
    previous: () => {},
    toggleShuffle: () => {},
    cycleRepeatMode: () => {},
    playTrack: (track: LibraryTrack, queue?: LibraryTrack[], index?: number) => {
      played.push({ track, queue, index })
    },
    addToQueue: (track: LibraryTrack) => {
      queued.push(track)
    },
  }),
}))

// Other test files replace this module for the whole run (bun shares
// module mocks), so the menu's playlist source is pinned here as well.
let playlists: Playlist[] = []
let playlistRequests = 0
const realPlaylists = await import('@/lib/api/playlists')
mock.module('@/lib/api/playlists', () => ({
  ...realPlaylists,
  listPlaylists: async () => {
    playlistRequests++
    return playlists
  },
}))

const auth = {
  user: null,
  loading: false,
  setupRequired: false,
  isAdmin: false,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
  checkSetup: async () => {},
}

let openChanges: boolean[] = []

function renderMenu() {
  return render(
    <AuthContext.Provider value={auth}>
      <CommandMenu open onOpenChange={(open) => openChanges.push(open)} />
    </AuthContext.Provider>,
  )
}

function type(text: string) {
  const input = screen.getByRole('combobox')
  fireEvent.change(input, { target: { value: text } })
  return input
}

/* ---------------------------------------------------------- stubbed API */

function track(id: string, title: string): LibraryTrack {
  return {
    id,
    release_id: 'r1',
    title,
    artists: ['Kraftwerk'],
    album: 'Computerwelt',
    duration_ms: 200000,
    lyrics_state: 'none',
    codec: 'opus',
    bitrate_kbps: 160,
    created_at: '2026-01-01T00:00:00Z',
  } as LibraryTrack
}

const kraftwerk = {
  tracks: [track('t1', 'Computerwelt'), track('t2', 'Taschenrechner'), track('t3', 'Heimcomputer')],
  artists: [
    {
      id: 'a1',
      name: 'Kraftwerk',
      provider: 'test',
      source_id: 'kw',
      source_url: '',
      release_count: 2,
      track_count: 12,
      total_size_bytes: 0,
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
  releases: [
    {
      id: 'r1',
      title: 'Computer World',
      artists: ['Kraftwerk'],
      album_artist: 'Kraftwerk',
      release_type: 'album',
      year: 1981,
      track_count: 7,
      track_count_in_library: 7,
      total_size_bytes: 0,
      created_at: '2026-01-01T00:00:00Z',
    },
  ],
} as LibrarySearchResults

function playlist(id: string, name: string, trackCount = 3): Playlist {
  return {
    id,
    user_id: 'u1',
    name,
    description: '',
    track_count: trackCount,
    duration_ms: 0,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
  }
}

const NOTHING: LibrarySearchResults = { tracks: [], artists: [], releases: [] }

let calls: { url: string; signal?: AbortSignal | null }[] = []
let searchAnswer: (query: string) => Promise<LibrarySearchResults> = async () => NOTHING
const originalFetch = globalThis.fetch

function json(data: unknown) {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function searchCalls() {
  return calls.filter((call) => call.url.includes('/library/search'))
}

function option(name: string) {
  return screen.getByRole('option', { name })
}

/** Results arrive after the typing; the first one then takes the selection. */
async function firstResultSelected() {
  await waitFor(() =>
    expect(option('Computerwelt von Kraftwerk abspielen').getAttribute('aria-selected')).toBe('true'),
  )
}

beforeEach(() => {
  loadedTrack = null
  played = []
  queued = []
  openChanges = []
  calls = []
  playlists = []
  playlistRequests = 0
  searchAnswer = async () => NOTHING
  window.history.replaceState(null, '', '/listen')
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    calls.push({ url, signal: init?.signal })
    if (url.includes('/library/search')) {
      const query = new URL(url, 'http://localhost').searchParams.get('q') ?? ''
      return json(await searchAnswer(query))
    }
    return json(null)
  }) as typeof fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('CommandMenu ranking', () => {
  it('offers no search entries while the field is empty', () => {
    renderMenu()
    expect(screen.queryByText(/In der Bibliothek suchen/)).toBeNull()
    expect(screen.getByText('Hören')).toBeDefined()
  })

  it('offers every destination of both areas, grouped by area', () => {
    renderMenu()
    const groupOf = (label: string) => screen.getByRole('option', { name: label }).closest('[cmdk-group]')?.querySelector('[cmdk-group-heading]')?.textContent
    for (const label of ['Übersicht', 'Für dich', 'Player', 'Bibliothek', 'Playlists', 'Favoriten', 'Offline-Musik']) {
      expect(groupOf(label)).toBe('Hören')
    }
    for (const label of ['Suche', 'Entdecken', 'Downloads', 'Abonnements']) {
      expect(groupOf(label)).toBe('Laden')
    }
  })

  it('opens the matching view on Enter instead of searching the library', async () => {
    renderMenu()
    const input = type('bib')
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole('option')
    expect(options[0]?.textContent).toBe('Bibliothek')
    expect(options[0]?.getAttribute('aria-selected')).toBe('true')
    expect(options.at(-1)?.textContent).toContain('Im Katalog entdecken')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.pathname).toBe('/library')
    expect(window.location.search).toBe('')
  })

  it('selects the library search when nothing else matches', async () => {
    renderMenu()
    const input = type('qqxzv')
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(2)
    expect(options[0]?.getAttribute('aria-selected')).toBe('true')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.pathname).toBe('/library')
    expect(window.location.search).toContain('q=qqxzv')
  })

  it("does not run a player action whose letters merely contain the query in order", async () => {
    loadedTrack = { id: "t1", title: "Song" }
    renderMenu()
    const input = type("dash")
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole("option")
    // "Suche" is the former dashboard and still found by that name.
    expect(options[0]?.textContent).toBe("Suche")
    expect(options[0]?.getAttribute("aria-selected")).toBe("true")
    expect(screen.queryByText(/Zufallswiedergabe/)).toBeNull()

    fireEvent.keyDown(input, { key: "Enter" })
    expect(window.location.pathname).toBe("/search")
  })
})

describe('CommandMenu library results', () => {
  it('asks nothing before two characters, then shows titles, artists, albums and playlists', async () => {
    searchAnswer = async () => kraftwerk
    playlists = [playlist('p1', 'Kraftwerk am Abend'), playlist('p2', 'Sonntag'), playlist('p3', 'Krautrock', 1)]
    renderMenu()

    type('k')
    await new Promise((resolve) => setTimeout(resolve, 250))
    expect(calls).toHaveLength(0)
    expect(playlistRequests).toBe(0)

    type('kra')
    await waitFor(() => expect(option('Computerwelt von Kraftwerk abspielen')).toBeDefined())
    await waitFor(() => expect(screen.getByText('Kraftwerk am Abend')).toBeDefined())
    expect(searchCalls()).toHaveLength(1)
    expect(searchCalls()[0]?.url).toContain('limit=5')

    const headings = [...document.querySelectorAll('[cmdk-group]:not([hidden]) [cmdk-group-heading]')].map(
      (node) => node.textContent,
    )
    expect(headings.slice(0, 4)).toEqual(['Titel', 'Künstler', 'Alben', 'Playlists'])
    // The server's order is kept, and the first result is the one Enter runs.
    const titles = screen
      .getAllByRole('option')
      .slice(0, 3)
      .map((item) => item.getAttribute('aria-label'))
    expect(titles).toEqual([
      'Computerwelt von Kraftwerk abspielen',
      'Taschenrechner von Kraftwerk abspielen',
      'Heimcomputer von Kraftwerk abspielen',
    ])
    await firstResultSelected()

    // Playlists are matched here rather than asked for per keystroke.
    expect(screen.getByText('Kraftwerk am Abend')).toBeDefined()
    expect(screen.getByText('Krautrock')).toBeDefined()
    expect(screen.queryByText('Sonntag')).toBeNull()
    expect(screen.getByText('Playlist · 1 Titel')).toBeDefined()

    type('kraf')
    await waitFor(() => expect(searchCalls()).toHaveLength(2))
    expect(screen.queryByText('Krautrock')).toBeNull()
    expect(playlistRequests).toBe(1)
    // Covers are decoration; the row says what it is.
    for (const image of document.querySelectorAll('[cmdk-item] img')) expect(image.getAttribute('alt')).toBe('')
  })

  it('keeps a slow answer to an older query from replacing the newer one', async () => {
    let releaseFirst!: () => void
    searchAnswer = (query) => {
      if (query === 'com') {
        return new Promise((resolve) => {
          releaseFirst = () => resolve({ ...NOTHING, tracks: [track('old', 'Veraltet')] })
        })
      }
      return Promise.resolve({ ...NOTHING, tracks: [track('new', 'Aktuell')] })
    }
    renderMenu()

    type('com')
    await waitFor(() => expect(searchCalls()).toHaveLength(1))
    type('comp')
    // The request still running is dropped at the keystroke.
    expect(searchCalls()[0]?.signal?.aborted).toBe(true)
    await waitFor(() => expect(screen.getByText('Aktuell')).toBeDefined())

    await act(async () => {
      releaseFirst()
      await new Promise((resolve) => setTimeout(resolve, 20))
    })
    expect(screen.queryByText('Veraltet')).toBeNull()
    expect(screen.getByText('Aktuell')).toBeDefined()
  })

  it('keeps the previous results on screen while the next query loads', async () => {
    let releaseSecond!: () => void
    searchAnswer = (query) =>
      query === 'kra'
        ? Promise.resolve(kraftwerk)
        : new Promise((resolve) => {
            releaseSecond = () => resolve({ ...NOTHING, tracks: [track('t9', 'Radioaktivität')] })
          })
    renderMenu()
    type('kra')
    await waitFor(() => expect(screen.getByText('Taschenrechner')).toBeDefined())
    type('kraft')
    await waitFor(() => expect(searchCalls()).toHaveLength(2))
    expect(screen.getByText('Taschenrechner')).toBeDefined()
    await act(async () => releaseSecond())
    await waitFor(() => expect(screen.getByText('Radioaktivität')).toBeDefined())
    expect(screen.queryByText('Taschenrechner')).toBeNull()
  })

  it('plays a title on Enter with the other titles queued behind it', async () => {
    searchAnswer = async () => kraftwerk
    renderMenu()
    const input = type('kra')
    await firstResultSelected()

    fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })

    expect(played).toHaveLength(1)
    expect(played[0]?.track.id).toBe('t2')
    expect(played[0]?.queue?.map((item) => item.id)).toEqual(['t2', 't1', 't3'])
    expect(played[0]?.index).toBe(0)
    expect(openChanges).toEqual([false])
  })

  it('adds a title to the queue on Shift+Enter or its button and stays open', async () => {
    searchAnswer = async () => kraftwerk
    renderMenu()
    const input = type('kra')
    await firstResultSelected()

    fireEvent.keyDown(input, { key: 'Enter', shiftKey: true })
    expect(queued.map((item) => item.id)).toEqual(['t1'])

    fireEvent.click(screen.getByRole('button', { name: 'Taschenrechner zur Warteschlange hinzufügen' }))
    expect(queued.map((item) => item.id)).toEqual(['t1', 't2'])
    expect(played).toHaveLength(0)
    expect(openChanges).toEqual([])
  })

  it('opens an artist on Enter and an album or playlist on click, then closes', async () => {
    searchAnswer = async () => kraftwerk
    playlists = [playlist('p1', 'Kraftwerk am Abend')]
    renderMenu()
    const input = type('kra')
    await waitFor(() => expect(screen.getByText('Kraftwerk am Abend')).toBeDefined())
    await firstResultSelected()

    // Three titles, then the artist.
    for (let step = 0; step < 3; step++) fireEvent.keyDown(input, { key: 'ArrowDown' })
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.pathname).toBe('/library/artists/a1')

    fireEvent.click(screen.getByText('Computer World').closest('[cmdk-item]')!)
    expect(window.location.pathname).toBe('/library/releases/r1')

    fireEvent.click(screen.getByText('Kraftwerk am Abend').closest('[cmdk-item]')!)
    expect(window.location.pathname).toBe('/playlists/p1')
    expect(openChanges).toEqual([false, false, false])
    expect(played).toHaveLength(0)
  })

  it('says when nothing matched and still offers both searches', async () => {
    renderMenu()
    const input = type('zzqx')
    await waitFor(() => expect(screen.getByText('Nichts gefunden')).toBeDefined())
    expect(screen.getByRole('status').textContent).toContain('Nichts')
    const options = screen.getAllByRole('option')
    expect(options.map((item) => item.textContent)).toEqual([
      'In der Bibliothek suchen: „zzqx“',
      'Im Katalog entdecken: „zzqx“',
    ])
    expect(options[0]?.getAttribute('aria-selected')).toBe('true')
    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.search).toContain('q=zzqx')
  })

  it('keeps the player shortcuts out of the menu', () => {
    const seen: string[] = []
    const listener = (event: KeyboardEvent) => seen.push(event.key)
    window.addEventListener('keydown', listener)
    try {
      renderMenu()
      const list = screen.getByRole('listbox')
      fireEvent.keyDown(list, { key: 'ArrowDown' })
      fireEvent.keyDown(list, { key: ' ' })
      fireEvent.keyDown(list, { key: 'm' })
      fireEvent.keyDown(list, { key: 'ArrowLeft' })
      // A shortcut with a modifier is none of the menu's business.
      fireEvent.keyDown(list, { key: 'l', ctrlKey: true })
      expect(seen).toEqual(['l'])
    } finally {
      window.removeEventListener('keydown', listener)
    }
  })
})
