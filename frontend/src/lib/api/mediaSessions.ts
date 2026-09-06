import { ApiError, request, requestVoid, type RequestOptions } from '@/lib/api/client'
import type {
  CreateMediaSessionPayload,
  MediaSession,
  MediaSessionProbeResult,
  UpdateMediaSessionPayload,
} from '@/types/api'

export interface UploadCookiesResponse {
  session: MediaSession
  probe: MediaSessionProbeResult
}

export interface ProbeMediaSessionResponse {
  probe: MediaSessionProbeResult
  session: MediaSession
}

/** Lists all media sessions (Admin only). */
export function listMediaSessions(
  options: RequestOptions = {},
): Promise<MediaSession[]> {
  return request<MediaSession[]>('/admin/media-sessions', options)
}

/** Gets a media session by ID (Admin only). */
export function getMediaSession(
  id: string,
  options: RequestOptions = {},
): Promise<MediaSession> {
  return request<MediaSession>(`/admin/media-sessions/${encodeURIComponent(id)}`, options)
}

/** Creates a new media session (Admin only). */
export function createMediaSession(
  payload: CreateMediaSessionPayload,
  options: RequestOptions = {},
): Promise<MediaSession> {
  return request<MediaSession>('/admin/media-sessions', {
    ...options,
    method: 'POST',
    body: payload,
  })
}

/** Installs or replaces cookies for a media session (Admin only). */
export function uploadMediaSessionCookies(
  id: string,
  file: File | Blob,
  options: RequestOptions = {},
): Promise<UploadCookiesResponse> {
  const formData = new FormData()
  formData.append('cookie_file', file)
  return request<UploadCookiesResponse>(
    `/admin/media-sessions/${encodeURIComponent(id)}/cookies`,
    {
      ...options,
      method: 'POST',
      body: formData,
    },
  )
}

/** Explicitly probes a media session (Admin only). */
export function probeMediaSession(
  id: string,
  options: RequestOptions = {},
): Promise<ProbeMediaSessionResponse> {
  return request<ProbeMediaSessionResponse>(
    `/admin/media-sessions/${encodeURIComponent(id)}/probe`,
    {
      ...options,
      method: 'POST',
    },
  )
}

/** Updates a media session's metadata, such as name or enabled status (Admin only). */
export function updateMediaSession(
  id: string,
  payload: UpdateMediaSessionPayload,
  options: RequestOptions = {},
): Promise<MediaSession> {
  return request<MediaSession>(
    `/admin/media-sessions/${encodeURIComponent(id)}`,
    {
      ...options,
      method: 'PATCH',
      body: payload,
    },
  )
}

/** Deletes a media session (Admin only). */
export function deleteMediaSession(
  id: string,
  options: RequestOptions = {},
): Promise<void> {
  return requestVoid(
    `/admin/media-sessions/${encodeURIComponent(id)}`,
    {
      ...options,
      method: 'DELETE',
    },
  )
}

/** Checks whether a session is a synthetic/external legacy session. */
export function isLegacySession(session: Pick<MediaSession, 'id'> | string): boolean {
  const id = typeof session === 'string' ? session : session.id
  return id === 'legacy:default_cookiefile' || id.startsWith('legacy:')
}

/** Centralized mapping of backend error codes to user-facing German messages. */
export function mapMediaSessionError(err: unknown, fallback?: string): string {
  if (err instanceof ApiError) {
    switch (err.code) {
      case 'SESSION_IN_USE':
        return 'Die Session wird gerade für einen Download verwendet. Versuche es nach Abschluss des Downloads erneut.'
      case 'SESSION_RATE_LIMITED':
      case 'RATE_LIMITED':
      case 'PROVIDER_RATE_LIMITED':
        return 'YouTube begrenzt diese Session derzeit. YTMDL verwendet sie bis zum Ende der Abkühlzeit nicht.'
      case 'SESSION_BOT_CHALLENGE':
        return 'Diese YouTube-Session muss erneuert werden. Exportiere neue Cookies aus einer funktionierenden Browser-Sitzung.'
      case 'SESSION_AUTH_FAILED':
        return 'Die Anmeldung dieser Session ist nicht mehr gültig.'
      case 'SESSION_NOT_FOUND':
        return 'Die angeforderte Session wurde nicht gefunden.'
      case 'FORBIDDEN':
      case 'UNAUTHENTICATED':
        return 'Für diese Aktion sind Administratorrechte erforderlich.'
      case 'CSRF_INVALID':
        return 'Die Sitzung ist abgelaufen oder das Sicherheits-Token ist ungültig. Bitte lade die Seite neu.'
      case 'INVALID_REQUEST':
        if (/1\s*MiB|1\s*MB|limit|too large|exceeds/i.test(err.message)) {
          return 'Die Cookie-Datei überschreitet das Limit von 1 MB.'
        }
        if (/Netscape|malformed|format|cookie/i.test(err.message)) {
          return 'Die Cookie-Datei ist ungültig oder entspricht nicht dem Netscape-Format.'
        }
        return err.message || 'Die Anfrage ist ungültig.'
      default:
        if (err.status === 409) {
          return 'Die Session wird gerade für einen Download verwendet. Versuche es nach Abschluss des Downloads erneut.'
        }
        if (err.message) return err.message
    }
  }
  if (err instanceof Error) {
    return err.message || fallback || 'Ein unerwarteter Fehler ist aufgetreten.'
  }
  return fallback || 'Ein unbekannter Fehler ist aufgetreten.'
}
