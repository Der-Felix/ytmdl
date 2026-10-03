import { useEffect, useRef, useState } from 'react'
import { Download, WifiOff, HardDrive } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Link } from '@/lib/router'
import { useAuth } from '@/hooks/useAuth'
import { prepareOfflineShell, saveOfflinePlaylist } from '@/lib/offline-store'
import type { PlaylistDetail } from '@/types/playlist'
export function OfflineSavePanel({ playlist }: { playlist: PlaylistDetail }) {
  const { user } = useAuth()
  const control = useRef<AbortController | null>(null)
  const [agreed, setAgreed] = useState(false),
    [busy, setBusy] = useState(false),
    [done, setDone] = useState(0),
    [committed, setCommitted] = useState(false),
    [error, setError] = useState(''),
    [message, setMessage] = useState('')
  useEffect(() => () => control.current?.abort(), [])
  const save = async () => {
    if (control.current || !user || !agreed) return
    const abort = new AbortController()
    control.current = abort
    setBusy(true)
    setError('')
    setMessage('')
    setDone(0)
    setCommitted(false)
    try {
      await saveOfflinePlaylist(
        user.id,
        user.username,
        playlist,
        abort.signal,
        (n) => setDone(n),
      )
      setCommitted(true)
      setMessage('Musik in diesem Browser gespeichert.')
      try {
        const shell = await prepareOfflineShell()
        setMessage(
          shell
            ? 'Musik und App-Hülle sind für den Offline-Start vorbereitet. Öffne zum Test /offline.'
            : 'Musik gespeichert. Über diese HTTP-Adresse bleibt Offline-Wiedergabe auf eine bereits geöffnete App beschränkt. Ein späterer Offline-Start benötigt HTTPS.',
        )
      } catch (e) {
        setMessage(
          'Musik gespeichert. ' +
            (e instanceof Error ? e.message : 'App-Hülle noch nicht bereit.'),
        )
      }
    } catch (e) {
      if (abort.signal.aborted)
        setMessage(
          'Speichern abgebrochen. Die vorherige Offline-Kopie bleibt erhalten.',
        )
      else
        setError(
          e instanceof Error ? e.message : 'Offline-Speichern fehlgeschlagen.',
        )
    } finally {
      control.current = null
      setBusy(false)
    }
  }
  return (
    <section className="rounded-xl border border-border bg-muted/20 p-4 space-y-3">
      <div className="flex gap-2 items-center">
        <WifiOff className="size-4 text-primary" />
        <h2 className="font-medium">Diese Playlist offline mitnehmen</h2>
      </div>
      <p className="text-xs text-muted-foreground">
        Eine lokale Kopie für dieses Browserprofil, bis zu 200 Titel und 512
        MiB. Audio und verfügbare lokale Cover bleiben auch nach dem Abmelden
        zugänglich. Der Server und die Playlist werden nicht verändert. Der
        Browser kann Speicher bei Platzmangel entfernen.
      </p>
      <label className="flex gap-2 text-sm items-start">
        <input
          type="checkbox"
          checked={agreed}
          disabled={busy}
          onChange={(e) => setAgreed(e.target.checked)}
        />
        <span>Musik und Metadaten in diesem Browserprofil aufbewahren.</span>
      </label>
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={
            busy ||
            !agreed ||
            !playlist.tracks.length ||
            playlist.tracks.length > 200
          }
          onClick={() => void save()}
        >
          <Download className="size-4" />
          {busy
            ? `${done}/${playlist.tracks.length} gespeichert …`
            : 'Offline-Kopie speichern / erneuern'}
        </Button>
        {busy && !committed && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => control.current?.abort()}
          >
            Abbrechen
          </Button>
        )}
        <Button
          size="sm"
          variant="ghost"
          disabled={busy || !navigator.storage?.persist}
          onClick={async () => {
            try {
              const granted = await navigator.storage.persist()
              setMessage(
                granted
                  ? 'Browser bevorzugt dauerhafte Aufbewahrung. Backups auf dem Server bleiben wichtig.'
                  : 'Der Browser garantiert keine dauerhafte Aufbewahrung.',
              )
            } catch {
              setError('Dauerhafte Aufbewahrung konnte nicht angefragt werden.')
            }
          }}
        >
          <HardDrive className="size-4" />
          Browserspeicher schützen
        </Button>
        <Link
          href="/offline"
          className="inline-flex items-center rounded-lg px-3 py-2 text-sm hover:bg-white/8 focus-visible:outline-2 focus-visible:outline-ring"
        >
          Offline-Musik öffnen
        </Link>
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
    </section>
  )
}
