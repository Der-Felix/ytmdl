import { useState, useRef, type ChangeEvent, type DragEvent } from 'react'
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  FileUpIcon,
  HelpCircleIcon,
  InfoIcon,
  Loader2Icon,
  RefreshCwIcon,
  UploadIcon,
  XCircleIcon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { errorMessage, isAbortError } from '@/lib/api/client'
import {
  applyImportSubscriptions,
  previewImportSubscriptions,
} from '@/lib/api/subscriptions'
import { cn } from '@/lib/utils'
import type {
  ImportItemStatus,
  ImportPreview,
  ImportResult,
  SubscriptionExport,
} from '@/types/api'

interface SubscriptionImportDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onImportSuccess: () => void
}

type Stage = 'pick' | 'preview' | 'result'

export function SubscriptionImportDialog({
  open,
  onOpenChange,
  onImportSuccess,
}: SubscriptionImportDialogProps) {
  const [stage, setStage] = useState<Stage>('pick')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [payload, setPayload] = useState<SubscriptionExport | null>(null)
  const [preview, setPreview] = useState<ImportPreview | null>(null)
  const [result, setResult] = useState<ImportResult | null>(null)
  const [isDragging, setIsDragging] = useState(false)
  const fileInputRef = useRef<HTMLInputElement>(null)

  function resetState() {
    setStage('pick')
    setLoading(false)
    setError(null)
    setPayload(null)
    setPreview(null)
    setResult(null)
    setIsDragging(false)
  }

  function handleOpenChange(nextOpen: boolean) {
    if (!nextOpen) {
      resetState()
    }
    onOpenChange(nextOpen)
  }

  async function processFile(file: File) {
    if (!file.name.endsWith('.json') && file.type !== 'application/json') {
      setError('Bitte wähle eine gültige .json Datei aus.')
      return
    }

    setLoading(true)
    setError(null)

    try {
      const text = await file.text()
      let parsed: SubscriptionExport
      try {
        parsed = JSON.parse(text)
      } catch {
        setError('Die Datei enthält kein gültiges JSON.')
        setLoading(false)
        return
      }

      if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed.subscriptions)) {
        setError('Ungültiges Dateiformat: Das Feld "subscriptions" fehlt oder ist ungültig.')
        setLoading(false)
        return
      }

      const previewData = await previewImportSubscriptions(parsed)
      setPayload(parsed)
      setPreview(previewData)
      setStage('preview')
    } catch (err) {
      if (!isAbortError(err)) {
        setError(errorMessage(err))
      }
    } finally {
      setLoading(false)
    }
  }

  function handleFileInput(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (file) {
      void processFile(file)
    }
  }

  function handleDrop(e: DragEvent<HTMLDivElement>) {
    e.preventDefault()
    setIsDragging(false)
    const file = e.dataTransfer.files?.[0]
    if (file) {
      void processFile(file)
    }
  }

  async function handleApply() {
    if (!payload) return

    setLoading(true)
    setError(null)

    try {
      const res = await applyImportSubscriptions(payload)
      setResult(res)
      setStage('result')
      onImportSuccess()
    } catch (err) {
      if (!isAbortError(err)) {
        setError(errorMessage(err))
      }
    } finally {
      setLoading(false)
    }
  }

  const importableCount = preview ? preview.new + preview.would_update : 0

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UploadIcon className="size-5 text-primary" />
            Abonnements importieren
          </DialogTitle>
          <DialogDescription>
            {stage === 'pick' &&
              'Wähle eine zuvor exportierte JSON-Abonnementdatei aus, um eine Vorschau anzuzeigen.'}
            {stage === 'preview' &&
              'Vorschau der Änderungen prüfen. Es wurden noch keine Datenbank-Änderungen vorgenommen.'}
            {stage === 'result' && 'Zusammenfassung des abgeschlossenen Imports.'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto py-2 space-y-4">
          {error && (
            <div
              role="alert"
              className="flex items-start gap-2.5 rounded-xl border border-destructive/30 bg-destructive/10 p-3 text-xs text-destructive"
            >
              <AlertCircleIcon className="mt-0.5 size-4 shrink-0" />
              <span>{error}</span>
            </div>
          )}

          {/* 1. PICK STAGE */}
          {stage === 'pick' && (
            <div className="space-y-4">
              <div
                onDragOver={(e) => {
                  e.preventDefault()
                  setIsDragging(true)
                }}
                onDragLeave={() => setIsDragging(false)}
                onDrop={handleDrop}
                onClick={() => fileInputRef.current?.click()}
                className={cn(
                  'flex flex-col items-center justify-center gap-3 rounded-2xl border-2 border-dashed p-8 text-center cursor-pointer transition-colors',
                  isDragging
                    ? 'border-primary bg-primary/5'
                    : 'border-border hover:border-primary/50 hover:bg-white/2',
                  loading && 'pointer-events-none opacity-60',
                )}
              >
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".json,application/json"
                  className="hidden"
                  onChange={handleFileInput}
                />
                <div className="flex size-12 items-center justify-center rounded-xl bg-primary/10 text-primary">
                  {loading ? (
                    <Loader2Icon className="size-6 animate-spin" />
                  ) : (
                    <FileUpIcon className="size-6" />
                  )}
                </div>
                <div className="space-y-1">
                  <p className="text-sm font-medium text-foreground">
                    {loading
                      ? 'Datei wird analysiert...'
                      : 'Klicke zum Auswählen oder ziehe die JSON-Datei hierher'}
                  </p>
                  <p className="text-xs text-muted-foreground">
                    Unterstützt: Versionierte YTMDL-Abonnement-Exporte (.json)
                  </p>
                </div>
              </div>

              <div className="rounded-xl border border-border/50 bg-white/2 p-3 text-xs text-muted-foreground flex items-start gap-2.5">
                <InfoIcon className="size-4 shrink-0 text-primary mt-0.5" />
                <p className="leading-relaxed">
                  <strong>Sicherer zweistufiger Import:</strong> Beim Auswählen der Datei wird zuerst
                  eine vollständige Vorschau erstellt. Es werden keine Downloads gestartet oder Jobs
                  erzeugt.
                </p>
              </div>
            </div>
          )}

          {/* 2. PREVIEW STAGE */}
          {stage === 'preview' && preview && (
            <div className="space-y-4">
              {/* Stat counters */}
              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
                <div className="rounded-xl border border-border bg-white/2 p-3 text-center">
                  <div className="text-xs text-muted-foreground">Gesamt</div>
                  <div className="text-lg font-semibold text-foreground">{preview.total}</div>
                </div>
                <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3 text-center">
                  <div className="text-xs text-emerald-400">Neu</div>
                  <div className="text-lg font-semibold text-emerald-400">+{preview.new}</div>
                </div>
                <div className="rounded-xl border border-amber-500/20 bg-amber-500/5 p-3 text-center">
                  <div className="text-xs text-amber-400">Aktualisierungen</div>
                  <div className="text-lg font-semibold text-amber-400">{preview.would_update}</div>
                </div>
                <div className="rounded-xl border border-border bg-white/2 p-3 text-center">
                  <div className="text-xs text-muted-foreground">Unverändert</div>
                  <div className="text-lg font-semibold text-muted-foreground">{preview.unchanged}</div>
                </div>
              </div>

              {(preview.invalid > 0 || preview.duplicates > 0) && (
                <div className="flex items-center gap-3 rounded-xl border border-destructive/20 bg-destructive/5 p-3 text-xs text-destructive">
                  <XCircleIcon className="size-4 shrink-0" />
                  <span>
                    {preview.invalid > 0 && `${preview.invalid} ungültige Einträge. `}
                    {preview.duplicates > 0 && `${preview.duplicates} Duplikate in Datei.`}
                  </span>
                </div>
              )}

              {/* Items Table */}
              <div className="space-y-2">
                <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">
                  Vorschau der Einträge ({preview.items.length})
                </div>
                <div className="max-h-60 overflow-y-auto rounded-xl border border-border bg-white/2 divide-y divide-border/50 text-xs">
                  {preview.items.map((item, idx) => (
                    <div key={idx} className="flex items-center justify-between gap-3 p-2.5 hover:bg-white/2">
                      <div className="min-w-0 flex-1 space-y-0.5">
                        <div className="flex items-center gap-2">
                          <span className="font-medium text-foreground truncate">{item.artist_name}</span>
                          <Badge variant="outline" className="text-[10px] py-0 px-1.5">
                            {item.provider}
                          </Badge>
                        </div>
                        {item.changes && item.changes.length > 0 && (
                          <div className="text-[11px] text-amber-400/90 truncate">
                            {item.changes.join(', ')}
                          </div>
                        )}
                        {item.error && (
                          <div className="text-[11px] text-destructive truncate">{item.error}</div>
                        )}
                      </div>
                      <StatusBadge status={item.status} />
                    </div>
                  ))}
                </div>
              </div>
            </div>
          )}

          {/* 3. RESULT STAGE */}
          {stage === 'result' && result && (
            <div className="space-y-4">
              <div className="flex items-center gap-3 rounded-xl border border-emerald-500/30 bg-emerald-500/10 p-4 text-emerald-400">
                <CheckCircle2Icon className="size-6 shrink-0" />
                <div className="space-y-0.5">
                  <p className="text-sm font-semibold">Import erfolgreich angewendet</p>
                  <p className="text-xs text-emerald-300/80">
                    Abonnements wurden aktualisiert. Bestehende Jobs und Downloads bleiben unverändert.
                  </p>
                </div>
              </div>

              <div className="grid grid-cols-2 sm:grid-cols-4 gap-2.5">
                <div className="rounded-xl border border-emerald-500/20 bg-emerald-500/5 p-3 text-center">
                  <div className="text-xs text-emerald-400">Neu erstellt</div>
                  <div className="text-lg font-semibold text-emerald-400">{result.created}</div>
                </div>
                <div className="rounded-xl border border-amber-500/20 bg-amber-500/5 p-3 text-center">
                  <div className="text-xs text-amber-400">Aktualisiert</div>
                  <div className="text-lg font-semibold text-amber-400">{result.updated}</div>
                </div>
                <div className="rounded-xl border border-border bg-white/2 p-3 text-center">
                  <div className="text-xs text-muted-foreground">Unverändert</div>
                  <div className="text-lg font-semibold text-muted-foreground">{result.unchanged}</div>
                </div>
                <div className="rounded-xl border border-border bg-white/2 p-3 text-center">
                  <div className="text-xs text-muted-foreground">Fehlgeschlagen</div>
                  <div className="text-lg font-semibold text-muted-foreground">{result.failed}</div>
                </div>
              </div>

              {result.errors && result.errors.length > 0 && (
                <div className="space-y-2">
                  <div className="text-xs font-semibold text-destructive">Fehlerhafte Einträge:</div>
                  <div className="max-h-40 overflow-y-auto rounded-xl border border-destructive/20 bg-destructive/5 p-2 text-xs space-y-1">
                    {result.errors.map((e, idx) => (
                      <div key={idx} className="text-destructive">
                        {e.artist_name ? `"${e.artist_name}": ` : ''}
                        {e.error}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>

        <DialogFooter className="pt-2 border-t border-border gap-2 sm:gap-0">
          {stage === 'pick' && (
            <Button variant="ghost" onClick={() => handleOpenChange(false)}>
              Abbrechen
            </Button>
          )}

          {stage === 'preview' && (
            <>
              <Button variant="ghost" onClick={() => setStage('pick')} disabled={loading}>
                Zurück
              </Button>
              <Button
                variant="default"
                onClick={() => void handleApply()}
                disabled={loading || importableCount === 0}
              >
                {loading ? (
                  <>
                    <Loader2Icon className="size-4 animate-spin" />
                    Wird importiert...
                  </>
                ) : (
                  <>
                    <RefreshCwIcon className="size-4" />
                    {importableCount > 0
                      ? `${importableCount} Abonnements importieren`
                      : 'Keine Änderungen vorhanden'}
                  </>
                )}
              </Button>
            </>
          )}

          {stage === 'result' && (
            <Button variant="default" onClick={() => handleOpenChange(false)}>
              Fertig
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function StatusBadge({ status }: { status: ImportItemStatus }) {
  switch (status) {
    case 'new':
      return (
        <Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/10 text-emerald-400 shrink-0">
          Neu
        </Badge>
      )
    case 'would_update':
      return (
        <Badge variant="outline" className="border-amber-500/40 bg-amber-500/10 text-amber-400 shrink-0">
          Änderung
        </Badge>
      )
    case 'unchanged':
      return (
        <Badge variant="neutral" className="shrink-0">
          Unverändert
        </Badge>
      )
    case 'duplicate':
      return (
        <Badge variant="outline" className="border-destructive/40 bg-destructive/10 text-destructive shrink-0">
          Duplikat
        </Badge>
      )
    case 'invalid':
      return (
        <Badge variant="destructive" className="shrink-0">
          Ungültig
        </Badge>
      )
    default:
      return (
        <Badge variant="neutral" className="shrink-0">
          <HelpCircleIcon className="size-3" />
        </Badge>
      )
  }
}
