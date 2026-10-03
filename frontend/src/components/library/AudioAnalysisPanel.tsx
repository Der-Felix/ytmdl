import { useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { ErrorState } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import {
  audioAnalysisStatus,
  analyzeAudioFingerprint,
  resetAudioAnalysis,
} from '@/lib/api/libraryTools'
export function AudioAnalysisPanel() {
  const { state, reload } = useAsync(
    (signal) => audioAnalysisStatus(signal),
    [],
  )
  const controller = useRef<AbortController | null>(null)
  const [busy, setBusy] = useState(false),
    [progress, setProgress] = useState(0),
    [limit, setLimit] = useState(50),
    [error, setError] = useState(''),
    [reset, setReset] = useState(false)
  useEffect(() => () => controller.current?.abort(), [])
  const start = async () => {
    if (controller.current) return
    const control = new AbortController()
    controller.current = control
    setBusy(true)
    setProgress(0)
    setError('')
    try {
      let done = 0
      while (done < limit && !control.signal.aborted) {
        const status = await audioAnalysisStatus(control.signal)
        if (!status.next_ids.length) break
        for (const id of status.next_ids) {
          if (done >= limit || control.signal.aborted) break
          await analyzeAudioFingerprint(id, control.signal)
          setProgress(++done)
        }
      }
    } catch (e) {
      if (!control.signal.aborted)
        setError(e instanceof Error ? e.message : 'Analyse fehlgeschlagen.')
    } finally {
      controller.current = null
      setBusy(false)
      reload()
    }
  }
  return (
    <div className="space-y-4">
      <div className="rounded-xl border border-border bg-muted/20 p-4 space-y-2">
        <h3 className="font-semibold">Audio statt nur Namen vergleichen</h3>
        <p className="text-sm text-muted-foreground">
          Chromaprint untersucht bis zu 90 Sekunden je lokaler Audiodatei. Es
          werden keine Audiodaten an einen Anbieter geschickt und keine
          Musikdateien verändert. Ähnliche Aufnahmen erscheinen unter „Mögliche
          Duplikate“; bitte die Versionen dort anhören.
        </p>
        <p className="text-xs text-muted-foreground">
          Kurze oder gleichförmige Aufnahmen können unklar bleiben. Ein
          Ausschnitt beweist nicht, dass zwei vollständige Songs identisch sind.
          Die Analyse startet nur hier und endet beim Abbrechen oder Verlassen
          dieses Bereichs.
        </p>
      </div>
      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}
      {state.status === 'success' && (
        <p className="text-sm">
          {state.data.ready} erkannt · {state.data.pending} offen ·{' '}
          {state.data.inconclusive} unklar · {state.data.failed} nicht
          analysierbar
        </p>
      )}
      <div className="flex flex-wrap items-center gap-3">
        <label className="text-sm">
          Titel je Durchlauf{' '}
          <select
            className="ml-2 rounded border border-border bg-background p-2"
            aria-label="Titel je Audioanalyse"
            value={limit}
            disabled={busy}
            onChange={(e) => setLimit(Number(e.target.value))}
          >
            {[10, 50, 100].map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={busy || state.status !== 'success' || !state.data.pending}
          onClick={() => void start()}
        >
          Audioerkennung starten
        </Button>
        {busy && (
          <Button variant="outline" onClick={() => controller.current?.abort()}>
            Abbrechen
          </Button>
        )}
        <Button variant="ghost" disabled={busy} onClick={() => reload()}>
          Aktualisieren
        </Button>
      </div>
      <p role="status" className="text-sm">
        {progress ? `${progress} Titel in diesem Durchlauf geprüft.` : ''}
        {busy ? ' Analyse läuft …' : ''}
      </p>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <Button
        variant="ghost"
        size="sm"
        disabled={busy}
        onClick={() => setReset(!reset)}
      >
        Analyseindex zurücksetzen
      </Button>
      {reset && (
        <div className="rounded-lg border border-border p-3 space-y-2">
          <p className="text-sm">
            Nur abgeleitete Audioergebnisse zurücksetzen? Musik, Favoriten und
            Playlists bleiben erhalten. Bereits erledigte Audiovergleiche können
            nach einer Neuanalyse erneut erscheinen.
          </p>
          <Button
            size="sm"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              try {
                await resetAudioAnalysis()
                setReset(false)
                reload()
              } catch (e) {
                setError(
                  e instanceof Error
                    ? e.message
                    : 'Zurücksetzen fehlgeschlagen.',
                )
              } finally {
                setBusy(false)
              }
            }}
          >
            Index zurücksetzen
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setReset(false)}>
            Abbrechen
          </Button>
        </div>
      )}
    </div>
  )
}
