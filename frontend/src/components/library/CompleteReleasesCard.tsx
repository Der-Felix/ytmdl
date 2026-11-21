import { useEffect, useState } from 'react'
import { ArrowRightIcon, CircleCheckIcon, CopyPlusIcon, TriangleAlertIcon } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { ErrorState, ListSkeleton, LoadingRegion } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { ApiError, isAbortError } from '@/lib/api/client'
import {
  applyCompleteReleases,
  cancelCompleteReleasesScan,
  getCompleteReleases,
  startCompleteReleasesScan,
} from '@/lib/api/library'
import { notifyError, notifyInfo, notifySuccess } from '@/lib/notify'
import { explainProblem } from '@/lib/problems'
import { Link, paths } from '@/lib/router'
import { formatBytes, formatDateTime, formatNumber, pluralize } from '@/lib/utils/format'
import type {
  CompleteReleasesApply,
  CompleteReleasesResult,
  CompleteReleasesStatus,
} from '@/types/api'

/** How often a running scan is asked again. */
const REFRESH_MS = 2000

/**
 * What the card shows. A scanned result splits into the cases a person acts on
 * differently: something to fill, nothing to fill, too old to apply, applied.
 */
type View = 'idle' | 'scanning' | 'ready' | 'complete' | 'expired' | 'applied' | 'failed' | 'cancelled'

const VIEW_BADGES: Record<View, { label: string; variant: 'default' | 'neutral' | 'success' | 'warning' | 'destructive' | 'outline' }> = {
  idle: { label: 'Nicht geprüft', variant: 'outline' },
  scanning: { label: 'Prüft', variant: 'default' },
  ready: { label: 'Lücken gefunden', variant: 'warning' },
  complete: { label: 'Vollständig', variant: 'success' },
  expired: { label: 'Veraltet', variant: 'neutral' },
  applied: { label: 'Aufgefüllt', variant: 'success' },
  failed: { label: 'Fehlgeschlagen', variant: 'destructive' },
  cancelled: { label: 'Abgebrochen', variant: 'outline' },
}

/** Why a release could not be looked up, in a few words. */
const FAILED_REASONS: Record<string, string> = {
  PROVIDER_NOT_FOUND: 'Provider nicht eingerichtet',
  RELEASE_NOT_FOUND: 'beim Provider nicht gefunden',
  PROVIDER_UNAVAILABLE: 'Provider nicht erreichbar',
  PROVIDER_RATE_LIMITED: 'Provider drosselt Anfragen',
}

function viewOf(status: CompleteReleasesStatus, stale: boolean, now: number): View {
  switch (status.state) {
    case 'scanning':
      return 'scanning'
    case 'failed':
      return 'failed'
    case 'cancelled':
      return 'cancelled'
    case 'scanned': {
      if (status.applied_at) return 'applied'
      const result = status.result
      if (!result || result.copies === 0 || result.releases_incomplete === 0) return 'complete'
      const expires = status.expires_at ? Date.parse(status.expires_at) : Number.NaN
      if (stale || Number.isNaN(expires) || expires <= now) return 'expired'
      return 'ready'
    }
    default:
      return 'idle'
  }
}

/** A German sentence for a failed action, never the server's English message. */
function describe(error: unknown): string | undefined {
  if (error instanceof ApiError) return explainProblem(error.code, error.status).title
  return undefined
}

function notEnoughSpace(result: CompleteReleasesResult): boolean {
  return result.free_bytes !== undefined && result.bytes > result.free_bytes
}

/** "2 × Provider nicht eingerichtet, 1 × beim Provider nicht gefunden" */
function failedReasons(byCode: Record<string, number>): string {
  return Object.entries(byCode)
    .sort(([, a], [, b]) => b - a)
    .map(([code, count]) => `${formatNumber(count)} × ${FAILED_REASONS[code] ?? explainProblem(code).title}`)
    .join(', ')
}

