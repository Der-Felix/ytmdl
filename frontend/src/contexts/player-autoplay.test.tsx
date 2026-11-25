import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { useContext } from 'react'

import { Toaster } from '@/components/ui/toast'
import { AuthContext, type AuthContextValue } from '@/contexts/auth-context'
import { AudioEngine, type AudioEngineCallbacks } from '@/lib/audio/engine'
import type { LibraryTrack } from '@/types/api'
import {
  PlayerActionsContext,
  PlayerProvider,
  PlayerStateContext,
  type PlayerActionsContextValue,
  type PlayerStateContextValue,
} from './PlayerContext'

/*
 * Automatic queue changes (autoplay appending similar tracks, a radio started
 * from the track that plays) must never be reported as a skip of a track the
 * user did not leave. A manual "next" in the same setup is, which shows the
 * harness would see one.
 */

const track = (id: string): LibraryTrack => ({
  id, title: id, artists: ['A'], album: 'Album', album_artist: 'A', release_id: 'rel', track_number: 1,
  track_total: 2, disc_number: 1, disc_total: 1, duration_ms: 180000, year: 2026, source_provider: '',
  source_id: '', source_url: '', created_at: '',
})
const album = [track('a1'), track('a2')]
const radio = [track('r1'), track('r2'), track('a2')]

const auth = {
  user: { id: 'listener', username: 'listener', role: 'user', enabled: true },
  role: 'user',
  isAdmin: false,
  setupRequired: false,
  loading: false,
  error: null,
} as unknown as AuthContextValue

describe('automatic queue transitions', () => {
  let actions: PlayerActionsContextValue
  let state: PlayerStateContextValue
  let requests: string[]
  let loads: string[]
  let clock = 0
  const realNow = performance.now.bind(performance)
  const originalFetch = globalThis.fetch
  const callbacks = () => (AudioEngine.getInstance() as unknown as { callbacks: AudioEngineCallbacks }).callbacks

  beforeEach(() => {
    localStorage.clear()
    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    const engine = AudioEngine.getInstance()
    loads = []
    engine.loadAndPlay = async (url) => { loads.push(url) }
    engine.load = (url) => { loads.push(url) }
    engine.preloadNext = () => {}
    engine.pause = () => {}
    engine.play = () => {}
    engine.seek = () => {}
    clock = 1000
    performance.now = () => clock
    requests = []
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      requests.push(`${init?.method ?? 'GET'} ${url}`)
      if (url.includes('/library/radio')) return new Response(JSON.stringify({ data: radio }))
      return new Response(null, { status: 204 })
    }) as typeof fetch
    function Consumer() {
      actions = useContext(PlayerActionsContext)!
      state = useContext(PlayerStateContext)!
      return null
    }
    render(
      <AuthContext.Provider value={auth}>
        <PlayerProvider>
          <Consumer />
          <Toaster />
        </PlayerProvider>
      </AuthContext.Provider>,
    )
  })

  afterEach(() => {
    performance.now = realNow
    globalThis.fetch = originalFetch
    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    localStorage.clear()
  })

  /** Plays the first album track for a few seconds: long enough to be a skip if left. */
  function listen() {
    act(() => actions.playAlbum(album, 0, { kind: 'album', id: 'rel', title: 'Album' }))
    act(() => callbacks().onStatusChange?.('playing'))
    for (let i = 0; i < 5; i++) {
      clock += 1000
      act(() => callbacks().onTimeUpdate?.(i + 1, 180))
    }
  }
  const skips = () => requests.filter((r) => r.startsWith('POST') && r.includes('/history/skips'))

  it('appends autoplay tracks near the end without reporting a skip', async () => {
    listen()
    await waitFor(() => expect(state.queue.some((t) => t.queueOrigin === 'autoplay')).toBe(true))
    expect(state.queue.map((t) => t.id)).toEqual(['a1', 'a2', 'r1', 'r2'])
    expect(requests.filter((r) => r.includes('/library/radio'))).toHaveLength(1)
    expect(requests.find((r) => r.includes('/library/radio'))).toContain('seed=a2')
    expect(state.currentTrack?.id).toBe('a1')
    expect(skips()).toEqual([])
    // A second look at the same queue end does not ask again.
    act(() => callbacks().onStatusChange?.('playing'))
    expect(requests.filter((r) => r.includes('/library/radio'))).toHaveLength(1)
  })

  it('starts a radio from the playing track without a skip or a reload', async () => {
    listen()
    act(() => actions.setAutoplay(false))
    const before = loads.length
    await act(() => actions.startRadio({ seed: state.currentTrack! }))
    expect(state.queue.map((t) => t.id)).toEqual(['a1', 'r1', 'r2', 'a2'])
    expect(state.queueContext?.title).toBe('Radio: a1')
    expect(state.currentTrack?.id).toBe('a1')
    expect(loads.length).toBe(before)
    expect(skips()).toEqual([])
  })

  it('reports a manual next in the same setup as a skip', async () => {
    listen()
    act(() => actions.next(true))
    await waitFor(() => expect(skips()).toHaveLength(1))
  })

  it('does not ask for similar tracks while repeat is on or autoplay is off', async () => {
    act(() => actions.setRepeatMode('queue'))
    listen()
    act(() => actions.setAutoplay(false))
    act(() => actions.setRepeatMode('off'))
    act(() => callbacks().onStatusChange?.('playing'))
    await new Promise((resolve) => setTimeout(resolve, 20))
    expect(requests.filter((r) => r.includes('/library/radio'))).toEqual([])
  })

  it('undoes clearing the queue from the toast', async () => {
    listen()
    act(() => actions.setAutoplay(false))
    const loaded = loads.length
    act(() => actions.clearQueue())
    expect(state.queue).toEqual([])
    fireEvent.click(await screen.findByRole('button', { name: 'Rückgängig' }))
    expect(state.queue.map((t) => t.id)).toEqual(['a1', 'a2'])
    expect(state.queueContext?.title).toBe('Album')
    expect(state.currentTrack?.id).toBe('a1')
    expect(state.status).toBe('buffering')
    expect(loads.length).toBe(loaded + 1)
    expect(skips()).toEqual([])
  })
})
