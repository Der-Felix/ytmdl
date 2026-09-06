import { useCallback, useMemo, useState } from 'react'
import {
  AlertTriangleIcon,
  CheckCircle2Icon,
  ClockIcon,
  DownloadIcon,
  Loader2Icon,
  PauseCircleIcon,
  PlayCircleIcon,
  RefreshCwIcon,
  Trash2Icon,
} from 'lucide-react'

import { ActiveWorkersCard } from '@/components/downloads/ActiveWorkersCard'
import { JobCard } from '@/components/downloads/JobCard'
import { NextUpCard } from '@/components/downloads/NextUpCard'
import { QueueSummaryCard } from '@/components/downloads/QueueSummaryCard'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

import { Pagination } from '@/components/ui/pagination'
import { Panel } from '@/components/ui/panel'
import {
  EmptyState,
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { useAuth } from '@/hooks/useAuth'
import { useCurrentTracks, useJobs } from '@/hooks/useJobs'
import { useQueueSummary } from '@/hooks/useQueueSummary'
import { deleteJobHistory, section } from '@/lib/api/jobs'
import { errorMessage, isAbortError } from '@/lib/api/client'
import { useLocation, useNavigate } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatNumber, pluralize } from '@/lib/utils/format'
import type { JobPriority } from '@/types/api'

type TabKey = 'all' | 'active' | 'queued' | 'paused' | 'done' | 'failed'

interface DownloadsPageProps {
  jobId?: string
}

const PAGE_SIZE = 20

