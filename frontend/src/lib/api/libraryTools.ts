import { request, requestVoid } from './client'
import type { LibraryTrack } from '@/types/api'
import type { PlaylistDetail, SmartRules } from '@/types/playlist'
export interface HistoryTrack extends LibraryTrack {
  play_count: number
  last_played_at: string
}
export interface DuplicateGroup {
  key: string
  count: number
  tracks: LibraryTrack[]
  fingerprint: string
  outcome: '' | 'preferred' | 'distinct'
  preferred_track_id: string
}
export interface DuplicateReview {
  group_key: string
  fingerprint: string
  outcome: 'preferred' | 'distinct'
  preferred_track_id: string
}
export interface DuplicateRemoval {
  deleted_track_ids: string[]
  failed_track_id?: string
  error_code?: string
  message?: string
}
export interface MetadataPatch {
  album?: string
  album_artist?: string
  artists?: string[]
  year?: number
}
export const listeningHistory = (sort = 'recent', signal?: AbortSignal) =>
  request<HistoryTrack[]>('/history', { query: { sort }, signal })
export const recordPlayback = (track_id: string, event_id: string) =>
  requestVoid('/history', { method: 'POST', body: { track_id, event_id } })
export const clearListeningHistory = () =>
  requestVoid('/history', { method: 'DELETE' })
export const duplicateGroups = (
  offset = 0,
  signal?: AbortSignal,
  options: { includeReviewed?: boolean; after?: string } = {},
) =>
  request<DuplicateGroup[]>('/library/duplicates', {
    query: {
      offset,
      review: options.includeReviewed === false ? 'open' : 'all',
      after: options.after,
    },
    signal,
  })
export const saveDuplicateReview = (review: DuplicateReview) =>
  requestVoid('/library/duplicates/review', { method: 'POST', body: review })
export const resetDuplicateReview = (key: string) =>
  requestVoid(`/library/duplicates/review/${encodeURIComponent(key)}`, {
    method: 'DELETE',
  })
export const removeDuplicateVersions = (
  review: DuplicateReview,
  remove_track_ids: string[],
) =>
  request<DuplicateRemoval>('/library/duplicates/remove', {
    method: 'POST',
    body: { ...review, remove_track_ids, confirmed: true },
  })
export const updateSelectedMetadata = (
  track_ids: string[],
  patch: MetadataPatch,
) =>
  requestVoid('/library/tracks/metadata', {
    method: 'PATCH',
    body: { track_ids, patch },
  })
export const addPlaylistTracks = (id: string, track_ids: string[]) =>
  request<PlaylistDetail>(`/playlists/${encodeURIComponent(id)}/tracks/bulk`, {
    method: 'POST',
    body: { track_ids },
  })
export const setPlaylistRules = (id: string, smart_rules: SmartRules | null) =>
  requestVoid(`/playlists/${encodeURIComponent(id)}/rules`, {
    method: 'PUT',
    body: { smart_rules },
  })
export async function uploadArtwork(
  kind: 'artists' | 'releases',
  id: string,
  image: File,
) {
  const body = new FormData()
  body.append('image', image)
  return requestVoid(`/library/${kind}/${encodeURIComponent(id)}/artwork`, {
    method: 'PUT',
    body,
  })
}
export const deleteArtwork = (kind: 'artists' | 'releases', id: string) =>
  requestVoid(`/library/${kind}/${encodeURIComponent(id)}/artwork`, {
    method: 'DELETE',
  })
export const analyzeLoudness = (id: string, signal?: AbortSignal) =>
  request<{ gain_db: number; integrated_lufs: number; true_peak_db: number }>(
    `/library/tracks/${encodeURIComponent(id)}/loudness`,
    { method: 'POST', signal },
  )
export interface TrashEntry {
  id: string
  track_id: string
  title: string
  created_at: string
  expires_at: string
  state: 'preparing' | 'ready' | 'restoring' | 'purging'
}
export const libraryTrash = (offset = 0, signal?: AbortSignal) =>
  request<TrashEntry[]>('/library/trash', { query: { offset }, signal })
export const restoreTrash = (id: string) =>
  requestVoid(`/library/trash/${encodeURIComponent(id)}/restore`, {
    method: 'POST',
  })
export const purgeTrash = (id: string) =>
  requestVoid(`/library/trash/${encodeURIComponent(id)}/purge`, {
    method: 'POST',
    body: { confirmed: true },
  })
export const recoverTrash = () =>
  requestVoid('/library/trash/recover', { method: 'POST' })
