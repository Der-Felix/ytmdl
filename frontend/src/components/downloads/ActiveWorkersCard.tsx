import { ArrowDownToLineIcon, CpuIcon } from 'lucide-react'

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
    <Panel className={cn('p-4 space-y-4 sm:p-5', className)}>
      <PanelHeader
        title={
          <div className="flex items-center gap-2">
            <CpuIcon className="size-4 text-primary" />
            <span className="font-heading font-semibold">Aktive Verarbeitung</span>
          </div>
        }
        action={
          <Badge variant={workers.length > 0 ? 'default' : 'neutral'} className="text-xs">
            {workers.length} {workers.length === 1 ? 'Worker aktiv' : 'Worker aktiv'}
          </Badge>
        }
      />

      {workers.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-6 text-center text-muted-foreground border border-dashed border-border/60 rounded-lg">
          <ArrowDownToLineIcon className="size-8 stroke-[1.25] text-muted-foreground/50 mb-2" />
          <p className="text-sm font-medium text-foreground/80">Keine Downloads in Bearbeitung</p>
          <p className="text-xs text-muted-foreground max-w-sm mt-0.5">
            Wartende Downloads werden automatisch gestartet, sobald freie Worker-Kapazität verfügbar ist.
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {workers.map((w, index) => {
            const isDownloading = w.phase === 'downloading'
            const percent = isDownloading ? Math.max(0, Math.min(100, Math.round(w.progress_percent))) : null
            const phaseLabel = ITEM_STATUS_LABELS[w.phase] || w.phase

            const trackDisplay = w.track_number > 0
              ? `${String(w.track_number).padStart(2, '0')}. ${w.track}`
              : w.track

            return (
              <div
                key={w.item_id || index}
                className="rounded-lg border border-border/60 bg-white/[0.02] p-3 space-y-2.5 transition-colors"
              >
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0 space-y-0.5">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className="inline-flex items-center px-1.5 py-0.5 rounded text-[0.625rem] font-semibold bg-primary/10 text-primary uppercase tracking-wider">
                        Worker {index + 1}
                      </span>
                      <span className="text-xs font-semibold text-foreground truncate">
                        {w.artist || 'Unbekannter Künstler'}
                      </span>
                      {w.release && (
                        <span className="text-xs text-muted-foreground truncate">
                          — {w.release}
                        </span>
                      )}
                    </div>
                    <div className="text-xs font-medium text-foreground/90 truncate">
                      {trackDisplay}
                    </div>
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
                    <Badge
                      variant={isDownloading ? 'default' : 'neutral'}
                      className="text-[0.6875rem] h-5 px-1.5"
                    >
                      {phaseLabel}
                    </Badge>
                  </div>
                </div>

                {/* Progress bar */}
                <div className="space-y-1">
                  <Progress value={percent} className="h-1.5" />
                  <div className="flex items-center justify-between text-[0.6875rem] text-muted-foreground">
                    <span>{phaseLabel}</span>
                    <span>{percent !== null ? `${percent}%` : 'Verarbeitung …'}</span>
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </Panel>
  )
}
