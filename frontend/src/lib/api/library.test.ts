import { describe, expect, it } from 'bun:test'

import {
  deleteTrackLyrics,
  getCompatibilityReport,
  refreshTrackLyrics,
  reorganizeLibrary,
  startLyricsBackfill,
  trackLyrics,
} from './library'

describe('library api client', () => {
  it('fetches track lyrics with envelope unwrapping', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string) => {
      expect(url).toContain('/library/tracks/t-123/lyrics')
      return new Response(
        JSON.stringify({
          data: {
            track_id: 't-123',
            state: 'available_synced',
            provider: 'lrclib',
            content: '[00:01.00] Hello',
            synced: true,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await trackLyrics('t-123')
      expect(res.state).toBe('available_synced')
      expect(res.synced).toBe(true)
      expect(res.content).toBe('[00:01.00] Hello')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('refreshes track lyrics', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/library/tracks/t-123/lyrics/refresh')
      expect(init?.method).toBe('POST')
      return new Response(
        JSON.stringify({
          data: {
            track_id: 't-123',
            state: 'available_plain',
            provider: 'ytmusic',
            content: 'Hello world',
            synced: false,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await refreshTrackLyrics('t-123')
      expect(res.state).toBe('available_plain')
      expect(res.synced).toBe(false)
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('deletes track lyrics', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/library/tracks/t-123/lyrics')
      expect(init?.method).toBe('DELETE')
      return new Response(JSON.stringify({ data: { ok: true } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    try {
      await deleteTrackLyrics('t-123')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('starts lyrics backfill', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/library/lyrics/backfill')
      expect(init?.method).toBe('POST')
      return new Response(
        JSON.stringify({
          data: {
            status: 'running',
            candidates: 10,
            processed: 0,
            written: 0,
            instrumental: 0,
            missing: 0,
            remaining: 10,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await startLyricsBackfill()
      expect(res.status).toBe('running')
      expect(res.candidates).toBe(10)
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('fetches compatibility report', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string) => {
      expect(url).toContain('/library/compatibility')
      return new Response(
        JSON.stringify({
          data: {
            files_scanned: 50,
            issues: [
              {
                id: 'issue-1',
                kind: 'artist_folder',
                track_id: 't-1',
                title: 'Track 1',
                from: 'A & B/2020 - Album/01.opus',
                to: 'A/2020 - Album/01.opus',
              },
            ],
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await getCompatibilityReport()
      expect(res.files_scanned).toBe(50)
      expect(res.issues).toHaveLength(1)
      expect(res.issues[0].kind).toBe('artist_folder')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('reorganizes library with confirmation', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/library/reorganize')
      expect(init?.method).toBe('POST')
      const body = JSON.parse(init?.body as string)
      expect(body.confirm).toBe(true)
      expect(body.issue_ids).toEqual(['issue-1'])
      return new Response(
        JSON.stringify({
          data: {
            requested: 1,
            moved: 1,
            skipped: 0,
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await reorganizeLibrary({ confirm: true, issue_ids: ['issue-1'] })
      expect(res.moved).toBe(1)
      expect(res.requested).toBe(1)
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})
