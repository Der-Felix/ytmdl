import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ErrorState, ListSkeleton } from '@/components/ui/state-view'
import { Cover } from '@/components/music/Cover'
import { ArtworkEditor } from '@/components/music/ArtworkEditor'
import { DuplicateReviewPanel } from './DuplicateReviewPanel'
import { TrashPanel } from './TrashPanel'
import { SongRadioPanel } from './SongRadioPanel'
import { AudioAnalysisPanel } from './AudioAnalysisPanel'
import { usePlayerActions } from '@/hooks/usePlayer'
import { useAsync } from '@/hooks/useAsync'
import { libraryArtists, libraryReleases } from '@/lib/api/library'
import { clearListeningHistory, listeningHistory } from '@/lib/api/libraryTools'
import { libraryArtwork } from '@/lib/artwork'
import { navigate } from '@/lib/router'
import type { LibraryTrack } from '@/types/api'
const detail = (track: LibraryTrack) =>
  navigate(`/library?view=tracks&track=${encodeURIComponent(track.id)}`)
export function LibraryToolsPanel({ isAdmin }: { isAdmin: boolean }) {
  const [tab, setTab] = useState('history')
  return (
    <section className="space-y-4 rounded-xl border border-border p-4">
      <h2 className="text-lg font-semibold">Deine Bibliothek</h2>
      <div className="flex flex-wrap gap-2">
        {(
          [
            ['history', 'Hörverlauf'],
            ['radio', 'Song-Radio'],
            ['duplicates', 'Mögliche Duplikate'],
            ['artwork', 'Cover verwalten'],
            ['trash', 'Papierkorb'],
            ['audio', 'Audioerkennung'],
          ] as const
        )
          .filter(([id]) => !['trash', 'audio'].includes(id) || isAdmin)
          .map(([id, label]) => (
            <Button
              key={id}
              size="sm"
              variant={tab === id ? 'default' : 'outline'}
              onClick={() => setTab(id)}
            >
              {label}
            </Button>
          ))}
      </div>
      {tab === 'radio' ? (
        <SongRadioPanel />
      ) : tab === 'audio' && isAdmin ? (
        <AudioAnalysisPanel />
      ) : tab === 'trash' && isAdmin ? (
        <TrashPanel />
      ) : tab === 'history' ? (
        <HistoryPanel />
      ) : tab === 'duplicates' ? (
        <DuplicateReviewPanel isAdmin={isAdmin} />
      ) : (
        <ArtworkPanel isAdmin={isAdmin} />
      )}
    </section>
  )
}
function HistoryPanel() {
  const { playTrack } = usePlayerActions()
  const [sort, setSort] = useState('recent'),
    [busy, setBusy] = useState(false),
    [error, setError] = useState<string | null>(null),
    [confirm, setConfirm] = useState(false)
  const { state, reload } = useAsync(
    (signal) => listeningHistory(sort, signal),
    [sort],
  )
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <Button
          variant={sort === 'recent' ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setSort('recent')}
        >
          Zuletzt gehört
        </Button>
        <Button
          variant={sort === 'frequent' ? 'secondary' : 'ghost'}
          size="sm"
          onClick={() => setSort('frequent')}
        >
          Häufig gespielt
        </Button>
        <Button size="sm" variant="ghost" onClick={() => void reload()}>
          Aktualisieren
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={busy}
          onClick={() => setConfirm(!confirm)}
        >
          Verlauf löschen
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">
        Ein Titel zählt nach 30 Sekunden Wiedergabe, bei kurzen Titeln nach der
        halben Dauer. Dein Verlauf ist auf deinen Geräten verfügbar.
      </p>
      {confirm && (
        <div className="flex flex-wrap gap-2 rounded-md border border-border p-3">
          <span className="text-sm">Deinen gesamten Hörverlauf löschen?</span>
          <Button
            size="sm"
            disabled={busy}
            onClick={async () => {
              setBusy(true)
              setError(null)
              try {
                await clearListeningHistory()
                setConfirm(false)
                void reload()
              } catch (e) {
                setError(
                  e instanceof Error ? e.message : 'Löschen fehlgeschlagen.',
                )
              } finally {
                setBusy(false)
              }
            }}
          >
            Verlauf endgültig löschen
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setConfirm(false)}>
            Abbrechen
          </Button>
        </div>
      )}
      {error && <p role="alert">{error}</p>}
      {state.status === 'loading' && <ListSkeleton rows={3} />}{' '}
      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}{' '}
      {state.status === 'success' &&
        (state.data.length ? (
          <>
            <div className="divide-y divide-border">
              {state.data.map((track, index) => (
                <div key={track.id} className="flex items-center gap-3 py-3">
                  <Cover
                    src={libraryArtwork('tracks', track.id)}
                    fallbackSrc={track.cover_url}
                    alt=""
                    className="size-10 shrink-0"
                  />
                  <button
                    className="min-w-0 flex-1 text-left"
                    onClick={() => detail(track)}
                  >
                    <span className="block truncate text-sm font-medium">
                      {track.title}
                    </span>
                    <span className="block truncate text-xs text-muted-foreground">
                      {track.artists?.join(' · ')} · {track.play_count}{' '}
                      Wiedergaben ·{' '}
                      {new Date(track.last_played_at).toLocaleString()}
                    </span>
                  </button>
                  <Button
                    size="sm"
                    variant="ghost"
                    aria-label={`${track.title} abspielen`}
                    onClick={() => playTrack(track, state.data, index)}
                  >
                    Abspielen
                  </Button>
                </div>
              ))}
            </div>
          </>
        ) : (
          <p className="text-sm text-muted-foreground">
            Noch keine gehörten Titel.
          </p>
        ))}
    </div>
  )
}
function ArtworkPanel({ isAdmin }: { isAdmin: boolean }) {
  const [kind, setKind] = useState<'artists' | 'releases'>('artists'),
    [query, setQuery] = useState(''),
    [offset, setOffset] = useState(0),
    [version, setVersion] = useState(0),
    [missingOnly, setMissingOnly] = useState(false)
  const { state, reload } = useAsync(
    async (signal) => {
      const result =
        kind === 'artists'
          ? await libraryArtists({ q: query, offset, limit: 20, signal })
          : await libraryReleases({ q: query, offset, limit: 20, signal })
      const items = await Promise.all(
        result.items.map(async (item) => {
          const response = await fetch(libraryArtwork(kind, item.id), {
            method: 'HEAD',
            signal,
          })
          if (!response.ok && response.status !== 404)
            throw new Error('Bilder konnten nicht geprüft werden.')
          return {
            id: item.id,
            label: 'name' in item ? item.name : item.title,
            missing: response.status === 404,
          }
        }),
      )
      return { items, total: result.meta.total ?? result.items.length }
    },
    [kind, query, offset, version],
  )
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          variant={kind === 'artists' ? 'secondary' : 'ghost'}
          onClick={() => {
            setKind('artists')
            setOffset(0)
          }}
        >
          Künstlerbilder
        </Button>
        <Button
          size="sm"
          variant={kind === 'releases' ? 'secondary' : 'ghost'}
          onClick={() => {
            setKind('releases')
            setOffset(0)
          }}
        >
          Albumcover
        </Button>
      </div>
      <Input
        aria-label="Cover nach Namen suchen"
        placeholder="Name suchen …"
        value={query}
        onChange={(e) => {
          setQuery(e.target.value)
          setOffset(0)
        }}
      />
      <label className="flex gap-2 text-sm">
        <input
          type="checkbox"
          checked={missingOnly}
          onChange={(e) => setMissingOnly(e.target.checked)}
        />
        Auf dieser Seite nur fehlende lokale Bilder
      </label>
      {state.status === 'loading' && <ListSkeleton rows={3} />}{' '}
      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}{' '}
      {state.status === 'success' && (
        <>
          <div className="grid gap-3 lg:grid-cols-2">
            {state.data.items
              .filter((i) => !missingOnly || i.missing)
              .map((item) => (
                <div
                  key={item.id}
                  className="flex flex-wrap gap-3 rounded-lg border border-border p-3"
                >
                  <Cover
                    src={libraryArtwork(kind, item.id) + `?v=${version}`}
                    alt={item.label}
                    shape={kind === 'artists' ? 'circle' : 'square'}
                    className="size-16 shrink-0"
                  />
                  <div className="min-w-0 flex-1">
                    <p className="font-medium truncate">{item.label}</p>
                    <p className="text-xs text-muted-foreground mb-2">
                      {item.missing
                        ? 'Kein lokales Bild vorhanden'
                        : 'Lokales Bild vorhanden'}
                    </p>
                    {isAdmin && (
                      <ArtworkEditor
                        kind={kind}
                        id={item.id}
                        onSaved={() => setVersion((v) => v + 1)}
                      />
                    )}
                  </div>
                </div>
              ))}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - 20))}
            >
              Zurück
            </Button>
            <span className="text-xs text-muted-foreground">
              Seite {Math.floor(offset / 20) + 1} · {state.data.total} Einträge
            </span>
            <Button
              size="sm"
              variant="outline"
              disabled={offset + 20 >= state.data.total}
              onClick={() => setOffset(offset + 20)}
            >
              Weiter
            </Button>
          </div>
        </>
      )}
    </div>
  )
}
