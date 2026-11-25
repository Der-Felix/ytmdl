import { useState } from 'react'
import { ArrowDown, ArrowUp, ArrowUpDown, Heart } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'
import type { QueueContext } from '@/lib/audio/types'
import { AddToPlaylistDialog } from './AddToPlaylistDialog'
import { useOptionalAuth } from '@/hooks/useAuth'
import type { MetadataPatch } from '@/lib/api/libraryTools'
import { BulkMetadataDialog } from './BulkMetadataDialog'
import { LyricsBadge } from './LyricsBadge'
import { TrackPlaybackButton } from './TrackPlaybackButton'
import { TrackAlbumLink, TrackArtistLink } from './TrackLinks'
import { isRowGesture } from '@/lib/track-row'
import { TrackMenu } from './TrackMenu'

/*
 * Rows: the number button plays or pauses, a click on the title plays (or
 * resumes) the title with the list as context, and so does a double-click
 * anywhere else in the row. Artist and album are links. Everything else sits
 * in the track menu ("more" button or right-click); the heart stays as the
 * one quick action.
 */

interface TracksTableProps {
  tracks: LibraryTrack[]
  sort: string
  order: string
  onSortChange: (sort: string) => void
  /** Opens the track's details ("Details anzeigen" in the track menu). */
  onTrackSelect: (track: LibraryTrack) => void
  onLyricsSelect?: (track: LibraryTrack) => void
  fullQueue?: LibraryTrack[]
  /** What the list is, shown in the queue as "Weiter aus: …". */
  queueContext?: QueueContext | null
  showAlbum?: boolean
  showArtist?: boolean
  showDiscNumber?: boolean
  onMetadataUpdated?: () => void
  className?: string
}

