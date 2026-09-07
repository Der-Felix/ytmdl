import { request, requestVoid } from '@/lib/api/client'
import type { LibraryTrack } from '@/types/api'
import type {
  CreatePlaylistInput,
  Playlist,
  PlaylistDetail,
  UpdatePlaylistInput,
} from '@/types/playlist'

export async function listPlaylists(signal?: AbortSignal): Promise<Playlist[]> {
  return request<Playlist[]>('/playlists', { signal })
}

export async function createPlaylist(input: CreatePlaylistInput): Promise<Playlist> {
  return request<Playlist>('/playlists', {
    method: 'POST',
    body: input,
  })
}

export async function getPlaylist(id: string, signal?: AbortSignal): Promise<PlaylistDetail> {
  return request<PlaylistDetail>(`/playlists/${encodeURIComponent(id)}`, { signal })
}

export async function updatePlaylist(
  id: string,
  input: UpdatePlaylistInput,
): Promise<Playlist> {
  return request<Playlist>(`/playlists/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: input,
  })
}

export async function deletePlaylist(id: string): Promise<void> {
  return requestVoid(`/playlists/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

export async function addPlaylistTrack(
  playlistId: string,
  trackId: string,
): Promise<PlaylistDetail> {
  return request<PlaylistDetail>(`/playlists/${encodeURIComponent(playlistId)}/tracks`, {
    method: 'POST',
    body: { track_id: trackId },
  })
}

export const addTrackToPlaylist = addPlaylistTrack

export async function removePlaylistTrack(
  playlistId: string,
  trackId: string,
): Promise<PlaylistDetail> {
  return request<PlaylistDetail>(
    `/playlists/${encodeURIComponent(playlistId)}/tracks/${encodeURIComponent(trackId)}`,
    {
      method: 'DELETE',
    },
  )
}

export const removeTrackFromPlaylist = removePlaylistTrack

export async function reorderPlaylistTracks(
  playlistId: string,
  trackIds: string[],
): Promise<PlaylistDetail> {
  return request<PlaylistDetail>(`/playlists/${encodeURIComponent(playlistId)}/tracks/reorder`, {
    method: 'PUT',
    body: { track_ids: trackIds },
  })
}

export async function listFavorites(signal?: AbortSignal): Promise<LibraryTrack[]> {
  return request<LibraryTrack[]>('/favorites', { signal })
}

export async function listFavoriteIDs(signal?: AbortSignal): Promise<string[]> {
  return request<string[]>('/favorites/ids', { signal })
}

export async function favoriteTrack(trackId: string): Promise<void> {
  return requestVoid(`/favorites/${encodeURIComponent(trackId)}`, {
    method: 'PUT',
  })
}

export async function unfavoriteTrack(trackId: string): Promise<void> {
  return requestVoid(`/favorites/${encodeURIComponent(trackId)}`, {
    method: 'DELETE',
  })
}