/** The lines that keep the figures explainable: what was left alone and why. */
function explanations(result: CompleteReleasesResult): string[] {
  const lines = [`${pluralize(result.releases_examined, 'Release')} geprüft.`]
  if (result.held_tracks > 0) {
    lines.push(`${pluralize(result.held_tracks, 'Song')} ${result.held_tracks === 1 ? 'liegt' : 'liegen'} schon im richtigen Release.`)
  }
  if (result.unknown_tracks > 0) {
    lines.push(
      `${pluralize(result.unknown_tracks, 'Song')} ${result.unknown_tracks === 1 ? 'fehlt' : 'fehlen'} der Bibliothek ganz und ${result.unknown_tracks === 1 ? 'wird' : 'werden'} hierbei nicht geladen.`,
    )
  }
  if (result.releases_failed > 0) {
    const reasons = failedReasons(result.failed_by_code)
    lines.push(
      `${pluralize(result.releases_failed, 'Release')} ${result.releases_failed === 1 ? 'konnte' : 'konnten'} nicht geprüft werden${reasons ? `: ${reasons}` : ''}.`,
    )
  }
  if (result.releases_without_source > 0) {
    lines.push(
      `${pluralize(result.releases_without_source, 'Release')} ohne Provider-Kennung ${result.releases_without_source === 1 ? 'wurde' : 'wurden'} ausgelassen.`,
    )
  }
  return lines
}

interface CompleteReleasesCardProps {
  isAdmin: boolean
  refreshMs?: number
}

/**
 * Completing releases with copies (GET /library/complete-releases): a scan
 * finds releases whose tracks the library already holds in another release,
 * and applying it queues one copies-only job per release. The status is
 * fetched once and asked again only while a scan runs, like the naming
 * migration card next to it.
 */
