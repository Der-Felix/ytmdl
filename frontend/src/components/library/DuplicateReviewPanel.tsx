import { useContext, useEffect, useRef, useState, type PointerEvent } from 'react'
import { ArrowLeft, ArrowRight, Check, Pause, Play, RotateCcw, SkipForward } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ErrorState, ListSkeleton } from '@/components/ui/state-view'
import { Cover } from '@/components/music/Cover'
import { TracksTable } from '@/components/music/TracksTable'
import { useAsync } from '@/hooks/useAsync'
import { PlayerActionsContext, PlayerStateContext } from '@/contexts/PlayerContext'
import {
  duplicateGroups,
  saveDuplicateReview,
  resetDuplicateReview,
  removeDuplicateVersions,
  type DuplicateGroup,
  type DuplicateReview,
} from '@/lib/api/libraryTools'
import { libraryArtwork } from '@/lib/artwork'
import { deletionSelection, duplicateSwipe } from '@/lib/duplicate-review'
import { formatDuration, formatBytes, joinArtists } from '@/lib/utils/format'
import { navigate } from '@/lib/router'
import type { LibraryTrack } from '@/types/api'

export function DuplicateReviewPanel({ isAdmin }: { isAdmin: boolean }) {
  const [mode, setMode] = useState<'swipe' | 'table'>('swipe')
  const [includeReviewed, setIncludeReviewed] = useState(false)
  const [cursors, setCursors] = useState([''])
  const [page, setPage] = useState(0)
  const [index, setIndex] = useState(0)
  const { state, reload } = useAsync(
    (signal) => duplicateGroups(0, signal, { includeReviewed, after: cursors[page] }),
    [includeReviewed, cursors[page]],
  )
  const restart = () => {
    setPage(0)
    setCursors([''])
    setIndex(0)
    reload()
  }
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          variant={mode === 'swipe' ? 'default' : 'outline'}
          aria-pressed={mode === 'swipe'}
          onClick={() => setMode('swipe')}
        >
          Wischvergleich
        </Button>
        <Button
          size="sm"
          variant={mode === 'table' ? 'default' : 'outline'}
          aria-pressed={mode === 'table'}
          onClick={() => setMode('table')}
        >
          Tabelle
        </Button>
        <Button size="sm" variant="ghost" onClick={restart}>
          Neu laden
        </Button>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            checked={includeReviewed}
            onChange={(e) => {
              setIncludeReviewed(e.target.checked)
              setPage(0)
              setCursors([''])
              setIndex(0)
            }}
          />
          Erledigte Gruppen anzeigen
        </label>
      </div>
      <p className="text-sm text-muted-foreground">
        Gleicher Titel und Künstler sind ein Hinweis, kein Beweis. Höre beide Versionen an und
        vergleiche Album, Dauer und Format. Live- und Remix-Versionen können absichtlich verschieden
        sein.
      </p>
      {state.status === 'loading' && <ListSkeleton rows={3} />}
      {state.status === 'error' && <ErrorState error={state.error} onRetry={reload} />}
      {state.status === 'success' && (
        <>
          {state.data.length === 0 && (
            <p role="status" className="rounded-xl border border-border p-6">
              Keine weiteren offenen Kandidaten. Neue oder geänderte Versionen erscheinen beim
              nächsten Laden.
            </p>
          )}
          {mode === 'swipe' &&
            state.data.length > 0 &&
            (index < state.data.length ? (
              state.data[index]!.tracks.length === 0 ? (
                <p role="status">Diese Gruppe hat sich geändert. Bitte neu laden.</p>
              ) : (
                <DuplicateComparison
                  key={`${state.data[index]!.key}:${state.data[index]!.fingerprint}`}
                  group={state.data[index]!}
                  isAdmin={isAdmin}
                  position={index + 1}
                  total={state.data.length}
                  onNext={() => setIndex((i) => i + 1)}
                  onReload={restart}
                />
              )
            ) : (
              <div className="rounded-xl border border-border p-6">
                <p>Diese Seite ist durchgesehen. Übersprungene Gruppen bleiben offen.</p>
                <Button className="mt-3" size="sm" onClick={() => setIndex(0)}>
                  Diese Seite nochmals ansehen
                </Button>
              </div>
            ))}
          {mode === 'table' &&
            state.data.map((group) => (
              <div key={group.key} className="space-y-2">
                <p className="font-medium">
                  {group.tracks[0]?.title} · {group.count} Versionen{' '}
                  {group.count > group.tracks.length
                    ? `(erste ${group.tracks.length} angezeigt)`
                    : ''}
                </p>
                {group.outcome && (
                  <p className="text-xs text-muted-foreground">
                    {group.outcome === 'preferred'
                      ? 'Bevorzugte Version gespeichert'
                      : 'Alle Versionen behalten'}
                  </p>
                )}
                <TracksTable
                  tracks={group.tracks}
                  sort="title"
                  order="asc"
                  onSortChange={() => {}}
                  onTrackSelect={(track) =>
                    navigate(`/library?view=tracks&track=${encodeURIComponent(track.id)}`)
                  }
                />
              </div>
            ))}
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              disabled={page === 0}
              onClick={() => {
                setPage((p) => p - 1)
                setIndex(0)
              }}
            >
              Zurück
            </Button>
            <Button
              size="sm"
              disabled={state.data.length < 20}
              onClick={() => {
                const after = state.data.at(-1)!.key
                setCursors((prev) => [...prev.slice(0, page + 1), after])
                setPage((p) => p + 1)
                setIndex(0)
              }}
            >
              Weitere Gruppen
            </Button>
          </div>
        </>
      )}
    </div>
  )
}

