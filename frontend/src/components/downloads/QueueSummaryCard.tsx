import {
  AlertTriangleIcon,
  ClockIcon,
  GaugeIcon,
  ListMusicIcon,
  PauseCircleIcon,
  PlayCircleIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { cn } from '@/lib/utils'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { QueueSummary } from '@/types/api'

interface QueueSummaryCardProps {
  summary: QueueSummary
  className?: string
}

export function QueueSummaryCard({ summary, className }: QueueSummaryCardProps) {
  const isStorageUnhealthy = !summary.storage_healthy

  const getEtaBadgeVariant = () => {
    if (isStorageUnhealthy) return 'warning'
    if (summary.eta_confidence === 'high') return 'success'
    if (summary.eta_confidence === 'medium') return 'default'
    if (summary.eta_confidence === 'paused') return 'warning'
    return 'neutral'
  }

  return (
    <Panel className={cn('p-4 space-y-4 sm:p-5', className)}>
      <PanelHeader
        title={
          <div className="flex items-center gap-2">
            <span className="font-heading font-semibold">Warteschlangen-Status & ETA</span>
          </div>
        }
        action={
          <div className="flex items-center gap-2">
            <Badge variant={getEtaBadgeVariant()} className="font-medium text-xs">
              <ClockIcon className="size-3 mr-1" />
              {summary.eta_text}
            </Badge>
          </div>
        }
      />

      {isStorageUnhealthy && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/30 bg-warning/10 p-3 text-xs text-warning">
          <AlertTriangleIcon className="size-4 shrink-0" />
          <span>
            Der Bibliotheksspeicher ist aktuell nicht schreibbar oder nicht erreichbar. Downloads warten automatisch, bis der Speicher bereit ist.
          </span>
        </div>
      )}

      {/* Primary KPI Grid */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {/* Open Tracks */}
        <div className="rounded-lg border border-border/50 bg-white/[0.02] p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <ListMusicIcon className="size-3.5" />
            <span>Offene Tracks</span>
          </div>
          <div className="mt-1.5 text-xl font-bold tracking-tight text-foreground">
            {formatNumber(summary.remaining_items)}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            in anstehenden Jobs
          </div>
        </div>

        {/* Active Workers */}
        <div className="rounded-lg border border-border/50 bg-white/[0.02] p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <PlayCircleIcon className="size-3.5 text-primary" />
            <span>In Bearbeitung</span>
          </div>
          <div className="mt-1.5 text-xl font-bold tracking-tight text-foreground">
            {summary.active_items}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {pluralize(summary.active_items, 'aktiver Worker', 'aktive Worker')}
          </div>
        </div>

        {/* Throughput */}
        <div className="rounded-lg border border-border/50 bg-white/[0.02] p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <GaugeIcon className="size-3.5" />
            <span>Durchsatz</span>
          </div>
          <div className="mt-1.5 text-xl font-bold tracking-tight text-foreground">
            {summary.throughput_items_per_hour > 0
              ? `~${summary.throughput_items_per_hour.toFixed(0)}`
              : '—'}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.throughput_items_per_hour > 0
              ? 'Tracks / Stunde (gemessen)'
              : 'Berechnung läuft …'}
          </div>
        </div>

        {/* Paused Jobs */}
        <div className="rounded-lg border border-border/50 bg-white/[0.02] p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <PauseCircleIcon className="size-3.5 text-amber-400" />
            <span>Pausiert</span>
          </div>
          <div className="mt-1.5 text-xl font-bold tracking-tight text-foreground">
            {formatNumber(summary.paused_jobs)}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.paused_jobs > 0 ? 'Jobs (separat, nicht in ETA)' : 'Keine Jobs pausiert'}
          </div>
        </div>

        {/* Retry Wait / Issues */}
        <div className="col-span-2 sm:col-span-1 rounded-lg border border-border/50 bg-white/[0.02] p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <AlertTriangleIcon className={cn('size-3.5', summary.retry_wait_items > 0 ? 'text-warning' : 'text-muted-foreground')} />
            <span>Retry-Wartend</span>
          </div>
          <div className={cn('mt-1.5 text-xl font-bold tracking-tight', summary.retry_wait_items > 0 ? 'text-warning' : 'text-foreground')}>
            {summary.retry_wait_items}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.retry_wait_items > 0 ? 'Warten auf Wiederholung' : '0 Fehler'}
          </div>
        </div>
      </div>
    </Panel>
  )
}
