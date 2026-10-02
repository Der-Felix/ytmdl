import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { ApiError } from '@/lib/api/client'
import { explainProblem } from '@/lib/problems'
import { ErrorState } from './state-view'
import { ProblemNotice } from './problem-notice'

describe('actionable error presentation', () => {
  it('explains provider connectivity without guessing a cookie failure or exposing diagnostics', () => {
    render(<ErrorState error={new ApiError({ code: 'PROVIDER_UNAVAILABLE', status: 503, message: 'YouTube Music could not be reached: https://secret:password@private.invalid', requestId: 'request-123' })} onRetry={() => {}} />)
    expect(screen.getByText('Musikquelle nicht erreichbar')).toBeDefined()
    expect(screen.getByText(/Ob die Ursache beim Anbieter, der Verbindung oder dem Proxy liegt/)).toBeDefined()
    expect(screen.getByRole('button', { name: 'Erneut versuchen' })).toBeDefined()
    expect(document.body.textContent).not.toContain('secret:password')
    expect(document.body.textContent).not.toContain('Cookies sind abgelaufen')
    const details = screen.getByText('Technische Details').closest('details')!
    expect(details.open).toBe(false)
    expect(details.textContent).toContain('PROVIDER_UNAVAILABLE')
    expect(details.textContent).toContain('request-123')
  })

  it('keeps challenge and cooldown failures out of immediate retry controls even on HTTP 503', () => {
    render(<ErrorState error={new ApiError({ code: 'SESSION_BOT_CHALLENGE', status: 503, message: 'challenge' })} onRetry={() => {}} />)
    expect(screen.getByText(/Neue Cookies heben eine laufende Schutzpause nicht auf/)).toBeDefined()
    expect(screen.queryByRole('button', { name: 'Erneut versuchen' })).toBeNull()
    expect(explainProblem('SESSION_UNAVAILABLE', 503).retryable).toBe(false)
    expect(explainProblem('PROVIDER_RATE_LIMITED', 429).retryable).toBe(false)
  })

  it('distinguishes an unreachable server from an unknown server failure', () => {
    const { unmount } = render(<ErrorState error={new ApiError({ code: 'INTERNAL_ERROR', status: 0, message: 'fetch failed' })} />)
    expect(screen.getByText('Verbindung zum Server unterbrochen')).toBeDefined()
    unmount()
    render(<ErrorState error={new ApiError({ code: 'NEW_SERVER_ERROR', status: 500, message: 'private output' })} />)
    expect(screen.getByText('Die genaue Ursache ist noch nicht bekannt.')).toBeDefined()
    expect(document.body.textContent).not.toContain('private output')
  })

  it('does not promise an automatic repeat for a failed search request', () => {
    render(<ErrorState error={new ApiError({ code: 'SESSION_UNAVAILABLE', status: 503, message: 'No session' })} onRetry={() => {}} />)
    expect(document.body.textContent).not.toContain('automatisch')
    expect(screen.queryByRole('button', { name: 'Erneut versuchen' })).toBeNull()
  })

  it('does not suggest retrying permission, validation or storage-identity failures', () => {
    for (const [code, status] of [['FORBIDDEN', 403], ['INVALID_REQUEST', 400], ['STORAGE_GUARD_MISMATCH', 503]] as const) {
      expect(explainProblem(code, status).retryable).toBe(false)
    }
    expect(explainProblem('NEW_VALIDATION_CODE', 400).retryable).toBe(false)
    expect(explainProblem('INTERNAL_ERROR', 403).title).toBe('Keine Berechtigung für diese Aktion')
    expect(explainProblem('INTERNAL_ERROR', 413).title).toBe('Datei ist zu groß')
    expect(explainProblem('INTERNAL_ERROR', 429).retryable).toBe(false)
  })

  it('only renders bounded technical identifiers and preserves scheduled continuation', () => {
    render(<ProblemNotice code="SESSION_UNAVAILABLE" requestId="https://private.invalid/token" waiting continuation="Automatische Fortsetzung ab 14:20" />)
    expect(screen.getByText('Automatische Fortsetzung ab 14:20')).toBeDefined()
    expect(document.body.textContent).not.toContain('private.invalid')
    expect(screen.queryByText('Anfrage-ID:')).toBeNull()
  })
})
