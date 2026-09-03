import {
  BUILTIN_PRESETS,
  DEFAULT_PARAMETRIC_FILTERS,
} from '@/lib/audio/eqPresets'
import type {
  EQMode,
  EQPreset,
  ParametricFilter,
  PlaybackStatus,
  PlayerHistoryItem,
  PlayerState,
  RepeatMode,
  SleepTimerOption,
  StopAfterOption,
  VisualizerMode,
} from '@/lib/audio/types'
import type { LibraryTrack } from '@/types/api'

export type PlayerAction =
  | { type: 'PLAY_TRACK'; payload: { track: LibraryTrack; queue?: LibraryTrack[]; queueIndex?: number } }
  | { type: 'PLAY_ALBUM'; payload: { tracks: LibraryTrack[]; startIndex?: number } }
  | { type: 'PLAY_ARTIST'; payload: { tracks: LibraryTrack[]; shuffle?: boolean } }
  | { type: 'PLAY_NEXT'; payload: { track: LibraryTrack } }
  | { type: 'ADD_TO_QUEUE'; payload: { tracks: LibraryTrack[] | LibraryTrack } }
  | { type: 'REMOVE_FROM_QUEUE'; payload: { index: number } }
  | { type: 'REORDER_QUEUE'; payload: { fromIndex: number; toIndex: number } }
  | { type: 'CLEAR_QUEUE' }
  | { type: 'SET_STATUS'; payload: PlaybackStatus }
  | { type: 'SET_CURRENT_TIME'; payload: { currentTime: number; duration?: number } }
  | { type: 'SET_VOLUME'; payload: number }
  | { type: 'SET_MUTED'; payload: boolean }
  | { type: 'SET_SHUFFLE'; payload: boolean }
  | { type: 'SET_REPEAT_MODE'; payload: RepeatMode }
  | { type: 'SET_PLAYBACK_RATE'; payload: number }
  | { type: 'SET_CROSSFADE'; payload: number }
  | { type: 'SET_SMART_ALBUM_TRANSITION'; payload: boolean }
  | { type: 'SET_SLEEP_TIMER'; payload: SleepTimerOption }
  | { type: 'SET_STOP_AFTER'; payload: StopAfterOption }
  | { type: 'SET_EQ_ENABLED'; payload: boolean }
  | { type: 'SET_EQ_MODE'; payload: EQMode }
  | { type: 'SET_EQ_PRESET'; payload: string }
  | { type: 'SET_EQ_BAND'; payload: { index: number; gain: number } }
  | { type: 'SAVE_CUSTOM_PRESET'; payload: { name: string } }
  | { type: 'DELETE_CUSTOM_PRESET'; payload: string }
  | { type: 'SET_PARAMETRIC_FILTER'; payload: ParametricFilter }
  | { type: 'ADD_PARAMETRIC_FILTER'; payload: ParametricFilter }
  | { type: 'DELETE_PARAMETRIC_FILTER'; payload: string }
  | { type: 'SET_PREAMP'; payload: number }
  | { type: 'SET_AUTO_HEADROOM'; payload: boolean }
  | { type: 'SET_LIMITER'; payload: boolean }
  | { type: 'SET_BALANCE'; payload: number }
  | { type: 'SET_MONO'; payload: boolean }
  | { type: 'SET_BASS_BOOST'; payload: number }
  | { type: 'SET_VISUALIZER_MODE'; payload: VisualizerMode }
  | { type: 'SET_ERROR'; payload: string | null }
  | { type: 'PREVIOUS' }
  | { type: 'NEXT'; payload?: { manual?: boolean } }
  | { type: 'RESTORE_STATE'; payload: Partial<PlayerState> }

