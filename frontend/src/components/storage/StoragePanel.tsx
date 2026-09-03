import { useState } from 'react'
import {
  HardDriveIcon,
  FolderSyncIcon,
  ShieldCheckIcon,
  ShieldAlertIcon,
  RefreshCwIcon,
  PauseIcon,
  PlayIcon,
  NetworkIcon,
  AlertTriangleIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Panel } from '@/components/ui/panel'
import { Progress } from '@/components/ui/progress'
import {
  getStorageStatus,
  probeStorage,
  pauseStorageQueue,
  resumeStorageQueue,
} from '@/lib/api/storage'
import { formatBytes, formatRelative } from '@/lib/utils/format'
import type { StorageStatusResponse, StorageHealthStatus } from '@/types/api'

interface StoragePanelProps {
  initialData?: StorageStatusResponse
  onReload?: () => void
}

const STORAGE_STATUS_LABELS: Record<StorageHealthStatus, string> = {
  healthy: 'Bereit & Beschreibbar',
  degraded: 'Eingeschränkt',
  unavailable: 'Nicht erreichbar',
  guard_missing: 'Identity Marker fehlt (Nicht gemountet?)',
  guard_mismatch: 'Guard ID Mismatch',
  read_only: 'Schreibgeschützt',
  low_space: 'Wenig freier Speicher',
  unknown: 'Unbekannter Status',
}

const STORAGE_STATUS_TONE: Record<StorageHealthStatus, 'success' | 'destructive' | 'warning' | 'neutral'> = {
  healthy: 'success',
  degraded: 'warning',
  unavailable: 'destructive',
  guard_missing: 'destructive',
  guard_mismatch: 'destructive',
  read_only: 'warning',
  low_space: 'warning',
  unknown: 'neutral',
}

