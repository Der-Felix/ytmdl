import type { MouseEvent } from 'react'
import { Loader2, Play, Sparkles } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { Button } from '@/components/ui/button'
import type { PersonalMix } from '@/lib/api/mixes'
import { libraryArtwork } from '@/lib/artwork'
import { mixLabel } from '@/lib/mixes'
import { Link, paths } from '@/lib/router'
import { cn } from '@/lib/utils'
import { pluralize } from '@/lib/utils/format'

/**
 * A mix cover: a collage of up to four covers under a scrim in the two brand
 * colours, with the title set on it. The scrim keeps the title readable on
 * any artwork; it is the page's one decorative element.
 */
export function MixCover({ mix, showTitle = true, className }: { mix: PersonalMix; showTitle?: boolean; className?: string }) {
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

/** One mix: its cover links to the mix, the button plays it right away. */
export function MixCard({ mix, busy, disabled, onPlay }: { mix: PersonalMix; busy: boolean; disabled: boolean; onPlay: (event: MouseEvent, mix: PersonalMix) => void }) {
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