export function CompleteReleasesCard({ isAdmin, refreshMs = REFRESH_MS }: CompleteReleasesCardProps) {
  // When the status was last read; a result past expires_at counts as stale.
  // An apply the server refuses as stale is caught as well (see `stale`).
  const [readAt, setReadAt] = useState(() => Date.now())
  const { state, reload, setData } = useAsync(
    (signal) =>
      getCompleteReleases({ signal }).then((status) => {
        setReadAt(Date.now())
        return status
      }),
    [],
    { keepDataOnReload: true },
  )
  const [busy, setBusy] = useState<'scan' | 'cancel' | 'apply' | null>(null)
  const [confirmOpen, setConfirmOpen] = useState(false)
  // What the last apply in this browser queued; the status only says when.
  const [applied, setApplied] = useState<CompleteReleasesApply | null>(null)
  // The server refused the apply as stale before the status caught up.
  const [stale, setStale] = useState(false)

  const scanning = state.status === 'success' && state.data?.state === 'scanning'
  useEffect(() => {
    if (!scanning) return
    const timer = window.setInterval(reload, refreshMs)
    return () => window.clearInterval(timer)
  }, [scanning, reload, refreshMs])

  async function handleScan() {
    if (busy) return
    setBusy('scan')
    try {
      const next = await startCompleteReleasesScan()
      setApplied(null)
      setStale(false)
      setData(next)
    } catch (error) {
      if (isAbortError(error)) return
      if (error instanceof ApiError && error.code === 'ALREADY_EXISTS') {
        notifyInfo('Eine Prüfung läuft bereits')
        reload()
      } else {
        notifyError('Prüfung konnte nicht starten', describe(error))
      }
    } finally {
      setBusy(null)
    }
  }

  async function handleCancel() {
    if (busy) return
    setBusy('cancel')
    try {
      setData(await cancelCompleteReleasesScan())
    } catch (error) {
      if (isAbortError(error)) return
      // INVALID_REQUEST: the scan finished in the meantime.
      if (!(error instanceof ApiError && error.code === 'INVALID_REQUEST')) {
        notifyError('Prüfung konnte nicht abgebrochen werden', describe(error))
      }
      reload()
    } finally {
      setBusy(null)
    }
  }

  async function handleApply() {
    if (busy) return
    setBusy('apply')
    try {
      const result = await applyCompleteReleases()
      setApplied(result)
      if (result.failed_jobs > 0) {
        notifyError(
          'Nicht alle Releases wurden eingereiht',
          `${pluralize(result.failed_jobs, 'Release')} fehlen noch. Auffüllen lässt sich erneut starten.`,
        )
      } else {
        notifySuccess(
          'Auffüllen gestartet',
          `${pluralize(result.queued_jobs, 'Auftrag', 'Aufträge')} unter Downloads eingereiht.`,
        )
      }
      setConfirmOpen(false)
      reload()
    } catch (error) {
      if (isAbortError(error)) return
      setConfirmOpen(false)
      if (error instanceof ApiError && error.code === 'STALE_REPAIR') {
        setStale(true)
        notifyError('Ergebnis ist nicht mehr gültig', 'Erneut prüfen, um den aktuellen Stand zu sehen.')
      } else {
        notifyError('Auffüllen fehlgeschlagen', describe(error))
      }
      reload()
    } finally {
      setBusy(null)
    }
  }

  if (state.status === 'loading') {
    return (
      <LoadingRegion label="Alben vervollständigen wird geladen">
        <ListSkeleton rows={1} />
      </LoadingRegion>
    )
  }
  if (state.status === 'error') {
    return <ErrorState error={state.error} onRetry={reload} className="mx-0 max-w-none px-0 py-0" />
  }

  const status = state.data
  if (status === null) return null

  const view = viewOf(status, stale, readAt)
  const badge = VIEW_BADGES[view]
  const result = status.result
  const tooBig = result ? notEnoughSpace(result) : false
  const timestamp =
    view === 'applied'
      ? `Aufgefüllt: ${formatDateTime(status.applied_at)}`
      : status.finished_at
        ? `Geprüft: ${formatDateTime(status.finished_at)}`
        : status.started_at
          ? `Gestartet: ${formatDateTime(status.started_at)}`
          : ''

  const rescanButton = isAdmin && (
    <Button
      variant={view === 'ready' ? 'ghost' : 'outline'}
      size="sm"
      className="max-sm:h-11"
      disabled={busy !== null}
      onClick={() => void handleScan()}
    >
      {busy === 'scan' ? 'Startet …' : view === 'idle' ? 'Prüfen' : 'Erneut prüfen'}
    </Button>
  )

  return (
    <section
      aria-labelledby="complete-releases-title"
      className="p-4 rounded-xl border border-border bg-card/30 space-y-3 text-xs"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <CopyPlusIcon className="size-4 text-primary" aria-hidden="true" />
          <h3 id="complete-releases-title" className="font-heading font-semibold text-foreground text-sm">
            Alben vervollständigen
          </h3>
          <Badge variant={badge.variant}>{badge.label}</Badge>
        </div>
        {timestamp && <div className="text-muted-foreground">{timestamp}</div>}
      </div>

      <p className="text-muted-foreground">
        Legt Songs, die schon in der Bibliothek liegen, als Kopie auch in die anderen Releases, auf denen sie
        erscheinen. So zeigen Medienserver vollständige Alben; heruntergeladen wird dabei nichts.
      </p>

      {!status.setting_enabled && (
        <p className="text-muted-foreground">
          „Alben vollständig halten“ ist ausgeschaltet: Auffüllen funktioniert hier trotzdem, neue Downloads halten
          Releases aber nicht vollständig.
        </p>
      )}

      {view === 'scanning' && (
        <div className="space-y-1.5">
          <div className="flex justify-between gap-3 text-muted-foreground text-[11px]">
            <span>Prüft Releases …</span>
            <span className="figures">
              {status.progress.total > 0
                ? `${formatNumber(status.progress.done)} / ${formatNumber(status.progress.total)}`
                : 'Wird ermittelt'}
            </span>
          </div>
          <Progress
            aria-label="Fortschritt der Prüfung"
            value={status.progress.total > 0 ? (status.progress.done / status.progress.total) * 100 : null}
            className="h-2"
          />
        </div>
      )}

      {view === 'failed' && (
        <div role="alert" className="space-y-0.5">
          <p className="font-medium text-destructive">
            Die Prüfung ist fehlgeschlagen: {explainProblem(status.error_code).title}.
          </p>
          <p className="text-muted-foreground">{explainProblem(status.error_code).nextStep}</p>
        </div>
      )}

      {view === 'cancelled' && <p className="text-foreground">Die Prüfung wurde abgebrochen.</p>}

      {view === 'expired' && (
        <p className="text-foreground">
          Das Ergebnis ist älter als 24 Stunden oder nicht mehr aktuell. Erneut prüfen, um den aktuellen Stand zu
          sehen.
        </p>
      )}

      {view === 'complete' && result && (
        <div className="space-y-1">
          <p className="flex items-center gap-1.5 font-medium text-foreground">
            <CircleCheckIcon className="size-4 shrink-0 text-success" aria-hidden="true" />
            Keine Kopien nötig: Alle Releases enthalten die Songs, die in der Bibliothek liegen.
          </p>
          <Explanations result={result} />
        </div>
      )}

      {view === 'applied' && (
        <div className="space-y-1">
          <p className="flex items-center gap-1.5 font-medium text-foreground">
            <CircleCheckIcon className="size-4 shrink-0 text-success" aria-hidden="true" />
            {applied
              ? `${pluralize(applied.queued_jobs, 'Auftrag', 'Aufträge')} unter Downloads eingereiht.`
              : 'Die Aufträge wurden unter Downloads eingereiht.'}
          </p>
          {applied && applied.already_queued > 0 && (
            <p className="text-muted-foreground">
              {pluralize(applied.already_queued, 'Release')} {applied.already_queued === 1 ? 'hatte' : 'hatten'} schon
              einen offenen Auftrag.
            </p>
          )}
        </div>
      )}

      {view === 'ready' && result && <ScanResult result={result} tooBig={tooBig} expiresAt={status.expires_at} />}

      {!isAdmin && view !== 'applied' && (
        <p className="text-muted-foreground">Prüfen und Auffüllen können nur Administratoren.</p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {view === 'scanning' ? (
          isAdmin && (
            <Button
              variant="outline"
              size="sm"
              className="max-sm:h-11"
              disabled={busy !== null}
              onClick={() => void handleCancel()}
            >
              {busy === 'cancel' ? 'Bricht ab …' : 'Abbrechen'}
            </Button>
          )
        ) : (
          <>
            {view === 'ready' && isAdmin && (
              <Button
                variant="default"
                size="sm"
                className="max-sm:h-11"
                disabled={busy !== null}
                onClick={() => setConfirmOpen(true)}
              >
                Auffüllen
              </Button>
            )}
            {view === 'applied' && (
              <Link
                href={paths.downloads()}
                className={buttonVariants({ variant: 'outline', size: 'sm', className: 'max-sm:h-11' })}
              >
                Zu den Downloads
                <ArrowRightIcon aria-hidden="true" />
              </Link>
            )}
            {rescanButton}
          </>
        )}
      </div>

      {result && (
        <Dialog
          open={confirmOpen}
          onOpenChange={(open) => {
            if (!open && busy !== 'apply') setConfirmOpen(false)
          }}
        >
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Releases auffüllen?</DialogTitle>
              <DialogDescription>
                Für {pluralize(result.releases_incomplete, 'Release')} werden{' '}
                {pluralize(result.copies, 'Datei', 'Dateien')} angelegt, zusammen etwa {formatBytes(result.bytes)}.
                Es sind Kopien von Songs, die schon in der Bibliothek liegen.
              </DialogDescription>
            </DialogHeader>
            <p className="text-xs leading-relaxed text-muted-foreground">
              Die Kopien erscheinen als Aufträge unter Downloads und lassen sich dort abbrechen.
            </p>
            {tooBig && result.free_bytes !== undefined && (
              <p className="flex items-start gap-2 text-xs leading-relaxed text-warning">
                <TriangleAlertIcon className="mt-0.5 size-4 shrink-0" aria-hidden="true" />
                Frei sind nur {formatBytes(result.free_bytes)}. Das reicht nicht für alle Kopien.
              </p>
            )}
            <DialogFooter>
              <Button variant="outline" disabled={busy === 'apply'} onClick={() => setConfirmOpen(false)}>
                Abbrechen
              </Button>
              <Button variant="default" disabled={busy === 'apply'} onClick={() => void handleApply()}>
                {busy === 'apply' ? 'Wird eingereiht …' : 'Auffüllen'}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </section>
  )
}

function Explanations({ result }: { result: CompleteReleasesResult }) {
  return (
    <ul className="space-y-0.5 text-muted-foreground">
      {explanations(result).map((line) => (
        <li key={line}>{line}</li>
      ))}
    </ul>
  )
}

function ScanResult({
  result,
  tooBig,
  expiresAt,
}: {
  result: CompleteReleasesResult
  tooBig: boolean
  expiresAt?: string
}) {
  const figures: Array<{ label: string; value: string; warn?: boolean }> = [
    { label: 'Unvollständige Releases', value: formatNumber(result.releases_incomplete) },
    { label: 'Kopien anzulegen', value: formatNumber(result.copies) },
    { label: 'Benötigter Platz', value: formatBytes(result.bytes) },
    {
      label: 'Freier Platz',
      value: result.free_bytes !== undefined ? formatBytes(result.free_bytes) : 'Unbekannt',
      warn: tooBig,
    },
  ]

  return (
    <div className="space-y-3">
      <dl className="grid grid-cols-2 sm:grid-cols-4 gap-3">
        {figures.map((figure) => (
          <div
            key={figure.label}
            className={
              figure.warn
                ? 'p-2.5 rounded-lg border border-warning/40 bg-warning/8'
                : 'p-2.5 rounded-lg bg-card/60 border border-border/60'
            }
          >
            <dt className="text-muted-foreground/80 text-[11px]">{figure.label}</dt>
            <dd className={`text-sm font-semibold figures mt-0.5 ${figure.warn ? 'text-warning' : 'text-foreground'}`}>
              {figure.value}
            </dd>
          </div>
        ))}
      </dl>

      {tooBig && result.free_bytes !== undefined && (
        <p
          role="alert"
          className="flex items-start gap-2 rounded-lg border border-warning/25 bg-warning/8 px-3 py-2 text-foreground"
        >
          <TriangleAlertIcon className="mt-0.5 size-4 shrink-0 text-warning" aria-hidden="true" />
          <span>
            Der freie Speicher reicht nicht: Die Kopien brauchen {formatBytes(result.bytes)}, frei sind{' '}
            {formatBytes(result.free_bytes)}. Schaffe vor dem Auffüllen Platz.
          </span>
        </p>
      )}

      <Explanations result={result} />

      <details className="group rounded-lg border border-border/60">
        <summary className="cursor-pointer rounded-lg px-3 py-2 font-semibold text-foreground focus-visible:outline-2 focus-visible:outline-ring max-sm:py-3">
          Betroffene Releases ({formatNumber(result.releases_incomplete)})
        </summary>
        <div className="border-t border-border/60">
          <p className="px-3 py-2 text-[11px] text-muted-foreground">
            „7 von 12“: so viele Songs liegen schon im Release. „+3“: so viele Kopien kommen dazu.
          </p>
          <ul className="max-h-64 overflow-y-auto divide-y divide-border border-t border-border/60">
            {result.releases.map((release) => (
              <li key={release.release_id} className="flex items-center justify-between gap-3 px-3 py-2">
                <div className="min-w-0">
                  <p className="font-medium text-foreground break-words">{release.title}</p>
                  <p className="text-muted-foreground break-words">{release.artist}</p>
                </div>
                <div className="flex shrink-0 items-center gap-2 figures">
                  <span className="text-muted-foreground">
                    {formatNumber(release.held)} von {formatNumber(release.total)}
                  </span>
                  <Badge variant="default">
                    <span aria-hidden="true">+{formatNumber(release.copies)}</span>
                    <span className="sr-only">{pluralize(release.copies, 'Kopie', 'Kopien')}</span>
                  </Badge>
                </div>
              </li>
            ))}
          </ul>
          {result.releases_truncated && (
            <p className="border-t border-border/60 px-3 py-2 text-muted-foreground">
              Angezeigt werden {formatNumber(result.releases.length)} von{' '}
              {formatNumber(result.releases_incomplete)} Releases. Auffüllen erfasst alle.
            </p>
          )}
        </div>
      </details>

      {expiresAt && (
        <p className="text-muted-foreground">Das Ergebnis lässt sich bis {formatDateTime(expiresAt)} anwenden.</p>
      )}
    </div>
  )
}