function Downloads({ jobId }: DownloadsPageProps) {
  const { isAdmin } = useAuth()
  const { params } = useLocation()
  const navigate = useNavigate()

  const validViews: TabKey[] = ['all', 'active', 'queued', 'paused', 'done', 'failed']
  const rawView = params.get('view') as TabKey
  const activeTab: TabKey = validViews.includes(rawView) ? rawView : 'all'

  const parsedPage = parseInt(params.get('page') || '1', 10)
  const page = parsedPage > 0 ? parsedPage : 1

  const rawPriority = params.get('priority') as JobPriority | 'all'
  const priorityFilter: JobPriority | 'all' =
    rawPriority === 'high' || rawPriority === 'normal' || rawPriority === 'low'
      ? rawPriority
      : 'all'

  const updateUrl = useCallback(
    (updates: { view?: TabKey; page?: number; priority?: JobPriority | 'all' }) => {
      const nextParams = new URLSearchParams(params.toString())
      if (updates.view !== undefined) {
        if (updates.view === 'all') nextParams.delete('view')
        else nextParams.set('view', updates.view)
      }
      if (updates.page !== undefined) {
        if (updates.page <= 1) nextParams.delete('page')
        else nextParams.set('page', updates.page.toString())
      }
      if (updates.priority !== undefined) {
        if (updates.priority === 'all') nextParams.delete('priority')
        else nextParams.set('priority', updates.priority)
      }
      const qs = nextParams.toString()
      navigate(qs ? `/downloads?${qs}` : '/downloads', { replace: true })
    },
    [params, navigate],
  )

  const queryPriority = priorityFilter === 'all' ? undefined : priorityFilter

  const { state, meta, reload, setJobs } = useJobs({
    limit: PAGE_SIZE,
    offset: (page - 1) * PAGE_SIZE,
    priority: queryPriority,
  })

  const currentTracks = useCurrentTracks()
  const { state: summaryState, reload: reloadSummary } = useQueueSummary()

  const [cleanupOpen, setCleanupOpen] = useState(false)
  const [cleanupDays, setCleanupDays] = useState(30)
  const [cleanupPending, setCleanupPending] = useState(false)
  const [cleanupError, setCleanupError] = useState<string | null>(null)
  const [cleanupSuccess, setCleanupSuccess] = useState<string | null>(null)

  const rawJobs = state.status === 'success' ? state.data : null

  const counts = useMemo(() => {
    let active = 0
    let queued = 0
    let paused = 0
    let done = 0
    let failed = 0

    if (rawJobs) {
      for (const job of rawJobs) {
        if (job.paused) paused++
        const s = section(job)
        if (s === 'active') active++
        else if (s === 'queued') queued++
        else if (s === 'done') done++
        else if (s === 'failed') failed++
      }
    }

    return {
      all: meta?.total ?? (rawJobs ? rawJobs.length : 0),
      active,
      queued,
      paused,
      done,
      failed,
    }
  }, [rawJobs, meta])

  const filteredJobs = useMemo(() => {
    if (!rawJobs) return []
    if (jobId) {
      const match = rawJobs.filter((j) => j.id === jobId)
      if (match.length > 0) return match
    }

    if (activeTab === 'all') return rawJobs
    if (activeTab === 'paused') return rawJobs.filter((j) => j.paused)
    return rawJobs.filter((j) => section(j) === activeTab)
  }, [rawJobs, activeTab, jobId])

  async function handleCleanup() {
    setCleanupPending(true)
    setCleanupError(null)
    setCleanupSuccess(null)
    try {
      const res = await deleteJobHistory(cleanupDays)
      setCleanupSuccess(
        `${pluralize(res.deleted_jobs, 'Job', 'Jobs')} und ${pluralize(
          res.deleted_items,
          'Track',
          'Tracks',
        )} erfolgreich aus dem Verlauf entfernt.`,
      )
      reload()
      setTimeout(() => {
        setCleanupOpen(false)
        setCleanupSuccess(null)
      }, 1800)
    } catch (err) {
      if (!isAbortError(err)) {
        setCleanupError(errorMessage(err))
      }
    } finally {
      setCleanupPending(false)
    }
  }

  const tabs: { key: TabKey; label: string; icon: React.ReactNode; count: number }[] = [

    { key: 'all', label: 'Alle', icon: <DownloadIcon className="size-3.5" />, count: counts.all },
    { key: 'active', label: 'Aktiv', icon: <PlayCircleIcon className="size-3.5" />, count: counts.active },
    { key: 'queued', label: 'Warteschlange', icon: <ClockIcon className="size-3.5" />, count: counts.queued },
    { key: 'paused', label: 'Pausiert', icon: <PauseCircleIcon className="size-3.5" />, count: counts.paused },
    { key: 'done', label: 'Fertig', icon: <CheckCircle2Icon className="size-3.5" />, count: counts.done },
    { key: 'failed', label: 'Fehlgeschlagen', icon: <AlertTriangleIcon className="size-3.5" />, count: counts.failed },
  ]

  return (
    <div className="space-y-6">
      <header className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="space-y-1">
          <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
            Downloads & Warteschlange
          </h1>
          <p className="text-sm text-muted-foreground">
            Verwalten Sie laufende und anstehende Download-Aufträge, Prioritäten und Verlaufsdaten.
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {isAdmin && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setCleanupOpen(true)}
              title="Bereinigt alte Verlaufsdaten aus der Warteschlange"
            >
              <Trash2Icon className="size-3.5" />
              Verlauf bereinigen
            </Button>
          )}

          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              reload()
              reloadSummary()
            }}
            disabled={state.status === 'loading' || summaryState.status === 'loading'}
          >
            <RefreshCwIcon
              className={cn(
                'size-3.5',
                (state.status === 'loading' || summaryState.status === 'loading') && 'animate-spin',
              )}
            />
            Aktualisieren
          </Button>
        </div>
      </header>

      {/* Live Queue ETA and Worker Previews */}
      {summaryState.status === 'success' && (
        <section aria-label="Warteschlangen-Status und Vorschau" className="space-y-4">
          <QueueSummaryCard summary={summaryState.data} />

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <ActiveWorkersCard workers={summaryState.data.current} />
            <NextUpCard jobs={summaryState.data.next} />
          </div>
        </section>
      )}

      {summaryState.status === 'loading' && (
        <div className="space-y-4" aria-busy="true">
          <div className="h-44 rounded-xl border border-border/50 bg-white/[0.02] animate-pulse" />
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div className="h-40 rounded-xl border border-border/50 bg-white/[0.02] animate-pulse" />
            <div className="h-40 rounded-xl border border-border/50 bg-white/[0.02] animate-pulse" />
          </div>
        </div>
      )}

      {/* Filter Toolbar */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between border-b border-border pb-3">
        {/* Status Tabs */}
        <div className="flex flex-wrap items-center gap-1.5 overflow-x-auto py-1">
          {tabs.map((tab) => {
            const isActive = activeTab === tab.key
            return (
              <button
                key={tab.key}
                type="button"
                onClick={() => updateUrl({ view: tab.key, page: 1 })}
                className={cn(
                  'flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors',
                  isActive
                    ? 'bg-primary text-primary-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                )}
              >
                {tab.icon}
                <span>{tab.label}</span>
                {tab.count > 0 && (
                  <span
                    className={cn(
                      'ml-0.5 rounded-full px-1.5 py-0.2 text-[0.6875rem] font-semibold',
                      isActive ? 'bg-primary-foreground/20 text-primary-foreground' : 'bg-muted text-muted-foreground',
                    )}
                  >
                    {formatNumber(tab.count)}
                  </span>
                )}
              </button>
            )
          })}
        </div>

        {/* Priority Filter */}
        <div className="flex items-center gap-2 shrink-0">
          <label htmlFor="priority-filter" className="text-xs text-muted-foreground whitespace-nowrap">
            Priorität:
          </label>
          <select
            id="priority-filter"
            value={priorityFilter}
            onChange={(e) => updateUrl({ priority: e.target.value as JobPriority | 'all', page: 1 })}
            className="rounded-lg border border-border bg-background px-2.5 py-1 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
          >
            <option value="all">Alle Prioritäten</option>
            <option value="very_high">Sehr hoch</option>
            <option value="high">Hoch</option>
            <option value="normal">Normal</option>
            <option value="low">Niedrig</option>
          </select>
        </div>
      </div>

      {state.status === 'loading' && (
        <LoadingRegion label="Downloads werden geladen">
          <ListSkeleton rows={3} />
        </LoadingRegion>
      )}

      {state.status === 'error' && (
        <Panel>
          <ErrorState error={state.error} onRetry={reload} />
        </Panel>
      )}

      {state.status === 'success' && filteredJobs.length === 0 && (
        <Panel>
          <EmptyState
            icon={<DownloadIcon />}
            title={activeTab === 'all' ? 'Keine Downloads vorhanden' : `Keine Jobs im Status "${activeTab}"`}
            description={
              priorityFilter !== 'all'
                ? 'Mit dem gewählten Prioritätsfilter wurden keine Aufträge gefunden.'
                : 'Fügen Sie neue Downloads über die Suche oder Entdecken-Seite hinzu.'
            }
          />
        </Panel>
      )}

      {state.status === 'success' && filteredJobs.length > 0 && (
        <div className="space-y-4">
          <div className="space-y-3">
            {filteredJobs.map((job) => (
              <JobCard
                key={job.id}
                job={job}
                currentTrack={currentTracks[job.id]}
                onUpdated={(updated) =>
                  setJobs((list) =>
                    list.map((j) => (j.id === updated.id ? updated : j)),
                  )
                }
                onCancelled={(cancelled) =>
                  setJobs((list) =>
                    list.map((j) => (j.id === cancelled.id ? cancelled : j)),
                  )
                }
              />
            ))}

          </div>

          {meta?.total && meta.total > PAGE_SIZE ? (
            <Pagination
              page={page}
              pageSize={PAGE_SIZE}
              total={meta.total}
              onPageChange={(p) => updateUrl({ page: p })}
            />
          ) : null}
        </div>
      )}

      {/* History Cleanup Dialog */}
      <Dialog open={cleanupOpen} onOpenChange={setCleanupOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Download-Verlauf bereinigen</DialogTitle>
            <DialogDescription>
              Löscht alte abgeschlossene und abgebrochene Download-Jobs aus der
              Datenbank.
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-2 text-sm text-foreground">
            <div className="rounded-xl border border-border/60 bg-muted/30 p-3 text-xs leading-relaxed text-muted-foreground">
              <strong className="text-foreground">Sicherheitsgarantie:</strong>{' '}
              Ihre Musikbibliothek, Audiodateien, Metadaten und Cover bleiben zu 100%
              unberührt. Es werden ausschließlich Verlaufsdaten aus der Warteschlange
              entfernt.
            </div>

            <div className="space-y-2">
              <label htmlFor="cleanup-days" className="text-xs font-medium text-foreground">
                Jobs löschen, die älter sind als:
              </label>
              <select
                id="cleanup-days"
                value={cleanupDays}
                onChange={(e) => setCleanupDays(Number(e.target.value))}
                className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              >
                <option value={7}>7 Tage</option>
                <option value={14}>14 Tage</option>
                <option value={30}>30 Tage (Standard)</option>
                <option value={60}>60 Tage</option>
                <option value={90}>90 Tage</option>
              </select>
            </div>

            {cleanupError && (
              <p role="alert" className="text-xs text-destructive">
                {cleanupError}
              </p>
            )}

            {cleanupSuccess && (
              <p role="status" className="text-xs text-success font-medium">
                {cleanupSuccess}
              </p>
            )}
          </div>

          <DialogFooter>
            <Button
              variant="ghost"
              disabled={cleanupPending}
              onClick={() => setCleanupOpen(false)}
            >
              Abbrechen
            </Button>
            <Button
              variant="destructive"
              onClick={handleCleanup}
              disabled={cleanupPending}
            >
              {cleanupPending && <Loader2Icon className="size-3.5 animate-spin" />}
              Jetzt bereinigen
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export { Downloads }
