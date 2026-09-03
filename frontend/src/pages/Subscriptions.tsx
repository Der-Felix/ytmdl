import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  AlertCircleIcon,
  BellIcon,
  CheckIcon,
  DownloadIcon,
  Loader2Icon,
  PlayIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  Settings2Icon,
  Trash2Icon,
  UploadIcon,
  XIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Cover } from '@/components/music/Cover'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Panel } from '@/components/ui/panel'
import {
  EmptyState,
  ErrorState,
  ListSkeleton,
  LoadingRegion,
} from '@/components/ui/state-view'
import { SubscriptionImportDialog } from '@/components/subscriptions/SubscriptionImportDialog'
import { useAsync } from '@/hooks/useAsync'
import { useJobEvents } from '@/hooks/useJobs'
import { errorMessage, isAbortError } from '@/lib/api/client'
import {
  SYNC_STATUS_LABELS,
  exportSubscriptionsToFile,
  getSubscription,
  listSubscriptions,
  syncSubscription,
  syncStatusTone,
  unsubscribe,
  updateSubscription,
} from '@/lib/api/subscriptions'
import { Link, paths } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatDateTime, formatRelative, pluralize } from '@/lib/utils/format'
import type {
  JobEvent,
  JobPriority,
  ReleaseFilter,
  Subscription,
  SyncStatus,
} from '@/types/api'

type SortOption =
  | 'name_asc'
  | 'name_desc'
  | 'last_sync_desc'
  | 'last_sync_asc'
  | 'next_sync_asc'
  | 'created_desc'
  | 'status'

