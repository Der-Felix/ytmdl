import { useEffect, useMemo, useState } from 'react'
import {
  ClockIcon,
  FileTextIcon,
  Music2Icon,
  RefreshCwIcon,
  SparklesIcon,
  Trash2Icon,
} from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import {
  deleteTrackLyrics,
  refreshTrackLyrics,
  trackLyrics,
} from '@/lib/api/library'
import { joinArtists } from '@/lib/utils/format'
import { parseLrc } from '@/lib/utils/lrc'
import type { Track, TrackLyrics } from '@/types/api'

interface LyricsPanelProps {
  track: Track
  open: boolean
  onOpenChange: (open: boolean) => void
  onLyricsChanged?: (lyrics: TrackLyrics | null) => void
}

export function LyricsPanel({
  track,
  open,
  onOpenChange,
  onLyricsChanged,
}: LyricsPanelProps) {
  const [lyrics, setLyrics] = useState<TrackLyrics | null>(null)
  const [loading, setLoading] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [showTimestamps, setShowTimestamps] = useState(false)

  useEffect(() => {
    if (!open) {
      return
    }
    let cancelled = false
    setLoading(true)
    setError(null)

    trackLyrics(track.id)
      .then((data) => {
        if (!cancelled) {
          setLyrics(data)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Fehler beim Laden der Lyrics')
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false)
        }
      })

    return () => {
      cancelled = true
    }
  }, [track.id, open])

  const parsedLrc = useMemo(() => {
    if (!lyrics?.content) return null
    if (lyrics.state === 'available_synced' || lyrics.synced) {
      return parseLrc(lyrics.content)
    }
    return null
  }, [lyrics?.content, lyrics?.state, lyrics?.synced])

  const handleRefresh = async () => {
    setRefreshing(true)
    setError(null)
    try {
      const updated = await refreshTrackLyrics(track.id)
      setLyrics(updated)
      onLyricsChanged?.(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Aktualisieren der Lyrics')
    } finally {
      setRefreshing(false)
    }
  }

  const handleDelete = async () => {
    setDeleting(true)
    setError(null)
    try {
      await deleteTrackLyrics(track.id)
      setLyrics(null)
      onLyricsChanged?.(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Löschen der Lyrics')
    } finally {
      setDeleting(false)
    }
  }

  const artistText = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
        <DialogHeader>
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <DialogTitle className="text-xl">{track.title}</DialogTitle>
              {lyrics?.state === 'available_synced' && (
                <Badge variant="success" className="gap-1">
                  <SparklesIcon className="size-3" />
                  Synchronisiert
                </Badge>
              )}
              {lyrics?.state === 'available_plain' && (
                <Badge variant="default" className="gap-1">
                  <FileTextIcon className="size-3" />
                  Text
                </Badge>
              )}
              {lyrics?.state === 'instrumental' && (
                <Badge variant="neutral" className="gap-1">
                  <Music2Icon className="size-3" />
                  Instrumental
                </Badge>
              )}
              {lyrics?.state === 'not_found' && (
                <Badge variant="outline" className="gap-1 text-neutral-400">
                  Nicht gefunden
                </Badge>
              )}
              {lyrics?.state === 'unknown' && (
                <Badge variant="outline" className="gap-1 text-neutral-400">
                  Ungeprüft
                </Badge>
              )}
            </div>

            {parsedLrc?.isSynced && (
              <Button
                variant={showTimestamps ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs gap-1.5"
                onClick={() => setShowTimestamps(!showTimestamps)}
              >
                <ClockIcon className="size-3" />
                <span>Zeitstempel</span>
              </Button>
            )}
          </div>
          <DialogDescription>
            {artistText} {track.album ? `· ${track.album}` : ''}
            {lyrics?.provider && ` · Quelle: ${lyrics.provider.toUpperCase()}`}
          </DialogDescription>
        </DialogHeader>

        <div className="flex-1 overflow-y-auto min-h-[200px] max-h-[50vh] pr-2 my-2 rounded-xl bg-black/20 p-4 border border-white/5 text-sm leading-relaxed whitespace-pre-wrap selection:bg-primary/30">
          {loading ? (
            <div className="space-y-2 py-4">
              <Skeleton className="h-4 w-3/4" />
              <Skeleton className="h-4 w-1/2" />
              <Skeleton className="h-4 w-2/3" />
              <Skeleton className="h-4 w-4/5" />
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center h-full py-8 text-center text-muted-foreground">
              <p className="text-destructive mb-2">{error}</p>
              <Button size="sm" variant="outline" onClick={handleRefresh}>
                Erneut versuchen
              </Button>
            </div>
          ) : parsedLrc?.isSynced ? (
            <div className="space-y-1 font-sans">
              {parsedLrc.lines.map((line, idx) => (
                <div key={idx} className="flex items-start gap-3 py-0.5 group">
                  {showTimestamps && (
                    <span className="font-mono text-xs text-neutral-500 select-none pt-0.5 w-10 shrink-0">
                      {line.timestamp}
                    </span>
                  )}
                  <span className="text-foreground/90 leading-relaxed">{line.text || ' '}</span>
                </div>
              ))}
            </div>
          ) : lyrics?.content ? (
            <div className="text-foreground/90 font-sans leading-relaxed whitespace-pre-line">{lyrics.content}</div>
          ) : lyrics?.state === 'instrumental' ? (
            <div className="flex flex-col items-center justify-center h-full py-8 text-center text-muted-foreground">
              <Music2Icon className="size-8 mb-2 opacity-50 text-accent" />
              <p className="font-sans font-medium text-foreground">Instrumentaler Titel</p>
              <p className="text-xs font-sans mt-1 text-neutral-400">
                Für diesen Track ist hinterlegt, dass kein Gesang vorhanden ist.
              </p>
            </div>
          ) : lyrics?.state === 'not_found' ? (
            <div className="flex flex-col items-center justify-center h-full py-8 text-center text-muted-foreground">
              <p className="font-sans font-medium text-foreground">Keine Lyrics gefunden</p>
              <p className="text-xs font-sans mt-1 text-neutral-400">
                Bei den konfigurierten Providern (LRCLIB, YouTube Music) wurden keine Texte gefunden.
              </p>
              {lyrics?.checked_at && (
                <p className="text-xs font-sans mt-1 text-neutral-500">
                  Zuletzt geprüft: {new Date(lyrics.checked_at).toLocaleDateString('de-DE')}
                </p>
              )}
            </div>
          ) : (
            <div className="flex flex-col items-center justify-center h-full py-8 text-center text-muted-foreground">
              <p className="font-sans font-medium">Keine Lyrics vorhanden</p>
              <Button size="sm" variant="secondary" className="mt-3 gap-1.5" onClick={handleRefresh}>
                <RefreshCwIcon className="size-3.5" />
                Lyrics jetzt suchen
              </Button>
            </div>
          )}
        </div>

        <DialogFooter className="flex items-center justify-between sm:justify-between w-full">
          <div className="flex items-center gap-2">
            <Button
              size="sm"
              variant="outline"
              onClick={handleRefresh}
              disabled={loading || refreshing || deleting}
            >
              <RefreshCwIcon className={refreshing ? 'animate-spin size-3.5' : 'size-3.5'} />
              Aktualisieren
            </Button>

            {lyrics?.content && (
              <Button
                size="sm"
                variant="destructive"
                onClick={handleDelete}
                disabled={loading || refreshing || deleting}
              >
                <Trash2Icon className="size-3.5" />
                Löschen
              </Button>
            )}
          </div>

          <Button
            size="sm"
            variant="ghost"
            onClick={() => onOpenChange(false)}
          >
            Schließen
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
