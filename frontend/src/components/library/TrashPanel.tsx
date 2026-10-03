import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { ErrorState, ListSkeleton } from '@/components/ui/state-view'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAsync } from '@/hooks/useAsync'
import {
  libraryTrash,
  purgeTrash,
  recoverTrash,
  restoreTrash,
  type TrashEntry,
} from '@/lib/api/libraryTools'

export function TrashPanel() {
  const [page, setPage] = useState(0),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null),
    [message, setMessage] = useState(''),
    [confirm, setConfirm] = useState<TrashEntry | null>(null)
  const { state, reload } = useAsync(
    (signal) => libraryTrash(page * 20, signal),
    [page],
  )
  const run = async (action: () => Promise<void>, success: string) => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      await action()
      setMessage(success)
      setConfirm(null)
      await reload()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Aktion fehlgeschlagen.')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-border bg-muted/20 p-4 space-y-2">
        <h3 className="font-semibold">Dein Papierkorb</h3>
        <p className="text-sm text-muted-foreground">
          Entfernte Duplikate bleiben sieben Tage mit Audio und Lyrics auf dem
          Server erhalten. Danach werden sie automatisch endgültig gelöscht.
          Wiederherstellen bringt Favoriten und Zuordnungen zu noch vorhandenen
          Playlists zurück; neu hinzugefügte Titel bleiben erhalten.
        </p>
        <p className="text-xs text-muted-foreground">
          Bereits vor dieser Version endgültig gelöschte Titel können nicht
          zurückgeholt werden. Die Wiederherstellung überschreibt keine
          vorhandenen Dateien.
        </p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => void reload()}
        >
          Aktualisieren
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() =>
            void run(recoverTrash, 'Unterbrochene Aktionen geprüft.')
          }
        >
          Unterbrochene Aktionen wiederherstellen
        </Button>
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {message && (
        <p role="status" className="text-sm">
          {message}
        </p>
      )}
      {state.status === 'loading' && <ListSkeleton rows={3} />}
      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}
      {state.status === 'success' &&
        (state.data.length ? (
          <>
            <div className="divide-y divide-border">
              {state.data.map((entry) => (
                <div
                  key={entry.id}
                  className="flex flex-wrap items-center gap-3 py-4"
                >
                  <div className="min-w-0 flex-1">
                    <p className="font-medium break-words">{entry.title}</p>
                    <p className="text-xs text-muted-foreground">
                      Entfernt am {new Date(entry.created_at).toLocaleString()}{' '}
                      · Aufbewahrung bis{' '}
                      {new Date(entry.expires_at).toLocaleString()}
                    </p>
                    {entry.state !== 'ready' && (
                      <p className="text-xs text-muted-foreground">
                        {entry.state === 'purging'
                          ? 'Endgültiges Entfernen noch nicht abgeschlossen.'
                          : 'Aktion unterbrochen – bitte oben wiederherstellen.'}
                      </p>
                    )}
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy || entry.state !== 'ready'}
                    onClick={() =>
                      void run(
                        () => restoreTrash(entry.id),
                        'Titel, Favoriten und Playlist-Zuordnungen wiederhergestellt.',
                      )
                    }
                  >
                    Wiederherstellen
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={
                      busy || !['ready', 'purging'].includes(entry.state)
                    }
                    onClick={() => setConfirm(entry)}
                  >
                    Endgültig löschen
                  </Button>
                </div>
              ))}
            </div>
          </>
        ) : (
          <p className="text-sm text-muted-foreground">
            {page ? 'Keine weiteren Einträge.' : 'Der Papierkorb ist leer.'}
          </p>
        ))}
      <div className="flex gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy || page === 0}
          onClick={() => setPage((p) => p - 1)}
        >
          Zurück
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={
            busy || state.status !== 'success' || state.data.length < 20
          }
          onClick={() => setPage((p) => p + 1)}
        >
          Weitere Einträge
        </Button>
      </div>
      <Dialog
        open={!!confirm}
        onOpenChange={(open) => {
          if (!open && !busy) setConfirm(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Endgültig löschen?</DialogTitle>
            <DialogDescription>
              „{confirm?.title}“ wird mit allen aufbewahrten Dateien dauerhaft
              aus dem Papierkorb entfernt. Danach ist keine Wiederherstellung
              mehr möglich.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setConfirm(null)}
            >
              Abbrechen
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => {
                if (confirm)
                  void run(
                    () => purgeTrash(confirm.id),
                    'Papierkorbeintrag endgültig gelöscht.',
                  )
              }}
            >
              {busy ? 'Verarbeite …' : 'Dateien endgültig löschen'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
