import { describe, expect, it } from 'bun:test'
import { act, render, screen } from '@testing-library/react'
import { useContext } from 'react'

import {
  PlayerProvider,
  PlayerStateContext,
  PlayerProgressContext,
  PlayerActionsContext,
} from './PlayerContext'
import { AudioEngine } from '@/lib/audio/engine'

describe('PlayerContext Performance and Splitting', () => {
  it('isolates progress updates so usePlayerState does not re-render on time ticks', () => {
    let stateRenderCount = 0
    let progressRenderCount = 0
    let actionsRenderCount = 0

    function StateConsumer() {
      const state = useContext(PlayerStateContext)
      stateRenderCount++
      return <div data-testid="state-track">{state?.currentTrack?.title ?? 'none'}</div>
    }

    function ProgressConsumer() {
      const progress = useContext(PlayerProgressContext)
      progressRenderCount++
      return (
        <div data-testid="progress">
          {progress?.currentTime}/{progress?.duration}
        </div>
      )
    }

    function ActionsConsumer() {
      useContext(PlayerActionsContext)
      actionsRenderCount++
      return <div data-testid="actions">actions</div>
    }

    render(
      <PlayerProvider>
        <StateConsumer />
        <ProgressConsumer />
        <ActionsConsumer />
      </PlayerProvider>,
    )

    expect(stateRenderCount).toBe(1)
    expect(progressRenderCount).toBe(1)
    expect(actionsRenderCount).toBe(1)
    expect(screen.getByTestId('progress').textContent).toBe('0/0')

    // Simulate audio engine time ticks
    const engine = AudioEngine.getInstance()
    const callbacks = (engine as unknown as { callbacks: { onTimeUpdate: (c: number, d: number) => void } }).callbacks

    act(() => {
      callbacks.onTimeUpdate(1.5, 180)
    })

    expect(screen.getByTestId('progress').textContent).toBe('1.5/180')
    expect(progressRenderCount).toBe(2)
    // CRITICAL: state and action consumers MUST NOT re-render on time ticks!
    expect(stateRenderCount).toBe(1)
    expect(actionsRenderCount).toBe(1)

    act(() => {
      callbacks.onTimeUpdate(2.0, 180)
    })
    act(() => {
      callbacks.onTimeUpdate(2.5, 180)
    })

    expect(screen.getByTestId('progress').textContent).toBe('2.5/180')
    expect(progressRenderCount).toBe(4)
    expect(stateRenderCount).toBe(1)
    expect(actionsRenderCount).toBe(1)
  })

  it('keeps action references stable across state mutations', () => {
    const actionRefs: Array<() => void> = []

    function ActionTester() {
      const actions = useContext(PlayerActionsContext)
      if (actions) {
        actionRefs.push(actions.toggleShuffle)
      }

      return (
        <button data-testid="toggle" onClick={actions?.toggleShuffle}>
          Toggle
        </button>
      )
    }

    render(
      <PlayerProvider>
        <ActionTester />
      </PlayerProvider>,
    )

    act(() => {
      screen.getByTestId('toggle').click()
    })

    expect(actionRefs.length).toBe(1) // Component did not re-render when shuffle toggled!
  })

  it('respects modifier keys and input focus in keyboard shortcuts', () => {
    function ShortcutConsumer() {
      const state = useContext(PlayerStateContext)
      return (
        <div>
          <span data-testid="repeat-mode">{state?.repeatMode}</span>
          <input data-testid="test-input" />
        </div>
      )
    }

    render(
      <PlayerProvider>
        <ShortcutConsumer />
      </PlayerProvider>,
    )

    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')

    // 1. Cmd+R (metaKey) -> must NOT cycle repeat
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', metaKey: true }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')

    // 2. Ctrl+R (ctrlKey) -> must NOT cycle repeat
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', ctrlKey: true }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')

    // 3. Alt+R (altKey) -> must NOT cycle repeat
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', altKey: true }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')

    // 4. Typing 'r' while input is focused -> must NOT cycle repeat
    const input = screen.getByTestId('test-input')
    act(() => {
      input.dispatchEvent(new KeyboardEvent('keydown', { key: 'r', bubbles: true }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')

    // 5. Plain 'r' -> cycles to queue
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r' }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('queue')

    // 6. Plain 'R' -> cycles to track
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'R' }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('track')

    // 7. Plain 'r' -> cycles back to off
    act(() => {
      window.dispatchEvent(new KeyboardEvent('keydown', { key: 'r' }))
    })
    expect(screen.getByTestId('repeat-mode').textContent).toBe('off')
  })

  it('triggers loadAndPlay when selecting a track that transitions status to buffering (regression test)', () => {
    const engine = AudioEngine.getInstance()
    let loadAndPlayUrl: string | null = null
    let loadUrl: string | null = null

    const origLoadAndPlay = engine.loadAndPlay
    const origLoad = engine.load

    engine.loadAndPlay = async (url: string) => {
      loadAndPlayUrl = url
    }
    engine.load = (url: string) => {
      loadUrl = url
    }

    const dummyTrack: Parameters<NonNullable<ReturnType<typeof useContext<typeof PlayerActionsContext>>>['playTrack']>[0] = {
      id: 'trk-regress-1',
      title: 'Regress Track',
      artists: [],
      album: 'Regress Album',
      album_artist: 'Regress Artist',
      track_number: 1,
      track_total: 1,
      disc_number: 1,
      disc_total: 1,
      duration_ms: 180000,
      year: 2026,
      lyrics_state: 'unknown',
      source_provider: '',
      source_id: '',
      source_url: '',
      release_id: 'rel-1',
      created_at: '',
    }

    try {
      function TrackSelector() {
        const actions = useContext(PlayerActionsContext)
        return (
          <button
            data-testid="select-track"
            onClick={() => actions?.playTrack(dummyTrack)}
          >
            Select
          </button>
        )
      }

      render(
        <PlayerProvider>
          <TrackSelector />
        </PlayerProvider>,
      )

      act(() => {
        screen.getByTestId('select-track').click()
      })

      // Must call loadAndPlay because selecting a track transitions status to 'buffering'
      // Under old bug (checking status === 'playing'), loadAndPlay was NOT called, only load was called!
      expect(loadAndPlayUrl).toBe('/api/v1/library/tracks/trk-regress-1/stream')
      expect(loadUrl).toBeNull()
    } finally {
      engine.loadAndPlay = origLoadAndPlay
      engine.load = origLoad
    }
  })
})

