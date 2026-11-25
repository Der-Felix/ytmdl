import { libraryTracks } from '@/lib/api/library'
import type { LibraryTrack } from '@/types/api'

/** The most titles "Alle abspielen" queues; a larger list plays its first ones. */
export const PLAY_ALL_CAP = 500

/** The server hands out at most this many titles per request. */
const PAGE = 100

/** A title list query without paging: the library's current filter and order. */
export type TrackQuery = Omit<NonNullable<Parameters<typeof libraryTracks>[0]>, 'limit' | 'offset' | 'signal'>

/** Loads the first `cap` titles the query returns, in its order, page by page. */
export async function loadFilteredTracks(
  query: TrackQuery,
  signal: AbortSignal,
  cap = PLAY_ALL_CAP,
  list: typeof libraryTracks = libraryTracks,
): Promise<LibraryTrack[]> {
  const out: LibraryTrack[] = []
  while (out.length < cap) {
    const res = await list({ ...query, limit: Math.min(PAGE, cap - out.length), offset: out.length, signal })
    out.push(...res.items)
    const total = res.meta.total ?? Infinity
    if (res.items.length === 0 || out.length >= total) break
  }
  return out
}
