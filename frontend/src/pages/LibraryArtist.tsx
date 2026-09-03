import { useEffect, useMemo, useState } from 'react'
import {
  ArrowLeft,
  Disc3Icon,
  HardDriveIcon,
  Music2Icon,
  Play,
  Shuffle,
  UserIcon,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { ReleaseCard } from '@/components/music/ReleaseCard'
import { TrackDetailDialog } from '@/components/music/TrackDetailDialog'
import { TracksTable } from '@/components/music/TracksTable'
import { SubscribeControl } from '@/components/subscriptions/SubscribeControl'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { useOptionalAuth } from '@/hooks/useAuth'
import { usePlayer } from '@/hooks/usePlayer'
import { libraryArtistDetail } from '@/lib/api/library'
import { Link, paths } from '@/lib/router'
import { formatBytes, pluralize } from '@/lib/utils/format'
import type { LibraryArtistDetail, LibraryTrack } from '@/types/api'

interface LibraryArtistProps {
  id: string
}

export function LibraryArtist({ id }: LibraryArtistProps) {
  const auth = useOptionalAuth()
  const isAdmin = auth ? auth.isAdmin : true
  const { playArtist } = usePlayer()
  const [detail, setDetail] = useState<LibraryArtistDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [releaseTypeFilter, setReleaseTypeFilter] = useState<string>('all')
  const [trackSort, setTrackSort] = useState<string>('recent')
  const [trackOrder, setTrackOrder] = useState<string>('desc')
  const [selectedTrack, setSelectedTrack] = useState<LibraryTrack | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)

    libraryArtistDetail(id)
      .then((data) => {
        if (!cancelled) {
          setDetail(data)
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Künstler konnte nicht geladen werden')
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

  const filteredReleases = useMemo(() => {
    if (!detail?.releases) return []
    if (releaseTypeFilter === 'all') return detail.releases
    return detail.releases.filter((r) => r.release_type === releaseTypeFilter)
  }, [detail?.releases, releaseTypeFilter])

  const sortedTracks = useMemo(() => {
    if (!detail?.tracks) return []
    const list = [...detail.tracks]
    list.sort((a, b) => {
      let cmp = 0
      switch (trackSort) {
        case 'title':
          cmp = a.title.localeCompare(b.title)
          break
        case 'album':
          cmp = (a.album || '').localeCompare(b.album || '')
          break
        case 'year':
          cmp = (a.year || 0) - (b.year || 0)
          break
        case 'duration':
          cmp = (a.duration_ms || 0) - (b.duration_ms || 0)
          break
        case 'track_number':
          cmp = (a.track_number || 0) - (b.track_number || 0)
          break
        case 'recent':
        default:
          cmp = new Date(a.created_at || 0).getTime() - new Date(b.created_at || 0).getTime()
          break
      }
      return trackOrder === 'asc' ? cmp : -cmp
    })
    return list
  }, [detail?.tracks, trackSort, trackOrder])

  const handleTrackSortChange = (field: string) => {
    if (trackSort === field) {
      setTrackOrder(trackOrder === 'asc' ? 'desc' : 'asc')
    } else {
      setTrackSort(field)
      setTrackOrder(field === 'recent' || field === 'year' || field === 'duration' ? 'desc' : 'asc')
    }
  }

  if (loading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center gap-4">
          <Skeleton className="size-28 rounded-full" />
          <div className="space-y-2">
            <Skeleton className="h-8 w-64" />
            <Skeleton className="h-4 w-48" />
          </div>
        </div>
        <Skeleton className="h-48 w-full rounded-2xl" />
      </div>
    )
  }

  if (error || !detail) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center space-y-4">
        <UserIcon className="size-12 text-neutral-600" />
        <h2 className="text-xl font-semibold text-neutral-200">Künstler nicht gefunden</h2>
        <p className="text-sm text-neutral-400 max-w-md">
          {error || 'Dieser Künstler ist nicht in der lokalen Bibliothek vorhanden.'}
        </p>
        <Link href={paths.library({ view: 'artists' })}>
          <Button variant="secondary" size="sm" className="gap-2">
            <ArrowLeft className="size-4" />
            Zurück zur Bibliothek
          </Button>
        </Link>
      </div>
    )
  }

  const artist = detail.artist
  const releaseTypes = Array.from(new Set(detail.releases.map((r) => r.release_type).filter(Boolean)))

  return (
    <div className="space-y-8">
      {/* Header & Breadcrumb */}
      <div>
        <Link
          href={paths.library({ view: 'artists' })}
          className="inline-flex items-center gap-1.5 text-xs text-neutral-400 hover:text-neutral-200 transition-colors mb-4"
        >
          <ArrowLeft className="size-3.5" />
          <span>Bibliothek · Künstler</span>
        </Link>

        <div className="flex flex-col md:flex-row md:items-center justify-between gap-6 bg-neutral-900/40 border border-neutral-800/80 rounded-2xl p-6">
          <div className="flex flex-col sm:flex-row items-center sm:items-start md:items-center gap-5 text-center sm:text-left">
            <Cover
              src={artist.image_url}
              alt={artist.name}
              shape="circle"
              className="size-28 shadow-xl shrink-0"
            />
            <div className="space-y-2">
              <h1 className="text-2xl sm:text-3xl font-bold font-heading text-neutral-100">{artist.name}</h1>
              <div className="flex flex-wrap items-center justify-center sm:justify-start gap-3 text-xs text-neutral-400">
                <span className="flex items-center gap-1">
                  <Disc3Icon className="size-3.5" />
                  {pluralize(detail.release_count, 'Release', 'Releases')}
                </span>
                <span>·</span>
                <span className="flex items-center gap-1">
                  <Music2Icon className="size-3.5" />
                  {pluralize(detail.track_count, 'Track')}
                </span>
                <span>·</span>
                <span className="flex items-center gap-1">
                  <HardDriveIcon className="size-3.5" />
                  {formatBytes(detail.total_size_bytes)}
                </span>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-center sm:justify-end gap-2 shrink-0">
            {detail.tracks.length > 0 && (
              <>
                <Button
                  size="sm"
                  onClick={() => playArtist(detail.tracks, false)}
                  className="gap-2 bg-primary text-primary-foreground font-semibold shadow-lg shadow-primary/25 hover:bg-primary/90"
                >
                  <Play className="size-4 fill-current" />
                  <span>Künstler abspielen</span>
                </Button>

                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => playArtist(detail.tracks, true)}
                  className="gap-1.5 text-xs border-white/10 hover:bg-white/5"
                  title="Zufallswiedergabe aller Titel"
                >
                  <Shuffle className="size-3.5" />
                  <span>Mischen</span>
                </Button>
              </>
            )}

            <SubscribeControl artist={artist} />
          </div>
        </div>
      </div>

      {/* Local Releases Section */}
      <div className="space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-neutral-800 pb-3">
          <h2 className="text-lg font-semibold text-neutral-200 font-heading">
            Lokale Veröffentlichungen ({detail.release_count})
          </h2>

          {releaseTypes.length > 1 && (
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                variant={releaseTypeFilter === 'all' ? 'default' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => setReleaseTypeFilter('all')}
              >
                Alle ({detail.releases.length})
              </Button>
              {releaseTypes.map((t) => {
                const count = detail.releases.filter((r) => r.release_type === t).length
                return (
                  <Button
                    key={t}
                    variant={releaseTypeFilter === t ? 'default' : 'ghost'}
                    size="sm"
                    className="h-7 text-xs capitalize"
                    onClick={() => setReleaseTypeFilter(t)}
                  >
                    {t} ({count})
                  </Button>
                )
              })}
            </div>
          )}
        </div>

        {filteredReleases.length === 0 ? (
          <div className="py-8 text-center text-sm text-neutral-500">
            Keine Veröffentlichungen für diesen Filter vorhanden.
          </div>
        ) : (
          <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
            {filteredReleases.map((rel) => (
              <ReleaseCard key={rel.id} release={rel} isLocal />
            ))}
          </div>
        )}
      </div>

      {/* Local Tracks Section */}
      <div className="space-y-4 pt-4">
        <div className="flex items-center justify-between border-b border-neutral-800 pb-3">
          <h2 className="text-lg font-semibold text-neutral-200 font-heading">
            Alle Titel ({detail.track_count})
          </h2>
        </div>

        <TracksTable
          tracks={sortedTracks}
          sort={trackSort}
          order={trackOrder}
          onSortChange={handleTrackSortChange}
          onTrackSelect={setSelectedTrack}
          showArtist={false}
          showAlbum={true}
        />
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
          libraryArtistDetail(id).then(setDetail).catch(() => {})
        }}
        onTrackDeleted={() => {
          setSelectedTrack(null)
          libraryArtistDetail(id).then(setDetail).catch(() => {})
        }}
      />
    </div>
  )
}
