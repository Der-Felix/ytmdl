import { ReleaseAnnouncementNotice } from './ReleaseAnnouncementNotice'
import { useState } from 'react'
import {
  ChevronDownIcon,
  ChevronUpIcon,
  Loader2Icon,
  PauseIcon,
  PlayIcon,
  RotateCcwIcon,
  XIcon,
  AlertCircleIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Progress } from '@/components/ui/progress'
import { ProblemNotice } from '@/components/ui/problem-notice'
import { ErrorState } from '@/components/ui/state-view'
import {
  ITEM_STATUS_LABELS,
  JOB_PRIORITY_LABELS,
  JOB_STATUS_LABELS,
  JOB_TYPE_LABELS,
  cancelJob,
  getJob,
  isTerminal,
  isWaitingForProvider,
  pauseJob,
  processed,
  progressPercent,
  resumeJob,
  retryFailedJob,
  retryJobItem,
  updateJob,
} from '@/lib/api/jobs'
import { isAbortError } from '@/lib/api/client'
import { cn } from '@/lib/utils'
import { formatContinuationTime, formatNumber, formatRelative } from '@/lib/utils/format'
import type { Job, JobItem, JobPriority, JobStatus } from '@/types/api'

interface JobCardProps {
  job: Job
  /** The track a worker is on right now; only known while SSE is connected. */
  currentTrack?: string
  onCancelled?: (job: Job) => void
  onUpdated?: (job: Job) => void
  className?: string
}

/**
 * One download job.
 *
 * Failed tracks are reported as a count next to the successful ones, never as
 * the state of the whole job: a discography where one track could not be
 * matched is a job that completed, not a job that failed.
 */
