import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { deleteArtwork, uploadArtwork } from '@/lib/api/libraryTools'
import { notifyError, notifySuccess } from '@/lib/notify'
export function ArtworkEditor({
  kind,
  id,
  onSaved,
}: {
  kind: 'artists' | 'releases'
  id: string
  onSaved?: () => void
}) {
  const [busy, setBusy] = useState(false)
  const save = async (file?: File) => {
    if (busy) return
    setBusy(true)
    try {
      if (file) await uploadArtwork(kind, id, file)
      else await deleteArtwork(kind, id)
      notifySuccess(file ? 'Bild gespeichert' : 'Eigenes Bild entfernt')
      onSaved?.()
    } catch (e) {
      notifyError('Bild konnte nicht gespeichert werden', e)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-2 text-xs">
      <label data-slot="button" data-disabled={busy ? '' : undefined} className="inline-flex rounded-md border border-border px-3 py-2 hover:bg-muted">
        {busy ? 'Speichere …' : 'Eigenes Bild hochladen'}
        <input
          className="sr-only"
          aria-label="Eigenes Bild hochladen"
          type="file"
          accept="image/jpeg,image/png"
          disabled={busy}
          onChange={(e) => {
            const file = e.target.files?.[0]
            if (file) void save(file)
            e.target.value = ''
          }}
        />
      </label>
      <Button size="sm" variant="ghost" disabled={busy} onClick={() => void save()}>
        Eigenes Bild entfernen
      </Button>
      <p className="text-muted-foreground">
        JPEG oder PNG, bis 8 MiB. Audiodateien bleiben unverändert.
      </p>
    </div>
  )
}
