import { useCallback, useMemo, useState } from 'react'
import {
  Heart,
  Play,
  Shuffle,
} from 'lucide-react'

import { TrackDetailDialog } from '@/components/music/TrackDetailDialog'
import { TracksTable } from '@/components/music/TracksTable'
import { Button } from '@/components/ui/button'
import { EmptyState, ErrorState, ListSkeleton } from '@/components/ui/state-view'
import { useAsync } from '@/hooks/useAsync'
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerActions } from '@/hooks/usePlayer'
import { listFavorites } from '@/lib/api/playlists'
import { Link, paths } from '@/lib/router'
import { formatDuration } from '@/lib/utils/format'

export function Favorites() {
  const { playAlbum } = usePlayerActions()
  const favorites = useOptionalFavorites()
  const favoriteIds = useMemo(() => favorites?.favoriteIds ?? new Set<string>(), [favorites?.favoriteIds])

  const { state, reload } = useAsync(
    (signal) => listFavorites(signal),
    [],
  )

  // Track detail modal state
  const [selectedTrackId, setSelectedTrackId] = useState<string | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)

  // Sorting state
  const [sort, setSort] = useState<string>('title')
  const [order, setOrder] = useState<'asc' | 'desc'>('asc')

  const handleSortChange = (newSort: string) => {
    if (sort === newSort) {
      setOrder((prev) => (prev === 'asc' ? 'desc' : 'asc'))
    } else {
      setSort(newSort)
      setOrder('asc')
    }
  }

  // Filter against favoriteIds so unfavorited tracks vanish or update cleanly
  const rawTracks = state.status === 'success' ? state.data : []
  const tracks = useMemo(() => {
    if (favoriteIds.size === 0) return rawTracks
    return rawTracks.filter((t) => favoriteIds.has(t.id))
  }, [rawTracks, favoriteIds])

  // Sorted tracks
  const sortedTracks = useMemo(() => {
    const list = [...tracks]
    list.sort((a, b) => {
      let valA: string | number = ''
      let valB: string | number = ''

      switch (sort) {
        case 'title':
          valA = a.title.toLowerCase()
          valB = b.title.toLowerCase()
          break
        case 'artist':
          valA = (a.artists?.[0] || a.album_artist || '').toLowerCase()
          valB = (b.artists?.[0] || b.album_artist || '').toLowerCase()
          break
        case 'album':
          valA = (a.album || '').toLowerCase()
          valB = (b.album || '').toLowerCase()
          break
        case 'duration':
          valA = a.duration_ms || 0
          valB = b.duration_ms || 0
          break
        case 'track_number':
          valA = a.track_number || 0
          valB = b.track_number || 0
          break
        default:
          return 0
      }

      if (valA < valB) return order === 'asc' ? -1 : 1
      if (valA > valB) return order === 'asc' ? 1 : -1
      return 0
    })
    return list
  }, [tracks, sort, order])

  // Calculate total duration
  const totalDurationMs = useMemo(() => {
    return tracks.reduce((acc, t) => acc + (t.duration_ms || 0), 0)
  }, [tracks])

  const handlePlayAll = useCallback(() => {
    if (sortedTracks.length === 0) return
    playAlbum(sortedTracks)
  }, [playAlbum, sortedTracks])

  const handlePlayShuffled = useCallback(() => {
    if (sortedTracks.length === 0) return
    const shuffled = [...sortedTracks].sort(() => Math.random() - 0.5)
    playAlbum(shuffled)
  }, [playAlbum, sortedTracks])

  return (
    <div className="space-y-6 pb-28">
      {/* Header */}
      <div className="flex flex-col md:flex-row md:items-end gap-6 p-6 rounded-3xl border border-white/5 bg-gradient-to-b from-rose-500/[0.07] to-white/[0.01]">
        <div className="size-32 sm:size-40 rounded-2xl bg-gradient-to-br from-rose-950/60 to-neutral-900 border border-rose-500/20 flex items-center justify-center shrink-0 shadow-2xl">
          <Heart className="size-16 sm:size-20 text-rose-500 fill-rose-500/30" />
        </div>

        <div className="flex-1 min-w-0 space-y-2">
          <span className="text-[11px] uppercase tracking-wider font-semibold text-rose-400">
            Sammlung
          </span>
          <h1 className="text-2xl sm:text-4xl font-extrabold tracking-tight text-white flex items-center gap-3">
            Lieblingstitel
          </h1>
          <p className="text-sm text-neutral-300 leading-relaxed max-w-xl">
            Deine persönlich markierten Favoriten auf einen Blick.
          </p>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pt-1 text-xs text-neutral-400 font-mono">
            <span>
              {tracks.length} {tracks.length === 1 ? 'Titel' : 'Titel'}
            </span>
            {tracks.length > 0 && (
              <>
                <span>•</span>
                <span>{formatDuration(totalDurationMs)}</span>
              </>
            )}
          </div>

          {tracks.length > 0 && (
            <div className="flex flex-wrap items-center gap-2.5 pt-3">
              <Button onClick={handlePlayAll} className="gap-2 shadow-lg shadow-primary/20">
                <Play className="size-4 fill-current" />
                Favoriten abspielen
              </Button>
              <Button variant="outline" onClick={handlePlayShuffled} className="gap-2">
                <Shuffle className="size-4" />
                Zufall
              </Button>
            </div>
          )}
        </div>
      </div>

      {/* Main Content */}
      {state.status === 'loading' && <ListSkeleton rows={5} />}

      {state.status === 'error' && (
        <ErrorState error={state.error} onRetry={reload} />
      )}

      {state.status === 'success' && tracks.length === 0 && (
        <EmptyState
          icon={<Heart className="text-rose-500" />}
          title="Noch keine Favoriten vorhanden"
          description="Klicke auf das Herz-Symbol bei Titeln in der Bibliothek oder im Player, um sie hier zu sammeln."
          action={
            <Link href={paths.library()}>
              <Button variant="outline" className="mt-2">
                Zur Bibliothek
              </Button>
            </Link>
          }
        />
      )}

      {state.status === 'success' && tracks.length > 0 && (
        <TracksTable
          tracks={sortedTracks}
          sort={sort}
          order={order}
          onSortChange={handleSortChange}
          onTrackSelect={(t) => {
            setSelectedTrackId(t.id)
            setDetailOpen(true)
          }}
          fullQueue={sortedTracks}
        />
      )}

      {/* Track Detail Dialog */}
      <TrackDetailDialog
        trackId={selectedTrackId}
        open={detailOpen}
        onOpenChange={setDetailOpen}
      />
    </div>
  )
}
