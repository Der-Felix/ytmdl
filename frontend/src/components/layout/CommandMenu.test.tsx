import { beforeEach, describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'
import { CommandMenu } from './CommandMenu'

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ currentTrack: null }),
  usePlayerState: () => ({ currentTrack: null, status: 'idle', shuffle: false, repeatMode: 'off' }),
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
  window.history.replaceState(null, '', '/dashboard')
})

describe('CommandMenu ranking', () => {
  it('offers no search entries while the field is empty', () => {
    renderMenu()
    expect(screen.queryByText(/In der Bibliothek suchen/)).toBeNull()
    expect(screen.getByText('Ansichten')).toBeDefined()
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
})
