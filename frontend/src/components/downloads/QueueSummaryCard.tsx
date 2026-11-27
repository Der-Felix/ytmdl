import { useState } from 'react'
import {
  AlertTriangleIcon,
  ClockIcon,
  GaugeIcon,
  ListMusicIcon,
  Loader2Icon,
  PauseCircleIcon,
  PlayCircleIcon,
  RotateCcwIcon,
  ShieldAlertIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Panel, PanelHeader } from '@/components/ui/panel'
import { errorMessage } from '@/lib/api/client'
import { probeProviderPause, retryBlockedFailures } from '@/lib/api/jobs'
import { cn } from '@/lib/utils'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { QueueSummary } from '@/types/api'

interface QueueSummaryCardProps {
  summary: QueueSummary
  className?: string
  /** Shows the queue-wide actions (probe now, requeue blocked failures); admins only. */
  canManage?: boolean
  /** Called after an action changed the queue, so the summary is reloaded. */
  onChanged?: () => void
}

export function QueueSummaryCard({ summary, className, canManage = false, onChanged }: QueueSummaryCardProps) {
  const isStorageUnhealthy = !summary.storage_healthy
  const pause = summary.provider_pauses?.[0]
  const blockedFailed = summary.blocked_failed_items ?? 0
  const [busy, setBusy] = useState<'probe' | 'requeue' | null>(null)
  const [feedback, setFeedback] = useState<{ tone: 'ok' | 'error'; text: string } | null>(null)

  // "Pause beendet …" describes the probe that was just sent. Once the probe
  // has an outcome (paused again until a new time, or reopened) it is stale.
  const pauseState = pause ? `${pause.phase}|${pause.resumes_at ?? ''}` : 'open'
  const [feedbackFor, setFeedbackFor] = useState(pauseState)
  if (feedbackFor !== pauseState) {
    setFeedbackFor(pauseState)
    if (feedback?.tone === 'ok' && pauseState.startsWith('paused')) setFeedback(null)
  }

  async function run(action: 'probe' | 'requeue') {
    setBusy(action)
    setFeedback(null)
    try {
      if (action === 'probe') {
        await probeProviderPause()
        setFeedback({ tone: 'ok', text: 'Pause beendet. Ein einzelner Download prüft jetzt, ob die Sperre aufgehoben ist.' })
      } else {
        const res = await retryBlockedFailures()
        setFeedback({
          tone: 'ok',
          text:
            res.items > 0
              ? `${pluralize(res.items, 'Track', 'Tracks')} aus ${pluralize(res.jobs, 'Job', 'Jobs')} wieder eingereiht.`
              : 'Keine gesperrten Tracks mehr zum Einreihen.',
        })
      }
      onChanged?.()
    } catch (err) {
      setFeedback({ tone: 'error', text: errorMessage(err) })
    } finally {
      setBusy(null)
    }
  }

  const getEtaBadgeVariant = () => {
    if (isStorageUnhealthy) return 'warning'
    if (summary.eta_confidence === 'provider_paused') return 'warning'
    if (summary.eta_confidence === 'high') return 'success'
    if (summary.eta_confidence === 'medium') return 'default'
    if (summary.eta_confidence === 'paused') return 'warning'
    return 'neutral'
  }

  return (
    <Panel className={cn('p-4 space-y-4 sm:p-5', className)}>
      <PanelHeader
        className="flex-col gap-2 sm:flex-row"
        title={
          <div className="flex items-center gap-2">
            <span className="font-heading font-semibold">Deine Warteschlange</span>
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

      {pause && (
        <div
          role="status"
          className="flex flex-col gap-3 rounded-lg border border-warning/30 bg-warning/10 p-3 text-xs text-warning sm:flex-row sm:items-center sm:justify-between"
        >
          <div className="flex min-w-0 items-start gap-2">
            {pause.phase === 'probing' ? (
              <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin motion-reduce:animate-none" aria-hidden />
            ) : (
              <ShieldAlertIcon className="mt-0.5 size-4 shrink-0" aria-hidden />
            )}
            <span className="leading-relaxed">{pause.message}</span>
          </div>
          {canManage && pause.phase === 'paused' && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => run('probe')}
              disabled={busy !== null}
              className="shrink-0 self-start border-warning/40 text-warning sm:self-auto"
            >
              {busy === 'probe' ? <Loader2Icon className="size-3.5 animate-spin" /> : <RotateCcwIcon className="size-3.5" />}
              Jetzt erneut prüfen
            </Button>
          )}
        </div>
      )}

      {canManage && blockedFailed > 0 && (
        <div className="flex flex-col gap-3 rounded-lg border border-border/50 bg-muted p-3 text-xs text-muted-foreground sm:flex-row sm:items-center sm:justify-between">
          <span className="leading-relaxed">
            <span className="figures font-semibold text-foreground">{formatNumber(blockedFailed)}</span>{' '}
            {blockedFailed === 1 ? 'Track ist' : 'Tracks sind'} endgültig fehlgeschlagen, weil der Anbieter
            Anfragen gesperrt hat. Sie lassen sich mit neuen Versuchen wieder einreihen.
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => run('requeue')}
            disabled={busy !== null}
            className="shrink-0 self-start sm:self-auto"
          >
            {busy === 'requeue' ? <Loader2Icon className="size-3.5 animate-spin" /> : <RotateCcwIcon className="size-3.5" />}
            Wieder einreihen
          </Button>
        </div>
      )}

      {feedback && (
        <p
          role={feedback.tone === 'error' ? 'alert' : 'status'}
          className={cn('text-xs', feedback.tone === 'error' ? 'text-destructive' : 'text-muted-foreground')}
        >
          {feedback.text}
        </p>
      )}

      {/* Primary KPI Grid */}
      {/* Five columns only from xl: beside the sidebar at lg the labels
          ("In Bearbeitung", "Wiederholung geplant") would wrap. */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-5">
        {/* Open Tracks */}
        <div className="rounded-lg border border-border/50 bg-muted p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <ListMusicIcon className="size-3.5" />
            <span>Offene Tracks</span>
          </div>
          <div className="figures mt-1.5 text-xl font-bold text-foreground">
            {formatNumber(summary.remaining_items)}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            in anstehenden Jobs
          </div>
        </div>

        {/* Active Workers */}
        <div className="rounded-lg border border-border/50 bg-muted p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <PlayCircleIcon className="size-3.5 text-primary" />
            <span>In Bearbeitung</span>
          </div>
          <div className="figures mt-1.5 text-xl font-bold text-foreground">
            {summary.active_items}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {pluralize(summary.active_items, 'aktiver Worker', 'aktive Worker')}
          </div>
        </div>

        {/* Throughput */}
        <div className="rounded-lg border border-border/50 bg-muted p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <GaugeIcon className="size-3.5" />
            <span>Durchsatz</span>
          </div>
          <div className="figures mt-1.5 text-xl font-bold text-foreground">
            {summary.throughput_items_per_hour > 0
              ? `~${summary.throughput_items_per_hour.toFixed(0)}`
              : '—'}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.throughput_items_per_hour > 0
              ? 'Tracks / Stunde (gemessen)'
              : pause
                ? 'Anbieter pausiert'
                : 'Berechnung läuft …'}
          </div>
        </div>

        {/* Paused Jobs */}
        <div className="rounded-lg border border-border/50 bg-muted p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <PauseCircleIcon className="size-3.5 text-warning" />
            <span>Pausiert</span>
          </div>
          <div className="figures mt-1.5 text-xl font-bold text-foreground">
            {formatNumber(summary.paused_jobs)}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.paused_jobs > 0 ? 'Jobs (separat, nicht in ETA)' : 'Keine Jobs pausiert'}
          </div>
        </div>

        {/* Retry Wait / Issues */}
        <div className="col-span-2 sm:col-span-1 rounded-lg border border-border/50 bg-muted p-3">
          <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <AlertTriangleIcon className={cn('size-3.5', summary.retry_wait_items > 0 ? 'text-warning' : 'text-muted-foreground')} />
            <span>Wiederholung geplant</span>
          </div>
          <div className={cn('figures mt-1.5 text-xl font-bold', summary.retry_wait_items > 0 ? 'text-warning' : 'text-foreground')}>
            {summary.retry_wait_items}
          </div>
          <div className="mt-0.5 text-[0.6875rem] text-muted-foreground">
            {summary.retry_wait_items > 0 ? 'Warten auf Wiederholung' : 'Keine Wiederholung ausstehend'}
          </div>
        </div>
      </div>
    </Panel>
  )
}