export const INITIAL_PLAYER_STATE: PlayerState = {
  currentTrack: null,
  queue: [],
  originalQueue: [],
  queueIndex: -1,
  status: 'idle',
  currentTime: 0,
  duration: 0,
  volume: 1.0,
  muted: false,
  shuffle: false,
  repeatMode: 'off',
  playbackRate: 1.0,
  crossfadeSeconds: 0,
  smartAlbumTransition: true,
  sleepTimer: 'off',
  sleepTimerEndsAt: null,
  stopAfter: 'none',

  // DSP
  eqEnabled: true,
  eqMode: 'graphic',
  selectedPresetId: 'flat',
  graphicBands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
  parametricFilters: [...DEFAULT_PARAMETRIC_FILTERS],
  customPresets: [],
  preamp: 0,
  autoHeadroom: true,
  limiterEnabled: true,
  balance: 0,
  mono: false,
  bassBoost: 0,

  // UI
  visualizerMode: 'spectrum',
  peakWarning: false,
  history: [],
  error: null,
}

function shuffleArray<T>(array: T[]): T[] {
  const arr = [...array]
  for (let i = arr.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    const temp = arr[i]!
    arr[i] = arr[j]!
    arr[j] = temp
  }
  return arr
}

function pushHistory(history: PlayerHistoryItem[], track: LibraryTrack): PlayerHistoryItem[] {
  const item: PlayerHistoryItem = {
    track,
    playedAt: new Date().toISOString(),
  }
  const filtered = history.filter((h) => h.track.id !== track.id)
  return [item, ...filtered].slice(0, 100)
}

