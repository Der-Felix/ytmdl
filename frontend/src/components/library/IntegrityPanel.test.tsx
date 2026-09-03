import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { IntegrityPanel } from './IntegrityPanel'
import type { AuditFinding, AuditRun } from '@/types/api'

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

describe('IntegrityPanel', () => {
  beforeEach(() => {
    calls = []
    originalFetch = globalThis.fetch
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('renders completed audit run and findings with action buttons', async () => {
    const run: AuditRun = {
      id: 'run-100',
      mode: 'quick',
      status: 'completed',
      started_at: new Date().toISOString(),
      scanned: 250,
      total: 250,
      findings_count: 1,
      created_at: new Date().toISOString(),
    }

    const findings: AuditFinding[] = [
      {
        id: 'finding-1',
        run_id: 'run-100',
        finding_code: 'PATH_MISMATCH',
        severity: 'info',
        relative_path: 'Artist 01/Album 01/01 - Track 01.opus',
        artist_name: 'Artist 01',
        track_title: 'Track 01',
        suggested_action: 'MOVE_CANONICAL',
        evidence: {
          level: 'EXACT_CATALOG_ID',
          expected_path: 'Artist 01/2020 - Album 01/01 - Track 01.opus',
          actual_path: 'Artist 01/Album 01/01 - Track 01.opus',
          details: 'File path differs from current canonical library layout.',
        },
        created_at: new Date().toISOString(),
      },
    ]

    stubFetch({
      'GET /api/v1/library/audits/current': () => ({ body: { data: run } }),
      'GET /api/v1/library/audits/run-100/findings': () => ({
        body: { data: findings, meta: { count: 1, total: 1 } },
      }),
    })

    render(<IntegrityPanel isAdmin={true} />)

    await waitFor(() => {
      expect(screen.getByText('Bibliotheks-Integritätsprüfung')).toBeTruthy()
      expect(screen.getByText('Artist 01/Album 01/01 - Track 01.opus')).toBeTruthy()
      expect(screen.getAllByText('PATH_MISMATCH').length).toBeGreaterThan(0)
      expect(screen.getByText('Pfad korrigieren')).toBeTruthy()
    })
  })

  it('opens preview dialog when clicking In Quarantäne and applies repair', async () => {
    const run: AuditRun = {
      id: 'run-quar',
      mode: 'quick',
      status: 'completed',
      started_at: new Date().toISOString(),
      scanned: 10,
      total: 10,
      findings_count: 1,
      created_at: new Date().toISOString(),
    }

    const findings: AuditFinding[] = [
      {
        id: 'finding-quar-1',
        run_id: 'run-quar',
        finding_code: 'FILE_UNTRACKED',
        severity: 'warning',
        relative_path: 'Artist 01/Album 01/orphan.opus',
        artist_name: 'Artist 01',
        track_title: 'Orphan',
        suggested_action: 'QUARANTINE_FILE',
        created_at: new Date().toISOString(),
      },
    ]

    stubFetch({
      'GET /api/v1/library/audits/current': () => ({ body: { data: run } }),
      'GET /api/v1/library/audits/run-quar/findings': () => ({
        body: { data: findings, meta: { count: 1, total: 1 } },
      }),
      'POST /api/v1/library/repairs/preview': () => ({
        body: {
          data: [
            {
              finding_id: 'finding-quar-1',
              action: 'QUARANTINE_FILE',
              allowed: true,
              message: 'Sicherheitsprüfung bestanden',
              source_path: 'Artist 01/Album 01/orphan.opus',
              destination_path: '.ytmdl-trash/finding-quar-1/orphan.opus',
              file_changes: ['Move to .ytmdl-trash/finding-quar-1/orphan.opus'],
              warnings: ['Datei wird aus der aktiven Bibliothek isoliert.'],
            },
          ],
        },
      }),
      'POST /api/v1/library/repairs/apply': () => ({
        body: {
          data: {
            applied: 1,
            failed: 0,
            skipped: 0,
          },
        },
      }),
    })

    render(<IntegrityPanel isAdmin={true} />)

    await waitFor(() => {
      expect(screen.getByText('In Quarantäne')).toBeTruthy()
    })

    const quarBtn = screen.getByRole('button', { name: /In Quarantäne/i })
    fireEvent.click(quarBtn)

    await waitFor(() => {
      expect(screen.getByText('In Quarantäne verschieben (.ytmdl-trash)')).toBeTruthy()
      expect(screen.getByText('Bestätigen & Ausführen')).toBeTruthy()
    })

    const confirmBtn = screen.getByRole('button', { name: /Bestätigen & Ausführen/i })
    fireEvent.click(confirmBtn)

    await waitFor(() => {
      const applyCalls = calls.filter((c) => c.method === 'POST' && c.url.includes('/repairs/apply'))
      expect(applyCalls.length).toBeGreaterThan(0)
    })
  })

  it('renders Adoptieren button for exact catalog match and Tags reparieren for tag mismatch', async () => {
    const run: AuditRun = {
      id: 'run-actions',
      mode: 'quick',
      status: 'completed',
      started_at: new Date().toISOString(),
      scanned: 10,
      total: 10,
      findings_count: 2,
      created_at: new Date().toISOString(),
    }

    const findings: AuditFinding[] = [
      {
        id: 'finding-adopt',
        run_id: 'run-actions',
        finding_code: 'FILE_UNTRACKED',
        severity: 'info',
        relative_path: 'Artist 02/Album 02/track.opus',
        artist_name: 'Artist 02',
        track_title: 'Track 02',
        suggested_action: 'ADOPT_FILE',
        evidence: { level: 'EXACT_CATALOG_ID' },
        created_at: new Date().toISOString(),
      },
      {
        id: 'finding-tags',
        run_id: 'run-actions',
        finding_code: 'TAG_MISMATCH',
        severity: 'info',
        relative_path: 'Artist 03/Album 03/track.opus',
        artist_name: 'Artist 03',
        track_title: 'Track 03',
        suggested_action: 'RESTORE_TAGS',
        created_at: new Date().toISOString(),
      },
    ]

    stubFetch({
      'GET /api/v1/library/audits/current': () => ({ body: { data: run } }),
      'GET /api/v1/library/audits/run-actions/findings': () => ({
        body: { data: findings, meta: { count: 2, total: 2 } },
      }),
    })

    render(<IntegrityPanel isAdmin={true} />)

    await waitFor(() => {
      expect(screen.getByText('Adoptieren')).toBeTruthy()
      expect(screen.getByText('Tags reparieren')).toBeTruthy()
    })
  })
})
