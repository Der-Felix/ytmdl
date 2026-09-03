import { Cover } from '@/components/music/Cover'
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
 * One artist card for discover or library.
 */
function ArtistCard({ artist, href, isLocal }: ArtistCardProps) {
  const targetHref = href ?? (isLocal ? paths.libraryArtist(artist.id) : paths.artist(artist.id, artist.provider))
  const genres = ('genres' in artist && artist.genres ? artist.genres : []).filter(Boolean).slice(0, 2)
  const isLibArtist = 'release_count' in artist

  return (
    <Link
      href={targetHref}
      className="panel panel-interactive focus-ring group flex h-full flex-col items-center gap-4 p-5 text-center"
    >
      <Cover
        src={artist.image_url}
        alt=""
        shape="circle"
        className="w-28 max-w-full"
      />

      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p
          className="line-clamp-2 font-heading text-[0.9375rem] leading-snug font-semibold text-foreground"
          title={artist.name}
        >
          {artist.name}
        </p>

        {isLibArtist ? (
          <p className="text-xs text-muted-foreground">
            {pluralize(artist.release_count, 'Release', 'Releases')} · {pluralize(artist.track_count, 'Track')}
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
    </Link>
  )
}

export { ArtistCard }
