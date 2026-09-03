import { DiscIcon } from 'lucide-react'

import { DiscographyDialog } from '@/components/downloads/DiscographyDialog'
import { SubscribeControl } from '@/components/subscriptions/SubscribeControl'
import { Cover } from '@/components/music/Cover'
import { ReleaseCard } from '@/components/music/ReleaseCard'
import { Panel, SectionHeading } from '@/components/ui/panel'
import { Skeleton } from '@/components/ui/skeleton'
import {
  EmptyState,
  ErrorState,
  GridSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import {
  ALL_RELEASE_TYPES,
  RELEASE_TYPE_LABELS,
  getArtist,
  getDiscography,
  groupReleases,
} from '@/lib/api/artists'
import { getSettings } from '@/lib/api/settings'
import { paths, useNavigate } from '@/lib/router'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { Artist as ArtistModel, Release, ReleaseType } from '@/types/api'

interface ArtistPageProps {
  id: string
  provider?: string
}

/**
 * An artist and everything they released.
 *
 * The discography is requested with every release type selected, so the page
 * can group what actually exists instead of the backend's default subset. The
 * grouping decides which headings appear — a type with no releases gets none.
 */
function Artist({ id, provider }: ArtistPageProps) {
  const artist = useAsync(
    (signal) => getArtist(id, { provider, signal }),
    [id, provider],
  )
  const discography = useAsync(
    (signal) =>
      getDiscography(id, { provider, filter: ALL_RELEASE_TYPES, signal }),
    [id, provider],
  )

  if (artist.state.status === 'error') {
    return (
      <Panel>
        <ErrorState error={artist.state.error} onRetry={artist.reload} />
      </Panel>
    )
  }

  const releases =
    discography.state.status === 'success' ? discography.state.data : []
  const counts = countByType(releases)

  return (
    <div className="space-y-8">
      {artist.state.status === 'loading' ? (
        <HeaderSkeleton />
      ) : (
        <ArtistHeader
          artist={artist.state.data}
          releases={releases}
          counts={counts}
          discographyReady={discography.state.status === 'success'}
        />
      )}

      {discography.state.status === 'loading' && (
        <LoadingRegion label="Diskografie wird geladen">
          <GridSkeleton tiles={8} />
        </LoadingRegion>
      )}

      {discography.state.status === 'error' && (
        <Panel>
          <ErrorState error={discography.state.error} onRetry={discography.reload} />
        </Panel>
      )}

      {discography.state.status === 'success' && (
        <ReleaseGroups releases={releases} provider={provider} />
      )}
    </div>
  )
}

function ArtistHeader({
  artist,
  releases,
  counts,
  discographyReady,
}: {
  artist: ArtistModel
  releases: Release[]
  counts: Partial<Record<ReleaseType, number>>
  discographyReady: boolean
}) {
  const navigate = useNavigate()
  const settings = useAsync((signal) => getSettings(signal), [])

  return (
    <header className="flex flex-col gap-6 sm:flex-row sm:items-end">
      <Cover
        src={artist.image_url}
        alt=""
        shape="circle"
        className="w-32 shrink-0 sm:w-44"
      />

      <div className="min-w-0 flex-1 space-y-4">
        <div className="space-y-2">
          <h1 className="font-heading text-3xl leading-tight font-semibold tracking-tight break-words text-foreground sm:text-4xl">
            {artist.name}
          </h1>
          <Tally releases={releases} counts={counts} ready={discographyReady} />
        </div>

        <div className="flex flex-wrap items-start gap-3">
          {discographyReady && releases.length > 0 && (
            <DiscographyDialog
              artist={artist}
              counts={counts}
              // Falls back to the backend's own default while settings load.
              defaultSkipExisting={
                settings.state.status === 'success'
                  ? settings.state.data.skip_existing
                  : true
              }
              onStarted={() => navigate(paths.downloads())}
            />
          )}

          {/*
            The subscribe control does not wait for the discography: watching
            an artist is independent of whether their releases have loaded.
          */}
          <SubscribeControl artist={artist} />
        </div>
      </div>
    </header>
  )
}

/**
 * The counts under the artist name.
 *
 * A total track count is only shown when every release actually reports one.
 * The YouTube Music discography listing sends track_count: 0, so summing it
 * there would invent a number — the release count is what is genuinely known.
 */
function Tally({
  releases,
  counts,
  ready,
}: {
  releases: Release[]
  counts: Partial<Record<ReleaseType, number>>
  ready: boolean
}) {
  if (!ready) {
    return <Skeleton className="h-4 w-48 rounded-md" />
  }
  if (releases.length === 0) {
    return (
      <p className="text-sm text-muted-foreground">Keine Veröffentlichungen gefunden</p>
    )
  }

  const everyReleaseKnowsItsTracks = releases.every((release) => release.track_count > 0)
  const trackTotal = releases.reduce((total, release) => total + release.track_count, 0)

  const parts = [
    pluralize(releases.length, 'Veröffentlichung', 'Veröffentlichungen'),
    ...Object.entries(counts).map(([type, count]) => {
      const labels = RELEASE_TYPE_LABELS[type as ReleaseType]
      return `${formatNumber(count)} ${count === 1 ? labels.one : labels.many}`
    }),
  ]
  if (everyReleaseKnowsItsTracks) parts.push(pluralize(trackTotal, 'Track'))

  return (
    <p className="text-sm text-muted-foreground">{parts.join(' · ')}</p>
  )
}

function ReleaseGroups({
  releases,
  provider,
}: {
  releases: Release[]
  provider?: string
}) {
  if (releases.length === 0) {
    return (
      <Panel>
        <EmptyState
          icon={<DiscIcon />}
          title="Keine Veröffentlichungen"
          description="Der Metadatenprovider liefert für diesen Künstler keine Releases."
        />
      </Panel>
    )
  }

  return (
    <div className="space-y-9">
      {groupReleases(releases).map((group) => (
        <section key={group.type} className="space-y-4">
          <SectionHeading
            title={RELEASE_TYPE_LABELS[group.type].many}
            count={formatNumber(group.releases.length)}
          />
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5">
            {group.releases.map((release) => (
              <ReleaseCard
                key={`${release.provider}:${release.source_id || release.id}`}
                release={release}
                provider={provider}
              />
            ))}
          </div>
        </section>
      ))}
    </div>
  )
}

function countByType(releases: Release[]): Partial<Record<ReleaseType, number>> {
  const counts: Partial<Record<ReleaseType, number>> = {}
  for (const release of releases) {
    counts[release.release_type] = (counts[release.release_type] ?? 0) + 1
  }
  return counts
}

function HeaderSkeleton() {
  return (
    <div className="flex flex-col gap-6 sm:flex-row sm:items-end" aria-hidden>
      <Skeleton className="size-32 shrink-0 rounded-full sm:size-44" />
      <div className="flex-1 space-y-3">
        <Skeleton className="h-9 w-64 max-w-full rounded-lg" />
        <Skeleton className="h-4 w-48 rounded-md" />
        <Skeleton className="h-11 w-56 rounded-xl" />
      </div>
    </div>
  )
}

export { Artist }
