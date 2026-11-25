import { ListMusicIcon, ListPlusIcon, PlayIcon } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { TrackMenu } from '@/components/music/TrackMenu'
import { CommandGroup, CommandItem } from '@/components/ui/command'
import { resultValue } from '@/hooks/useCommandSearch'
import type { CommandSearch } from '@/hooks/useCommandSearch'
import { libraryArtwork } from '@/lib/artwork'
import { formatDuration, formatYear, joinArtists, pluralize } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'

interface ResultGroupsProps {
  search: CommandSearch
  currentTrackId: string | undefined
  onPlay: (track: LibraryTrack) => void
  onQueue: (track: LibraryTrack) => void
  onOpen: (href: string) => void
  /** Closes the menu, e.g. when a title's track menu opens another page. */
  onClose: () => void
  hrefs: {
    artist: (id: string) => string
    release: (id: string) => string
    playlist: (id: string) => string
  }
}

/** The four result groups. They render above the menu's own entries. */
export function ResultGroups({ search, currentTrackId, onPlay, onQueue, onOpen, onClose, hrefs }: ResultGroupsProps) {
  return (
    <>
      {search.tracks.length > 0 && (
        <CommandGroup heading="Titel">
          {search.tracks.map((track, index) => {
            const artist = track.album_artist || joinArtists(track.artists)
            const current = track.id === currentTrackId
            return (
              <CommandItem
                key={track.id}
                value={resultValue('track', index, track.id)}
                onSelect={() => onPlay(track)}
                aria-label={`${track.title} von ${artist} abspielen`}
                className="group pr-1.5"
                data-track-row
              >
                <span className="relative size-9 shrink-0">
                  <Cover
                    src={libraryArtwork('tracks', track.id)}
                    fallbackSrc={track.cover_url}
                    alt=""
                    className="size-9 rounded-md"
                  />
                  <span
                    aria-hidden="true"
                    className="absolute inset-0 flex items-center justify-center rounded-md bg-overlay/55 opacity-0 group-data-[selected=true]:opacity-100"
                  >
                    <PlayIcon className="size-3.5 fill-current text-foreground!" />
                  </span>
                </span>
                <ResultText
                  title={track.title}
                  detail={[artist, track.album].filter(Boolean).join(' · ')}
                  highlight={current}
                />
                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {formatDuration(track.duration_ms)}
                </span>
                <button
                  type="button"
                  tabIndex={-1}
                  aria-label={`${track.title} zur Warteschlange hinzufügen`}
                  title="Zur Warteschlange hinzufügen (⇧↵)"
                  // Keep the focus in the field, so typing goes on.
                  onMouseDown={(event) => event.preventDefault()}
                  onClick={(event) => {
                    event.stopPropagation()
                    onQueue(track)
                  }}
                  className="flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-[opacity,background-color] duration-(--duration-fast) hover:bg-background/40 hover:text-foreground focus-visible:opacity-100 pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:group-data-[selected=true]:opacity-100"
                >
                  <ListPlusIcon className="size-4" />
                </button>
                {/* The same menu as in every other track list; right-click on the
                    row opens it as well (data-track-row). The menu is portaled,
                    but React still bubbles its events to this item: without the
                    wrapper a click or Enter in the menu would also play the title.
                    A link in the menu ("Zum Album") closes the command menu. */}
                <span
                  className="contents"
                  onClick={(event) => {
                    event.stopPropagation()
                    if ((event.target as Element).closest('a[href]')) onClose()
                  }}
                  onKeyDown={(event) => event.stopPropagation()}
                  onPointerMove={(event) => event.stopPropagation()}
                >
                  <TrackMenu
                    track={track}
                    size="compact"
                    className="text-muted-foreground pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:group-data-[selected=true]:opacity-100 focus-visible:opacity-100 data-[popup-open]:opacity-100"
                  />
                </span>
              </CommandItem>
            )
          })}
        </CommandGroup>
      )}

      {search.artists.length > 0 && (
        <CommandGroup heading="Künstler">
          {search.artists.map((artist, index) => (
            <CommandItem
              key={artist.id}
              value={resultValue('artist', index, artist.id)}
              onSelect={() => onOpen(hrefs.artist(artist.id))}
            >
              <Cover
                src={artist.image_url}
                fallbackSrc={libraryArtwork('artists', artist.id)}
                alt=""
                shape="circle"
                className="size-9 shrink-0"
              />
              <ResultText title={artist.name} detail={`Künstler · ${pluralize(artist.track_count, 'Titel', 'Titel')}`} />
            </CommandItem>
          ))}
        </CommandGroup>
      )}

      {search.releases.length > 0 && (
        <CommandGroup heading="Alben">
          {search.releases.map((release, index) => (
            <CommandItem
              key={release.id}
              value={resultValue('release', index, release.id)}
              onSelect={() => onOpen(hrefs.release(release.id))}
            >
              <Cover
                src={libraryArtwork('releases', release.id)}
                fallbackSrc={release.cover_url}
                alt=""
                className="size-9 shrink-0 rounded-md"
              />
              <ResultText
                title={release.title}
                detail={[release.album_artist || joinArtists(release.artists), release.year ? formatYear(release.year) : '']
                  .filter(Boolean)
                  .join(' · ')}
              />
            </CommandItem>
          ))}
        </CommandGroup>
      )}

      {search.playlists.length > 0 && (
        <CommandGroup heading="Playlists">
          {search.playlists.map((playlist, index) => (
            <CommandItem
              key={playlist.id}
              value={resultValue('playlist', index, playlist.id)}
              onSelect={() => onOpen(hrefs.playlist(playlist.id))}
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
                <ListMusicIcon aria-hidden="true" />
              </span>
              <ResultText
                title={playlist.name}
                detail={
                  playlist.smart_rules
                    ? 'Smarte Playlist'
                    : `Playlist · ${pluralize(playlist.track_count, 'Titel', 'Titel')}`
                }
              />
            </CommandItem>
          ))}
        </CommandGroup>
      )}
    </>
  )
}

function ResultText({ title, detail, highlight }: { title: string; detail: string; highlight?: boolean }) {
  return (
    <span className="flex min-w-0 flex-1 flex-col">
      <span className={highlight ? 'truncate font-medium text-primary' : 'truncate font-medium'}>{title}</span>
      <span className="truncate text-xs text-muted-foreground in-data-[selected=true]:text-accent-foreground/75">
        {detail}
      </span>
    </span>
  )
}
