import { useState } from 'react'
import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import {
  ArrowDown,
  ArrowUp,
  GripVertical,
  ListMusic,
  Maximize2,
  Radio,
  Trash2,
  X,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { Button } from '@/components/ui/button'
import { usePlayer } from '@/hooks/usePlayer'
import { Link } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatDuration, joinArtists } from '@/lib/utils/format'

interface QueueDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function QueueDrawer({ open, onOpenChange }: QueueDrawerProps) {
  const {
    queue,
    queueIndex,
    status,
    playQueueIndex,
    removeFromQueue,
    reorderQueue,
    clearUpcomingQueue,
    clearQueue,
  } = usePlayer()

  const [draggedIndex, setDraggedIndex] = useState<number | null>(null)
  const [dropTargetIndex, setDropTargetIndex] = useState<number | null>(null)

  const isPlaying = status === 'playing'

  // Calculate remaining queue duration
  const remainingDurationMs = queue
    .slice(Math.max(0, queueIndex))
    .reduce((acc, t) => acc + (t.duration_ms || 0), 0)

  const handleDragStart = (e: React.DragEvent, index: number) => {
    e.dataTransfer.setData('text/plain', String(index))
    e.dataTransfer.effectAllowed = 'move'
    setDraggedIndex(index)
  }

  const handleDragOver = (e: React.DragEvent, index: number) => {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    if (dropTargetIndex !== index) {
      setDropTargetIndex(index)
    }
  }

  const handleDrop = (e: React.DragEvent, targetIndex: number) => {
    e.preventDefault()
    const fromStr = e.dataTransfer.getData('text/plain')
    const fromIndex = parseInt(fromStr, 10)
    if (!isNaN(fromIndex) && fromIndex !== targetIndex) {
      reorderQueue(fromIndex, targetIndex)
    }
    setDraggedIndex(null)
    setDropTargetIndex(null)
  }

