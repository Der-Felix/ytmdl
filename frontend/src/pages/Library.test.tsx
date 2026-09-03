import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { Library } from './Library'
import { navigate } from '@/lib/router'
import type { LibraryArtist, LibraryRelease, LibraryStats, LibraryTrack, ScanResult } from '@/types/api'

interface Call {
  url: string
  method: string
  body: unknown
}

let calls: Call[] = []
let originalFetch: typeof fetch

type Routes = Record<string, () => { status?: number; body: unknown }>

function stubFetch(routes: Routes): Call[] {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    calls.push({
      url,
      method,
      body: init?.body ? JSON.parse(init.body as string) : undefined,
    })

    let pathname = url.split('?')[0]
    try {
      pathname = new URL(url, 'http://localhost').pathname
    } catch {}

    const key = `${method} ${pathname}`
    const route = routes[key] ?? routes[`${method} *`]
    if (!route) {
      if (originalFetch) return originalFetch(input, init)
      throw new Error(`no route for ${key}`)
    }

    const { status = 200, body } = route()
    return new Response(status === 204 ? null : JSON.stringify(body), {
      status,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as typeof fetch
  return calls
}

function baseRoutes(): Routes {
  const artists: LibraryArtist[] = [
    {
      id: 'art-1',
      name: 'Daft Punk',
      provider: 'deezer',
      source_id: '27',
      source_url: '',
      release_count: 1,
      track_count: 14,
      total_size_bytes: 150_000_000,
      created_at: new Date().toISOString(),
    },
  ]
  const releases: LibraryRelease[] = [
    {
      id: 'rel-1',
      title: 'Discovery',
      artists: ['Daft Punk'],
      album_artist: 'Daft Punk',
      release_type: 'album',
      year: 2001,
      track_count: 14,
      track_count_in_library: 14,
      total_size_bytes: 150_000_000,
      created_at: new Date().toISOString(),
    },
  ]
  const tracks: LibraryTrack[] = [
    {
      id: 'trk-1',
      release_id: 'rel-1',
      title: 'One More Time',
      artists: ['Daft Punk'],
      album: 'Discovery',
      duration_ms: 320000,
      lyrics_state: 'available_synced',
      codec: 'opus',
      bitrate_kbps: 160,
      created_at: new Date().toISOString(),
    },
  ]
  const stats: LibraryStats = {
    total_artists: 1,
    total_releases: 1,
    total_tracks: 14,
    total_files: 14,
    total_bytes: 150_000_000,
    healthy_count: 14,
    issue_count: 0,
    codec_breakdown: { opus: 14 },
    lyrics_coverage: {
      available_synced: 10,
      available_plain: 2,
      instrumental: 1,
      not_found: 1,
      unknown: 0,
    },
  }
  const scanResult: ScanResult = {
    id: 'scan-1',
    status: 'completed',
    started_at: new Date().toISOString(),
    duration_ms: 450,
    files_scanned: 14,
    summary: {
      total_files_scanned: 14,
      healthy: 14,
      missing_files: 0,
      orphan_files: 0,
      invalid_files: 0,
      metadata_mismatches: 0,
      duplicate_files: 0,
    },
    issues: [],
  }

  return {
    'GET /api/v1/library/artists': () => ({ body: { data: artists, meta: { count: 1, total: 1 } } }),
    'GET /api/v1/library/releases': () => ({ body: { data: releases, meta: { count: 1, total: 1 } } }),
    'GET /api/v1/library/tracks': () => ({ body: { data: tracks, meta: { count: 1, total: 1 } } }),
    'GET /api/v1/library/stats': () => ({ body: { data: stats } }),
    'GET /api/v1/library/scan': () => ({ body: { data: scanResult } }),
    'POST /api/v1/library/scan': () => ({ body: { data: scanResult } }),
    'GET /api/v1/library/audits/current': () => ({
      body: {
        data: {
          id: 'run-1',
          mode: 'quick',
          status: 'completed',
          started_at: new Date().toISOString(),
          scanned: 14,
          total: 14,
          findings_count: 0,
          created_at: new Date().toISOString(),
        },
      },
    }),
    'GET /api/v1/library/audits/run-1/findings': () => ({
      body: { data: [], meta: { count: 0, total: 0 } },
    }),
  }
}

describe('Library Page', () => {
  beforeEach(() => {
    calls = []
    originalFetch = globalThis.fetch
    window.history.replaceState(null, '', '/')
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('renders library overview, stats and release cards', async () => {
    stubFetch(baseRoutes())

    render(<Library />)

    try {
      await waitFor(() => {
        expect(screen.getByText('Discovery')).toBeTruthy()
      })
    } catch (e) {
      console.error('INNER HTML IS:\n', document.body.innerHTML)
      throw e
    }
  })

  it('triggers a library scan when clicking the scan button', async () => {
    stubFetch(baseRoutes())

    render(<Library />)

    await waitFor(() => {
      expect(screen.getByText('Discovery')).toBeTruthy()
    })

    const scanBtn = screen.getByRole('button', { name: /^Scan$/i })
    fireEvent.click(scanBtn)

    await waitFor(() => {
      const scanCalls = calls.filter((c) => c.method === 'POST' && c.url.includes('/library/scan'))
      expect(scanCalls.length).toBeGreaterThan(0)
    })
  })

  it('displays maintenance audit results and findings', async () => {
    const routes = baseRoutes()
    routes['GET /api/v1/library/audits/current'] = () => ({
      body: {
        data: {
          id: 'run-2',
          mode: 'deep',
          status: 'completed',
          started_at: new Date().toISOString(),
          scanned: 15,
          total: 15,
          findings_count: 1,
          created_at: new Date().toISOString(),
        },
      },
    })
    routes['GET /api/v1/library/audits/run-2/findings'] = () => ({
      body: {
        data: [
          {
            id: 'find-1',
            run_id: 'run-2',
            finding_code: 'PATH_MISMATCH',
            severity: 'info',
            relative_path: 'Daft Punk/Discovery/02 - Aerodynamic.opus',
            track_id: 'trk-2',
            artist_name: 'Daft Punk',
            track_title: 'Aerodynamic',
            suggested_action: 'MOVE_CANONICAL',
            evidence: {
              level: 'EXACT_CATALOG_ID',
              expected_path: 'Daft Punk/2001 - Discovery/02 - Aerodynamic.opus',
              details: 'File path differs from current canonical library layout.',
            },
            created_at: new Date().toISOString(),
          },
        ],
        meta: { count: 1, total: 1 },
      },
    })

    stubFetch(routes)

    render(<Library />)

    // Switch to Wartung tab
    const wartungBtn = screen.getByRole('button', { name: /Wartung/i })
    fireEvent.click(wartungBtn)

    await waitFor(() => {
      expect(screen.getByText('Bibliotheks-Integritätsprüfung')).toBeTruthy()
      expect(screen.getByText('Daft Punk/Discovery/02 - Aerodynamic.opus')).toBeTruthy()
      expect(screen.getAllByText('PATH_MISMATCH').length).toBeGreaterThan(0)
      expect(screen.getByText('Pfad korrigieren')).toBeTruthy()
    })
  })
})