export function StoragePanel({ initialData, onReload }: StoragePanelProps) {
  const [data, setData] = useState<StorageStatusResponse | undefined>(initialData)
  const [loading, setLoading] = useState(false)
  const [actionLoading, setActionLoading] = useState(false)

  const handleProbe = async () => {
    try {
      setLoading(true)
      const updated = await probeStorage()
      setData(updated)
      onReload?.()
    } finally {
      setLoading(false)
    }
  }

  const handleToggleQueue = async () => {
    if (!data) return
    try {
      setActionLoading(true)
      if (data.queue.paused) {
        await resumeStorageQueue()
      } else {
        await pauseStorageQueue()
      }
      const refreshed = await getStorageStatus()
      setData(refreshed)
      onReload?.()
    } finally {
      setActionLoading(false)
    }
  }

  if (!data) {
    return (
      <Panel className="p-5 text-sm text-muted-foreground">
        Lade Speicherstatus...
      </Panel>
    )
  }

  const { library, staging, queue } = data
  const libUsedPercent = library.total_bytes > 0
    ? Math.round(((library.total_bytes - library.free_bytes) / library.total_bytes) * 100)
    : 0

  return (
    <div className="space-y-4">
      <div className="grid gap-4 md:grid-cols-2">
        {/* Library Storage Card */}
        <Panel className="space-y-4 p-5">
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-xl bg-accent text-accent-foreground">
                <HardDriveIcon className="size-5" />
              </div>
              <div>
                <h3 className="font-heading text-sm font-semibold text-foreground">
                  Musik-Bibliothek
                </h3>
                <p className="font-mono text-xs text-muted-foreground">{library.path}</p>
              </div>
            </div>
            <Badge variant={STORAGE_STATUS_TONE[library.status] || 'neutral'}>
              {STORAGE_STATUS_LABELS[library.status] || library.status}
            </Badge>
          </div>

          {library.status_detail && (
            <div className="flex items-start gap-2 rounded-xl border border-destructive/20 bg-destructive/8 p-3 text-xs text-destructive">
              <AlertTriangleIcon className="mt-0.5 size-4 shrink-0" />
              <span>{library.status_detail}</span>
            </div>
          )}

          <div className="space-y-2">
            <div className="flex justify-between text-xs text-muted-foreground">
              <span>Belegung ({libUsedPercent} %)</span>
              <span>
                {formatBytes(library.used_bytes)} / {formatBytes(library.total_bytes)}
              </span>
            </div>
            <Progress value={libUsedPercent} />
            <div className="flex justify-between text-[11px] text-muted-foreground">
              <span>Frei: {formatBytes(library.free_bytes)} ({library.free_percent.toFixed(1)} %)</span>
              <span>Reserve-Limit: {formatBytes(library.min_free_bytes)}</span>
            </div>
          </div>

          <div className="border-t border-border pt-3 text-xs space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Dateisystem:</span>
              <span className="font-mono inline-flex items-center gap-1.5 text-foreground">
                {library.is_network_fs && <NetworkIcon className="size-3.5 text-primary" />}
                {library.fs_type || 'unbekannt'}
              </span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Storage Guard:</span>
              {library.guard_status === 'verified' && (
                <span className="inline-flex items-center gap-1 text-success font-medium">
                  <ShieldCheckIcon className="size-3.5" />
                  <span>Verifiziert</span>
                </span>
              )}
              {library.guard_status === 'missing' && (
                <span className="inline-flex items-center gap-1 text-destructive font-medium">
                  <ShieldAlertIcon className="size-3.5" />
                  <span>Marker fehlt</span>
                </span>
              )}
              {library.guard_status === 'mismatch' && (
                <span className="inline-flex items-center gap-1 text-destructive font-medium">
                  <ShieldAlertIcon className="size-3.5" />
                  <span>Guard Mismatch</span>
                </span>
              )}
              {library.guard_status === 'invalid' && (
                <span className="inline-flex items-center gap-1 text-destructive font-medium">
                  <ShieldAlertIcon className="size-3.5" />
                  <span>Ungültiger Marker</span>
                </span>
              )}
              {(library.guard_status === 'disabled' || !library.guard_configured) && (
                <span className="inline-flex items-center gap-1 text-muted-foreground">
                  <ShieldAlertIcon className="size-3.5" />
                  <span>Deaktiviert</span>
                </span>
              )}
            </div>
            {library.last_checked_at && (
              <div className="flex items-center justify-between text-[11px] text-muted-foreground">
                <span>Zuletzt geprüft:</span>
                <span>{formatRelative(library.last_checked_at)}</span>
              </div>
            )}
          </div>
        </Panel>

        {/* Local Persistent Staging Card */}
        <Panel className="space-y-4 p-5">
          <div className="flex items-start justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <div className="flex size-9 items-center justify-center rounded-xl bg-accent text-accent-foreground">
                <FolderSyncIcon className="size-5" />
              </div>
              <div>
                <h3 className="font-heading text-sm font-semibold text-foreground">
                  Persistentes Staging
                </h3>
                <p className="font-mono text-xs text-muted-foreground">{staging.path}</p>
              </div>
            </div>
            <Badge variant="neutral">Lokal (/data)</Badge>
          </div>

          <div className="space-y-2">
            <div className="flex justify-between text-xs text-muted-foreground">
              <span>Laufwerks-Kapazität</span>
              <span>
                {formatBytes(staging.used_bytes)} / {formatBytes(staging.total_bytes)}
              </span>
            </div>
            <Progress
              value={
                staging.total_bytes > 0
                  ? Math.round((staging.used_bytes / staging.total_bytes) * 100)
                  : 0
              }
            />
            <div className="flex justify-between text-[11px] text-muted-foreground">
              <span>Frei: {formatBytes(staging.free_bytes)}</span>
              <span>Reserve: {formatBytes(staging.min_free_bytes)}</span>
            </div>
          </div>

          <div className="border-t border-border pt-3 text-xs space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Aktuell belegtes Staging:</span>
              <span className="font-medium text-foreground">{formatBytes(staging.current_staged_bytes)}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-muted-foreground">Aktive Teildownloads (.part):</span>
              <span className="font-medium text-foreground">{staging.active_partials}</span>
            </div>
            {staging.max_bytes > 0 && (
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Staging-Kontingent:</span>
                <span className="text-foreground">{formatBytes(staging.max_bytes)}</span>
              </div>
            )}
          </div>
        </Panel>
      </div>

      {/* Queue & Probing Toolbar */}
      <Panel className="flex flex-wrap items-center justify-between gap-3 p-4">
        <div className="flex items-center gap-3">
          <div className="text-xs">
            <span className="text-muted-foreground">Download-Warteschlange: </span>
            <span className={queue.paused ? 'font-semibold text-warning' : 'font-semibold text-success'}>
              {queue.paused ? 'Pausiert' : 'Aktiv'}
            </span>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleProbe}
            disabled={loading}
            className="gap-1.5"
          >
            <RefreshCwIcon className={`size-3.5 ${loading ? 'animate-spin' : ''}`} />
            <span>Jetzt prüfen</span>
          </Button>

          <Button
            type="button"
            variant={queue.paused ? 'default' : 'outline'}
            size="sm"
            onClick={handleToggleQueue}
            disabled={actionLoading}
            className="gap-1.5"
          >
            {queue.paused ? (
              <>
                <PlayIcon className="size-3.5" />
                <span>Warteschlange fortsetzen</span>
              </>
            ) : (
              <>
                <PauseIcon className="size-3.5" />
                <span>Warteschlange anhalten</span>
              </>
            )}
          </Button>
        </div>
      </Panel>
    </div>
  )
}