describe('playerReducer duration and restore state transitions', () => {
  const dummyTrack1 = {
    id: 'trk-1',
    title: 'Track One',
    album: 'Album 1',
    album_artist: 'Artist 1',
    artists: [],
    duration_ms: 180000,
    media_path: '',
    lyrics_state: 'unknown' as const,
    source_provider: '',
    source_id: '',
    source_url: '',
    release_id: 'rel-1',
    created_at: '',
  }

  const dummyTrackUnknownDuration = {
    id: 'trk-2',
    title: 'Track Two (Unknown Duration)',
    album: 'Album 2',
    album_artist: 'Artist 2',
    artists: [],
    duration_ms: 0,
    media_path: '',
    lyrics_state: 'unknown' as const,
    source_provider: '',
    source_id: '',
    source_url: '',
    release_id: 'rel-2',
    created_at: '',
  }

  it('RESTORE_STATE initializes duration from restoredCurrentTrack.duration_ms', async () => {
    const { playerReducer, INITIAL_PLAYER_STATE } = await import('./player-reducer')

    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'RESTORE_STATE',
      payload: {
        queue: [dummyTrack1],
        queueIndex: 0,
        currentTime: 45,
      },
    })

    expect(state.currentTrack?.id).toBe('trk-1')
    expect(state.duration).toBe(180)
    expect(state.currentTime).toBe(45)
    expect(state.status).toBe('paused')
  })

  it('RESTORE_STATE safely sets duration to 0 if track duration is missing or non-positive', async () => {
    const { playerReducer, INITIAL_PLAYER_STATE } = await import('./player-reducer')

    const state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'RESTORE_STATE',
      payload: {
        queue: [dummyTrackUnknownDuration],
        queueIndex: 0,
        currentTime: 0,
      },
    })

    expect(state.currentTrack?.id).toBe('trk-2')
    expect(state.duration).toBe(0)
  })

  it('track change transitions do not leak the previous track duration', async () => {
    const { playerReducer, INITIAL_PLAYER_STATE } = await import('./player-reducer')

    // Start with track 1 (180s)
    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrack1, queue: [dummyTrack1, dummyTrackUnknownDuration], queueIndex: 0 },
    })
    expect(state.duration).toBe(180)

    // Switch to track 2 with unknown duration: MUST be 0, NOT 180!
    state = playerReducer(state, {
      type: 'NEXT',
      payload: { manual: true },
    })
    expect(state.currentTrack?.id).toBe('trk-2')
    expect(state.duration).toBe(0)
  })

  it('SET_CURRENT_TIME updates duration when a valid positive number is provided', async () => {
    const { playerReducer, INITIAL_PLAYER_STATE } = await import('./player-reducer')

    let state = playerReducer(INITIAL_PLAYER_STATE, {
      type: 'PLAY_TRACK',
      payload: { track: dummyTrackUnknownDuration },
    })
    expect(state.duration).toBe(0)

    // Audio file metadata provides real duration
    state = playerReducer(state, {
      type: 'SET_CURRENT_TIME',
      payload: { currentTime: 10, duration: 215.5 },
    })
    expect(state.currentTime).toBe(10)
    expect(state.duration).toBe(215.5)

    // Subsequent tick with 0 or undefined duration preserves known 215.5
    state = playerReducer(state, {
      type: 'SET_CURRENT_TIME',
      payload: { currentTime: 11, duration: 0 },
    })
    expect(state.currentTime).toBe(11)
    expect(state.duration).toBe(215.5)
  })
})
