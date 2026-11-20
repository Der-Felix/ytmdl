import { DiscIcon } from 'lucide-react'

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
    <Panel className={cn('space-y-3 p-4 sm:p-5', className)}>
      <PanelHeader
        className="items-center"
        title="Nächste in der Warteschlange"
        action={
          jobs.length > 0 && (
            <Badge variant="neutral" className="figures text-xs">
              {jobs.length} in Vorschau
            </Badge>
          )
        }
      />

      {jobs.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          <span className="font-medium text-foreground">Keine anstehenden Jobs.</span>{' '}
          Nach den laufenden Downloads ist die Warteschlange leer.
        </p>
      ) : (
        <ol className="divide-y divide-border">
          {jobs.map((job, index) => (
            <li key={job.job_id} className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0">
              <span className="figures w-4 shrink-0 text-right text-xs text-muted-foreground">
                {index + 1}
              </span>

              <div className="flex size-9 shrink-0 items-center justify-center overflow-hidden rounded-md bg-muted">
                {job.cover_url ? (
                  <img src={job.cover_url} alt="" className="size-full object-cover" loading="lazy" />
                ) : (
                  <DiscIcon aria-hidden className="size-4 text-muted-foreground" />
                )}
              </div>

              <div className="min-w-0 flex-1">
                <div className="truncate text-sm font-medium text-foreground">
                  {job.release || 'Unbekanntes Release'}
                </div>
                <div className="truncate text-xs text-muted-foreground">
                  {job.artist || 'Unbekannter Künstler'}
                </div>
              </div>

              <span className="figures shrink-0 text-xs text-muted-foreground">
                {typeof job.open_tracks === 'number' &&
                Number.isFinite(job.open_tracks) &&
                job.open_tracks >= 0
                  ? `${pluralize(job.open_tracks, 'Track', 'Tracks')} offen`
                  : 'Anzahl unbekannt'}
              </span>
            </li>
          ))}
        </ol>
      )}
    </Panel>
  )
}
