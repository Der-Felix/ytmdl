import type { MouseEvent } from 'react'

import { joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'

/** The artist line of a track: its artists, or the album artist. */
export function trackArtistText(track: Pick<LibraryTrack, 'artists' | 'album_artist'>): string {
  return track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''
}

/**
 * True for a click or double-click on a track row itself: not on a control
 * in it, and not in a menu or dialog the row opened. Those render in a
 * portal, and React passes their events up to the row all the same.
 */
export function isRowGesture(event: MouseEvent<HTMLElement>): boolean {
  const target = event.target as Element
  if (!event.currentTarget.contains(target)) return false
  return !target.closest('button, a, input, label, select, textarea')
}
