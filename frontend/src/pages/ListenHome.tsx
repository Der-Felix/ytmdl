import { ArrowRightIcon, DownloadIcon, HeadphonesIcon, PauseIcon, PlayIcon } from 'lucide-react'
import type { ReactNode } from 'react'

import { Cover } from '@/components/music/Cover'
import { MixCard } from '@/components/music/MixCard'
import { ReleasePlayButton } from '@/components/music/ReleaseCard'
import { Button, buttonVariants } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { Progress } from '@/components/ui/progress'
import { EmptyState, ErrorState, GridSkeleton, ListSkeleton, LoadingRegion } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import type { AsyncState } from '@/hooks/useAsync'
import { useMixPlayback } from '@/hooks/useMixPlayback'
import { usePlayerActions, usePlayerProgress, usePlayerState } from '@/hooks/usePlayer'
import { libraryReleases } from '@/lib/api/library'
import { listeningHistory } from '@/lib/api/libraryTools'
import type { HistoryTrack } from '@/lib/api/libraryTools'
import { personalMixes } from '@/lib/api/mixes'
import type { PersonalMix } from '@/lib/api/mixes'
import { libraryArtwork } from '@/lib/artwork'
import { Link, paths } from '@/lib/router'
import { formatPlaybackTime, formatRelative, joinArtists } from '@/lib/utils/format'
import type { LibraryRelease } from '@/types/api'

/** Tracks listed under "Zuletzt gehört". */
const HISTORY_LIMIT = 8

/** Releases requested for "Neu in der Mediathek"; the row shows what fits. */
const NEW_RELEASES_LIMIT = 6

/** Mix cards offered; the row shows what fits, "Alle Mixe" has the rest. */
const MIX_LIMIT = 4

/**
 * One-row grids: every child past the column count of the current breakpoint
 * is hidden, so the row never wraps into a second one. The ranges do not
 * overlap, so no rule has to undo another.
 */
// Every breakpoint is spelled out so these also override GridSkeleton's own.
const MIX_ROW =
  'grid grid-cols-2 gap-3 sm:grid-cols-2 sm:gap-6 md:grid-cols-3 lg:grid-cols-3 xl:grid-cols-4 max-md:[&>*:nth-child(n+3)]:hidden md:max-xl:[&>*:nth-child(n+4)]:hidden'
const RELEASE_ROW =
  'grid grid-cols-3 gap-3 sm:grid-cols-3 sm:gap-4 md:grid-cols-4 lg:grid-cols-4 xl:grid-cols-6 max-md:[&>*:nth-child(n+4)]:hidden md:max-xl:[&>*:nth-child(n+5)]:hidden'

/**
 * The start page of the listening area: pick up where you left off, the
 * day's mixes, what you heard last and what arrived in the library.
 *
 * Each section loads on its own and disappears when it has nothing; one that
 * fails shows its error in place and leaves the others alone. Only a library
 * with nothing in any section gets the one empty state, pointing to Laden.
 */
function ListenHome() {
  const { currentTrack } = usePlayerState()
  const mixes = useAsync((signal) => personalMixes(signal), [])
  const history = useAsync((signal) => listeningHistory('recent', signal), [])
  const releases = useAsync(
    async (signal) => (await libraryReleases({ limit: NEW_RELEASES_LIMIT, sort: 'recent', order: 'desc', inLibrary: true, signal })).items,
    [],
  )

  const empty =
    !currentTrack &&
    isEmpty(mixes.state) &&
    isEmpty(history.state) &&
    isEmpty(releases.state)

  return (
    <div className="space-y-10">
      <header className="space-y-1.5">
        <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">Hören</h1>
        <p className="text-sm text-muted-foreground">Weiterhören, deine Mixe und was neu in der Mediathek ist.</p>
      </header>

      {empty ? (
        <Panel>
          <EmptyState
            icon={<HeadphonesIcon />}
            title="Hier gibt es noch nichts zu hören"
            description="Deine Mediathek ist noch leer. Unter „Laden“ suchst du Künstler und Alben und lädst sie herunter – danach erscheinen hier deine Musik, Mixe und dein Verlauf."
            action={
              <Link href={paths.downloadHome()} className={buttonVariants({ variant: 'default', className: 'mt-2' })}>
                <DownloadIcon />
                Zu Laden
              </Link>
            }
          />
        </Panel>
      ) : (
        <>
          {currentTrack && <ContinueListening />}
          <MixesSection state={mixes.state} onRetry={mixes.reload} />
          <HistorySection state={history.state} onRetry={history.reload} />
          <NewReleasesSection state={releases.state} onRetry={releases.reload} />
        </>
      )}
    </div>
  )
}

