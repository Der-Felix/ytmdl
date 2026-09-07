import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { LibrarySearchField } from './LibrarySearchField'
import { PlayerProvider } from '@/contexts/PlayerContext'
import { AudioEngine } from '@/lib/audio/engine'
import type { LibrarySearchResults } from '@/types/api'

let originalFetch: typeof fetch
let fetchCalls: string[] = []

const sampleResults: LibrarySearchResults = {
  artists: [
    {
      id: 'art-1',
      name: 'Kraftwerk',
      provider: 'test',
      source_id: 'kw1',
      source_url: '',
      release_count: 5,
      track_count: 40,
      total_size_bytes: 500000000,
      created_at: new Date().toISOString(),
    },
  ],
  releases: [
    {
      id: 'rel-1',
      title: 'Computerwelt (Album)',
      artists: ['Kraftwerk'],
      album_artist: 'Kraftwerk',
      release_type: 'album',
      year: 1981,
      track_count: 8,
      track_count_in_library: 8,
      total_size_bytes: 100000000,
      created_at: new Date().toISOString(),
    },
  ],
  tracks: [
    {
      id: 'trk-1',
      release_id: 'rel-1',
      title: 'Computerwelt (Track)',
      artists: ['Kraftwerk'],
      album: 'Computerwelt (Album)',
      duration_ms: 305000,
      lyrics_state: 'available_synced',
      codec: 'opus',
      bitrate_kbps: 160,
      created_at: new Date().toISOString(),
    },
    {
      id: 'trk-2',
      release_id: 'rel-1',
      title: 'Taschenrechner',
      artists: ['Kraftwerk'],
      album: 'Computerwelt (Album)',
      duration_ms: 295000,
      lyrics_state: 'available_synced',
      codec: 'opus',
      bitrate_kbps: 160,
      created_at: new Date().toISOString(),
    },
  ],
}

