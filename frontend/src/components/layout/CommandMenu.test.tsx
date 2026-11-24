import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { CommandMenu } from './CommandMenu'

let loadedTrack: unknown = null

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: loadedTrack, status: "idle", shuffle: true, repeatMode: "off" }),
  usePlayerProgress: () => ({ currentTime: 0, duration: 0 }),
  usePlayerActions: () => ({
    togglePlayPause: () => {},
    next: () => {},
    previous: () => {},
    toggleShuffle: () => {},
    cycleRepeatMode: () => {},
  }),
}))

const auth = {
  user: null,
  loading: false,
  setupRequired: false,
  isAdmin: false,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
  checkSetup: async () => {},
}

function renderMenu() {
  return render(
    <AuthContext.Provider value={auth}>
      <CommandMenu open onOpenChange={() => {}} />
    </AuthContext.Provider>,
  )
}

function type(text: string) {
  const input = screen.getByRole('combobox')
  fireEvent.change(input, { target: { value: text } })
  return input
}

beforeEach(() => {
  loadedTrack = null
  window.history.replaceState(null, '', '/listen')
})

describe('CommandMenu ranking', () => {
  it('offers no search entries while the field is empty', () => {
    renderMenu()
    expect(screen.queryByText(/In der Bibliothek suchen/)).toBeNull()
    expect(screen.getByText('Hören')).toBeDefined()
  })

  it('offers every destination of both areas, grouped by area', () => {
    renderMenu()
    const groupOf = (label: string) => screen.getByRole('option', { name: label }).closest('[cmdk-group]')?.querySelector('[cmdk-group-heading]')?.textContent
    for (const label of ['Übersicht', 'Für dich', 'Player', 'Bibliothek', 'Playlists', 'Favoriten', 'Offline-Musik']) {
      expect(groupOf(label)).toBe('Hören')
    }
    for (const label of ['Suche', 'Entdecken', 'Downloads', 'Abonnements']) {
      expect(groupOf(label)).toBe('Laden')
    }
  })

  it('opens the matching view on Enter instead of searching the library', async () => {
    renderMenu()
    const input = type('bib')
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole('option')
    expect(options[0]?.textContent).toBe('Bibliothek')
    expect(options[0]?.getAttribute('aria-selected')).toBe('true')
    expect(options.at(-1)?.textContent).toContain('Im Katalog entdecken')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.pathname).toBe('/library')
    expect(window.location.search).toBe('')
  })

  it('selects the library search when nothing else matches', async () => {
    renderMenu()
    const input = type('qqxzv')
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole('option')
    expect(options).toHaveLength(2)
    expect(options[0]?.getAttribute('aria-selected')).toBe('true')

    fireEvent.keyDown(input, { key: 'Enter' })
    expect(window.location.pathname).toBe('/library')
    expect(window.location.search).toContain('q=qqxzv')
  })

  it("does not run a player action whose letters merely contain the query in order", async () => {
    loadedTrack = { id: "t1", title: "Song" }
    renderMenu()
    const input = type("dash")
    await waitFor(() => expect(screen.getByText(/In der Bibliothek suchen/)).toBeDefined())
    const options = screen.getAllByRole("option")
    // "Suche" is the former dashboard and still found by that name.
    expect(options[0]?.textContent).toBe("Suche")
    expect(options[0]?.getAttribute("aria-selected")).toBe("true")
    expect(screen.queryByText(/Zufallswiedergabe/)).toBeNull()

    fireEvent.keyDown(input, { key: "Enter" })
    expect(window.location.pathname).toBe("/search")
  })
})
