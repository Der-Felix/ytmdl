import { useCallback, useEffect, useRef, useState } from 'react'
import type { KeyboardEvent } from 'react'
import {
  ArrowRight,
  Disc3Icon,
  ListPlus,
  Loader2,
  Music2Icon,
  Pause,
  Play,
  SearchIcon,
  UserIcon,
  X,
} from 'lucide-react'

import { useContext } from 'react'
import { Cover } from '@/components/music/Cover'
import { PlayerActionsContext, PlayerStateContext } from '@/hooks/usePlayer'
import { librarySearch } from '@/lib/api/library'
import { paths, useNavigate } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatDuration, joinArtists, pluralize } from '@/lib/utils/format'
import type {
  LibraryArtist,
  LibraryRelease,
  LibrarySearchResults,
  LibraryTrack,
} from '@/types/api'

export interface LibrarySearchFieldProps {
  defaultValue?: string
  placeholder?: string
  onSearchSubmit?: (query: string) => void
  onClear?: () => void
  autoFocus?: boolean
  className?: string
}

export function LibrarySearchField({
  defaultValue = '',
  placeholder = 'Bibliothek durchsuchen (Künstler, Alben, Titel)...',
  onSearchSubmit,
  onClear,
  autoFocus,
  className,
}: LibrarySearchFieldProps) {
  const [value, setValue] = useState(defaultValue)
  const [isOpen, setIsOpen] = useState(false)
  const [isLoading, setIsLoading] = useState(false)
  const [results, setResults] = useState<LibrarySearchResults | null>(null)

  const navigate = useNavigate()
  const playerState = useContext(PlayerStateContext)
  const playerActions = useContext(PlayerActionsContext)
  const currentTrack = playerState?.currentTrack
  const status = playerState?.status

  const containerRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const abortControllerRef = useRef<AbortController | null>(null)
  const requestSeqRef = useRef<number>(0)

  // Synchronize when defaultValue changes from URL
  useEffect(() => {
    setValue(defaultValue)
  }, [defaultValue])

  const performSearch = useCallback(async (searchQuery: string) => {
    const q = searchQuery.trim()
    if (!q) {
      requestSeqRef.current++
      setResults(null)
      setIsLoading(false)
      return
    }

    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    const controller = new AbortController()
    abortControllerRef.current = controller
    const seq = ++requestSeqRef.current

    setIsLoading(true)
    try {
      const data = await librarySearch(q, { limit: 5, signal: controller.signal })
      if (!controller.signal.aborted && seq === requestSeqRef.current) {
        setResults(data)
        setIsOpen(true)
      }
    } catch (err) {
      if (!controller.signal.aborted && seq === requestSeqRef.current) {
        // Drop on error or aborted
        setResults(null)
      }
    } finally {
      if (!controller.signal.aborted && seq === requestSeqRef.current) {
        setIsLoading(false)
      }
    }
  }, [])

  const handleInputChange = (newVal: string) => {
    setValue(newVal)
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
    }

    const trimmed = newVal.trim()
    if (!trimmed) {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
      requestSeqRef.current++
      setResults(null)
      setIsLoading(false)
      setIsOpen(false)
      return
    }

    debounceTimerRef.current = setTimeout(() => {
      void performSearch(trimmed)
    }, 250)
  }

  const handleClear = () => {
    setValue('')
    if (debounceTimerRef.current) {
      clearTimeout(debounceTimerRef.current)
    }
    if (abortControllerRef.current) {
      abortControllerRef.current.abort()
    }
    requestSeqRef.current++
    setResults(null)
    setIsLoading(false)
    setIsOpen(false)
    onClear?.()
  }

  const handleSubmit = (e?: React.FormEvent) => {
    e?.preventDefault()
    const trimmed = value.trim()
    setIsOpen(false)
    if (onSearchSubmit) {
      onSearchSubmit(trimmed)
    } else if (trimmed) {
      navigate(paths.library({ view: 'tracks', q: trimmed }))
    }
  }

  const handleKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Escape') {
      setIsOpen(false)
      inputRef.current?.blur()
    } else if (e.key === 'Enter') {
      handleSubmit()
    }
  }

  // Click outside listener
  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(event.target as Node)) {
        setIsOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => {
      document.removeEventListener('mousedown', handleClickOutside)
      if (debounceTimerRef.current) {
        clearTimeout(debounceTimerRef.current)
      }
      if (abortControllerRef.current) {
        abortControllerRef.current.abort()
      }
    }
  }, [])

  const handleTrackPlayClick = (e: React.MouseEvent, track: LibraryTrack, index: number) => {
    e.stopPropagation()
    if (!playerActions) return
    if (currentTrack?.id === track.id) {
      playerActions.togglePlayPause()
      return
    }
    if (results?.tracks && results.tracks.length > 0) {
      playerActions.playTrack(track, results.tracks, index)
    } else {
      playerActions.playTrack(track, [track], 0)
    }
  }

  const handleTrackRowClick = (track: LibraryTrack, index: number) => {
    if (!playerActions) return
    if (currentTrack?.id === track.id) {
      playerActions.togglePlayPause()
      return
    }
    if (results?.tracks && results.tracks.length > 0) {
      playerActions.playTrack(track, results.tracks, index)
    } else {
      playerActions.playTrack(track, [track], 0)
    }
  }

  const handleArtistClick = (artist: LibraryArtist) => {
    setIsOpen(false)
    navigate(paths.libraryArtist(artist.id))
  }

  const handleReleaseClick = (release: LibraryRelease) => {
    setIsOpen(false)
    navigate(paths.libraryRelease(release.id))
  }

  const hasResults =
    results &&
    ((results.artists && results.artists.length > 0) ||
      (results.releases && results.releases.length > 0) ||
      (results.tracks && results.tracks.length > 0))

  const isSearchEmpty =
    !isLoading &&
    results &&
    (!results.artists || results.artists.length === 0) &&
    (!results.releases || results.releases.length === 0) &&
    (!results.tracks || results.tracks.length === 0)

  return (
    <div ref={containerRef} className={cn('relative w-full', className)}>
      <form role="search" onSubmit={handleSubmit} className="relative w-full">
        <div className="relative flex items-center w-full">
          <SearchIcon
            aria-hidden
            className="pointer-events-none absolute left-3.5 top-1/2 -translate-y-1/2 size-4 text-neutral-400"
          />

          <input
            ref={inputRef}
            type="search"
            name="library-search"
            value={value}
            autoFocus={autoFocus}
            onFocus={() => {
              if (value.trim() && (results || isLoading)) {
                setIsOpen(true)
              }
            }}
            onChange={(e) => handleInputChange(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            aria-label="Bibliothek durchsuchen"
            aria-expanded={isOpen}
            className={cn(
              'w-full h-11 pl-10 pr-10 text-sm rounded-xl border border-neutral-800 bg-neutral-900/90 text-neutral-100',
              'placeholder:text-neutral-500 transition-colors outline-none',
              'hover:border-neutral-700',
              'focus:border-accent/60 focus:bg-neutral-900 focus:ring-1 focus:ring-accent/40',
              '[&::-webkit-search-cancel-button]:appearance-none',
            )}
          />

          <div className="absolute right-3 top-1/2 -translate-y-1/2 flex items-center gap-1.5">
            {isLoading && (
              <Loader2 className="size-4 animate-spin text-neutral-400" aria-label="Laden..." />
            )}
            {value && (
              <button
                type="button"
                onClick={handleClear}
                aria-label="Suche löschen"
                className="p-1 text-neutral-400 hover:text-neutral-200 rounded transition-colors"
              >
                <X className="size-4" />
              </button>
            )}
          </div>
        </div>
      </form>

      {/* Flyout Results Dropdown */}
      {isOpen && (
        <div
          data-testid="library-search-flyout"
          className="absolute z-50 left-0 right-0 top-full mt-1.5 max-h-[75vh] overflow-y-auto rounded-xl border border-neutral-800 bg-neutral-950/95 backdrop-blur-md shadow-2xl p-2 space-y-4"
        >
          {isLoading && !hasResults && (
            <div className="flex items-center justify-center py-8 text-xs text-neutral-400 gap-2">
              <Loader2 className="size-4 animate-spin text-accent" />
              <span>Suche in der Bibliothek...</span>
            </div>
          )}

          {isSearchEmpty && (
            <div className="py-8 text-center text-xs text-neutral-400 space-y-1">
              <p className="font-medium text-neutral-300">Keine Treffer</p>
              <p>Keine Einträge für „{value}“ in der lokalen Bibliothek gefunden.</p>
            </div>
          )}

          {/* Artists Section */}
          {results?.artists && results.artists.length > 0 && (
            <div className="space-y-1">
              <div className="px-2 py-1 text-[0.7rem] font-semibold tracking-wider uppercase text-neutral-500 flex items-center gap-1.5">
                <UserIcon className="size-3" />
                <span>Künstler ({results.artists.length})</span>
              </div>
              <div className="space-y-0.5">
                {results.artists.map((artist) => (
                  <button
                    key={artist.id}
                    type="button"
                    onClick={() => handleArtistClick(artist)}
                    className="w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-left transition-colors hover:bg-neutral-800/60 group"
                  >
                    <Cover
                      src={artist.image_url}
                      alt=""
                      shape="circle"
                      className="size-9 shrink-0 border border-neutral-700/50"
                    />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-neutral-200 group-hover:text-neutral-100 truncate">
                        {artist.name}
                      </p>
                      <p className="text-xs text-neutral-400 truncate">
                        {pluralize(artist.release_count, 'Release', 'Releases')} ·{' '}
                        {pluralize(artist.track_count, 'Track')}
                      </p>
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Releases Section */}
          {results?.releases && results.releases.length > 0 && (
            <div className="space-y-1">
              <div className="px-2 py-1 text-[0.7rem] font-semibold tracking-wider uppercase text-neutral-500 flex items-center gap-1.5">
                <Disc3Icon className="size-3" />
                <span>Alben & Releases ({results.releases.length})</span>
              </div>
              <div className="space-y-0.5">
                {results.releases.map((release) => (
                  <button
                    key={release.id}
                    type="button"
                    onClick={() => handleReleaseClick(release)}
                    className="w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-left transition-colors hover:bg-neutral-800/60 group"
                  >
                    <Cover
                      src={release.cover_url}
                      alt=""
                      shape="square"
                      className="size-9 shrink-0 rounded-md border border-neutral-700/50"
                    />
                    <div className="min-w-0 flex-1">
                      <p className="text-sm font-medium text-neutral-200 group-hover:text-neutral-100 truncate">
                        {release.title}
                      </p>
                      <p className="text-xs text-neutral-400 truncate">
                        {release.album_artist || joinArtists(release.artists) || 'Unbekannt'}
                        {release.year ? ` · ${release.year}` : ''}
                      </p>
                    </div>
                  </button>
                ))}
              </div>
            </div>
          )}

          {/* Tracks Section */}
          {results?.tracks && results.tracks.length > 0 && (
            <div className="space-y-1">
              <div className="px-2 py-1 text-[0.7rem] font-semibold tracking-wider uppercase text-neutral-500 flex items-center gap-1.5">
                <Music2Icon className="size-3" />
                <span>Titel ({results.tracks.length})</span>
              </div>
              <div className="space-y-0.5">
                {results.tracks.map((track, idx) => {
                  const isPlaying = currentTrack?.id === track.id && status === 'playing'
                  return (
                    <div
                      key={track.id}
                      onClick={() => handleTrackRowClick(track, idx)}
                      className="w-full flex items-center justify-between gap-3 px-2.5 py-1.5 rounded-lg text-left transition-colors hover:bg-neutral-800/60 cursor-pointer group"
                    >
                      <div className="flex items-center gap-3 min-w-0 flex-1">
                        <button
                          type="button"
                          onClick={(e) => handleTrackPlayClick(e, track, idx)}
                          aria-label={isPlaying ? 'Pause' : 'Abspielen'}
                          className={cn(
                            'size-8 rounded-full flex items-center justify-center shrink-0 transition-colors',
                            isPlaying
                              ? 'bg-accent text-neutral-950'
                              : 'bg-neutral-800 text-neutral-300 group-hover:bg-accent group-hover:text-neutral-950',
                          )}
                        >
                          {isPlaying ? (
                            <Pause className="size-3.5 fill-current" />
                          ) : (
                            <Play className="size-3.5 fill-current ml-0.5" />
                          )}
                        </button>
                        <div className="min-w-0 flex-1">
                          <p
                            className={cn(
                              'text-sm font-medium truncate',
                              currentTrack?.id === track.id
                                ? 'text-accent'
                                : 'text-neutral-200 group-hover:text-neutral-100',
                            )}
                          >
                            {track.title}
                          </p>
                          <p className="text-xs text-neutral-400 truncate">
                            {track.album_artist || joinArtists(track.artists) || 'Unbekannt'}
                            {track.album ? ` · ${track.album}` : ''}
                          </p>
                        </div>
                      </div>
                      <div className="flex items-center gap-1 shrink-0">
                        <span className="text-xs text-neutral-500 font-mono mr-1">
                          {formatDuration(track.duration_ms)}
                        </span>
                        {playerActions && (
                          <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation()
                                playerActions.playNext(track)
                              }}
                              className="size-7 rounded-md flex items-center justify-center text-neutral-400 hover:text-white hover:bg-neutral-700/60 transition-colors"
                              title="Als Nächstes abspielen"
                              aria-label="Als Nächstes abspielen"
                            >
                              <Play className="size-3" />
                            </button>
                            <button
                              type="button"
                              onClick={(e) => {
                                e.stopPropagation()
                                playerActions.addToQueue(track)
                              }}
                              className="size-7 rounded-md flex items-center justify-center text-neutral-400 hover:text-white hover:bg-neutral-700/60 transition-colors"
                              title="Zur Queue hinzufügen"
                              aria-label="Zur Queue hinzufügen"
                            >
                              <ListPlus className="size-3.5" />
                            </button>
                          </div>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}

          {/* Footer Navigation */}
          {hasResults && (
            <div className="pt-2 border-t border-neutral-800">
              <button
                type="button"
                onClick={() => handleSubmit()}
                className="w-full flex items-center justify-center gap-2 py-2 text-xs font-medium text-accent hover:text-accent/80 hover:bg-neutral-800/40 rounded-lg transition-colors"
              >
                <span>Alle Ergebnisse in Titeln anzeigen</span>
                <ArrowRight className="size-3.5" />
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
