import type { LibraryTrack } from '@/types/api'

export type PlaybackStatus = 'idle' | 'playing' | 'paused' | 'buffering' | 'error'

export type RepeatMode = 'off' | 'queue' | 'track'

export type EQMode = 'graphic' | 'parametric' | 'off'

export type VisualizerMode = 'spectrum' | 'waveform' | 'off'

export type ParametricFilterType =
  | 'peaking'
  | 'lowshelf'
  | 'highshelf'
  | 'lowpass'
  | 'highpass'

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

export type SleepTimerOption =
  | 'off'
  | '15'
  | '30'
  | '45'
  | '60'
  | 'end_of_track'
  | 'end_of_album'

export type StopAfterOption = 'none' | 'track' | 'album'

export interface PlayerHistoryItem {
  track: LibraryTrack
  playedAt: string
}

export interface PlayerState {
  currentTrack: LibraryTrack | null
  queue: LibraryTrack[]
  originalQueue: LibraryTrack[]
  queueIndex: number
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
