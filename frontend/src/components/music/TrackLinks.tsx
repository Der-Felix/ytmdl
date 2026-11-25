import type { MouseEvent, ReactNode } from 'react'

import { Link, paths } from '@/lib/router'
import { cn } from '@/lib/utils'
import { trackArtistText } from '@/lib/track-row'
import type { LibraryTrack } from '@/types/api'

/*
 * Artist names and album titles in track rows and the players. They link to
 * the library pages when the track carries the id, and stay plain text when
 * it does not (offline copies, older servers). A link sits inside rows that
 * react to clicks themselves, so its click never reaches the row.
 */

// The names sit in truncating cells that clip anything drawn outside the
// text, so the focus ring is drawn inside the link.
const linkClass =
  'rounded-sm underline-offset-2 outline-none transition-colors duration-(--duration-fast) hover:text-foreground hover:underline focus-visible:text-foreground focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring'

function stop(event: MouseEvent) {
  event.stopPropagation()
}

interface TrackLinkProps {
  track: LibraryTrack
  className?: string
  /** Shown instead of the default text. */
  children?: ReactNode
}

export function TrackArtistLink({ track, className, children }: TrackLinkProps) {
  const text = children ?? trackArtistText(track)
  if (!track.artist_id || !text) return <span className={className}>{text}</span>
  return (
    <Link
      href={paths.libraryArtist(track.artist_id)}
      onClick={stop}
      onDoubleClick={stop}
      className={cn(linkClass, className)}
    >
      {text}
    </Link>
  )
}

export function TrackAlbumLink({ track, className, children }: TrackLinkProps) {
  const text = children ?? track.album
  if (!track.release_id || !text) return <span className={className}>{text}</span>
  return (
    <Link
      href={paths.libraryRelease(track.release_id)}
      onClick={stop}
      onDoubleClick={stop}
      className={cn(linkClass, className)}
    >
      {text}
    </Link>
  )
}
