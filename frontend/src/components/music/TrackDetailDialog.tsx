import { useEffect, useState } from 'react'
import {
  CalendarIcon,
  Disc3Icon,
  FileAudioIcon,
  FileTextIcon,
  HardDriveIcon,
  HashIcon,
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
  deleteLibraryTrack,
  libraryTrackDetail,
  redownloadLibraryTrack,
  retagLibraryTrack,
} from '@/lib/api/library'
import { formatBytes, formatDuration, joinArtists } from '@/lib/utils/format'
import type { LibraryTrackDetail } from '@/types/api'
import { LyricsPanel } from './LyricsPanel'

interface TrackDetailDialogProps {
  trackId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
  isAdmin?: boolean
  onTrackUpdated?: () => void
  onTrackDeleted?: (trackId: string) => void
}

export function TrackDetailDialog({
  trackId,
  open,
  onOpenChange,
  isAdmin = false,
  onTrackUpdated,
  onTrackDeleted,
}: TrackDetailDialogProps) {
  const [detail, setDetail] = useState<LibraryTrackDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [lyricsOpen, setLyricsOpen] = useState(false)
  const [actionInProgress, setActionInProgress] = useState<string | null>(null)

  useEffect(() => {
    if (!open || !trackId) {
      setDetail(null)
      return
    }

    let cancelled = false
    setLoading(true)
    setError(null)

    libraryTrackDetail(trackId)
      .then((data) => {
        if (!cancelled) {
          setDetail(data)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Fehler beim Laden der Track-Details')
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
  }, [trackId, open])

  const handleRetag = async () => {
    if (!trackId) return
    setActionInProgress('retag')
    try {
      await retagLibraryTrack(trackId)
      onTrackUpdated?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Neu-Taggen')
    } finally {
      setActionInProgress(null)
    }
  }

  const handleRedownload = async () => {
    if (!trackId) return
    setActionInProgress('redownload')
    try {
      await redownloadLibraryTrack(trackId)
      onTrackUpdated?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Neu-Herunterladen')
    } finally {
      setActionInProgress(null)
    }
  }

  const handleDelete = async () => {
    if (!trackId) return
    if (!confirm('Möchtest du diesen Track wirklich aus der Library löschen? Die Audiodatei wird gelöscht.')) {
      return
    }
    setActionInProgress('delete')
    try {
      await deleteLibraryTrack(trackId)
      onOpenChange(false)
      onTrackDeleted?.(trackId)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Löschen')
    } finally {
      setActionInProgress(null)
    }
  }

  const track = detail?.track
  const file = detail?.file
  const artistText = track?.artists && track.artists.length > 0 ? joinArtists(track.artists) : track?.album_artist || ''

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className="max-w-2xl max-h-[90vh] flex flex-col">
          <DialogHeader>
            <div className="flex flex-wrap items-center gap-2">
              <DialogTitle className="text-xl">
                {loading ? <Skeleton className="h-6 w-48" /> : track?.title || 'Track-Details'}
              </DialogTitle>
              {file?.codec && (
                <Badge variant="outline" className="font-mono uppercase text-xs">
                  {file.codec} {file.bitrate_kbps ? `${Math.round(file.bitrate_kbps)}k` : ''}
                </Badge>
              )}
              {track?.lyrics_state === 'available_synced' && (
                <Badge variant="success" className="gap-1 text-xs">
                  <SparklesIcon className="size-3" />
                  Synced
                </Badge>
              )}
              {track?.lyrics_state === 'available_plain' && (
                <Badge variant="default" className="gap-1 text-xs">
                  <FileTextIcon className="size-3" />
                  Plain
                </Badge>
              )}
              {track?.lyrics_state === 'instrumental' && (
                <Badge variant="neutral" className="gap-1 text-xs">
                  <Music2Icon className="size-3" />
                  Instrumental
                </Badge>
              )}
            </div>
            <DialogDescription>
              {loading ? (
                <Skeleton className="h-4 w-64 mt-1" />
              ) : (
                `${artistText}${track?.album ? ` · ${track.album}` : ''}`
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto min-h-[220px] max-h-[55vh] py-2 space-y-4">
            {loading ? (
              <div className="space-y-4 py-2">
                <Skeleton className="h-20 w-full rounded-xl" />
                <Skeleton className="h-24 w-full rounded-xl" />
                <Skeleton className="h-20 w-full rounded-xl" />
              </div>
            ) : error ? (
              <div className="text-center py-8 text-destructive">{error}</div>
            ) : track ? (
              <>
                {/* Track Metadata Grid */}
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 bg-neutral-900/50 p-3.5 rounded-xl border border-neutral-800/80 text-sm">
                  <div>
                    <div className="text-xs text-neutral-400 flex items-center gap-1 mb-1">
                      <HashIcon className="size-3" /> Track
                    </div>
                    <div className="font-medium text-neutral-200">
                      {track.track_number || '–'}{track.track_total ? ` / ${track.track_total}` : ''}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-neutral-400 flex items-center gap-1 mb-1">
                      <Disc3Icon className="size-3" /> Disc
                    </div>
                    <div className="font-medium text-neutral-200">
                      {track.disc_number || '1'}{track.disc_total ? ` / ${track.disc_total}` : ''}
                    </div>
                  </div>
                  <div>
                    <div className="text-xs text-neutral-400 flex items-center gap-1 mb-1">
                      <CalendarIcon className="size-3" /> Jahr
                    </div>
                    <div className="font-medium text-neutral-200">{track.year || '–'}</div>
                  </div>
                  <div>
                    <div className="text-xs text-neutral-400 mb-1">Dauer</div>
                    <div className="font-medium text-neutral-200 font-mono">
                      {formatDuration(track.duration_ms)}
                    </div>
                  </div>
                </div>

                {/* File Information */}
                <div className="bg-neutral-900/50 p-3.5 rounded-xl border border-neutral-800/80 space-y-2.5">
                  <div className="text-xs font-semibold text-neutral-400 uppercase tracking-wider flex items-center gap-1.5">
                    <FileAudioIcon className="size-3.5" /> Audiodatei
                  </div>
                  {file ? (
                    <div className="space-y-2 text-xs">
                      <div className="font-mono text-neutral-300 bg-black/30 p-2 rounded border border-neutral-800 break-all select-all">
                        {file.path}
                      </div>
                      <div className="grid grid-cols-2 sm:grid-cols-4 gap-2 text-neutral-300 pt-1">
                        <div>
                          <span className="text-neutral-500">Größe:</span> {formatBytes(file.size_bytes)}
                        </div>
                        <div>
                          <span className="text-neutral-500">Format:</span> {file.container || file.codec || '–'}
                        </div>
                        <div>
                          <span className="text-neutral-500">Rate:</span> {file.sample_rate ? `${(file.sample_rate / 1000).toFixed(1)} kHz` : '–'}
                        </div>
                        <div>
                          <span className="text-neutral-500">Kanäle:</span> {file.channels === 2 ? 'Stereo' : file.channels ? `${file.channels}ch` : '–'}
                        </div>
                      </div>
                    </div>
                  ) : (
                    <div className="text-xs text-neutral-500">Keine Datei mit diesem Track verknüpft.</div>
                  )}
                </div>

                {/* Technical Tags & Lyrics Info */}
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {/* ISRC & Provider */}
                  <div className="bg-neutral-900/50 p-3 rounded-xl border border-neutral-800/80 text-xs space-y-1.5">
                    <div className="font-semibold text-neutral-400 uppercase tracking-wider">Identifikatoren</div>
                    <div>
                      <span className="text-neutral-500">ISRC:</span>{' '}
                      <span className="font-mono text-neutral-300">{track.isrc || '–'}</span>
                    </div>
                    <div>
                      <span className="text-neutral-500">Quelle:</span>{' '}
                      <span className="text-neutral-300 capitalize">{track.source_provider || '–'}</span>
                    </div>
                    {detail.release?.release_type && (
                      <div>
                        <span className="text-neutral-500">Release-Typ:</span>{' '}
                        <span className="text-neutral-300 uppercase">{detail.release.release_type}</span>
                      </div>
                    )}
                  </div>

                  {/* Lyrics Info */}
                  <div className="bg-neutral-900/50 p-3 rounded-xl border border-neutral-800/80 text-xs space-y-1.5 flex flex-col justify-between">
                    <div>
                      <div className="font-semibold text-neutral-400 uppercase tracking-wider mb-1 flex items-center justify-between">
                        <span>Lyrics-Status</span>
                        {track.lyrics_provider && (
                          <span className="text-neutral-500 font-mono uppercase">{track.lyrics_provider}</span>
                        )}
                      </div>
                      <div className="text-neutral-300">
                        {track.lyrics_state === 'available_synced' && 'Synchronisierte LRC-Lyrics gespeichert.'}
                        {track.lyrics_state === 'available_plain' && 'Unsynchronisierter Text gespeichert.'}
                        {track.lyrics_state === 'instrumental' && 'Als rein instrumental markiert.'}
                        {track.lyrics_state === 'not_found' && 'Keine Lyrics bei Providern gefunden.'}
                        {(!track.lyrics_state || track.lyrics_state === 'unknown') && 'Noch nicht gesucht.'}
                      </div>
                      {detail.lyrics_path && (
                        <div className="text-neutral-500 font-mono text-[11px] truncate mt-1 select-all">
                          {detail.lyrics_path}
                        </div>
                      )}
                    </div>

                    <Button
                      variant="secondary"
                      size="sm"
                      className="w-full mt-2 h-7 text-xs gap-1.5"
                      onClick={() => setLyricsOpen(true)}
                    >
                      <FileTextIcon className="size-3" />
                      Lyrics ansehen / verwalten
                    </Button>
                  </div>
                </div>
              </>
            ) : null}
          </div>

          <DialogFooter className="flex items-center justify-between sm:justify-between w-full pt-2 border-t border-neutral-800">
            <div className="flex items-center gap-2">
              <Button
                variant="secondary"
                size="sm"
                className="gap-1.5"
                disabled={!track || actionInProgress !== null}
                onClick={handleRetag}
              >
                <RefreshCwIcon className={`size-3.5 ${actionInProgress === 'retag' ? 'animate-spin' : ''}`} />
                Retag
              </Button>
              <Button
                variant="secondary"
                size="sm"
                className="gap-1.5"
                disabled={!track || actionInProgress !== null}
                onClick={handleRedownload}
              >
                <HardDriveIcon className="size-3.5" />
                Neu laden
              </Button>
            </div>

            <div className="flex items-center gap-2">
              {isAdmin && (
                <Button
                  variant="destructive"
                  size="sm"
                  className="gap-1.5"
                  disabled={!track || actionInProgress !== null}
                  onClick={handleDelete}
                >
                  <Trash2Icon className="size-3.5" />
                  Löschen
                </Button>
              )}
              <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)}>
                Schließen
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {track && (
        <LyricsPanel
          track={track}
          open={lyricsOpen}
          onOpenChange={setLyricsOpen}
          onLyricsChanged={() => {
            if (trackId) libraryTrackDetail(trackId).then(setDetail).catch(() => {})
          }}
        />
      )}
    </>
  )
}
