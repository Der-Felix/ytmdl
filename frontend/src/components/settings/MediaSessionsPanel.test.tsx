import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { MediaSessionsPanel } from './MediaSessionsPanel'
import type { MediaSession } from '@/types/api'

let originalFetch: typeof fetch

beforeEach(() => {
  originalFetch = globalThis.fetch
})

afterEach(() => {
  globalThis.fetch = originalFetch
})

function createMockSession(overrides: Partial<MediaSession> = {}): MediaSession {
  return {
    id: 'sess_1',
    name: 'Standard Account',
    provider_family: 'youtube',
    enabled: true,
    health_status: 'healthy',
    has_credentials: true,
    in_use: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

describe('MediaSessionsPanel - List & States', () => {
  it('renders loading skeleton while fetching sessions', () => {
    globalThis.fetch = (() => new Promise(() => {})) as unknown as typeof fetch

    render(<MediaSessionsPanel />)
    expect(screen.getByText('YouTube-Sessions werden geladen')).toBeDefined()
  })

  it('renders empty state when no sessions are configured', async () => {
    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect(await screen.findByText('Keine YouTube-Session eingerichtet')).toBeDefined()
    expect(screen.getAllByText(/standardmäßig anonym/i).length).toBeGreaterThan(0)
    expect(screen.getByText('Anonymer Modus')).toBeDefined()
  })

  it('renders a healthy configured session', async () => {
    const session = createMockSession({ name: 'Haupt-Account' })
    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Haupt-Account')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Bereit').length).toBeGreaterThan(0)
    expect(screen.getByText(/1 bereit \/ 1 eingerichtet/)).toBeDefined()
  })

  it('renders distinct health badges for all statuses', async () => {
    const sessions: MediaSession[] = [
      createMockSession({ id: 's1', name: 'S1 Healthy', health_status: 'healthy' }),
      createMockSession({ id: 's2', name: 'S2 Unknown', health_status: 'unknown' }),
      createMockSession({ id: 's3', name: 'S3 Rate Limited', health_status: 'rate_limited' }),
      createMockSession({ id: 's4', name: 'S4 Bot Challenge', health_status: 'bot_challenge' }),
      createMockSession({ id: 's5', name: 'S5 Auth Failed', health_status: 'auth_failed' }),
      createMockSession({ id: 's6', name: 'S6 Cooldown', health_status: 'cooldown' }),
      createMockSession({ id: 's7', name: 'S7 No Creds', has_credentials: false, health_status: 'unknown' }),
    ]

    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: sessions }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('S1 Healthy')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Bereit').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Ungeprüft').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Rate-Limit').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Bot-Prüfung erforderlich').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Anmeldung erforderlich').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Abkühlphase / Pause').length).toBeGreaterThan(0)
    expect(screen.getAllByText('Cookies fehlen').length).toBeGreaterThan(0)
  })

  it('distinguishes disabled configuration from health status', async () => {
    const disabledHealthySession = createMockSession({
      name: 'Deaktivierter Account',
      enabled: false,
      health_status: 'healthy',
    })

    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [disabledHealthySession] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Deaktivierter Account')).length).toBeGreaterThan(0)
    // Health status badge still says "Bereit"
    expect(screen.getAllByText('Bereit').length).toBeGreaterThan(0)
    // Checkbox is unchecked
    const checkbox = screen.getAllByRole('checkbox', {
      name: /Session "Deaktivierter Account" aktivieren oder deaktivieren/i,
    })[0]
    expect(checkbox.getAttribute('aria-checked')).toBe('false')
  })

  it('displays legacy external session with restricted actions', async () => {
    const legacySession = createMockSession({
      id: 'legacy:default_cookiefile',
      name: 'Legacy Cookie File',
      has_credentials: true,
      health_status: 'healthy',
    })

    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [legacySession] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Legacy Cookie File')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('Extern konfiguriert').length).toBeGreaterThan(0)
    // Probe action is present
    expect(screen.getAllByRole('button', { name: /Session "Legacy Cookie File" testen/i }).length).toBeGreaterThan(0)
    // Replace and delete actions are NOT present for legacy
    expect(screen.queryByRole('button', { name: /Cookies für "Legacy Cookie File" ersetzen/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Session "Legacy Cookie File" löschen/i })).toBeNull()
  })

  it('displays in-use badge and future cooldown countdown', async () => {
    const futureTime = new Date(Date.now() + 10 * 60 * 1000).toISOString() // +10 minutes
    const inUseSession = createMockSession({
      name: 'Aktiver Download Session',
      in_use: true,
      cooldown_until: futureTime,
    })

    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [inUseSession] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Aktiver Download Session')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('In Verwendung').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/erneut verfügbar in \d+ Min\./i).length).toBeGreaterThan(0)
  })
})

