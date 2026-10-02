import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { act, render } from '@testing-library/react'
import { useContext } from 'react'
import { PlayerActionsContext, PlayerProvider, PlayerStateContext, type PlayerActionsContextValue, type PlayerStateContextValue } from './PlayerContext'
import { AudioEngine, type AudioEngineCallbacks } from '@/lib/audio/engine'
import type { LibraryTrack } from '@/types/api'

const tracks: LibraryTrack[] = [1, 2, 3].map(number => ({
  id: `transition-${number}`, title: `Track ${number}`, artists: [], album: `Album ${number}`,
  album_artist: '', release_id: `album-${number}`, track_number: number, track_total: 3,
  disc_number: 1, disc_total: 1, duration_ms: 180000, year: 2026, source_provider: '',
  source_id: '', source_url: '', created_at: '',
}))

describe('PlayerProvider transitions', () => {
  let engine: AudioEngine
  let actions: PlayerActionsContextValue
  let state: PlayerStateContextValue
  let loads: string[]
  let preloads: Array<string | null>
  let seeks: number[]
  let playCalls: number
  let pauseCalls: number
  const callbacks = () => (engine as unknown as { callbacks: AudioEngineCallbacks }).callbacks

  beforeEach(() => {
    localStorage.clear()
    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    engine = AudioEngine.getInstance()
    loads = []; preloads = []; seeks = []; playCalls = 0; pauseCalls = 0
    engine.loadAndPlay = async url => { loads.push(url) }
    engine.load = url => { loads.push(url) }
    engine.preloadNext = url => { preloads.push(url) }
    engine.pause = () => { pauseCalls++ }
    engine.play = () => { playCalls++ }
    engine.seek = seconds => { seeks.push(seconds) }
    function Consumer() {
      actions = useContext(PlayerActionsContext)!
      state = useContext(PlayerStateContext)!
      return null
    }
    render(<PlayerProvider><Consumer /></PlayerProvider>)
  })
  afterEach(() => {
    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    localStorage.clear()
  })

  it('adopts the crossfade deck without reloading and advances once per real completion', () => {
    act(() => actions.playTrack(tracks[0]!, tracks, 0))
    expect(loads.length).toBe(1)
    act(() => callbacks().onNextDeckCrossfadeStart?.('/api/v1/library/tracks/transition-2/stream'))
    expect(state.currentTrack?.id).toBe('transition-2')
    expect(state.queueIndex).toBe(1)
    expect(state.status).toBe('playing')
    expect(loads.length).toBe(1)
    expect(preloads.at(-1)).toBe('/api/v1/library/tracks/transition-3/stream')
    act(() => callbacks().onTrackEnded?.())
    expect(state.queueIndex).toBe(2)
    expect(loads.length).toBe(2)
    expect(preloads.at(-1)).toBeNull()
  })

  it('removes stale preloads at stop-after, sleep and repeat-one boundaries', () => {
    act(() => actions.playTrack(tracks[0]!, tracks, 0))
    act(() => actions.setStopAfter('track'))
    expect(preloads.at(-1)).toBeNull()
    act(() => actions.setStopAfter('none'))
    act(() => actions.setSleepTimer('end_of_track'))
    expect(preloads.at(-1)).toBeNull()
    act(() => callbacks().onTrackEnded?.())
    expect(state.queueIndex).toBe(0)
    expect(state.status).toBe('paused')
    expect(state.sleepTimer).toBe('off')
    expect(pauseCalls).toBeGreaterThan(1)
    act(() => actions.setRepeatMode('track'))
    expect(preloads.at(-1)).toBeNull()
    act(() => callbacks().onTrackEnded?.())
    expect(seeks).toEqual([0])
    expect(playCalls).toBe(1)
    expect(state.queueIndex).toBe(0)
  })

  it('preserves album bypass when changing volume and restores fading when disabled', () => {
    const album = tracks.map(track => ({ ...track, album: 'Album', release_id: 'album' }))
    act(() => { actions.setCrossfade(4); actions.playTrack(album[0]!, album, 0) })
    expect((engine as unknown as { crossfadeSeconds: number }).crossfadeSeconds).toBe(0)
    act(() => actions.setVolume(0.5))
    expect((engine as unknown as { crossfadeSeconds: number }).crossfadeSeconds).toBe(0)
    act(() => actions.setSmartAlbumTransition(false))
    expect((engine as unknown as { crossfadeSeconds: number }).crossfadeSeconds).toBe(4)
  })

  it('restarts an explicitly selected current track or duplicate queue occurrence', () => {
    const queue = [tracks[0]!, tracks[0]!, tracks[1]!]
    act(() => actions.playTrack(queue[0]!, queue, 0))
    act(() => actions.playQueueIndex(0))
    expect(loads.length).toBe(2)
    act(() => actions.next(true))
    expect(loads.length).toBe(3)
    expect(state.queueIndex).toBe(1)
    act(() => actions.clearUpcomingQueue())
    expect(preloads.at(-1)).toBeNull()
  })

  it('restarts the current song with Previous without leaving playback stuck buffering', () => {
    act(() => actions.playTrack(tracks[0]!, tracks, 0))
    act(() => callbacks().onTimeUpdate?.(5, 180))
    act(() => actions.previous())
    expect(seeks).toEqual([0])
    expect(playCalls).toBe(1)
    expect(loads.length).toBe(1)
    act(() => callbacks().onStatusChange?.('playing'))
    expect(state.status).toBe('playing')
  })

  it('honors a stop boundary that arrives while the incoming play promise settles', () => {
    act(() => actions.playTrack(tracks[0]!, tracks, 0))
    act(() => actions.setStopAfter('track'))
    act(() => callbacks().onNextDeckCrossfadeStart?.('/api/v1/library/tracks/transition-2/stream'))
    expect(state.currentTrack?.id).toBe('transition-1')
    expect(state.queueIndex).toBe(0)
    expect(state.status).toBe('paused')
    expect(loads.at(-1)).toBe('/api/v1/library/tracks/transition-1/stream')
  })
})
