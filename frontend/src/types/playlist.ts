import type { LibraryTrack } from './api'

export interface Playlist {
  smart_rules?: SmartRules | null
  id: string
  user_id: string
  name: string
  description: string
  track_count: number
  duration_ms: number
  created_at: string
  updated_at: string
}

export interface PlaylistTrack extends LibraryTrack {
  position: number
  added_at: string
}

export interface PlaylistDetail extends Playlist {
  tracks: PlaylistTrack[]
}

export interface CreatePlaylistInput {
  smart_rules?: SmartRules | null
  name: string
  description?: string
}

export interface UpdatePlaylistInput {
  name: string
  description?: string
}

export interface ReorderTracksInput {
  track_ids: string[]
}

export interface SmartRules {
  genre?: string
  artist_id?: string
  favorites?: boolean
  added_days?: number
  sort: 'recent' | 'title' | 'frequent' | 'last_played'
  limit: number
}
