import { describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, within } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { AppShell } from './AppShell'
import { BottomNav } from './BottomNav'
import type { Route } from '@/lib/router'

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: null }),
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({}),
}))

const auth = {
  user: {
    id: 'usr-member',
    username: 'member',
    display_name: 'Member',
    role: 'user' as const,
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    last_login_at: null,
  },
  loading: false,
  setupRequired: false,
  isAdmin: false,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
  checkSetup: async () => {},
}

function renderNav(route: Route, area: 'listen' | 'download', activeDownloads = 0, onOpen = () => {}) {
  render(<BottomNav route={route} area={area} activeDownloads={activeDownloads} onOpenCommandMenu={onOpen} />)
  return screen.getByRole('navigation', { name: 'Schnellzugriff' })
}

function entries(nav: HTMLElement) {
  return within(nav)
    .getAllByRole('listitem')
    .map((item) => {
      const control = item.querySelector('a, button')!
      return {
        label: control.textContent ?? '',
        href: control.getAttribute('href'),
        current: control.getAttribute('aria-current'),
      }
    })
}

describe('BottomNav', () => {
  it('lists the listening destinations and marks the current page', () => {
    const nav = renderNav({ name: 'library' }, 'listen')
    expect(entries(nav)).toEqual([
      { label: 'Start', href: '/listen', current: null },
      { label: 'Für dich', href: '/mixes', current: null },
      { label: 'Bibliothek', href: '/library', current: 'page' },
      { label: 'Playlists', href: '/playlists', current: null },
      { label: 'Suche', href: null, current: null },
    ])
  })

  it('marks a detail page under its section', () => {
    const nav = renderNav({ name: 'playlistDetail', id: 'pl-1' } as Route, 'listen')
    expect(within(nav).getByRole('link', { name: 'Playlists' }).getAttribute('aria-current')).toBe('page')
  })

  it('lists the loading destinations with the open-jobs count on Downloads', () => {
    const nav = renderNav({ name: 'downloadHome' }, 'download', 3)
    expect(entries(nav).map(({ label, href, current }) => ({ label, href, current }))).toEqual([
      { label: 'Suche', href: '/search', current: 'page' },
      { label: 'Entdecken', href: '/discover', current: null },
      // The badge on the icon, then the label and the spoken count.
      { label: '3Downloads, 3 offene Downloads', href: '/downloads', current: null },
      { label: 'Abonnements', href: '/subscriptions', current: null },
    ])
    expect(within(nav).getByRole('link', { name: /Downloads.*3 offene Downloads/ })).toBeDefined()
  })

  it('leaves the count out while nothing is queued', () => {
    const nav = renderNav({ name: 'downloads' }, 'download', 0)
    expect(within(nav).getByRole('link', { name: 'Downloads' }).getAttribute('aria-current')).toBe('page')
  })

  it('is a phone-only bar with 48px targets and the bottom safe area', () => {
    const nav = renderNav({ name: 'listenHome' }, 'listen')
    expect(nav.className).toContain('md:hidden')
    expect(nav.className).toContain('pb-[env(safe-area-inset-bottom)]')
    // h-14 is 56px; every entry fills it.
    expect(nav.querySelector('ul')?.className).toContain('h-14')
    for (const control of nav.querySelectorAll('a, button')) {
      expect(control.className).toContain('h-full')
    }
  })

  it('opens the command menu from the search entry', () => {
    const onOpen = mock(() => {})
    const nav = renderNav({ name: 'listenHome' }, 'listen', 0, onOpen)
    fireEvent.click(within(nav).getByRole('button', { name: 'Suche' }))
    expect(onOpen).toHaveBeenCalledTimes(1)
  })
})

describe('BottomNav in the shell', () => {
  function renderShell(route: Route) {
    return render(
      <AuthContext.Provider value={auth}>
        <AppShell route={route} activeDownloads={0}>
          <div />
        </AppShell>
      </AuthContext.Provider>,
    )
  }

  it('opens the command menu from the search entry', async () => {
    renderShell({ name: 'listenHome' })
    const nav = screen.getByRole('navigation', { name: 'Schnellzugriff' })
    fireEvent.click(within(nav).getByRole('button', { name: 'Suche' }))
    expect(await screen.findByRole('dialog')).toBeDefined()
  })

  it('is not on the player page', () => {
    renderShell({ name: 'player' })
    expect(screen.queryByRole('navigation', { name: 'Schnellzugriff' })).toBeNull()
  })

  it('pads the page by the bottom bars on phones and keeps the desktop padding', () => {
    const { container } = renderShell({ name: 'library' })
    const main = container.querySelector('main')!
    expect(main.className).toContain('pb-[calc(var(--bottom-chrome)+2rem)]')
    expect(main.className).toContain('md:pb-8')
  })
})
