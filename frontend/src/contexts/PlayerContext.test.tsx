import { describe, expect, it, mock } from 'bun:test'
import { act, render } from '@testing-library/react'

import { AuthContext } from '@/contexts/auth-context'

// Mock AudioEngine to avoid unhandled HTMLAudioElement errors in test DOM
mock.module('@/lib/audio/engine', () => ({
  AudioEngine: {
    getInstance: () => ({
      play: () => {},
      pause: () => {},
      seek: () => {},
      load: () => {},
      loadAndPlay: () => {},
      preloadNext: () => {},
      setCallbacks: () => {},
      setVolume: () => {},
      setMuted: () => {},
      setPlaybackRate: () => {},
      setCrossfade: () => {},
      setEQEnabled: () => {},
      setEQMode: () => {},
      setGraphicBands: () => {},
      setParametricFilters: () => {},
      setPreamp: () => {},
      setAutoHeadroom: () => {},
      setLimiter: () => {},
      setBalance: () => {},
      setMono: () => {},
    }),
  },
}))

import { PlayerProvider } from './PlayerContext'

const dummyAuth = {
  user: null,
  loading: false,
  setupRequired: false,
  isAdmin: false,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
  checkSetup: async () => {},
}

describe('PlayerContext keyboard shortcuts', () => {
  it('does not prevent default for browser reload and system shortcuts (Cmd+R, Cmd+Shift+R, Ctrl+R, etc.)', () => {
    render(
      <AuthContext.Provider value={dummyAuth}>
        <PlayerProvider>
          <div data-testid="test-child">Child</div>
        </PlayerProvider>
      </AuthContext.Provider>,
    )

    // 1. macOS Cmd+R (Reload)
    const cmdR = new KeyboardEvent('keydown', { key: 'r', metaKey: true, cancelable: true })
    window.dispatchEvent(cmdR)
    expect(cmdR.defaultPrevented).toBe(false)

    // 2. macOS Cmd+Shift+R (Hard Reload)
    const cmdShiftR = new KeyboardEvent('keydown', { key: 'R', metaKey: true, shiftKey: true, cancelable: true })
    window.dispatchEvent(cmdShiftR)
    expect(cmdShiftR.defaultPrevented).toBe(false)

    // 3. Linux/Windows Ctrl+R (Reload)
    const ctrlR = new KeyboardEvent('keydown', { key: 'r', ctrlKey: true, cancelable: true })
    window.dispatchEvent(ctrlR)
    expect(ctrlR.defaultPrevented).toBe(false)

    // 4. Linux/Windows Ctrl+Shift+R (Hard Reload)
    const ctrlShiftR = new KeyboardEvent('keydown', { key: 'R', ctrlKey: true, shiftKey: true, cancelable: true })
    window.dispatchEvent(ctrlShiftR)
    expect(ctrlShiftR.defaultPrevented).toBe(false)

    // 5. Alt+R / Other modifier combinations
    const altR = new KeyboardEvent('keydown', { key: 'r', altKey: true, cancelable: true })
    window.dispatchEvent(altR)
    expect(altR.defaultPrevented).toBe(false)

    // 6. Cmd+Space (System shortcut)
    const cmdSpace = new KeyboardEvent('keydown', { key: ' ', metaKey: true, cancelable: true })
    window.dispatchEvent(cmdSpace)
    expect(cmdSpace.defaultPrevented).toBe(false)

    // 7. Plain 'r' (Application player repeat mode toggle)
    const plainR = new KeyboardEvent('keydown', { key: 'r', cancelable: true })
    act(() => {
      window.dispatchEvent(plainR)
    })
    expect(plainR.defaultPrevented).toBe(true)

    // 8. Plain ' ' (Application play/pause toggle)
    const plainSpace = new KeyboardEvent('keydown', { key: ' ', cancelable: true })
    act(() => {
      window.dispatchEvent(plainSpace)
    })
    expect(plainSpace.defaultPrevented).toBe(true)
  })
})
