import { useState } from 'react'
import type { ReactNode } from 'react'
import { Button, buttonVariants } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { deleteArtwork, uploadArtwork } from '@/lib/api/libraryTools'
import { notifyError, notifySuccess } from '@/lib/notify'
export function ArtworkEditor({
  kind,
  id,
  onSaved,
  actions,
}: {
  kind: 'artists' | 'releases'
  id: string
  onSaved?: () => void
  /** Further admin actions of the page, in the same row (e.g. delete). */
  actions?: ReactNode
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
    // Both controls in one row of the same height; the file input is hidden,
    // so its keyboard focus is shown on the label that looks like a button.
    <div className="flex flex-wrap items-center justify-center gap-2 text-center text-xs sm:justify-start sm:text-left">
      <label
        data-slot="button"
        data-disabled={busy ? '' : undefined}
        className={cn(
          buttonVariants({ variant: 'outline', size: 'sm' }),
          'cursor-pointer has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-focus-visible:outline-ring data-disabled:pointer-events-none data-disabled:opacity-45',
        )}
      >
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
      {actions}
      <p className="basis-full text-muted-foreground">
        JPEG oder PNG, bis 8 MiB. Audiodateien bleiben unverändert.
      </p>
    </div>
  )
}
