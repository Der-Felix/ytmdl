import { describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { AppShell } from './AppShell'

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: null }),
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({}),
}))

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

function renderShell() {
  window.history.replaceState(null, '', '/listen')
  return render(
    <AuthContext.Provider value={auth}>
      <AppShell route={{ name: 'listenHome' }} activeDownloads={0}>
        <button type="button">Seiteninhalt</button>
      </AppShell>
    </AuthContext.Provider>,
  )
}

async function openDrawer() {
  const trigger = screen.getByRole('button', { name: 'Navigation öffnen' })
  trigger.focus()
  fireEvent.click(trigger)
  const drawer = await screen.findByRole('dialog', { name: 'Navigation' })
  return { trigger, drawer }
}

/**
 * The drawer is a modal Base UI dialog. The tests only check the behaviour the
 * app relies on; the keyboard itself (real Tab handling) is not simulated by
 * the DOM used here, so Tab is modelled by focusing the focus guards.
 */
describe('AppShell mobile navigation drawer', () => {
  it('moves focus into the drawer when it opens', async () => {
    renderShell()
    const { drawer } = await openDrawer()
    await waitFor(() => expect(drawer.contains(document.activeElement)).toBe(true))
  })

  it('keeps focus inside when tabbing past either end', async () => {
    renderShell()
    const { drawer } = await openDrawer()
    await waitFor(() => expect(drawer.contains(document.activeElement)).toBe(true))

    const guards = Array.from(document.querySelectorAll<HTMLElement>('[data-base-ui-focus-guard]'))
    // Tab from the last entry lands on a guard after the popup, Shift+Tab from
    // the first on the one before it; both must send focus back inside.
    expect(guards.length).toBeGreaterThan(0)
    for (const guard of guards) {
      guard.focus()
      await waitFor(() => expect(drawer.contains(document.activeElement)).toBe(true))
    }
  })

  it('makes the page behind the drawer inert for assistive technology', async () => {
    renderShell()
    expect(screen.getByRole('button', { name: 'Seiteninhalt' })).toBeDefined()
    await openDrawer()
    expect(screen.queryByRole('button', { name: 'Seiteninhalt' })).toBeNull()
  })

  it('closes on Escape and returns focus to the opening button', async () => {
    renderShell()
    const { trigger, drawer } = await openDrawer()

    fireEvent.keyDown(drawer, { key: 'Escape' })

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull())
    await waitFor(() => expect(document.activeElement).toBe(trigger))
    expect(trigger.getAttribute('aria-expanded')).toBe('false')
  })

  it('closes when a navigation entry is chosen', async () => {
    renderShell()
    const { drawer } = await openDrawer()

    const link = within(drawer).getByRole('link', { name: /Benutzerverwaltung/i })
    // Stay on the page instead of letting the click navigate the test window.
    link.addEventListener('click', (event) => event.preventDefault())
    fireEvent.click(link)

    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Navigation' })).toBeNull())
  })
})
