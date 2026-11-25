import { Fragment, useMemo, useRef, useState } from 'react'
import { Accessibility } from '@dnd-kit/dom'
import type { DragEndEvent, DragOverEvent, DragStartEvent } from '@dnd-kit/dom'
import { DragDropProvider } from '@dnd-kit/react'
import { isSortable, useSortable } from '@dnd-kit/react/sortable'
import { ArrowDown, ArrowUp, GripVertical, Radio, Sparkles, X } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { moveQueueEntry, queueOrigin } from '@/contexts/player-reducer'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { libraryArtwork } from '@/lib/artwork'
import type { QueueContext, QueueTrack } from '@/lib/audio/types'
import { cn } from '@/lib/utils'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import { TrackArtistLink } from '@/components/music/TrackLinks'
import { isRowGesture } from '@/lib/track-row'
import { TrackMenu } from '@/components/music/TrackMenu'

/*
 * The queue as a sortable list, shared by the queue drawer and the player
 * page. Dragging comes from dnd-kit, which reads pointer events and so works
 * with touch, mouse and pen alike; the earlier HTML drag and drop API never
 * fires on touch screens. A touch drag starts from the grip after a short
 * press, so the list still scrolls when swiped elsewhere.
 *
 * Without a pointer: focus the grip, press Space to pick the title up, move
 * it with the arrow keys and press Space again (Escape cancels). The track
 * menu of every row moves a title one step up or down.
 *
 * The list is split into sections: what played, the current track, "Als
 * Nächstes" (what the user queued), the rest of what was started, and the
 * similar tracks autoplay appended. A title moved into a section belongs to
 * it: the drop takes the origin of the title it lands after (directly after
 * the current track that is "Als Nächstes"). While dragging, the list is
 * drawn from the player's own move rule, so the row shows up in the section
 * it will belong to before it is dropped. React keeps the order; dnd-kit's
 * own reordering of the DOM is turned off for that.
 */

/** The 1-based position of the dragged row, for the screen reader. */
function position(source: unknown) {
  const index = (source as { index?: unknown } | null)?.index
  return typeof index === 'number' ? index + 1 : undefined
}

// dnd-kit announces in English by default.
const germanAccessibility = Accessibility.configure({
  announcements: {
    dragstart: ({ operation }: DragStartEvent) => `Titel an Position ${position(operation.source)} aufgenommen.`,
    dragover: ({ operation }: DragOverEvent) => `Position ${position(operation.source)}.`,
    dragend: ({ operation, canceled }: DragEndEvent) =>
      canceled ? 'Verschieben abgebrochen.' : `Titel an Position ${position(operation.source)} abgelegt.`,
  },
  screenReaderInstructions: {
    draggable:
      'Leertaste drücken, um den Titel aufzunehmen. Mit den Pfeiltasten verschieben, mit der Leertaste ablegen, mit Escape abbrechen.',
  },
})

/** Stable ids for the rows; the same track can be queued more than once. */
function rowIds(queue: QueueTrack[]) {
  const seen = new Map<string, number>()
  return queue.map((track) => {
    const n = seen.get(track.id) ?? 0
    seen.set(track.id, n + 1)
    return `${track.id}#${n}`
  })
}

type QueueSection = 'played' | 'current' | 'user' | 'context' | 'autoplay'

function queueSection(track: QueueTrack, index: number, queueIndex: number): QueueSection {
  if (index < queueIndex) return 'played'
  if (index === queueIndex) return 'current'
  return queueOrigin(track)
}

function sectionHeading(section: QueueSection, context: QueueContext | null) {
  switch (section) {
    case 'played':
      return 'Zuvor gespielt'
    case 'current':
      return 'Aktueller Titel'
    case 'user':
      return 'Als Nächstes'
    case 'context':
      return context ? `Weiter aus: ${context.title}` : 'Weiter in der Warteschlange'
    case 'autoplay':
      return 'Ähnliche Titel'
  }
}

