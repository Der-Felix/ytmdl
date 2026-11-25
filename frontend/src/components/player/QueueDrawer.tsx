import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import { ListMusic, Maximize2, Trash2, X } from 'lucide-react'

import { QueueList } from '@/components/player/QueueList'
import { QueueOptions } from '@/components/player/QueueOptions'
import { Button } from '@/components/ui/button'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { Link } from '@/lib/router'
import { formatDuration } from '@/lib/utils/format'

interface QueueDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/*
 * The queue reads only the player state, never the playback position, so a
 * progress tick does not redraw it.
 */
export function QueueDrawer({ open, onOpenChange }: QueueDrawerProps) {
  const { queue, queueIndex } = usePlayerState()
  const { clearUpcomingQueue, clearQueue } = usePlayerActions()

  const remainingDurationMs = queue
    .slice(Math.max(0, queueIndex))
    .reduce((acc, t) => acc + (t.duration_ms || 0), 0)

  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-overlay/65 transition-opacity duration-(--duration-base) ease-(--ease-out) data-[starting-style]:opacity-0 data-[ending-style]:opacity-0" />
        <DialogPrimitive.Popup
          data-slot="queue-drawer"
          aria-label="Wiedergabeliste / Warteschlange"
          className="fixed inset-y-0 right-0 z-50 flex w-full max-w-md flex-col border-l border-border bg-surface outline-none transition-transform duration-(--duration-slow) ease-(--ease-drawer) data-[starting-style]:translate-x-full data-[ending-style]:translate-x-full"
        >
          <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3.5 sm:px-5">
            <div className="flex min-w-0 items-center gap-2.5">
              <div className="flex size-8 shrink-0 max-sm:hidden items-center justify-center rounded-lg bg-accent text-accent-foreground">
                <ListMusic className="size-4" aria-hidden="true" />
              </div>
              <div className="min-w-0">
                <DialogPrimitive.Title className="truncate text-base leading-tight font-semibold text-foreground">
                  Warteschlange
                </DialogPrimitive.Title>
                <p className="figures text-xs text-muted-foreground">{queue.length} Titel</p>
              </div>
            </div>

            <div className="flex shrink-0 items-center gap-1">
              {queueIndex >= 0 && queueIndex < queue.length - 1 && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={clearUpcomingQueue}
                  className="h-8 px-2.5 text-xs text-muted-foreground hover:text-foreground max-sm:px-1.5"
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
                  className="h-8 px-2.5 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive max-sm:px-1.5"
                  title="Komplette Warteschlange leeren und Wiedergabe stoppen"
                >
                  <Trash2 className="mr-1 size-3.5" aria-hidden="true" />
                  Leeren
                </Button>
              )}

              <DialogPrimitive.Close
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    className="size-8 text-muted-foreground hover:text-foreground"
                    title="Schließen"
                    aria-label="Schließen"
                  />
                }
              >
                <X className="size-4" aria-hidden="true" />
              </DialogPrimitive.Close>
            </div>
          </div>

          {queue.length > 0 && <QueueOptions className="border-b border-border px-4 py-1 sm:px-5" />}

          <div className="flex-1 overflow-y-auto overscroll-contain px-2 py-2 sm:px-3">
            {queue.length === 0 ? (
              <div className="flex h-64 flex-col items-center justify-center px-4 text-center">
                <div className="mb-3 flex size-12 items-center justify-center rounded-xl border border-border bg-muted">
                  <ListMusic className="size-6 text-muted-foreground" aria-hidden="true" />
                </div>
                <p className="text-sm font-medium text-foreground">Die Warteschlange ist leer</p>
                <p className="mt-1 max-w-xs text-xs text-muted-foreground">
                  Spiele einen Titel oder ein Album aus deiner Bibliothek ab, um die Warteschlange zu füllen.
                </p>
              </div>
            ) : (
              <QueueList />
            )}
          </div>

          {queue.length > 0 && (
            <div className="flex items-center justify-between border-t border-border bg-muted px-4 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:px-5">
              <div className="text-xs text-muted-foreground">
                <span>Verbleibend: </span>
                <span className="figures text-foreground">{formatDuration(remainingDurationMs)}</span>
              </div>

              <Link
                href="/player?tab=queue"
                onClick={() => onOpenChange(false)}
                className="inline-flex items-center gap-1.5 rounded-md text-xs font-medium text-accent-foreground transition-colors hover:text-foreground focus-ring"
              >
                <span>Im Player öffnen</span>
                <Maximize2 className="size-3.5" aria-hidden="true" />
              </Link>
            </div>
          )}
        </DialogPrimitive.Popup>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  )
}
