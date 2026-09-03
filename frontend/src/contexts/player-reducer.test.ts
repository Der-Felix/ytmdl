import { describe, expect, it } from 'bun:test'
import type { LibraryTrack } from '@/types/api'
import {
  INITIAL_PLAYER_STATE,
  playerReducer,
} from './player-reducer'

const dummyTrack1: LibraryTrack = {
  id: 'track-1',
  title: 'Track One',
  artists: ['Artist A'],
  album: 'Album 1',
  album_artist: 'Artist A',
  release_id: 'rel-1',
  track_number: 1,
  track_total: 10,
  disc_number: 1,
  disc_total: 1,
  duration_ms: 180000,
  year: 2024,
  source_provider: 'youtube',
  source_id: 'src-1',
  source_url: 'https://...',
  created_at: new Date().toISOString(),
}

const dummyTrack2: LibraryTrack = {
  ...dummyTrack1,
  id: 'track-2',
  title: 'Track Two',
  track_number: 2,
}

const dummyTrack3: LibraryTrack = {
  ...dummyTrack1,
  id: 'track-3',
  title: 'Track Three',
  release_id: 'rel-2',
  album: 'Album 2',
  track_number: 1,
}

describe('playerReducer', () => {
  it('handles PLAY_TRACK with single track and custom queue', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    expect(state.currentTrack?.id).toBe('track-2')
    expect(state.queueIndex).toBe(1)
    expect(state.queue.length).toBe(3)
    expect(state.status).toBe('buffering')
    expect(state.currentTime).toBe(0)
    expect(state.duration).toBe(180)
    expect(state.history.length).toBe(1)
  })

  it('handles PLAY_ALBUM starting from specific index', () => {
    const tracks = [dummyTrack1, dummyTrack2, dummyTrack3]
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_ALBUM',
      payload: { tracks, startIndex: 2 },
    })

    expect(state.currentTrack?.id).toBe('track-3')
    expect(state.queueIndex).toBe(2)
    expect(state.queue.length).toBe(3)
    expect(state.shuffle).toBe(false)
  })

  it('handles PLAY_ARTIST with shuffle enabled', () => {
    const tracks = [dummyTrack1, dummyTrack2, dummyTrack3]
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_ARTIST',
      payload: { tracks, shuffle: true },
    })

    expect(state.queue.length).toBe(3)
    expect(state.originalQueue).toEqual(tracks)
    expect(state.shuffle).toBe(true)
    expect(state.queueIndex).toBe(0)
  })

  it('handles PLAY_NEXT inserting track directly after current track', () => {
    const queue = [dummyTrack1, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, {
      type: 'PLAY_NEXT',
      payload: { track: dummyTrack2 },
    })

    expect(state.queue.length).toBe(3)
    expect(state.queue[1].id).toBe('track-2')
  })

  it('handles ADD_TO_QUEUE adding to end of queue', () => {
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1 },
    })

    state = playerReducer(state, {
      type: 'ADD_TO_QUEUE',
      payload: { tracks: [dummyTrack2, dummyTrack3] },
    })

    expect(state.queue.length).toBe(3)
    expect(state.queue[2].id).toBe('track-3')
  })

  it('handles REMOVE_FROM_QUEUE and adjusts queueIndex properly', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    // Remove index 0 (before current)
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 0 },
    })

    expect(state.queue.length).toBe(2)
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-2')
  })

  it('handles REORDER_QUEUE', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, {
      type: 'REORDER_QUEUE',
      payload: { fromIndex: 0, toIndex: 2 },
    })

    expect(state.queue[2].id).toBe('track-1')
    expect(state.queueIndex).toBe(2)
  })

  it('handles SET_SHUFFLE on and off without losing original queue order', () => {
    const original = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: original, queueIndex: 0 },
    })

    // Turn ON
    state = playerReducer(state, {
      type: 'SET_SHUFFLE',
      payload: true,
    })
    expect(state.shuffle).toBe(true)
    expect(state.originalQueue.length).toBe(3)

    // Turn OFF
    state = playerReducer(state, {
      type: 'SET_SHUFFLE',
      payload: false,
    })
    expect(state.shuffle).toBe(false)
    expect(state.queue).toEqual(original)
    expect(state.queueIndex).toBe(0)
  })

  it('handles PREVIOUS: restarts track if currentTime > 3s, otherwise goes to prev', () => {
    const queue = [dummyTrack1, dummyTrack2]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    // 1. Time > 3s: reset to 0
    state = { ...state, currentTime: 15 }
    state = playerReducer(state, { type: 'PREVIOUS' })
    expect(state.currentTime).toBe(0)
    expect(state.queueIndex).toBe(1)

    // 2. Time <= 3s: go to prev track
    state = { ...state, currentTime: 1.5 }
    state = playerReducer(state, { type: 'PREVIOUS' })
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-1')
  })

  it('handles NEXT with repeat modes and stop after triggers', () => {
    const queue = [dummyTrack1, dummyTrack2]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    // 1. End of queue with repeat=off -> pauses
    let nextState = playerReducer(state, { type: 'NEXT', payload: { manual: false } })
    expect(nextState.status).toBe('paused')

    // 2. End of queue with repeat=queue -> wraps to 0
    state = { ...state, repeatMode: 'queue' }
    nextState = playerReducer(state, { type: 'NEXT', payload: { manual: false } })
    expect(nextState.queueIndex).toBe(0)
    expect(nextState.currentTrack?.id).toBe('track-1')

    // 3. Stop after track
    state = { ...state, stopAfter: 'track' }
    nextState = playerReducer(state, { type: 'NEXT', payload: { manual: false } })
    expect(nextState.status).toBe('paused')
    expect(nextState.stopAfter).toBe('none')
  })

  it('handles Sleep Timer options', () => {
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'SET_SLEEP_TIMER',
      payload: '30',
    })
    expect(state.sleepTimer).toBe('30')
    expect(state.sleepTimerEndsAt).toBeGreaterThan(Date.now())
  })

  it('handles EQ presets and custom presets', () => {
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'SET_EQ_PRESET',
      payload: 'bass_boost',
    })
    expect(state.selectedPresetId).toBe('bass_boost')
    expect(state.graphicBands[0]).toBe(5.5)

    // Modify a band -> turns into custom
    state = playerReducer(state, {
      type: 'SET_EQ_BAND',
      payload: { index: 0, gain: 8.0 },
    })
    expect(state.selectedPresetId).toBe('custom')
    expect(state.graphicBands[0]).toBe(8.0)

    // Save as custom preset
    state = playerReducer(state, {
      type: 'SAVE_CUSTOM_PRESET',
      payload: { name: 'My Bass' },
    })
    expect(state.customPresets.length).toBe(1)
    expect(state.customPresets[0].name).toBe('My Bass')
    expect(state.customPresets[0].values[0]).toBe(8.0)
  })

  it('handles RESTORE_STATE safely without autoplay', () => {
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'RESTORE_STATE',
      payload: {
        currentTrack: dummyTrack1,
        queue: [dummyTrack1],
        volume: 0.8,
        status: 'playing', // attempts autoplay
      },
    })
    expect(state.currentTrack?.id).toBe('track-1')
    expect(state.volume).toBe(0.8)
    expect(state.status).toBe('paused') // MUST remain paused
  })
})
