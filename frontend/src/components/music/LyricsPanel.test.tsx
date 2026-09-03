import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { LyricsPanel } from './LyricsPanel'
import type { Track, TrackLyrics } from '@/types/api'

const track: Track = {
  id: 't-123',
  title: 'Get Lucky',
  artists: ['Daft Punk', 'Pharrell Williams'],
  album: 'Random Access Memories',
  album_artist: 'Daft Punk',
  track_number: 8,
  track_total: 13,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 369000,
  year: 2013,
  source_provider: 'deezer',
  source_id: '123',
  source_url: 'https://deezer.com/track/123',
}

const sampleLyrics: TrackLyrics = {
  track_id: 't-123',
  state: 'available_synced',
  provider: 'lrclib',
  content: '[00:01.00] Like the legend of the phoenix\n[00:05.00] All ends with beginnings',
  synced: true,
}

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

describe('LyricsPanel', () => {
  it('loads and renders lyrics content when opened', async () => {
    globalThis.fetch = (async (url: string) => {
      expect(url).toContain('/library/tracks/t-123/lyrics')
      return new Response(
        JSON.stringify({ data: sampleLyrics }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    render(<LyricsPanel track={track} open={true} onOpenChange={() => {}} />)

    expect(await screen.findByText(/Like the legend of the phoenix/)).toBeDefined()
    expect(screen.getByText(/Synchronisiert/)).toBeDefined()
    expect(screen.getByText(/LRCLIB/)).toBeDefined()
  })

  it('handles refresh action', async () => {
    let refreshCalled = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (init?.method === 'POST' && url.includes('/refresh')) {
        refreshCalled = true
        return new Response(
          JSON.stringify({
            data: {
              ...sampleLyrics,
              content: 'Refreshed lyrics line',
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(
        JSON.stringify({ data: sampleLyrics }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    let updatedLyrics: TrackLyrics | null = null
    render(
      <LyricsPanel
        track={track}
        open={true}
        onOpenChange={() => {}}
        onLyricsChanged={(l) => {
          updatedLyrics = l
        }}
      />,
    )

    await screen.findByText(/Like the legend of the phoenix/)

    const refreshButton = screen.getByRole('button', { name: /Aktualisieren/ })
    fireEvent.click(refreshButton)

    await waitFor(() => {
      expect(refreshCalled).toBe(true)
      expect(updatedLyrics?.content).toBe('Refreshed lyrics line')
    })
  })

  it('handles delete action', async () => {
    let deleteCalled = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        deleteCalled = true
        return new Response(JSON.stringify({ data: { ok: true } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      }
      return new Response(
        JSON.stringify({ data: sampleLyrics }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    let updatedLyrics: TrackLyrics | null = sampleLyrics
    render(
      <LyricsPanel
        track={track}
        open={true}
        onOpenChange={() => {}}
        onLyricsChanged={(l) => {
          updatedLyrics = l
        }}
      />,
    )

    await screen.findByText(/Like the legend of the phoenix/)

    const deleteButton = screen.getByRole('button', { name: /Löschen/ })
    fireEvent.click(deleteButton)

    await waitFor(() => {
      expect(deleteCalled).toBe(true)
      expect(updatedLyrics).toBeNull()
    })
  })
})
