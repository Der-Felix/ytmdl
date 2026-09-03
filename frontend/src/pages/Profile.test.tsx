import { describe, expect, test, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { Profile } from '@/pages/Profile'
import { AuthProvider } from '@/hooks/useAuth'

// Mock auth and profile apis
mock.module('@/lib/api/auth', () => ({
  getAuthStatus: async () => ({
    setup_required: false,
    authenticated: true,
    user: {
      id: 'user_1',
      username: 'alice',
      display_name: 'Alice Wonderland',
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

mock.module('@/lib/api/profile', () => ({
  getProfile: async () => ({
    id: 'user_1',
    username: 'alice',
    display_name: 'Alice Wonderland',
    role: 'admin',
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }),
  updateProfile: async () => ({
    id: 'user_1',
    username: 'alice',
    display_name: 'Alice W.',
    role: 'admin',
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }),
  changePassword: async () => {},
  listSessions: async () => [
    {
      id: 'sess_1',
      user_agent: 'Mozilla/5.0 (Macintosh)',
      ip_address: '127.0.0.1',
      created_at: new Date().toISOString(),
      expires_at: new Date(Date.now() + 86400000).toISOString(),
      last_seen_at: new Date().toISOString(),
      is_current: true,
    },
  ],
  revokeSession: async () => {},
  revokeOtherSessions: async () => {},
}))

describe('Profile Page', () => {
  test('renders user profile info and active sessions', async () => {
    render(
      <AuthProvider>
        <Profile />
      </AuthProvider>,
    )

    const heading = await screen.findByText(/Profil & Sicherheit/i)
    expect(heading).toBeDefined()
    expect(await screen.findByText(/Passwort ändern/i)).toBeDefined()
    expect(await screen.findByText('Aktive Sitzungen')).toBeDefined()
    expect(await screen.findByText(/Mozilla\/5.0/i)).toBeDefined()
  })
})
