import { BUILTIN_PRESETS, DEFAULT_PARAMETRIC_FILTERS } from '@/lib/audio/eqPresets'
import type {
  EQMode,
  EQPreset,
  ParametricFilter,
  PlaybackStatus,
  PlayerHistoryItem,
  PlayerState,
  QueueContext,
  QueueOrigin,
  QueueTrack,
  RepeatMode,
  SleepTimerOption,
  StopAfterOption,
  VisualizerMode,
} from '@/lib/audio/types'
import type { LibraryTrack } from '@/types/api'

/** What CLEAR_QUEUE took away, so RESTORE_QUEUE can put it back. */
export interface QueueSnapshot {
  queue: QueueTrack[]
  originalQueue: QueueTrack[]
  queueIndex: number
  currentTrack: LibraryTrack | null
  queueContext: QueueContext | null
  shuffle: boolean
  currentTime: number
  status: PlaybackStatus
}

export type PlayerAction =
  | {type:'RESUME_SESSION';payload:{tracks:LibraryTrack[];index:number;position:number;repeatMode:RepeatMode}}
  | {
      type: 'PLAY_TRACK'
      payload: { track: LibraryTrack; queue?: LibraryTrack[]; queueIndex?: number; context?: QueueContext | null }
    }
  | { type: 'PLAY_ALBUM'; payload: { tracks: LibraryTrack[]; startIndex?: number; context?: QueueContext | null } }
  | {
      type: 'PLAY_ARTIST'
      // order is the queue with the shuffle already drawn (see playArtistAction).
      payload: { tracks: LibraryTrack[]; shuffle?: boolean; order?: LibraryTrack[]; context?: QueueContext | null }
    }
  /** Radio tracks replace the queue; a seed that is the current track keeps playing. */
  | { type: 'START_RADIO'; payload: { tracks: LibraryTrack[]; seed?: LibraryTrack; context: QueueContext } }
  /** Similar tracks appended at the end of the queue (autoplay). */
  | { type: 'APPEND_AUTOPLAY'; payload: { tracks: LibraryTrack[] } }
  | { type: 'RESTORE_QUEUE'; payload: QueueSnapshot }
  | { type: 'SET_AUTOPLAY'; payload: boolean }
  | { type: 'PLAY_NEXT'; payload: { track: LibraryTrack } }
  | { type: 'PLAY_QUEUE_INDEX'; payload: { index: number } }
  | { type: 'ADD_TO_QUEUE'; payload: { tracks: LibraryTrack[] | LibraryTrack } }
  | { type: 'REMOVE_FROM_QUEUE'; payload: { index: number } }
  | { type: 'REORDER_QUEUE'; payload: { fromIndex: number; toIndex: number } }
  | { type: 'CLEAR_QUEUE' }
  | { type: 'CLEAR_UPCOMING_QUEUE' }
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
  | { type: 'SET_NORMALIZATION'; payload: boolean }
  | { type: 'SET_NORMALIZATION_MESSAGE'; payload: string }
  | { type: 'SET_LIMITER'; payload: boolean }
  | { type: 'SET_BALANCE'; payload: number }
  | { type: 'SET_MONO'; payload: boolean }
  | { type: 'SET_BASS_BOOST'; payload: number }
  | { type: 'SET_VISUALIZER_MODE'; payload: VisualizerMode }
  | { type: 'SET_ERROR'; payload: string | null }
  | { type: 'PREVIOUS' }
  | { type: 'NEXT'; payload?: { manual?: boolean } }
  | { type: 'RESTORE_STATE'; payload: Partial<PlayerState> }
  | { type: 'RESET_SESSION' }

