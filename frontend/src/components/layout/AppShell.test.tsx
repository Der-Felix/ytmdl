import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { render, screen, within } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { AppShell } from './AppShell'
import { resetStoredArea, saveArea } from '@/lib/areas'
import type { Route } from '@/lib/router'

// Mock usePlayer hook for AppShell
mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({
    currentTrack: null,
  }),
  usePlayerState: () => ({
    currentTrack: null,
  }),
  usePlayerProgress: () => ({
    currentTime: 0,
    duration: 0,
  }),
  usePlayerActions: () => ({}),
}))

describe('AppShell Layout', () => {
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

  const standardUserAuth = {
    ...adminAuth,
    user: {
      ...adminAuth.user,
      id: 'usr-normal',
      username: 'member',
      display_name: 'Member User',
      role: 'user' as const,
    },
    isAdmin: false,
  }

  it('renders standard sidebar and highlights Benutzerverwaltung on /users for admin', () => {
    const route: Route = { name: 'users' }

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div data-testid="page-content">Benutzerverwaltung Inhalt</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    // Sidebar should contain both main navigation and administration
    expect(screen.getByRole('navigation', { name: 'Hauptnavigation' })).toBeDefined()
    expect(screen.getByText('Administration')).toBeDefined()

    const usersLink = screen.getByRole('link', { name: /Benutzerverwaltung/i })
    expect(usersLink).toBeDefined()
    expect(usersLink.getAttribute('aria-current')).toBe('page')

    // Page content is rendered inside the shell
    expect(screen.getByTestId('page-content').textContent).toBe('Benutzerverwaltung Inhalt')
  })

  it('renders standard sidebar and highlights Servereinstellungen on /settings/server for admin', () => {
    const route: Route = { name: 'settings' }

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div data-testid="page-content">Servereinstellungen Inhalt</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    expect(screen.getByText('Administration')).toBeDefined()
    const settingsLink = screen.getByRole('link', { name: /Servereinstellungen/i })
    expect(settingsLink).toBeDefined()
    expect(settingsLink.getAttribute('aria-current')).toBe('page')
  })

  it('does not render administration section in sidebar for non-admin users', () => {
    const route: Route = { name: 'downloadHome' }

    render(
      <AuthContext.Provider value={standardUserAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div>Laden</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    expect(screen.queryByText('Administration')).toBeNull()
    expect(screen.queryByRole('link', { name: /Benutzerverwaltung/i })).toBeNull()
    expect(screen.queryByRole('link', { name: /Servereinstellungen/i })).toBeNull()
  })

  it('hides sidebar and header on player route (/player) for immersive fullscreen', () => {
    const route: Route = { name: 'player' }

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div data-testid="player-view">Player View</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    // No desktop or mobile sidebar on player route
    expect(screen.queryByRole('navigation', { name: 'Hauptnavigation' })).toBeNull()
    expect(screen.getByTestId('player-view')).toBeDefined()
  })

  it('renders brand logo in sidebar and mobile header', () => {
    const route: Route = { name: 'listenHome' }

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div>Hören</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    const desktopLogo = screen.getByTestId('brand-logo')
    expect(desktopLogo).toBeDefined()
    expect(desktopLogo.getAttribute('src')).toBe('/logo-mark.png')

    const mobileLogo = screen.getByTestId('brand-logo-mobile')
    expect(mobileLogo).toBeDefined()
    expect(mobileLogo.getAttribute('src')).toBe('/logo-mark.png')
  })

  it('renders mobile-only header with md:hidden and no Live indicator', () => {
    const route: Route = { name: 'listenHome' }

    const { container } = render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div data-testid="page-content">Hören Content</div>
        </AppShell>
      </AuthContext.Provider>,
    )

    // Header element must exist only as a mobile bar (md:hidden)
    const header = container.querySelector('header')
    expect(header).not.toBeNull()
    expect(header?.className).toContain('md:hidden')

    // Mobile navigation trigger button exists
    expect(screen.getByRole('button', { name: 'Navigation öffnen' })).toBeDefined()

    // Mobile logo exists
    expect(screen.getByTestId('brand-logo-mobile')).toBeDefined()

    // No "Live" badge or connection status indicator exists anywhere
    expect(screen.queryByText('Live')).toBeNull()
    expect(screen.queryByTitle(/Live-Updates/i)).toBeNull()
  })
})

describe('AppShell areas', () => {
  const auth = {
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

  beforeEach(() => resetStoredArea())

  function sidebarLabels(route: Route): string[] {
    render(
      <AuthContext.Provider value={auth}>
        <AppShell route={route} activeDownloads={0}>
          <div />
        </AppShell>
      </AuthContext.Provider>,
    )
    const nav = screen.getByRole('navigation', { name: 'Hauptnavigation' })
    return within(nav)
      .getAllByRole('link')
      .map((link) => link.textContent ?? '')
  }

  it('lists the listening entries, in order, plus administration in Hören', () => {
    expect(sidebarLabels({ name: 'listenHome' })).toEqual([
      'Für dich',
      'Player',
      'Bibliothek',
      'Playlists',
      'Favoriten',
      'Offline-Musik',
      'Benutzerverwaltung',
      'Servereinstellungen',
    ])
  })

  it('lists the download entries, in order, plus administration in Laden', () => {
    expect(sidebarLabels({ name: 'downloads' })).toEqual([
      'Suche',
      'Entdecken',
      'Downloads',
      'Abonnements',
      'Benutzerverwaltung',
      'Servereinstellungen',
    ])
  })

  it('keeps the profile link in both areas', () => {
    sidebarLabels({ name: 'discover', query: '' })
    expect(screen.getByRole('link', { name: 'Profil & Sicherheit' })).toBeDefined()
  })

  it('shows the last area on a neutral page', () => {
    saveArea('download')
    const labels = sidebarLabels({ name: 'profile' })
    expect(labels[0]).toBe('Suche')
    const switches = screen.getAllByRole('navigation', { name: 'Bereich' })
    for (const area of switches) {
      expect(within(area).getByRole('link', { name: /Laden/ }).getAttribute('aria-current')).toBe('true')
    }
  })

  it('renders the switch in the mobile header and above the page', () => {
    const { container } = render(
      <AuthContext.Provider value={auth}>
        <AppShell route={{ name: 'users' }} activeDownloads={0}>
          <div />
        </AppShell>
      </AuthContext.Provider>,
    )
    const switches = screen.getAllByRole('navigation', { name: 'Bereich' })
    expect(switches).toHaveLength(2)
    expect(container.querySelector('header')?.contains(switches[0]!)).toBe(true)
    expect(container.querySelector('main')?.contains(switches[1]!)).toBe(true)
  })

  it('has no switch on the immersive player page', () => {
    render(
      <AuthContext.Provider value={auth}>
        <AppShell route={{ name: 'player' }} activeDownloads={0}>
          <div />
        </AppShell>
      </AuthContext.Provider>,
    )
    expect(screen.queryByRole('navigation', { name: 'Bereich' })).toBeNull()
  })
})