function isEmpty(state: AsyncState<unknown[]>): boolean {
  return state.status === 'success' && state.data.length === 0
}

/** A section heading with an optional link at its trailing edge. */
function Section({ id, title, action, children }: { id: string; title: string; action?: ReactNode; children: ReactNode }) {
  return (
    <section aria-labelledby={id} className="space-y-3">
      <div className="flex items-center justify-between gap-4">
        <h2 id={id} className="font-heading text-lg font-semibold text-foreground">
          {title}
        </h2>
        {action}
      </div>
      {children}
    </section>
  )
}

/* ----------------------------------------------------------- Weiterhören */

/**
 * The player's current or last session. The player restores it paused from
 * this browser; the button resumes it where it stopped, as the mini player's
 * does.
 */
function ContinueListening() {
  const { currentTrack, status, queue, queueIndex } = usePlayerState()
  const { togglePlayPause } = usePlayerActions()
  if (!currentTrack) return null

  const playing = status === 'playing'
  const artists = currentTrack.artists?.length ? joinArtists(currentTrack.artists) : currentTrack.album_artist

  return (
    <Section id="listen-continue" title="Weiterhören">
      <Panel className="flex flex-col gap-4 p-4 sm:flex-row sm:items-center sm:p-5">
        <div className="flex min-w-0 flex-1 items-center gap-4">
          <Cover
            src={libraryArtwork('tracks', currentTrack.id)}
            fallbackSrc={currentTrack.cover_url}
            alt=""
            className="size-16 shrink-0 rounded-xl sm:size-20"
          />
          <div className="min-w-0 flex-1 space-y-1">
            <p className="truncate font-medium text-foreground">{currentTrack.title}</p>
            <p className="truncate text-sm text-muted-foreground">
              {[artists, currentTrack.album].filter(Boolean).join(' · ')}
            </p>
            {queue.length > 1 && (
              <p className="figures text-xs text-muted-foreground">
                Titel {queueIndex + 1} von {queue.length} in der Warteschlange
              </p>
            )}
            <ResumeProgress />
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button onClick={togglePlayPause} className="flex-1 sm:flex-none">
            {playing ? <PauseIcon /> : <PlayIcon className="fill-current" />}
            {playing ? 'Pause' : 'Weiterhören'}
          </Button>
          <Button variant="outline" nativeButton={false} render={<Link href={paths.player()} />}>
            Zum Player
          </Button>
        </div>
      </Panel>
    </Section>
  )
}

/** Position in the track; separate so the time updates re-render only this. */
function ResumeProgress() {
  const { currentTime, duration } = usePlayerProgress()
  if (!duration) return null
  return (
    <div className="flex max-w-sm items-center gap-3 pt-1">
      <div className="min-w-0 flex-1">
        <Progress value={Math.min(100, (currentTime / duration) * 100)} aria-label="Position im Titel" className="h-1" />
      </div>
      <span className="figures shrink-0 text-xs text-muted-foreground">
        {formatPlaybackTime(currentTime)} / {formatPlaybackTime(duration)}
      </span>
    </div>
  )
}

/* ----------------------------------------------------------- Deine Mixe */

function MixesSection({ state, onRetry }: { state: AsyncState<PersonalMix[]>; onRetry: () => void }) {
  const { playing, playError, onPlay } = useMixPlayback()
  if (isEmpty(state)) return null

  // Genre mixes first, as on "Für dich".
  const mixes =
    state.status === 'success'
      ? [...state.data.filter((m) => m.kind === 'genre'), ...state.data.filter((m) => m.kind !== 'genre')].slice(0, MIX_LIMIT)
      : []

  return (
    <Section
      id="listen-mixes"
      title="Deine Mixe"
      action={
        <Button variant="ghost" size="sm" nativeButton={false} render={<Link href={paths.mixes()} />}>
          Alle Mixe
          <ArrowRightIcon />
        </Button>
      }
    >
      {state.status === 'loading' && (
        <LoadingRegion label="Mixe werden geladen">
          <GridSkeleton tiles={MIX_LIMIT} className={MIX_ROW} />
        </LoadingRegion>
      )}
      {state.status === 'error' && <ErrorState error={state.error} onRetry={onRetry} className="py-8" />}
      {playError && (
        <p role="alert" className="text-sm text-destructive">
          {playError}
        </p>
      )}
      {mixes.length > 0 && (
        <div className={MIX_ROW}>
          {mixes.map((mix) => (
            <MixCard key={mix.id} mix={mix} busy={playing === mix.id} disabled={playing !== null} onPlay={onPlay} />
          ))}
        </div>
      )}
    </Section>
  )
}