export function QueueList({ className }: { className?: string }) {
  const { queue, queueIndex, status, queueContext } = usePlayerState()
  const { reorderQueue } = usePlayerActions()
  const baseIds = useMemo(() => rowIds(queue), [queue])
  const [drag, setDragState] = useState<{ from: number; to: number } | null>(null)
  // The handlers read the move from here, never from a stale render.
  const dragRef = useRef<{ from: number; to: number } | null>(null)
  const setDrag = (next: { from: number; to: number } | null) => {
    dragRef.current = next
    setDragState(next)
  }

  // The order shown: the queue, or while dragging the queue as it will be.
  const view = useMemo(() => {
    if (!drag || drag.from === drag.to) return { queue, queueIndex, ids: baseIds }
    const moved = moveQueueEntry(queue, queueIndex, drag.from, drag.to)
    const ids = [...baseIds]
    const [id] = ids.splice(drag.from, 1)
    ids.splice(drag.to, 0, id!)
    return { ...moved, ids }
  }, [baseIds, drag, queue, queueIndex])

  const counts = useMemo(() => {
    const n: Record<QueueSection, number> = { played: 0, current: 0, user: 0, context: 0, autoplay: 0 }
    view.queue.forEach((t, i) => n[queueSection(t, i, view.queueIndex)]++)
    return n
  }, [view])

  return (
    <DragDropProvider
      plugins={(defaults) => defaults.map((plugin) => (plugin === Accessibility ? germanAccessibility : plugin))}
      onDragStart={(event) => {
        const { source } = event.operation
        if (isSortable(source)) setDrag({ from: source.index, to: source.index })
      }}
      onDragOver={(event) => {
        const { source, target } = event.operation
        // The order comes from state; dnd-kit must not move rows itself.
        event.preventDefault()
        if (!isSortable(source) || !isSortable(target) || source.id === target.id) return
        const current = dragRef.current
        if (current && current.to !== target.index) setDrag({ from: current.from, to: target.index })
      }}
      onDragEnd={(event) => {
        const current = dragRef.current
        setDrag(null)
        if (event.canceled || !current || current.from === current.to) return
        reorderQueue(current.from, current.to)
      }}
    >
      <ol aria-label="Titel in der Warteschlange" className={cn('space-y-1', className)}>
        {view.queue.map((track, idx) => {
          const section = queueSection(track, idx, view.queueIndex)
          const previous = idx > 0 ? queueSection(view.queue[idx - 1]!, idx - 1, view.queueIndex) : null
          return (
            <Fragment key={view.ids[idx]}>
              {section !== previous && (
                <li
                  className={cn('flex items-baseline gap-2 px-2 pb-1', idx > 0 && 'pt-3')}
                  data-queue-section={section}
                >
                  <h3 className="flex min-w-0 items-center gap-1.5 text-xs font-semibold text-muted-foreground">
                    {section === 'autoplay' && <Sparkles className="size-3.5 shrink-0" aria-hidden="true" />}
                    <span className="truncate" title={sectionHeading(section, queueContext)}>
                      {sectionHeading(section, queueContext)}
                    </span>
                  </h3>
                  {section !== 'current' && (
                    <span className="figures shrink-0 text-[11px] text-muted-foreground/80">{counts[section]}</span>
                  )}
                </li>
              )}
              <QueueRow
                id={view.ids[idx]!}
                track={track}
                index={idx}
                total={view.queue.length}
                section={section}
                isPlaying={status === 'playing'}
              />
            </Fragment>
          )
        })}
      </ol>
    </DragDropProvider>
  )
}

interface QueueRowProps {
  id: string
  track: QueueTrack
  index: number
  total: number
  section: QueueSection
  isPlaying: boolean
}