export const INITIAL_PLAYER_STATE: PlayerState = {
  currentTrack: null,
  queue: [],
  originalQueue: [],
  queueIndex: -1,
  queueContext: null,
  autoplay: true,
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
  normalizationEnabled: false,
  normalizationMessage: '',
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

/**
 * PLAY_ARTIST with its shuffle drawn up front. The reducer is then pure for
 * the action, so checking it in advance (whether it moves on to another
 * track, a skip) and dispatching it see the same first track.
 */
export function playArtistAction(
  tracks: LibraryTrack[],
  shuffle = false,
  context?: QueueContext | null,
): PlayerAction {
  return {
    type: 'PLAY_ARTIST',
    payload: {
      tracks,
      shuffle,
      order: shuffle ? shuffleArray(tracks) : undefined,
      ...(context !== undefined ? { context } : {}),
    },
  }
}

/* ------------------------------------------------------------------------ *
 * Queue origins
 *
 * The upcoming part of the queue (after the current track) is kept in three
 * runs: first what the user queued ("Als Nächstes", origin user), then the
 * rest of what was started (context), then similar tracks appended by
 * autoplay. Every action below keeps that order.
 * ------------------------------------------------------------------------ */

export function queueOrigin(track: QueueTrack | null | undefined): QueueOrigin {
  return track?.queueOrigin ?? 'context'
}

/** The entry with the given origin; context entries carry no mark at all. */
export function withOrigin(track: LibraryTrack, origin: QueueOrigin): QueueTrack {
  const marked = track as QueueTrack
  if (origin === 'context') {
    if (!marked.queueOrigin) return marked
    const { queueOrigin: _origin, ...plain } = marked
    return plain
  }
  return marked.queueOrigin === origin ? marked : { ...track, queueOrigin: origin }
}

const asContext = (track: LibraryTrack) => withOrigin(track, 'context')

/**
 * Where "Zur Warteschlange" inserts: after the last user entry that directly
 * follows the current track, so queued tracks play before the rest of the
 * album or playlist, in the order they were queued.
 */
export function userInsertIndex(queue: QueueTrack[], queueIndex: number): number {
  let at = Math.max(0, queueIndex + 1)
  while (at < queue.length && queueOrigin(queue[at]) === 'user') at++
  return at
}

/**
 * Moves one entry and gives it the origin of the place it lands in. An entry
 * dropped into the upcoming part takes the origin of the entry it now follows;
 * directly after the current track that is "Als Nächstes" (user). Moving the
 * current track, or an entry into the already played part, keeps its origin.
 * Pure, so the queue list can preview a drag with the same rule.
 */
export function moveQueueEntry(
  queue: QueueTrack[],
  queueIndex: number,
  fromIndex: number,
  toIndex: number,
): { queue: QueueTrack[]; queueIndex: number } {
  const item = queue[fromIndex]
  if (!item || toIndex < 0 || toIndex >= queue.length || fromIndex === toIndex) return { queue, queueIndex }
  const next = [...queue]
  next.splice(fromIndex, 1)
  next.splice(toIndex, 0, item)

  let index = queueIndex
  if (fromIndex === queueIndex) index = toIndex
  else if (fromIndex < queueIndex && toIndex >= queueIndex) index--
  else if (fromIndex > queueIndex && toIndex <= queueIndex) index++

  if (fromIndex !== queueIndex && toIndex > index) {
    const origin = toIndex - 1 === index ? 'user' : queueOrigin(next[toIndex - 1])
    next[toIndex] = withOrigin(item, origin)
  }
  return { queue: next, queueIndex: index }
}

/**
 * The context entries of `queue` in the unshuffled order remembered in
 * `original`. Entries that are no longer context are left out, context
 * entries the order does not know (moved in while shuffled) follow at the end.
 */
function unshuffledContext(original: QueueTrack[], queue: QueueTrack[]): QueueTrack[] {
  const pending = queue.filter((t) => queueOrigin(t) === 'context')
  const ordered: QueueTrack[] = []
  for (const track of original) {
    const at = pending.findIndex((t) => t.id === track.id)
    if (at !== -1) ordered.push(...pending.splice(at, 1))
  }
  return [...ordered, ...pending]
}

/** Where a radio starts from: a track (it plays first) or a genre. */
export interface RadioSource {
  seed?: LibraryTrack
  genre?: string
}

/** The context a radio queue is shown under: "Radio: {Titel|Genre}". */
export function radioContext({ seed, genre }: RadioSource): QueueContext {
  const name = seed?.title ?? (genre?.trim() || 'Alle Genres')
  const id = seed?.id ?? (genre?.trim() || undefined)
  return { kind: 'radio', ...(id ? { id } : {}), title: `Radio: ${name}` }
}

/** How many tracks may be left before autoplay looks for similar ones. */
export const AUTOPLAY_LOOKAHEAD = 2

/** Autoplay seeds with the last track the user chose (context or queued). */
export function autoplaySeed(queue: QueueTrack[]): QueueTrack | undefined {
  for (let i = queue.length - 1; i >= 0; i--) if (queueOrigin(queue[i]) !== 'autoplay') return queue[i]
  return queue.at(-1)
}

/**
 * Whether the player should look for similar tracks now: the queue is about
 * to run out and nothing the user set ends playback there on purpose (repeat,
 * stop after, sleep timer), and the track is not an offline copy.
 */
export function wantsAutoplay(state: PlayerState, online = true): boolean {
  if (!state.autoplay || !online) return false
  if (state.repeatMode !== 'off' || state.stopAfter !== 'none' || state.sleepTimer !== 'off') return false
  if (!state.currentTrack || state.currentTrack.offline_blob_key) return false
  if (state.status !== 'playing' && state.status !== 'buffering') return false
  if (state.queueIndex < 0 || state.queue.length === 0) return false
  return state.queue.length - 1 - state.queueIndex <= AUTOPLAY_LOOKAHEAD
}

export function shuffleArray<T>(array: T[]): T[] {
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

function trackDurationSeconds(track: LibraryTrack | null | undefined): number {
  if (
    !track ||
    typeof track.duration_ms !== 'number' ||
    !Number.isFinite(track.duration_ms) ||
    track.duration_ms <= 0
  ) {
    return 0
  }
  return track.duration_ms / 1000
}

export function playerReducer(state: PlayerState, action: PlayerAction): PlayerState {
  switch (action.type) {
    case 'RESUME_SESSION': {
      const { tracks, index, position, repeatMode } = action.payload
      if (
        !tracks.length ||
        index < 0 ||
        index >= tracks.length ||
        !Number.isInteger(index) ||
        !Number.isFinite(position)
      )
        return state
      const queue = tracks.map(asContext)
      const track = queue[index]!,
        duration = trackDurationSeconds(track)
      return {
        ...state,
        currentTrack: track,
        queue,
        originalQueue: [...queue],
        queueIndex: index,
        queueContext: null,
        shuffle: false,
        repeatMode,
        status: 'buffering',
        currentTime: Math.max(
          0,
          duration
            ? Math.min(position, Math.max(0, duration - 0.25))
            : position,
        ),
        duration,
        sleepTimer: 'off',
        sleepTimerEndsAt: null,
        stopAfter: 'none',
        error: null,
        history: pushHistory(state.history, track),
      }
    }
    case 'PLAY_TRACK': {
      const { queue, queueIndex, context } = action.payload
      const track = asContext(action.payload.track)
      const passed = Boolean(queue && queue.length > 0)
      const newQueue = passed ? queue!.map(asContext) : [track]
      let idx = queueIndex ?? newQueue.findIndex((t) => t.id === track.id)
      if (idx === -1) {
        newQueue.push(track)
        idx = newQueue.length - 1
      }
      return {
        ...state,
        currentTrack: track,
        queue: newQueue,
        // A queue passed along is a new listening context. The unshuffled
        // order remembered for the previous one must not survive it, or
        // turning shuffle off would bring the old queue back. The list
        // plays in the order given, so shuffle is off for it.
        originalQueue:
          state.shuffle && state.originalQueue.length > 0 && !passed
            ? state.originalQueue
            : newQueue,
        shuffle: passed ? false : state.shuffle,
        queueContext: context ?? null,
        queueIndex: idx,
        status: 'buffering',
        currentTime: 0,
        duration: trackDurationSeconds(track),
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_ALBUM': {
      const { startIndex = 0, context } = action.payload
      if (!action.payload.tracks.length) return state
      const tracks = action.payload.tracks.map(asContext)
      const startIdx = Math.max(0, Math.min(startIndex, tracks.length - 1))
      const track = tracks[startIdx]!
      return {
        ...state,
        currentTrack: track,
        queue: tracks,
        originalQueue: [...tracks],
        queueIndex: startIdx,
        queueContext: context ?? null,
        shuffle: false,
        status: 'buffering',
        currentTime: 0,
        duration: trackDurationSeconds(track),
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_ARTIST': {
      const { shuffle = false, order, context } = action.payload
      if (!action.payload.tracks.length) return state
      const tracks = action.payload.tracks.map(asContext)
      const originalQueue = [...tracks]
      const finalQueue =
        order?.length === tracks.length ? order.map(asContext) : shuffle ? shuffleArray(tracks) : [...tracks]
      const track = finalQueue[0]!
      return {
        ...state,
        currentTrack: track,
        queue: finalQueue,
        originalQueue,
        queueIndex: 0,
        queueContext: context ?? null,
        shuffle,
        status: 'buffering',
        currentTime: 0,
        duration: trackDurationSeconds(track),
        history: pushHistory(state.history, track),
        error: null,
      }
    }

    case 'PLAY_NEXT': {
      if (!state.currentTrack || state.queue.length === 0) {
        return playerReducer(state, { type: 'PLAY_TRACK', payload: { track: action.payload.track } })
      }
      if (state.currentTrack.id === action.payload.track.id) {
        return state
      }
      // Directly after the current track, ahead of anything queued before.
      const track = withOrigin(action.payload.track, 'user')
      let currentIdx = state.queueIndex
      const existingIdx = state.queue.findIndex((t) => t.id === track.id)
      const newQueue = [...state.queue]

      if (existingIdx !== -1) {
        newQueue.splice(existingIdx, 1)
        if (existingIdx < currentIdx) {
          currentIdx--
        }
      }

      const insertAt = currentIdx + 1
      newQueue.splice(insertAt, 0, track)

      // While shuffled, originalQueue is the unshuffled order of the context;
      // a queued track is no part of it any more.
      return {
        ...state,
        queue: newQueue,
        originalQueue: state.shuffle ? state.originalQueue.filter((t) => t.id !== track.id) : newQueue,
        queueIndex: currentIdx,
      }
    }

    case 'PLAY_QUEUE_INDEX': {
      const { index } = action.payload
      if (index < 0 || index >= state.queue.length) return state
      const track = state.queue[index]!
      return {
        ...state,
        currentTrack: track,
        queueIndex: index,
        status: 'buffering',
        currentTime: 0,
        duration: trackDurationSeconds(track),
        history: pushHistory(state.history, track),
        error: null,
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

      const at = userInsertIndex(state.queue, state.queueIndex)
      const newQueue = [...state.queue]
      newQueue.splice(at, 0, ...tracksToAdd.map((t) => withOrigin(t, 'user')))
      return {
        ...state,
        queue: newQueue,
        originalQueue: state.shuffle ? state.originalQueue : newQueue,
      }
    }

    case 'APPEND_AUTOPLAY': {
      // Only onto a queue that would otherwise end; repeat never ends.
      if (!state.currentTrack || state.queue.length === 0 || state.repeatMode !== 'off') return state
      const known = new Set(state.queue.map((t) => t.id))
      const added: QueueTrack[] = []
      for (const track of action.payload.tracks) {
        if (known.has(track.id)) continue
        known.add(track.id)
        added.push(withOrigin(track, 'autoplay'))
      }
      if (added.length === 0) return state
      const newQueue = [...state.queue, ...added]
      return {
        ...state,
        queue: newQueue,
        originalQueue: state.shuffle ? state.originalQueue : newQueue,
      }
    }

    case 'START_RADIO': {
      const { seed, context } = action.payload
      const rest = action.payload.tracks.filter((t) => t.id !== seed?.id).map(asContext)
      const current = state.currentTrack
      // Started from the track that plays: it carries on where it is.
      if (seed && current && current.id === seed.id && current.offline_blob_key === seed.offline_blob_key) {
        const head = asContext(current)
        const queue = [head, ...rest]
        return {
          ...state,
          currentTrack: head,
          queue,
          originalQueue: [...queue],
          queueIndex: 0,
          queueContext: context,
          shuffle: false,
          error: null,
        }
      }
      const tracks = seed ? [asContext(seed), ...rest] : rest
      if (tracks.length === 0) return state
      return playerReducer(state, { type: 'PLAY_ALBUM', payload: { tracks, startIndex: 0, context } })
    }

    case 'RESTORE_QUEUE': {
      // Undo of CLEAR_QUEUE: only while nothing else was started since.
      const snapshot = action.payload
      if (state.queue.length > 0 || snapshot.queue.length === 0) return state
      const index = Math.max(0, Math.min(snapshot.queueIndex, snapshot.queue.length - 1))
      const track = snapshot.currentTrack ?? snapshot.queue[index] ?? null
      const wasPlaying = snapshot.status === 'playing' || snapshot.status === 'buffering'
      return {
        ...state,
        queue: snapshot.queue,
        originalQueue: snapshot.originalQueue,
        queueIndex: index,
        currentTrack: track,
        queueContext: snapshot.queueContext,
        shuffle: snapshot.shuffle,
        currentTime: Math.max(0, snapshot.currentTime),
        duration: trackDurationSeconds(track),
        status: wasPlaying ? 'buffering' : 'paused',
        error: null,
      }
    }

    case 'SET_AUTOPLAY':
      return { ...state, autoplay: action.payload }

    case 'REMOVE_FROM_QUEUE': {
      const { index } = action.payload
      if (index < 0 || index >= state.queue.length) return state

      const removedTrack = state.queue[index]
      const newQueue = state.queue.filter((_, i) => i !== index)
      if (newQueue.length === 0) {
        return {
          ...state,
          currentTrack: null,
          queue: [],
          originalQueue: [],
          queueIndex: -1,
          queueContext: null,
          status: 'idle',
          currentTime: 0,
          duration: 0,
        }
      }

      let newIndex = state.queueIndex
      let newCurrentTrack = state.currentTrack
      let newStatus = state.status
      let newCurrentTime = state.currentTime
      let newDuration = state.duration

      if (index < state.queueIndex) {
        newIndex--
      } else if (index === state.queueIndex) {
        if (newIndex >= newQueue.length) {
          newIndex = newQueue.length - 1
        }
        newCurrentTrack = newQueue[newIndex] ?? null
        newStatus = 'buffering'
        newCurrentTime = 0
        newDuration = trackDurationSeconds(newCurrentTrack)
      }

      let newOriginal = [...state.originalQueue]
      if (removedTrack) {
        const origIdx = newOriginal.findIndex((t) => t.id === removedTrack.id)
        if (origIdx !== -1) {
          newOriginal.splice(origIdx, 1)
        }
      }

      return {
        ...state,
        queue: newQueue,
        originalQueue: state.shuffle ? newOriginal : newQueue,
        queueIndex: newIndex,
        currentTrack: newCurrentTrack,
        status: newStatus,
        currentTime: newCurrentTime,
        duration: newDuration,
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

      const moved = moveQueueEntry(state.queue, state.queueIndex, fromIndex, toIndex)
      return {
        ...state,
        queue: moved.queue,
        originalQueue: state.shuffle ? state.originalQueue : moved.queue,
        queueIndex: moved.queueIndex,
      }
    }

    case 'CLEAR_QUEUE': {
      return {
        ...state,
        currentTrack: null,
        queue: [],
        originalQueue: [],
        queueIndex: -1,
        queueContext: null,
        status: 'idle',
        currentTime: 0,
        duration: 0,
      }
    }

    case 'CLEAR_UPCOMING_QUEUE': {
      if (
        !state.currentTrack ||
        state.queueIndex < 0 ||
        state.queueIndex >= state.queue.length - 1
      ) {
        return state
      }
      const removedTracks = state.queue.slice(state.queueIndex + 1)
      const newQueue = state.queue.slice(0, state.queueIndex + 1)
      let newOriginal = [...state.originalQueue]
      for (const rt of removedTracks) {
        const idx = newOriginal.findIndex((t) => t.id === rt.id)
        if (idx !== -1) {
          newOriginal.splice(idx, 1)
        }
      }

      return {
        ...state,
        queue: newQueue,
        originalQueue: state.shuffle ? newOriginal : newQueue,
      }
    }

    case 'SET_STATUS': {
      return { ...state, status: action.payload }
    }

    case 'SET_CURRENT_TIME': {
      const { currentTime, duration } = action.payload
      const isPositiveDuration = duration !== undefined && Number.isFinite(duration) && duration > 0
      const currentTrackDuration = trackDurationSeconds(state.currentTrack)

      return {
        ...state,
        currentTime: Number.isFinite(currentTime) && currentTime >= 0 ? currentTime : 0,
        duration: isPositiveDuration
          ? duration
          : state.duration > 0
            ? state.duration
            : currentTrackDuration,
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

      // Shuffle reorders the context only: the current track leads, what the
      // user queued stays next in its order, autoplay stays at the end.
      // originalQueue remembers the unshuffled context for turning it off.
      const index = state.queueIndex
      const current = state.queue[index]
      const upcoming = current ? state.queue.slice(index + 1) : []
      const user = upcoming.filter((t) => queueOrigin(t) === 'user')
      const auto = upcoming.filter((t) => queueOrigin(t) === 'autoplay')

      if (shuffle) {
        const original = state.queue.filter((t) => queueOrigin(t) === 'context')
        if (!state.currentTrack || !current || state.queue.length <= 1) {
          return { ...state, shuffle: true, originalQueue: original }
        }
        const pool = state.queue.filter((t, i) => i !== index && queueOrigin(t) === 'context')
        return {
          ...state,
          shuffle: true,
          originalQueue: original,
          queue: [current, ...user, ...shuffleArray(pool), ...auto],
          queueIndex: 0,
        }
      }

      const ordered = unshuffledContext(state.originalQueue, state.queue)
      if (!state.currentTrack || !current) {
        const queue = [...ordered, ...state.queue.filter((t) => queueOrigin(t) !== 'context')]
        return { ...state, shuffle: false, queue, originalQueue: queue }
      }
      // The current track goes back to its place in the context; a queued
      // one plays on after the context track that came before it.
      let pivot = ordered.indexOf(current)
      const isContext = pivot !== -1
      if (!isContext) {
        const before = state.queue
          .slice(0, index)
          .reverse()
          .find((t) => queueOrigin(t) === 'context')
        pivot = before ? ordered.indexOf(before) : -1
      }
      const head = isContext ? ordered.slice(0, pivot) : ordered.slice(0, pivot + 1)
      const tail = ordered.slice(pivot + 1)
      const queue = [...head, current, ...user, ...tail, ...auto]
      return {
        ...state,
        shuffle: false,
        queue,
        originalQueue: queue,
        queueIndex: head.length,
      }
    }

    case 'SET_REPEAT_MODE': {
      return { ...state, repeatMode: action.payload }
    }

    case 'SET_PLAYBACK_RATE': {
      return { ...state, playbackRate: action.payload }
    }

    case 'SET_CROSSFADE': {
      return {
        ...state,
        crossfadeSeconds: Number.isFinite(action.payload)
          ? Math.max(0, Math.min(12, action.payload))
          : 0,
      }
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
        parametricFilters: state.parametricFilters.map((f) => (f.id === filter.id ? filter : f)),
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

    case 'SET_NORMALIZATION':
      return { ...state, normalizationEnabled: action.payload }
    case 'SET_NORMALIZATION_MESSAGE':
      return { ...state, normalizationMessage: action.payload }
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
        duration: trackDurationSeconds(prevTrack),
        status: 'buffering',
        history: prevTrack ? pushHistory(state.history, prevTrack) : state.history,
      }
    }

    case 'NEXT': {
      if (state.queue.length === 0) return state
      const isManual = action.payload?.manual ?? false

      // Check Stop After options on automatic track completion
      if (!isManual) {
        if (state.sleepTimer === 'end_of_track') {
          return { ...state, status: 'paused', sleepTimer: 'off', sleepTimerEndsAt: null }
        }
        if (state.sleepTimer === 'end_of_album' && state.currentTrack) {
          const nextCandidate = state.queue[state.queueIndex + 1]
          if (
            !nextCandidate ||
            nextCandidate.release_id !== state.currentTrack.release_id ||
            nextCandidate.album !== state.currentTrack.album
          ) {
            return { ...state, status: 'paused', sleepTimer: 'off', sleepTimerEndsAt: null }
          }
        }
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
        duration: trackDurationSeconds(nextTrack),
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

      const restoredDuration = trackDurationSeconds(restoredCurrentTrack)

      return {
        ...state,
        ...action.payload,
        currentTrack: restoredCurrentTrack,
        duration: restoredDuration,
        status: 'paused', // NEVER autoplay on restored state
      }
    }

    // The signed-in user changed. Nothing of the previous session may carry
    // over: RESTORE_STATE keeps whatever the new user's saved state omits.
    case 'RESET_SESSION':
      return { ...INITIAL_PLAYER_STATE }

    default:
      return state
  }
}
