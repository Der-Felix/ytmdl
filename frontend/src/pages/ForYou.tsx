import { useCallback, useMemo, useState } from 'react'
import type { MouseEvent } from 'react'
import { ArrowLeft, ListPlus, Loader2, Play, Shuffle, Sparkles } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { TrackDetailDialog } from '@/components/music/TrackDetailDialog'
import { TracksTable } from '@/components/music/TracksTable'
import { Button, buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { EmptyState, ErrorState, GridSkeleton, ListSkeleton, LoadingRegion } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { usePlayerActions } from '@/hooks/usePlayer'
import { ApiError } from '@/lib/api/client'
import { personalMix, personalMixes } from '@/lib/api/mixes'
import type { PersonalMix } from '@/lib/api/mixes'
import { libraryArtwork } from '@/lib/artwork'
import { Link, paths } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatDuration, pluralize } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'

/** The small line above a mix title: its genre or where it comes from. */
function mixLabel(mix: PersonalMix): string {
  switch (mix.kind) {
    case 'genre':
      return mix.genre || 'Genre-Mix'
    case 'on_repeat':
      return 'Meistgehört'
    case 'rediscover':
      return 'Aus deinem Verlauf'
    case 'unheard':
      return 'Entdecken'
    case 'new':
      return 'Neuzugänge'
    default:
      return 'Mix'
  }
}

function shuffled<T>(list: T[]): T[] {
  const out = [...list]
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[out[i], out[j]] = [out[j]!, out[i]!]
  }
  return out
}

/**
 * A mix cover: a collage of up to four covers under a scrim in the two brand
 * colours, with the title set on it. The scrim keeps the title readable on
 * any artwork; it is the page's one decorative element.
 */
function MixCover({ mix, showTitle = true, className }: { mix: PersonalMix; showTitle?: boolean; className?: string }) {
  const ids = mix.cover_track_ids ?? []
  const tiles = ids.length > 1 ? Array.from({ length: 4 }, (_, i) => ids[i % ids.length]!) : ids
  return (
    <div className={cn('relative isolate aspect-square overflow-hidden rounded-xl border border-border bg-muted', className)}>
      {tiles.length > 0 ? (
        <div className={cn('grid size-full', tiles.length > 1 && 'grid-cols-2 grid-rows-2')} aria-hidden>
          {tiles.map((id, index) => (
            <Cover key={index} src={libraryArtwork('tracks', id)} alt="" className="size-full aspect-auto rounded-none border-0" />
          ))}
        </div>
      ) : (
        <div className={cn('flex w-full items-center justify-center text-muted-foreground', showTitle ? 'h-3/5' : 'h-full')} aria-hidden>
          <Sparkles className="size-12" strokeWidth={1.5} />
        </div>
      )}
      {showTitle && (
        <>
          <div className="mix-scrim pointer-events-none absolute inset-0" aria-hidden />
          <div className="absolute inset-x-0 bottom-0 space-y-0.5 p-2.5 sm:p-4">
            <p className="truncate text-xs font-semibold tracking-wide text-accent-foreground uppercase">{mixLabel(mix)}</p>
            <h3 className="font-heading text-base leading-tight font-semibold text-foreground sm:text-xl">{mix.title}</h3>
          </div>
        </>
      )}
    </div>
  )
}

function MixCard({ mix, busy, disabled, onPlay }: { mix: PersonalMix; busy: boolean; disabled: boolean; onPlay: (event: MouseEvent, mix: PersonalMix) => void }) {
  return (
    <article className="panel panel-interactive relative flex min-w-0 flex-col p-2.5 sm:p-3">
      <Link href={paths.mix(mix.id)} className="focus-ring block rounded-xl after:absolute after:inset-0 after:rounded-2xl after:content-['']">
        <MixCover mix={mix} />
      </Link>
      <p className="mt-3 line-clamp-2 px-1 text-xs text-muted-foreground sm:text-sm">{mix.description}</p>
      <div className="mt-auto flex items-center justify-between gap-2 px-1 pt-3">
        <span className="figures text-xs text-muted-foreground">{pluralize(mix.track_count, 'Titel', 'Titel')}</span>
        <Button
          variant="secondary"
          size="sm"
          className="relative z-10 gap-2"
          disabled={disabled}
          aria-label={`${mix.title} abspielen`}
          onClick={(event) => onPlay(event, mix)}
        >
          {busy ? <Loader2 className="size-4 animate-spin" /> : <Play className="size-4 fill-current" />}
          <span className="max-sm:sr-only">Abspielen</span>
        </Button>
      </div>
    </article>
  )
}

