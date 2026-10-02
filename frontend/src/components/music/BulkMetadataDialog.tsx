import { useState } from 'react'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { updateSelectedMetadata, type MetadataPatch } from '@/lib/api/libraryTools'
export function BulkMetadataDialog({
  ids,
  open,
  onOpenChange,
  onSaved,
}: {
  ids: string[]
  open: boolean
  onOpenChange: (v: boolean) => void
  onSaved?: (patch: MetadataPatch) => void
}) {
  const [enabled, setEnabled] = useState<Record<string, boolean>>({}),
    [values, setValues] = useState<Record<string, string>>({}),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null)
  const save = async () => {
    const patch: MetadataPatch = {}
    if (enabled.album) patch.album = values.album || ''
    if (enabled.album_artist) patch.album_artist = values.album_artist || ''
    if (enabled.artists)
      patch.artists = (values.artists || '')
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean)
    if (enabled.year) patch.year = Number(values.year || 0)
    setBusy(true)
    setError(null)
    try {
      await updateSelectedMetadata(ids, patch)
      onSaved?.(patch)
      onOpenChange(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Speichern fehlgeschlagen.')
    } finally {
      setBusy(false)
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!busy) onOpenChange(v)
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Metadaten für {ids.length} Titel</DialogTitle>
          <DialogDescription>
            Nur angehakte Felder werden geändert. Ein leeres Feld löscht seinen Wert. Die Änderungen
            gelten in der Bibliothek; Audiodateien und Ordner bleiben unverändert.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          {(
            [
              ['album', 'Album'],
              ['album_artist', 'Album-Künstler'],
              ['artists', 'Künstler (mit Kommas trennen)'],
              ['year', 'Jahr'],
            ] as const
          ).map(([key, label]) => (
            <label key={key} className="block space-y-1 text-sm">
              <span className="flex gap-2">
                <input
                  type="checkbox"
                  checked={!!enabled[key]}
                  onChange={(e) => setEnabled({ ...enabled, [key]: e.target.checked })}
                />
                {label}
              </span>
              <Input
                aria-label={label}
                disabled={!enabled[key] || busy}
                type={key === 'year' ? 'number' : 'text'}
                min={0}
                max={9999}
                value={values[key] || ''}
                onChange={(e) => setValues({ ...values, [key]: e.target.value })}
              />
            </label>
          ))}
          {error && (
            <p role="alert" className="text-destructive">
              {error}
            </p>
          )}
          <Button
            disabled={busy || !Object.values(enabled).some(Boolean)}
            onClick={() => void save()}
          >
            {busy ? 'Speichere …' : 'Änderungen speichern'}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
