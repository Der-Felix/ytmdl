import type { ReactNode } from 'react'
import { AlertTriangleIcon, CircleCheckIcon, ClockIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { cn } from '@/lib/utils'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { QueueSummary } from '@/types/api'

interface QueueSummaryCardProps {
  summary: QueueSummary
  className?: string
}

/** Nothing running, nothing waiting, nothing that needs attention. */
function isQueueIdle(summary: QueueSummary): boolean {
  return (
    summary.storage_healthy &&
    summary.active_items === 0 &&
    summary.remaining_items === 0 &&
    summary.paused_jobs === 0 &&
    summary.retry_wait_items === 0 &&
    summary.current.length === 0 &&
    summary.next.length === 0
  )
}

export function QueueSummaryCard({ summary, className }: QueueSummaryCardProps) {
  // An idle queue is one calm line. Five zeros in five boxes say the same
  // thing five times and push the job list below the fold.
  if (isQueueIdle(summary)) {
    return (
      <Panel className={cn('flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 sm:px-5', className)}>
        {/* The text keeps a minimum width, so on a phone the hourly count
            wraps below it instead of squeezing it into a column of words. */}
        <div className="flex min-w-56 flex-1 items-center gap-3">
          <CircleCheckIcon aria-hidden className="size-4 shrink-0 text-success" />
          <div className="min-w-0">
            <h2 className="font-heading text-sm font-semibold text-foreground">Warteschlange ist leer</h2>
            <p className="text-xs text-muted-foreground">
              Neue Downloads starten von selbst, sobald du etwas hinzufügst.
            </p>
          </div>
        </div>
        {summary.completed_last_hour > 0 && (
          <p className="figures pl-7 text-xs text-muted-foreground sm:pl-0">
            {pluralize(summary.completed_last_hour, 'Track', 'Tracks')} in der letzten Stunde
          </p>
        )}
      </Panel>
    )
  }

  const isStorageUnhealthy = !summary.storage_healthy
  const hasThroughput = summary.throughput_items_per_hour > 0

  const etaVariant = () => {
    if (isStorageUnhealthy) return 'warning'
    if (summary.eta_confidence === 'high') return 'success'
    if (summary.eta_confidence === 'medium') return 'default'
    if (summary.eta_confidence === 'paused') return 'warning'
    return 'neutral'
  }

  return (
    <Panel className={cn('space-y-4 p-4 sm:p-5', className)}>
      <PanelHeader
        className="flex-wrap items-center gap-2"
        title="Deine Warteschlange"
        action={
          <Badge variant={etaVariant()} className="text-xs font-medium">
            <ClockIcon aria-hidden className="mr-1 size-3" />
            {summary.eta_text}
          </Badge>
        }
      />

      {isStorageUnhealthy && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-xs text-warning">
          <AlertTriangleIcon aria-hidden className="size-4 shrink-0" />
          <span>
            Der Bibliotheksspeicher ist aktuell nicht schreibbar oder nicht erreichbar. Downloads warten automatisch, bis der Speicher bereit ist.
          </span>
        </div>
      )}

      {/* One strip of figures. Hairlines separate them; a box around every
          figure inside the panel would be a card in a card. */}
      <dl className="grid grid-cols-2 gap-x-5 gap-y-4 sm:grid-cols-3 lg:grid-cols-5 lg:gap-x-0 lg:divide-x lg:divide-border">
        <Figure
          label="Offene Tracks"
          value={formatNumber(summary.remaining_items)}
          hint="in anstehenden Jobs"
          quiet={summary.remaining_items === 0}
        />
        <Figure
          label="In Bearbeitung"
          value={formatNumber(summary.active_items)}
          hint={summary.active_items === 1 ? 'aktiver Worker' : 'aktive Worker'}
          quiet={summary.active_items === 0}
        />
        <Figure
          label="Durchsatz"
          value={hasThroughput ? `~${summary.throughput_items_per_hour.toFixed(0)}` : '—'}
          hint={hasThroughput ? 'Tracks / Stunde (gemessen)' : 'Berechnung läuft …'}
          quiet={!hasThroughput}
        />
        <Figure
          label="Pausiert"
          value={formatNumber(summary.paused_jobs)}
          hint={summary.paused_jobs > 0 ? 'Jobs (separat, nicht in ETA)' : 'Keine Jobs pausiert'}
          quiet={summary.paused_jobs === 0}
        />
        <Figure
          label="Wiederholung geplant"
          value={formatNumber(summary.retry_wait_items)}
          hint={summary.retry_wait_items > 0 ? 'Warten auf Wiederholung' : 'Keine Wiederholung ausstehend'}
          quiet={summary.retry_wait_items === 0}
          tone={summary.retry_wait_items > 0 ? 'warning' : undefined}
          icon={summary.retry_wait_items > 0 ? <AlertTriangleIcon aria-hidden className="size-3.5" /> : undefined}
          className="col-span-2 sm:col-span-1"
        />
      </dl>
    </Panel>
  )
}

interface FigureProps {
  label: string
  value: string
  hint: string
  /** A zero or missing value steps back so the figures that matter stand out. */
  quiet?: boolean
  tone?: 'warning'
  icon?: ReactNode
  className?: string
}

function Figure({ label, value, hint, quiet, tone, icon, className }: FigureProps) {
  return (
    <div className={cn('min-w-0 lg:px-5 lg:first:pl-0 lg:last:pr-0', className)}>
      <dt className={cn('flex items-center gap-1.5 text-xs text-muted-foreground', tone === 'warning' && 'text-warning')}>
        {icon}
        {label}
      </dt>
      <dd className="mt-1">
        <span
          className={cn(
            'figures block text-2xl font-semibold leading-tight',
            tone === 'warning' ? 'text-warning' : quiet ? 'text-muted-foreground' : 'text-foreground',
          )}
        >
          {value}
        </span>
        <span className="mt-0.5 block text-[0.6875rem] text-muted-foreground">{hint}</span>
      </dd>
    </div>
  )
}
