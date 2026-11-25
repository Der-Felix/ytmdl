import { describe, expect, it, mock } from 'bun:test'
import { fireEvent, render, screen } from '@testing-library/react'

import { MiniPlayer } from './MiniPlayer'
import type { LibraryTrack } from '@/types/api'

const track: LibraryTrack = {
  id: 'trk-1',
  title: 'Ein sehr langer Titel, der auf dem Telefon gekürzt wird',
  artists: ['Künstlerin'],
  album: 'Album',
  album_artist: 'Künstlerin',
  release_id: 'rel-1',
  track_number: 1,
  duration_ms: 180000,
  source_provider: '',
  source_id: '',
  source_url: '',
  created_at: '',
}

const actions = {
  togglePlayPause: mock(() => {}),
  previous: mock(() => {}),
  next: mock(() => {}),
  seek: mock(() => {}),
  setVolume: mock(() => {}),
  toggleMute: mock(() => {}),
  toggleShuffle: mock(() => {}),
  cycleRepeatMode: mock(() => {}),
}

const state = {
  currentTrack: track,
  queue: [track],
  queueIndex: 0,
  status: 'playing',
  volume: 0.8,
  muted: false,
  shuffle: false,
  repeatMode: 'off',
}

mock.module('@/hooks/usePlayer', () => ({
  usePlayer: () => ({ ...state, ...actions }),
  usePlayerState: () => state,
  usePlayerActions: () => actions,
  usePlayerProgress: () => ({ currentTime: 30, duration: 180 }),
}))

/** The class list as tokens, so "hidden" is not found inside "sm:hidden". */
function classes(element: Element): string[] {
  return element.className.toString().split(/\s+/)
}

describe('MiniPlayer on a phone', () => {
  it('always shows previous, play/pause and next at 44px', () => {
    render(<MiniPlayer />)
    for (const name of ['Vorheriger Titel', 'Pause', 'Nächster Titel', 'Wiedergabeliste / Queue öffnen']) {
      const button = screen.getByRole('button', { name })
      expect({ name, hidden: classes(button).includes('hidden') }).toEqual({ name, hidden: false })
      expect({ name, size: classes(button).includes('size-11') }).toEqual({ name, size: true })
    }
  })

  it('runs the actions from the bar', () => {
    render(<MiniPlayer />)
    fireEvent.click(screen.getByRole('button', { name: 'Vorheriger Titel' }))
    fireEvent.click(screen.getByRole('button', { name: 'Pause' }))
    fireEvent.click(screen.getByRole('button', { name: 'Nächster Titel' }))
    expect(actions.previous).toHaveBeenCalledTimes(1)
    expect(actions.togglePlayPause).toHaveBeenCalledTimes(1)
    expect(actions.next).toHaveBeenCalledWith(true)
  })

  it('opens the full player from the cover and from the title', () => {
    const { container } = render(<MiniPlayer />)
    const links = [...container.querySelectorAll('a[href="/player"]')]
    // Cover, title, and the expand button for wider screens.
    expect(links.length).toBeGreaterThanOrEqual(2)
    expect(links.some((link) => link.querySelector('img'))).toBe(true)
    expect(links.some((link) => link.textContent?.includes(track.title))).toBe(true)
  })

  it('offers the volume slider to precise pointers only', () => {
    render(<MiniPlayer />)
    const control = screen.getByRole('slider', { name: 'Lautstärkeregler' }).closest('[data-slot="volume-control"]')!
    // Hidden by default; shown only from lg up *and* with a fine pointer, so
    // a touch screen (pointer: coarse) never gets it, however wide.
    expect(classes(control)).toContain('hidden')
    expect(classes(control)).toContain('lg:pointer-fine:flex')
    expect(classes(control).filter((name) => /(^|:)(flex|block|inline-flex)$/.test(name))).toEqual([
      'lg:pointer-fine:flex',
    ])
  })

  it('docks on the bottom navigation and leaves the safe area to it', () => {
    const { container } = render(<MiniPlayer />)
    const bar = container.querySelector('[data-slot="mini-player"]')!
    expect(classes(bar)).toContain('bottom-(--bottom-nav-height)')
    expect(classes(bar)).toContain('pb-(--player-inset)')
  })
})