  const handleDragEnd = () => {
    setDraggedIndex(null)
    setDropTargetIndex(null)
  }

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Backdrop
          className="fixed inset-0 z-50 bg-overlay/65 transition-opacity duration-200 ease-out data-[starting-style]:opacity-0 data-[ending-style]:opacity-0"
        />
        <DialogPrimitive.Popup
          data-slot="queue-drawer"
          aria-label="Wiedergabeliste / Warteschlange"
          className={cn(
            'fixed inset-y-0 right-0 z-50 flex w-full max-w-md flex-col border-l border-border bg-surface shadow-2xl transition-transform duration-200 ease-out data-[starting-style]:translate-x-full data-[ending-style]:translate-x-full outline-none',
          )}
        >
          {/* Header */}
          <div className="flex items-center justify-between px-5 py-4 border-b border-border">
            <div className="flex items-center gap-2.5">
              <div className="flex size-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <ListMusic className="size-4" />
              </div>
              <div>
                <h2 className="text-base font-semibold text-foreground leading-tight">Warteschlange</h2>
                <p className="text-xs text-muted-foreground">
                  {queue.length === 1 ? '1 Titel' : `${queue.length} Titel`}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-1.5">
              {queueIndex >= 0 && queueIndex < queue.length - 1 && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={clearUpcomingQueue}
                  className="h-8 px-2.5 text-xs text-muted-foreground hover:text-foreground rounded-lg hover:bg-input"
                  title="Kommende Titel entfernen (bereits gespielte und aktuellen Titel beibehalten)"
                >
                  Nächste leeren
                </Button>
              )}

              {queue.length > 0 && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={clearQueue}
                  className="h-8 px-2.5 text-xs text-destructive hover:text-destructive hover:bg-destructive/10 rounded-lg"
                  title="Komplette Warteschlange leeren und Wiedergabe stoppen"
                >
                  <Trash2 className="size-3.5 mr-1" />
                  Leeren
                </Button>
              )}

              <DialogPrimitive.Close
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-8 rounded-lg text-muted-foreground hover:text-foreground hover:bg-input"
                    title="Schließen"
                    aria-label="Schließen"
                  />
                }
              >
                <X className="size-4" />
              </DialogPrimitive.Close>
            </div>
          </div>

          {/* Queue Body */}
          <div className="flex-1 overflow-y-auto px-3 py-3 space-y-1">
            {queue.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-64 text-center px-4">
                <div className="size-12 rounded-2xl bg-muted border border-border flex items-center justify-center mb-3">
                  <ListMusic className="size-6 text-muted-foreground/80" />
                </div>
                <p className="text-sm font-medium text-foreground/80">Die Warteschlange ist leer</p>
                <p className="text-xs text-muted-foreground/80 mt-1 max-w-xs">
                  Spiele einen Titel oder ein Album aus deiner Bibliothek ab, um die Warteschlange zu füllen.
                </p>
              </div>
            ) : (
              queue.map((track, idx) => {
                const isCurrent = idx === queueIndex
                const isDragging = draggedIndex === idx
                const isDropTarget = dropTargetIndex === idx

                const artistText =
                  track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist || ''

                return (
                  <div
                    key={`${track.id}-${idx}`}
                    draggable
                    onDragStart={(e) => handleDragStart(e, idx)}
                    onDragOver={(e) => handleDragOver(e, idx)}
                    onDrop={(e) => handleDrop(e, idx)}
                    onDragEnd={handleDragEnd}
                    aria-current={isCurrent ? 'true' : undefined}
                    onClick={() => playQueueIndex(idx)}
                    className={cn(
                      'group relative flex items-center justify-between gap-2.5 p-2 rounded-xl transition cursor-pointer select-none border',
                      isCurrent
                        ? 'bg-primary/10 border-primary/40 text-primary-foreground'
                        : 'border-transparent hover:bg-muted text-foreground',
                      isDragging && 'opacity-30 scale-98',
                      isDropTarget && !isDragging && 'border-primary/60 bg-secondary',
                    )}
                  >
                    {/* Drag Handle & Position / Current Indicator */}
                    <div
                      className="flex items-center gap-1.5 shrink-0"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <button
                        type="button"
                        className="cursor-grab active:cursor-grabbing p-1 text-muted-foreground/80 hover:text-foreground/80 opacity-60 group-hover:opacity-100 transition-opacity"
                        title="Verschieben per Drag & Drop"
                        aria-label="Verschieben per Drag & Drop"
                      >
                        <GripVertical className="size-3.5" />
                      </button>

                      <div className="size-4 flex items-center justify-center">
                        {isCurrent ? (
                          <Radio
                            className={cn(
                              'size-3.5 text-primary',
                              isPlaying && 'animate-pulse',
                            )}
                          />
                        ) : (
                          <span className="text-[11px] font-mono text-muted-foreground/80 tabular-nums">
                            {idx + 1}
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Track Artwork */}
                    <Cover
                      src={track.cover_url}
                      alt={track.title}
                      shape="square"
                      className="size-9.5 rounded-lg shrink-0 border border-input"
                    />

                    {/* Track Info */}
                    <div className="min-w-0 flex-1">
                      <p
                        className={cn(
                          'truncate text-xs sm:text-[13px] font-semibold leading-tight',
                          isCurrent ? 'text-primary' : 'text-foreground',
                        )}
                        title={track.title}
                      >
                        {track.title}
                      </p>
                      <p
                        className="truncate text-[11px] text-muted-foreground mt-0.5"
                        title={artistText}
                      >
                        {artistText}
                      </p>
                    </div>

                    {/* Track Duration & Reorder / Remove Controls */}
                    <div
                      className="flex items-center gap-1 shrink-0"
                      onClick={(e) => e.stopPropagation()}
                    >
                      <span className="text-[11px] font-mono text-muted-foreground tabular-nums pr-1">
                        {formatDuration(track.duration_ms)}
                      </span>

                      {/* Accessible Move Up */}
                      <button
                        type="button"
                        disabled={idx === 0}
                        onClick={() => reorderQueue(idx, idx - 1)}
                        className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-input disabled:opacity-20 disabled:pointer-events-none transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100"
                        title="Nach oben verschieben"
                        aria-label={`Titel ${track.title} nach oben verschieben`}
                      >
                        <ArrowUp className="size-3.5" />
                      </button>

                      {/* Accessible Move Down */}
                      <button
                        type="button"
                        disabled={idx === queue.length - 1}
                        onClick={() => reorderQueue(idx, idx + 1)}
                        className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-input disabled:opacity-20 disabled:pointer-events-none transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100"
                        title="Nach unten verschieben"
                        aria-label={`Titel ${track.title} nach unten verschieben`}
                      >
                        <ArrowDown className="size-3.5" />
                      </button>

                      {/* Remove Track */}
                      <button
                        type="button"
                        onClick={() => removeFromQueue(idx)}
                        className="p-1 rounded-md text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100"
                        title="Aus Warteschlange entfernen"
                        aria-label={`Titel ${track.title} aus Warteschlange entfernen`}
                      >
                        <X className="size-3.5" />
                      </button>
                    </div>
                  </div>
                )
              })
            )}
          </div>

          {/* Footer */}
          {queue.length > 0 && (
            <div className="flex items-center justify-between px-5 py-3 border-t border-border bg-muted">
              <div className="text-xs text-muted-foreground">
                <span>Verbleibend: </span>
                <span className="font-mono text-foreground">
                  {formatDuration(remainingDurationMs)}
                </span>
              </div>

              <Link
                href="/player?tab=queue"
                onClick={() => onOpenChange(false)}
                className="inline-flex items-center gap-1.5 text-xs text-primary hover:text-primary/80 font-medium transition-colors"
              >
                <span>Im Player öffnen</span>
                <Maximize2 className="size-3.5" />
              </Link>
            </div>
          )}
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