/* -------------------------------------------------------- Zuletzt gehört */

function HistorySection({ state, onRetry }: { state: AsyncState<HistoryTrack[]>; onRetry: () => void }) {
  const { playTrack } = usePlayerActions()
  if (isEmpty(state)) return null

  const tracks = state.status === 'success' ? state.data.slice(0, HISTORY_LIMIT) : []

  return (
    <Section id="listen-history" title="Zuletzt gehört">
      {state.status === 'loading' && (
        <LoadingRegion label="Verlauf wird geladen">
          <ListSkeleton rows={4} />
        </LoadingRegion>
      )}
      {state.status === 'error' && <ErrorState error={state.error} onRetry={onRetry} className="py-8" />}
      {tracks.length > 0 && (
        <Panel className="p-2">
          <ul className="grid gap-x-2 lg:grid-cols-2">
            {tracks.map((track, index) => (
              <li key={track.id}>
                <button
                  type="button"
                  onClick={() => playTrack(track, tracks, index, { kind: 'history', title: 'Zuletzt gehört' })}
                  aria-label={`${track.title} abspielen`}
                  className="focus-ring group flex w-full items-center gap-3 rounded-xl px-2 py-2 text-left transition-colors duration-(--duration-fast) hover:bg-muted"
                >
                  <span className="relative shrink-0">
                    <Cover
                      src={libraryArtwork('tracks', track.id)}
                      fallbackSrc={track.cover_url}
                      alt=""
                      className="size-10 rounded-lg"
                    />
                    <span
                      aria-hidden
                      className="absolute inset-0 flex items-center justify-center rounded-lg bg-overlay/60 text-foreground opacity-0 transition-opacity duration-(--duration-fast) group-hover:opacity-100 group-focus-visible:opacity-100"
                    >
                      <PlayIcon className="size-4 fill-current" />
                    </span>
                  </span>
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium text-foreground">{track.title}</span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {track.artists?.length ? joinArtists(track.artists) : track.album_artist}
                    </span>
                  </span>
                  <span className="shrink-0 text-xs text-muted-foreground">{formatRelative(track.last_played_at)}</span>
                </button>
              </li>
            ))}
          </ul>
        </Panel>
      )}
    </Section>
  )
}

/* --------------------------------------------------- Neu in der Mediathek */

function NewReleasesSection({ state, onRetry }: { state: AsyncState<LibraryRelease[]>; onRetry: () => void }) {
  if (isEmpty(state)) return null

  return (
    <Section
      id="listen-new"
      title="Neu in der Mediathek"
      action={
        <Button variant="ghost" size="sm" nativeButton={false} render={<Link href={paths.library()} />}>
          Bibliothek
          <ArrowRightIcon />
        </Button>
      }
    >
      {state.status === 'loading' && (
        <LoadingRegion label="Neuzugänge werden geladen">
          <GridSkeleton tiles={NEW_RELEASES_LIMIT} className={RELEASE_ROW} />
        </LoadingRegion>
      )}
      {state.status === 'error' && <ErrorState error={state.error} onRetry={onRetry} className="py-8" />}
      {state.status === 'success' && (
        <ul className={RELEASE_ROW}>
          {state.data.map((release) => (
            <li key={release.id} className="group relative min-w-0">
              <Link
                href={paths.libraryRelease(release.id)}
                className="focus-ring block space-y-2 rounded-xl"
              >
                <Cover
                  src={libraryArtwork('releases', release.id)}
                  fallbackSrc={release.cover_url}
                  alt=""
                  className="w-full rounded-xl"
                />
                <span className="block px-0.5">
                  <span className="block truncate text-sm font-medium text-foreground group-hover:underline group-hover:underline-offset-4">
                    {release.title}
                  </span>
                  <span className="block truncate text-xs text-muted-foreground">{release.album_artist}</span>
                </span>
              </Link>
              {/* Over the square cover, beside the link rather than inside it. */}
              <div className="pointer-events-none absolute inset-x-0 top-0 aspect-square [&>button]:pointer-events-auto">
                {/* Three covers share a phone's width here; the button stays in proportion. */}
                <ReleasePlayButton id={release.id} title={release.title} className="max-sm:right-1.5 max-sm:bottom-1.5 max-sm:size-9" />
              </div>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

export { ListenHome }