describe('MediaSessionsPanel - Add Flow & Secret Safety', () => {
  it('validates input and prevents duplicate submissions', async () => {
    globalThis.fetch = (async () => {
      return new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    // Open add dialog
    const addBtn = (await screen.findAllByRole('button', { name: /Session hinzufügen/i }))[0]
    fireEvent.click(addBtn)

    expect(screen.getByRole('heading', { name: 'Session hinzufügen' })).toBeDefined()

    // Submit with empty fields
    const submitBtn = screen.getByRole('button', { name: 'Session speichern' })
    fireEvent.click(submitBtn)

    expect(await screen.findByText('Bitte gib einen Namen für die Session ein.')).toBeDefined()

    // Fill name only
    const nameInput = screen.getByLabelText('Name der Session')
    fireEvent.change(nameInput, { target: { value: 'Mein Account' } })
    fireEvent.click(submitBtn)

    expect(await screen.findByText('Bitte wähle eine Cookie-Datei aus.')).toBeDefined()
  })

  it('submits valid session and guarantees secret sentinel is never leaked', async () => {
    const sentinelSecret = 'SENTINEL_SECRET_TOKEN_DO_NOT_EXPOSE_998877'
    let postSessionCalled = false
    let uploadCookiesCalled = false

    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/admin/media-sessions') && init?.method === 'POST') {
        if (url.endsWith('/cookies')) {
          uploadCookiesCalled = true
          return new Response(
            JSON.stringify({
              data: {
                session: createMockSession({ id: 'new_id', name: 'Geheimer Account' }),
                probe: {
                  status: 'healthy',
                  tested_at: new Date().toISOString(),
                  metadata_ok: true,
                  usable_audio_formats: true,
                },
              },
            }),
            { status: 200, headers: { 'Content-Type': 'application/json' } },
          )
        }
        postSessionCalled = true
        return new Response(
          JSON.stringify({
            data: createMockSession({ id: 'new_id', name: 'Geheimer Account', has_credentials: false }),
          }),
          { status: 201, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }) as unknown as typeof fetch

    const { container } = render(<MediaSessionsPanel />)

    // Open Add Dialog
    const addBtn = (await screen.findAllByRole('button', { name: /Session hinzufügen/i }))[0]
    fireEvent.click(addBtn)

    const nameInput = screen.getByLabelText('Name der Session')
    fireEvent.change(nameInput, { target: { value: 'Geheimer Account' } })

    const fileInput = screen.getByLabelText(/Cookie-Datei/i)
    const file = new File([`# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 2000000000 SID ${sentinelSecret}`], 'my_cookies.txt', {
      type: 'text/plain',
    })
    fireEvent.change(fileInput, { target: { files: [file] } })

    // UI shows generic confirmation, not filename or content
    expect(screen.getByText('Cookie-Datei ausgewählt')).toBeDefined()
    expect(screen.queryByText(sentinelSecret)).toBeNull()
    expect(screen.queryByText('my_cookies.txt')).toBeNull()

    const submitBtn = screen.getByRole('button', { name: 'Session speichern' })
    fireEvent.click(submitBtn)

    await waitFor(() => {
      expect(postSessionCalled).toBe(true)
      expect(uploadCookiesCalled).toBe(true)
    })

    // Success notification shown
    expect(await screen.findByText('YouTube-Session wurde hinzugefügt.')).toBeDefined()

    // CRITICAL SECRET SAFETY CHECK: Verify sentinel does NOT appear anywhere in DOM!
    expect(container.innerHTML).not.toContain(sentinelSecret)
    expect(document.body.innerHTML).not.toContain(sentinelSecret)
  })

  it('handles create success + upload failure cleanly without losing state', async () => {
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/cookies')) {
        return new Response(
          JSON.stringify({
            error: {
              code: 'INVALID_REQUEST',
              message: 'Cookie file exceeds 1 MiB limit.',
            },
          }),
          { status: 400, headers: { 'Content-Type': 'application/json' } },
        )
      }
      if (url.includes('/admin/media-sessions') && init?.method === 'POST') {
        return new Response(
          JSON.stringify({
            data: createMockSession({ id: 'sess_partial', name: 'Partial Session', has_credentials: false }),
          }),
          { status: 201, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    const addBtn = (await screen.findAllByRole('button', { name: /Session hinzufügen/i }))[0]
    fireEvent.click(addBtn)

    const nameInput = screen.getByLabelText('Name der Session')
    fireEvent.change(nameInput, { target: { value: 'Partial Session' } })

    const fileInput = screen.getByLabelText(/Cookie-Datei/i)
    const file = new File(['dummy'], 'large_cookies.txt', { type: 'text/plain' })
    fireEvent.change(fileInput, { target: { files: [file] } })

    const submitBtn = screen.getByRole('button', { name: 'Session speichern' })
    fireEvent.click(submitBtn)

    // Shows informative error that metadata was created and cookies can be replaced
    expect(await screen.findByText(/wurde erstellt, aber der Cookie-Upload ist fehlgeschlagen/i)).toBeDefined()
    expect(screen.getByText(/1 MB/i)).toBeDefined()
  })
})

describe('MediaSessionsPanel - Replace & Delete Actions', () => {
  it('replaces cookies with safety note that old credentials survive failure', async () => {
    const session = createMockSession({ id: 'sess_rep', name: 'Zu ersetzender Account' })

    let uploadAttempted = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/cookies')) {
        uploadAttempted = true
        return new Response(
          JSON.stringify({
            error: {
              code: 'SESSION_BOT_CHALLENGE',
              message: 'YouTube bot challenge encountered during probe',
            },
          }),
          { status: 403, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Zu ersetzender Account')).length).toBeGreaterThan(0)

    // Open replace dialog
    const replaceBtn = (await screen.findAllByRole('button', { name: /Cookies für "Zu ersetzender Account" ersetzen/i }))[0]
    fireEvent.click(replaceBtn)

    expect(screen.getByRole('heading', { name: 'Cookies ersetzen' })).toBeDefined()
    expect(screen.getByText(/bleiben die bisherigen funktionierenden Cookies aktiv/i)).toBeDefined()

    const fileInput = screen.getByLabelText(/Neue Cookie-Datei/i)
    const file = new File(['valid content'], 'new.txt', { type: 'text/plain' })
    fireEvent.change(fileInput, { target: { files: [file] } })

    const submitBtn = screen.getByRole('button', { name: 'Cookies aktualisieren' })
    fireEvent.click(submitBtn)

    await waitFor(() => expect(uploadAttempted).toBe(true))

    // Verifies the exact required message that existing cookies remain active on failure!
    expect(
      await screen.findByText(/Die neuen Cookies konnten nicht verwendet werden\. Die bisherigen Cookies bleiben aktiv\./i),
    ).toBeDefined()
  })

  it('handles 409 SESSION_IN_USE conflict during cookie replacement', async () => {
    const session = createMockSession({ id: 'sess_rep_busy', name: 'Beschäftigte Session' })

    globalThis.fetch = (async (url: string) => {
      if (url.includes('/cookies')) {
        return new Response(
          JSON.stringify({
            error: {
              code: 'SESSION_IN_USE',
              message: 'Session is currently in use',
            },
          }),
          { status: 409, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    const replaceBtn = (await screen.findAllByRole('button', { name: /Cookies für "Beschäftigte Session" ersetzen/i }))[0]
    fireEvent.click(replaceBtn)

    const fileInput = screen.getByLabelText(/Neue Cookie-Datei/i)
    fireEvent.change(fileInput, { target: { files: [new File(['x'], 'c.txt')] } })

    const submitBtn = screen.getByRole('button', { name: 'Cookies aktualisieren' })
    fireEvent.click(submitBtn)

    expect(
      await screen.findByText('Die Session wird gerade für einen Download verwendet. Versuche es nach Abschluss des Downloads erneut.'),
    ).toBeDefined()
  })

  it('requires confirmation before delete and handles 409 SESSION_IN_USE', async () => {
    const session = createMockSession({ id: 'sess_del_busy', name: 'Lösch-Kandidat' })

    let deleteCalled = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (init?.method === 'DELETE') {
        deleteCalled = true
        return new Response(
          JSON.stringify({
            error: {
              code: 'SESSION_IN_USE',
              message: 'Session in use',
            },
          }),
          { status: 409, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    const deleteBtn = (await screen.findAllByRole('button', { name: /Session "Lösch-Kandidat" löschen/i }))[0]
    fireEvent.click(deleteBtn)

    // Confirmation dialog open
    expect(screen.getByRole('heading', { name: 'Session löschen' })).toBeDefined()
    expect(screen.getByText(/unwiderruflich vom Server/i)).toBeDefined()

    const confirmDeleteBtn = screen.getByRole('button', { name: 'Session löschen' })
    fireEvent.click(confirmDeleteBtn)

    await waitFor(() => expect(deleteCalled).toBe(true))

    expect(
      await screen.findByText('Die Session wird gerade für einen Download verwendet. Versuche es nach Abschluss des Downloads erneut.'),
    ).toBeDefined()

    // Row is preserved!
    expect((await screen.findAllByText('Lösch-Kandidat')).length).toBeGreaterThan(0)
  })
})

describe('MediaSessionsPanel - Probe & Toggle', () => {
  it('executes probe with spinner and updates session state', async () => {
    const session = createMockSession({
      id: 'sess_probe',
      name: 'Probe Target',
      health_status: 'unknown',
    })

    let probeCalled = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (url.includes('/probe')) {
        probeCalled = true
        return new Response(
          JSON.stringify({
            data: {
              probe: {
                status: 'healthy',
                tested_at: new Date().toISOString(),
                metadata_ok: true,
                usable_audio_formats: true,
              },
              session: {
                ...session,
                health_status: 'healthy',
              },
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Probe Target')).length).toBeGreaterThan(0)

    const probeBtn = (await screen.findAllByRole('button', { name: /Session "Probe Target" testen/i }))[0]
    fireEvent.click(probeBtn)

    await waitFor(() => expect(probeCalled).toBe(true))

    expect(await screen.findByText('Session-Test für "Probe Target" erfolgreich: Bereit.')).toBeDefined()
    expect(screen.getAllByText('Bereit').length).toBeGreaterThan(0)
  })

  it('toggles enabled state while keeping health distinct', async () => {
    const session = createMockSession({
      id: 'sess_toggle',
      name: 'Toggle Target',
      enabled: true,
      health_status: 'healthy',
    })

    let patchCalled = false
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      if (init?.method === 'PATCH') {
        patchCalled = true
        const body = JSON.parse(init.body as string)
        return new Response(
          JSON.stringify({
            data: {
              ...session,
              enabled: body.enabled,
            },
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } },
        )
      }
      return new Response(JSON.stringify({ data: [session] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    }) as unknown as typeof fetch

    render(<MediaSessionsPanel />)

    expect((await screen.findAllByText('Toggle Target')).length).toBeGreaterThan(0)

    const checkbox = screen.getAllByRole('checkbox', {
      name: /Session "Toggle Target" aktivieren oder deaktivieren/i,
    })[0]
    fireEvent.click(checkbox)

    await waitFor(() => expect(patchCalled).toBe(true))

    expect(await screen.findByText('Session "Toggle Target" deaktiviert.')).toBeDefined()
    // Health status remains "Bereit"
    expect(screen.getAllByText('Bereit').length).toBeGreaterThan(0)
  })
})
