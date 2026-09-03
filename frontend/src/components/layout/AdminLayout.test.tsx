import { describe, expect, it } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { AdminLayout } from './AdminLayout'
import type { Route } from '@/lib/router'

describe('AdminLayout', () => {
  const dummyAuth = {
    user: {
      id: 'usr-1',
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

  const usersRoute: Route = { name: 'users', pathname: '/users' }

  it('renders top admin header and horizontal navigation tabs', () => {
    render(
      <AuthContext.Provider value={dummyAuth}>
        <AdminLayout route={usersRoute}>
          <div data-testid="child-content">Benutzer Inhalt</div>
        </AdminLayout>
      </AuthContext.Provider>,
    )

    expect(screen.getByText('Zurück zu YTMDL')).toBeDefined()
    expect(screen.getByText('Verwaltung')).toBeDefined()
    expect(screen.getByText('Benutzerverwaltung')).toBeDefined()
    expect(screen.getByText('Allgemein & Diagnose')).toBeDefined()
    expect(screen.getByText('Downloads & Automation')).toBeDefined()
    expect(screen.getByText('Speicher & Storage')).toBeDefined()
    expect(screen.getByText('Provider')).toBeDefined()
    expect(screen.getByTestId('child-content').textContent).toBe('Benutzer Inhalt')
  })
})
