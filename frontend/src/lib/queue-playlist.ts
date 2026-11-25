import { addPlaylistTracks } from '@/lib/api/libraryTools'
import { createPlaylist } from '@/lib/api/playlists'
import type { Playlist } from '@/types/playlist'

/** The bulk endpoint takes 1–100 titles per request (playlists_tools.go). */
export const PLAYLIST_BULK_LIMIT = 100

/**
 * Creates a playlist and fills it with the given titles in order, in requests
 * of at most PLAYLIST_BULK_LIMIT. When adding fails after the playlist was
 * created, the error says so: the playlist exists with the titles added so far.
 */
export async function saveQueueAsPlaylist(
  name: string,
  trackIds: string[],
  api: {
    create: (input: { name: string }) => Promise<Playlist>
    add: (id: string, trackIds: string[]) => Promise<unknown>
  } = { create: createPlaylist, add: addPlaylistTracks },
): Promise<Playlist> {
  const ids = [...new Set(trackIds)]
  const playlist = await api.create({ name })
  let added = 0
  try {
    for (let start = 0; start < ids.length; start += PLAYLIST_BULK_LIMIT) {
      const chunk = ids.slice(start, start + PLAYLIST_BULK_LIMIT)
      await api.add(playlist.id, chunk)
      added += chunk.length
    }
  } catch (error) {
    const reason = error instanceof Error && error.message ? ` ${error.message}` : ''
    throw new Error(
      `Die Playlist „${playlist.name}“ wurde angelegt, aber nur ${added} von ${ids.length} Titeln hinzugefügt.${reason}`,
      { cause: error },
    )
  }
  return playlist
}
