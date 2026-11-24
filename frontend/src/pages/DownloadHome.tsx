import {
  ActivityIcon,
  ArrowRightIcon,
  CheckCircle2Icon,
  DiscIcon,
  DownloadIcon,
  MusicIcon,
  SparklesIcon,
  UsersIcon,
} from 'lucide-react'

import { useMemo } from 'react'

import { JobCard } from '@/components/downloads/JobCard'
import { Cover } from '@/components/music/Cover'
import { SearchField } from '@/components/music/SearchField'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { Button } from '@/components/ui/button'
import {
  EmptyState,
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { Skeleton } from '@/components/ui/skeleton'
import { useAsync } from '@/hooks/useAsync'
import { useCurrentTracks, useJobs } from '@/hooks/useJobs'
import { useQueueSummary } from '@/hooks/useQueueSummary'
import {
  libraryReleases,
  libraryStats,
} from '@/lib/api/library'
import { getHealth } from '@/lib/api/settings'
import { Link, paths } from '@/lib/router'
import { formatNumber, formatRelative } from '@/lib/utils/format'
import type { Health, Job, NextUpJob } from '@/types/api'

/** How many open jobs the start page lists; the rest live under Downloads. */
export const ACTIVE_LIMIT = 5

/** How many finished jobs "Zuletzt geladen" lists. */
const RECENT_LIMIT = 4

/**
 * The start page of the download area: search first, then a short look at
 * what is running and what arrived. The full queue — it can hold thousands of
 * jobs — belongs to the Downloads page; this page names its size and links
 * there. The server metrics get one compact panel at the end.
 */
function DownloadHome() {
  // Only what is shown is requested: the running jobs, at most ACTIVE_LIMIT,
  // and the last finished ones. Both lists stay live through the event stream.
  const running = useJobs({ view: 'active', limit: ACTIVE_LIMIT })
  const finished = useJobs({ view: 'done', limit: RECENT_LIMIT })
  const { state: summaryState } = useQueueSummary()
  const currentTracks = useCurrentTracks()

  const runningJobs = useMemo(() => (running.state.status === 'success' ? running.state.data : []), [running.state])
  const recent = finished.state.status === 'success' ? finished.state.data : []

  const summary = summaryState.status === 'success' ? summaryState.data : undefined
  const openJobsCount = useMemo(() => {
    if (!summary) return undefined
    return (summary.active_jobs ?? 0) + (summary.queued_jobs ?? 0)
  }, [summary])

  // What the dispatcher picks next fills the list up to ACTIVE_LIMIT. The
  // summary already carries it in dispatcher order; running jobs appear there
  // too and are left out.
  const nextUp = useMemo(() => {
    const shown = new Set(runningJobs.map((job) => job.id))
    return (summary?.next ?? [])
      .filter((job) => !shown.has(job.job_id))
      .slice(0, Math.max(0, ACTIVE_LIMIT - runningJobs.length))
  }, [summary, runningJobs])

  const listed = runningJobs.length + nextUp.length
  const more = openJobsCount !== undefined ? Math.max(0, openJobsCount - listed) : 0

  return (
    <div className="space-y-7">
      <Greeting />

      <section aria-label="Suche">
        <SearchField size="hero" />
        <p className="mt-3 px-1 text-sm text-muted-foreground">
          Name eines Künstlers oder eines Albums — oder ein Link von YouTube
          Music, YouTube oder Spotify.
        </p>
      </section>

      <section aria-labelledby="active-downloads" className="space-y-3">
        <PanelHeader
          title={<span id="active-downloads">Aktive Downloads</span>}
          description={
            openJobsCount !== undefined && openJobsCount > 0
              ? `${formatNumber(openJobsCount)} ${openJobsCount === 1 ? 'offener Job' : 'offene Jobs'}`
              : undefined
          }
          action={
            <Button
              variant="ghost"
              size="sm"
              nativeButton={false}
              render={<Link href={paths.downloads()} />}
            >
              Alle Downloads
              <ArrowRightIcon />
            </Button>
          }
        />

        {running.state.status === 'loading' && (
          <LoadingRegion label="Downloads werden geladen">
            <ListSkeleton rows={2} />
          </LoadingRegion>
        )}

        {running.state.status === 'error' && (
          <Panel>
            <ErrorState error={running.state.error} onRetry={running.reload} />
          </Panel>
        )}

        {running.state.status === 'success' && listed === 0 && (
          <Panel className="flex items-center gap-3 px-4 py-3.5">
            <span
              aria-hidden
              className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-border bg-muted text-muted-foreground"
            >
              <DownloadIcon className="size-4" />
            </span>
            <p className="text-sm text-muted-foreground">
              {openJobsCount
                ? 'Gerade läuft kein Download; die Warteschlange ist pausiert oder wartet.'
                : 'Zurzeit läuft kein Download.'}
            </p>
          </Panel>
        )}

        {runningJobs.length > 0 && (
          <div className="space-y-3">
            {runningJobs.map((job) => (
              <JobCard
                key={job.id}
                job={job}
                currentTrack={currentTracks[job.id]}
                onCancelled={running.reload}
              />
            ))}
          </div>
        )}

        {nextUp.length > 0 && <NextUp jobs={nextUp} />}

        {more > 0 && (
          <p className="px-1 text-sm text-muted-foreground">
            <Link
              href={paths.downloads()}
              className="focus-ring rounded text-foreground underline-offset-4 hover:underline"
            >
              {`${formatNumber(more)} ${more === 1 ? 'weiterer Job' : 'weitere Jobs'}`}
            </Link>{' '}
            in der Warteschlange
          </p>
        )}
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        <RecentDownloads
          jobs={recent}
          loading={finished.state.status === 'loading'}
          error={finished.state.status === 'error' ? finished.state.error : undefined}
          onRetry={finished.reload}
        />
        <LibraryOverview />
      </div>

      <ServerStatus />
    </div>
  )
}

function Greeting() {
  return (
    <header className="space-y-1.5">
      <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
        Laden
      </h1>
      <p className="text-sm text-muted-foreground">
        Diskografien auflösen, Musik herunterladen, Bibliothek füllen.
      </p>
    </header>
  )
}

/** The jobs the dispatcher takes next, one line each. */
function NextUp({ jobs }: { jobs: NextUpJob[] }) {
  return (
    <Panel className="px-4 py-3">
      <h3 className="text-xs font-medium text-muted-foreground">Als Nächstes</h3>
      <ul className="mt-1 divide-y divide-border">
        {jobs.map((job) => (
          <li key={job.job_id}>
            <Link
              href={paths.downloads(job.job_id)}
              className="focus-ring flex items-center gap-3 rounded-lg py-2"
            >
              <Cover src={job.cover_url} alt="" className="size-8 shrink-0 rounded-lg" />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-sm text-foreground">{job.release || 'Unbenannter Job'}</span>
                {job.artist && (
                  <span className="block truncate text-xs text-muted-foreground">{job.artist}</span>
                )}
              </span>
              <span className="figures shrink-0 text-xs text-muted-foreground">
                {formatNumber(job.open_tracks)} / {formatNumber(job.total_tracks)}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </Panel>
  )
}

/**
 * The last finished jobs.
 *
 * This reads from the job history rather than from the library listing: the
 * catalogue is ordered by year and title, so it cannot answer "what arrived
 * most recently". The jobs can.
 */
function RecentDownloads({
  jobs,
  loading,
  error,
  onRetry,
}: {
  jobs: Job[]
  loading: boolean
  error?: unknown
  onRetry: () => void
}) {
  return (
    <Panel className="flex flex-col gap-4 p-5">
      <PanelHeader
        title="Zuletzt geladen"
        description="Die zuletzt abgeschlossenen Jobs"
      />

      {loading && <ListSkeleton rows={3} />}

      {error !== undefined && <ErrorState error={error} onRetry={onRetry} className="py-7" />}

      {!loading && error === undefined && jobs.length === 0 && (
        <EmptyState
          icon={<CheckCircle2Icon />}
          title="Noch nichts heruntergeladen"
          description="Abgeschlossene Downloads erscheinen hier."
          className="py-7"
        />
      )}

      {jobs.length > 0 && (
        <ul className="divide-y divide-border">
          {jobs.map((job) => (
            <li key={job.id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
              <span
                aria-hidden
                className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-border bg-muted text-muted-foreground"
              >
                <MusicIcon className="size-4" />
              </span>
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium text-foreground">
                  {job.label || 'Unbenannter Job'}
                </p>
                <p className="truncate text-xs text-muted-foreground">
                  {formatNumber(job.completed)} von {formatNumber(job.total)} Tracks
                  {job.finished_at && ` · ${formatRelative(job.finished_at)}`}
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

/**
 * How much is in the library.
 *
 * The list endpoints report only how many rows they returned, not how many
 * exist, so the counts are requested at the maximum page size and shown as
 * "500+" when that ceiling is reached. Inventing an exact total would be worse
 * than admitting the limit.
 */
/**
 * How much is in the library and recently added releases.
 *
 * Uses SQL aggregates from libraryStats() and fetches the 5 most recently
 * added releases via libraryReleases({ limit: 5, sort: 'recent' }).
 */
function LibraryOverview() {
  const { state, reload } = useAsync(
    async (signal) => {
      const [stats, recentReleases] = await Promise.all([
        libraryStats({ signal }),
        libraryReleases({ limit: 5, sort: 'recent', order: 'desc', signal }),
      ])
      return {
        stats,
        recentReleases: recentReleases.items,
      }
    },
    [],
  )

  const stats = state.data?.stats
  const totalTracks = stats?.total_tracks ?? 0
  const coverage = stats?.lyrics_coverage

  const syncedCount = coverage?.available_synced ?? 0
  const plainCount = coverage?.available_plain ?? 0
  const instrumentalCount = coverage?.instrumental ?? 0

  const syncedPct = totalTracks > 0 ? (syncedCount / totalTracks) * 100 : 0
  const plainPct = totalTracks > 0 ? (plainCount / totalTracks) * 100 : 0
  const instrumentalPct = totalTracks > 0 ? (instrumentalCount / totalTracks) * 100 : 0

  const entries = [
    { label: 'Künstler', icon: UsersIcon, value: stats?.total_artists },
    { label: 'Releases', icon: DiscIcon, value: stats?.total_releases },
    { label: 'Titel', icon: MusicIcon, value: stats?.total_tracks },
  ]

  return (
    <Panel className="flex flex-col gap-4 p-5">
      <PanelHeader
        title="Bibliothek"
        action={
          <Button variant="ghost" size="sm" nativeButton={false} render={<Link href={paths.library()} />}>
            Öffnen
            <ArrowRightIcon />
          </Button>
        }
      />

      {state.status === 'error' ? (
        <ErrorState error={state.error} onRetry={reload} className="py-8" />
      ) : (
        <div className="space-y-4">
          <dl className="grid grid-cols-3 gap-3">
            {entries.map((entry) => (
              <div
                key={entry.label}
                className="rounded-xl border border-border bg-muted p-3.5"
              >
                <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <entry.icon className="size-3.5" />
                  {entry.label}
                </dt>
                <dd className="mt-1.5 figures text-xl font-semibold text-foreground">
                  {state.status === 'loading' ? (
                    <Skeleton className="h-6 w-12 rounded-md" />
                  ) : (
                    formatNumber(entry.value ?? 0)
                  )}
                </dd>
              </div>
            ))}
          </dl>

          {/* Compact Lyrics Coverage Bar */}
          {state.status === 'success' && totalTracks > 0 && (
            <div className="space-y-1.5 pt-1">
              <div className="flex items-center justify-between text-xs text-muted-foreground">
                <span className="flex items-center gap-1 font-medium text-foreground/80">
                  <SparklesIcon className="size-3 text-accent" />
                  Lyrics-Abdeckung
                </span>
                <span>
                  {formatNumber(syncedCount + plainCount)} / {formatNumber(totalTracks)} ({Math.round(((syncedCount + plainCount) / totalTracks) * 100)}%)
                </span>
              </div>
              <div className="h-2 w-full overflow-hidden rounded-full bg-muted flex">
                <div
                  style={{ width: `${syncedPct}%` }}
                  title={`Synchronisiert: ${syncedCount}`}
                  className="bg-success transition-[width] duration-500"
                />
                <div
                  style={{ width: `${plainPct}%` }}
                  title={`Text: ${plainCount}`}
                  className="bg-primary transition-[width] duration-500"
                />
                <div
                  style={{ width: `${instrumentalPct}%` }}
                  title={`Instrumental: ${instrumentalCount}`}
                  className="bg-input transition-[width] duration-500"
                />
              </div>
            </div>
          )}
        </div>
      )}

      {/* Recently Added Release Covers */}
      {state.status === 'success' && state.data.recentReleases.length > 0 && (
        <div className="space-y-2 pt-2">
          <div className="text-xs font-medium text-muted-foreground">Zuletzt hinzugefügt</div>
          <ul className="flex gap-2.5 overflow-hidden">
            {state.data.recentReleases.map((release) => (
              <li key={release.id} className="min-w-0 flex-1">
                <Link
                  href={paths.libraryRelease(release.id)}
                  title={`${release.title} · ${release.album_artist}`}
                  className="focus-ring block rounded-xl overflow-hidden group"
                >
                  <Cover src={release.cover_url} alt="" className="w-full rounded-xl group-hover:scale-105 transition-transform" />
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}

      {state.status === 'success' && stats?.total_artists === 0 && (
        <p className="text-sm text-muted-foreground">
          Die Bibliothek ist leer.{' '}
          <Link
            href={paths.discover()}
            className="focus-ring rounded text-primary underline-offset-4 hover:underline"
          >
            Etwas suchen
          </Link>
          .
        </p>
      )}
    </Panel>
  )
}

function ServerStatus() {
  const { state, reload } = useAsync((signal) => getHealth({ signal }), [])

  return (
    <Panel className="flex flex-col gap-4 p-5">
      <PanelHeader
        title="Serverstatus"
        action={
          <Button variant="ghost" size="sm" nativeButton={false} render={<Link href={paths.settings()} />}>
            Einstellungen
            <ArrowRightIcon />
          </Button>
        }
      />

      {state.status === 'loading' && <Skeleton className="h-9 w-full rounded-xl" />}

      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} className="py-8" />
      )}

      {state.status === 'success' && <HealthSummary health={state.data} />}
    </Panel>
  )
}

function HealthSummary({ health }: { health: Health }) {
  const entries = Object.entries(health.checks)
  const failing = entries.filter(([, check]) => !check.ok)

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
        <span className="inline-flex items-center gap-2 text-sm">
          <ActivityIcon
            className={
              health.status === 'ok'
                ? 'size-4 text-success'
                : health.status === 'degraded'
                  ? 'size-4 text-warning'
                  : 'size-4 text-destructive'
            }
          />
          <span className="font-medium text-foreground">
            {health.status === 'ok'
              ? 'Alles betriebsbereit'
              : health.status === 'degraded'
                ? 'Eingeschränkt betriebsbereit'
                : 'Nicht betriebsbereit'}
          </span>
        </span>
        <span className="text-sm text-muted-foreground">Version {health.version}</span>
      </div>

      <ul className="flex flex-wrap gap-x-4 gap-y-1.5">
        {entries.map(([name, check]) => (
          <li key={name} className="flex items-center gap-1.5 text-xs">
            <span
              aria-hidden
              className={`size-1.5 rounded-full ${check.ok ? 'bg-success' : 'bg-destructive'}`}
            />
            <span className="text-muted-foreground">{name}</span>
          </li>
        ))}
      </ul>

      {failing.map(([name, check]) => (
        <p key={name} className="text-xs leading-relaxed text-warning">
          {name}: {check.detail ?? 'nicht verfügbar'}
        </p>
      ))}
    </div>
  )
}

export { DownloadHome }
