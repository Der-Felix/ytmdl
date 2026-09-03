import { describe, expect, test, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { Users } from '@/pages/Users'
import { AuthProvider } from '@/hooks/useAuth'

// Mock auth and users apis
mock.module('@/lib/api/auth', () => ({
  getAuthStatus: async () => ({
    setup_required: false,
    authenticated: true,
    user: {
      id: 'admin_1',
      username: 'sysadmin',
      display_name: 'System Admin',
      role: 'admin',
      enabled: true,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    },
  }),
  setupAdmin: async () => {},
  login: async () => {},
  logout: async () => {},
  getMe: async () => {},
}))

mock.module('@/lib/api/users', () => ({
  listUsers: async () => ({
    items: [
      {
        id: 'admin_1',
        username: 'sysadmin',
        display_name: 'System Admin',
        role: 'admin',
        enabled: true,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        last_login_at: new Date().toISOString(),
      },
      {
        id: 'user_2',
        username: 'bob',
        display_name: 'Bob Marley',
        role: 'user',
        enabled: true,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
        last_login_at: null,
      },
    ],
    meta: { count: 2 },
  }),
  createUser: async () => {},
  getUser: async () => {},
  updateUser: async () => {},
  resetPassword: async () => {},
  deleteUser: async () => {},
}))

describe('Users Page', () => {
  test('renders users list and create button for admin', async () => {
    render(
      <AuthProvider>
        <Users />
      </AuthProvider>,
    )

    const heading = await screen.findByText(/Benutzerverwaltung/i)
    expect(heading).toBeDefined()
    expect(await screen.findByText(/Neuer Benutzer/i)).toBeDefined()
    expect(await screen.findByText(/@sysadmin/i)).toBeDefined()
    expect(await screen.findByText(/@bob/i)).toBeDefined()
  })
})