function JobCard({
  job,
  currentTrack,
  onCancelled,
  onUpdated,
  className,
}: JobCardProps) {
  const percent = progressPercent(job)
  const done = isTerminal(job)

  const [expanded, setExpanded] = useState(false)
  const [items, setItems] = useState<JobItem[] | null>(null)
  const [loadingItems, setLoadingItems] = useState(false)
  const [itemFilter, setItemFilter] = useState<'all' | 'failed'>('all')
  const [detailError, setDetailError] = useState<unknown>(null)
  const [actionPending, setActionPending] = useState(false)
  const [actionError, setActionError] = useState<unknown>(null)

  async function showDetails(filter: 'all' | 'failed' = 'all') {
    setItemFilter(filter)
    setExpanded(true)
    if (!expanded || items === null) {
      setLoadingItems(true)
      setDetailError(null)
      try {
        const detail = await getJob(job.id)
        setItems(detail.items)
      } catch (e) {
        if (!isAbortError(e)) setDetailError(e)
      } finally {
        setLoadingItems(false)
      }
    }
  }

  function toggleExpand() {
    if (expanded) setExpanded(false)
    else void showDetails()
  }

  async function handleTogglePause() {
    setActionPending(true)
    setActionError(null)
    try {
      const updated = job.paused ? await resumeJob(job.id) : await pauseJob(job.id)
      onUpdated?.(updated)
    } catch (e) {
      if (!isAbortError(e)) setActionError(e)
    } finally {
      setActionPending(false)
    }
  }

  async function handleSetPriority(priority: JobPriority) {
    if (priority === job.priority) return
    setActionPending(true)
    setActionError(null)
    try {
      const updated = await updateJob(job.id, { priority })
      onUpdated?.(updated)
    } catch (e) {
      if (!isAbortError(e)) setActionError(e)
    } finally {
      setActionPending(false)
    }
  }

  async function handleRetryFailed() {
    setActionPending(true)
    setActionError(null)
    try {
      const res = await retryFailedJob(job.id)
      onUpdated?.(res.job)
      if (expanded) {
        const detail = await getJob(job.id)
        setItems(detail.items)
      }
    } catch (e) {
      if (!isAbortError(e)) setActionError(e)
    } finally {
      setActionPending(false)
    }
  }

  async function handleRetryItem(itemId: string) {
    setActionPending(true)
    setActionError(null)
    try {
      await retryJobItem(job.id, itemId)
      const detail = await getJob(job.id)
      setItems(detail.items)
      onUpdated?.(detail.job)
    } catch (e) {
      if (!isAbortError(e)) setActionError(e)
    } finally {
      setActionPending(false)
    }
  }

  return (
    <article className={cn('panel space-y-3.5 p-4 sm:p-5', className)}>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2">
            <h3 className="min-w-0 break-words font-heading text-[0.9375rem] font-semibold text-foreground">
              {job.label || 'Unbenannter Job'}
            </h3>
            {job.paused && (
              <Badge variant="warning" className="shrink-0 text-[0.6875rem]">
                Pausiert
              </Badge>
            )}
          </div>
          <p className="truncate text-xs text-muted-foreground">
            {JOB_TYPE_LABELS[job.type]}
            {job.created_at && ` · ${formatRelative(job.created_at)}`}
          </p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center gap-2">
          <PriorityBadge priority={job.priority} />
          <StatusBadge status={job.status} errorCode={job.error_code} failed={job.failed} />
        </div>
      </div>

      {!done && (
        <div className="space-y-2">
          <Progress value={percent} aria-label="Fortschritt" />
          <div className="flex items-baseline justify-between gap-3 text-xs">
            <span className="truncate text-muted-foreground">
              {currentTrack ? (
                <span className="inline-flex items-center gap-1.5">
                  <Loader2Icon className="size-3 shrink-0 animate-spin text-primary" />
                  <span className="truncate">{currentTrack}</span>
                </span>
              ) : job.paused ? (
                'Pausiert (aktive Downloads laufen aus)'
              ) : isWaitingForProvider(job) ? (
                'Wartet auf Provider'
              ) : (
                JOB_STATUS_LABELS[job.status]
              )}
            </span>
            <span className="shrink-0 tabular-nums text-muted-foreground">
              {job.total > 0
                ? `${formatNumber(processed(job))} / ${formatNumber(job.total)}`
                : 'Wird ermittelt'}
              {percent !== null && ` · ${percent} %`}
            </span>
          </div>
        </div>
      )}

      <Outcome job={job} />

      {(job.failed > 0 || job.status === 'failed') && <ReleaseAnnouncementNotice announcement={job.options.release_announcement} />}

      {(job.error_code || job.error_message || job.status === 'failed') && (
        <ProblemNotice
          code={job.error_code}
          waiting={!done && (job.status === 'retry_wait' || job.status === 'waiting_for_storage' || job.status === 'waiting_for_space')}
          continuation={job.status === 'retry_wait' ? 'Dieser Auftrag wird automatisch fortgesetzt, sobald er wieder an der Reihe ist und die Musikquelle bereit ist.' : undefined}
        />
      )}

      {job.failed > 0 && (
        <button
          type="button"
          onClick={() => void showDetails('failed')}
          className="flex w-full items-center justify-between gap-3 rounded-lg border border-amber-500/20 bg-amber-500/5 px-3 py-2.5 text-left text-xs text-foreground transition-colors hover:bg-amber-500/10 focus-visible:outline-2 focus-visible:outline-ring"
        >
          <span className="flex items-center gap-2"><AlertCircleIcon aria-hidden className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />{formatNumber(job.failed)} {job.failed === 1 ? 'Track braucht' : 'Tracks brauchen'} Prüfung</span>
          <span className="shrink-0 text-muted-foreground">Ansehen</span>
        </button>
      )}

      {actionError !== null && <ErrorState error={actionError} />}

      {/* Job Actions Bar */}
      <div className="flex flex-wrap items-center justify-between gap-2 pt-1 border-t border-border/40">
        <div className="flex flex-wrap items-center gap-2">
          {!done && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={handleTogglePause}
                disabled={actionPending}
                className="h-7 text-xs"
              >
                {actionPending ? (
                  <Loader2Icon className="size-3 animate-spin" />
                ) : job.paused ? (
                  <PlayIcon className="size-3 text-success" />
                ) : (
                  <PauseIcon className="size-3 text-muted-foreground" />
                )}
                {job.paused ? 'Fortsetzen' : 'Pausieren'}
              </Button>

              <div className="flex items-center gap-1 text-xs text-muted-foreground pl-1">
                <span>Priorität:</span>
                <select
                  value={job.priority}
                  onChange={(e) => handleSetPriority(e.target.value as JobPriority)}
                  disabled={actionPending}
                  aria-label="Priorität ändern"
                  className="rounded border border-border bg-background px-1.5 py-0.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                >
                  <option value="very_high">Sehr hoch</option>
                  <option value="high">Hoch</option>
                  <option value="normal">Normal</option>
                  <option value="low">Niedrig</option>
                </select>
              </div>
            </>
          )}

          {job.failed > 0 && (
            <Button
              variant="outline"
              size="sm"
              onClick={handleRetryFailed}
              disabled={actionPending}
              className="h-7 text-xs border-destructive/30 text-destructive hover:bg-destructive/10"
            >
              {actionPending ? (
                <Loader2Icon className="size-3 animate-spin" />
              ) : (
                <RotateCcwIcon className="size-3" />
              )}
              Fehlgeschlagene wiederholen
            </Button>
          )}
        </div>

        <div className="flex items-center gap-2">
          {job.total > 0 && (
            <Button
              variant="ghost"
              size="sm"
              onClick={toggleExpand}
              aria-expanded={expanded}
              className="h-7 text-xs text-muted-foreground"
            >
              {loadingItems ? (
                <Loader2Icon className="size-3 animate-spin" />
              ) : expanded ? (
                <ChevronUpIcon className="size-3" />
              ) : (
                <ChevronDownIcon className="size-3" />
              )}
              {expanded ? 'Details verbergen' : 'Details'}
            </Button>
          )}

          {!done && <CancelButton job={job} onCancelled={onCancelled} />}
        </div>
      </div>

      {/* Expanded Items Drawer */}
      {expanded && loadingItems && <p role="status" className="text-xs text-muted-foreground">Tracks werden geladen …</p>}
      {expanded && detailError !== null && <ErrorState error={detailError} onRetry={() => void showDetails(itemFilter)} />}
      {expanded && items && (
        <div className="mt-3 space-y-2 rounded-lg border border-border/60 bg-muted/20 p-3">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h4 className="text-xs font-semibold text-foreground">Tracks ({formatNumber(items.length)})</h4>
            <div role="group" aria-label="Tracks filtern" className="flex gap-1">
              {(['all', 'failed'] as const).map((filter) => (
                <button key={filter} type="button" aria-pressed={itemFilter === filter} onClick={() => setItemFilter(filter)} className={cn('rounded-md px-2.5 py-1.5 text-xs transition-colors focus-visible:outline-2 focus-visible:outline-ring', itemFilter === filter ? 'bg-secondary text-foreground' : 'text-muted-foreground hover:text-foreground')}>
                  {filter === 'all' ? 'Alle Tracks' : `Fehlgeschlagene (${items.filter((item) => item.status === 'failed').length})`}
                </button>
              ))}
            </div>
          </div>
          {itemFilter === 'failed' && !items.some((item) => item.status === 'failed') && <p className="py-3 text-xs text-muted-foreground">Keine fehlgeschlagenen Tracks in diesen Details.</p>}
          <div className="max-h-96 space-y-3 overflow-y-auto">
            {items.filter((item) => itemFilter === 'all' || item.status === 'failed').map((item) => (
              <div
                key={item.id}
                className="space-y-2 rounded-lg bg-background/40 p-3 text-xs"
              >
                <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between sm:gap-3">
                  <div className="min-w-0 flex-1 break-words">
                    <span className="font-medium text-foreground">
                      {item.track?.title || item.label || 'Track'}
                    </span>
                    {item.track?.artists?.length ? (
                      <span className="text-muted-foreground">
                        {' '}
                        · {item.track.artists.join(', ')}
                      </span>
                    ) : null}
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <ItemStatusBadge status={item.status} errorCode={item.error_code} />
                    {(item.status === 'failed' || (item.status === 'retry_wait' && !isWaitingForProvider(item))) && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleRetryItem(item.id)}
                        disabled={actionPending}
                        className="h-6 px-1.5 text-[0.6875rem]"
                        title="Track jetzt wiederholen"
                        aria-label={`${item.track?.title || item.label || 'Track'} erneut versuchen`}
                      >
                        <RotateCcwIcon className="size-2.5" />
                      </Button>
                    )}
                  </div>
                </div>
                {(item.error_code || item.error_message || item.status === 'failed' || item.status === 'retry_wait') && (
                  <ProblemNotice
                    compact
                    code={item.error_code}
                    waiting={item.status === 'retry_wait' || item.status === 'waiting_for_storage' || item.status === 'waiting_for_space'}
                    continuation={item.status === 'retry_wait' ? (item.next_retry_at ? formatContinuationTime(item.next_retry_at) : 'Automatische Wiederholung geplant') : undefined}
                  />
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </article>
  )
}

/** The tally of what the job produced. */
function Outcome({ job }: { job: Job }) {
  const parts: {
    label: string
    value: number
    tone: 'success' | 'destructive' | 'neutral'
  }[] = [
    { label: 'erfolgreich', value: job.completed, tone: 'success' },
    { label: 'fehlgeschlagen', value: job.failed, tone: 'destructive' },
    { label: 'übersprungen', value: job.skipped, tone: 'neutral' },
  ]
  const visible = parts.filter((part) => part.value > 0)
  if (visible.length === 0) return null

  return (
    <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
      {visible.map((part) => (
        <li key={part.label} className="flex items-center gap-1.5">
          <span
            aria-hidden
            className={cn(
              'size-1.5 rounded-full',
              part.tone === 'success' && 'bg-success',
              part.tone === 'destructive' && 'bg-destructive',
              part.tone === 'neutral' && 'bg-muted-foreground',
            )}
          />
          <span className="tabular-nums text-foreground">{formatNumber(part.value)}</span>
          <span className="text-muted-foreground">{part.label}</span>
        </li>
      ))}
    </ul>
  )
}

const STATUS_TONE: Record<
  JobStatus,
  'default' | 'success' | 'destructive' | 'neutral' | 'warning'
> = {
  queued: 'neutral',
  resolving_artist: 'default',
  resolving_releases: 'default',
  resolving_tracks: 'default',
  deduplicating: 'default',
  matching: 'default',
  downloading: 'default',
  tagging: 'default',
  finalizing: 'default',
  retry_wait: 'warning',
  waiting_for_storage: 'warning',
  waiting_for_space: 'warning',
  completed: 'success',
  failed: 'destructive',
  cancelled: 'neutral',
}

function StatusBadge({ status, errorCode, failed = 0 }: { status: JobStatus; errorCode?: string; failed?: number }) {
  const isWaiting = isWaitingForProvider({ status, error_code: errorCode })
  const partial = status === 'completed' && failed > 0
  const tone = isWaiting || partial ? 'warning' : STATUS_TONE[status]
  const label = partial ? 'Abgeschlossen mit Problemen' : isWaiting ? 'Wartet auf Provider' : (JOB_STATUS_LABELS[status] || status)

  return (
    <Badge variant={tone} className="shrink-0">
      {label}
    </Badge>
  )
}

function PriorityBadge({ priority }: { priority?: JobPriority }) {
  const p = priority || 'normal'
  const tone =
    p === 'very_high'
      ? 'border-rose-500/30 bg-rose-500/10 text-rose-500 font-semibold'
      : p === 'high'
      ? 'border-amber-500/30 bg-amber-500/10 text-amber-500'
      : p === 'low'
      ? 'border-slate-500/20 bg-slate-500/10 text-muted-foreground'
      : 'border-border bg-secondary text-secondary-foreground'

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border px-1.5 py-0.5 text-[0.6875rem] font-medium',
        tone,
      )}
    >
      {JOB_PRIORITY_LABELS[p]}
    </span>
  )
}

function ItemStatusBadge({ status, errorCode }: { status: string; errorCode?: string }) {
  const isWaiting = isWaitingForProvider({ status, error_code: errorCode })
  const tone =
    status === 'completed'
      ? 'text-success'
      : status === 'failed'
      ? 'text-destructive'
      : status === 'retry_wait' ||
        status === 'waiting_for_storage' ||
        status === 'waiting_for_space'
      ? 'text-amber-500'
      : 'text-muted-foreground'

  const label = isWaiting ? 'Wartet auf Provider' : (ITEM_STATUS_LABELS[status] || status)

  return (
    <span className={cn('text-[0.6875rem] font-medium', tone)}>
      {label}
    </span>
  )
}

function CancelButton({
  job,
  onCancelled,
}: {
  job: Job
  onCancelled?: (job: Job) => void
}) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)

  async function handleCancel() {
    setPending(true)
    setError(null)
    try {
      const cancelled = await cancelJob(job.id)
      onCancelled?.(cancelled)
    } catch (caught) {
      if (!isAbortError(caught)) setError(caught)
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex items-center gap-2">
      <Button
        variant="ghost"
        size="sm"
        onClick={handleCancel}
        disabled={pending}
        className="h-7 text-xs text-muted-foreground hover:text-destructive"
      >
        {pending ? <Loader2Icon className="size-3 animate-spin" /> : <XIcon className="size-3" />}
        Abbrechen
      </Button>
      {error !== null && <ErrorState error={error} />}
    </div>
  )
}

export { JobCard }
