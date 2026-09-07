import { useCallback, useEffect, useState } from 'react'
import {
  CheckCircle2Icon,
  Disc3Icon,
  FolderSyncIcon,
  HardDriveIcon,
  Heart,
  LibraryIcon,
  Music2Icon,
  RefreshCwIcon,
  ShieldCheckIcon,
  SparklesIcon,
  UserIcon,
  WrenchIcon,
  X,
} from 'lucide-react'

import { IntegrityPanel } from '@/components/library/IntegrityPanel'
import { LibrarySearchField } from '@/components/library/LibrarySearchField'
import { ArtistCard } from '@/components/music/ArtistCard'
import { ReleaseCard } from '@/components/music/ReleaseCard'
import { TrackDetailDialog } from '@/components/music/TrackDetailDialog'
import { TracksTable } from '@/components/music/TracksTable'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Pagination } from '@/components/ui/pagination'
import { Panel } from '@/components/ui/panel'
import { EmptyState, ErrorState } from '@/components/ui/state-view'
import {
  deleteLibraryOrphanIssue,
  deleteLibraryRelease,
  deleteLibraryTrack,
  getCompatibilityReport,
  libraryArtists,
  libraryReleases,
  libraryStats,
  libraryTracks,
  reorganizeLibrary,
  startLibraryScan,
  startLyricsBackfill,
} from '@/lib/api/library'
import { useOptionalAuth } from '@/hooks/useAuth'
import { navigate, useLocation } from '@/lib/router'
import { cn } from '@/lib/utils'
import {
  formatBytes,
  pluralize,
} from '@/lib/utils/format'
import type {
  CompatReport,
  LibraryArtist,
  LibraryRelease,
  LibraryStats,
  LibraryTrack,
} from '@/types/api'

type LibraryTab = 'releases' | 'tracks' | 'artists' | 'maintenance'

