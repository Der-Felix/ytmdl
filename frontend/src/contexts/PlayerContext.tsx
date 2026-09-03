import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useReducer,
  useRef,
} from 'react'
import type { ReactNode } from 'react'

import { useAuth } from '@/hooks/useAuth'
import { AudioEngine } from '@/lib/audio/engine'
import {
  loadCustomPresets,
  loadHistory,
  loadPlayerState,
  saveCustomPresets,
  saveHistory,
  savePlayerState,
} from '@/lib/audio/player-storage'
import type {
  EQMode,
  ParametricFilter,
  PlayerState,
  RepeatMode,
  SleepTimerOption,
  StopAfterOption,
  VisualizerMode,
} from '@/lib/audio/types'
import { joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'
import {
  INITIAL_PLAYER_STATE,
  playerReducer,
} from './player-reducer'

export interface PlayerContextValue extends PlayerState {
  engine: AudioEngine
  playTrack: (track: LibraryTrack, queue?: LibraryTrack[], queueIndex?: number) => void
  playAlbum: (tracks: LibraryTrack[], startIndex?: number) => void
  playArtist: (tracks: LibraryTrack[], shuffle?: boolean) => void
  playNext: (track: LibraryTrack) => void
  addToQueue: (tracks: LibraryTrack[] | LibraryTrack) => void
  removeFromQueue: (index: number) => void
  reorderQueue: (fromIndex: number, toIndex: number) => void
  clearQueue: () => void
  togglePlayPause: () => void
  play: () => void
  pause: () => void
  seek: (seconds: number) => void
  previous: () => void
  next: (manual?: boolean) => void
  setVolume: (volume: number) => void
  setMuted: (muted: boolean) => void
  toggleMute: () => void
  setShuffle: (shuffle: boolean) => void
  toggleShuffle: () => void
  setRepeatMode: (mode: RepeatMode) => void
  cycleRepeatMode: () => void
  setPlaybackRate: (rate: number) => void
  setCrossfade: (seconds: number) => void
  setSmartAlbumTransition: (enabled: boolean) => void
  setSleepTimer: (option: SleepTimerOption) => void
  setStopAfter: (option: StopAfterOption) => void
  setEQEnabled: (enabled: boolean) => void
  toggleEQ: () => void
  setEQMode: (mode: EQMode) => void
  setEQPreset: (presetId: string) => void
  setEQBand: (index: number, gain: number) => void
  saveCustomPreset: (name: string) => void
  deleteCustomPreset: (id: string) => void
  setParametricFilter: (filter: ParametricFilter) => void
  addParametricFilter: (filter: ParametricFilter) => void
  deleteParametricFilter: (id: string) => void
  setPreamp: (db: number) => void
  setAutoHeadroom: (enabled: boolean) => void
  setLimiter: (enabled: boolean) => void
  setBalance: (balance: number) => void
  setMono: (mono: boolean) => void
  setBassBoost: (boost: number) => void
  setVisualizerMode: (mode: VisualizerMode) => void
}

const PlayerContext = createContext<PlayerContextValue | null>(null)

export function PlayerProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth()
  const [state, dispatch] = useReducer(playerReducer, INITIAL_PLAYER_STATE)
  const engine = useMemo(() => AudioEngine.getInstance(), [])

  const stateRef = useRef(state)
  stateRef.current = state

  const userRef = useRef(user)
  userRef.current = user

  // Track ID of the currently playing audio on the engine
  const currentEngineTrackIdRef = useRef<string | null>(null)

  // 1. Initial State Restoration from LocalStorage
  useEffect(() => {
    const customPresets = loadCustomPresets()
    const history = loadHistory(user?.id)
    const saved = loadPlayerState(user?.id)

    if (saved || customPresets.length > 0 || history.length > 0) {
      dispatch({
        type: 'RESTORE_STATE',
        payload: {
          ...(saved || {}),
          customPresets,
          history,
        },
      })
    }
  }, [user?.id])

  // 2. Connect Engine Callbacks
  useEffect(() => {
    engine.setCallbacks({
      onTimeUpdate: (currentTime, duration) => {
        dispatch({
          type: 'SET_CURRENT_TIME',
          payload: { currentTime, duration },
        })
      },
      onStatusChange: (status) => {
        dispatch({ type: 'SET_STATUS', payload: status })
      },
      onError: (error) => {
        dispatch({ type: 'SET_ERROR', payload: error })
      },
      onTrackEnded: () => {
        dispatch({ type: 'NEXT', payload: { manual: false } })
      },
      onNextDeckCrossfadeStart: () => {
        dispatch({ type: 'NEXT', payload: { manual: false } })
      },
    })
  }, [engine])

  // 3. Sync Audio Engine DSP settings with State
  useEffect(() => {
    engine.setVolume(state.volume)
    engine.setMuted(state.muted)
    engine.setPlaybackRate(state.playbackRate)
    engine.setCrossfade(state.crossfadeSeconds)
    engine.setEQEnabled(state.eqEnabled)
    engine.setEQMode(state.eqMode)
    engine.setGraphicBands(state.graphicBands)
    engine.setParametricFilters(state.parametricFilters)
    engine.setPreamp(state.preamp)
    engine.setAutoHeadroom(state.autoHeadroom)
    engine.setLimiter(state.limiterEnabled)
    engine.setBalance(state.balance)
    engine.setMono(state.mono)
  }, [
    engine,
    state.volume,
    state.muted,
    state.playbackRate,
    state.crossfadeSeconds,
    state.eqEnabled,
    state.eqMode,
    state.graphicBands,
    state.parametricFilters,
    state.preamp,
    state.autoHeadroom,
    state.limiterEnabled,
    state.balance,
    state.mono,
  ])

  // 4. Load & Play Track when currentTrack or status changes
  useEffect(() => {
    const track = state.currentTrack
    if (!track) {
      currentEngineTrackIdRef.current = null
      engine.pause()
      return
    }

    const streamUrl = `/api/v1/library/tracks/${encodeURIComponent(track.id)}/stream`

    if (currentEngineTrackIdRef.current !== track.id) {
      currentEngineTrackIdRef.current = track.id
      if (state.status === 'playing') {
        void engine.loadAndPlay(streamUrl, state.currentTime)
      } else {
        engine.load(streamUrl, state.currentTime)
      }
    }

    // Determine & Preload Next Track
    const nextIdx = state.queueIndex + 1
    let nextTrack: LibraryTrack | null = null
    if (nextIdx < state.queue.length) {
      nextTrack = state.queue[nextIdx] ?? null
    } else if (state.repeatMode === 'queue' && state.queue.length > 0) {
      nextTrack = state.queue[0] ?? null
    }

    if (nextTrack) {
      const nextStreamUrl = `/api/v1/library/tracks/${encodeURIComponent(nextTrack.id)}/stream`
      // Check Smart Album Transition Bypass
      const isConsecutiveAlbumTrack =
        Boolean(track.release_id) &&
        track.release_id === nextTrack.release_id &&
        (nextTrack.track_number === track.track_number + 1 ||
          (nextTrack.disc_number === track.disc_number + 1 && nextTrack.track_number === 1))

      if (state.smartAlbumTransition && isConsecutiveAlbumTrack) {
        // Gapless seamless transition without crossfade
        engine.setCrossfade(0)
      } else {
        engine.setCrossfade(state.crossfadeSeconds)
      }

      engine.preloadNext(nextStreamUrl)
    }
  }, [
    engine,
    state.currentTrack,
    state.queueIndex,
    state.repeatMode,
    state.crossfadeSeconds,
    state.smartAlbumTransition,
    state.queue,
  ])

  // 5. Media Session API Integration
  useEffect(() => {
    if (typeof window === 'undefined' || !('mediaSession' in navigator)) return

    const track = state.currentTrack
    if (track) {
      const artist = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist
      const artwork = track.cover_url
        ? [
            { src: track.cover_url, sizes: '96x96', type: 'image/jpeg' },
            { src: track.cover_url, sizes: '256x256', type: 'image/jpeg' },
            { src: track.cover_url, sizes: '512x512', type: 'image/jpeg' },
          ]
        : []

      navigator.mediaSession.metadata = new MediaMetadata({
        title: track.title,
        artist,
        album: track.album || '',
        artwork,
      })

      navigator.mediaSession.playbackState = state.status === 'playing' ? 'playing' : 'paused'
    } else {
      navigator.mediaSession.metadata = null
      navigator.mediaSession.playbackState = 'none'
    }

    const actionHandlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      ['play', () => { engine.play(); dispatch({ type: 'SET_STATUS', payload: 'playing' }) }],
      ['pause', () => { engine.pause(); dispatch({ type: 'SET_STATUS', payload: 'paused' }) }],
      ['previoustrack', () => dispatch({ type: 'PREVIOUS' })],
      ['nexttrack', () => dispatch({ type: 'NEXT', payload: { manual: true } })],
      ['seekto', (details) => {
        if (details.seekTime !== undefined) {
          engine.seek(details.seekTime)
          dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: details.seekTime } })
        }
      }],
      ['seekbackward', (details) => {
        const skip = details.seekOffset || 5
        const newTime = Math.max(0, stateRef.current.currentTime - skip)
        engine.seek(newTime)
        dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
      }],
      ['seekforward', (details) => {
        const skip = details.seekOffset || 5
        const newTime = Math.min(stateRef.current.duration, stateRef.current.currentTime + skip)
        engine.seek(newTime)
        dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
      }],
      ['stop', () => { engine.pause(); dispatch({ type: 'SET_STATUS', payload: 'paused' }) }],
    ]

    for (const [action, handler] of actionHandlers) {
      try {
        navigator.mediaSession.setActionHandler(action, handler)
      } catch {
        // Some actions might not be supported on all browsers
      }
    }

    return () => {
      for (const [action] of actionHandlers) {
        try {
          navigator.mediaSession.setActionHandler(action, null)
        } catch {
          // Ignore
        }
      }
    }
  }, [engine, state.currentTrack, state.status])

  // Update MediaSession Position State
  useEffect(() => {
    if (
      typeof window !== 'undefined' &&
      'mediaSession' in navigator &&
      'setPositionState' in navigator.mediaSession &&
      state.duration > 0
    ) {
      try {
        navigator.mediaSession.setPositionState({
          duration: state.duration,
          playbackRate: state.playbackRate,
          position: Math.max(0, Math.min(state.currentTime, state.duration)),
        })
      } catch {
        // Ignore invalid range errors during transitions
      }
    }
  }, [state.currentTime, state.duration, state.playbackRate])

  // 6. Global Keyboard Shortcuts
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      const target = e.target as HTMLElement | null
      if (
        target &&
        (target.tagName === 'INPUT' ||
          target.tagName === 'TEXTAREA' ||
          target.tagName === 'SELECT' ||
          target.isContentEditable)
      ) {
        return
      }

      switch (e.key) {
        case ' ':
          e.preventDefault()
          if (stateRef.current.status === 'playing') {
            engine.pause()
            dispatch({ type: 'SET_STATUS', payload: 'paused' })
          } else {
            engine.play()
            dispatch({ type: 'SET_STATUS', payload: 'playing' })
          }
          break

        case 'ArrowLeft':
          if (e.shiftKey) {
            e.preventDefault()
            dispatch({ type: 'PREVIOUS' })
          } else {
            e.preventDefault()
            const newTime = Math.max(0, stateRef.current.currentTime - 5)
            engine.seek(newTime)
            dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
          }
          break

        case 'ArrowRight':
          if (e.shiftKey) {
            e.preventDefault()
            dispatch({ type: 'NEXT', payload: { manual: true } })
          } else {
            e.preventDefault()
            const newTime = Math.min(stateRef.current.duration, stateRef.current.currentTime + 5)
            engine.seek(newTime)
            dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
          }
          break

        case 'ArrowUp':
          e.preventDefault()
          dispatch({ type: 'SET_VOLUME', payload: stateRef.current.volume + 0.05 })
          break

        case 'ArrowDown':
          e.preventDefault()
          dispatch({ type: 'SET_VOLUME', payload: stateRef.current.volume - 0.05 })
          break

        case 'm':
        case 'M':
          e.preventDefault()
          dispatch({ type: 'SET_MUTED', payload: !stateRef.current.muted })
          break

        case 's':
        case 'S':
          e.preventDefault()
          dispatch({ type: 'SET_SHUFFLE', payload: !stateRef.current.shuffle })
          break

        case 'r':
        case 'R':
          e.preventDefault()
          {
            const modes: RepeatMode[] = ['off', 'queue', 'track']
            const nextIdx = (modes.indexOf(stateRef.current.repeatMode) + 1) % modes.length
            const nextMode = modes[nextIdx] ?? 'off'
            dispatch({ type: 'SET_REPEAT_MODE', payload: nextMode })
          }
          break
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [engine])

  // 7. Throttled LocalStorage Persistence
  useEffect(() => {
    const interval = window.setInterval(() => {
      const s = stateRef.current
      savePlayerState(userRef.current?.id, {
        queue: s.queue,
        originalQueue: s.originalQueue,
        queueIndex: s.queueIndex,
        currentTime: s.currentTime,
        volume: s.volume,
        muted: s.muted,
        shuffle: s.shuffle,
        repeatMode: s.repeatMode,
        playbackRate: s.playbackRate,
        crossfadeSeconds: s.crossfadeSeconds,
        smartAlbumTransition: s.smartAlbumTransition,
        eqEnabled: s.eqEnabled,
        eqMode: s.eqMode,
        selectedPresetId: s.selectedPresetId,
        graphicBands: s.graphicBands,
        parametricFilters: s.parametricFilters,
        preamp: s.preamp,
        autoHeadroom: s.autoHeadroom,
        limiterEnabled: s.limiterEnabled,
        balance: s.balance,
        mono: s.mono,
        bassBoost: s.bassBoost,
        visualizerMode: s.visualizerMode,
      })
      saveHistory(userRef.current?.id, s.history)
      saveCustomPresets(s.customPresets)
    }, 5000)

    const handleBeforeUnload = () => {
      const s = stateRef.current
      savePlayerState(userRef.current?.id, {
        queue: s.queue,
        originalQueue: s.originalQueue,
        queueIndex: s.queueIndex,
        currentTime: s.currentTime,
        volume: s.volume,
        muted: s.muted,
        shuffle: s.shuffle,
        repeatMode: s.repeatMode,
        playbackRate: s.playbackRate,
        crossfadeSeconds: s.crossfadeSeconds,
        smartAlbumTransition: s.smartAlbumTransition,
        eqEnabled: s.eqEnabled,
        eqMode: s.eqMode,
        selectedPresetId: s.selectedPresetId,
        graphicBands: s.graphicBands,
        parametricFilters: s.parametricFilters,
        preamp: s.preamp,
        autoHeadroom: s.autoHeadroom,
        limiterEnabled: s.limiterEnabled,
        balance: s.balance,
        mono: s.mono,
        bassBoost: s.bassBoost,
        visualizerMode: s.visualizerMode,
      })
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => {
      clearInterval(interval)
      window.removeEventListener('beforeunload', handleBeforeUnload)
    }
  }, [])

  // 8. Sleep Timer Countdown Check
  useEffect(() => {
    if (!state.sleepTimerEndsAt) return

    const checkTimer = window.setInterval(() => {
      if (state.sleepTimerEndsAt && Date.now() >= state.sleepTimerEndsAt) {
        engine.pause()
        dispatch({ type: 'SET_STATUS', payload: 'paused' })
        dispatch({ type: 'SET_SLEEP_TIMER', payload: 'off' })
      }
    }, 1000)

    return () => clearInterval(checkTimer)
  }, [engine, state.sleepTimerEndsAt])

  // Context Actions
  const playTrack = useCallback(
    (track: LibraryTrack, queue?: LibraryTrack[], queueIndex?: number) => {
      dispatch({ type: 'PLAY_TRACK', payload: { track, queue, queueIndex } })
    },
    [],
  )

  const playAlbum = useCallback((tracks: LibraryTrack[], startIndex = 0) => {
    dispatch({ type: 'PLAY_ALBUM', payload: { tracks, startIndex } })
  }, [])

  const playArtist = useCallback((tracks: LibraryTrack[], shuffle = false) => {
    dispatch({ type: 'PLAY_ARTIST', payload: { tracks, shuffle } })
  }, [])

  const playNextAction = useCallback((track: LibraryTrack) => {
    dispatch({ type: 'PLAY_NEXT', payload: { track } })
  }, [])

  const addToQueueAction = useCallback((tracks: LibraryTrack[] | LibraryTrack) => {
    dispatch({ type: 'ADD_TO_QUEUE', payload: { tracks } })
  }, [])

  const removeFromQueueAction = useCallback((index: number) => {
    dispatch({ type: 'REMOVE_FROM_QUEUE', payload: { index } })
  }, [])

  const reorderQueueAction = useCallback((fromIndex: number, toIndex: number) => {
    dispatch({ type: 'REORDER_QUEUE', payload: { fromIndex, toIndex } })
  }, [])

  const clearQueueAction = useCallback(() => {
    dispatch({ type: 'CLEAR_QUEUE' })
    engine.pause()
  }, [engine])

  const togglePlayPause = useCallback(() => {
    if (state.status === 'playing') {
      engine.pause()
      dispatch({ type: 'SET_STATUS', payload: 'paused' })
    } else {
      engine.play()
      dispatch({ type: 'SET_STATUS', payload: 'playing' })
    }
  }, [engine, state.status])

  const playAction = useCallback(() => {
    engine.play()
    dispatch({ type: 'SET_STATUS', payload: 'playing' })
  }, [engine])

  const pauseAction = useCallback(() => {
    engine.pause()
    dispatch({ type: 'SET_STATUS', payload: 'paused' })
  }, [engine])

  const seekAction = useCallback(
    (seconds: number) => {
      engine.seek(seconds)
      dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: seconds } })
    },
    [engine],
  )

  const previousAction = useCallback(() => {
    dispatch({ type: 'PREVIOUS' })
  }, [])

  const nextAction = useCallback((manual = true) => {
    dispatch({ type: 'NEXT', payload: { manual } })
  }, [])

  const setVolumeAction = useCallback((vol: number) => {
    dispatch({ type: 'SET_VOLUME', payload: vol })
  }, [])

  const setMutedAction = useCallback((muted: boolean) => {
    dispatch({ type: 'SET_MUTED', payload: muted })
  }, [])

  const toggleMuteAction = useCallback(() => {
    dispatch({ type: 'SET_MUTED', payload: !state.muted })
  }, [state.muted])

  const setShuffleAction = useCallback((shuffle: boolean) => {
    dispatch({ type: 'SET_SHUFFLE', payload: shuffle })
  }, [])

  const toggleShuffleAction = useCallback(() => {
    dispatch({ type: 'SET_SHUFFLE', payload: !state.shuffle })
  }, [state.shuffle])

  const setRepeatModeAction = useCallback((mode: RepeatMode) => {
    dispatch({ type: 'SET_REPEAT_MODE', payload: mode })
  }, [])

  const cycleRepeatModeAction = useCallback(() => {
    const modes: RepeatMode[] = ['off', 'queue', 'track']
    const nextIdx = (modes.indexOf(state.repeatMode) + 1) % modes.length
    const nextMode = modes[nextIdx] ?? 'off'
    dispatch({ type: 'SET_REPEAT_MODE', payload: nextMode })
  }, [state.repeatMode])

  const setPlaybackRateAction = useCallback((rate: number) => {
    dispatch({ type: 'SET_PLAYBACK_RATE', payload: rate })
  }, [])

  const setCrossfadeAction = useCallback((seconds: number) => {
    dispatch({ type: 'SET_CROSSFADE', payload: seconds })
  }, [])

  const setSmartAlbumTransitionAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_SMART_ALBUM_TRANSITION', payload: enabled })
  }, [])

  const setSleepTimerAction = useCallback((option: SleepTimerOption) => {
    dispatch({ type: 'SET_SLEEP_TIMER', payload: option })
  }, [])

  const setStopAfterAction = useCallback((option: StopAfterOption) => {
    dispatch({ type: 'SET_STOP_AFTER', payload: option })
  }, [])

  const setEQEnabledAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_EQ_ENABLED', payload: enabled })
  }, [])

  const toggleEQAction = useCallback(() => {
    dispatch({ type: 'SET_EQ_ENABLED', payload: !state.eqEnabled })
  }, [state.eqEnabled])

  const setEQModeAction = useCallback((mode: EQMode) => {
    dispatch({ type: 'SET_EQ_MODE', payload: mode })
  }, [])

  const setEQPresetAction = useCallback((presetId: string) => {
    dispatch({ type: 'SET_EQ_PRESET', payload: presetId })
  }, [])

  const setEQBandAction = useCallback((index: number, gain: number) => {
    dispatch({ type: 'SET_EQ_BAND', payload: { index, gain } })
  }, [])

  const saveCustomPresetAction = useCallback((name: string) => {
    dispatch({ type: 'SAVE_CUSTOM_PRESET', payload: { name } })
  }, [])

  const deleteCustomPresetAction = useCallback((id: string) => {
    dispatch({ type: 'DELETE_CUSTOM_PRESET', payload: id })
  }, [])

  const setParametricFilterAction = useCallback((filter: ParametricFilter) => {
    dispatch({ type: 'SET_PARAMETRIC_FILTER', payload: filter })
  }, [])

  const addParametricFilterAction = useCallback((filter: ParametricFilter) => {
    dispatch({ type: 'ADD_PARAMETRIC_FILTER', payload: filter })
  }, [])

  const deleteParametricFilterAction = useCallback((id: string) => {
    dispatch({ type: 'DELETE_PARAMETRIC_FILTER', payload: id })
  }, [])

  const setPreampAction = useCallback((db: number) => {
    dispatch({ type: 'SET_PREAMP', payload: db })
  }, [])

  const setAutoHeadroomAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_AUTO_HEADROOM', payload: enabled })
  }, [])

  const setLimiterAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_LIMITER', payload: enabled })
  }, [])

  const setBalanceAction = useCallback((balance: number) => {
    dispatch({ type: 'SET_BALANCE', payload: balance })
  }, [])

  const setMonoAction = useCallback((mono: boolean) => {
    dispatch({ type: 'SET_MONO', payload: mono })
  }, [])

  const setBassBoostAction = useCallback((boost: number) => {
    dispatch({ type: 'SET_BASS_BOOST', payload: boost })
  }, [])

  const setVisualizerModeAction = useCallback((mode: VisualizerMode) => {
    dispatch({ type: 'SET_VISUALIZER_MODE', payload: mode })
  }, [])

  const value = useMemo<PlayerContextValue>(
    () => ({
      ...state,
      engine,
      playTrack,
      playAlbum,
      playArtist,
      playNext: playNextAction,
      addToQueue: addToQueueAction,
      removeFromQueue: removeFromQueueAction,
      reorderQueue: reorderQueueAction,
      clearQueue: clearQueueAction,
      togglePlayPause,
      play: playAction,
      pause: pauseAction,
      seek: seekAction,
      previous: previousAction,
      next: nextAction,
      setVolume: setVolumeAction,
      setMuted: setMutedAction,
      toggleMute: toggleMuteAction,
      setShuffle: setShuffleAction,
      toggleShuffle: toggleShuffleAction,
      setRepeatMode: setRepeatModeAction,
      cycleRepeatMode: cycleRepeatModeAction,
      setPlaybackRate: setPlaybackRateAction,
      setCrossfade: setCrossfadeAction,
      setSmartAlbumTransition: setSmartAlbumTransitionAction,
      setSleepTimer: setSleepTimerAction,
      setStopAfter: setStopAfterAction,
      setEQEnabled: setEQEnabledAction,
      toggleEQ: toggleEQAction,
      setEQMode: setEQModeAction,
      setEQPreset: setEQPresetAction,
      setEQBand: setEQBandAction,
      saveCustomPreset: saveCustomPresetAction,
      deleteCustomPreset: deleteCustomPresetAction,
      setParametricFilter: setParametricFilterAction,
      addParametricFilter: addParametricFilterAction,
      deleteParametricFilter: deleteParametricFilterAction,
      setPreamp: setPreampAction,
      setAutoHeadroom: setAutoHeadroomAction,
      setLimiter: setLimiterAction,
      setBalance: setBalanceAction,
      setMono: setMonoAction,
      setBassBoost: setBassBoostAction,
      setVisualizerMode: setVisualizerModeAction,
    }),
    [
      state,
      engine,
      playTrack,
      playAlbum,
      playArtist,
      playNextAction,
      addToQueueAction,
      removeFromQueueAction,
      reorderQueueAction,
      clearQueueAction,
      togglePlayPause,
      playAction,
      pauseAction,
      seekAction,
      previousAction,
      nextAction,
      setVolumeAction,
      setMutedAction,
      toggleMuteAction,
      setShuffleAction,
      toggleShuffleAction,
      setRepeatModeAction,
      cycleRepeatModeAction,
      setPlaybackRateAction,
      setCrossfadeAction,
      setSmartAlbumTransitionAction,
      setSleepTimerAction,
      setStopAfterAction,
      setEQEnabledAction,
      toggleEQAction,
      setEQModeAction,
      setEQPresetAction,
      setEQBandAction,
      saveCustomPresetAction,
      deleteCustomPresetAction,
      setParametricFilterAction,
      addParametricFilterAction,
      deleteParametricFilterAction,
      setPreampAction,
      setAutoHeadroomAction,
      setLimiterAction,
      setBalanceAction,
      setMonoAction,
      setBassBoostAction,
      setVisualizerModeAction,
    ],
  )

  return <PlayerContext.Provider value={value}>{children}</PlayerContext.Provider>
}

export function usePlayer(): PlayerContextValue {
  const ctx = useContext(PlayerContext)
  if (!ctx) {
    throw new Error('usePlayer must be used within a PlayerProvider')
  }
  return ctx
}