function QueueRow({ id, track, index, total, section, isPlaying }: QueueRowProps) {
  const { playQueueIndex, removeFromQueue, reorderQueue } = usePlayerActions()
  const { ref, handleRef, isDragSource } = useSortable({ id, index })
  const artistText = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''
  const isCurrent = section === 'current'
  // Played titles and autoplay suggestions step back; hovering or focusing
  // a row brings it to full strength.
  const quiet = section === 'played' || section === 'autoplay'

  // On pointer devices the row controls appear on hover or when a control in
  // the row has focus. Removing stays as the one quick action; moving one
  // step, the radio and the rest live in the track menu.
  const reveal =
    'transition-[opacity,color,background-color] duration-(--duration-fast) pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:group-focus-within:opacity-100'
  const rowControl = cn(
    'inline-flex size-8 items-center justify-center rounded-md text-muted-foreground disabled:pointer-events-none disabled:opacity-25',
    reveal,
  )

  return (
    <li
      ref={ref}
      data-track-row
      onDoubleClick={(event) => {
        if (!isRowGesture(event)) return
        playQueueIndex(index)
      }}
      data-dragging={isDragSource ? '' : undefined}
      data-queue-origin={queueOrigin(track)}
      className={cn(
        'group relative flex items-center gap-1.5 rounded-lg border p-1.5 transition-[background-color,border-color,opacity] duration-(--duration-fast)',
        isCurrent ? 'border-primary/40 bg-accent' : 'border-transparent pointer-fine:hover:bg-muted',
        isDragSource && 'border-primary/60 bg-secondary opacity-80',
      )}
    >
      <button
        ref={handleRef}
        type="button"
        className="inline-flex size-8 shrink-0 cursor-grab touch-none items-center justify-center rounded-md text-muted-foreground hover:text-foreground active:cursor-grabbing"
        aria-label={`${track.title} verschieben`}
        title="Ziehen zum Verschieben"
      >
        <GripVertical className="size-4" aria-hidden="true" />
      </button>

      {/* The play button stretches over the whole block, so a click anywhere
          on cover, title or duration plays; the artist link sits above it. */}
      <div
        className={cn(
          'relative flex min-w-0 flex-1 items-center gap-2.5 rounded-md py-0.5 text-left transition-opacity duration-(--duration-fast)',
          quiet && !isDragSource && 'opacity-60 group-hover:opacity-100 group-focus-within:opacity-100',
        )}
      >
        <span className="flex w-5 shrink-0 justify-center" aria-hidden="true">
          {isCurrent ? (
            <Radio className={cn('size-3.5 text-primary', isPlaying && 'animate-pulse motion-reduce:animate-none')} />
          ) : (
            <span className="figures text-[11px] text-muted-foreground">{index + 1}</span>
          )}
        </span>
        <Cover
          src={libraryArtwork('tracks', track.id)}
          fallbackSrc={track.cover_url}
          alt=""
          shape="square"
          className={cn('shrink-0 rounded-md border border-input', section === 'autoplay' ? 'size-8' : 'size-9')}
        />
        <span className="min-w-0 flex-1">
          <button
            type="button"
            onClick={() => playQueueIndex(index)}
            aria-current={isCurrent ? 'true' : undefined}
            aria-label={`${track.title} abspielen`}
            className={cn(
              'block w-full cursor-pointer truncate text-left text-[13px] leading-tight outline-none after:absolute after:inset-0 after:rounded-md focus-visible:after:outline-2 focus-visible:after:outline-offset-1 focus-visible:after:outline-ring',
              section === 'autoplay' ? 'font-medium' : 'font-semibold',
              isCurrent ? 'text-accent-foreground' : 'text-foreground',
            )}
            title={track.title}
          >
            {track.title}
          </button>
          <span className="relative mt-0.5 block truncate text-[11px] text-muted-foreground" title={artistText}>
            <TrackArtistLink track={track}>{artistText}</TrackArtistLink>
          </span>
        </span>
        <span className="figures shrink-0 pr-1 text-[11px] text-muted-foreground">
          {formatDuration(track.duration_ms)}
        </span>
      </div>

      <div className="flex shrink-0 items-center">
        <button
          type="button"
          onClick={() => removeFromQueue(index)}
          className={cn(rowControl, 'hover:bg-destructive/10 hover:text-destructive')}
          title="Aus Warteschlange entfernen"
          aria-label={`Titel ${track.title} aus Warteschlange entfernen`}
        >
          <X className="size-3.5" aria-hidden="true" />
        </button>
        <TrackMenu
          track={track}
          inQueue
          className={cn(
            'rounded-md pointer-fine:size-8 data-[popup-open]:opacity-100 [&_svg]:pointer-fine:size-4',
            reveal,
          )}
          extras={[
            {
              id: 'move-up',
              label: 'Nach oben verschieben',
              icon: ArrowUp,
              disabled: index === 0,
              onSelect: () => reorderQueue(index, index - 1),
            },
            {
              id: 'move-down',
              label: 'Nach unten verschieben',
              icon: ArrowDown,
              disabled: index === total - 1,
              onSelect: () => reorderQueue(index, index + 1),
            },
            {
              id: 'remove',
              label: 'Aus Warteschlange entfernen',
              icon: X,
              destructive: true,
              onSelect: () => removeFromQueue(index),
            },
          ]}
        />
      </div>
    </li>
  )
}