describe('LibrarySearchField', () => {
  beforeEach(() => {
    fetchCalls = []
    originalFetch = globalThis.fetch
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      fetchCalls.push(url)

      if (url.includes('/library/search')) {
        return new Response(
          JSON.stringify({
            data: sampleResults,
          }),
          {
            status: 200,
            headers: { 'Content-Type': 'application/json' },
          },
        )
      }
      return new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as typeof fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('renders search input with placeholder', () => {
    render(
      <PlayerProvider>
        <LibrarySearchField placeholder="Test placeholder" />
      </PlayerProvider>,
    )

    const input = screen.getByPlaceholderText('Test placeholder')
    expect(input).toBeTruthy()
  })

  it('debounces live typing and displays flyout with categorized results', async () => {
    render(
      <PlayerProvider>
        <LibrarySearchField />
      </PlayerProvider>,
    )

    const input = screen.getByRole('searchbox')
    fireEvent.change(input, { target: { value: 'Computer' } })

    // Wait for 250ms debounce and response
    await waitFor(() => {
      expect(screen.getByTestId('library-search-flyout')).toBeTruthy()
      expect(screen.getByText('Kraftwerk')).toBeTruthy()
      expect(screen.getByText('Computerwelt (Album)')).toBeTruthy()
      expect(screen.getByText('Computerwelt (Track)')).toBeTruthy()
      expect(screen.getByText('Taschenrechner')).toBeTruthy()
    })

    const searchCalls = fetchCalls.filter((u) => u.includes('/library/search'))
    expect(searchCalls.length).toBeGreaterThan(0)
    expect(searchCalls[0]).toContain('q=Computer')
  })

  it('clears search input and closes flyout on clear button click', async () => {
    let cleared = false
    render(
      <PlayerProvider>
        <LibrarySearchField onClear={() => (cleared = true)} />
      </PlayerProvider>,
    )

    const input = screen.getByRole('searchbox')
    fireEvent.change(input, { target: { value: 'Computer' } })

    await waitFor(() => {
      expect(screen.getByTestId('library-search-flyout')).toBeTruthy()
    })

    const clearBtn = screen.getByLabelText('Suche löschen')
    fireEvent.click(clearBtn)

    expect(cleared).toBe(true)
    expect((input as HTMLInputElement).value).toBe('')
    expect(screen.queryByTestId('library-search-flyout')).toBeNull()
  })

  it('triggers onSearchSubmit on form submit or Enter key', async () => {
    let submittedQuery = ''
    render(
      <PlayerProvider>
        <LibrarySearchField onSearchSubmit={(q) => (submittedQuery = q)} />
      </PlayerProvider>,
    )

    const input = screen.getByRole('searchbox')
    fireEvent.change(input, { target: { value: 'Kraftwerk' } })
    const form = input.closest('form')!
    fireEvent.submit(form)

    expect(submittedQuery).toBe('Kraftwerk')
    expect(screen.queryByTestId('library-search-flyout')).toBeNull()
  })

  it('closes flyout on Escape key', async () => {
    render(
      <PlayerProvider>
        <LibrarySearchField />
      </PlayerProvider>,
    )

    const input = screen.getByRole('searchbox')
    fireEvent.change(input, { target: { value: 'Computer' } })

    await waitFor(() => {
      expect(screen.getByTestId('library-search-flyout')).toBeTruthy()
    })

    fireEvent.keyDown(input, { key: 'Escape', code: 'Escape' })
    expect(screen.queryByTestId('library-search-flyout')).toBeNull()
  })

  it('plays track from search results and updates player state', async () => {
    const engine = AudioEngine.getInstance()
    const origLoadAndPlay = engine.loadAndPlay
    const origLoad = engine.load
    let loadedUrl: string | null = null

    engine.loadAndPlay = async (url: string) => {
      loadedUrl = url
    }
    engine.load = (url: string) => {
      loadedUrl = url
    }

    try {
      render(
        <PlayerProvider>
          <LibrarySearchField />
        </PlayerProvider>,
      )

      const input = screen.getByRole('searchbox')
      fireEvent.change(input, { target: { value: 'Computer' } })

      await waitFor(() => {
        expect(screen.getByText('Computerwelt (Track)')).toBeTruthy()
      })

      const playBtns = screen.getAllByLabelText('Abspielen')
      expect(playBtns.length).toBeGreaterThan(0)
      fireEvent.click(playBtns[0])

      await waitFor(() => {
        expect(loadedUrl).toBe('/api/v1/library/tracks/trk-1/stream')
      })
    } finally {
      engine.loadAndPlay = origLoadAndPlay
      engine.load = origLoad
    }
  })

  it('prevents stale out-of-order response from overwriting latest query results', async () => {
    let resolveFirstQuery!: (value: Response) => void
    const firstPromise = new Promise<Response>((resolve) => {
      resolveFirstQuery = resolve
    })

    globalThis.fetch = ((input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString()
      if (url.includes('q=first')) {
        return firstPromise
      }
      if (url.includes('q=second')) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              data: {
                artists: [],
                releases: [],
                tracks: [
                  {
                    id: 'trk-second',
                    title: 'Second Query Track',
                    artists: ['Artist B'],
                    album: 'Album B',
                    duration_ms: 180000,
                    lyrics_state: 'none',
                    codec: 'opus',
                    bitrate_kbps: 160,
                    created_at: new Date().toISOString(),
                  },
                ],
              },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          ),
        )
      }
      return Promise.resolve(
        new Response(JSON.stringify({ data: { artists: [], releases: [], tracks: [] } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      )
    }) as typeof fetch

    render(
      <PlayerProvider>
        <LibrarySearchField />
      </PlayerProvider>,
    )

    const input = screen.getByRole('searchbox')

    // First search
    fireEvent.change(input, { target: { value: 'first' } })

    // Second search after small delay to trigger debounce
    await new Promise((r) => setTimeout(r, 260))

    fireEvent.change(input, { target: { value: 'second' } })

    // Wait for second search to settle
    await waitFor(() => {
      expect(screen.getByText('Second Query Track')).toBeTruthy()
    })

    // Now resolve the late first query
    resolveFirstQuery(
      new Response(
        JSON.stringify({
          data: {
            artists: [],
            releases: [],
            tracks: [
              {
                id: 'trk-first',
                title: 'Stale Old Track',
                artists: ['Artist A'],
                album: 'Album A',
                duration_ms: 180000,
                lyrics_state: 'none',
                codec: 'opus',
                bitrate_kbps: 160,
                created_at: new Date().toISOString(),
              },
            ],
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      ),
    )

    // Wait and verify the stale response did NOT overwrite the UI
    await new Promise((r) => setTimeout(r, 50))
    expect(screen.queryByText('Stale Old Track')).toBeNull()
    expect(screen.getByText('Second Query Track')).toBeTruthy()
  })
})
