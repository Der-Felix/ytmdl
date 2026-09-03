import { useCallback, useEffect, useState } from 'react'
import {
  AlertCircleIcon,
  ArchiveIcon,
  CheckCircle2Icon,
  FileSearchIcon,
  FolderSyncIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  TagIcon,
  Trash2Icon,
} from 'lucide-react'

import { RepairPreviewDialog } from '@/components/library/RepairPreviewDialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Pagination } from '@/components/ui/pagination'
import { Progress } from '@/components/ui/progress'
import {
  cancelLibraryAudit,
  getCurrentLibraryAudit,
  listLibraryAuditFindings,
  previewLibraryRepairs,
  startLibraryAudit,
} from '@/lib/api/library'
import { formatRelative } from '@/lib/utils/format'
import type {
  AuditFinding,
  AuditMode,
  AuditRun,
  EvidenceLevel,
  FindingSeverity,
  RepairAction,
  RepairPreview,
} from '@/types/api'

interface IntegrityPanelProps {
  isAdmin: boolean
}

export function IntegrityPanel({ isAdmin }: IntegrityPanelProps) {
  const [currentRun, setCurrentRun] = useState<AuditRun | null>(null)
  const [isStarting, setIsStarting] = useState(false)
  const [isCancelling, setIsCancelling] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)

  // Findings query state
  const [findings, setFindings] = useState<AuditFinding[]>([])
  const [findingsTotal, setFindingsTotal] = useState(0)
  const [findingsLoading, setFindingsLoading] = useState(false)
  const [currentPage, setCurrentPage] = useState(1)
  const [severityFilter, setSeverityFilter] = useState<FindingSeverity | 'all'>('all')
  const [codeFilter, setCodeFilter] = useState<string>('all')

  // Repair preview modal state
  const [selectedFinding, setSelectedFinding] = useState<AuditFinding | null>(null)
  const [selectedAction, setSelectedAction] = useState<RepairAction | null>(null)
  const [activePreview, setActivePreview] = useState<RepairPreview | null>(null)
  const [previewLoading, setPreviewLoading] = useState(false)
  const [previewDialogOpen, setPreviewDialogOpen] = useState(false)

  // Fetch current audit run
  const fetchCurrentRun = useCallback(async () => {
    try {
      const run = await getCurrentLibraryAudit()
      setCurrentRun(run)
    } catch (err: unknown) {
      setRunError(err instanceof Error ? err.message : 'Fehler beim Laden des Audit-Status.')
    }
  }, [])

  useEffect(() => {
    fetchCurrentRun()
  }, [fetchCurrentRun])

  // Polling loop when audit is running
  useEffect(() => {
    if (!currentRun || currentRun.status !== 'running') {
      return
    }

    const interval = setInterval(async () => {
      try {
        const updated = await getCurrentLibraryAudit()
        if (updated) {
          setCurrentRun(updated)
          if (updated.status !== 'running') {
            clearInterval(interval)
            // Reload findings when audit completes
            loadFindings(updated.id, 1, severityFilter, codeFilter)
          }
        }
      } catch {
        // Ignore background polling errors
      }
    }, 1500)

    return () => clearInterval(interval)
  }, [currentRun?.status, currentRun?.id, severityFilter, codeFilter])

  // Load findings for current run
  const loadFindings = useCallback(
    async (
      runId: string,
      page: number,
      severity: FindingSeverity | 'all',
      code: string,
    ) => {
      setFindingsLoading(true)
      try {
        const res = await listLibraryAuditFindings(runId, {
          severity: severity !== 'all' ? severity : undefined,
          findingCode: code !== 'all' ? code : undefined,
          limit: 25,
          offset: (page - 1) * 25,
        })
        setFindings(res.items || [])
        setFindingsTotal(res.meta?.total ?? res.items?.length ?? 0)
      } catch (err: unknown) {
        console.error('Failed to load findings:', err)
      } finally {
        setFindingsLoading(false)
      }
    },
    [],
  )

  useEffect(() => {
    if (currentRun && currentRun.id && currentRun.status === 'completed') {
      loadFindings(currentRun.id, currentPage, severityFilter, codeFilter)
    } else {
      setFindings([])
      setFindingsTotal(0)
    }
  }, [currentRun?.id, currentRun?.status, currentPage, severityFilter, codeFilter, loadFindings])

  const handleStartAudit = async (mode: AuditMode) => {
    setIsStarting(true)
    setRunError(null)
    try {
      const run = await startLibraryAudit(mode)
      setCurrentRun(run)
      setCurrentPage(1)
      setFindings([])
    } catch (err: unknown) {
      setRunError(err instanceof Error ? err.message : 'Audit konnte nicht gestartet werden.')
    } finally {
      setIsStarting(false)
    }
  }

  const handleCancelAudit = async () => {
    if (!currentRun || currentRun.status !== 'running') return
    setIsCancelling(true)
    try {
      await cancelLibraryAudit(currentRun.id)
      await fetchCurrentRun()
    } catch (err: unknown) {
      setRunError(err instanceof Error ? err.message : 'Audit konnte nicht abgebrochen werden.')
    } finally {
      setIsCancelling(false)
    }
  }

  const handleOpenRepair = async (finding: AuditFinding, action: RepairAction) => {
    setSelectedFinding(finding)
    setSelectedAction(action)
    setPreviewLoading(true)
    try {
      const previews = await previewLibraryRepairs([finding.id])
      if (previews.length > 0) {
        setActivePreview(previews[0] ?? null)
        setPreviewDialogOpen(true)
      }
    } catch (err: unknown) {
      alert(err instanceof Error ? err.message : 'Vorschau fehlgeschlagen.')
    } finally {
      setPreviewLoading(false)
    }
  }

  const handleRepairSuccess = () => {
    if (currentRun) {
      loadFindings(currentRun.id, currentPage, severityFilter, codeFilter)
      fetchCurrentRun()
    }
  }

  const renderSeverityBadge = (severity: FindingSeverity) => {
    switch (severity) {
      case 'error':
        return <Badge variant="destructive">Error</Badge>
      case 'warning':
        return <Badge variant="warning">Warning</Badge>
      case 'info':
        return <Badge variant="neutral">Info</Badge>
    }
  }

  const renderEvidenceBadge = (level?: EvidenceLevel) => {
    if (!level) return null
    switch (level) {
      case 'EXACT_CONTENT':
        return <Badge variant="success">Exact Content (SHA256)</Badge>
      case 'EXACT_CATALOG_ID':
        return <Badge variant="default">Katalog-ID Match</Badge>
      case 'STRONG_METADATA':
        return <Badge variant="outline" className="text-amber-400 border-amber-500/40">Metadaten Match</Badge>
      case 'WEAK_METADATA':
        return <Badge variant="outline" className="text-neutral-400">Schwacher Match</Badge>
      case 'UNKNOWN':
        return <Badge variant="outline" className="text-neutral-500">Unbekannt</Badge>
    }
  }

  return (
    <div className="space-y-6">
      {/* Header & Trigger Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 p-5 rounded-2xl border border-neutral-800 bg-neutral-900/50">
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <ShieldCheckIcon className="size-5 text-primary" />
            <h3 className="font-heading font-semibold text-neutral-200 text-base">
              Bibliotheks-Integritätsprüfung
            </h3>
          </div>
          <p className="text-xs text-neutral-400">
            Read-Only Audit zur Identifikation von fehlenden, unregistrierten oder veralteten Dateien.
          </p>
        </div>

        {isAdmin && (
          <div className="flex items-center gap-2">
            {currentRun?.status === 'running' ? (
              <Button
                variant="destructive"
                size="sm"
                className="h-8 text-xs"
                disabled={isCancelling}
                onClick={handleCancelAudit}
              >
                {isCancelling ? 'Bricht ab...' : 'Audit abbrechen'}
              </Button>
            ) : (
              <>
                <Button
                  variant="outline"
                  size="sm"
                  className="h-8 text-xs gap-1.5"
                  disabled={isStarting}
                  onClick={() => handleStartAudit('quick')}
                >
                  <RefreshCwIcon className={`size-3.5 ${isStarting ? 'animate-spin' : ''}`} />
                  Quick Audit
                </Button>
                <Button
                  variant="default"
                  size="sm"
                  className="h-8 text-xs gap-1.5"
                  disabled={isStarting}
                  onClick={() => handleStartAudit('deep')}
                >
                  <FileSearchIcon className="size-3.5" />
                  Deep Audit
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      {/* Error Message */}
      {runError && (
        <div className="p-4 rounded-xl border border-destructive/40 bg-destructive/10 text-destructive text-xs flex items-center gap-2">
          <AlertCircleIcon className="size-4 shrink-0" />
          <div>{runError}</div>
        </div>
      )}

      {/* Current Run Card */}
      {currentRun && (
        <div className="p-4 rounded-xl border border-neutral-800 bg-neutral-900/30 space-y-3 text-xs">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <span className="font-semibold text-neutral-200">
                Letzter Lauf: <span className="uppercase text-primary">{currentRun.mode} Audit</span>
              </span>
              <Badge
                variant={
                  currentRun.status === 'completed'
                    ? 'success'
                    : currentRun.status === 'running'
                    ? 'default'
                    : currentRun.status === 'cancelled'
                    ? 'outline'
                    : 'destructive'
                }
              >
                {currentRun.status}
              </Badge>
            </div>
            <div className="text-neutral-400">
              Gestartet: {formatRelative(currentRun.started_at)}
            </div>
          </div>

          {/* Progress Bar when running */}
          {currentRun.status === 'running' && (
            <div className="space-y-1.5">
              <div className="flex justify-between text-neutral-400 text-[11px]">
                <span>Prüfe Dateien...</span>
                <span>{currentRun.scanned} / {currentRun.total || '?'}</span>
              </div>
              <Progress
                value={currentRun.total > 0 ? (currentRun.scanned / currentRun.total) * 100 : null}
                className="h-2"
              />
            </div>
          )}

          {/* Summary Stats */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 pt-1">
            <div className="p-2.5 rounded-lg bg-neutral-900/60 border border-neutral-800/60">
              <div className="text-neutral-500 text-[11px]">Geprüfte Einträge</div>
              <div className="text-sm font-semibold text-neutral-200 font-mono mt-0.5">{currentRun.scanned}</div>
            </div>
            <div className="p-2.5 rounded-lg bg-neutral-900/60 border border-neutral-800/60">
              <div className="text-neutral-500 text-[11px]">Gefundene Probleme</div>
              <div className="text-sm font-semibold text-amber-400 font-mono mt-0.5">{currentRun.findings_count}</div>
            </div>
            <div className="p-2.5 rounded-lg bg-neutral-900/60 border border-neutral-800/60">
              <div className="text-neutral-500 text-[11px]">Status</div>
              <div className="text-sm font-semibold text-neutral-300 capitalize mt-0.5">{currentRun.status}</div>
            </div>
            <div className="p-2.5 rounded-lg bg-neutral-900/60 border border-neutral-800/60">
              <div className="text-neutral-500 text-[11px]">Modus</div>
              <div className="text-sm font-semibold text-neutral-300 uppercase mt-0.5">{currentRun.mode}</div>
            </div>
          </div>
        </div>
      )}

      {/* Findings Section */}
      {currentRun && currentRun.status === 'completed' && (
        <div className="space-y-4">
          {/* Filter Bar */}
          <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="text-neutral-500 mr-1">Schweregrad:</span>
              <Button
                variant={severityFilter === 'all' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => { setSeverityFilter('all'); setCurrentPage(1) }}
              >
                Alle
              </Button>
              <Button
                variant={severityFilter === 'error' ? 'destructive' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => { setSeverityFilter('error'); setCurrentPage(1) }}
              >
                Error
              </Button>
              <Button
                variant={severityFilter === 'warning' ? 'default' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => { setSeverityFilter('warning'); setCurrentPage(1) }}
              >
                Warning
              </Button>
              <Button
                variant={severityFilter === 'info' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => { setSeverityFilter('info'); setCurrentPage(1) }}
              >
                Info
              </Button>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-neutral-500">Problemtyp:</span>
              <select
                className="h-7 bg-neutral-900 border border-neutral-800 rounded px-2 text-xs text-neutral-300"
                value={codeFilter}
                onChange={(e) => { setCodeFilter(e.target.value); setCurrentPage(1) }}
              >
                <option value="all">Alle Typen</option>
                <option value="FILE_MISSING">FILE_MISSING</option>
                <option value="FILE_UNTRACKED">FILE_UNTRACKED</option>
                <option value="LEGACY_DUPLICATE">LEGACY_DUPLICATE</option>
                <option value="FILE_DUPLICATE">FILE_DUPLICATE</option>
                <option value="AUDIO_INVALID">AUDIO_INVALID</option>
                <option value="TAG_MISMATCH">TAG_MISMATCH</option>
                <option value="PATH_MISMATCH">PATH_MISMATCH</option>
                <option value="COVER_MISSING">COVER_MISSING</option>
                <option value="COVER_INVALID">COVER_INVALID</option>
                <option value="LYRICS_MISSING">LYRICS_MISSING</option>
                <option value="RELEASE_INCOMPLETE">RELEASE_INCOMPLETE</option>
              </select>
            </div>
          </div>

          {/* Findings List */}
          {findingsLoading ? (
            <div className="space-y-2.5">
              {Array.from({ length: 6 }).map((_, i) => (
                <div key={i} className="h-20 bg-neutral-900/40 rounded-xl animate-pulse" />
              ))}
            </div>
          ) : findings.length === 0 ? (
            <div className="p-8 rounded-2xl border border-neutral-800/80 bg-neutral-900/30 text-center space-y-2">
              <CheckCircle2Icon className="size-8 text-success mx-auto" />
              <h3 className="font-medium text-neutral-200">Keine Befunde für die aktuellen Filter</h3>
              <p className="text-xs text-neutral-400">
                Alle geprüften Dateien entsprechen den Richtlinien.
              </p>
            </div>
          ) : (
            <div className="space-y-2.5">
              {findings.map((f) => (
                <div
                  key={f.id}
                  className="p-3.5 rounded-xl border border-neutral-800/80 bg-neutral-900/40 hover:bg-neutral-900/60 transition-colors space-y-2 text-xs"
                >
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div className="flex flex-wrap items-center gap-1.5">
                      {renderSeverityBadge(f.severity)}
                      <Badge variant="outline" className="font-mono text-[10px]">
                        {f.finding_code}
                      </Badge>
                      {renderEvidenceBadge(f.evidence?.level)}
                    </div>

                    {isAdmin && (
                      <div className="flex items-center gap-1.5">
                        {f.finding_code === 'PATH_MISMATCH' && (
                          <Button
                            size="sm"
                            variant="secondary"
                            className="h-6 text-[11px] px-2 gap-1"
                            disabled={previewLoading}
                            onClick={() => handleOpenRepair(f, 'MOVE_CANONICAL')}
                          >
                            <FolderSyncIcon className="size-3" />
                            Pfad korrigieren
                          </Button>
                        )}
                        {f.finding_code === 'TAG_MISMATCH' && (
                          <Button
                            size="sm"
                            variant="secondary"
                            className="h-6 text-[11px] px-2 gap-1"
                            disabled={previewLoading}
                            onClick={() => handleOpenRepair(f, 'RESTORE_TAGS')}
                          >
                            <TagIcon className="size-3" />
                            Tags reparieren
                          </Button>
                        )}
                        {f.finding_code === 'FILE_UNTRACKED' && f.evidence?.level === 'EXACT_CATALOG_ID' && (
                          <Button
                            size="sm"
                            variant="secondary"
                            className="h-6 text-[11px] px-2 gap-1 text-emerald-400"
                            disabled={previewLoading}
                            onClick={() => handleOpenRepair(f, 'ADOPT_FILE')}
                          >
                            <ArchiveIcon className="size-3" />
                            Adoptieren
                          </Button>
                        )}
                        {(f.finding_code === 'FILE_UNTRACKED' || f.finding_code === 'LEGACY_DUPLICATE' || f.finding_code === 'FILE_DUPLICATE') && (
                          <Button
                            size="sm"
                            variant="destructive"
                            className="h-6 text-[11px] px-2 gap-1"
                            disabled={previewLoading}
                            onClick={() => handleOpenRepair(f, 'QUARANTINE_FILE')}
                          >
                            <Trash2Icon className="size-3" />
                            In Quarantäne
                          </Button>
                        )}
                      </div>
                    )}
                  </div>

                  {/* Relative Path */}
                  <div className="font-mono text-neutral-200 break-all select-all">{f.relative_path}</div>

                  {/* Details / Metadata */}
                  {f.evidence?.details && (
                    <div className="text-neutral-400 text-[11px]">{f.evidence.details}</div>
                  )}

                  {/* Expected vs Actual */}
                  {f.evidence?.expected_path && (
                    <div className="text-[11px] text-neutral-500 font-mono">
                      Erwarteter Pfad: <span className="text-primary">{f.evidence.expected_path}</span>
                    </div>
                  )}
                </div>
              ))}

              <Pagination
                page={currentPage}
                pageSize={25}
                total={findingsTotal}
                onPageChange={(p) => setCurrentPage(p)}
              />
            </div>
          )}
        </div>
      )}

      {/* Repair Preview Dialog */}
      <RepairPreviewDialog
        finding={selectedFinding}
        action={selectedAction}
        preview={activePreview}
        open={previewDialogOpen}
        onOpenChange={setPreviewDialogOpen}
        onSuccess={handleRepairSuccess}
      />
    </div>
  )
}