export function Subscriptions() {
  const { state, reload, setData } = useAsync(
    (signal) => listSubscriptions({ limit: 500, signal }),
    [],
  )
  const [failures, setFailures] = useState<Record<string, string>>({})
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set())
  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [bulkDeleting, setBulkDeleting] = useState(false)
  const [bulkActionLoading, setBulkActionLoading] = useState(false)

  // Search & Filter state
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<'all' | SyncStatus>('all')
  const [enabledFilter, setEnabledFilter] = useState<'all' | 'enabled' | 'disabled'>('all')
  const [autoDownloadFilter, setAutoDownloadFilter] = useState<'all' | 'on' | 'off'>('all')
  const [providerFilter, setProviderFilter] = useState<string>('all')
  const [sortBy, setSortBy] = useState<SortOption>('name_asc')

  const controllersRef = useRef(new Set<AbortController>())
  useEffect(() => {
    const controllers = controllersRef.current
    return () => {
      for (const controller of controllers) controller.abort()
      controllers.clear()
    }
  }, [])

  const patch = useCallback(
    (id: string, change: Partial<Subscription>) => {
      setData((list) =>
        list.map((item) => (item.id === id ? { ...item, ...change } : item)),
      )
    },
    [setData],
  )

  const refresh = useCallback(
    async (id: string) => {
      try {
        const { subscription } = await getSubscription(id)
        patch(id, subscription)
      } catch {
        // Background refresh failure is ignored
      }
    },
    [patch],
  )

  useJobEvents(
    useCallback(
      (event: JobEvent) => {
        const id = event.subscription_id
        if (!id) return

        switch (event.type) {
          case 'subscription.sync.started':
            patch(id, { syncing: true })
            break
          case 'subscription.sync.completed':
          case 'subscription.sync.failed':
            patch(id, { syncing: false })
            void refresh(id)
            break
        }
      },
      [patch, refresh],
    ),
  )

  const act = useCallback(
    async (id: string, action: (signal: AbortSignal) => Promise<Subscription | void>) => {
      setFailures((current) => {
        if (!(id in current)) return current
        const { [id]: _gone, ...rest } = current
        return rest
      })
      const controller = new AbortController()
      controllersRef.current.add(controller)
      try {
        const updated = await action(controller.signal)
        if (updated) patch(id, updated)
        else reload()
      } catch (cause) {
        if (isAbortError(cause)) return
        setFailures((current) => ({ ...current, [id]: errorMessage(cause) }))
      } finally {
        controllersRef.current.delete(controller)
      }
    },
    [patch, reload],
  )

  const rawList = state.status === 'success' ? state.data : []

  // Extract distinct providers for filter
  const distinctProviders = useMemo(() => {
    const set = new Set<string>()
    for (const sub of rawList) {
      if (sub.provider) set.add(sub.provider)
    }
    return Array.from(set).sort()
  }, [rawList])

  // Filter & Sort
  const filteredList = useMemo(() => {
    let result = [...rawList]

    if (search.trim()) {
      const q = search.trim().toLowerCase()
      result = result.filter(
        (s) =>
          s.artist_name.toLowerCase().includes(q) ||
          s.provider.toLowerCase().includes(q),
      )
    }

    if (statusFilter !== 'all') {
      result = result.filter((s) => s.last_sync_status === statusFilter)
    }

    if (enabledFilter === 'enabled') {
      result = result.filter((s) => s.enabled)
    } else if (enabledFilter === 'disabled') {
      result = result.filter((s) => !s.enabled)
    }

    if (autoDownloadFilter === 'on') {
      result = result.filter((s) => s.auto_download)
    } else if (autoDownloadFilter === 'off') {
      result = result.filter((s) => !s.auto_download)
    }

    if (providerFilter !== 'all') {
      result = result.filter((s) => s.provider === providerFilter)
    }

    result.sort((a, b) => {
      switch (sortBy) {
        case 'name_asc':
          return a.artist_name.localeCompare(b.artist_name)
        case 'name_desc':
          return b.artist_name.localeCompare(a.artist_name)
        case 'last_sync_desc': {
          const tA = a.last_sync_at ? new Date(a.last_sync_at).getTime() : 0
          const tB = b.last_sync_at ? new Date(b.last_sync_at).getTime() : 0
          return tB - tA
        }
        case 'last_sync_asc': {
          const tA = a.last_sync_at ? new Date(a.last_sync_at).getTime() : 0
          const tB = b.last_sync_at ? new Date(b.last_sync_at).getTime() : 0
          return tA - tB
        }
        case 'next_sync_asc':
          return new Date(a.next_sync_at).getTime() - new Date(b.next_sync_at).getTime()
        case 'created_desc':
          return new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
        case 'status':
          return a.last_sync_status.localeCompare(b.last_sync_status)
        default:
          return 0
      }
    })

    return result
  }, [
    rawList,
    search,
    statusFilter,
    enabledFilter,
    autoDownloadFilter,
    providerFilter,
    sortBy,
  ])

  // Multi-select handlers
  const allFilteredSelected =
    filteredList.length > 0 &&
    filteredList.every((sub) => selectedIds.has(sub.id))

  function toggleSelectAll() {
    if (allFilteredSelected) {
      setSelectedIds(new Set())
    } else {
      const next = new Set(selectedIds)
      for (const sub of filteredList) {
        next.add(sub.id)
      }
      setSelectedIds(next)
    }
  }

  function toggleSelect(id: string) {
    const next = new Set(selectedIds)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelectedIds(next)
  }

  // Bulk Actions
  async function handleBulkEnable(enable: boolean) {
    setBulkActionLoading(true)
    try {
      const promises = Array.from(selectedIds).map((id) =>
        updateSubscription(id, { enabled: enable }),
      )
      await Promise.all(promises)
      reload()
    } catch {
      reload()
    } finally {
      setBulkActionLoading(false)
    }
  }

  async function handleBulkSync() {
    setBulkActionLoading(true)
    try {
      const promises = Array.from(selectedIds).map((id) => syncSubscription(id))
      await Promise.all(promises)
      reload()
    } catch {
      reload()
    } finally {
      setBulkActionLoading(false)
    }
  }

  async function handleBulkExport() {
    const selectedSubs = rawList.filter((s) => selectedIds.has(s.id))
    await exportSubscriptionsToFile(selectedSubs)
  }

  async function handleBulkDelete() {
    setBulkActionLoading(true)
    try {
      const promises = Array.from(selectedIds).map((id) => unsubscribe(id))
      await Promise.all(promises)
      setSelectedIds(new Set())
      setBulkDeleting(false)
      reload()
    } catch {
      reload()
    } finally {
      setBulkActionLoading(false)
    }
  }

  const selectedCount = selectedIds.size

  return (
    <div className="space-y-6">
      {/* Header with Title and Import/Export Actions */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <header className="space-y-1">
          <h1 className="font-heading text-2xl font-semibold tracking-tight text-foreground sm:text-3xl">
            Abonnements
          </h1>
          <p className="text-sm text-muted-foreground">
            {state.status === 'success'
              ? `${pluralize(rawList.length, 'Künstler', 'Künstler')} ${
                  rawList.length === 1 ? 'wird' : 'werden'
                } auf neue Veröffentlichungen geprüft.`
              : 'Künstler, die auf neue Veröffentlichungen geprüft werden.'}
          </p>
        </header>

        {rawList.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setImportDialogOpen(true)}
              className="gap-1.5"
            >
              <UploadIcon className="size-4" />
              Importieren
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={() => void exportSubscriptionsToFile()}
              className="gap-1.5"
            >
              <DownloadIcon className="size-4" />
              Exportieren
            </Button>

            <Link
              href={paths.discover()}
              className={cn(buttonVariants({ variant: 'default', size: 'sm' }), 'gap-1.5')}
            >
              <PlusIcon className="size-4" />
              Künstler entdecken
            </Link>
          </div>
        )}
      </div>

      {/* Loading & Error States */}
      {state.status === 'loading' && (
        <LoadingRegion label="Abonnements werden geladen">
          <ListSkeleton rows={4} />
        </LoadingRegion>
      )}

      {state.status === 'error' && (
        <Panel>
          <ErrorState error={state.error} onRetry={reload} />
        </Panel>
      )}

      {state.status === 'success' && rawList.length === 0 && (
        <Panel>
          <EmptyState
            icon={<BellIcon />}
            title="Noch keine Abonnements"
            description="Öffne einen Künstler unter Entdecken und abonniere ihn, um seine Diskografie im Blick zu behalten."
            action={
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" onClick={() => setImportDialogOpen(true)}>
                  <UploadIcon className="size-4 mr-1.5" />
                  Importieren
                </Button>
                <Link
                  href={paths.discover()}
                  className={buttonVariants({ variant: 'default', size: 'sm' })}
                >
                  Künstler entdecken
                </Link>
              </div>
            }
          />
        </Panel>
      )}

      {state.status === 'success' && rawList.length > 0 && (
        <div className="space-y-4">
          {/* Filter Toolbar */}
          <div className="flex flex-col gap-3 rounded-2xl border border-border bg-card/60 p-3.5 sm:flex-row sm:items-center sm:justify-between">
            <div className="relative flex-1 min-w-[200px] max-w-sm">
              <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="Künstler suchen..."
                className="pl-9 pr-8 h-9 text-xs"
              />
              {search && (
                <button
                  type="button"
                  onClick={() => setSearch('')}
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  <XIcon className="size-3.5" />
                </button>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-2">
              {/* Status filter */}
              <select
                value={statusFilter}
                onChange={(e) => setStatusFilter(e.target.value as 'all' | SyncStatus)}
                className="h-9 rounded-xl border border-border bg-background px-2.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="all">Status: Alle</option>
                <option value="success">Status: Erfolgreich</option>
                <option value="partial">Status: Teilweise</option>
                <option value="failed">Status: Fehlgeschlagen</option>
                <option value="pending">Status: Noch nicht geprüft</option>
              </select>

              {/* Enabled / Paused filter */}
              <select
                value={enabledFilter}
                onChange={(e) =>
                  setEnabledFilter(e.target.value as 'all' | 'enabled' | 'disabled')
                }
                className="h-9 rounded-xl border border-border bg-background px-2.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="all">Zustand: Alle</option>
                <option value="enabled">Zustand: Aktiv</option>
                <option value="disabled">Zustand: Pausiert</option>
              </select>

              {/* Auto Download filter */}
              <select
                value={autoDownloadFilter}
                onChange={(e) =>
                  setAutoDownloadFilter(e.target.value as 'all' | 'on' | 'off')
                }
                className="h-9 rounded-xl border border-border bg-background px-2.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
              >
                <option value="all">Auto-Download: Alle</option>
                <option value="on">Auto-Download: Aktiviert</option>
                <option value="off">Auto-Download: Deaktiviert</option>
              </select>

              {/* Provider filter */}
              {distinctProviders.length > 1 && (
                <select
                  value={providerFilter}
                  onChange={(e) => setProviderFilter(e.target.value)}
                  className="h-9 rounded-xl border border-border bg-background px-2.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                >
                  <option value="all">Provider: Alle</option>
                  {distinctProviders.map((p) => (
                    <option key={p} value={p}>
                      Provider: {p}
                    </option>
                  ))}
                </select>
              )}

              {/* Sort by */}
              <select
                value={sortBy}
                onChange={(e) => setSortBy(e.target.value as SortOption)}
                className="h-9 rounded-xl border border-border bg-background px-2.5 text-xs text-foreground focus:outline-none focus:ring-1 focus:ring-primary font-medium"
              >
                <option value="name_asc">Name (A–Z)</option>
                <option value="name_desc">Name (Z–A)</option>
                <option value="last_sync_desc">Zuletzt geprüft (Neueste)</option>
                <option value="last_sync_asc">Zuletzt geprüft (Älteste)</option>
                <option value="next_sync_asc">Nächste Prüfung</option>
                <option value="created_desc">Erstellt (Neueste)</option>
                <option value="status">Status</option>
              </select>
            </div>
          </div>

          {/* Bulk Selection Toolbar */}
          {selectedCount > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-primary/30 bg-primary/10 px-4 py-2.5 text-xs animate-in fade-in duration-150">
              <div className="flex items-center gap-2.5">
                <span className="font-semibold text-primary">
                  {selectedCount} von {filteredList.length} Abonnements ausgewählt
                </span>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setSelectedIds(new Set())}
                  className="h-7 text-xs px-2 text-muted-foreground hover:text-foreground"
                >
                  Auswahl aufheben
                </Button>
              </div>

              <div className="flex flex-wrap items-center gap-1.5">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={bulkActionLoading}
                  onClick={() => void handleBulkEnable(true)}
                  className="h-7 px-2.5 text-xs gap-1"
                >
                  <PlayIcon className="size-3 text-emerald-400" />
                  Aktivieren
                </Button>

                <Button
                  variant="outline"
                  size="sm"
                  disabled={bulkActionLoading}
                  onClick={() => void handleBulkEnable(false)}
                  className="h-7 px-2.5 text-xs gap-1"
                >
                  Pausieren
                </Button>

                <Button
                  variant="outline"
                  size="sm"
                  disabled={bulkActionLoading}
                  onClick={() => void handleBulkSync()}
                  className="h-7 px-2.5 text-xs gap-1"
                >
                  <RefreshCwIcon className="size-3" />
                  Jetzt prüfen
                </Button>

                <Button
                  variant="outline"
                  size="sm"
                  disabled={bulkActionLoading}
                  onClick={() => void handleBulkExport()}
                  className="h-7 px-2.5 text-xs gap-1"
                >
                  <DownloadIcon className="size-3" />
                  Exportieren
                </Button>

                <Button
                  variant="destructive"
                  size="sm"
                  disabled={bulkActionLoading}
                  onClick={() => setBulkDeleting(true)}
                  className="h-7 px-2.5 text-xs gap-1"
                >
                  <Trash2Icon className="size-3" />
                  Löschen
                </Button>
              </div>
            </div>
          )}

          {/* Subscriptions Table / List */}
          {filteredList.length === 0 ? (
            <Panel className="p-8 text-center text-xs text-muted-foreground">
              Keine Abonnements für die aktuellen Filterkriterien gefunden.
            </Panel>
          ) : (
            <div className="overflow-x-auto rounded-2xl border border-border bg-card">
              <table className="w-full text-left text-xs">
                <thead>
                  <tr className="border-b border-border bg-white/3 font-medium text-muted-foreground">
                    <th className="w-10 py-3 pl-4 pr-1">
                      <button
                        type="button"
                        onClick={toggleSelectAll}
                        aria-label="Alle auswählen"
                        className={cn(
                          'flex size-4 items-center justify-center rounded border transition-colors',
                          allFilteredSelected
                            ? 'border-primary bg-primary text-primary-foreground'
                            : 'border-muted-foreground/40 hover:border-primary',
                        )}
                      >
                        {allFilteredSelected && <CheckIcon className="size-3 stroke-[3]" />}
                      </button>
                    </th>
                    <th className="py-3 px-3">Künstler</th>
                    <th className="py-3 px-3">Provider</th>
                    <th className="py-3 px-3">Status</th>
                    <th className="py-3 px-3">Downloads</th>
                    <th className="py-3 px-3">Filter</th>
                    <th className="py-3 px-3">Zuletzt geprüft</th>
                    <th className="py-3 px-3">Nächste Prüfung</th>
                    <th className="py-3 pl-3 pr-4 text-right">Aktionen</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {filteredList.map((sub) => {
                    const isSelected = selectedIds.has(sub.id)
                    return (
                      <SubscriptionTableRow
                        key={sub.id}
                        subscription={sub}
                        selected={isSelected}
                        failure={failures[sub.id]}
                        onToggleSelect={() => toggleSelect(sub.id)}
                        onSync={() =>
                          act(sub.id, (signal) => syncSubscription(sub.id, signal))
                        }
                        onUpdate={(update) =>
                          act(sub.id, (signal) =>
                            updateSubscription(sub.id, update, signal),
                          )
                        }
                        onToggleEnabled={() =>
                          act(sub.id, (signal) =>
                            updateSubscription(
                              sub.id,
                              { enabled: !sub.enabled },
                              signal,
                            ),
                          )
                        }
                        onDelete={() =>
                          act(sub.id, async (signal) => {
                            await unsubscribe(sub.id, signal)
                          })
                        }
                      />
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {/* Import Modal */}
      <SubscriptionImportDialog
        open={importDialogOpen}
        onOpenChange={setImportDialogOpen}
        onImportSuccess={reload}
      />

      {/* Bulk Delete Confirmation Dialog */}
      <Dialog open={bulkDeleting} onOpenChange={setBulkDeleting}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Abonnements entfernen</DialogTitle>
            <DialogDescription>
              Möchtest du wirklich {selectedCount} ausgewählte Abonnements entfernen? Bereits
              heruntergeladene Titel verbleiben in der Bibliothek.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => setBulkDeleting(false)}
              disabled={bulkActionLoading}
            >
              Abbrechen
            </Button>
            <Button
              variant="destructive"
              onClick={() => void handleBulkDelete()}
              disabled={bulkActionLoading}
            >
              {bulkActionLoading ? 'Wird gelöscht...' : `${selectedCount} Abonnements löschen`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

interface SubscriptionTableRowProps {
  subscription: Subscription
  selected: boolean
  failure?: string
  onToggleSelect: () => void
  onSync: () => void
  onUpdate: (update: {
    auto_download?: boolean
    download_priority?: JobPriority
    release_filter?: ReleaseFilter
  }) => void
  onToggleEnabled: () => void
  onDelete: () => void
}

function SubscriptionTableRow({
  subscription,
  selected,
  failure,
  onToggleSelect,
  onSync,
  onUpdate,
  onToggleEnabled,
  onDelete,
}: SubscriptionTableRowProps) {
  const [confirmingDelete, setConfirmingDelete] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)

  const [editAutoDownload, setEditAutoDownload] = useState(subscription.auto_download)
  const [editPriority, setEditPriority] = useState<JobPriority>(
    subscription.download_priority || 'low',
  )
  const [editFilter, setEditFilter] = useState<ReleaseFilter>(
    subscription.release_filter || {
      albums: true,
      singles: true,
      eps: true,
      live: true,
      compilations: true,
      remixes: true,
    },
  )

  function openSettings() {
    setEditAutoDownload(subscription.auto_download)
    setEditPriority(subscription.download_priority || 'low')
    setEditFilter(
      subscription.release_filter || {
        albums: true,
        singles: true,
        eps: true,
        live: true,
        compilations: true,
        remixes: true,
      },
    )
    setSettingsOpen(true)
  }

  function saveSettings() {
    onUpdate({
      auto_download: editAutoDownload,
      download_priority: editPriority,
      release_filter: editFilter,
    })
    setSettingsOpen(false)
  }

  // Format filter badges
  const filterList = useMemo(() => {
    const f = subscription.release_filter
    if (!f) return 'Standard'
    const active: string[] = []
    if (f.albums) active.push('Alben')
    if (f.singles) active.push('Singles')
    if (f.eps) active.push('EPs')
    if (f.live) active.push('Live')
    if (f.compilations) active.push('Comp.')
    if (f.remixes) active.push('Remix')
    return active.length > 0 ? active.join(', ') : 'Keine'
  }, [subscription.release_filter])

  return (
    <>
      <tr
        className={cn(
          'transition-colors hover:bg-white/3',
          selected && 'bg-primary/5 hover:bg-primary/8',
          !subscription.enabled && 'opacity-75',
        )}
      >
        <td className="py-3 pl-4 pr-1">
          <button
            type="button"
            onClick={onToggleSelect}
            aria-label={`${subscription.artist_name} auswählen`}
            className={cn(
              'flex size-4 items-center justify-center rounded border transition-colors',
              selected
                ? 'border-primary bg-primary text-primary-foreground'
                : 'border-muted-foreground/40 hover:border-primary',
            )}
          >
            {selected && <CheckIcon className="size-3 stroke-[3]" />}
          </button>
        </td>

        {/* Artist Name & Cover */}
        <td className="py-3 px-3">
          <div className="flex items-center gap-2.5 min-w-[180px] max-w-[320px]">
            <Cover
              src={subscription.artist_image_url}
              alt=""
              shape="circle"
              className="size-8 shrink-0"
            />
            <div className="min-w-0 flex-1">
              <Link
                href={paths.artist(subscription.artist_source_id, subscription.provider)}
                className="font-medium text-foreground truncate block hover:underline"
                title={subscription.artist_name}
              >
                {subscription.artist_name}
              </Link>
              {subscription.last_error && (
                <p className="flex items-start gap-1 text-[11px] text-destructive truncate max-w-xs" title={subscription.last_error}>
                  <AlertCircleIcon className="mt-px size-3 shrink-0" />
                  <span>{subscription.last_error}</span>
                </p>
              )}
              {failure && (
                <p role="alert" className="flex items-start gap-1 text-[11px] text-destructive truncate max-w-xs" title={failure}>
                  <AlertCircleIcon className="mt-px size-3 shrink-0" />
                  <span>{failure}</span>
                </p>
              )}
            </div>
          </div>
        </td>

        {/* Provider */}
        <td className="py-3 px-3 whitespace-nowrap">
          <Badge variant="outline" className="text-[10px] uppercase font-mono">
            {subscription.provider}
          </Badge>
        </td>

        {/* Status */}
        <td className="py-3 px-3 whitespace-nowrap">
          <div className="flex items-center gap-1.5">
            {subscription.syncing ? (
              <Badge variant="default" className="text-[10px] gap-1">
                <Loader2Icon className="size-3 animate-spin" />
                Wird geprüft
              </Badge>
            ) : (
              <Badge
                variant={syncStatusTone(subscription.last_sync_status)}
                className="text-[10px]"
              >
                {SYNC_STATUS_LABELS[subscription.last_sync_status]}
              </Badge>
            )}
            {!subscription.enabled && (
              <Badge variant="neutral" className="text-[10px]">
                Pausiert
              </Badge>
            )}
            {subscription.auto_download && (
              <Badge variant="outline" className="text-[10px]">
                Auto-Download
              </Badge>
            )}
          </div>
        </td>

        {/* Auto Download Checkbox */}
        <td className="py-3 px-3 whitespace-nowrap">
          <label className="flex cursor-pointer items-center gap-2 text-xs font-medium text-foreground">
            <Checkbox
              checked={subscription.auto_download}
              onCheckedChange={(checked) =>
                onUpdate({ auto_download: checked === true })
              }
            />
            <span className="text-xs text-muted-foreground hidden sm:inline">
              {subscription.auto_download ? 'Aktiv' : 'Aus'}
            </span>
          </label>
        </td>

        {/* Release Filter Summary */}
        <td className="py-3 px-3 whitespace-nowrap text-muted-foreground text-[11px] max-w-[180px] truncate" title={filterList}>
          {filterList}
        </td>

        {/* Last Sync */}
        <td className="py-3 px-3 whitespace-nowrap text-muted-foreground" title={formatDateTime(subscription.last_sync_at)}>
          {subscription.last_sync_at ? formatRelative(subscription.last_sync_at) : 'nie'}
        </td>

        {/* Next Sync */}
        <td className="py-3 px-3 whitespace-nowrap text-muted-foreground" title={formatDateTime(subscription.next_sync_at)}>
          {subscription.enabled ? formatRelative(subscription.next_sync_at) : 'pausiert'}
        </td>

        {/* Actions */}
        <td className="py-3 pl-3 pr-4 text-right whitespace-nowrap">
          <div className="inline-flex items-center gap-1">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={subscription.syncing}
              onClick={onSync}
              aria-label="Jetzt prüfen"
              title="Jetzt prüfen"
              className="h-7 px-2 text-xs"
            >
              {subscription.syncing ? (
                <Loader2Icon className="size-3.5 animate-spin" />
              ) : (
                <RefreshCwIcon className="size-3.5" />
              )}
              <span className="hidden md:inline ml-1">Jetzt prüfen</span>
            </Button>

            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={openSettings}
              title="Filter & Priorität"
              className="h-7 px-2 text-xs"
            >
              <Settings2Icon className="size-3.5" />
            </Button>

            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onToggleEnabled}
              title={subscription.enabled ? 'Pausieren' : 'Fortsetzen'}
              className="h-7 px-2 text-xs"
            >
              {subscription.enabled ? 'Pausieren' : 'Fortsetzen'}
            </Button>

            {confirmingDelete ? (
              <div className="inline-flex items-center gap-1">
                <Button
                  type="button"
                  variant="destructive"
                  size="sm"
                  onClick={onDelete}
                  aria-label={`${subscription.artist_name} wirklich entfernen`}
                  className="h-7 px-2 text-[10px]"
                >
                  Wirklich entfernen
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  onClick={() => setConfirmingDelete(false)}
                  className="h-7 px-1.5 text-[10px]"
                >
                  X
                </Button>
              </div>
            ) : (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                onClick={() => setConfirmingDelete(true)}
                aria-label={`${subscription.artist_name} entfernen`}
                title="Abonnement entfernen"
                className="h-7 px-2 text-xs text-muted-foreground hover:text-destructive hover:bg-destructive/10"
              >
                <Trash2Icon className="size-3.5" />
              </Button>
            )}
          </div>
        </td>
      </tr>

      {/* Settings Dialog */}
      {settingsOpen && (
        <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Abonnement-Einstellungen</DialogTitle>
              <DialogDescription>
                Legen Sie fest, welche Veröffentlichungen von {subscription.artist_name}{' '}
                automatisch heruntergeladen werden sollen.
              </DialogDescription>
            </DialogHeader>

            <div className="space-y-5 py-2 text-sm text-foreground">
              <div className="space-y-2">
                <label className="flex cursor-pointer items-center gap-2 text-sm text-foreground font-medium">
                  <Checkbox
                    checked={editAutoDownload}
                    onCheckedChange={(checked) => setEditAutoDownload(checked === true)}
                  />
                  Automatischer Download neuer Tracks
                </label>
              </div>

              <div className="space-y-2">
                <label htmlFor="sub-priority" className="text-xs font-medium text-foreground">
                  Download-Priorität:
                </label>
                <select
                  id="sub-priority"
                  value={editPriority}
                  onChange={(e) => setEditPriority(e.target.value as JobPriority)}
                  className="w-full rounded-lg border border-border bg-background px-3 py-2 text-sm text-foreground focus:outline-none focus:ring-1 focus:ring-primary"
                >
                  <option value="low">Niedrig (Standard für Abos)</option>
                  <option value="normal">Normal</option>
                  <option value="high">Hoch</option>
                </select>
              </div>

              <div className="space-y-2">
                <span className="text-xs font-medium text-foreground">
                  Erlaubte Release-Typen:
                </span>
                <div className="grid grid-cols-2 gap-3 pt-1">
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.albums}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({ ...prev, albums: checked === true }))
                      }
                    />
                    Alben
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.singles}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({ ...prev, singles: checked === true }))
                      }
                    />
                    Singles
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.eps}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({ ...prev, eps: checked === true }))
                      }
                    />
                    EPs
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.live}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({ ...prev, live: checked === true }))
                      }
                    />
                    Live-Aufnahmen
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.compilations}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({
                          ...prev,
                          compilations: checked === true,
                        }))
                      }
                    />
                    Compilations
                  </label>
                  <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
                    <Checkbox
                      checked={editFilter.remixes}
                      onCheckedChange={(checked) =>
                        setEditFilter((prev) => ({ ...prev, remixes: checked === true }))
                      }
                    />
                    Remixe
                  </label>
                </div>
              </div>
            </div>

            <DialogFooter>
              <Button variant="ghost" onClick={() => setSettingsOpen(false)}>
                Abbrechen
              </Button>
              <Button onClick={saveSettings}>Speichern</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </>
  )
}
