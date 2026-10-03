import { useContext, useEffect, useRef, useState, type PointerEvent } from 'react'
import {
  Check,
  Heart,
  Layers3,
  Pause,
  Play,
  RotateCcw,
  SkipForward,
  Sparkles,
  X,
} from 'lucide-react'
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
import { AuthContext } from '@/contexts/auth-context'
import './duplicate-review.css'
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
import {
  deletionSelection,
  duplicateSwipe,
  readDuplicateConfirmation,
  writeDuplicateConfirmation,
} from '@/lib/duplicate-review'
import { formatDuration, formatBytes, joinArtists } from '@/lib/utils/format'
import { navigate } from '@/lib/router'
import type { LibraryTrack } from '@/types/api'

export function DuplicateReviewPanel({ isAdmin }: { isAdmin: boolean }) {
  const auth = useContext(AuthContext)
  const userID = auth?.user?.id ?? 'default'
  const [preference, setPreference] = useState(() => ({
    userID,
    ask: readDuplicateConfirmation(userID),
  }))
  const askBeforeDelete =
    preference.userID === userID ? preference.ask : readDuplicateConfirmation(userID)
  const [working, setWorking] = useState(false)
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
    <div className="duplicate-review space-y-5">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          size="sm"
          disabled={working}
          variant={mode === 'swipe' ? 'default' : 'outline'}
          aria-pressed={mode === 'swipe'}
          onClick={() => setMode('swipe')}
        >
          Wischvergleich
        </Button>
        <Button
          size="sm"
          disabled={working}
          variant={mode === 'table' ? 'default' : 'outline'}
          aria-pressed={mode === 'table'}
          onClick={() => setMode('table')}
        >
          Tabelle
        </Button>
        <Button size="sm" variant="ghost" disabled={working} onClick={restart}>
          Neu laden
        </Button>
        <label className="flex items-center gap-2 text-sm text-muted-foreground">
          <input
            type="checkbox"
            disabled={working}
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
      <div className="duplicate-review-intro">
        <div>
          <span className="duplicate-review-eyebrow">
            <Layers3 className="size-4" />
            DEIN SOUND. DEINE VERSION.
          </span>
          <h3 className="mt-2 text-2xl font-semibold tracking-tight sm:text-3xl">
            Ein Song. Dein Favorit.
          </h3>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            Cover ansehen, kurz reinhören, entscheiden. Gleicher Titel und Künstler sind nur ein
            Hinweis: Live- und Remix-Versionen können absichtlich verschieden sein.
          </p>
        </div>
        {isAdmin && (
          <div className={`duplicate-review-preference ${askBeforeDelete ? '' : 'is-direct'}`}>
            <label className="flex cursor-pointer items-center gap-3 text-sm font-medium">
              <input
                type="checkbox"
                role="switch"
                checked={askBeforeDelete}
                disabled={working}
                onChange={(e) => {
                  const ask = e.target.checked
                  writeDuplicateConfirmation(userID, ask)
                  setPreference({ userID, ask })
                }}
              />
              Vor dem Löschen nachfragen
            </label>
            <p className="mt-2 text-xs text-muted-foreground">
              {askBeforeDelete
                ? 'Du wählst nach dem Vergleich aus, welche Versionen in den Papierkorb kommen.'
                : 'Direktmodus: Nach der letzten Auswahl werden alle anderen Versionen samt Dateien und Favoriten-/Playlist-Einträgen für alle Nutzer in den Papierkorb verschoben und nach sieben Tagen endgültig gelöscht. Wiederherstellen ist bis dahin möglich. Nur in diesem Browser und für dein Konto.'}
            </p>
          </div>
        )}
      </div>
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
                  askBeforeDelete={askBeforeDelete}
                  onBusyChange={setWorking}
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
              disabled={working || page === 0}
              onClick={() => {
                setPage((p) => p - 1)
                setIndex(0)
              }}
            >
              Zurück
            </Button>
            <Button
              size="sm"
              disabled={working || state.data.length < 20}
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
  askBeforeDelete,
  onBusyChange,
  position,
  total,
  onNext,
  onReload,
}: {
  group: DuplicateGroup
  isAdmin: boolean
  askBeforeDelete: boolean
  onBusyChange: (busy: boolean) => void
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
  const [exitDirection, setExitDirection] = useState<'left' | 'right' | null>(null)
  const choiceLock = useRef(false)
  const choiceTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  useEffect(
    () => () => {
      if (choiceTimer.current) clearTimeout(choiceTimer.current)
    },
    [],
  )
  const setWorking = (value: boolean) => {
    busyRef.current = value
    setBusy(value)
    onBusyChange(value)
  }
  const [drag, setDrag] = useState(0)
  const focusedCard = useRef<HTMLElement | null>(null)
  const restoreCardFocus = useRef(false)
  useEffect(() => {
    if (restoreCardFocus.current) {
      focusedCard.current?.focus({ preventScroll: true })
      restoreCardFocus.current = false
    }
  }, [cursor])
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
  const deleteVersions = async (preferred: LibraryTrack, selection: string[]) => {
    const ids = deletionSelection(
      preferred.id,
      ordered.map((t) => t.id),
      selection,
    )
    const result = await removeDuplicateVersions(review(preferred), ids)
    setRemoved((prev) => [...prev, ...result.deleted_track_ids])
    setSelected((prev) => prev.filter((id) => !result.deleted_track_ids.includes(id)))
    const deleted = new Set(result.deleted_track_ids)
    for (let i = queue.current.length - 1; i >= 0; i--)
      if (deleted.has(queue.current[i]!.id)) removeFromQueue(i)
    setConfirm(false)
    setMessage(
      `${result.deleted_track_ids.length} Versionen im Papierkorb. Die bevorzugte Version bleibt erhalten.`,
    )
    if (result.failed_track_id)
      setError(
        `${result.deleted_track_ids.length} Versionen wurden in den Papierkorb verschoben. ${result.message || 'Eine weitere Version konnte nicht gelöscht werden.'} Bitte die Gruppe neu laden.`,
      )
  }
  const save = async (track: LibraryTrack, result: 'preferred' | 'distinct') => {
    if (busyRef.current) return
    setWorking(true)
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
        const losers = ordered.filter((t) => t.id !== track.id).map((t) => t.id)
        setSelected(losers)
        if (askBeforeDelete) setConfirm(true)
        else await deleteVersions(track, losers)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Entscheidung konnte nicht gespeichert werden.')
    } finally {
      setWorking(false)
    }
  }
  const choose = (preferNew: boolean) => {
    if (busyRef.current || choiceLock.current || outcome || !canCompare || !challenger) return
    stopPreview()
    choiceLock.current = true
    setWorking(true)
    restoreCardFocus.current = document.activeElement === focusedCard.current
    setExitDirection(preferNew ? 'right' : 'left')
    const next = preferNew ? challenger : winner
    const reduceMotion = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches
    choiceTimer.current = setTimeout(
      () => {
        choiceTimer.current = null
        setDrag(0)
        setExitDirection(null)
        choiceLock.current = false
        setWorking(false)
        if (cursor + 1 === ordered.length) void save(next, 'preferred')
        else {
          setHistory((prev) => [...prev, { winner, cursor }])
          setWinner(next)
          setCursor((c) => c + 1)
        }
      },
      reduceMotion ? 0 : 180,
    )
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
      <div className="flex flex-wrap justify-between gap-2 items-center">
        <h3 className="font-medium">
          {group.tracks[0]?.title} · {group.count} Versionen
        </h3>
        <span className="text-xs text-muted-foreground">
          Gruppe {position} von {total} auf dieser Seite
        </span>
      </div>
      <div
        className="duplicate-review-progress"
        role="progressbar"
        aria-label="Vergleichsfortschritt"
        aria-valuemin={0}
        aria-valuemax={ordered.length - 1}
        aria-valuenow={outcome ? ordered.length - 1 : cursor - 1}
      >
        <span
          style={{
            width: `${outcome ? 100 : ((cursor - 1) / Math.max(1, ordered.length - 1)) * 100}%`,
          }}
        />
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
        <div className="duplicate-review-result space-y-4">
          <div className="flex items-center gap-3">
            <span className="duplicate-review-match">
              <Heart className="size-6" />
            </span>
            <div>
              <p className="duplicate-review-eyebrow">
                {outcome === 'preferred' ? 'DEIN FAVORIT' : 'BEWUSST VERSCHIEDEN'}
              </p>
              <h4 className="text-xl font-semibold">
                {outcome === 'preferred'
                  ? 'Die richtige Version für dich.'
                  : 'Mehr Vielfalt in deiner Bibliothek.'}
              </h4>
            </div>
          </div>
          <p role="status" aria-live="polite" className="text-sm">
            {message ||
              (outcome === 'preferred'
                ? 'Bevorzugte Version gespeichert.'
                : 'Alle Versionen behalten.')}
          </p>
          {outcome === 'preferred' && (
            <div className="duplicate-review-winner space-y-3">
              {card(winner, 'Bevorzugte Version')}
            </div>
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
                  setWorking(true)
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
                    setWorking(false)
                  }
                }}
              >
                <RotateCcw className="size-4" />
                Neu vergleichen
              </Button>
            )}
            <Button size="sm" variant="default" disabled={busy} onClick={nextGroup}>
              Nächste Gruppe
            </Button>
          </div>
        </div>
      ) : (
        <>
          <p className="text-sm text-muted-foreground" aria-live="polite">
            Vergleich {cursor} von {ordered.length - 1}: links behält die bisherige Version, rechts
            bevorzugt diese neue Version.
            {!askBeforeDelete && isAdmin
              ? ' Die letzte Auswahl löscht alle anderen Versionen sofort.'
              : ' Wischen speichert zunächst nur die Auswahl.'}
          </p>
          <div className="duplicate-review-arena">
            <article className="duplicate-review-incumbent">
              {card(winner, 'Bisher bevorzugt')}
            </article>
            <div className="duplicate-review-deck">
              <div
                className="duplicate-review-stack duplicate-review-stack-back"
                aria-hidden="true"
              />
              <div className="duplicate-review-stack" aria-hidden="true" />
              <article
                key={challenger.id}
                ref={focusedCard}
                aria-label="Versionen mit Pfeiltasten vergleichen"
                tabIndex={0}
                className={`duplicate-review-card ${exitDirection ? `is-exiting-${exitDirection}` : ''} ${cursor > 1 && !exitDirection ? 'is-entering' : ''}`}
                style={{
                  transform: `translateX(${drag}px) rotate(${drag / 28}deg)`,
                }}
                onKeyDown={(e) => {
                  if (e.target !== e.currentTarget || e.ctrlKey || e.metaKey || e.altKey) return
                  if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
                    e.preventDefault()
                    choose(e.key === 'ArrowRight')
                  }
                }}
                onDragStart={(e) => e.preventDefault()}
                onPointerDown={(e: PointerEvent<HTMLElement>) => {
                  if (
                    busy ||
                    choiceLock.current ||
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
                    const dx = e.clientX - start.x,
                      dy = e.clientY - start.y
                    setDrag(
                      Math.abs(dx) > Math.abs(dy) * 1.3 ? Math.max(-160, Math.min(160, dx)) : 0,
                    )
                  }
                }}
                onPointerUp={(e) => {
                  const start = gesture.current
                  gesture.current = null
                  if (e.currentTarget.hasPointerCapture(e.pointerId))
                    e.currentTarget.releasePointerCapture(e.pointerId)
                  if (!start) return
                  const direction = duplicateSwipe(e.clientX - start.x, e.clientY - start.y)
                  if (direction) choose(direction === 'right')
                  else setDrag(0)
                }}
                onPointerCancel={() => {
                  gesture.current = null
                  setDrag(0)
                }}
              >
                <div className="duplicate-review-art">
                  <Cover
                    src={libraryArtwork('tracks', challenger.id)}
                    fallbackSrc={challenger.cover_url}
                    alt={challenger.title}
                    className="size-full rounded-none border-0"
                  />
                  <span className="duplicate-review-card-label">
                    <Sparkles className="size-3.5" />
                    Neue Version
                  </span>
                  <span
                    aria-hidden="true"
                    className={`duplicate-review-stamp is-keep ${drag > 0 || exitDirection === 'right' ? 'is-visible' : ''}`}
                  >
                    BEHALTEN
                  </span>
                  <span
                    aria-hidden="true"
                    className={`duplicate-review-stamp is-pass ${drag < 0 || exitDirection === 'left' ? 'is-visible' : ''}`}
                  >
                    PASST NICHT
                  </span>
                  <div className="duplicate-review-art-caption">
                    <h4 className="text-xl font-semibold break-words">{challenger.title}</h4>
                    <p className="mt-1 text-sm text-white/80">
                      {joinArtists(challenger.artists || []) || challenger.album_artist}
                    </p>
                  </div>
                </div>
                <div className="duplicate-review-card-details">
                  <p className="font-medium break-words">
                    {challenger.album || 'Unbekanntes Album'}
                  </p>
                  <div className="duplicate-review-chips">
                    <span>{formatDuration(challenger.duration_ms)}</span>
                    <span>
                      {challenger.codec?.toUpperCase() || 'Format unbekannt'}
                      {challenger.bitrate_kbps
                        ? ` · ${Math.round(challenger.bitrate_kbps)} kbit/s`
                        : ''}
                    </span>
                    {challenger.year > 0 && <span>{challenger.year}</span>}
                    {challenger.file_size_bytes && challenger.file_size_bytes > 0 ? (
                      <span>{formatBytes(challenger.file_size_bytes)}</span>
                    ) : null}
                  </div>
                  <Button
                    size="sm"
                    variant="outline"
                    className="w-full"
                    disabled={busy || exitDirection !== null}
                    aria-label={`${challenger.album || challenger.title}: ${previewID === challenger.id ? 'Hörprobe stoppen' : 'Hörprobe abspielen'}`}
                    onClick={() => listen(challenger)}
                  >
                    {previewID === challenger.id ? (
                      <Pause className="size-4" />
                    ) : (
                      <Play className="size-4" />
                    )}
                    {previewID === challenger.id ? 'Hörprobe stoppen' : 'Hörprobe anhören'}
                  </Button>
                </div>
              </article>
            </div>
          </div>
          <div className="duplicate-review-decisions">
            <div>
              <Button
                aria-label="Bisherige behalten"
                title="Bisherige behalten · Pfeil links"
                className="duplicate-review-choice is-pass"
                variant="ghost"
                disabled={busy || exitDirection !== null}
                onClick={() => choose(false)}
              >
                <X className="size-7" />
              </Button>
              <span>Bisherige behalten</span>
            </div>
            <div>
              <Button
                aria-label="Auswahl zurück"
                title="Letzte Auswahl zurücknehmen"
                className="duplicate-review-undo"
                variant="ghost"
                disabled={busy || exitDirection !== null || history.length === 0}
                onClick={() => {
                  stopPreview()
                  const prev = history.at(-1)!
                  setWinner(prev.winner)
                  setCursor(prev.cursor)
                  setHistory((h) => h.slice(0, -1))
                  setError(null)
                }}
              >
                <RotateCcw className="size-5" />
              </Button>
              <span>Zurück</span>
            </div>
            <div>
              <Button
                aria-label="Diese bevorzugen"
                title="Diese bevorzugen · Pfeil rechts"
                className="duplicate-review-choice is-keep"
                variant="ghost"
                disabled={busy || exitDirection !== null}
                onClick={() => choose(true)}
              >
                <Heart className="size-7" />
              </Button>
              <span>Diese bevorzugen</span>
            </div>
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
          <div className="flex flex-wrap justify-center gap-2">
            <Button
              size="sm"
              variant="ghost"
              disabled={busy || exitDirection !== null}
              onClick={() => void save(winner, 'distinct')}
            >
              Alle Versionen behalten
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy || exitDirection !== null}
              onClick={nextGroup}
            >
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
            <DialogTitle>Andere Versionen in den Papierkorb?</DialogTitle>
            <DialogDescription>
              Die bevorzugte Version bleibt erhalten. Ausgewählte Titel, Audiodateien und zugehörige
              Favoriten-/Playlist-Einträge werden für alle Nutzer entfernt und für eine Wiederherstellung sieben Tage aufbewahrt. Die Dateien bleiben im Papierkorb. Abonnements können Titel
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
                setWorking(true)
                setError(null)
                stopPreview()
                try {
                  await deleteVersions(winner, selected)
                } catch (e) {
                  setError(
                    e instanceof Error
                      ? e.message
                      : 'Löschen fehlgeschlagen. Bitte die Gruppe neu laden.',
                  )
                  setConfirm(false)
                } finally {
                  setWorking(false)
                }
              }}
            >
              {busy ? 'Verarbeite …' : `${selected.length} Versionen in den Papierkorb`}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
