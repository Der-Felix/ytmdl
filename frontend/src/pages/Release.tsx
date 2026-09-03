import { ArrowLeftIcon, DiscIcon } from 'lucide-react'
import { useState } from 'react'

import { DownloadButton } from '@/components/downloads/DownloadButton'
import { Cover } from '@/components/music/Cover'
import { LyricsBadge } from '@/components/music/LyricsBadge'
import { LyricsPanel } from '@/components/music/LyricsPanel'
import { Button } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { Skeleton } from '@/components/ui/skeleton'
import {
  EmptyState,
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { RELEASE_TYPE_LABELS, getRelease } from '@/lib/api/artists'
import { downloadRelease, downloadTrack } from '@/lib/api/jobs'
import { paths, useNavigate } from '@/lib/router'
import {
  formatDuration,
  formatYear,
  joinArtists,
  pluralize,
} from '@/lib/utils/format'
import type { Release as ReleaseModel, Track } from '@/types/api'

interface ReleasePageProps {
  id: string
  provider?: string
}

/**
 * An album, single or EP with its track list.
 *
 * Each track can be downloaded individually, or the entire release at once.
 */
function Release({ id, provider }: ReleasePageProps) {
  const { state, reload } = useAsync(
    (signal) => getRelease(id, { provider, signal }),
    [id, provider],
  )

  if (state.status === 'loading') {
    return (
      <div className="space-y-8">
        <ReleaseHeaderSkeleton />
        <LoadingRegion label="Tracks werden geladen">
          <ListSkeleton rows={8} />
        </LoadingRegion>
      </div>
    )
  }

  if (state.status === 'error') {
    return (
      <Panel>
        <ErrorState error={state.error} onRetry={reload} />
      </Panel>
    )
  }

  const { release, tracks } = state.data
  const source = provider ?? release.provider

  return (
    <div className="space-y-8">
      <ReleaseHeader release={release} tracks={tracks} provider={source} />

      <section aria-labelledby="tracklist-heading" className="space-y-4">
        <h2 id="tracklist-heading" className="sr-only">
          Titelliste
        </h2>

        {tracks.length === 0 ? (
          <Panel>
            <EmptyState
              icon={<DiscIcon />}
              title="Keine Tracks vorhanden"
              description="Für dieses Release wurden keine einzelnen Titel gefunden."
            />
          </Panel>
        ) : (
          <TrackList release={release} tracks={tracks} provider={source} />
        )}
      </section>
    </div>
  )
}

function ReleaseHeader({
  release,
  tracks,
  provider,
}: {
  release: ReleaseModel
  tracks: Track[]
  provider: string
}) {
  const navigate = useNavigate()
  const year = formatYear(release.year)
  const trackCount = tracks.length > 0 ? tracks.length : release.track_count
  const totalDurationMs = tracks.reduce((sum, t) => sum + (t.duration_ms || 0), 0)
  const typeLabel = RELEASE_TYPE_LABELS[release.release_type]?.one ?? 'Release'

  return (
    <div className="space-y-6">
      <div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => window.history.back()}
          className="text-muted-foreground"
        >
          <ArrowLeftIcon />
          Zurück
        </Button>
      </div>

      <header className="flex flex-col gap-6 sm:flex-row sm:items-end">
        <Cover
          src={release.cover_url}
          alt=""
          shape="square"
          className="w-36 shrink-0 shadow-xl sm:w-48"
        />

        <div className="min-w-0 flex-1 space-y-4">
          <div className="space-y-2">
            <span className="text-xs font-semibold tracking-wider text-accent-foreground uppercase">
              {typeLabel}
            </span>
            <h1 className="font-heading text-2xl leading-tight font-semibold tracking-tight break-words text-foreground sm:text-3xl lg:text-4xl">
              {release.title}
            </h1>
            <p className="text-base text-muted-foreground">
              {release.artists?.length > 0 ? (
                <span>{joinArtists(release.artists)}</span>
              ) : (
                <span>{release.album_artist}</span>
              )}
            </p>
            <p className="text-xs text-muted-foreground">
              {[
                year,
                trackCount > 0 ? pluralize(trackCount, 'Track') : '',
                totalDurationMs > 0 ? formatDuration(totalDurationMs) : '',
              ]
                .filter(Boolean)
                .join(' · ')}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-3 pt-1">
            <DownloadButton
              variant="default"
              size="lg"
              label="Release herunterladen"
              startedLabel="Download gestartet"
              start={(signal) =>
                downloadRelease(
                  {
                    release_id: release.source_id || release.id,
                    provider,
                  },
                  signal,
                )
              }
              onStarted={() => navigate(paths.downloads())}
            />
          </div>
        </div>
      </header>
    </div>
  )
}

function TrackList({
  release,
  tracks,
  provider,
}: {
  release: ReleaseModel
  tracks: Track[]
  provider: string
}) {
  const [selectedTrack, setSelectedTrack] = useState<Track | null>(null)
  const [lyricsOpen, setLyricsOpen] = useState(false)

  return (
    <>
      <Panel className="overflow-hidden p-0">
        <ul className="divide-y divide-border">
          {tracks.map((track, index) => {
            const trackNum = track.track_number || index + 1
            const duration = formatDuration(track.duration_ms)
            const targetId = track.source_id || track.id

            return (
              <li
                key={`${targetId}-${index}`}
                className="group flex items-center gap-4 px-4 py-3 transition-colors hover:bg-white/3 sm:px-6"
              >
                <span className="w-7 shrink-0 text-center font-mono text-xs tabular-nums text-muted-foreground">
                  {trackNum}
                </span>

                <div className="min-w-0 flex-1 space-y-0.5">
                  <div className="flex items-center gap-2">
                    <p className="truncate text-sm font-medium text-foreground">
                      {track.title}
                    </p>
                    <LyricsBadge
                      state={track.lyrics_state}
                      onClick={() => {
                        setSelectedTrack(track)
                        setLyricsOpen(true)
                      }}
                    />
                  </div>
                  {track.artists?.length > 0 && (
                    <p className="truncate text-xs text-muted-foreground">
                      {joinArtists(track.artists)}
                    </p>
                  )}
                </div>

                <span className="shrink-0 text-xs tabular-nums text-muted-foreground">
                  {duration}
                </span>

                <div className="shrink-0">
                  <DownloadButton
                    size="sm"
                    iconOnly
                    label={`„${track.title}" herunterladen`}
                    start={(signal) =>
                      downloadTrack(
                        {
                          track_id: targetId,
                          provider: track.source_provider || provider,
                          release_id: release.source_id || release.id,
                        },
                        signal,
                      )
                    }
                  />
                </div>
              </li>
            )
          })}
        </ul>
      </Panel>

      {selectedTrack && (
        <LyricsPanel
          track={selectedTrack}
          open={lyricsOpen}
          onOpenChange={setLyricsOpen}
        />
      )}
    </>
  )
}

function ReleaseHeaderSkeleton() {
  return (
    <div className="flex flex-col gap-6 sm:flex-row sm:items-end" aria-hidden>
      <Skeleton className="size-36 shrink-0 rounded-2xl sm:size-48" />
      <div className="flex-1 space-y-3">
        <Skeleton className="h-4 w-20 rounded-md" />
        <Skeleton className="h-9 w-64 max-w-full rounded-lg" />
        <Skeleton className="h-4 w-40 rounded-md" />
        <Skeleton className="h-11 w-48 rounded-xl" />
      </div>
    </div>
  )
}

export { Release }
