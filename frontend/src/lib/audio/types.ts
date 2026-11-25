import type { LibraryTrack } from '@/types/api'

export type PlaybackStatus = 'idle' | 'playing' | 'paused' | 'buffering' | 'error'

export type RepeatMode = 'off' | 'queue' | 'track'

export type EQMode = 'graphic' | 'parametric' | 'off'

export type VisualizerMode = 'spectrum' | 'waveform' | 'off'

export type ParametricFilterType = 'peaking' | 'lowshelf' | 'highshelf' | 'lowpass' | 'highpass'

export interface ParametricFilter {
  id: string
  enabled: boolean
  type: ParametricFilterType
  frequency: number // 20 - 20000 Hz
  gain: number // -18 to +18 dB
  q: number // 0.1 to 10
}

export interface EQPreset {
  id: string
  name: string
  values: number[] // 10 gain values in dB (-12 to +12)
  isCustom?: boolean
}

export type SleepTimerOption = 'off' | '15' | '30' | '45' | '60' | 'end_of_track' | 'end_of_album'

export type StopAfterOption = 'none' | 'track' | 'album'

export interface PlayerHistoryItem {
  track: LibraryTrack
  playedAt: string
}

/**
 * Where a queue entry came from. `context` is what was started (an album, a
 * playlist, a radio …), `user` what the user queued on top of it ("Als
 * Nächstes"), `autoplay` similar tracks appended when the queue ran out.
 * Entries carry it as `queueOrigin`; an entry without one is `context`, so
 * queues saved by older versions read as plain context.
 */
export type QueueOrigin = 'user' | 'context' | 'autoplay'

/** A queue entry: a library track, optionally marked with its origin. */
export type QueueTrack = LibraryTrack & { queueOrigin?: Exclude<QueueOrigin, 'context'> }

export type QueueContextKind =
  | 'album'
  | 'playlist'
  | 'mix'
  | 'artist'
  | 'favorites'
  | 'radio'
  | 'search'
  | 'library'
  | 'offline'
  | 'history'

/** What the queue was started from, shown as "Weiter aus: {title}". */
export interface QueueContext {
  kind: QueueContextKind
  id?: string
  title: string
}

export interface PlayerState {
  currentTrack: LibraryTrack | null
  queue: QueueTrack[]
  originalQueue: QueueTrack[]
  queueIndex: number
  /** What the context entries of the queue were started from; null when unknown. */
  queueContext: QueueContext | null
  /** Continue with similar tracks when the queue runs out. */
  autoplay: boolean
  status: PlaybackStatus
  currentTime: number
  duration: number
  volume: number // 0 to 1
  muted: boolean
  shuffle: boolean
  repeatMode: RepeatMode
  playbackRate: number // 0.5 to 2.0
  crossfadeSeconds: number // 0 to 12
  smartAlbumTransition: boolean
  sleepTimer: SleepTimerOption
  sleepTimerEndsAt: number | null // timestamp ms
  stopAfter: StopAfterOption

  // DSP & EQ
  eqEnabled: boolean
  eqMode: EQMode
  selectedPresetId: string
  graphicBands: number[] // 10 band gains in dB (-12 to +12)
  parametricFilters: ParametricFilter[]
  customPresets: EQPreset[]
  preamp: number // -12 to +6 dB
  autoHeadroom: boolean
  normalizationEnabled: boolean
  normalizationMessage: string
  limiterEnabled: boolean
  balance: number // -1 (L) to +1 (R)
  mono: boolean
  bassBoost: number // 0 to 100%

  // Visualizer & UI
  visualizerMode: VisualizerMode
  peakWarning: boolean
  history: PlayerHistoryItem[]
  error: string | null
}
