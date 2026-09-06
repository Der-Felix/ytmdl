import { describe, expect, it } from 'bun:test'

import {
  createMediaSession,
  deleteMediaSession,
  getMediaSession,
  isLegacySession,
  listMediaSessions,
  mapMediaSessionError,
  probeMediaSession,
  updateMediaSession,
  uploadMediaSessionCookies,
} from './mediaSessions'
import { ApiError } from './client'
import type { MediaSession } from '@/types/api'

describe('mediaSessions api client', () => {
  it('identifies legacy session accurately', () => {
    expect(isLegacySession('legacy:default_cookiefile')).toBe(true)
    expect(isLegacySession({ id: 'legacy:default_cookiefile' })).toBe(true)
    expect(isLegacySession('legacy:something_else')).toBe(true)
    expect(isLegacySession({ id: 'sess_123' })).toBe(false)
    expect(isLegacySession('sess_123')).toBe(false)
  })

  it('maps errors correctly', () => {
    const inUseErr = new ApiError({
      code: 'SESSION_IN_USE',
      message: 'Session is in use',
      status: 409,
    })
    expect(mapMediaSessionError(inUseErr)).toContain('wird gerade für einen Download verwendet')

    const rateLimitErr = new ApiError({
      code: 'SESSION_RATE_LIMITED',
      message: 'rate limited',
      status: 429,
    })
    expect(mapMediaSessionError(rateLimitErr)).toContain('YouTube begrenzt diese Session derzeit')

    const botErr = new ApiError({
      code: 'SESSION_BOT_CHALLENGE',
      message: 'bot challenge',
      status: 403,
    })
    expect(mapMediaSessionError(botErr)).toContain('muss erneuert werden')

    const authErr = new ApiError({
      code: 'SESSION_AUTH_FAILED',
      message: 'auth failed',
      status: 401,
    })
    expect(mapMediaSessionError(authErr)).toContain('Anmeldung dieser Session ist nicht mehr gültig')

    const csrfErr = new ApiError({
      code: 'CSRF_INVALID',
      message: 'bad csrf',
      status: 403,
    })
    expect(mapMediaSessionError(csrfErr)).toContain('Sicherheits-Token')

    const oversizedErr = new ApiError({
      code: 'INVALID_REQUEST',
      message: 'Cookie file exceeds 1 MiB limit.',
      status: 400,
    })
    expect(mapMediaSessionError(oversizedErr)).toContain('1 MB')

    const malformedErr = new ApiError({
      code: 'INVALID_REQUEST',
      message: 'Malformed cookie file: Netscape format expected',
      status: 400,
    })
    expect(mapMediaSessionError(malformedErr)).toContain('Netscape-Format')

    const genericErr = new Error('Random connection drop')
    expect(mapMediaSessionError(genericErr)).toBe('Random connection drop')
  })

  it('lists media sessions', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string) => {
      expect(url).toContain('/admin/media-sessions')
      return new Response(
        JSON.stringify({
          data: [
            {
              id: 'sess_1',
              name: 'Test Session',
              provider_family: 'youtube',
              enabled: true,
              health_status: 'healthy',
              has_credentials: true,
              in_use: false,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            },
          ],
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await listMediaSessions()
      expect(res.length).toBe(1)
      expect(res[0].id).toBe('sess_1')
      expect(res[0].name).toBe('Test Session')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('creates a media session', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/admin/media-sessions')
      expect(init?.method).toBe('POST')
      const parsedBody = JSON.parse(init?.body as string)
      expect(parsedBody.name).toBe('New YouTube Account')
      return new Response(
        JSON.stringify({
          data: {
            id: 'sess_new',
            name: 'New YouTube Account',
            provider_family: 'youtube',
            enabled: true,
            health_status: 'unknown',
            has_credentials: false,
            in_use: false,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        }),
        { status: 201, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const created = await createMediaSession({
        name: 'New YouTube Account',
        provider_family: 'youtube',
      })
      expect(created.id).toBe('sess_new')
      expect(created.has_credentials).toBe(false)
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('uploads cookies via multipart form', async () => {
    const originalFetch = globalThis.fetch
    let uploadedFormData: FormData | null = null

    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/admin/media-sessions/sess_123/cookies')
      expect(init?.method).toBe('POST')
      expect(init?.body instanceof FormData).toBe(true)
      uploadedFormData = init?.body as FormData

      return new Response(
        JSON.stringify({
          data: {
            session: {
              id: 'sess_123',
              name: 'Updated Session',
              provider_family: 'youtube',
              enabled: true,
              health_status: 'healthy',
              has_credentials: true,
              in_use: false,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            },
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
    }) as unknown as typeof fetch

    try {
      const file = new File(['# Netscape HTTP Cookie File\n.youtube.com TRUE / FALSE 2000000000 SID abc'], 'cookies.txt', {
        type: 'text/plain',
      })
      const res = await uploadMediaSessionCookies('sess_123', file)
      expect(res.session.has_credentials).toBe(true)
      expect(res.probe.status).toBe('healthy')
      expect(uploadedFormData).not.toBeNull()
      expect(uploadedFormData!.get('cookie_file')).toBeDefined()
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('probes a media session', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/admin/media-sessions/sess_456/probe')
      expect(init?.method).toBe('POST')
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
              id: 'sess_456',
              name: 'Probed Session',
              provider_family: 'youtube',
              enabled: true,
              health_status: 'healthy',
              has_credentials: true,
              in_use: false,
              created_at: new Date().toISOString(),
              updated_at: new Date().toISOString(),
            },
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await probeMediaSession('sess_456')
      expect(res.probe.status).toBe('healthy')
      expect(res.session.id).toBe('sess_456')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('updates a media session', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/admin/media-sessions/sess_789')
      expect(init?.method).toBe('PATCH')
      const body = JSON.parse(init?.body as string)
      expect(body.enabled).toBe(false)
      return new Response(
        JSON.stringify({
          data: {
            id: 'sess_789',
            name: 'Disabled Session',
            provider_family: 'youtube',
            enabled: false,
            health_status: 'healthy',
            has_credentials: true,
            in_use: false,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          },
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } },
      )
    }) as unknown as typeof fetch

    try {
      const res = await updateMediaSession('sess_789', { enabled: false })
      expect(res.enabled).toBe(false)
      expect(res.health_status).toBe('healthy')
    } finally {
      globalThis.fetch = originalFetch
    }
  })

  it('deletes a media session', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = (async (url: string, init?: RequestInit) => {
      expect(url).toContain('/admin/media-sessions/sess_del')
      expect(init?.method).toBe('DELETE')
      return new Response(null, { status: 204 })
    }) as unknown as typeof fetch

    try {
      await deleteMediaSession('sess_del')
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})