function DuplicateComparison({
  group,
  isAdmin,
  position,
  total,
  onNext,
  onReload,
}: {
  group: DuplicateGroup
  isAdmin: boolean
  position: number
  total: number
  onNext: () => void
  onReload: () => void
}) {
  const ordered = [...group.tracks]
  const preferred = ordered.findIndex((track) => track.id === group.preferred_track_id)
  if (preferred > 0) ordered.unshift(ordered.splice(preferred, 1)[0]!)
  const [winner, setWinner] = useState<LibraryTrack>(ordered[0]!)
  const [cursor, setCursor] = useState(1)
  const [history, setHistory] = useState<{ winner: LibraryTrack; cursor: number }[]>([])
  const [outcome, setOutcome] = useState(group.outcome || '')
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const [error, setError] = useState<string | null>(null)
  const [previewError, setPreviewError] = useState<string | null>(null)
  const [previewID, setPreviewID] = useState<string | null>(null)
  const previewTrack = useRef<string | null>(null)
  const [cue, setCue] = useState(0)
  const audio = useRef<HTMLAudioElement | null>(null)
  const [confirm, setConfirm] = useState(false)
  const [selected, setSelected] = useState<string[]>([])
  const [removed, setRemoved] = useState<string[]>([])
  const [message, setMessage] = useState('')
  const [drag, setDrag] = useState(0)
  const gesture = useRef<{ id: number; x: number; y: number } | null>(null)
  const { pause, removeFromQueue } = useContext(PlayerActionsContext)!
  const player = useContext(PlayerStateContext)!
  const queue = useRef(player.queue)
  useEffect(() => {
    queue.current = player.queue
  }, [player.queue])
  const challenger = ordered[cursor]!
  const cueMax = Math.max(
    0,
    Math.floor(Math.min(winner.duration_ms, challenger?.duration_ms ?? winner.duration_ms) / 1000) -
      5,
  )
  const cuePoint = Math.min(cue, cueMax)
  const canCompare = group.count <= 100 && group.count === ordered.length && ordered.length > 1
  const stopPreview = () => {
    audio.current?.pause()
    previewTrack.current = null
    setPreviewID(null)
  }
  useEffect(() => {
    const element = audio.current
    return () => {
      element?.pause()
      element?.removeAttribute('src')
      element?.load()
    }
  }, [])
  useEffect(() => {
    if (audio.current) {
      audio.current.volume = player.volume
      audio.current.muted = player.muted
      if (player.status === 'playing') audio.current.pause()
    }
  }, [player.volume, player.muted, player.status])

  const review = (
    track = winner,
    result: 'preferred' | 'distinct' = 'preferred',
  ): DuplicateReview => ({
    group_key: group.key,
    fingerprint: group.fingerprint,
    outcome: result,
    preferred_track_id: result === 'preferred' ? track.id : '',
  })
  const save = async (track: LibraryTrack, result: 'preferred' | 'distinct') => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setError(null)
    stopPreview()
    try {
      await saveDuplicateReview(review(track, result))
      setWinner(track)
      setOutcome(result)
      setMessage(
        result === 'preferred'
          ? 'Bevorzugte Version gespeichert. Bisher wurde nichts gelöscht.'
          : 'Alle Versionen bleiben erhalten. Die Gruppe ist erledigt.',
      )
      if (isAdmin && result === 'preferred') {
        setSelected(ordered.filter((t) => t.id !== track.id).map((t) => t.id))
        setConfirm(true)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Entscheidung konnte nicht gespeichert werden.')
    } finally {
      busyRef.current = false
      setBusy(false)
    }
  }
  const choose = (preferNew: boolean) => {
    if (busyRef.current || outcome || !canCompare || !challenger) return
    stopPreview()
    setDrag(0)
    const next = preferNew ? challenger : winner
    if (cursor + 1 === ordered.length) void save(next, 'preferred')
    else {
      setHistory((prev) => [...prev, { winner, cursor }])
      setWinner(next)
      setCursor((c) => c + 1)
    }
  }
  const listen = (track: LibraryTrack) => {
    const element = audio.current
    if (!element) return
    if (previewID === track.id && !element.paused) {
      stopPreview()
      return
    }
    pause()
    setPreviewError(null)
    previewTrack.current = track.id
    element.volume = player.volume
    element.muted = player.muted
    element.src = `/api/v1/library/tracks/${encodeURIComponent(track.id)}/stream`
    void element.play().catch(() => {
      if (previewTrack.current === track.id) {
        setPreviewError('Hörprobe konnte nicht abgespielt werden. Bitte erneut versuchen.')
        setPreviewID(null)
      }
    })
  }
  const nextGroup = () => {
    stopPreview()
    onNext()
  }
  const card = (track: LibraryTrack, label: string) => (
    <>
      <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{label}</p>
      <div className="flex gap-4 items-start">
        <Cover
          src={libraryArtwork('tracks', track.id)}
          fallbackSrc={track.cover_url}
          alt={track.title}
          className="size-20 sm:size-28 shrink-0 rounded-xl"
        />
        <div className="min-w-0">
          <h3 className="font-semibold text-lg break-words">{track.title}</h3>
          <p className="text-sm text-muted-foreground break-words">
            {joinArtists(track.artists || []) || track.album_artist}
          </p>
        </div>
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-sm">
        <dt className="text-muted-foreground">Album</dt>
        <dd className="break-words">{track.album || 'Unbekannt'}</dd>
        <dt className="text-muted-foreground">Dauer</dt>
        <dd>{formatDuration(track.duration_ms)}</dd>
        <dt className="text-muted-foreground">Format</dt>
        <dd>
          {track.codec?.toUpperCase() || 'Unbekannt'}{' '}
          {track.bitrate_kbps ? `· ${Math.round(track.bitrate_kbps)} kbit/s` : ''}
        </dd>
        <dt className="text-muted-foreground">Größe</dt>
        <dd>{track.file_size_bytes ? formatBytes(track.file_size_bytes) : 'Unbekannt'}</dd>
        <dt className="text-muted-foreground">Jahr</dt>
        <dd>{track.year || 'Unbekannt'}</dd>
      </dl>
      <Button
        size="sm"
        variant="outline"
        disabled={busy}
        aria-label={`${track.album || track.title}: ${previewID === track.id ? 'Hörprobe stoppen' : 'Hörprobe abspielen'}`}
        onClick={() => listen(track)}
      >
        {previewID === track.id ? <Pause className="size-4" /> : <Play className="size-4" />}
        {previewID === track.id ? 'Hörprobe stoppen' : 'Hörprobe'}
      </Button>
    </>
  )

  return (
    <section
      className="space-y-4 min-w-0"
      aria-label="Duplikat-Wischvergleich"
      onKeyDown={(e) => {
        if (['ArrowLeft', 'ArrowRight', ' '].includes(e.key)) e.stopPropagation()
      }}
    >
      <audio
        ref={audio}
        preload="none"
        aria-hidden="true"
        onLoadedMetadata={() => {
          const element = audio.current
          if (element)
            element.currentTime = Math.min(
              cuePoint,
              Math.max(0, (Number.isFinite(element.duration) ? element.duration : 0) - 1),
            )
        }}
        onPlay={() => setPreviewID(previewTrack.current)}
        onPause={() => setPreviewID(null)}
        onEnded={stopPreview}
        onError={() => {
          if (previewTrack.current) {
            setPreviewError('Die Audiodatei konnte nicht geladen werden.')
            setPreviewID(null)
          }
        }}
      />
      <div className="flex flex-wrap justify-between gap-2">
        <h3 className="font-medium">
          {group.tracks[0]?.title} · {group.count} Versionen
        </h3>
        <span className="text-xs text-muted-foreground">
          Gruppe {position} von {total} auf dieser Seite
        </span>
      </div>
      {error && (
        <div role="alert" className="rounded-lg border border-destructive/30 p-3 text-sm">
          <p>{error}</p>
          <Button size="sm" variant="ghost" className="mt-2" onClick={onReload}>
            Gruppe neu laden
          </Button>
        </div>
      )}
      {previewError && (
        <p role="alert" className="text-sm text-destructive">
          {previewError}
        </p>
      )}
      {!canCompare ? (
        <div className="rounded-xl border border-border p-5">
          <p>
            Der Wischvergleich unterstützt vollständige Gruppen mit bis zu 100 Versionen. Bitte
            nutze für diese Gruppe die Tabellenansicht.
          </p>
          <Button size="sm" className="mt-3" onClick={nextGroup}>
            Überspringen
          </Button>
        </div>
      ) : outcome ? (
        <div className="space-y-3 rounded-xl border border-primary/25 bg-primary/5 p-5">
          <p role="status" className="text-sm">
            {message ||
              (outcome === 'preferred'
                ? 'Bevorzugte Version gespeichert.'
                : 'Alle Versionen behalten.')}
          </p>
          {outcome === 'preferred' && (
            <div className="space-y-3">{card(winner, 'Bevorzugte Version')}</div>
          )}
          <div className="flex flex-wrap gap-2">
            {isAdmin && outcome === 'preferred' && removed.length === 0 && (
              <Button
                size="sm"
                onClick={() => {
                  setSelected(ordered.filter((t) => t.id !== winner.id).map((t) => t.id))
                  setConfirm(true)
                }}
              >
                Andere Versionen prüfen und löschen
              </Button>
            )}
            {removed.length === 0 && (
              <Button
                size="sm"
                variant="ghost"
                disabled={busy}
                onClick={async () => {
                  if (busyRef.current) return
                  busyRef.current = true
                  setBusy(true)
                  setError(null)
                  try {
                    await resetDuplicateReview(group.key)
                    setOutcome('')
                    setCursor(1)
                    setWinner(ordered[0]!)
                    setHistory([])
                    setMessage('')
                  } catch (e) {
                    setError(e instanceof Error ? e.message : 'Zurücksetzen fehlgeschlagen.')
                  } finally {
                    busyRef.current = false
                    setBusy(false)
                  }
                }}
              >
                <RotateCcw className="size-4" />
                Neu vergleichen
              </Button>
            )}
            <Button size="sm" variant="default" onClick={nextGroup}>
              Nächste Gruppe
            </Button>
          </div>
        </div>
      ) : (
        <>
          <p className="text-sm text-muted-foreground">
            Vergleich {cursor} von {ordered.length - 1}: links behält die bisherige Version, rechts
            bevorzugt diese neue Version. Wischen speichert zunächst nur die Auswahl.
          </p>
          <div className="grid gap-4 md:grid-cols-2 overflow-x-clip py-2">
            <article className="min-w-0 space-y-4 rounded-2xl border border-border bg-white/[0.02] p-4">
              {card(winner, 'Bisher bevorzugt')}
            </article>
            <article
              aria-label="Versionen mit Pfeiltasten vergleichen"
              tabIndex={0}
              className="relative min-w-0 space-y-4 rounded-2xl border border-primary/25 bg-white/[0.02] p-4 select-none touch-pan-y focus-visible:outline-2 focus-visible:outline-primary focus-visible:outline-offset-2 motion-reduce:transition-none"
              style={{ transform: `translateX(${drag}px) rotate(${drag / 35}deg)` }}
              onKeyDown={(e) => {
                if (e.target !== e.currentTarget || e.ctrlKey || e.metaKey || e.altKey) return
                if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                  e.preventDefault()
                  e.stopPropagation()
                  choose(e.key === 'ArrowRight')
                }
              }}
              onPointerDown={(e: PointerEvent<HTMLElement>) => {
                if (
                  busy ||
                  e.button !== 0 ||
                  (e.target as HTMLElement).closest('button,input,select,a')
                )
                  return
                gesture.current = { id: e.pointerId, x: e.clientX, y: e.clientY }
                e.currentTarget.setPointerCapture(e.pointerId)
              }}
              onPointerMove={(e) => {
                const start = gesture.current
                if (start?.id === e.pointerId) {
                  const dx = e.clientX - start.x
                  const dy = e.clientY - start.y
                  setDrag(Math.abs(dx) > Math.abs(dy) * 1.3 ? Math.max(-140, Math.min(140, dx)) : 0)
                }
              }}
              onPointerUp={(e) => {
                const start = gesture.current
                gesture.current = null
                setDrag(0)
                if (e.currentTarget.hasPointerCapture(e.pointerId))
                  e.currentTarget.releasePointerCapture(e.pointerId)
                if (!start) return
                const direction = duplicateSwipe(e.clientX - start.x, e.clientY - start.y)
                if (direction) choose(direction === 'right')
              }}
              onPointerCancel={() => {
                gesture.current = null
                setDrag(0)
              }}
            >
              {drag !== 0 && (
                <p
                  aria-hidden="true"
                  className={`absolute right-2 top-2 rounded-md bg-background px-2 py-1 text-xs font-semibold ${drag > 0 ? 'text-success' : 'text-primary'}`}
                >
                  {drag > 0 ? 'Diese Version bevorzugen' : 'Bisherige Version behalten'}
                </p>
              )}
              {card(challenger, 'Neue Version · hier wischen')}
            </article>
          </div>
          <label className="flex flex-wrap items-center gap-3 text-sm">
            <span>Hörprobe ab {cuePoint === 0 ? '0:00' : formatDuration(cuePoint * 1000)}</span>
            <input
              aria-label="Startpunkt der Hörprobe"
              type="range"
              min={0}
              max={cueMax}
              step={1}
              value={cuePoint}
              onChange={(e) => {
                const value = Number(e.target.value)
                setCue(value)
                if (audio.current && previewID) audio.current.currentTime = value
              }}
              className="slider-quiet min-w-0 flex-1"
            />
          </label>
          <p className="text-xs text-muted-foreground">
            Hörproben pausieren den Player. Deine Queue bleibt erhalten. Pfeiltasten funktionieren,
            wenn die neue Karte fokussiert ist.
          </p>
          <div className="flex flex-wrap gap-2">
            <Button disabled={busy} onClick={() => choose(false)}>
              <ArrowLeft className="size-4" />
              Bisherige behalten
            </Button>
            <Button variant="default" disabled={busy} onClick={() => choose(true)}>
              Diese bevorzugen
              <ArrowRight className="size-4" />
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy || history.length === 0}
              onClick={() => {
                stopPreview()
                const prev = history.at(-1)!
                setWinner(prev.winner)
                setCursor(prev.cursor)
                setHistory((h) => h.slice(0, -1))
                setError(null)
              }}
            >
              <RotateCcw className="size-4" />
              Auswahl zurück
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => void save(winner, 'distinct')}
            >
              Alle Versionen behalten
            </Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={nextGroup}>
              <SkipForward className="size-4" />
              Später prüfen
            </Button>
          </div>
        </>
      )}
      <Dialog
        open={confirm}
        onOpenChange={(open) => {
          if (!busy) setConfirm(open)
        }}
      >
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>Andere Versionen wirklich löschen?</DialogTitle>
            <DialogDescription>
              Die bevorzugte Version bleibt erhalten. Ausgewählte Titel, Audiodateien und zugehörige
              Favoriten-/Playlist-Einträge werden für alle Nutzer entfernt. Abonnements können Titel
              später erneut herunterladen.
            </DialogDescription>
          </DialogHeader>
          <p className="text-sm font-medium">
            <Check className="inline size-4 mr-2 text-success" />
            Behalten: {winner.title} · {winner.album || 'Unbekanntes Album'}
          </p>
          <div className="max-h-64 overflow-y-auto space-y-2">
            {ordered
              .filter((t) => t.id !== winner.id && !removed.includes(t.id))
              .map((track) => (
                <label
                  key={track.id}
                  className="flex items-start gap-3 rounded-lg border border-border p-3"
                >
                  <input
                    type="checkbox"
                    aria-label={`${track.album || track.title} zum Löschen auswählen`}
                    checked={selected.includes(track.id)}
                    disabled={busy}
                    onChange={(e) =>
                      setSelected((prev) =>
                        e.target.checked
                          ? [...prev, track.id]
                          : prev.filter((id) => id !== track.id),
                      )
                    }
                  />
                  <span className="min-w-0 text-sm">
                    <span className="block font-medium break-words">
                      {track.title} · {track.album || 'Unbekanntes Album'}
                    </span>
                    <span className="text-muted-foreground">
                      {track.codec?.toUpperCase() || 'Format unbekannt'} ·{' '}
                      {formatDuration(track.duration_ms)}
                      {track.bitrate_kbps ? ` · ${Math.round(track.bitrate_kbps)} kbit/s` : ''}
                    </span>
                  </span>
                </label>
              ))}
          </div>
          <DialogFooter>
            <Button disabled={busy} onClick={() => setConfirm(false)}>
              Alle Dateien behalten
            </Button>
            <Button
              variant="destructive"
              disabled={busy || selected.length === 0}
              onClick={async () => {
                if (busyRef.current) return
                busyRef.current = true
                setBusy(true)
                setError(null)
                stopPreview()
                try {
                  const ids = deletionSelection(
                    winner.id,
                    ordered.map((t) => t.id),
                    selected,
                  )
                  const result = await removeDuplicateVersions(review(), ids)
                  setRemoved((prev) => [...prev, ...result.deleted_track_ids])
                  setSelected((prev) => prev.filter((id) => !result.deleted_track_ids.includes(id)))
                  // Remove only successfully deleted versions from this browser's queue.
                  const deleted = new Set(result.deleted_track_ids)
                  for (let i = queue.current.length - 1; i >= 0; i--)
                    if (deleted.has(queue.current[i]!.id)) removeFromQueue(i)
                  setConfirm(false)
                  setMessage(
                    `${result.deleted_track_ids.length} Versionen gelöscht. Die bevorzugte Version bleibt erhalten.`,
                  )
                  if (result.failed_track_id)
                    setError(
                      `${result.deleted_track_ids.length} Versionen wurden gelöscht. ${result.message || 'Eine weitere Version konnte nicht gelöscht werden.'} Bitte die Gruppe neu laden.`,
                    )
                } catch (e) {
                  setError(
                    e instanceof Error
                      ? e.message
                      : 'Löschen fehlgeschlagen. Bitte die Gruppe neu laden.',
                  )
                  setConfirm(false)
                } finally {
                  busyRef.current = false
                  setBusy(false)
                }
              }}
            >
              {busy ? 'Verarbeite …' : `${selected.length} Versionen endgültig löschen`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
