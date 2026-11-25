import { useCallback, useMemo } from 'react'

import { libraryArtwork } from '@/lib/artwork'
import { CardPlayButton } from '@/components/music/CardPlayButton'
import { Cover } from '@/components/music/Cover'
import { useCollectionPlayback } from '@/hooks/useCollectionPlayback'
import type { QueueContext } from '@/lib/audio/types'
import { artistTracks } from '@/lib/collection-tracks'
import { Link, paths } from '@/lib/router'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { Artist, LibraryArtist } from '@/types/api'

/** The provider names, written the way they are spelled. */
const PROVIDER_LABELS: Record<string, string> = {
  ytmusic: 'YouTube Music',
  youtube: 'YouTube',
  spotify: 'Spotify',
}

interface ArtistCardProps {
  artist: Artist | LibraryArtist
  href?: string
  isLocal?: boolean
}

/**
 * One artist card for discover or library. The name is a link stretched over
 * the whole card; a library artist also gets a play button on its picture.
 */
function ArtistCard({ artist, href, isLocal }: ArtistCardProps) {
  const targetHref = href ?? (isLocal ? paths.libraryArtist(artist.id) : paths.artist(artist.id, artist.provider))
  const genres = ('genres' in artist && artist.genres ? artist.genres : []).filter(Boolean).slice(0, 2)
  const isLibArtist = 'release_count' in artist

  return (
    <div className="panel panel-interactive group relative flex h-full flex-col items-center gap-4 p-5 text-center">
      <div className="relative w-28 max-w-full">
        <Cover
          src={artist.image_url}
          fallbackSrc={isLocal ? libraryArtwork('artists', artist.id) : undefined}
          alt=""
          shape="circle"
          className="w-full"
        />
        {isLocal && <ArtistPlayButton id={artist.id} name={artist.name} />}
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <Link
          href={targetHref}
          className="focus-ring rounded-md after:absolute after:inset-0 after:rounded-2xl after:content-['']"
        >
          <span
            className="line-clamp-2 font-heading text-[0.9375rem] leading-snug font-semibold text-foreground"
            title={artist.name}
          >
            {artist.name}
          </span>
        </Link>

        {isLibArtist ? (
          <p className="text-xs text-muted-foreground">
            {pluralize(artist.release_count, 'Release', 'Releases')} · {pluralize(artist.track_count, 'Track')}
            {genres.length > 0 && <span className="mt-1 block line-clamp-1">{genres.join(' · ')}</span>}
          </p>
        ) : (
          <>
            {genres.length > 0 && (
              <p className="line-clamp-1 text-xs text-muted-foreground" title={genres.join(', ')}>
                {genres.join(' · ')}
              </p>
            )}

            {'popularity' in artist && artist.popularity !== undefined && artist.popularity > 0 && (
              <p className="text-xs text-muted-foreground">
                Popularität {formatNumber(artist.popularity)}
              </p>
            )}
          </>
        )}

        <p className="mt-auto pt-1 text-[0.6875rem] text-muted-foreground/70">
          {PROVIDER_LABELS[artist.provider] ?? artist.provider}
        </p>
      </div>
    </div>
  )
}

/** Plays all of a library artist's tracks, as the artist page's "Abspielen" does. */
function ArtistPlayButton({ id, name }: { id: string; name: string }) {
  const load = useCallback((signal: AbortSignal) => artistTracks(id, signal), [id])
  const context = useMemo<QueueContext>(() => ({ kind: 'artist', id, title: name }), [id, name])
  const playback = useCollectionPlayback(context, load, 'artist')
  return <CardPlayButton title={name} playback={playback} className="right-0 bottom-0" />
}

export { ArtistCard }
