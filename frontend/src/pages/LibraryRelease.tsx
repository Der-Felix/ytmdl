import { useEffect, useMemo, useState } from 'react'
import {
  ArrowLeft,
  Disc3Icon,
  HardDriveIcon,
  ListPlus,
  Music2Icon,
  Play,
  Trash2Icon,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { TrackDetailDialog } from '@/components/music/TrackDetailDialog'
import { TracksTable } from '@/components/music/TracksTable'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useOptionalAuth } from '@/hooks/useAuth'
import { usePlayer } from '@/hooks/usePlayer'
import {
  deleteLibraryRelease,
  libraryReleaseDetail,
} from '@/lib/api/library'
import { Link, navigate, paths } from '@/lib/router'
import { formatBytes, formatDuration, pluralize } from '@/lib/utils/format'
import type { LibraryReleaseDetail, LibraryTrack } from '@/types/api'

interface LibraryReleaseProps {
  id: string
}

export function LibraryRelease({ id }: LibraryReleaseProps) {
  const auth = useOptionalAuth()
  const isAdmin = auth ? auth.isAdmin : true
  const { playAlbum, addToQueue } = usePlayer()
  const [detail, setDetail] = useState<LibraryReleaseDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selectedTrack, setSelectedTrack] = useState<LibraryTrack | null>(null)
  const [actionInProgress, setActionInProgress] = useState<string | null>(null)
  const [trackSort, setTrackSort] = useState<string>('track_number')
  const [trackOrder, setTrackOrder] = useState<string>('asc')

  const handleSortChange = (newSort: string) => {
    if (trackSort === newSort) {
      setTrackOrder((prev) => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setTrackSort(newSort)
      setTrackOrder('asc')
    }
  }

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)

    libraryReleaseDetail(id)
      .then((data) => {
        if (!cancelled) {
          setDetail(data)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Release konnte nicht geladen werden')
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
  }, [id])

  // Disc grouping
  const discGroups = useMemo(() => {
    if (!detail?.tracks) return []
    const map = new Map<number, LibraryTrack[]>()
    for (const t of detail.tracks) {
      const disc = t.disc_number || 1
      if (!map.has(disc)) map.set(disc, [])
      map.get(disc)!.push(t)
    }

    // Sort discs
    return Array.from(map.entries()).sort(([a], [b]) => a - b)
  }, [detail?.tracks])

  const totalDurationMS = useMemo(() => {
    if (!detail?.tracks) return 0
    return detail.tracks.reduce((acc, t) => acc + (t.duration_ms || 0), 0)
  }, [detail?.tracks])

  const handleDelete = async () => {
    if (!detail) return
    if (!confirm(`Möchtest du das gesamte Release „${detail.release.title}" und alle zugehörigen Dateien aus der Library löschen?`)) {
      return
    }
    setActionInProgress('delete')
    try {
      await deleteLibraryRelease(id)
      navigate(paths.library({ view: 'releases' }))
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Fehler beim Löschen des Releases')
      setActionInProgress(null)
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-6">
          <Skeleton className="size-36 rounded-2xl" />
          <div className="space-y-3">
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-4 w-48" />
            <Skeleton className="h-4 w-32" />
          </div>
        </div>
        <Skeleton className="h-64 w-full rounded-2xl" />
      </div>
    )
  }

  if (error || !detail) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center space-y-4">
        <Disc3Icon className="size-12 text-neutral-600" />
        <h2 className="text-xl font-semibold text-neutral-200">Release nicht gefunden</h2>
        <p className="text-sm text-neutral-400 max-w-md">
          {error || 'Dieses Release ist nicht in der lokalen Bibliothek vorhanden.'}
        </p>
        <Link href={paths.library({ view: 'releases' })}>
          <Button variant="secondary" size="sm" className="gap-2">
            <ArrowLeft className="size-4" />
            Zurück zur Bibliothek
          </Button>
        </Link>
      </div>
    )
  }

  const release = detail.release
  const artist = detail.artist
  const hasMultipleDiscs = discGroups.length > 1

  return (
    <div className="space-y-8">
      {/* Breadcrumb Navigation */}
      <div>
        <Link
          href={paths.library({ view: 'releases' })}
          className="inline-flex items-center gap-1.5 text-xs text-neutral-400 hover:text-neutral-200 transition-colors mb-4"
        >
          <ArrowLeft className="size-3.5" />
          <span>Bibliothek · Releases</span>
        </Link>

        {/* Release Header Banner */}
        <div className="flex flex-col sm:flex-row items-center sm:items-start gap-6 bg-neutral-900/40 border border-neutral-800/80 rounded-2xl p-6">
          <Cover
            src={release.cover_url}
            alt={release.title}
            className="size-36 sm:size-44 rounded-xl shadow-2xl shrink-0"
          />

          <div className="flex-1 space-y-3 text-center sm:text-left min-w-0">
            <div className="flex flex-wrap items-center justify-center sm:justify-start gap-2">
              <Badge variant="outline" className="text-xs uppercase font-mono tracking-wider">
                {release.release_type || 'Album'}
              </Badge>
              {release.year > 0 && (
                <Badge variant="neutral" className="text-xs">
                  {release.year}
                </Badge>
              )}
            </div>

            <h1 className="text-2xl sm:text-3xl font-bold font-heading text-neutral-100 truncate">
              {release.title}
            </h1>

            {artist ? (
              <p className="text-base text-neutral-300 font-medium">
                <Link
                  href={paths.libraryArtist(artist.id)}
                  className="hover:text-accent hover:underline transition-colors"
                >
                  {artist.name}
                </Link>
              </p>
            ) : (
              <p className="text-base text-neutral-300 font-medium">{release.album_artist}</p>
            )}

            <div className="flex flex-wrap items-center justify-center sm:justify-start gap-3 text-xs text-neutral-400 pt-1">
              <span className="flex items-center gap-1">
                <Music2Icon className="size-3.5" />
                {pluralize(detail.tracks.length, 'Track')}
              </span>
              {hasMultipleDiscs && (
                <>
                  <span>·</span>
                  <span className="flex items-center gap-1">
                    <Disc3Icon className="size-3.5" />
                    {pluralize(discGroups.length, 'Disc')}
                  </span>
                </>
              )}
              <span>·</span>
              <span>{formatDuration(totalDurationMS)}</span>
              <span>·</span>
              <span className="flex items-center gap-1">
                <HardDriveIcon className="size-3.5" />
                {formatBytes(detail.total_size_bytes)}
              </span>
            </div>
          </div>

          <div className="flex flex-wrap sm:flex-col items-center sm:items-end gap-2 shrink-0">
            <Button
              size="sm"
              onClick={() => detail?.tracks && playAlbum(detail.tracks)}
              className="gap-2 bg-primary text-primary-foreground font-semibold shadow-lg shadow-primary/25 hover:bg-primary/90"
            >
              <Play className="size-4 fill-current" />
              <span>Album abspielen</span>
            </Button>

            <Button
              variant="outline"
              size="sm"
              onClick={() => detail?.tracks && addToQueue(detail.tracks)}
              className="gap-1.5 text-xs border-white/10 hover:bg-white/5"
            >
              <ListPlus className="size-3.5" />
              <span>Zur Queue</span>
            </Button>

            {isAdmin && (
              <Button
                variant="destructive"
                size="sm"
                className="gap-1.5 text-xs mt-1"
                disabled={actionInProgress !== null}
                onClick={handleDelete}
              >
                <Trash2Icon className="size-3.5" />
                Release löschen
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Tracklist Section */}
      <div className="space-y-6">
        {hasMultipleDiscs ? (
          discGroups.map(([discNum, discTracks]) => (
            <div key={discNum} className="space-y-3">
              <h2 className="text-sm font-semibold text-neutral-400 font-heading uppercase tracking-wider flex items-center gap-2 border-b border-neutral-800 pb-2">
                <Disc3Icon className="size-4 text-accent" />
                Disc {discNum} ({discTracks.length} Titel)
              </h2>
              <TracksTable
                tracks={discTracks}
                sort={trackSort}
                order={trackOrder}
                onSortChange={handleSortChange}
                onTrackSelect={setSelectedTrack}
                showAlbum={false}
                showArtist={true}
              />
            </div>
          ))
        ) : (
          <div className="space-y-3">
            <h2 className="text-lg font-semibold text-neutral-200 font-heading border-b border-neutral-800 pb-2">
              Trackliste
            </h2>
            <TracksTable
              tracks={detail.tracks}
              sort={trackSort}
              order={trackOrder}
              onSortChange={handleSortChange}
              onTrackSelect={setSelectedTrack}
              showAlbum={false}
              showArtist={true}
            />
          </div>
        )}
      </div>

      {/* Track Detail Dialog */}
      <TrackDetailDialog
        trackId={selectedTrack?.id || null}
        open={selectedTrack !== null}
        onOpenChange={(open) => {
          if (!open) setSelectedTrack(null)
        }}
        isAdmin={isAdmin}
        onTrackUpdated={() => {
          libraryReleaseDetail(id).then(setDetail).catch(() => {})
        }}
        onTrackDeleted={() => {
          setSelectedTrack(null)
          libraryReleaseDetail(id).then(setDetail).catch(() => {})
        }}
      />
    </div>
  )
}
