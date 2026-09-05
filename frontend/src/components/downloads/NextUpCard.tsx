import { DiscIcon, ListOrderedIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { cn } from '@/lib/utils'
import { pluralize } from '@/lib/utils/format'
import type { NextUpJob } from '@/types/api'

interface NextUpCardProps {
  jobs: NextUpJob[]
  className?: string
}

export function NextUpCard({ jobs, className }: NextUpCardProps) {
  return (
    <Panel className={cn('p-4 space-y-4 sm:p-5', className)}>
      <PanelHeader
        title={
          <div className="flex items-center gap-2">
            <ListOrderedIcon className="size-4 text-primary" />
            <span className="font-heading font-semibold">Nächste in der Warteschlange</span>
          </div>
        }
        action={
          <Badge variant="neutral" className="text-xs">
            {jobs.length > 0 ? `${jobs.length} in Vorschau` : 'Keine'}
          </Badge>
        }
      />

      {jobs.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-6 text-center text-muted-foreground border border-dashed border-border/60 rounded-lg">
          <DiscIcon className="size-8 stroke-[1.25] text-muted-foreground/50 mb-2" />
          <p className="text-sm font-medium text-foreground/80">Keine anstehenden Jobs</p>
          <p className="text-xs text-muted-foreground max-w-sm mt-0.5">
            Alle geplanten Downloads wurden verarbeitet oder die Warteschlange ist leer.
          </p>
        </div>
      ) : (
        <div className="space-y-2.5">
          {jobs.map((job, index) => (
            <div
              key={job.job_id}
              className="flex items-center justify-between gap-3 rounded-lg border border-border/50 bg-white/[0.02] p-2.5 transition-colors hover:bg-white/[0.04]"
            >
              <div className="flex items-center gap-3 min-w-0">
                <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-white/5 text-[0.6875rem] font-semibold text-muted-foreground">
                  {index + 1}
                </span>

                {/* Cover / Icon */}
                <div className="size-9 shrink-0 overflow-hidden rounded border border-border/60 bg-muted flex items-center justify-center">
                  {job.cover_url ? (
                    <img
                      src={job.cover_url}
                      alt={job.release}
                      className="size-full object-cover"
                      loading="lazy"
                    />
                  ) : (
                    <DiscIcon className="size-4 text-muted-foreground/60" />
                  )}
                </div>

                <div className="min-w-0 space-y-0.5">
                  <div className="text-xs font-semibold text-foreground truncate">
                    {job.artist || 'Unbekannter Künstler'}
                  </div>
                  <div className="text-xs text-muted-foreground truncate">
                    {job.release || 'Unbekanntes Release'}
                  </div>
                </div>
              </div>

              <div className="shrink-0 flex items-center gap-2">
                <Badge variant="neutral" className="text-[0.6875rem]">
                  {pluralize(job.open_tracks, 'Track', 'Tracks')} offen
                </Badge>
              </div>
            </div>
          ))}
        </div>
      )}
    </Panel>
  )
}