export function Library() {
  const auth = useOptionalAuth()
  const isAdmin = auth ? auth.isAdmin : true
  const location = useLocation()
  const params = location.params

  // URL state
  const currentView = (params.get('view') as LibraryTab) || 'releases'
  const currentQ = params.get('q') || ''
  const currentSort = params.get('sort') || ''
  const currentOrder = params.get('order') || ''
  const currentType = params.get('type') || ''
  const currentLyrics = params.get('lyrics') || ''
  const currentYear = params.get('year') ? parseInt(params.get('year')!, 10) : undefined
  const currentFavorite = params.get('favorite') === 'true' || params.get('favorite') === '1'
  const currentArtistId = params.get('artist') || ''
  const currentReleaseId = params.get('release') || ''
  const currentPage = Math.max(1, parseInt(params.get('page') || '1', 10))
  const currentTrackId = params.get('track') || null

  // Active view state (synced with URL)
  const [view, setView] = useState<LibraryTab>(currentView)

  // Overall Stats
  const [stats, setStats] = useState<LibraryStats | null>(null)

  // Tab Data States
  const [releases, setReleases] = useState<LibraryRelease[]>([])
  const [releasesTotal, setReleasesTotal] = useState(0)
  const [releasesLoading, setReleasesLoading] = useState(false)

  const [tracks, setTracks] = useState<LibraryTrack[]>([])
  const [tracksTotal, setTracksTotal] = useState(0)
  const [tracksLoading, setTracksLoading] = useState(false)

  const [artists, setArtists] = useState<LibraryArtist[]>([])
  const [artistsTotal, setArtistsTotal] = useState(0)
  const [artistsLoading, setArtistsLoading] = useState(false)

  const [fetchError, setFetchError] = useState<string | null>(null)

  // Track Detail dialog
  const [activeTrackId, setActiveTrackId] = useState<string | null>(currentTrackId)

  // Maintenance & Scan State
  const [isScanning, setIsScanning] = useState(false)
  const [actionFeedback, setActionFeedback] = useState<string | null>(null)

  // Deletion Confirmation Dialog
  const [pendingDelete, setPendingDelete] = useState<{
    type: 'track' | 'release' | 'orphan'
    id: string
    title: string
    description: string
  } | null>(null)
  const [isDeleting, setIsDeleting] = useState(false)

  // Compatibility & Reorganize
  const [compatReport, setCompatReport] = useState<CompatReport | null>(null)
  const [isCheckingCompat, setIsCheckingCompat] = useState(false)
  const [compatModalOpen, setCompatModalOpen] = useState(false)
  const [selectedCompatIssues, setSelectedCompatIssues] = useState<string[]>([])
  const [isReorganizing, setIsReorganizing] = useState(false)

  // Lyrics Backfill
  const [isBackfilling, setIsBackfilling] = useState(false)

  // Update URL helper
  const updateUrl = useCallback(
    (newParams: Record<string, string | number | boolean | undefined>, replace = false) => {
      const sp = new URLSearchParams(location.params)
      for (const [k, v] of Object.entries(newParams)) {
        if (v === undefined || v === '' || v === false || (k === 'page' && v === 1)) {
          sp.delete(k)
        } else {
          sp.set(k, String(v))
        }
      }
      const qs = sp.toString()
      navigate(qs ? `/library?${qs}` : '/library', { replace })
    },
    [location.params],
  )

  const handleTabChange = (t: LibraryTab) => {
    setView(t)
    updateUrl({ view: t, page: 1 })
  }

  // Load Library Stats on mount
  useEffect(() => {
    let cancelled = false
    libraryStats()
      .then((data) => {
        if (!cancelled) setStats(data)
      })
      .catch(() => {})

    return () => {
      cancelled = true
    }
  }, [])

  // Synchronize view when URL changes
  useEffect(() => {
    setView(currentView)
  }, [currentView])

  // Synchronize activeTrackId when URL track param changes
  useEffect(() => {
    setActiveTrackId(currentTrackId)
  }, [currentTrackId])

  // Fetch Tab Data based on current URL params
  useEffect(() => {
    const controller = new AbortController()
    const signal = controller.signal

    setFetchError(null)

    if (view === 'releases') {
      setReleasesLoading(true)
      const pageSize = 24
      const offset = (currentPage - 1) * pageSize

      libraryReleases({
        q: currentQ,
        artistId: currentArtistId,
        releaseType: currentType,
        year: currentYear,
        sort: currentSort || 'recent',
        order: currentOrder || (currentSort === 'year' || !currentSort ? 'desc' : 'asc'),
        limit: pageSize,
        offset,
        signal,
      })
        .then((res) => {
          if (!signal.aborted) {
            setReleases(res.items)
            setReleasesTotal(res.meta.total ?? res.items.length)
          }
        })
        .catch((err) => {
          if (!signal.aborted) {
            setFetchError(err instanceof Error ? err.message : 'Fehler beim Laden der Releases')
          }
        })
        .finally(() => {
          if (!signal.aborted) setReleasesLoading(false)
        })
    } else if (view === 'tracks') {
      setTracksLoading(true)
      const pageSize = 50
      const offset = (currentPage - 1) * pageSize
      const effectiveSort = currentSort || (currentQ ? 'relevance' : 'recent')
      const defaultOrder =
        effectiveSort === 'relevance'
          ? 'asc'
          : effectiveSort === 'year' || effectiveSort === 'duration' || effectiveSort === 'recent'
            ? 'desc'
            : 'asc'

      libraryTracks({
        q: currentQ,
        artistId: currentArtistId,
        releaseId: currentReleaseId,
        lyricsState: currentLyrics,
        year: currentYear,
        favorite: currentFavorite,
        sort: effectiveSort,
        order: currentOrder || defaultOrder,
        limit: pageSize,
        offset,
        signal,
      })
        .then((res) => {
          if (!signal.aborted) {
            setTracks(res.items)
            setTracksTotal(res.meta.total ?? res.items.length)
          }
        })
        .catch((err) => {
          if (!signal.aborted) {
            setFetchError(err instanceof Error ? err.message : 'Fehler beim Laden der Tracks')
          }
        })
        .finally(() => {
          if (!signal.aborted) setTracksLoading(false)
        })
    } else if (view === 'artists') {
      setArtistsLoading(true)
      const pageSize = 24
      const offset = (currentPage - 1) * pageSize

      libraryArtists({
        q: currentQ,
        sort: currentSort || 'name',
        order: currentOrder || (currentSort === 'recent' || currentSort === 'release_count' ? 'desc' : 'asc'),
        limit: pageSize,
        offset,
        signal,
      })
        .then((res) => {
          if (!signal.aborted) {
            setArtists(res.items)
            setArtistsTotal(res.meta.total ?? res.items.length)
          }
        })
        .catch((err) => {
          if (!signal.aborted) {
            setFetchError(err instanceof Error ? err.message : 'Fehler beim Laden der Künstler')
          }
        })
        .finally(() => {
          if (!signal.aborted) setArtistsLoading(false)
        })
    }

    return () => {
      controller.abort()
    }
  }, [
    view,
    currentQ,
    currentSort,
    currentOrder,
    currentType,
    currentLyrics,
    currentYear,
    currentFavorite,
    currentArtistId,
    currentReleaseId,
    currentPage,
  ])

  // Scan handler
  const handleStartScan = async () => {
    setIsScanning(true)
    try {
      await startLibraryScan()
      const st = await libraryStats()
      setStats(st)
      setActionFeedback('Bibliotheks-Scan erfolgreich abgeschlossen.')
    } catch (err) {
      setActionFeedback(err instanceof Error ? err.message : 'Fehler beim Scan')
    } finally {
      setIsScanning(false)
    }
  }

  // Compatibility check
  const handleCheckCompat = async () => {
    setIsCheckingCompat(true)
    try {
      const report = await getCompatibilityReport()
      setCompatReport(report)
      setCompatModalOpen(true)
    } catch (err) {
      setActionFeedback(err instanceof Error ? err.message : 'Fehler beim Prüfen der Kompatibilität')
    } finally {
      setIsCheckingCompat(false)
    }
  }

  // Safe Reorganize
  const handleReorganize = async () => {
    setIsReorganizing(true)
    try {
      const res = await reorganizeLibrary({
        confirm: true,
        issue_ids: selectedCompatIssues.length > 0 ? selectedCompatIssues : (compatReport?.issues.map((i) => i.id) ?? []),
      })
      setActionFeedback(`Reorganisation abgeschlossen: ${res.moved} Dateien verschoben.`)
      setCompatModalOpen(false)
      const st = await libraryStats()
      setStats(st)
    } catch (err) {
      setActionFeedback(err instanceof Error ? err.message : 'Fehler bei der Reorganisation')
    } finally {
      setIsReorganizing(false)
    }
  }

  // Lyrics Backfill
  const handleStartBackfill = async () => {
    setIsBackfilling(true)
    try {
      await startLyricsBackfill()
      setActionFeedback('Lyrics-Backfill im Hintergrund gestartet.')
    } catch (err) {
      setActionFeedback(err instanceof Error ? err.message : 'Fehler beim Backfill')
    } finally {
      setIsBackfilling(false)
    }
  }

  // Delete Action Execution
  const handleExecuteDelete = async () => {
    if (!pendingDelete) return
    setIsDeleting(true)
    try {
      if (pendingDelete.type === 'track') {
        await deleteLibraryTrack(pendingDelete.id)
        setTracks((prev) => prev.filter((t) => t.id !== pendingDelete.id))
        setTracksTotal((prev) => Math.max(0, prev - 1))
      } else if (pendingDelete.type === 'release') {
        await deleteLibraryRelease(pendingDelete.id)
        setReleases((prev) => prev.filter((r) => r.id !== pendingDelete.id))
        setReleasesTotal((prev) => Math.max(0, prev - 1))
      } else if (pendingDelete.type === 'orphan') {
        await deleteLibraryOrphanIssue(pendingDelete.id)
      }
      setPendingDelete(null)
      const st = await libraryStats()
      setStats(st)
      setActionFeedback('Erfolgreich gelöscht.')
    } catch (err) {
      setActionFeedback(err instanceof Error ? err.message : 'Fehler beim Löschen')
    } finally {
      setIsDeleting(false)
    }
  }

  return (
    <div className="space-y-8">
      {/* Header Banner & Stats */}
      <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-6 bg-neutral-900/40 border border-neutral-800/80 rounded-2xl p-6">
        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <h1 className="text-2xl sm:text-3xl font-bold font-heading text-neutral-100 flex items-center gap-2.5">
              <LibraryIcon className="size-7 text-accent" />
              Bibliothek
            </h1>
          </div>
          <p className="text-sm text-neutral-400">
            Verwalte und durchsuche deine lokal gespeicherte Musiksammlung.
          </p>

          <div className="flex flex-wrap items-center gap-4 text-xs text-neutral-300 pt-2">
            <span className="flex items-center gap-1.5 font-medium">
              <UserIcon className="size-4 text-neutral-400" />
              {stats?.total_artists !== undefined ? pluralize(stats.total_artists, 'Künstler') : '–'}
            </span>
            <span>·</span>
            <span className="flex items-center gap-1.5 font-medium">
              <Disc3Icon className="size-4 text-neutral-400" />
              {stats?.total_releases !== undefined ? pluralize(stats.total_releases, 'Release', 'Releases') : '–'}
            </span>
            <span>·</span>
            <span className="flex items-center gap-1.5 font-medium">
              <Music2Icon className="size-4 text-neutral-400" />
              {stats?.total_tracks !== undefined ? pluralize(stats.total_tracks, 'Track') : '–'}
            </span>
            <span>·</span>
            <span className="flex items-center gap-1.5 font-medium">
              <HardDriveIcon className="size-4 text-neutral-400" />
              {stats ? formatBytes(stats.total_bytes) : '–'}
            </span>
          </div>
        </div>

        {/* Quick Action Buttons */}
        <div className="flex flex-wrap items-center gap-2 shrink-0">
          <Button
            variant="secondary"
            size="sm"
            className="gap-1.5 text-xs h-8"
            disabled={isScanning}
            onClick={handleStartScan}
          >
            <RefreshCwIcon className={`size-3.5 ${isScanning ? 'animate-spin' : ''}`} />
            Scan
          </Button>

          <Button
            variant="secondary"
            size="sm"
            className="gap-1.5 text-xs h-8"
            disabled={isCheckingCompat}
            onClick={handleCheckCompat}
          >
            <ShieldCheckIcon className="size-3.5 text-success" />
            Kompatibilität
          </Button>

          <Button
            variant="secondary"
            size="sm"
            className="gap-1.5 text-xs h-8"
            disabled={isBackfilling}
            onClick={handleStartBackfill}
          >
            <SparklesIcon className="size-3.5 text-accent" />
            Lyrics Backfill
          </Button>
        </div>
      </div>

      {/* Action feedback alert */}
      {actionFeedback && (
        <div className="flex items-center justify-between p-3 rounded-xl bg-accent/10 border border-accent/20 text-xs text-accent">
          <span>{actionFeedback}</span>
          <button onClick={() => setActionFeedback(null)} className="p-1 hover:opacity-70">
            <X className="size-3.5" />
          </button>
        </div>
      )}

      {/* Prominent Library Search */}
      <div className="w-full max-w-2xl">
        <LibrarySearchField
          defaultValue={currentQ}
          placeholder="Bibliothek durchsuchen (Künstler, Alben, Titel, ISRC)..."
          onSearchSubmit={(q) => {
            setView('tracks')
            updateUrl({ view: 'tracks', q: q || undefined, page: 1 })
          }}
          onClear={() => {
            updateUrl({ q: undefined, page: 1 })
          }}
        />
      </div>

      {/* Main Tabs Navigation */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4 border-b border-neutral-800 pb-2">
        <div className="flex items-center gap-2 overflow-x-auto">
          <Button
            variant={view === 'releases' ? 'default' : 'ghost'}
            size="sm"
            className="text-sm font-medium gap-2"
            onClick={() => handleTabChange('releases')}
          >
            <Disc3Icon className="size-4" />
            Releases {stats?.total_releases !== undefined ? `(${stats.total_releases})` : ''}
          </Button>

          <Button
            variant={view === 'tracks' ? 'default' : 'ghost'}
            size="sm"
            className="text-sm font-medium gap-2"
            onClick={() => handleTabChange('tracks')}
          >
            <Music2Icon className="size-4" />
            Titel {stats?.total_tracks !== undefined ? `(${stats.total_tracks})` : ''}
          </Button>

          <Button
            variant={view === 'artists' ? 'default' : 'ghost'}
            size="sm"
            className="text-sm font-medium gap-2"
            onClick={() => handleTabChange('artists')}
          >
            <UserIcon className="size-4" />
            Künstler {stats?.total_artists !== undefined ? `(${stats.total_artists})` : ''}
          </Button>

          <Button
            variant={view === 'maintenance' ? 'default' : 'ghost'}
            size="sm"
            className="text-sm font-medium gap-2"
            onClick={() => handleTabChange('maintenance')}
          >
            <WrenchIcon className="size-4" />
            Wartung
          </Button>
        </div>
      </div>

      {/* Tab 1: Releases Grid */}
      {view === 'releases' && (
        <div className="space-y-6">
          {/* Release Filter & Sort Bar */}
          <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
            <div className="flex flex-wrap items-center gap-1.5">
              <Button
                variant={!currentType ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ type: undefined, page: 1 })}
              >
                Alle
              </Button>
              {['album', 'ep', 'single', 'compilation'].map((t) => (
                <Button
                  key={t}
                  variant={currentType === t ? 'secondary' : 'ghost'}
                  size="sm"
                  className="h-7 text-xs capitalize"
                  onClick={() => updateUrl({ type: t, page: 1 })}
                >
                  {t}
                </Button>
              ))}
            </div>

            <div className="flex items-center gap-2">
              <span className="text-neutral-500">Sortierung:</span>
              <select
                value={currentSort || 'recent'}
                onChange={(e) => updateUrl({ sort: e.target.value, page: 1 })}
                className="bg-neutral-900 border border-neutral-800 rounded px-2 py-1 text-xs text-neutral-300 focus:outline-none focus:border-accent"
              >
                <option value="recent">Zuletzt hinzugefügt</option>
                <option value="year">Erscheinungsjahr</option>
                <option value="title">Titel</option>
                <option value="artist">Künstler</option>
              </select>
            </div>
          </div>

          {/* Active Filter Chips Bar (Releases) */}
          {(currentQ || currentType || currentYear !== undefined || currentArtistId) && (
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="text-neutral-500 font-medium">Aktive Filter:</span>
              {currentQ && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Suche: „{currentQ}“</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ q: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Suchbegriff entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentType && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700 capitalize"
                >
                  <span>Typ: {currentType}</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ type: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Typ-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentYear !== undefined && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Jahr: {currentYear}</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ year: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Jahr-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentArtistId && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Künstler gewählt</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ artist: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Künstler-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}

              <Button
                variant="ghost"
                size="sm"
                className="h-6 text-xs text-neutral-400 hover:text-accent px-1.5"
                onClick={() => {
                  updateUrl({
                    q: undefined,
                    type: undefined,
                    year: undefined,
                    artist: undefined,
                    page: 1,
                  })
                }}
              >
                Filter zurücksetzen
              </Button>
            </div>
          )}

          {releasesLoading ? (
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
              {Array.from({ length: 12 }).map((_, i) => (
                <div key={i} className="panel h-64 p-3 space-y-3 animate-pulse">
                  <div className="bg-neutral-800 aspect-square w-full rounded-lg" />
                  <div className="h-4 bg-neutral-800 rounded w-3/4" />
                  <div className="h-3 bg-neutral-800 rounded w-1/2" />
                </div>
              ))}
            </div>
          ) : fetchError ? (
            <Panel>
              <ErrorState error={new Error(fetchError)} onRetry={() => updateUrl({})} />
            </Panel>
          ) : releases.length === 0 ? (
            <Panel>
              <EmptyState
                icon={<Disc3Icon />}
                title={
                  currentQ || currentType || currentYear !== undefined || currentArtistId
                    ? 'Keine Treffer für deine Suche'
                    : 'Keine Releases in der Bibliothek'
                }
                description={
                  currentQ || currentType || currentYear !== undefined || currentArtistId
                    ? currentQ
                      ? `Keine Releases für „${currentQ}“ gefunden.`
                      : 'Keine Releases mit den ausgewählten Filtern gefunden.'
                    : 'Lade Musik über Discover herunter.'
                }
                action={
                  currentQ || currentType || currentYear !== undefined || currentArtistId ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        updateUrl({
                          q: undefined,
                          type: undefined,
                          year: undefined,
                          artist: undefined,
                          page: 1,
                        })
                      }}
                    >
                      Filter zurücksetzen
                    </Button>
                  ) : undefined
                }
              />
            </Panel>
          ) : (
            <>
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
                {releases.map((rel) => (
                  <ReleaseCard key={rel.id} release={rel} isLocal />
                ))}
              </div>

              <Pagination
                page={currentPage}
                pageSize={24}
                total={releasesTotal}
                onPageChange={(p) => updateUrl({ page: p })}
              />
            </>
          )}
        </div>
      )}

      {/* Tab 2: Tracks Table */}
      {view === 'tracks' && (
        <div className="space-y-6">
          {/* Tracks Filter Bar */}
          <div className="flex flex-wrap items-center justify-between gap-3 text-xs">
            <div className="flex flex-wrap items-center gap-1.5">
              {/* Favorite toggle button */}
              <Button
                variant={currentFavorite ? 'secondary' : 'ghost'}
                size="sm"
                className={cn(
                  'h-7 text-xs gap-1.5 transition-colors',
                  currentFavorite && 'text-red-400 border border-red-500/30 bg-red-500/10 hover:bg-red-500/20',
                )}
                onClick={() => updateUrl({ favorite: currentFavorite ? undefined : true, page: 1 })}
                title="Nur favorisierte Titel anzeigen"
              >
                <Heart className={cn('size-3.5', currentFavorite ? 'fill-red-500 text-red-500' : 'text-neutral-400')} />
                <span>Nur Favoriten</span>
              </Button>

              <span className="text-neutral-500 mx-1">·</span>

              <span className="text-neutral-500 mr-1">Lyrics:</span>
              <Button
                variant={!currentLyrics ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ lyrics: undefined, page: 1 })}
              >
                Alle
              </Button>
              <Button
                variant={currentLyrics === 'available_synced' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ lyrics: 'available_synced', page: 1 })}
              >
                Synced
              </Button>
              <Button
                variant={currentLyrics === 'available_plain' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ lyrics: 'available_plain', page: 1 })}
              >
                Plain
              </Button>
              <Button
                variant={currentLyrics === 'instrumental' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ lyrics: 'instrumental', page: 1 })}
              >
                Instrumental
              </Button>
              <Button
                variant={currentLyrics === 'not_found' ? 'secondary' : 'ghost'}
                size="sm"
                className="h-7 text-xs"
                onClick={() => updateUrl({ lyrics: 'not_found', page: 1 })}
              >
                Nicht gefunden
              </Button>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-neutral-500">Sortierung:</span>
              <select
                value={currentSort || (currentQ ? 'relevance' : 'recent')}
                onChange={(e) => updateUrl({ sort: e.target.value, page: 1 })}
                className="bg-neutral-900 border border-neutral-800 rounded px-2 py-1 text-xs text-neutral-300 focus:outline-none focus:border-accent"
              >
                {currentQ && <option value="relevance">Relevanz</option>}
                <option value="recent">Zuletzt hinzugefügt</option>
                <option value="title">Titel</option>
                <option value="artist">Künstler</option>
                <option value="year">Erscheinungsjahr</option>
                <option value="duration">Dauer</option>
              </select>
            </div>
          </div>

          {/* Active Filter Chips Bar (Tracks) */}
          {(currentQ ||
            currentFavorite ||
            currentYear !== undefined ||
            currentLyrics ||
            currentArtistId ||
            currentReleaseId) && (
            <div className="flex flex-wrap items-center gap-2 text-xs">
              <span className="text-neutral-500 font-medium">Aktive Filter:</span>
              {currentQ && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Suche: „{currentQ}“</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ q: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Suchbegriff entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentFavorite && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-red-950/40 text-red-300 border border-red-800/50"
                >
                  <Heart className="size-3 fill-red-500 text-red-500" />
                  <span>Nur Favoriten</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ favorite: undefined, page: 1 })}
                    className="hover:text-red-100"
                    aria-label="Favoriten-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentYear !== undefined && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Jahr: {currentYear}</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ year: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Jahr-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentLyrics && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Lyrics: {currentLyrics}</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ lyrics: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Lyrics-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentArtistId && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Künstler gewählt</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ artist: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Künstler-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}
              {currentReleaseId && (
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Release gewählt</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ release: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Release-Filter entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
              )}

              <Button
                variant="ghost"
                size="sm"
                className="h-6 text-xs text-neutral-400 hover:text-accent px-1.5"
                onClick={() => {
                  updateUrl({
                    q: undefined,
                    favorite: undefined,
                    year: undefined,
                    lyrics: undefined,
                    artist: undefined,
                    release: undefined,
                    page: 1,
                  })
                }}
              >
                Filter zurücksetzen
              </Button>
            </div>
          )}

          {tracksLoading ? (
            <div className="space-y-2">
              {Array.from({ length: 10 }).map((_, i) => (
                <div key={i} className="h-10 bg-neutral-900/60 rounded-xl animate-pulse" />
              ))}
            </div>
          ) : fetchError ? (
            <Panel>
              <ErrorState error={new Error(fetchError)} onRetry={() => updateUrl({})} />
            </Panel>
          ) : tracks.length === 0 ? (
            <Panel>
              <EmptyState
                icon={<Music2Icon />}
                title={
                  currentQ ||
                  currentFavorite ||
                  currentYear !== undefined ||
                  currentLyrics ||
                  currentArtistId ||
                  currentReleaseId
                    ? 'Keine Treffer für deine Suche'
                    : 'Keine Titel in der Bibliothek'
                }
                description={
                  currentQ ||
                  currentFavorite ||
                  currentYear !== undefined ||
                  currentLyrics ||
                  currentArtistId ||
                  currentReleaseId
                    ? currentQ
                      ? `Keine Titel für „${currentQ}“ mit den ausgewählten Filtern gefunden.`
                      : 'Keine Titel mit den ausgewählten Filtern gefunden.'
                    : 'Lade Musik über Discover herunter.'
                }
                action={
                  currentQ ||
                  currentFavorite ||
                  currentYear !== undefined ||
                  currentLyrics ||
                  currentArtistId ||
                  currentReleaseId ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => {
                        updateUrl({
                          q: undefined,
                          favorite: undefined,
                          year: undefined,
                          lyrics: undefined,
                          artist: undefined,
                          release: undefined,
                          sort: undefined,
                          order: undefined,
                          page: 1,
                        })
                      }}
                    >
                      Filter zurücksetzen
                    </Button>
                  ) : undefined
                }
              />
            </Panel>
          ) : (
            <>
              <TracksTable
                tracks={tracks}
                sort={currentSort || (currentQ ? 'relevance' : 'recent')}
                order={
                  currentOrder ||
                  (currentSort === 'relevance' || (!currentSort && currentQ) ? 'asc' : 'desc')
                }
                onSortChange={(s) => {
                  const activeSort = currentSort || (currentQ ? 'relevance' : 'recent')
                  const activeOrder =
                    currentOrder || (activeSort === 'relevance' ? 'asc' : 'desc')
                  const newOrder = activeSort === s && activeOrder === 'asc' ? 'desc' : 'asc'
                  updateUrl({ sort: s, order: newOrder, page: 1 })
                }}
                onTrackSelect={(t) => {
                  setActiveTrackId(t.id)
                  updateUrl({ track: t.id }, true)
                }}
              />

              <Pagination
                page={currentPage}
                pageSize={50}
                total={tracksTotal}
                onPageChange={(p) => updateUrl({ page: p })}
              />
            </>
          )}
        </div>
      )}

      {/* Tab 3: Artists Grid */}
      {view === 'artists' && (
        <div className="space-y-6">
          <div className="flex items-center justify-between gap-2 text-xs">
            {currentQ ? (
              <div className="flex items-center gap-2">
                <Badge
                  variant="outline"
                  className="gap-1.5 py-0.5 px-2 text-xs bg-neutral-800/90 text-neutral-200 border-neutral-700"
                >
                  <span>Suche: „{currentQ}“</span>
                  <button
                    type="button"
                    onClick={() => updateUrl({ q: undefined, page: 1 })}
                    className="hover:text-neutral-100"
                    aria-label="Suchbegriff entfernen"
                  >
                    <X className="size-3" />
                  </button>
                </Badge>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-6 text-xs text-neutral-400 hover:text-accent px-1.5"
                  onClick={() => updateUrl({ q: undefined, page: 1 })}
                >
                  Filter zurücksetzen
                </Button>
              </div>
            ) : <div />}

            <div className="flex items-center gap-2">
              <span className="text-neutral-500">Sortierung:</span>
              <select
                value={currentSort || 'name'}
                onChange={(e) => updateUrl({ sort: e.target.value, page: 1 })}
                className="bg-neutral-900 border border-neutral-800 rounded px-2 py-1 text-xs text-neutral-300 focus:outline-none focus:border-accent"
              >
                <option value="name">Name (A–Z)</option>
                <option value="recent">Zuletzt hinzugefügt</option>
                <option value="release_count">Anzahl Releases</option>
              </select>
            </div>
          </div>

          {artistsLoading ? (
            <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
              {Array.from({ length: 12 }).map((_, i) => (
                <div key={i} className="panel h-56 p-4 flex flex-col items-center gap-3 animate-pulse">
                  <div className="bg-neutral-800 size-24 rounded-full" />
                  <div className="h-4 bg-neutral-800 rounded w-3/4 mt-2" />
                </div>
              ))}
            </div>
          ) : fetchError ? (
            <Panel>
              <ErrorState error={new Error(fetchError)} onRetry={() => updateUrl({})} />
            </Panel>
          ) : artists.length === 0 ? (
            <Panel>
              <EmptyState
                icon={<UserIcon />}
                title={currentQ ? 'Keine Treffer für deine Suche' : 'Keine Künstler in der Bibliothek'}
                description={
                  currentQ
                    ? `Keine Künstler für „${currentQ}“ gefunden.`
                    : 'Lade Musik über Discover herunter.'
                }
                action={
                  currentQ ? (
                    <Button
                      variant="secondary"
                      size="sm"
                      onClick={() => updateUrl({ q: undefined, page: 1 })}
                    >
                      Filter zurücksetzen
                    </Button>
                  ) : undefined
                }
              />
            </Panel>
          ) : (
            <>
              <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
                {artists.map((art) => (
                  <ArtistCard key={art.id} artist={art} isLocal />
                ))}
              </div>

              <Pagination
                page={currentPage}
                pageSize={24}
                total={artistsTotal}
                onPageChange={(p) => updateUrl({ page: p })}
              />
            </>
          )}
        </div>
      )}

      {/* Tab 4: Maintenance & Scan */}
      {view === 'maintenance' && (
        <IntegrityPanel isAdmin={isAdmin} />
      )}

      {/* Track Detail Dialog (with deep-link support) */}
      <TrackDetailDialog
        trackId={activeTrackId}
        open={activeTrackId !== null}
        onOpenChange={(open) => {
          if (!open) {
            setActiveTrackId(null)
            updateUrl({ track: undefined }, true)
          }
        }}
        isAdmin={isAdmin}
        onTrackUpdated={() => {
          // Trigger refresh of active tab
          updateUrl({}, true)
        }}
        onTrackDeleted={(deletedId) => {
          setActiveTrackId(null)
          updateUrl({ track: undefined }, true)
          setTracks((prev) => prev.filter((t) => t.id !== deletedId))
          setTracksTotal((prev) => Math.max(0, prev - 1))
        }}
      />

      {/* Deletion Modal */}
      <Dialog open={pendingDelete !== null} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{pendingDelete?.title}</DialogTitle>
            <DialogDescription>{pendingDelete?.description}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setPendingDelete(null)}>
              Abbrechen
            </Button>
            <Button variant="destructive" disabled={isDeleting} onClick={handleExecuteDelete}>
              {isDeleting ? 'Lösche...' : 'Endgültig löschen'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Compatibility Report & Reorganize Dialog */}
      <Dialog
        open={compatModalOpen}
        onOpenChange={(open) => {
          if (!open && !isReorganizing) setCompatModalOpen(false)
        }}
      >
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>Server-Kompatibilitätsprüfung</DialogTitle>
            <DialogDescription>
              Prüfung auf standardkonforme Ordner- und Dateistrukturen für Plex, Jellyfin und Emby.
              {compatReport && ` (${compatReport.files_scanned} Dateien geprüft)`}
            </DialogDescription>
          </DialogHeader>

          <div className="flex-1 overflow-y-auto min-h-[250px] max-h-[50vh] pr-2 my-2 space-y-3">
            {compatReport && compatReport.issues.length === 0 ? (
              <div className="flex flex-col items-center justify-center h-full py-8 text-center text-muted-foreground">
                <CheckCircle2Icon className="size-8 text-success mb-2" />
                <p className="font-medium text-foreground">Alles optimal strukturiert</p>
                <p className="text-xs mt-1">
                  Keine Abweichungen für Plex, Jellyfin oder Emby gefunden.
                </p>
              </div>
            ) : (
              compatReport?.issues.map((issue) => {
                const isSelected = selectedCompatIssues.includes(issue.id)
                return (
                  <div
                    key={issue.id}
                    className="flex items-start gap-3 p-3 rounded-xl border border-neutral-800 bg-neutral-900/50 text-xs"
                  >
                    {issue.to && issue.from !== issue.to && (
                      <Checkbox
                        checked={isSelected}
                        onCheckedChange={(checked) => {
                          if (checked === true) {
                            setSelectedCompatIssues([...selectedCompatIssues, issue.id])
                          } else {
                            setSelectedCompatIssues(selectedCompatIssues.filter((id) => id !== issue.id))
                          }
                        }}
                        className="mt-1"
                      />
                    )}
                    <div className="min-w-0 flex-1 space-y-1">
                      <Badge variant="warning" className="text-[10px]">
                        {issue.kind}
                      </Badge>
                      <div className="font-mono text-neutral-300 break-all select-all">{issue.from}</div>
                      {issue.to && issue.from !== issue.to && (
                        <div className="font-mono text-success break-all select-all">➜ {issue.to}</div>
                      )}
                    </div>
                  </div>
                )
              })
            )}
          </div>

          <DialogFooter className="flex items-center justify-between sm:justify-between w-full pt-3 border-t border-neutral-800">
            <Button variant="ghost" size="sm" onClick={() => setCompatModalOpen(false)}>
              Schließen
            </Button>
            {compatReport && compatReport.issues.length > 0 && (
              <Button
                variant="default"
                size="sm"
                className="gap-2 text-xs"
                disabled={isReorganizing}
                onClick={handleReorganize}
              >
                <FolderSyncIcon className="size-3.5" />
                Reorganisieren ({selectedCompatIssues.length > 0 ? selectedCompatIssues.length : 'Alle'})
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
