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
import { isTerminal, section } from '@/lib/api/jobs'
import {
  libraryReleases,
  libraryStats,
} from '@/lib/api/library'
import { getHealth } from '@/lib/api/settings'
import { Link, paths } from '@/lib/router'
import { formatNumber, formatRelative } from '@/lib/utils/format'
import type { Health, Job } from '@/types/api'

/**
 * The entry point: search first, then what is currently happening and what is
 * already there. Deliberately not an admin dashboard — the server metrics get
 * one compact panel at the end, the music gets everything above it.
 */
function Dashboard() {
  const jobs = useJobs({ limit: 50 })
  const currentTracks = useCurrentTracks()

  const allJobs = jobs.state.status === 'success' ? jobs.state.data : []
  const active = allJobs.filter((job) => section(job) === 'active' || section(job) === 'queued')
  const recent = allJobs.filter(isTerminal).slice(0, 4)

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
            active.length > 0
              ? `${formatNumber(active.length)} ${active.length === 1 ? 'Job läuft' : 'Jobs laufen'}`
              : undefined
          }
          action={
            <Button
              variant="ghost"
              size="sm"
              render={<Link href={paths.downloads()} />}
            >
              Alle Downloads
              <ArrowRightIcon />
            </Button>
          }
        />

        {jobs.state.status === 'loading' && (
          <LoadingRegion label="Downloads werden geladen">
            <ListSkeleton rows={2} />
          </LoadingRegion>
        )}

        {jobs.state.status === 'error' && (
          <Panel>
            <ErrorState error={jobs.state.error} onRetry={jobs.reload} />
          </Panel>
        )}

        {jobs.state.status === 'success' && active.length === 0 && (
          <Panel className="flex items-center gap-3 px-4 py-3.5">
            <span
              aria-hidden
              className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-border bg-white/4 text-muted-foreground"
            >
              <DownloadIcon className="size-4" />
            </span>
            <p className="text-sm text-muted-foreground">
              Zurzeit läuft kein Download.
            </p>
          </Panel>
        )}

        {active.length > 0 && (
          <div className="space-y-3">
            {active.map((job) => (
              <JobCard
                key={job.id}
                job={job}
                currentTrack={currentTracks[job.id]}
                onCancelled={jobs.reload}
              />
            ))}
          </div>
        )}
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        <RecentDownloads jobs={recent} loading={jobs.state.status === 'loading'} />
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
        YTMDL
      </h1>
      <p className="text-sm text-muted-foreground">
        Diskografien auflösen, Musik herunterladen, Bibliothek verwalten.
      </p>
    </header>
  )
}

/**
 * The last finished jobs.
 *
 * This reads from the job history rather than from the library listing: the
 * catalogue is ordered by year and title, so it cannot answer "what arrived
 * most recently". The jobs can.
 */
function RecentDownloads({ jobs, loading }: { jobs: Job[]; loading: boolean }) {
  return (
    <Panel className="flex flex-col gap-4 p-5">
      <PanelHeader
        title="Zuletzt heruntergeladen"
        description="Die zuletzt abgeschlossenen Jobs"
      />

      {loading && <ListSkeleton rows={3} />}

      {!loading && jobs.length === 0 && (
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
                className="flex size-9 shrink-0 items-center justify-center rounded-xl border border-border bg-white/4 text-muted-foreground"
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
          <Button variant="ghost" size="sm" render={<Link href={paths.library()} />}>
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
                className="rounded-xl border border-border bg-white/3 p-3.5"
              >
                <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
                  <entry.icon className="size-3.5" />
                  {entry.label}
                </dt>
                <dd className="mt-1.5 font-heading text-xl font-semibold tabular-nums text-foreground">
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
              <div className="h-2 w-full overflow-hidden rounded-full bg-white/5 flex">
                <div
                  style={{ width: `${syncedPct}%` }}
                  title={`Synchronisiert: ${syncedCount}`}
                  className="bg-success transition-all duration-500"
                />
                <div
                  style={{ width: `${plainPct}%` }}
                  title={`Text: ${plainCount}`}
                  className="bg-primary transition-all duration-500"
                />
                <div
                  style={{ width: `${instrumentalPct}%` }}
                  title={`Instrumental: ${instrumentalCount}`}
                  className="bg-neutral-600 transition-all duration-500"
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
          <Button variant="ghost" size="sm" render={<Link href={paths.settings()} />}>
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

export { Dashboard }
