import type {
  EQPreset,
  ParametricFilter,
  PlayerHistoryItem,
  PlayerState,
  QueueContext,
  QueueContextKind,
  QueueTrack,
  RepeatMode,
} from './types'

const CURRENT_SCHEMA_VERSION = 2
const KEY_PREFIX_V2 = 'ytmdl.player.v2'
const KEY_PREFIX_V1 = 'ytmdl.player'

export interface PersistedPlayerState {
  schemaVersion?: number
  queue: PlayerState['queue']
  originalQueue: PlayerState['originalQueue']
  queueIndex: number
  queueContext: PlayerState['queueContext']
  autoplay: boolean
  currentTime: number
  volume: number
  muted: boolean
  shuffle: boolean
  repeatMode: RepeatMode
  playbackRate: number
  crossfadeSeconds: number
  smartAlbumTransition: boolean
  eqEnabled: boolean
  eqMode: PlayerState['eqMode']
  selectedPresetId: string
  graphicBands: number[]
  parametricFilters: ParametricFilter[]
  preamp: number
  autoHeadroom: boolean
  normalizationEnabled: boolean
  limiterEnabled: boolean
  balance: number
  mono: boolean
  bassBoost: number
  visualizerMode: PlayerState['visualizerMode']
}

function userKey(
  userId: string | null | undefined,
  suffix: string,
  prefix = KEY_PREFIX_V2,
): string {
  const uid = userId && userId.trim() ? userId.trim() : 'default'
  return `${prefix}.${uid}.${suffix}`
}

export function savePlayerState(
  userId: string | null | undefined,
  state: Partial<PersistedPlayerState>,
): void {
  try {
    const key = userKey(userId, 'state')
    const existing = loadPlayerState(userId) || {}
    const merged: PersistedPlayerState = {
      ...existing,
      ...state,
      schemaVersion: CURRENT_SCHEMA_VERSION,
    } as PersistedPlayerState
    localStorage.setItem(key, JSON.stringify(merged))
  } catch {
    // LocalStorage quota or access denied - ignore safely
  }
}

export function loadPlayerState(
  userId: string | null | undefined,
): Partial<PersistedPlayerState> | null {
  try {
    const keyV2 = userKey(userId, 'state', KEY_PREFIX_V2)
    const rawV2 = localStorage.getItem(keyV2)
    if (rawV2) {
      const parsed = JSON.parse(rawV2) as Partial<PersistedPlayerState>
      return sanitizePersistedState(parsed)
    }

    // Attempt migration from v1
    const keyV1 = userKey(userId, 'state', KEY_PREFIX_V1)
    const rawV1 = localStorage.getItem(keyV1)
    if (rawV1) {
      const parsed = JSON.parse(rawV1) as Partial<PersistedPlayerState>
      const sanitized = sanitizePersistedState(parsed)
      // Save forward to v2
      savePlayerState(userId, sanitized)
      return sanitized
    }

    return null
  } catch {
    return null
  }
}

function sanitizePersistedState(
  state: Partial<PersistedPlayerState>,
): Partial<PersistedPlayerState> {
  const out = { ...state }
  if (out.normalizationEnabled !== undefined)
    out.normalizationEnabled = out.normalizationEnabled === true
  if (out.crossfadeSeconds !== undefined) {
    out.crossfadeSeconds =
      typeof out.crossfadeSeconds === 'number' && Number.isFinite(out.crossfadeSeconds)
        ? Math.max(0, Math.min(12, out.crossfadeSeconds))
        : 0
  }

  // Sanitize queue: verify each track has valid ID and title. Queues saved
  // before entries carried an origin read as context (no mark); an unknown
  // mark is dropped the same way.
  if (Array.isArray(out.queue)) {
    out.queue = out.queue.filter(isStoredTrack).map(withKnownOrigin)
  }
  if (Array.isArray(out.originalQueue)) {
    out.originalQueue = out.originalQueue.filter(isStoredTrack).map(withKnownOrigin)
  }
  if (out.queueContext !== undefined) out.queueContext = sanitizeQueueContext(out.queueContext)
  if (out.autoplay !== undefined) out.autoplay = out.autoplay !== false

  // Ensure preamp is within [-12, +6]
  if (typeof out.preamp === 'number') {
    out.preamp = Math.max(-12, Math.min(6, out.preamp))
  }

  return out
}

function isStoredTrack(t: QueueTrack | null | undefined): t is QueueTrack {
  return Boolean(t && typeof t.id === 'string' && t.id.trim() !== '' && typeof t.title === 'string')
}

function withKnownOrigin(track: QueueTrack): QueueTrack {
  const origin = (track as { queueOrigin?: unknown }).queueOrigin
  if (origin === undefined || origin === 'user' || origin === 'autoplay') return track
  const { queueOrigin: _origin, ...plain } = track
  return plain
}

const CONTEXT_KINDS: ReadonlySet<string> = new Set<QueueContextKind>([
  'album', 'playlist', 'mix', 'artist', 'favorites', 'radio', 'search', 'library', 'offline', 'history',
])

function sanitizeQueueContext(value: unknown): QueueContext | null {
  if (!value || typeof value !== 'object') return null
  const { kind, id, title } = value as Record<string, unknown>
  if (typeof kind !== 'string' || !CONTEXT_KINDS.has(kind) || typeof title !== 'string' || !title.trim()) return null
  return {
    kind: kind as QueueContextKind,
    title: title.slice(0, 200),
    ...(typeof id === 'string' && id ? { id } : {}),
  }
}

export function saveCustomPresets(presets: EQPreset[]): void {
  try {
    localStorage.setItem(`${KEY_PREFIX_V2}.custom_presets`, JSON.stringify(presets))
  } catch {
    // Ignore
  }
}

export function loadCustomPresets(): EQPreset[] {
  try {
    const raw =
      localStorage.getItem(`${KEY_PREFIX_V2}.custom_presets`) ||
      localStorage.getItem(`${KEY_PREFIX_V1}.custom_presets`)
    if (!raw) return []
    return JSON.parse(raw) as EQPreset[]
  } catch {
    return []
  }
}

export function saveHistory(userId: string | null | undefined, history: PlayerHistoryItem[]): void {
  try {
    const key = userKey(userId, 'history')
    // Keep max 100 history items
    const truncated = history.slice(0, 100)
    localStorage.setItem(key, JSON.stringify(truncated))
  } catch {
    // Ignore
  }
}

export function loadHistory(userId: string | null | undefined): PlayerHistoryItem[] {
  try {
    const key = userKey(userId, 'history')
    const raw =
      localStorage.getItem(key) || localStorage.getItem(userKey(userId, 'history', KEY_PREFIX_V1))
    if (!raw) return []
    return JSON.parse(raw) as PlayerHistoryItem[]
  } catch {
    return []
  }
}