function MixSection({ title, mixes, playing, onPlay }: { title: string; mixes: PersonalMix[]; playing: string | null; onPlay: (event: MouseEvent, mix: PersonalMix) => void }) {
  if (mixes.length === 0) return null
  return (
    <section className="space-y-3" aria-label={title}>
      <h2 className="font-heading text-lg font-semibold text-foreground">{title}</h2>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-4 sm:gap-6">
        {mixes.map((mix) => (
          <MixCard key={mix.id} mix={mix} busy={playing === mix.id} disabled={playing !== null} onPlay={onPlay} />
        ))}
      </div>
    </section>
  )
}

/** "Für dich": the user's daily mixes. */
export function ForYou() {
  const { playAlbum } = usePlayerActions()
  const { state, reload } = useAsync((signal) => personalMixes(signal), [])
  const [playing, setPlaying] = useState<string | null>(null)
  const [playError, setPlayError] = useState('')
  const mixes = useMemo(() => (state.status === 'success' ? state.data : []), [state])
  const today = useMemo(() => new Date().toLocaleDateString('de-DE', { weekday: 'long', day: 'numeric', month: 'long' }), [])

  const handlePlay = useCallback(
    async (event: MouseEvent, mix: PersonalMix) => {
      event.preventDefault()
      event.stopPropagation()
      setPlayError('')
      setPlaying(mix.id)
      try {
        const detail = await personalMix(mix.id)
        if (detail.tracks.length > 0) playAlbum(detail.tracks)
      } catch (error) {
        setPlayError(error instanceof Error ? error.message : 'Mix konnte nicht geladen werden.')
      } finally {
        setPlaying(null)
      }
    },
    [playAlbum],
  )

  return (
    <div className="space-y-8 pb-28">
      <header className="space-y-1">
        <h1 className="flex items-center gap-3 font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
          <Sparkles aria-hidden className="size-7 text-primary" />
          Für dich
        </h1>
        <p className="text-sm text-muted-foreground">
          Täglich neu gemischt aus deiner Mediathek und deinem Hörverlauf · <span className="text-foreground/80">{today}</span>
        </p>
      </header>

      {state.status === 'loading' && (
        <LoadingRegion label="Mixe werden zusammengestellt">
          <GridSkeleton tiles={8} className="grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-3 xl:grid-cols-4 sm:gap-6" />
        </LoadingRegion>
      )}

      {state.status === 'error' && <ErrorState error={state.error} onRetry={reload} />}

      {state.status === 'success' && mixes.length === 0 && (
        <EmptyState
          icon={<Sparkles />}
          title="Noch keine Mixe"
          description="Mixe entstehen aus der Musik in deiner Mediathek und aus dem, was du hörst. Jeder Mix braucht mindestens zehn passende Titel – mit mehr Musik und Hörverlauf erscheinen sie hier und wechseln täglich."
          action={
            <Link href={paths.library()} className={buttonVariants({ variant: 'outline', className: 'mt-2' })}>
              Zur Bibliothek
            </Link>
          }
        />
      )}

      {playError && (
        <p role="alert" className="text-sm text-destructive">
          {playError}
        </p>
      )}

      <MixSection title="Deine Mixe" mixes={mixes.filter((m) => m.kind === 'genre')} playing={playing} onPlay={(e, m) => void handlePlay(e, m)} />
      <MixSection title="Aus deinem Hören" mixes={mixes.filter((m) => m.kind !== 'genre')} playing={playing} onPlay={(e, m) => void handlePlay(e, m)} />
    </div>
  )
}

type SortField = '' | 'title' | 'artist' | 'album' | 'duration' | 'track_number'

function sortValue(track: LibraryTrack, field: SortField): string | number {
  switch (field) {
    case 'title':
      return track.title.toLowerCase()
    case 'artist':
      return (track.artists?.[0] || track.album_artist || '').toLowerCase()
    case 'album':
      return (track.album || '').toLowerCase()
    case 'duration':
      return track.duration_ms || 0
    case 'track_number':
      return track.track_number || 0
    default:
      return 0
  }
}

