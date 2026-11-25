import { libraryArtistDetail, libraryReleaseDetail } from '@/lib/api/library'
import type { LibraryTrack } from '@/types/api'

/** A release in playing order: disc by disc, then by track number. */
export function releaseOrder(tracks: LibraryTrack[]): LibraryTrack[] {
  return [...tracks].sort((a, b) => {
    const disc = (a.disc_number || 1) - (b.disc_number || 1)
    if (disc !== 0) return disc
    return (a.track_number || 0) - (b.track_number || 0)
  })
}

/** The tracks a release card plays, in release order. */
export async function releaseTracks(id: string, signal: AbortSignal): Promise<LibraryTrack[]> {
  const detail = await libraryReleaseDetail(id, { signal })
  return releaseOrder(detail.tracks)
}

/** The tracks an artist card plays: the same list the artist page plays. */
export async function artistTracks(id: string, signal: AbortSignal): Promise<LibraryTrack[]> {
  const detail = await libraryArtistDetail(id, { signal })
  return detail.tracks
}
