// A test fixture: it is run, never hot-reloaded, so the fast-refresh rule does not apply.
/* eslint-disable react/only-export-components */
import { afterEach, beforeEach, describe, expect, it, mock } from 'bun:test'
import { act, fireEvent, render, screen } from '@testing-library/react'
import { useContext } from 'react'

import * as CoverModule from '@/components/music/Cover'
import { AudioEngine } from '@/lib/audio/engine'
import type { LibraryTrack } from '@/types/api'

/**
 * Render counts while a track plays. Every artwork in the mini player, the
 * queue and the player page is a Cover, so counting Cover renders counts how
 * often those views are redrawn. The wrapper still renders the real Cover.
 *
 * Measured on 1.4.0 before the progress was isolated: ten progress ticks
 * redrew the mini player with its open queue 40 times (one artwork in the bar
 * plus three in the queue, every tick) and the player page 40 times (artwork
 * plus three queue rows). After: none; only the time and the seek bar update.
 */
const RealCover = CoverModule.Cover
let coverRenders = 0
function CountingCover(props: Parameters<typeof RealCover>[0]) {
  coverRenders++
  return <RealCover {...props} />
}
mock.module('@/components/music/Cover', () => ({ ...CoverModule, Cover: CountingCover }))

const { PlayerActionsContext, PlayerProvider } = await import('@/contexts/PlayerContext')
const { MiniPlayer } = await import('@/components/player/MiniPlayer')
const { NowPlaying } = await import('@/pages/NowPlaying')

const track = (n: number): LibraryTrack => ({
  id: `trk-${n}`,
  title: `Titel ${n}`,
  artists: ['Künstler'],
  album: 'Album',
  album_artist: 'Künstler',
  release_id: 'rel-1',
  track_number: n,
  duration_ms: 180000,
  source_provider: '',
  source_id: '',
  source_url: '',
  created_at: '',
})
const tracks = [track(1), track(2), track(3)]

const engine = AudioEngine.getInstance()
const originalLoadAndPlay = engine.loadAndPlay
const originalLoad = engine.load
const originalFetch = globalThis.fetch

beforeEach(() => {
  engine.loadAndPlay = async () => {}
  engine.load = () => {}
  // Lyrics are not part of this measurement; the page asks for them once.
  globalThis.fetch = (async () =>
    new Response(JSON.stringify({ error: { code: 'NOT_FOUND', message: 'none' } }), { status: 404 })) as unknown as typeof fetch
})

afterEach(() => {
  engine.loadAndPlay = originalLoadAndPlay
  engine.load = originalLoad
  globalThis.fetch = originalFetch
  localStorage.clear()
})

function StartAlbum() {
  const actions = useContext(PlayerActionsContext)
  return <button onClick={() => actions?.playAlbum(tracks, 0)}>start</button>
}

function tick(seconds: number) {
  const callbacks = (engine as unknown as { callbacks: { onTimeUpdate: (c: number, d: number) => void } }).callbacks
  act(() => callbacks.onTimeUpdate(seconds, 180))
}

describe('rendering while a track plays', () => {
  it('does not redraw the mini player or its open queue on progress ticks', async () => {
    render(
      <PlayerProvider>
        <StartAlbum />
        <MiniPlayer />
      </PlayerProvider>,
    )
    act(() => fireEvent.click(screen.getByText('start')))
    act(() => fireEvent.click(screen.getByRole('button', { name: 'Wiedergabeliste / Queue öffnen' })))
    await screen.findByText('Warteschlange')

    coverRenders = 0
    for (let i = 1; i <= 10; i++) tick(i)

    expect(coverRenders).toBe(0)
    // The progress display itself still follows the position.
    expect(screen.getAllByText('0:10').length).toBeGreaterThan(0)
  })

  it('does not redraw the player page on progress ticks', async () => {
    render(
      <PlayerProvider>
        <StartAlbum />
        <NowPlaying />
      </PlayerProvider>,
    )
    act(() => fireEvent.click(screen.getByText('start')))
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0))
    })
    // The queue tab shows every queued track with its artwork.
    act(() => fireEvent.click(screen.getAllByRole('button', { name: /^Queue/ })[0]!))
    expect(screen.getByText('Titel 3')).toBeTruthy()

    coverRenders = 0
    for (let i = 1; i <= 10; i++) tick(i)

    expect(coverRenders).toBe(0)
    expect(screen.getAllByText('0:10').length).toBeGreaterThan(0)
  })

  it('redraws the player page once per lyric line, not once per tick', async () => {
    globalThis.fetch = (async () =>
      new Response(
        JSON.stringify({
          data: { track_id: 'trk-1', state: 'found', synced: true, content: '[00:00.00]Erste Zeile\n[00:05.00]Zweite Zeile' },
        }),
      )) as unknown as typeof fetch
    render(
      <PlayerProvider>
        <StartAlbum />
        <NowPlaying />
      </PlayerProvider>,
    )
    act(() => fireEvent.click(screen.getByText('start')))
    await screen.findByText('Zweite Zeile')

    coverRenders = 0
    for (let i = 1; i <= 10; i++) tick(i)

    // The page shows one artwork; it is redrawn once, when line two starts.
    expect(coverRenders).toBe(1)
  })
})
