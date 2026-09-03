import { useEffect, useMemo } from 'react'
import { LinkIcon, SearchIcon, SearchXIcon } from 'lucide-react'

import { ArtistCard } from '@/components/music/ArtistCard'
import { SearchField } from '@/components/music/SearchField'
import { Panel } from '@/components/ui/panel'
import {
  EmptyState,
  ErrorState,
  GridSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { resolveAddress, searchArtists } from '@/lib/api/artists'
import { ApiError, errorMessage } from '@/lib/api/client'
import { paths, useNavigate } from '@/lib/router'
import { looksLikeAddress } from '@/lib/utils/musicUrl'
import { pluralize } from '@/lib/utils/format'

/**
 * Search.
 *
 * A pasted address is not searched for — it is resolved. The backend does that
 * work, because reading an address is provider knowledge: it knows the id
 * formats, and it is the only side that can look up the canonical channel id
 * behind a handle such as youtube.com/@artist. Anything that is not an address
 * is a plain query.
 */
function Discover({ query }: { query: string }) {
  const isAddress = useMemo(() => looksLikeAddress(query), [query])

  return (
    <div className="space-y-8">
      <header className="space-y-5">
        <div className="space-y-1.5">
          <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
            Entdecken
          </h1>
          <p className="text-sm text-muted-foreground">
            Künstler und Alben suchen oder einen Link einfügen.
          </p>
        </div>
        <SearchField defaultValue={query} autoFocus={query.length === 0} />
      </header>

      {!query.trim() ? (
        <Panel>
          <EmptyState
            icon={<SearchIcon />}
            title="Wonach suchst du?"
            description="Gib den Namen eines Künstlers ein — oder füge einen Link von Deezer, Spotify, YouTube Music oder YouTube ein."
          />
        </Panel>
      ) : isAddress ? (
        <AddressResult address={query} />
      ) : (
        <ArtistResults query={query} />
      )}
    </div>
  )
}

/**
 * Resolves a pasted address and moves on to what it points at.
 *
 * The redirect replaces the search in the history, so going back does not
 * bounce through this page again.
 */
function AddressResult({ address }: { address: string }) {
  const { state, reload } = useAsync(
    (signal) => resolveAddress(address, signal),
    [address],
  )
  const navigate = useNavigate()
  const ref = state.status === 'success' ? state.data : undefined

  useEffect(() => {
    if (!ref) return
    if (ref.kind === 'artist') {
      navigate(paths.artist(ref.id, ref.provider), { replace: true })
    } else if (ref.kind === 'release') {
      navigate(paths.release(ref.id, ref.provider), { replace: true })
    }
  }, [ref, navigate])

  if (state.status === 'loading') {
    return (
      <LoadingRegion label="Link wird geöffnet">
        <GridSkeleton tiles={4} />
      </LoadingRegion>
    )
  }

  if (state.status === 'error') {
    // An address the backend cannot read is not a server failure — it is
    // something the user has to be told about in plain words.
    const unreadable =
      state.error instanceof ApiError && state.error.code === 'INVALID_REQUEST'

    if (unreadable) {
      return (
        <Panel>
          <EmptyState
            icon={<LinkIcon />}
            title="Dieser Link lässt sich nicht öffnen"
            description={errorMessage(state.error)}
          />
        </Panel>
      )
    }
    return (
      <Panel>
        <ErrorState error={state.error} onRetry={reload} />
      </Panel>
    )
  }

  if (ref?.kind === 'track') {
    return (
      <Panel>
        <EmptyState
          icon={<LinkIcon />}
          title="Einzelne Tracks lassen sich hier nicht öffnen"
          description="Ein Track kann aus der Albumansicht heruntergeladen werden. Verwende den Link des Albums oder des Künstlers."
        />
      </Panel>
    )
  }

  // An artist or release: the redirect above is already running.
  return (
    <LoadingRegion label="Link wird geöffnet">
      <GridSkeleton tiles={4} />
    </LoadingRegion>
  )
}

function ArtistResults({ query }: { query: string }) {
  const { state, reload } = useAsync(
    (signal) => searchArtists(query, { signal }),
    [query],
  )

  if (state.status === 'loading') {
    return (
      <LoadingRegion label="Suchergebnisse werden geladen">
        <GridSkeleton tiles={5} />
      </LoadingRegion>
    )
  }

  if (state.status === 'error') {
    return (
      <Panel>
        <ErrorState error={state.error} onRetry={reload} />
      </Panel>
    )
  }

  if (state.data.length === 0) {
    return (
      <Panel>
        <EmptyState
          icon={<SearchXIcon />}
          title={`Keine Treffer für „${query}"`}
          description="Prüfe die Schreibweise oder versuche einen anderen Namen."
        />
      </Panel>
    )
  }

  return (
    <section aria-label="Suchergebnisse" className="space-y-4">
      <p className="text-sm text-muted-foreground">
        {pluralize(state.data.length, 'Treffer', 'Treffer')}
      </p>
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
        {state.data.map((artist) => (
          <ArtistCard key={`${artist.provider}:${artist.id}`} artist={artist} />
        ))}
      </div>
    </section>
  )
}

export { Discover }