export function TracksTable({
  tracks: sourceTracks,
  sort,
  order,
  onSortChange,
  onTrackSelect,
  onLyricsSelect,
  fullQueue,
  queueContext,
  showAlbum = true,
  showArtist = true,
  showDiscNumber = false,
  className = '',
  onMetadataUpdated,
}: TracksTableProps) {
  const { currentTrack, status } = usePlayerState()
  const { playTrack, togglePlayPause, addToQueue } = usePlayerActions()
  const auth = useOptionalAuth()
  const [metadata, setMetadata] = useState<Record<string, MetadataPatch>>({})
  const tracks = sourceTracks.map((track) => ({ ...track, ...metadata[track.id] }))
  const [selection, setSelection] = useState<Set<string>>(new Set())
  const [bulkPlaylist, setBulkPlaylist] = useState(false),
    [bulkMetadata, setBulkMetadata] = useState(false)
  const selected = tracks.filter((t) => selection.has(t.id))
  const toggle = (id: string) =>
    setSelection((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  const favorites = useOptionalFavorites()

  const renderSortHeader = (label: string, field: string) => {
    const isCurrent = sort === field
    return (
      <button
        type="button"
        onClick={() => onSortChange(field)}
        className="inline-flex items-center gap-1 font-semibold text-xs text-muted-foreground hover:text-foreground transition-colors uppercase tracking-wider select-none"
      >
        <span>{label}</span>
        {isCurrent ? (
          order === 'asc' ? (
            <ArrowUp className="size-3 text-accent" />
          ) : (
            <ArrowDown className="size-3 text-accent" />
          )
        ) : (
          <ArrowUpDown className="size-3 opacity-30 group-hover:opacity-100" />
        )}
      </button>
    )
  }

  return (
    <div className="space-y-3">
      {selected.length > 0 && (
        <div
          className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-background p-3 text-sm"
          role="region"
          aria-label="Ausgewählte Titel"
        >
          <span>{selected.length} ausgewählt</span>
          {selected.length > 100 && <span className="text-xs text-muted-foreground">Für Playlist und Metadaten maximal 100 Titel auswählen.</span>}
          <Button size="sm" onClick={() => addToQueue(selected)}>
            Zur Queue
          </Button>
          <Button size="sm" variant="outline" disabled={selected.length > 100} onClick={() => setBulkPlaylist(true)}>
            Zur Playlist
          </Button>
          {auth?.isAdmin && (
            <Button size="sm" variant="outline" disabled={selected.length > 100} onClick={() => setBulkMetadata(true)}>
              Metadaten bearbeiten
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => setSelection(new Set())}>
            Auswahl aufheben
          </Button>
        </div>
      )}
      {/* relative: absolutely positioned children (the sr-only column
          names) would otherwise escape the scroll box and widen the page. */}
      <div
        className={`relative w-full overflow-x-auto rounded-xl border border-border bg-card/40 ${className}`}
      >
      <table className="w-full text-left text-sm border-collapse min-w-[650px]">
        <thead>
          {/* One header style for sortable and plain columns alike. */}
          <tr className="border-b border-border/80 bg-card/40 text-xs font-semibold tracking-wider text-muted-foreground uppercase [&>th]:font-semibold">

            <th className="p-3 w-10">
              <input type="checkbox" aria-label="Alle angezeigten Titel auswählen"
                checked={tracks.length > 0 && selected.length === tracks.length}
                onChange={(e) => setSelection(e.target.checked ? new Set(tracks.map((t) => t.id)) : new Set())} />
            </th>
            <th className="py-3 px-3 w-12 text-center">
              {renderSortHeader('#', 'track_number')}
            </th>
            {showDiscNumber && (
              <th className="py-3 px-2 w-12 text-center">Disc</th>
            )}
            <th className="py-3 px-3">
              {renderSortHeader('Titel', 'title')}
            </th>
            {showArtist && (
              <th className="w-[18%] py-3 px-3">
                {renderSortHeader('Künstler', 'artist')}
              </th>
            )}
            {showAlbum && (
              <th className="w-[20%] py-3 px-3 hidden md:table-cell">
                {renderSortHeader('Album', 'album')}
              </th>
            )}
            {/* Format is reference only; below xl its room goes to the names. */}
            <th className="py-3 px-3 w-28 text-center hidden xl:table-cell">Format</th>
            <th className="py-3 px-3 w-24 text-center">Lyrics</th>
            <th className="py-3 px-3 w-20 text-right">
              {renderSortHeader('Dauer', 'duration')}
            </th>
            <th className="py-3 px-2 w-24 text-right"><span className="sr-only">Aktionen</span></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-neutral-800/40">
          {tracks.map((track, trackIdx) => {
            const isCurrent = currentTrack?.id === track.id
            const isPlaying = isCurrent && status === 'playing'
            const artistName = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''

            const startTrack = () => {
              const targetQueue = fullQueue ?? tracks
              const queueIdx = fullQueue
                ? fullQueue.findIndex((t) => t.id === track.id)
                : trackIdx
              playTrack(track, targetQueue, queueIdx >= 0 ? queueIdx : trackIdx, queueContext)
            }
            // The number button toggles; the title and a double-click only
            // ever start or resume, so a double-click on the title never
            // pauses what its first click started.
            const handlePlayClick = (e: React.MouseEvent) => {
              e.stopPropagation()
              if (isCurrent) togglePlayPause()
              else startTrack()
            }
            const playFromRow = () => {
              if (!isCurrent) startTrack()
              else if (!isPlaying) togglePlayPause()
            }

            return (
              <tr
                key={track.id}
                data-track-row
                aria-selected={selection.has(track.id)}
                onDoubleClick={(e) => {
                  if (!isRowGesture(e)) return
                  playFromRow()
                }}
                className={`transition-colors group ${
                  isCurrent ? 'bg-muted' : 'hover:bg-muted'
                }`}
              >
                {/* Track Number / Play Button */}
                <td className="p-3" onClick={(e) => e.stopPropagation()}>
                  <input type="checkbox" aria-label={`${track.title} auswählen`}
                    checked={selection.has(track.id)} onChange={() => toggle(track.id)} />
                </td>
                <td className="py-2.5 px-3 text-center figures text-xs relative">
                  <TrackPlaybackButton
                    number={track.track_number || '–'}
                    title={track.title}
                    isCurrent={isCurrent}
                    isPlaying={isPlaying}
                    onClick={handlePlayClick}
                  />
                </td>

                {showDiscNumber && (
                  <td className="py-2.5 px-2 text-center text-muted-foreground/80 figures text-xs">
                    {track.disc_number || '1'}
                  </td>
                )}

                {/* Title, artist and album truncate inside their column
                    (max-w-0): artist and album take a share of the width (see
                    the header), the title the rest. A long name never pushes
                    duration and actions out of the table, and the columns do
                    not jump with the content. Below the title's minimum the
                    table scrolls sideways instead. */}
                <td className="max-w-0 min-w-36 py-2.5 px-3 font-medium">
                  <button
                    type="button"
                    onClick={playFromRow}
                    title={track.title}
                    className={`focus-ring block max-w-full cursor-pointer truncate rounded-sm text-left ${isCurrent ? 'text-primary font-semibold' : 'text-foreground'}`}
                  >
                    {track.title}
                  </button>
                </td>

                {showArtist && (
                  <td className="max-w-0 py-2.5 px-3 text-muted-foreground">
                    <div className="truncate" title={artistName}>
                      <TrackArtistLink track={track}>{artistName}</TrackArtistLink>
                    </div>
                  </td>
                )}

                {showAlbum && (
                  <td className="max-w-0 py-2.5 px-3 text-muted-foreground hidden md:table-cell">
                    <div className="truncate" title={track.album || undefined}>
                      {track.album ? <TrackAlbumLink track={track} /> : '–'}
                    </div>
                  </td>
                )}

                <td className="py-2.5 px-3 text-center hidden xl:table-cell">
                  {track.codec ? (
                    <Badge variant="outline" className="font-mono text-[11px] uppercase py-0 px-1.5 h-5">
                      {track.codec} {track.bitrate_kbps ? `${Math.round(track.bitrate_kbps)}k` : ''}
                    </Badge>
                  ) : (
                    <span className="text-muted-foreground/60 text-xs">–</span>
                  )}
                </td>

                <td
                  className="py-2.5 px-3 text-center"
                  onClick={(e) => {
                    if (onLyricsSelect) {
                      e.stopPropagation()
                      onLyricsSelect(track)
                    }
                  }}
                >
                  <LyricsBadge state={track.lyrics_state} />
                </td>

                <td className="py-2.5 px-3 text-right text-muted-foreground figures text-xs">
                  {formatDuration(track.duration_ms)}
                </td>

                <td className="py-2.5 px-2 text-right">
                  <div className="flex items-center justify-end gap-0.5">
                    {favorites && (
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className={`transition-colors ${
                          favorites.isFavorite(track.id)
                            ? 'text-rose-500 hover:text-rose-400 opacity-100'
                            : 'text-muted-foreground hover:text-rose-400'
                        }`}
                        onClick={(e) => {
                          e.stopPropagation()
                          void favorites.toggleFavorite(track.id)
                        }}
                        aria-label={`${track.title}: ${
                          favorites.isFavorite(track.id) ? 'Aus Favoriten entfernen' : 'Zu Favoriten hinzufügen'
                        }`}
                        title={
                          favorites.isFavorite(track.id)
                            ? 'Aus Favoriten entfernen'
                            : 'Zu Favoriten hinzufügen'
                        }
                      >
                        <Heart
                          className={`size-3.5 ${
                            favorites.isFavorite(track.id)
                              ? 'fill-rose-500 text-rose-500'
                              : ''
                          }`}
                        />
                      </Button>
                    )}
                    <TrackMenu track={track} size="compact" onShowDetails={() => onTrackSelect(track)} />
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
      <AddToPlaylistDialog track={null} tracks={selected} open={bulkPlaylist} onOpenChange={setBulkPlaylist} />
      <BulkMetadataDialog ids={selected.map(t=>t.id)} open={bulkMetadata} onOpenChange={setBulkMetadata} onSaved={(patch) => {
        setMetadata((previous) => { const next = { ...previous }; for (const track of selected) next[track.id] = { ...next[track.id], ...patch }; return next })
        onMetadataUpdated?.()
      }} />
    </div>
    </div>
  )
}
