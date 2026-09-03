import { useState } from 'react'
import {
  AlertTriangleIcon,
  CheckCircle2Icon,
  FileCheck2Icon,
  FolderSyncIcon,
  ShieldAlertIcon,
  XCircleIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { applyLibraryRepairs } from '@/lib/api/library'
import type { AuditFinding, RepairAction, RepairPreview } from '@/types/api'

interface RepairPreviewDialogProps {
  finding: AuditFinding | null
  action: RepairAction | null
  preview: RepairPreview | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function RepairPreviewDialog({
  finding,
  action,
  preview,
  open,
  onOpenChange,
  onSuccess,
}: RepairPreviewDialogProps) {
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!finding || !preview || !action) {
    return null
  }

  const handleApply = async () => {
    setApplying(true)
    setError(null)
    try {
      const res = await applyLibraryRepairs({
        confirm: true,
        actions: [{ finding_id: finding.id, action }],
      })
      if (res.failed > 0 && res.warnings && res.warnings.length > 0) {
        setError(res.warnings.join('\n'))
      } else {
        onSuccess()
        onOpenChange(false)
      }
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Reparatur fehlgeschlagen.')
    } finally {
      setApplying(false)
    }
  }

  const getActionLabel = (act: RepairAction) => {
    switch (act) {
      case 'MOVE_CANONICAL':
        return 'Pfad-Korrektur (Verschieben)'
      case 'RESTORE_TAGS':
        return 'Metadaten & Tags wiederherstellen'
      case 'ADOPT_FILE':
        return 'Datei in Katalog adoptieren'
      case 'QUARANTINE_FILE':
        return 'In Quarantäne verschieben (.ytmdl-trash)'
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <FolderSyncIcon className="size-5 text-primary" />
            <DialogTitle>{getActionLabel(action)}</DialogTitle>
          </div>
          <DialogDescription>
            Vorschau der Änderungen für Datei <span className="font-mono text-neutral-300">{finding.relative_path}</span>
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto space-y-4 py-2 text-xs">
          {/* Allowed Status Banner */}
          {!preview.allowed ? (
            <div className="p-3 rounded-xl border border-destructive/40 bg-destructive/10 text-destructive flex items-start gap-2.5">
              <XCircleIcon className="size-4 shrink-0 mt-0.5" />
              <div className="space-y-1">
                <div className="font-semibold">Aktion kann nicht ausgeführt werden</div>
                <div className="text-neutral-300">{preview.message}</div>
              </div>
            </div>
          ) : (
            <div className="p-3 rounded-xl border border-success/30 bg-success/10 text-success flex items-start gap-2.5">
              <CheckCircle2Icon className="size-4 shrink-0 mt-0.5" />
              <div className="space-y-1">
                <div className="font-semibold">Sicherheitsprüfung bestanden</div>
                <div className="text-neutral-300">
                  Alle Vorbedingungen (Pfade, Datei-Integrität, Identität) sind erfüllt.
                </div>
              </div>
            </div>
          )}

          {/* Path Details */}
          <div className="p-3 rounded-xl border border-neutral-800 bg-neutral-900/60 space-y-2">
            <div className="text-neutral-400 font-medium">Quelldatei:</div>
            <div className="font-mono text-neutral-200 break-all select-all">{preview.source_path}</div>
            {preview.destination_path && (
              <>
                <div className="text-neutral-400 font-medium pt-1">Zielpfad:</div>
                <div className="font-mono text-primary break-all select-all">{preview.destination_path}</div>
              </>
            )}
          </div>

          {/* File Changes */}
          {preview.file_changes && preview.file_changes.length > 0 && (
            <div className="space-y-1.5">
              <div className="font-medium text-neutral-300 flex items-center gap-1.5">
                <FileCheck2Icon className="size-3.5 text-primary" />
                Dateisystem-Änderungen:
              </div>
              <ul className="list-disc list-inside space-y-1 text-neutral-400 bg-neutral-900/30 p-2.5 rounded-lg border border-neutral-800/60">
                {preview.file_changes.map((fc, i) => (
                  <li key={i} className="font-mono text-[11px] break-all">{fc}</li>
                ))}
              </ul>
            </div>
          )}

          {/* DB Changes */}
          {preview.db_changes && preview.db_changes.length > 0 && (
            <div className="space-y-1.5">
              <div className="font-medium text-neutral-300 flex items-center gap-1.5">
                <FolderSyncIcon className="size-3.5 text-primary" />
                Datenbank-Änderungen:
              </div>
              <ul className="list-disc list-inside space-y-1 text-neutral-400 bg-neutral-900/30 p-2.5 rounded-lg border border-neutral-800/60">
                {preview.db_changes.map((dc, i) => (
                  <li key={i} className="font-mono text-[11px] break-all">{dc}</li>
                ))}
              </ul>
            </div>
          )}

          {/* Warnings */}
          {preview.warnings && preview.warnings.length > 0 && (
            <div className="p-3 rounded-xl border border-warning/30 bg-warning/10 text-warning space-y-1">
              <div className="font-medium flex items-center gap-1.5">
                <AlertTriangleIcon className="size-3.5" />
                Hinweis:
              </div>
              {preview.warnings.map((w, i) => (
                <div key={i} className="text-neutral-300 text-[11px]">{w}</div>
              ))}
            </div>
          )}

          {/* Error Banner */}
          {error && (
            <div className="p-3 rounded-xl border border-destructive/40 bg-destructive/10 text-destructive flex items-start gap-2">
              <ShieldAlertIcon className="size-4 shrink-0 mt-0.5" />
              <div className="break-all">{error}</div>
            </div>
          )}
        </div>

        <DialogFooter className="gap-2 pt-2 border-t border-neutral-800">
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
            Abbrechen
          </Button>
          <Button
            variant={action === 'QUARANTINE_FILE' ? 'destructive' : 'default'}
            size="sm"
            disabled={!preview.allowed || applying}
            onClick={handleApply}
          >
            {applying ? 'Wird ausgeführt...' : 'Bestätigen & Ausführen'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
