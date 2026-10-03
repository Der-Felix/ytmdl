import { useEffect, useRef, useState } from 'react'
import { MonitorSmartphone, Send, Play } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAsync } from '@/hooks/useAsync'
import {
  usePlayerActions,
  usePlayerProgress,
  usePlayerState,
} from '@/hooks/usePlayer'
import {
  getPlaybackHandoff,
  savePlaybackHandoff,
  deletePlaybackHandoff,
} from '@/lib/api/libraryTools'
import { formatDuration } from '@/lib/utils/format'
export function HandoffPanel() {
  const player = usePlayerState(),
    progress = usePlayerProgress()
  const operation = useRef<AbortController | null>(null)
  useEffect(() => () => operation.current?.abort(), [])
  const latest = useRef(player)
  latest.current = player
  const { pause, resumeSession } = usePlayerActions()
  const { state, reload } = useAsync((signal) => getPlaybackHandoff(signal), [])
  const [name, setName] = useState('Mein Gerät'),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [message, setMessage] = useState('')
  const send = async () => {
    if (busy || !player.currentTrack) return
    const control = new AbortController()
    operation.current = control
    setBusy(true)
    setError('')
    try {
      await savePlaybackHandoff(
        {
          queue_ids: player.queue.map((t) => t.id),
          queue_index: player.queueIndex,
          position_seconds: progress.currentTime,
          repeat_mode: player.repeatMode,
          source_name: name,
        },
        control.signal,
      )
      control.signal.throwIfAborted()
      const unchanged =
        latest.current.currentTrack?.id === player.currentTrack.id &&
        latest.current.queueIndex === player.queueIndex &&
        latest.current.queue === player.queue
      if (unchanged) pause()
      setMessage(
        unchanged
          ? 'Übergabe bereit. Wiedergabe hier pausiert. Auf dem anderen Gerät mit demselben Konto „Aktualisieren“ und „Übernehmen und abspielen“ wählen.'
          : 'Übergabe bereit. Deine Wiedergabe hat sich inzwischen geändert und läuft hier weiter.',
      )
      reload()
    } catch (e) {
      if (!control.signal.aborted)
        setError(e instanceof Error ? e.message : 'Übergabe fehlgeschlagen.')
    } finally {
      setBusy(false)
    }
  }
  const receive = async () => {
    if (busy) return
    const control = new AbortController()
    operation.current = control
    setBusy(true)
    setError('')
    try {
      const fresh = await getPlaybackHandoff(control.signal)
      control.signal.throwIfAborted()
      if (!fresh) {
        setMessage(
          'Keine aktuelle Übergabe vorhanden. Bitte am Ausgangsgerät neu übertragen.',
        )
        reload()
        return
      }
      if (
        state.status === 'success' &&
        state.data &&
        state.data.id !== fresh.id
      ) {
        setMessage(
          'Die Übergabe wurde inzwischen ersetzt. Bitte die neue Auswahl prüfen.',
        )
        reload()
        return
      }
      resumeSession(
        fresh.queue,
        fresh.queue_index,
        fresh.position_seconds,
        fresh.repeat_mode,
      )
      setMessage('Warteschlange und Position übernommen.')
      reload()
    } catch (e) {
      if (!control.signal.aborted)
        setError(e instanceof Error ? e.message : 'Übernehmen fehlgeschlagen.')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-4">
      <div className="rounded-2xl border border-border bg-muted/20 p-5 space-y-3">
        <MonitorSmartphone className="size-8 text-primary" />
        <h3 className="text-xl font-semibold">
          Auf einem anderen Gerät weiterhören
        </h3>
        <p className="text-sm text-muted-foreground">
          Übertrage deine aktuelle Warteschlange und Position für 15 Minuten.
          Beide Geräte benötigen Verbindung zum Server und dasselbe Konto.
          Übernehmen ersetzt die Warteschlange auf dem Zielgerät; dessen
          Lautstärke und Klangeinstellungen bleiben erhalten.
        </p>
        <label className="block text-sm space-y-2">
          <span>Name dieses Geräts</span>
          <Input
            value={name}
            maxLength={80}
            disabled={busy}
            onChange={(e) => setName(e.target.value)}
            placeholder="z. B. Laptop"
          />
        </label>
        <div className="flex flex-wrap gap-2">
          <Button
            disabled={busy || !player.currentTrack || player.queue.length > 500}
            onClick={() => void send()}
          >
            <Send className="size-4" />
            Hier pausieren und übertragen
          </Button>
          <Button variant="outline" disabled={busy} onClick={() => reload()}>
            Aktualisieren
          </Button>
        </div>
        {player.queue.length > 500 && (
          <p className="text-xs text-muted-foreground">
            Für eine Übergabe die Warteschlange auf höchstens 500 Titel
            verkürzen.
          </p>
        )}
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
      {state.status === 'error' && (
        <div role="alert" className="text-sm">
          {state.error instanceof Error
            ? state.error.message
            : 'Übergabe konnte nicht geladen werden.'}
          <Button variant="ghost" onClick={() => reload()}>
            Erneut versuchen
          </Button>
        </div>
      )}
      {state.status === 'success' &&
        (state.data ? (
          <div className="rounded-xl border border-primary/30 p-4 space-y-2">
            <p className="font-medium">
              Bereit von {state.data.source_name || 'einem anderen Gerät'}
            </p>
            <p className="text-sm">
              {state.data.queue[state.data.queue_index]?.title} ·{' '}
              {formatDuration(state.data.position_seconds * 1000)} ·{' '}
              {state.data.queue.length} Titel
            </p>
            <p className="text-xs text-muted-foreground">
              Gültig bis {new Date(state.data.expires_at).toLocaleTimeString()}
            </p>
            <div className="flex gap-2 flex-wrap">
              <Button disabled={busy} onClick={() => void receive()}>
                <Play className="size-4" />
                Übernehmen und abspielen
              </Button>
              <Button
                variant="ghost"
                disabled={busy}
                onClick={async () => {
                  if (!state.data) return
                  setBusy(true)
                  try {
                    await deletePlaybackHandoff(state.data.id)
                    setMessage('Übergabe verworfen.')
                    reload()
                  } catch (e) {
                    setError(
                      e instanceof Error
                        ? e.message
                        : 'Verwerfen fehlgeschlagen.',
                    )
                  } finally {
                    setBusy(false)
                  }
                }}
              >
                Übergabe verwerfen
              </Button>
            </div>
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">
            Keine aktuelle Übergabe vorhanden.
          </p>
        ))}
    </div>
  )
}
