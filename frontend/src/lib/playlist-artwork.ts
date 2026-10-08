import type { LibraryTrack } from '@/types/api'

type ArtworkTrack = Pick<LibraryTrack, 'id' | 'release_id' | 'cover_url' | 'title'>
export function playlistPreview<T extends ArtworkTrack>(tracks: T[]): T[] {
  const seen = new Set<string>()
  const result: T[] = []
  for (const track of tracks) {
    const key = track.release_id || track.cover_url || track.id
    if (seen.has(key)) continue
    seen.add(key); result.push(track)
    if (result.length === 4) break
  }
  return result
}
