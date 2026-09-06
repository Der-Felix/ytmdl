import { describe, expect, it, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { AppShell } from './AppShell'
import type { Route } from '@/lib/router'

// Mock usePlayer and useJobs hooks for AppShell
mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({
    currentTrack: null,
  }),
}))

mock.module('@/hooks/useJobs', () => ({
  useConnectionState: () => 'open',
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
    const route: Route = { name: 'dashboard' }

    render(
      <AuthContext.Provider value={standardUserAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div>Dashboard</div>
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
    const route: Route = { name: 'dashboard' }

    render(
      <AuthContext.Provider value={adminAuth}>
        <AppShell route={route} activeDownloads={0}>
          <div>Dashboard</div>
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
})
