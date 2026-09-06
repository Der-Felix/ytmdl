import { describe, expect, test, mock } from 'bun:test'
import { render, screen } from '@testing-library/react'

import { Login } from '@/pages/Login'
import { AuthProvider } from '@/hooks/useAuth'

// Mock getAuthStatus
mock.module('@/lib/api/auth', () => ({
  getAuthStatus: async () => ({
    setup_required: true,
    authenticated: false,
    user: null,
  }),
  setupAdmin: async () => ({
    id: 'user_1',
    username: 'admin',
    display_name: 'Admin User',
    role: 'admin',
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }),
  login: async () => ({
    id: 'user_1',
    username: 'admin',
    display_name: 'Admin User',
    role: 'admin',
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }),
  logout: async () => {},
  getMe: async () => ({
    id: 'user_1',
    username: 'admin',
    display_name: 'Admin User',
    role: 'admin',
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }),
}))

describe('Login Page', () => {
  test('renders setup form when setup is required', async () => {
    render(
      <AuthProvider>
        <Login />
      </AuthProvider>,
    )

    // Initially or after loading, finds Ersteinrichtung
    const heading = await screen.findByText(/YTMDL Ersteinrichtung/i)
    expect(heading).toBeDefined()
    expect(screen.getByText(/Administrator erstellen/i)).toBeDefined()

    // Brand logo should be rendered
    const logo = screen.getByTestId('brand-logo')
    expect(logo).toBeDefined()
    expect(logo.getAttribute('src')).toBe('/logo-mark.png')
  })
})