/** One mix with its tracks. */
export function MixDetail({ id }: { id: string }) {
  const { playAlbum, addToQueue } = usePlayerActions()
  const { state, reload } = useAsync((signal) => personalMix(id, signal), [id])
  const [sort, setSort] = useState<SortField>('')
  const [order, setOrder] = useState<'asc' | 'desc'>('asc')
  const [selectedTrackId, setSelectedTrackId] = useState<string | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)

  const tracks = useMemo(() => {
    const list = state.status === 'success' ? [...state.data.tracks] : []
    if (sort === '') return list
    return list.sort((a, b) => {
      const x = sortValue(a, sort),
        y = sortValue(b, sort)
      if (x === y) return 0
      return (x < y ? -1 : 1) * (order === 'asc' ? 1 : -1)
    })
  }, [state, sort, order])
  const totalMs = useMemo(() => tracks.reduce((sum, t) => sum + (t.duration_ms || 0), 0), [tracks])

  const handleSortChange = (field: string) => {
    if (sort === field) setOrder((o) => (o === 'asc' ? 'desc' : 'asc'))
    else {
      setSort(field as SortField)
      setOrder('asc')
    }
  }

  const back = (
    <Link href={paths.mixes()} className="focus-ring inline-flex items-center gap-1.5 rounded-md text-sm text-muted-foreground hover:text-foreground">
      <ArrowLeft className="size-4" aria-hidden />
      Für dich
    </Link>
  )

  if (state.status === 'loading') {
    return (
      <div className="space-y-6 pb-28">
        {back}
        <LoadingRegion label="Mix wird geladen">
          <div className="panel flex flex-col gap-6 p-4 sm:flex-row sm:items-end sm:p-6">
            <Skeleton className="aspect-square w-40 rounded-xl sm:w-48" />
            <div className="flex-1 space-y-3">
              <Skeleton className="h-4 w-24" />
              <Skeleton className="h-9 w-56" />
              <Skeleton className="h-4 w-72 max-w-full" />
            </div>
          </div>
          <ListSkeleton rows={6} className="mt-6" />
        </LoadingRegion>
      </div>
    )
  }

  if (state.status === 'error') {
    const gone = state.error instanceof ApiError && state.error.status === 404
    return (
      <div className="space-y-6 pb-28">
        {back}
        {gone ? (
          <EmptyState
            icon={<Sparkles />}
            title="Diesen Mix gibt es heute nicht"
            description="Mixe werden täglich neu zusammengestellt. Öffne die Übersicht, um die aktuellen Mixe zu sehen."
            action={
              <Link href={paths.mixes()} className={buttonVariants({ variant: 'outline', className: 'mt-2' })}>
                Zu deinen Mixen
              </Link>
            }
          />
        ) : (
          <ErrorState error={state.error} onRetry={reload} />
        )}
      </div>
    )
  }

  const mix = state.data
  return (
    <div className="space-y-6 pb-28">
      {back}
      <header className="panel flex flex-col gap-6 p-4 sm:flex-row sm:items-end sm:p-6">
        <MixCover mix={mix} showTitle={false} className="w-40 shrink-0 sm:w-48" />
        <div className="min-w-0 flex-1 space-y-2">
          <p className="text-xs font-semibold tracking-wide text-accent-foreground uppercase">{mixLabel(mix)}</p>
          <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-4xl">{mix.title}</h1>
          <p className="max-w-xl text-sm text-muted-foreground">{mix.description}</p>
          <p className="figures text-xs text-muted-foreground">
            {pluralize(tracks.length, 'Titel', 'Titel')} · {formatDuration(totalMs)}
          </p>
          <div className="flex flex-wrap items-center gap-2 pt-2">
            <Button variant="default" disabled={!tracks.length} onClick={() => playAlbum(tracks)}>
              <Play className="size-4 fill-current" />
              Abspielen
            </Button>
            <Button disabled={!tracks.length} onClick={() => playAlbum(shuffled(tracks))}>
              <Shuffle className="size-4" />
              Zufällig
            </Button>
            <Button disabled={!tracks.length} onClick={() => addToQueue(tracks)}>
              <ListPlus className="size-4" />
              Zur Warteschlange
            </Button>
            {sort !== '' && (
              <Button variant="ghost" size="sm" onClick={() => setSort('')}>
                Mix-Reihenfolge
              </Button>
            )}
          </div>
        </div>
      </header>

      {tracks.length === 0 ? (
        <EmptyState icon={<Sparkles />} title="Keine Titel mehr verfügbar" description="Die Titel dieses Mixes sind nicht mehr in der Mediathek." />
      ) : (
        <TracksTable
          tracks={tracks}
          sort={sort}
          order={order}
          onSortChange={handleSortChange}
          onTrackSelect={(t) => {
            setSelectedTrackId(t.id)
            setDetailOpen(true)
          }}
          fullQueue={tracks}
        />
      )}

      <TrackDetailDialog trackId={selectedTrackId} open={detailOpen} onOpenChange={setDetailOpen} />
    </div>
  )
}
