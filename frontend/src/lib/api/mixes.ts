import { request } from './client'
import type { LibraryTrack } from '@/types/api'

export type MixKind = 'genre' | 'on_repeat' | 'rediscover' | 'unheard' | 'new'

/** One automatic mix of the signed-in user; it changes once a day. */
export interface PersonalMix {
  id: string
  kind: MixKind
  title: string
  description: string
  genre?: string
  track_count: number
  /** Up to four tracks for a cover collage, different releases first. */
  cover_track_ids: string[]
  cover_release_ids: string[]
}

export interface PersonalMixDetail extends PersonalMix {
  tracks: LibraryTrack[]
}

export const personalMixes = (signal?: AbortSignal) =>
  request<PersonalMix[]>('/library/mixes', { signal })

export const personalMix = (id: string, signal?: AbortSignal) =>
  request<PersonalMixDetail>(`/library/mixes/${encodeURIComponent(id)}`, { signal })
