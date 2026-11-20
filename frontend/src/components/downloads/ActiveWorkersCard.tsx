import { Badge } from '@/components/ui/badge'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { Progress } from '@/components/ui/progress'
import { ITEM_STATUS_LABELS } from '@/lib/api/jobs'
import { cn } from '@/lib/utils'
import type { ActiveWorkerPreview } from '@/types/api'

interface ActiveWorkersCardProps {
  workers: ActiveWorkerPreview[]
  className?: string
}

export function ActiveWorkersCard({ workers, className }: ActiveWorkersCardProps) {
  return (
    <Panel className={cn('space-y-3 p-4 sm:p-5', className)}>
      <PanelHeader
        className="items-center"
        title="Aktive Verarbeitung"
        action={
          workers.length > 0 && (
            <Badge variant="default" className="figures text-xs">
              {workers.length} Worker aktiv
            </Badge>
          )
        }
      />

      {workers.length === 0 ? (
        // One line, not a boxed illustration: the panel beside it carries the
        // content, and this one only has to say that nothing runs yet.
        <p className="text-sm text-muted-foreground">
          <span className="font-medium text-foreground">Keine Downloads in Bearbeitung.</span>{' '}
          Wartende Downloads starten, sobald ein Worker frei ist.
        </p>
      ) : (
        <ul className="divide-y divide-border">
          {workers.map((w, index) => {
            const isDownloading = w.phase === 'downloading'
            const percent = isDownloading ? Math.max(0, Math.min(100, Math.round(w.progress_percent))) : null
            const phaseLabel = ITEM_STATUS_LABELS[w.phase] || w.phase

            const trackDisplay = w.track_number > 0
              ? `${String(w.track_number).padStart(2, '0')}. ${w.track}`
              : w.track

            return (
              <li key={w.item_id || index} className="space-y-2 py-3 first:pt-0 last:pb-0">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 space-y-0.5">
                    <div className="truncate text-sm font-medium text-foreground">{trackDisplay}</div>
                    <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                      <span className="shrink-0 font-mono text-[0.6875rem] uppercase tracking-wider text-primary">
                        Worker {index + 1}
                      </span>
                      <span className="truncate text-foreground/80">
                        {w.artist || 'Unbekannter Künstler'}
                      </span>
                      {w.release && <span className="truncate">— {w.release}</span>}
                    </div>
                  </div>
                  <span className="figures shrink-0 text-xs text-muted-foreground">
                    {percent !== null ? `${percent}%` : phaseLabel}
                  </span>
                </div>

                <Progress
                  value={percent}
                  className="h-1"
                  aria-label={`${phaseLabel}: ${w.track}`}
                />
              </li>
            )
          })}
        </ul>
      )}
    </Panel>
  )
}
