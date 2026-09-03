import {
  ArrowDown,
  ArrowUp,
  ArrowUpDown,
  InfoIcon,
  ListPlus,
  Pause,
  Play,
  Radio,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { usePlayer } from '@/hooks/usePlayer'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'
import { LyricsBadge } from './LyricsBadge'

interface TracksTableProps {
  tracks: LibraryTrack[]
  sort: string
  order: string
  onSortChange: (sort: string) => void
  onTrackSelect: (track: LibraryTrack) => void
  onLyricsSelect?: (track: LibraryTrack) => void
  showAlbum?: boolean
  showArtist?: boolean
  showDiscNumber?: boolean
  className?: string
}

export function TracksTable({
  tracks,
  sort,
  order,
  onSortChange,
  onTrackSelect,
  onLyricsSelect,
  showAlbum = true,
  showArtist = true,
  showDiscNumber = false,
  className = '',
}: TracksTableProps) {
  const { currentTrack, status, playTrack, togglePlayPause, playNext, addToQueue } = usePlayer()

  const renderSortHeader = (label: string, field: string) => {
    const isCurrent = sort === field
    return (
      <button
        type="button"
        onClick={() => onSortChange(field)}
        className="inline-flex items-center gap-1 font-semibold text-xs text-neutral-400 hover:text-neutral-200 transition-colors uppercase tracking-wider select-none"
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
    <div className={`w-full overflow-x-auto rounded-xl border border-neutral-800 bg-neutral-950/40 ${className}`}>
      <table className="w-full text-left text-sm border-collapse min-w-[650px]">
        <thead>
          <tr className="border-b border-neutral-800/80 bg-neutral-900/40 text-neutral-400">
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
              <th className="py-3 px-3">
                {renderSortHeader('Künstler', 'artist')}
              </th>
            )}
            {showAlbum && (
              <th className="py-3 px-3 hidden md:table-cell">
                {renderSortHeader('Album', 'album')}
              </th>
            )}
            <th className="py-3 px-3 w-28 text-center">Format</th>
            <th className="py-3 px-3 w-24 text-center">Lyrics</th>
            <th className="py-3 px-3 w-20 text-right">
              {renderSortHeader('Dauer', 'duration')}
            </th>
            <th className="py-3 px-2 w-20 text-right"></th>
          </tr>
        </thead>
        <tbody className="divide-y divide-neutral-800/40">
          {tracks.map((track, trackIdx) => {
            const isCurrent = currentTrack?.id === track.id
            const isPlaying = isCurrent && status === 'playing'
            const artistName = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''

            const handlePlayClick = (e: React.MouseEvent) => {
              e.stopPropagation()
              if (isCurrent) {
                togglePlayPause()
              } else {
                playTrack(track, tracks, trackIdx)
              }
            }

            return (
              <tr
                key={track.id}
                onClick={() => onTrackSelect(track)}
                className={`transition-colors cursor-pointer group ${
                  isCurrent ? 'bg-white/[0.04]' : 'hover:bg-white/[0.02]'
                }`}
              >
                {/* Track Number / Play Button */}
                <td className="py-2.5 px-3 text-center font-mono text-xs relative">
                  <span className={`group-hover:hidden ${isCurrent ? 'hidden' : 'text-neutral-500'}`}>
                    {track.track_number || '–'}
                  </span>
                  {isCurrent && !isPlaying && (
                    <Radio className="size-3.5 text-primary mx-auto group-hover:hidden" />
                  )}
                  {isPlaying && (
                    <Radio className="size-3.5 text-primary animate-pulse mx-auto group-hover:hidden" />
                  )}
                  <button
                    type="button"
                    onClick={handlePlayClick}
                    className={`size-6 items-center justify-center rounded-full bg-primary text-white mx-auto transition-transform hover:scale-110 active:scale-95 ${
                      isCurrent ? 'flex' : 'hidden group-hover:flex'
                    }`}
                    title={isPlaying ? 'Pause' : 'Abspielen'}
                    aria-label={isPlaying ? 'Pause' : 'Abspielen'}
                  >
                    {isPlaying ? (
                      <Pause className="size-3 fill-current" />
                    ) : (
                      <Play className="size-3 fill-current ml-0.5" />
                    )}
                  </button>
                </td>

                {showDiscNumber && (
                  <td className="py-2.5 px-2 text-center text-neutral-500 font-mono text-xs">
                    {track.disc_number || '1'}
                  </td>
                )}

                <td className="py-2.5 px-3 font-medium">
                  <div className={`truncate max-w-[280px] lg:max-w-md ${isCurrent ? 'text-primary font-semibold' : 'text-neutral-200'}`}>
                    {track.title}
                  </div>
                </td>

                {showArtist && (
                  <td className="py-2.5 px-3 text-neutral-400">
                    <div className="truncate max-w-[180px]">{artistName}</div>
                  </td>
                )}

                {showAlbum && (
                  <td className="py-2.5 px-3 text-neutral-400 hidden md:table-cell">
                    <div className="truncate max-w-[200px]">{track.album || '–'}</div>
                  </td>
                )}

                <td className="py-2.5 px-3 text-center">
                  {track.codec ? (
                    <Badge variant="outline" className="font-mono text-[11px] uppercase py-0 px-1.5 h-5">
                      {track.codec} {track.bitrate_kbps ? `${Math.round(track.bitrate_kbps)}k` : ''}
                    </Badge>
                  ) : (
                    <span className="text-neutral-600 text-xs">–</span>
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

                <td className="py-2.5 px-3 text-right text-neutral-400 font-mono text-xs">
                  {formatDuration(track.duration_ms)}
                </td>

                <td className="py-2.5 px-2 text-right">
                  <div className="flex items-center justify-end gap-1 opacity-40 group-hover:opacity-100 transition-opacity">
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-neutral-400 hover:text-primary hover:bg-white/10"
                      onClick={(e) => {
                        e.stopPropagation()
                        playNext(track)
                      }}
                      title="Als Nächstes abspielen"
                    >
                      <Play className="size-3" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-neutral-400 hover:text-primary hover:bg-white/10"
                      onClick={(e) => {
                        e.stopPropagation()
                        addToQueue(track)
                      }}
                      title="Zur Queue hinzufügen"
                    >
                      <ListPlus className="size-3.5" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 p-0 text-neutral-400 hover:text-foreground"
                      onClick={(e) => {
                        e.stopPropagation()
                        onTrackSelect(track)
                      }}
                      title="Track-Details"
                    >
                      <InfoIcon className="size-3.5" />
                    </Button>
                  </div>
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
