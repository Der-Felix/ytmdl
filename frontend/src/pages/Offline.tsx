import { useEffect, useState } from 'react'
import {
  WifiOff,
  Play,
  Pause,
  SkipBack,
  SkipForward,
  Trash2,
  RefreshCw,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Cover } from '@/components/music/Cover'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { useAsync } from '@/hooks/useAsync'
import { useOptionalAuth } from '@/hooks/useAuth'
import {
  usePlayerActions,
  usePlayerProgress,
  usePlayerState,
} from '@/hooks/usePlayer'
import {
  deleteOfflinePlaylist,
  listOfflinePlaylists,
  offlineCoverURL,
  forgetOfflineAudioURLs,
  type OfflinePlaylist,
} from '@/lib/offline-store'
import { Link } from '@/lib/router'
import type { LibraryTrack } from '@/types/api'
function OfflineCover({ track }: { track: LibraryTrack }) {
  const [image, setImage] = useState<string>()
  useEffect(() => {
    let active = true,
      url: string | undefined
    setImage(undefined)
    if (track.offline_blob_key)
      void offlineCoverURL(track.offline_blob_key).then(
        (value) => {
          if (!active) {
            if (value) URL.revokeObjectURL(value)
            return
          }
          url = value
          setImage(value)
        },
        () => {},
      )
    return () => {
      active = false
      if (url) URL.revokeObjectURL(url)
    }
  }, [track.offline_blob_key])
  return <Cover src={image} alt="" className="size-12 shrink-0" />
}
export function OfflinePage() {
  const auth = useOptionalAuth(),
    { state, reload } = useAsync(
      () => listOfflinePlaylists(auth?.user?.id),
      [auth?.user?.id],
    )
  const player = usePlayerState(),
    progress = usePlayerProgress(),
    {
      playAlbum,
      playQueueIndex,
      togglePlayPause,
      previous,
      next,
      pause,
      removeFromQueue,
      seek,
      setVolume,
    } = usePlayerActions()
  const [remove, setRemove] = useState<OfflinePlaylist | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState('')
  const local = !!player.currentTrack?.offline_blob_key
  const time = (seconds: number) =>
    `${Math.floor(seconds / 60)}:${Math.floor(seconds % 60)
      .toString()
      .padStart(2, '0')}`
  return (
    <main className="mx-auto max-w-4xl space-y-6 p-4 py-8 sm:p-8 bg-background min-h-dvh text-foreground">
      <header className="rounded-2xl border border-border bg-gradient-to-br from-primary/15 to-muted/20 p-5 space-y-3">
        <WifiOff className="size-8 text-primary" />
        <h1 className="text-2xl font-semibold">Deine Offline-Musik</h1>
        <p className="text-sm text-muted-foreground">
          Gespeicherte Musik aus diesem Browserprofil. Dafür ist keine
          Serveranmeldung erforderlich. Musik bleibt lokal; Favoriten, Lyrics,
          Radio und Hörverlauf auf dem Server sind hier nicht verfügbar.
        </p>
        <div className="flex flex-wrap gap-2">
          <Link
            href="/library"
            className="inline-flex items-center rounded-lg border border-border px-3 py-2 text-sm hover:bg-white/8 focus-visible:outline-2 focus-visible:outline-ring"
          >
            Zur Online-Bibliothek
          </Link>
          <Button variant="ghost" size="sm" onClick={() => reload()}>
            <RefreshCw className="size-4" />
            Lokale Kopien aktualisieren
          </Button>
        </div>
        {!window.isSecureContext && (
          <p className="text-xs text-muted-foreground">
            Über HTTP funktioniert diese Ansicht in einer bereits geöffneten
            App. Zum Neustart ohne Serververbindung braucht die App HTTPS und
            eine vorbereitete App-Hülle.
          </p>
        )}
      </header>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {state.status === 'loading' && (
        <p className="text-sm">Lokale Musik wird geladen …</p>
      )}
      {state.status === 'error' && (
        <div role="alert" className="rounded-xl border border-border p-4">
          <p>
            {state.error instanceof Error
              ? state.error.message
              : 'Lokale Musik konnte nicht gelesen werden.'}
          </p>
          <Button variant="ghost" onClick={() => reload()}>
            Erneut versuchen
          </Button>
        </div>
      )}
      {state.status === 'success' &&
        (state.data.length ? (
          <div className="grid gap-3 sm:grid-cols-2">
            {state.data.map((playlist) => (
              <article
                key={playlist.key}
                className="rounded-xl border border-border p-4 space-y-3"
              >
                <h2 className="font-semibold break-words">{playlist.name}</h2>
                <p className="text-xs text-muted-foreground">
                  {playlist.tracks.length} Titel ·{' '}
                  {(playlist.bytes / 1024 / 1024).toFixed(1)} MiB ·{' '}
                  {playlist.owner_name}
                </p>
                <p className="text-xs text-muted-foreground">
                  Gespeichert {new Date(playlist.saved_at).toLocaleString()}
                </p>
                <div className="flex flex-wrap gap-2">
                  <Button size="sm" onClick={() => playAlbum(playlist.tracks)}>
                    <Play className="size-4" />
                    Offline abspielen
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={busy}
                    onClick={() => setRemove(playlist)}
                  >
                    <Trash2 className="size-4" />
                    Lokale Kopie entfernen
                  </Button>
                </div>
              </article>
            ))}
          </div>
        ) : (
          <p className="rounded-xl border border-border p-5 text-sm text-muted-foreground">
            Noch keine Offline-Playlist in diesem Browser gespeichert. Öffne mit
            Serververbindung eine Playlist und wähle dort „Offline-Kopie
            speichern / erneuern“.
          </p>
        ))}
      {player.currentTrack && !local && (
        <p className="text-sm text-muted-foreground">
          Der aktuelle Titel läuft noch vom Server. Wähle oben eine gespeicherte
          Playlist für lokale Wiedergabe.{' '}
          <Button size="sm" variant="ghost" onClick={pause}>
            Server-Wiedergabe pausieren
          </Button>
        </p>
      )}
      {local && player.currentTrack && (
        <section
          aria-label="Offline-Player"
          className="rounded-2xl border border-primary/30 p-4 space-y-4"
        >
          <div className="flex gap-3 items-center">
            <OfflineCover track={player.currentTrack} />
            <div className="min-w-0">
              <h2 className="font-medium truncate">
                {player.currentTrack.title}
              </h2>
              <p className="text-xs text-muted-foreground truncate">
                {player.currentTrack.artists.join(' · ')}
              </p>
            </div>
          </div>
          <label className="block">
            <span className="sr-only">Offline-Wiedergabeposition</span>
            <input
              type="range"
              min={0}
              max={progress.duration || 1}
              step={0.1}
              value={Math.min(progress.currentTime, progress.duration || 1)}
              onChange={(e) => seek(Number(e.target.value))}
              className="w-full accent-primary"
            />
          </label>
          <div className="flex justify-between text-xs text-muted-foreground font-mono">
            <span>{time(progress.currentTime)}</span>
            <span>{time(progress.duration)}</span>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              size="icon"
              variant="ghost"
              aria-label="Offline vorheriger Titel"
              onClick={previous}
            >
              <SkipBack className="size-5" />
            </Button>
            <Button
              size="icon"
              aria-label={
                player.status === 'playing' || player.status === 'buffering'
                  ? 'Offline pausieren'
                  : 'Offline wiedergeben'
              }
              onClick={() =>
                player.status === 'buffering' ? pause() : togglePlayPause()
              }
            >
              {player.status === 'playing' ? (
                <Pause className="size-5" />
              ) : (
                <Play className="size-5" />
              )}
            </Button>
            <Button
              size="icon"
              variant="ghost"
              aria-label="Offline nächster Titel"
              onClick={() => next(true)}
            >
              <SkipForward className="size-5" />
            </Button>
            <label className="flex gap-2 items-center text-xs text-muted-foreground">
              Lautstärke
              <input
                aria-label="Offline-Lautstärke"
                type="range"
                min={0}
                max={1}
                step={0.01}
                value={player.volume}
                onChange={(e) => setVolume(Number(e.target.value))}
                className="w-24 accent-primary"
              />
            </label>
          </div>
          {player.error && (
            <p role="alert" className="text-sm text-destructive">
              {player.error}
            </p>
          )}
          <div className="divide-y divide-border">
            {player.queue.map((track, index) => (
              <button
                key={index}
                className={`flex w-full items-center gap-3 rounded-lg px-2 py-3 text-left text-sm hover:bg-white/8 focus-visible:outline-2 focus-visible:outline-ring ${index === player.queueIndex ? 'text-primary' : ''}`}
                onClick={() => playQueueIndex(index)}
              >
                <span className="w-5 text-xs">{index + 1}</span>
                <span className="min-w-0 flex-1 truncate">{track.title}</span>
                <Play className="size-3.5" />
              </button>
            ))}
          </div>
        </section>
      )}
      <Dialog
        open={!!remove}
        onOpenChange={(open) => {
          if (!open && !busy) setRemove(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Lokale Kopie entfernen?</DialogTitle>
            <DialogDescription>
              „{remove?.name}“ wird nur aus diesem Browser entfernt. Titel auf
              dem Server und die ursprüngliche Playlist bleiben erhalten. Gerade
              abgespielte Titel dieser Kopie werden pausiert und aus der lokalen
              Warteschlange entfernt.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setRemove(null)}
            >
              Abbrechen
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={async () => {
                if (!remove) return
                setBusy(true)
                setError('')
                try {
                  const keys = new Set(
                    remove.tracks
                      .map((t) => t.offline_blob_key)
                      .filter(Boolean) as string[],
                  )
                  await deleteOfflinePlaylist(remove.key)
                  if (
                    player.currentTrack?.offline_blob_key &&
                    keys.has(player.currentTrack.offline_blob_key)
                  )
                    pause()
                  for (let i = player.queue.length - 1; i >= 0; i--)
                    if (keys.has(player.queue[i]?.offline_blob_key || ''))
                      removeFromQueue(i)
                  forgetOfflineAudioURLs(keys)
                  setRemove(null)
                  reload()
                } catch (e) {
                  setError(
                    e instanceof Error
                      ? e.message
                      : 'Entfernen fehlgeschlagen.',
                  )
                } finally {
                  setBusy(false)
                }
              }}
            >
              Nur lokale Kopie entfernen
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </main>
  )
}
