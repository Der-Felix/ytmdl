import { useState } from 'react'
import { Radio, RefreshCw, Play, ListPlus } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Cover } from '@/components/music/Cover'
import { useAsync } from '@/hooks/useAsync'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { libraryGenres } from '@/lib/api/library'
import { localRadio } from '@/lib/api/libraryTools'
import { libraryArtwork } from '@/lib/artwork'
import type { LibraryTrack } from '@/types/api'
export function SongRadioPanel() {
  const { currentTrack } = usePlayerState()
  const { playAlbum, addToQueue } = usePlayerActions()
  const genres = useAsync((signal) => libraryGenres(signal), [])
  const [genre, setGenre] = useState(''),
    [seed, setSeed] = useState(false),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(''),
    [message, setMessage] = useState(''),
    [tracks, setTracks] = useState<LibraryTrack[]>([])
  const prepare = async () => {
    if (busy) return
    setBusy(true)
    setError('')
    setMessage('')
    try {
      const result = await localRadio(
        seed ? currentTrack?.id : undefined,
        genre,
        crypto.randomUUID(),
      )
      setTracks(result)
      if (!result.length)
        setMessage(
          'Keine passenden lokalen Titel verfügbar. Probiere ein anderes Genre.',
        )
    } catch (e) {
      setError(
        e instanceof Error
          ? e.message
          : 'Radio konnte nicht zusammengestellt werden.',
      )
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="space-y-4">
      <div className="rounded-2xl border border-border bg-gradient-to-br from-primary/15 to-muted/20 p-5 space-y-3">
        <Radio className="size-8 text-primary" />
        <h3 className="text-xl font-semibold">Dein Song-Radio</h3>
        <p className="text-sm text-muted-foreground">
          Ein Mix aus deiner vorhandenen Musik: verwandte Künstler und Genres,
          deine Favoriten und dein Hörverlauf. Ohne zusätzliche Downloads.
          Kürzlich gehörte Songs rücken nach hinten.
        </p>
        <div className="flex flex-wrap gap-3 items-center">
          <label className="text-sm">
            Genre{' '}
            <select
              aria-label="Radio-Genre"
              className="ml-2 rounded border border-border bg-background p-2"
              value={genre}
              disabled={busy}
              onChange={(e) => {
                setGenre(e.target.value)
                setTracks([])
              }}
            >
              <option value="">Alle Genres</option>
              {genres.state.status === 'success' &&
                genres.state.data.map((g) => <option key={g}>{g}</option>)}
            </select>
          </label>
          <label className="flex gap-2 text-sm items-center">
            <input
              type="checkbox"
              checked={seed}
              disabled={busy || !currentTrack}
              onChange={(e) => {
                setSeed(e.target.checked)
                setTracks([])
              }}
            />
            Zum aktuellen Titel{currentTrack ? `: ${currentTrack.title}` : ''}
          </label>
        </div>
        {genres.state.status === 'error' && (
          <p role="alert" className="text-sm">
            Genres konnten nicht geladen werden.{' '}
            <Button variant="ghost" size="sm" onClick={() => genres.reload()}>
              Erneut versuchen
            </Button>
          </p>
        )}
        <Button disabled={busy} onClick={() => void prepare()}>
          <RefreshCw className="size-4" />
          {busy ? 'Mix wird erstellt …' : 'Neuen Mix zusammenstellen'}
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
      {!!tracks.length && (
        <>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => playAlbum(tracks)}>
              <Play className="size-4" />
              {tracks.length} Titel abspielen
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                addToQueue(tracks)
                setMessage('Radio-Mix zur Warteschlange hinzugefügt.')
              }}
            >
              <ListPlus className="size-4" />
              Zur Warteschlange
            </Button>
          </div>
          <div className="divide-y divide-border">
            {tracks.map((track, i) => (
              <div className="flex items-center gap-3 py-3" key={track.id}>
                <Cover
                  src={libraryArtwork('tracks', track.id)}
                  fallbackSrc={track.cover_url}
                  alt=""
                  className="size-10 shrink-0"
                />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{track.title}</p>
                  <p className="truncate text-xs text-muted-foreground">
                    {track.artists.join(' · ')}
                  </p>
                </div>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`${track.title} im Radio abspielen`}
                  onClick={() => playAlbum(tracks, i)}
                >
                  Abspielen
                </Button>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