export function playerReducer(state: PlayerState, action: PlayerAction): PlayerState {
  switch (action.type) {
    case 'PLAY_TRACK': {
      const { track, queue, queueIndex } = action.payload
      const newQueue = queue && queue.length > 0 ? [...queue] : [track]
      let idx = queueIndex ?? newQueue.findIndex((t) => t.id === track.id)
      if (idx === -1) {
        newQueue.push(track)
        idx = newQueue.length - 1
      }
      return {
        ...state,
        currentTrack: track,
        queue: newQueue,
        originalQueue: state.shuffle && state.originalQueue.length > 0 ? state.originalQueue : newQueue,
        queueIndex: idx,
        status: 'buffering',
        currentTime: 0,
        duration: track.duration_ms ? track.duration_ms / 1000 : 0,
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_ALBUM': {
      const { tracks, startIndex = 0 } = action.payload
      if (!tracks.length) return state
      const startIdx = Math.max(0, Math.min(startIndex, tracks.length - 1))
      const track = tracks[startIdx]!
      return {
        ...state,
        currentTrack: track,
        queue: [...tracks],
        originalQueue: [...tracks],
        queueIndex: startIdx,
        shuffle: false,
        status: 'buffering',
        currentTime: 0,
        duration: track.duration_ms ? track.duration_ms / 1000 : 0,
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_ARTIST': {
      const { tracks, shuffle = false } = action.payload
      if (!tracks.length) return state
      const originalQueue = [...tracks]
      let finalQueue = [...tracks]
      if (shuffle) {
        finalQueue = shuffleArray(finalQueue)
      }
      const track = finalQueue[0]!
      return {
        ...state,
        currentTrack: track,
        queue: finalQueue,
        originalQueue,
        queueIndex: 0,
        shuffle,
        status: 'buffering',
        currentTime: 0,
        duration: track.duration_ms ? track.duration_ms / 1000 : 0,
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_NEXT': {
      const { track } = action.payload
      if (!state.currentTrack || state.queue.length === 0) {
        return playerReducer(state, { type: 'PLAY_TRACK', payload: { track } })
      }
      const newQueue = [...state.queue]
      const insertAt = state.queueIndex + 1
      newQueue.splice(insertAt, 0, track)
      const newOriginal = [...state.originalQueue]
      newOriginal.push(track)
      return {
        ...state,
        queue: newQueue,
        originalQueue: newOriginal,
      }
    }

    case 'ADD_TO_QUEUE': {
      const tracksToAdd = Array.isArray(action.payload.tracks)
        ? action.payload.tracks
        : [action.payload.tracks]
      if (tracksToAdd.length === 0) return state

      const firstTrack = tracksToAdd[0]
      if (!firstTrack) return state

      if (!state.currentTrack || state.queue.length === 0) {
        return playerReducer(state, {
          type: 'PLAY_TRACK',
          payload: { track: firstTrack, queue: tracksToAdd, queueIndex: 0 },
        })
      }

      return {
        ...state,
        queue: [...state.queue, ...tracksToAdd],
        originalQueue: [...state.originalQueue, ...tracksToAdd],
      }
    }

    case 'REMOVE_FROM_QUEUE': {
      const { index } = action.payload
      if (index < 0 || index >= state.queue.length) return state

      const newQueue = state.queue.filter((_, i) => i !== index)
      if (newQueue.length === 0) {
        return {
          ...state,
          currentTrack: null,
          queue: [],
          originalQueue: [],
          queueIndex: -1,
          status: 'idle',
          currentTime: 0,
          duration: 0,
        }
      }

      let newIndex = state.queueIndex
      let newCurrentTrack = state.currentTrack

      if (index < state.queueIndex) {
        newIndex--
      } else if (index === state.queueIndex) {
        if (newIndex >= newQueue.length) {
          newIndex = 0
        }
        newCurrentTrack = newQueue[newIndex] ?? null
      }

      return {
        ...state,
        queue: newQueue,
        queueIndex: newIndex,
        currentTrack: newCurrentTrack,
      }
    }

    case 'REORDER_QUEUE': {
      const { fromIndex, toIndex } = action.payload
      if (
        fromIndex < 0 ||
        fromIndex >= state.queue.length ||
        toIndex < 0 ||
        toIndex >= state.queue.length ||
        fromIndex === toIndex
      ) {
        return state
      }

      const item = state.queue[fromIndex]
      if (!item) return state
      const newQueue = [...state.queue]
      newQueue.splice(fromIndex, 1)
      newQueue.splice(toIndex, 0, item)

      let newIndex = state.queueIndex
      if (fromIndex === state.queueIndex) {
        newIndex = toIndex
      } else if (fromIndex < state.queueIndex && toIndex >= state.queueIndex) {
        newIndex--
      } else if (fromIndex > state.queueIndex && toIndex <= state.queueIndex) {
        newIndex++
      }

      return {
        ...state,
        queue: newQueue,
        queueIndex: newIndex,
      }
    }

    case 'CLEAR_QUEUE': {
      return {
        ...state,
        currentTrack: null,
        queue: [],
        originalQueue: [],
        queueIndex: -1,
        status: 'idle',
        currentTime: 0,
        duration: 0,
      }
    }

    case 'SET_STATUS': {
      return { ...state, status: action.payload }
    }

    case 'SET_CURRENT_TIME': {
      const { currentTime, duration } = action.payload
      return {
        ...state,
        currentTime,
        duration: duration !== undefined && duration > 0 ? duration : state.duration,
      }
    }

    case 'SET_VOLUME': {
      return { ...state, volume: Math.max(0, Math.min(1, action.payload)) }
    }

    case 'SET_MUTED': {
      return { ...state, muted: action.payload }
    }

    case 'SET_SHUFFLE': {
      const shuffle = action.payload
      if (shuffle === state.shuffle) return state

      if (shuffle) {
        // Turn ON: Preserve originalQueue, shuffle rest of queue around current track
        const original = state.originalQueue.length > 0 ? state.originalQueue : [...state.queue]
        if (!state.currentTrack || state.queue.length <= 1) {
          return { ...state, shuffle: true, originalQueue: original }
        }

        const current = state.currentTrack
        const rest = original.filter((t) => t.id !== current.id)
        const shuffled = [current, ...shuffleArray(rest)]

        return {
          ...state,
          shuffle: true,
          originalQueue: original,
          queue: shuffled,
          queueIndex: 0,
        }
      } else {
        // Turn OFF: Restore originalQueue, reposition queueIndex to current track
        const restored = state.originalQueue.length > 0 ? [...state.originalQueue] : [...state.queue]
        let newIndex = 0
        if (state.currentTrack) {
          const found = restored.findIndex((t) => t.id === state.currentTrack!.id)
          if (found !== -1) newIndex = found
        }
        return {
          ...state,
          shuffle: false,
          queue: restored,
          queueIndex: newIndex,
        }
      }
    }

    case 'SET_REPEAT_MODE': {
      return { ...state, repeatMode: action.payload }
    }

    case 'SET_PLAYBACK_RATE': {
      return { ...state, playbackRate: action.payload }
    }

    case 'SET_CROSSFADE': {
      return { ...state, crossfadeSeconds: Math.max(0, Math.min(12, action.payload)) }
    }

    case 'SET_SMART_ALBUM_TRANSITION': {
      return { ...state, smartAlbumTransition: action.payload }
    }

    case 'SET_SLEEP_TIMER': {
      const option = action.payload
      let endsAt: number | null = null
      if (option === '15') endsAt = Date.now() + 15 * 60 * 1000
      else if (option === '30') endsAt = Date.now() + 30 * 60 * 1000
      else if (option === '45') endsAt = Date.now() + 45 * 60 * 1000
      else if (option === '60') endsAt = Date.now() + 60 * 60 * 1000

      return {
        ...state,
        sleepTimer: option,
        sleepTimerEndsAt: endsAt,
      }
    }

    case 'SET_STOP_AFTER': {
      return { ...state, stopAfter: action.payload }
    }

    case 'SET_EQ_ENABLED': {
      return { ...state, eqEnabled: action.payload }
    }

    case 'SET_EQ_MODE': {
      return { ...state, eqMode: action.payload }
    }

    case 'SET_EQ_PRESET': {
      const presetId = action.payload
      const allPresets = [...BUILTIN_PRESETS, ...state.customPresets]
      const preset = allPresets.find((p) => p.id === presetId)
      if (!preset) return { ...state, selectedPresetId: presetId }
      return {
        ...state,
        selectedPresetId: presetId,
        graphicBands: [...preset.values],
      }
    }

    case 'SET_EQ_BAND': {
      const { index, gain } = action.payload
      if (index < 0 || index >= 10) return state
      const newBands = [...state.graphicBands]
      newBands[index] = Math.max(-12, Math.min(12, gain))
      return {
        ...state,
        graphicBands: newBands,
        selectedPresetId: 'custom',
      }
    }

    case 'SAVE_CUSTOM_PRESET': {
      const { name } = action.payload
      const newPreset: EQPreset = {
        id: `custom-${Date.now()}`,
        name: name.trim() || 'Custom Preset',
        values: [...state.graphicBands],
        isCustom: true,
      }
      return {
        ...state,
        customPresets: [...state.customPresets, newPreset],
        selectedPresetId: newPreset.id,
      }
    }

    case 'DELETE_CUSTOM_PRESET': {
      const id = action.payload
      return {
        ...state,
        customPresets: state.customPresets.filter((p) => p.id !== id),
        selectedPresetId: state.selectedPresetId === id ? 'flat' : state.selectedPresetId,
      }
    }

    case 'SET_PARAMETRIC_FILTER': {
      const filter = action.payload
      return {
        ...state,
        parametricFilters: state.parametricFilters.map((f) =>
          f.id === filter.id ? filter : f,
        ),
      }
    }

    case 'ADD_PARAMETRIC_FILTER': {
      if (state.parametricFilters.length >= 10) return state
      return {
        ...state,
        parametricFilters: [...state.parametricFilters, action.payload],
      }
    }

    case 'DELETE_PARAMETRIC_FILTER': {
      return {
        ...state,
        parametricFilters: state.parametricFilters.filter((f) => f.id !== action.payload),
      }
    }

    case 'SET_PREAMP': {
      return { ...state, preamp: Math.max(-12, Math.min(6, action.payload)) }
    }

    case 'SET_AUTO_HEADROOM': {
      return { ...state, autoHeadroom: action.payload }
    }

    case 'SET_LIMITER': {
      return { ...state, limiterEnabled: action.payload }
    }

    case 'SET_BALANCE': {
      return { ...state, balance: Math.max(-1, Math.min(1, action.payload)) }
    }

    case 'SET_MONO': {
      return { ...state, mono: action.payload }
    }

    case 'SET_BASS_BOOST': {
      const boost = Math.max(0, Math.min(100, action.payload))
      return { ...state, bassBoost: boost }
    }

    case 'SET_VISUALIZER_MODE': {
      return { ...state, visualizerMode: action.payload }
    }

    case 'SET_ERROR': {
      return { ...state, error: action.payload, status: action.payload ? 'error' : state.status }
    }

    case 'PREVIOUS': {
      if (state.queue.length === 0) return state

      // If playing past 3s, restart current track
      if (state.currentTime > 3.0) {
        return {
          ...state,
          currentTime: 0,
          status: 'buffering',
        }
      }

      // Otherwise go to previous track in queue
      let prevIdx = state.queueIndex - 1
      if (prevIdx < 0) {
        if (state.repeatMode === 'queue') {
          prevIdx = state.queue.length - 1
        } else {
          prevIdx = 0
        }
      }

      const prevTrack = state.queue[prevIdx] ?? null
      return {
        ...state,
        currentTrack: prevTrack,
        queueIndex: prevIdx,
        currentTime: 0,
        duration: prevTrack?.duration_ms ? prevTrack.duration_ms / 1000 : 0,
        status: 'buffering',
        history: prevTrack ? pushHistory(state.history, prevTrack) : state.history,
      }
    }

    case 'NEXT': {
      if (state.queue.length === 0) return state
      const isManual = action.payload?.manual ?? false

      // Check Stop After options on automatic track completion
      if (!isManual) {
        if (state.stopAfter === 'track') {
          return {
            ...state,
            status: 'paused',
            stopAfter: 'none',
          }
        }

        if (state.stopAfter === 'album' && state.currentTrack) {
          const nextCandidate = state.queue[state.queueIndex + 1]
          if (
            !nextCandidate ||
            nextCandidate.release_id !== state.currentTrack.release_id ||
            nextCandidate.album !== state.currentTrack.album
          ) {
            return {
              ...state,
              status: 'paused',
              stopAfter: 'none',
            }
          }
        }
      }

      // Handle Repeat Track mode (on auto next)
      if (!isManual && state.repeatMode === 'track' && state.currentTrack) {
        return {
          ...state,
          currentTime: 0,
          status: 'buffering',
        }
      }

      let nextIdx = state.queueIndex + 1
      if (nextIdx >= state.queue.length) {
        if (state.repeatMode === 'queue') {
          nextIdx = 0
        } else {
          return {
            ...state,
            status: 'paused',
            currentTime: 0,
          }
        }
      }

      const nextTrack = state.queue[nextIdx] ?? null
      return {
        ...state,
        currentTrack: nextTrack,
        queueIndex: nextIdx,
        currentTime: 0,
        duration: nextTrack?.duration_ms ? nextTrack.duration_ms / 1000 : 0,
        status: 'buffering',
        history: nextTrack ? pushHistory(state.history, nextTrack) : state.history,
      }
    }

    case 'RESTORE_STATE': {
      const restoredQueue = action.payload.queue ?? state.queue
      const restoredIndex = action.payload.queueIndex ?? state.queueIndex
      const restoredCurrentTrack =
        action.payload.currentTrack ??
        (restoredIndex >= 0 && restoredIndex < restoredQueue.length
          ? (restoredQueue[restoredIndex] ?? null)
          : null)

      return {
        ...state,
        ...action.payload,
        currentTrack: restoredCurrentTrack,
        status: 'paused', // NEVER autoplay on restored state
      }
    }

    default:
      return state
  }
}
