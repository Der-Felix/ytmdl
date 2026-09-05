import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { AppContent } from './App'
import { AuthContext } from '@/contexts/auth-context'
import { navigate } from '@/lib/router'

// Mock hooks and sub-page APIs
mock.module('@/hooks/useJobs', () => ({
  useJobs: () => ({ state: { status: 'success', data: [] }, meta: { total: 0 }, reload: async () => {} }),
  useConnectionState: () => 'open',
  useCurrentTracks: () => ({ activeTracks: [] }),
}))

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({
    currentTrack: null,
  }),
}))

mock.module('@/lib/api/users', () => ({
  listUsers: async () => ({ items: [], meta: { count: 0 } }),
}))

mock.module('@/lib/api/settings', () => ({
  getHealth: async () => ({
    version: '0.17.3',
    status: 'ok',
    checks: {},
  }),
  getSettings: async () => ({
    concurrent_downloads: 2,
    rate_limit_kbs: 0,
    download_window_start: '',
    download_window_end: '',
    skip_existing: true,
    default_metadata_provider: 'ytmusic',
    preferred_audio_format: 'opus',
    default_release_filter: ['album', 'single', 'ep'],
    library_path: '/music',
    match_min_score: 80,
    match_duration_tolerance_ms: 5000,
    allow_transcode: true,
  }),
  listProviders: async () => [],
}))

mock.module('@/lib/api/storage', () => ({
  getStorageStatus: async () => ({
    library: { status: 'healthy' },
    staging: {},
    queue: {},
  }),
}))

mock.module('@/lib/api/system', () => ({
  getUpdateStatus: async () => ({
    current_version: '0.17.3',
    latest_version: '0.17.3',
    state: 'up_to_date',
  }),
}))

describe('App Layout Consolidation and Route Permissions', () => {
  const adminAuth = {
    user: {
      id: 'usr-admin',
      username: 'admin',
      display_name: 'Administrator',
      role: 'admin' as const,
      enabled: true,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      last_login_at: null,
    },
    loading: false,
    setupRequired: false,
    isAdmin: true,
    login: async () => {},
    logout: async () => {},
    refresh: async () => {},
    checkSetup: async () => {},
  }

  const normalUserAuth = {
    ...adminAuth,
    user: {
      ...adminAuth.user,
      id: 'usr-member',
      username: 'member',
      display_name: 'Member',
      role: 'user' as const,
    },
    isAdmin: false,
  }

  const unauthenticatedAuth = {
    ...adminAuth,
    user: null,
    isAdmin: false,
  }

  beforeEach(() => {
    ;(window as any).happyDOM.setURL('http://localhost/')
  })

  it('renders Users within AppShell (with sidebar and header) for admin on /users', async () => {
    ;(window as any).happyDOM.setURL('http://localhost/users')

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppContent />
      </AuthContext.Provider>,
    )

    // Standard AppShell components should be present
    expect(screen.getByRole('navigation', { name: 'Hauptnavigation' })).toBeDefined()
    expect(screen.getByRole('link', { name: /Benutzerverwaltung/i }).getAttribute('aria-current')).toBe('page')

    // Users page content should be rendered
    expect(await screen.findByRole('heading', { level: 1, name: 'Benutzerverwaltung' })).toBeDefined()
    expect(screen.getByText('Erstelle und verwalte Benutzerkonten, Rollen und Zugriffsrechte.')).toBeDefined()

    // No old AdminLayout markers
    expect(screen.queryByText('Zurück zu YTMDL')).toBeNull()
  })

  it('renders NotFound within AppShell for non-admin on /users', async () => {
    ;(window as any).happyDOM.setURL('http://localhost/users')

    render(
      <AuthContext.Provider value={normalUserAuth}>
        <AppContent />
      </AuthContext.Provider>,
    )

    // Sidebar remains present
    expect(screen.getByRole('navigation', { name: 'Hauptnavigation' })).toBeDefined()

    // NotFound page should be rendered
    expect(await screen.findByRole('heading', { level: 1, name: 'Seite nicht gefunden' })).toBeDefined()
    expect(screen.queryByRole('heading', { level: 1, name: 'Benutzerverwaltung' })).toBeNull()
  })

  it('renders Settings within AppShell (with sidebar and header) for admin on /settings/server', async () => {
    ;(window as any).happyDOM.setURL('http://localhost/settings/server')

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppContent />
      </AuthContext.Provider>,
    )

    expect(screen.getByRole('navigation', { name: 'Hauptnavigation' })).toBeDefined()
    expect(screen.getByRole('link', { name: /Servereinstellungen/i }).getAttribute('aria-current')).toBe('page')

    expect(await screen.findByRole('heading', { level: 1, name: 'Servereinstellungen' })).toBeDefined()
    expect(screen.getByText('Konfiguration, Diagnose und Dienste verwalten.')).toBeDefined()

    // No old AdminLayout markers
    expect(screen.queryByText('Zurück zu YTMDL')).toBeNull()
  })

  it('renders NotFound within AppShell for non-admin on /settings/server', async () => {
    ;(window as any).happyDOM.setURL('http://localhost/settings/server')

    render(
      <AuthContext.Provider value={normalUserAuth}>
        <AppContent />
      </AuthContext.Provider>,
    )

    expect(screen.getByRole('navigation', { name: 'Hauptnavigation' })).toBeDefined()
    expect(await screen.findByRole('heading', { level: 1, name: 'Seite nicht gefunden' })).toBeDefined()
    expect(screen.queryByRole('heading', { level: 1, name: 'Servereinstellungen' })).toBeNull()
  })

  it('renders standalone Login page without AppShell when not authenticated', async () => {
    ;(window as any).happyDOM.setURL('http://localhost/dashboard')

    render(
      <AuthContext.Provider value={unauthenticatedAuth}>
        <AppContent />
      </AuthContext.Provider>,
    )

    // No sidebar
    expect(screen.queryByRole('navigation', { name: 'Hauptnavigation' })).toBeNull()
    // Login form should be present
    expect(await screen.findByRole('heading', { level: 1, name: /Anmelden bei YTMDL/i })).toBeDefined()
  })
})
