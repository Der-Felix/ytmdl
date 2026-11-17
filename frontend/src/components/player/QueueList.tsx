import { useMemo } from 'react'
import { Accessibility } from '@dnd-kit/dom'
import type { DragEndEvent, DragOverEvent, DragStartEvent } from '@dnd-kit/dom'
import { DragDropProvider } from '@dnd-kit/react'
import { isSortable, useSortable } from '@dnd-kit/react/sortable'
import { ArrowDown, ArrowUp, GripVertical, Radio, X } from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { libraryArtwork } from '@/lib/artwork'
import { cn } from '@/lib/utils'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'

/*
 * The queue as a sortable list, shared by the queue drawer and the player
 * page. Dragging comes from dnd-kit, which reads pointer events and so works
 * with touch, mouse and pen alike; the earlier HTML drag and drop API never
 * fires on touch screens. A touch drag starts from the grip after a short
 * press, so the list still scrolls when swiped elsewhere.
 *
 * Without a pointer: focus the grip, press Space to pick the title up, move
 * it with the arrow keys and press Space again (Escape cancels). The up and
 * down buttons on every row do the same in one step.
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
function rowIds(queue: LibraryTrack[]) {
  const seen = new Map<string, number>()
  return queue.map((track) => {
    const n = seen.get(track.id) ?? 0
    seen.set(track.id, n + 1)
    return `${track.id}#${n}`
  })
}

export function QueueList({ className }: { className?: string }) {
  const { queue, queueIndex, status } = usePlayerState()
  const { reorderQueue } = usePlayerActions()
  const ids = useMemo(() => rowIds(queue), [queue])

  return (
    <DragDropProvider
      plugins={(defaults) => defaults.map((plugin) => (plugin === Accessibility ? germanAccessibility : plugin))}
      onDragEnd={(event) => {
        if (event.canceled) return
        const { source } = event.operation
        if (isSortable(source) && source.initialIndex !== source.index) {
          reorderQueue(source.initialIndex, source.index)
        }
      }}
    >
      <ol aria-label="Titel in der Warteschlange" className={cn('space-y-1', className)}>
        {queue.map((track, idx) => (
          <QueueRow
            key={ids[idx]}
            id={ids[idx]!}
            track={track}
            index={idx}
            total={queue.length}
            isCurrent={idx === queueIndex}
            isPlaying={status === 'playing'}
          />
        ))}
      </ol>
    </DragDropProvider>
  )
}

interface QueueRowProps {
  id: string
  track: LibraryTrack
  index: number
  total: number
  isCurrent: boolean
  isPlaying: boolean
}

function QueueRow({ id, track, index, total, isCurrent, isPlaying }: QueueRowProps) {
  const { playQueueIndex, removeFromQueue, reorderQueue } = usePlayerActions()
  const { ref, handleRef, isDragSource } = useSortable({ id, index })
  const artistText = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''

  // On pointer devices the row controls appear on hover or when a control in
  // the row has focus. On touch screens the grip is dragged directly, so the
  // step buttons give way to the title; removing stays visible.
  const rowControl =
    'inline-flex size-8 items-center justify-center rounded-md text-muted-foreground transition-[opacity,color,background-color] duration-(--duration-fast) disabled:pointer-events-none disabled:opacity-25 pointer-fine:opacity-0 pointer-fine:group-hover:opacity-100 pointer-fine:group-focus-within:opacity-100'
  const stepControl = cn(rowControl, 'pointer-coarse:hidden hover:bg-input hover:text-foreground')

  return (
    <li
      ref={ref}
      data-dragging={isDragSource ? '' : undefined}
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

      <button
        type="button"
        onClick={() => playQueueIndex(index)}
        aria-current={isCurrent ? 'true' : undefined}
        aria-label={`${track.title} abspielen`}
        className="flex min-w-0 flex-1 items-center gap-2.5 rounded-md py-0.5 text-left"
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
          className="size-9 shrink-0 rounded-md border border-input"
        />
        <span className="min-w-0 flex-1">
          <span
            className={cn(
              'block truncate text-[13px] leading-tight font-semibold',
              isCurrent ? 'text-accent-foreground' : 'text-foreground',
            )}
            title={track.title}
          >
            {track.title}
          </span>
          <span className="mt-0.5 block truncate text-[11px] text-muted-foreground" title={artistText}>
            {artistText}
          </span>
        </span>
        <span className="figures shrink-0 pr-1 text-[11px] text-muted-foreground">
          {formatDuration(track.duration_ms)}
        </span>
      </button>

      <div className="flex shrink-0 items-center">
        <button
          type="button"
          disabled={index === 0}
          onClick={() => reorderQueue(index, index - 1)}
          className={stepControl}
          title="Nach oben verschieben"
          aria-label={`Titel ${track.title} nach oben verschieben`}
        >
          <ArrowUp className="size-3.5" aria-hidden="true" />
        </button>
        <button
          type="button"
          disabled={index === total - 1}
          onClick={() => reorderQueue(index, index + 1)}
          className={stepControl}
          title="Nach unten verschieben"
          aria-label={`Titel ${track.title} nach unten verschieben`}
        >
          <ArrowDown className="size-3.5" aria-hidden="true" />
        </button>
        <button
          type="button"
          onClick={() => removeFromQueue(index)}
          className={cn(rowControl, 'hover:bg-destructive/10 hover:text-destructive')}
          title="Aus Warteschlange entfernen"
          aria-label={`Titel ${track.title} aus Warteschlange entfernen`}
        >
          <X className="size-3.5" aria-hidden="true" />
        </button>
      </div>
    </li>
  )
}
