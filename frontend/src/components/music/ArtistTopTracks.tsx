import { useId, useState } from 'react'
import { ChevronDown, ChevronUp, Play } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { Button } from '@/components/ui/button'
import { useAsync } from '@/hooks/useAsync'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { libraryArtistTopTracks } from '@/lib/api/library'
import type { QueueContext } from '@/lib/audio/types'
import { libraryArtwork } from '@/lib/artwork'
import { cn } from '@/lib/utils'
import { formatDuration } from '@/lib/utils/format'

/** Rows shown at first; "Mehr anzeigen" reveals up to TOP_LIMIT. */
export const TOP_COLLAPSED = 5
export const TOP_LIMIT = 10

/**
 * "Am meisten gehört": the listener's most played tracks of one artist,
 * numbered by rank. The section only exists once there is history for the
 * artist; while it loads, or if the request fails, the page goes on without
 * it, since everything in it is reachable in the full track list below.
 */
export function ArtistTopTracks({ artistId, artistName }: { artistId: string; artistName: string }) {
  const { state } = useAsync((signal) => libraryArtistTopTracks(artistId, { limit: TOP_LIMIT, signal }), [artistId])
  const { currentTrack, queueContext, status } = usePlayerState()
  const { playTrack, togglePlayPause } = usePlayerActions()
  const [expanded, setExpanded] = useState(false)
  const headingId = useId()
  const listId = useId()

  if (state.status !== 'success' || state.data.length === 0) return null

  const tracks = state.data
  const shown = expanded ? tracks : tracks.slice(0, TOP_COLLAPSED)
  const context: QueueContext = { kind: 'history', id: `artist-top:${artistId}`, title: `Am meisten gehört · ${artistName}` }
  const fromHere = queueContext?.kind === context.kind && queueContext.id === context.id

  return (
    <section aria-labelledby={headingId} className="space-y-3">
      <h2 id={headingId} className="border-b border-border pb-3 font-heading text-lg font-semibold text-foreground">
        Am meisten gehört
      </h2>
      <ol id={listId} className="space-y-0.5">
        {shown.map((track, index) => {
          const current = fromHere && currentTrack?.id === track.id
          const playing = current && (status === 'playing' || status === 'buffering')
          return (
            <li key={track.id}>
              <button
                type="button"
                aria-current={current || undefined}
                aria-label={`${track.title}${playing ? ' pausieren' : ' abspielen'}, ${track.play_count}-mal gehört`}
                onClick={() => (current ? togglePlayPause() : playTrack(track, tracks, index, context))}
                className="focus-ring group/row flex w-full items-center gap-3 rounded-xl px-2 py-1.5 text-left transition-colors duration-(--duration-fast) hover:bg-muted"
              >
                <span className="figures w-5 shrink-0 text-center text-sm text-muted-foreground" aria-hidden>
                  <span className={cn('group-hover/row:hidden', current && 'text-primary')}>{index + 1}</span>
                  <Play className="hidden size-3.5 fill-current text-foreground group-hover/row:inline" />
                </span>
                <Cover src={libraryArtwork('tracks', track.id)} fallbackSrc={track.cover_url} alt="" className="size-10 shrink-0 rounded-lg" />
                <span className="min-w-0 flex-1">
                  <span className={cn('block truncate text-sm font-medium', current ? 'text-primary' : 'text-foreground')}>{track.title}</span>
                  <span className="block truncate text-xs text-muted-foreground">{track.album}</span>
                </span>
                <span className="figures hidden shrink-0 text-xs text-muted-foreground sm:block" aria-hidden>
                  {track.play_count}× gehört
                </span>
                <span className="figures w-12 shrink-0 text-right text-xs text-muted-foreground" aria-hidden>
                  {formatDuration(track.duration_ms || 0)}
                </span>
              </button>
            </li>
          )
        })}
      </ol>
      {tracks.length > TOP_COLLAPSED && (
        <Button
          variant="ghost"
          size="sm"
          aria-expanded={expanded}
          aria-controls={listId}
          onClick={() => setExpanded((value) => !value)}
        >
          {expanded ? <ChevronUp aria-hidden /> : <ChevronDown aria-hidden />}
          {expanded ? 'Weniger anzeigen' : 'Mehr anzeigen'}
        </Button>
      )}
    </section>
  )
}
