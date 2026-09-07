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

  it('handles PLAY_NEXT no-op when target track is already playing', () => {
    const queue = [dummyTrack1, dummyTrack2]
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    const nextState = playerReducer(state, {
      type: 'PLAY_NEXT',
      payload: { track: dummyTrack1 },
    })

    expect(nextState).toBe(state)
  })

  it('handles PLAY_NEXT repositioning existing track without duplicate', () => {
    // Queue: [T1 (current), T2, T3]. Play Next T3 -> [T1, T3, T2]
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, {
      type: 'PLAY_NEXT',
      payload: { track: dummyTrack3 },
    })

    expect(state.queue.length).toBe(3)
    expect(state.queue[0].id).toBe('track-1')
    expect(state.queue[1].id).toBe('track-3')
    expect(state.queue[2].id).toBe('track-2')
    expect(state.queueIndex).toBe(0)

    // Now current is T3 at idx 1. Play Next T1 (which is before current) -> reposition
    state = playerReducer(state, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack3, queue: state.queue, queueIndex: 1 },
    })
    state = playerReducer(state, {
      type: 'PLAY_NEXT',
      payload: { track: dummyTrack1 },
    })
    expect(state.queue.length).toBe(3)
    // T1 moved after T3: [T3 (idx 0), T1 (idx 1), T2 (idx 2)]
    expect(state.queue[0].id).toBe('track-3')
    expect(state.queue[1].id).toBe('track-1')
    expect(state.queue[2].id).toBe('track-2')
    expect(state.queueIndex).toBe(0)
  })

  it('handles PLAY_QUEUE_INDEX playing track at index', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, {
      type: 'PLAY_QUEUE_INDEX',
      payload: { index: 2 },
    })

    expect(state.currentTrack?.id).toBe('track-3')
    expect(state.queueIndex).toBe(2)
    expect(state.status).toBe('buffering')
    expect(state.currentTime).toBe(0)
    expect(state.history[0].track.id).toBe('track-3')

    // Out of bounds is no-op
    const noopState = playerReducer(state, {
      type: 'PLAY_QUEUE_INDEX',
      payload: { index: 99 },
    })
    expect(noopState).toBe(state)
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

  it('handles REMOVE_FROM_QUEUE before, after, and on active track', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    // 1. Remove index 0 (before current) -> queueIndex shifts from 1 to 0
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 0 },
    })
    expect(state.queue.length).toBe(2)
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-2')

    // 2. Remove index 1 (after current) -> queueIndex stays 0
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 1 },
    })
    expect(state.queue.length).toBe(1)
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-2')

    // 3. Remove index 0 (current, and only remaining track) -> queue becomes empty
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 0 },
    })
    expect(state.queue.length).toBe(0)
    expect(state.queueIndex).toBe(-1)
    expect(state.currentTrack).toBeNull()
    expect(state.status).toBe('idle')
  })

  it('handles REMOVE_FROM_QUEUE on active track when next track exists', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    // Remove active track at index 0 -> advances to dummyTrack2
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 0 },
    })
    expect(state.queue.length).toBe(2)
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-2')
    expect(state.status).toBe('buffering')
  })

  it('handles REMOVE_FROM_QUEUE on active track when active was the last track', () => {
    const queue = [dummyTrack1, dummyTrack2]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    // Remove active track at index 1 (last) -> falls back to dummyTrack1 at index 0
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 1 },
    })
    expect(state.queue.length).toBe(1)
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-1')
    expect(state.status).toBe('buffering')
  })

  it('handles CLEAR_UPCOMING_QUEUE preserving prior tracks and current track', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]

    // 1. Current middle: [T1, T2, T3], active T2 at idx 1 -> leaves [T1, T2], idx 1
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue, queueIndex: 1 },
    })

    state = playerReducer(state, {
      type: 'CLEAR_UPCOMING_QUEUE',
    })

    expect(state.queue.length).toBe(2)
    expect(state.queue[0].id).toBe('track-1')
    expect(state.queue[1].id).toBe('track-2')
    expect(state.queueIndex).toBe(1)
    expect(state.currentTrack?.id).toBe('track-2')

    // Verify Previous still works
    const prevState = playerReducer(state, { type: 'PREVIOUS' })
    expect(prevState.queueIndex).toBe(0)
    expect(prevState.currentTrack?.id).toBe('track-1')

    // Verify Next reaches end of queue and pauses (repeat off)
    const nextState = playerReducer(state, { type: 'NEXT', payload: { manual: false } })
    expect(nextState.status).toBe('paused')
  })

  it('handles CLEAR_UPCOMING_QUEUE when current is first in queue', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, {
      type: 'CLEAR_UPCOMING_QUEUE',
    })

    expect(state.queue.length).toBe(1)
    expect(state.queue[0].id).toBe('track-1')
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-1')
  })

  it('handles CLEAR_UPCOMING_QUEUE no-op when current is last in queue', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack3, queue, queueIndex: 2 },
    })

    const result = playerReducer(state, {
      type: 'CLEAR_UPCOMING_QUEUE',
    })

    expect(result).toBe(state)
  })

  it('handles CLEAR_UPCOMING_QUEUE no-op on one-item queue', () => {
    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: [dummyTrack1], queueIndex: 0 },
    })

    const result = playerReducer(state, {
      type: 'CLEAR_UPCOMING_QUEUE',
    })

    expect(result).toBe(state)
  })

  it('handles CLEAR_UPCOMING_QUEUE while shuffle is active without ghost resurrection', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: true })
    // In shuffle, assume queue has 3 tracks, current is at queueIndex 0
    state = playerReducer(state, { type: 'CLEAR_UPCOMING_QUEUE' })
    expect(state.queue.length).toBe(1)

    // Turning off shuffle must not resurrect removed upcoming tracks
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: false })
    expect(state.queue.length).toBe(1)
    expect(state.queue[0].id).toBe(state.currentTrack?.id)
  })

  it('handles ADD_TO_QUEUE allowing duplicate occurrences safely', () => {
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: [dummyTrack1], queueIndex: 0 },
    })

    // Add dummyTrack1 again -> should have 2 occurrences
    state = playerReducer(state, {
      type: 'ADD_TO_QUEUE',
      payload: { tracks: dummyTrack1 },
    })
    expect(state.queue.length).toBe(2)
    expect(state.queue[0].id).toBe('track-1')
    expect(state.queue[1].id).toBe('track-1')

    // Remove first occurrence (index 0)
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: 0 },
    })
    expect(state.queue.length).toBe(1)
    expect(state.queue[0].id).toBe('track-1')
    expect(state.originalQueue.length).toBe(1)
  })

  it('handles REORDER_QUEUE with index shift', () => {
    const queue = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue, queueIndex: 0 },
    })

    // Move current track from 0 to 2
    state = playerReducer(state, {
      type: 'REORDER_QUEUE',
      payload: { fromIndex: 0, toIndex: 2 },
    })

    expect(state.queue[2].id).toBe('track-1')
    expect(state.queueIndex).toBe(2)

    // Move track 0 (dummyTrack2) to 2 (after current at 1)
    // Queue is [T2, T3, T1(current)]. Move T2 (0) to 2.
    state = playerReducer(state, {
      type: 'REORDER_QUEUE',
      payload: { fromIndex: 0, toIndex: 2 },
    })
    // Queue becomes [T3, T1(current), T2], queueIndex shifts from 2 to 1
    expect(state.queueIndex).toBe(1)
    expect(state.queue[1].id).toBe('track-1')
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

  it('handles SET_SHUFFLE with 0 tracks cleanly', () => {
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'SET_SHUFFLE',
      payload: true,
    })
    expect(state.shuffle).toBe(true)
    expect(state.queue.length).toBe(0)
    expect(state.originalQueue.length).toBe(0)
    expect(state.currentTrack).toBeNull()

    state = playerReducer(state, {
      type: 'SET_SHUFFLE',
      payload: false,
    })
    expect(state.shuffle).toBe(false)
    expect(state.queue.length).toBe(0)
  })

  it('handles SET_SHUFFLE with 1 track cleanly', () => {
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1 },
    })
    expect(state.queue.length).toBe(1)

    state = playerReducer(state, {
      type: 'SET_SHUFFLE',
      payload: true,
    })
    expect(state.shuffle).toBe(true)
    expect(state.queue.length).toBe(1)
    expect(state.currentTrack?.id).toBe('track-1')

    state = playerReducer(state, {
      type: 'SET_SHUFFLE',
      payload: false,
    })
    expect(state.shuffle).toBe(false)
    expect(state.queue.length).toBe(1)
    expect(state.currentTrack?.id).toBe('track-1')
  })

  it('preserves playback state (playing, currentTime) when toggling shuffle during playback', () => {
    const original = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack2, queue: original, queueIndex: 1 },
    })
    state = { ...state, status: 'playing', currentTime: 45 }

    // Toggle shuffle ON
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: true })
    expect(state.shuffle).toBe(true)
    expect(state.status).toBe('playing') // MUST not restart or pause
    expect(state.currentTime).toBe(45) // MUST not reset
    expect(state.currentTrack?.id).toBe('track-2')
    expect(state.queueIndex).toBe(0) // Current track pinned at front of shuffled queue

    // Toggle shuffle OFF
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: false })
    expect(state.shuffle).toBe(false)
    expect(state.status).toBe('playing')
    expect(state.currentTime).toBe(45)
    expect(state.currentTrack?.id).toBe('track-2')
    expect(state.queue).toEqual(original)
    expect(state.queueIndex).toBe(1) // Repositioned to index in original queue
  })

  it('handles NEXT and PREVIOUS correctly across shuffle toggles', () => {
    const original = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: original, queueIndex: 0 },
    })

    // Shuffle ON
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: true })
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-1')

    // Next in shuffled order
    state = playerReducer(state, { type: 'NEXT', payload: { manual: true } })
    expect(state.queueIndex).toBe(1)
    const secondTrackId = state.currentTrack?.id
    expect(secondTrackId).toBeDefined()

    // Previous in shuffled order
    state = playerReducer(state, { type: 'PREVIOUS' })
    expect(state.queueIndex).toBe(0)
    expect(state.currentTrack?.id).toBe('track-1')

    // Advance to second track again
    state = playerReducer(state, { type: 'NEXT', payload: { manual: true } })
    expect(state.currentTrack?.id).toBe(secondTrackId)

    // Shuffle OFF while on second track
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: false })
    expect(state.shuffle).toBe(false)
    expect(state.currentTrack?.id).toBe(secondTrackId)
    const expectedOriginalIndex = original.findIndex((t) => t.id === secondTrackId)
    expect(state.queueIndex).toBe(expectedOriginalIndex)
  })

  it('synchronizes originalQueue when REMOVE_FROM_QUEUE is called while shuffle is active', () => {
    const original = [dummyTrack1, dummyTrack2, dummyTrack3]
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: original, queueIndex: 0 },
    })

    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: true })
    // Current is track-1 at index 0. Find where track-3 is in the shuffled queue
    const track3Idx = state.queue.findIndex((t) => t.id === 'track-3')
    expect(track3Idx).toBeGreaterThan(0)

    // Remove track-3 while in shuffle mode
    state = playerReducer(state, {
      type: 'REMOVE_FROM_QUEUE',
      payload: { index: track3Idx },
    })
    expect(state.queue.length).toBe(2)
    expect(state.queue.find((t) => t.id === 'track-3')).toBeUndefined()
    expect(state.originalQueue.find((t) => t.id === 'track-3')).toBeUndefined()

    // Turn shuffle OFF: track-3 must NOT reappear
    state = playerReducer(state, { type: 'SET_SHUFFLE', payload: false })
    expect(state.queue.length).toBe(2)
    expect(state.queue.find((t) => t.id === 'track-3')).toBeUndefined()
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

  it('correctly queues multi-disc albums in canonical order without queue truncation', () => {
    // Disc 1: tracks 1, 2. Disc 2: tracks 1, 2. Track with 0 or missing disc number.
    const disc1Track1: LibraryTrack = { ...dummyTrack1, id: 'd1-t1', disc_number: 1, track_number: 1 }
    const disc1Track2: LibraryTrack = { ...dummyTrack1, id: 'd1-t2', disc_number: 1, track_number: 2 }
    const disc2Track1: LibraryTrack = { ...dummyTrack1, id: 'd2-t1', disc_number: 2, track_number: 1 }
    const disc2Track2: LibraryTrack = { ...dummyTrack1, id: 'd2-t2', disc_number: 2, track_number: 2 }
    const noDiscTrack: LibraryTrack = { ...dummyTrack1, id: 'd0-t3', disc_number: 0, track_number: 3 }

    const rawTracks = [disc2Track2, disc1Track1, noDiscTrack, disc2Track1, disc1Track2]
    const canonical = [...rawTracks].sort((a, b) => {
      const discA = a.disc_number || 1
      const discB = b.disc_number || 1
      if (discA !== discB) return discA - discB
      return (a.track_number || 0) - (b.track_number || 0)
    })

    // Expected order:
    // disc 1 track 1, disc 1 track 2, disc 0 track 3 (disc 0 defaults to 1), disc 2 track 1, disc 2 track 2
    expect(canonical.map((t) => t.id)).toEqual(['d1-t1', 'd1-t2', 'd0-t3', 'd2-t1', 'd2-t2'])

    // Clicking middle track: Disc 2 Track 1 (index 3 in canonical queue)
    const clickedTrack = disc2Track1
    const clickedIdx = canonical.findIndex((t) => t.id === clickedTrack.id)
    expect(clickedIdx).toBe(3)

    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: clickedTrack, queue: canonical, queueIndex: clickedIdx },
    })

    // Full queue must NOT be truncated
    expect(state.queue.length).toBe(5)
    expect(state.currentTrack?.id).toBe('d2-t1')
    expect(state.queueIndex).toBe(3)

    // Next track must be Disc 2 Track 2
    const nextState = playerReducer(state, { type: 'NEXT', payload: { manual: true } })
    expect(nextState.currentTrack?.id).toBe('d2-t2')
    expect(nextState.queueIndex).toBe(4)

    // Previous from clicked track must go to d0-t3
    const prevState = playerReducer(state, { type: 'PREVIOUS' })
    expect(prevState.currentTrack?.id).toBe('d0-t3')
    expect(prevState.queueIndex).toBe(2)
  })

  it('preserves core queue invariant across all mutations', () => {
    function assertQueueInvariant(s: typeof INITIAL_PLAYER_STATE) {
      if (s.queue.length > 0) {
        expect(s.queueIndex).toBeGreaterThanOrEqual(0)
        expect(s.queueIndex).toBeLessThan(s.queue.length)
        expect(s.currentTrack?.id).toBe(s.queue[s.queueIndex].id)
      } else {
        expect(s.queueIndex).toBe(-1)
        expect(s.currentTrack).toBeNull()
        expect(s.status).toBe('idle')
      }
    }

    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: [dummyTrack1, dummyTrack2, dummyTrack3], queueIndex: 0 },
    })
    assertQueueInvariant(state)

    // Play next
    state = playerReducer(state, { type: 'PLAY_NEXT', payload: { track: dummyTrack3 } })
    assertQueueInvariant(state)

    // Reorder
    state = playerReducer(state, { type: 'REORDER_QUEUE', payload: { fromIndex: 0, toIndex: 2 } })
    assertQueueInvariant(state)

    // Play at index
    state = playerReducer(state, { type: 'PLAY_QUEUE_INDEX', payload: { index: 1 } })
    assertQueueInvariant(state)

    // Remove non-current
    state = playerReducer(state, { type: 'REMOVE_FROM_QUEUE', payload: { index: 0 } })
    assertQueueInvariant(state)

    // Remove current
    state = playerReducer(state, { type: 'REMOVE_FROM_QUEUE', payload: { index: state.queueIndex } })
    assertQueueInvariant(state)

    // Clear upcoming
    state = playerReducer(state, { type: 'CLEAR_UPCOMING_QUEUE' })
    assertQueueInvariant(state)

    // Full clear
    state = playerReducer(state, { type: 'CLEAR_QUEUE' })
    assertQueueInvariant(state)
  })
})
