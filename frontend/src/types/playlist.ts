import type { LibraryTrack } from './api'

export interface Playlist {
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
