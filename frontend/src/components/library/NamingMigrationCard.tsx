import { useEffect } from 'react'
import { FileSymlinkIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { ErrorState, ListSkeleton, LoadingRegion } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { getNamingMigration } from '@/lib/api/library'
import { formatDateTime, formatNumber } from '@/lib/utils/format'
import type { NamingMigrationState, NamingMigrationStatus } from '@/types/api'

/** How often a running migration is asked again. */
const REFRESH_MS = 4000

const STATE_LABELS: Record<NamingMigrationState, string> = {
  disabled: 'Ausgeschaltet',
  pending: 'Wartet auf den Start',
  running: 'Läuft',
  waiting_for_storage: 'Wartet auf den Speicher',
  incomplete: 'Unvollständig',
  completed: 'Abgeschlossen',
  failed: 'Fehlgeschlagen',
}

const STATE_VARIANTS: Record<NamingMigrationState, 'default' | 'neutral' | 'success' | 'warning' | 'destructive' | 'outline'> = {
  disabled: 'outline',
  pending: 'neutral',
  running: 'default',
  waiting_for_storage: 'warning',
  incomplete: 'warning',
  completed: 'success',
  failed: 'destructive',
}

const SKIP_REASONS: Record<string, string> = {
  target_exists: 'Am Ziel liegt schon eine Datei.',
  checksum_mismatch: 'Die Prüfsumme der kopierten Datei stimmt nicht.',
  source_missing: 'Die Datei fehlt am alten Ort.',
  move_failed: 'Das Verschieben ist fehlgeschlagen.',
  database_update_failed: 'Die Datenbank konnte nicht aktualisiert werden.',
  outside_library: 'Die Datei liegt außerhalb der Bibliothek.',
  partial_target: 'Am Ziel liegt eine unvollständige Datei.',
  folder_shared: 'Der Ordner wird von mehreren Veröffentlichungen genutzt.',
  extra_not_carried: 'Eine Zusatzdatei konnte nicht mitgenommen werden.',
}

/** The states in which the server is still working or about to; they are polled. */
function isRunning(state: NamingMigrationState): boolean {
  return state === 'pending' || state === 'running' || state === 'waiting_for_storage'
}

/** A finished run that had nothing to move needs no card. */
function isNothingToDo(status: NamingMigrationStatus): boolean {
  return (
    status.state === 'completed' &&
    status.planned === 0 &&
    status.moved === 0 &&
    status.skipped === 0
  )
}

/**
 * The status of the automatic move of existing files to the current naming
 * scheme (GET /library/naming-migration). It is fetched once and asked again
 * every few seconds only while the migration is still running. An older
 * server without the endpoint, and a library that needed no move, show at most
 * one quiet line.
 */
export function NamingMigrationCard({ refreshMs = REFRESH_MS }: { refreshMs?: number }) {
  const { state, reload } = useAsync((signal) => getNamingMigration({ signal }), [], {
    keepDataOnReload: true,
  })

  const running = state.status === 'success' && state.data !== null && isRunning(state.data.state)
  useEffect(() => {
    if (!running) return
    const timer = window.setInterval(reload, refreshMs)
    return () => window.clearInterval(timer)
  }, [running, reload, refreshMs])

  if (state.status === 'loading') {
    return (
      <LoadingRegion label="Dateinamen-Umstellung wird geladen">
        <ListSkeleton rows={1} />
      </LoadingRegion>
    )
  }
  if (state.status === 'error') {
    return <ErrorState error={state.error} onRetry={reload} className="mx-0 max-w-none px-0 py-0" />
  }

  const status = state.data
  if (status === null) return null
  if (isNothingToDo(status)) {
    return (
      <p className="text-xs text-muted-foreground">
        Dateinamen-Umstellung: Alle Dateien folgen bereits dem aktuellen Schema.
      </p>
    )
  }

  const skips = status.skips ?? []
  const figuresList: Array<[string, number]> = [
    ['Geplant', status.planned],
    ['Verschoben', status.moved],
    ['Übersprungen', status.skipped],
    ['Zusatzdateien mitgenommen', status.extras_carried],
    ['Leere Ordner entfernt', status.folders_removed],
  ]

  return (
    <section
      aria-labelledby="naming-migration-title"
      className="p-4 rounded-xl border border-border bg-card/30 space-y-3 text-xs"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <FileSymlinkIcon className="size-4 text-primary" aria-hidden="true" />
          <h3 id="naming-migration-title" className="font-heading font-semibold text-foreground text-sm">
            Dateinamen-Umstellung
          </h3>
          <Badge variant={STATE_VARIANTS[status.state] ?? 'neutral'}>
            {STATE_LABELS[status.state] ?? status.state}
          </Badge>
        </div>
        {(status.finished_at || status.started_at) && (
          <div className="text-muted-foreground">
            {status.finished_at
              ? `Beendet: ${formatDateTime(status.finished_at)}`
              : `Gestartet: ${formatDateTime(status.started_at)}`}
          </div>
        )}
      </div>

      <p className="text-muted-foreground">
        Bestehende Dateien werden automatisch auf das aktuelle Namensschema umgestellt.
      </p>

      {status.last_error && (
        <p role="alert" className="text-destructive break-words">
          {status.last_error}
        </p>
      )}

      <dl className="grid grid-cols-2 sm:grid-cols-5 gap-3">
        {figuresList.map(([label, value]) => (
          <div key={label} className="p-2.5 rounded-lg bg-card/60 border border-border/60">
            <dt className="text-muted-foreground/80 text-[11px]">{label}</dt>
            <dd className="text-sm font-semibold text-foreground figures mt-0.5">{formatNumber(value)}</dd>
          </div>
        ))}
      </dl>

      {skips.length > 0 && (
        <div className="space-y-2">
          <h4 className="font-semibold text-foreground">Übersprungene Dateien</h4>
          <ul className="max-h-64 overflow-y-auto divide-y divide-border rounded-lg border border-border/60">
            {skips.map((skip, index) => (
              <li key={`${skip.path}-${index}`} className="px-3 py-2 space-y-0.5">
                <p className="font-medium text-foreground">
                  {SKIP_REASONS[skip.reason] ?? 'Die Datei blieb unverändert.'}
                </p>
                <p className="font-mono text-[11px] text-muted-foreground break-all">{skip.path}</p>
                {skip.detail && <p className="text-[11px] text-muted-foreground break-words">{skip.detail}</p>}
              </li>
            ))}
          </ul>
          {status.skipped > skips.length && (
            <p className="text-muted-foreground">
              Angezeigt werden {formatNumber(skips.length)} von {formatNumber(status.skipped)} Dateien.
            </p>
          )}
        </div>
      )}
    </section>
  )
}
