import { useCallback, useRef, useState } from 'react'

import { useLibrarySearch } from '@/hooks/useLibrarySearch'
import { listPlaylists } from '@/lib/api/playlists'
import type { LibraryArtist, LibraryRelease, LibraryTrack } from '@/types/api'
import type { Playlist } from '@/types/playlist'

/** Library results start at this many characters. */
export const MIN_QUERY = 2
/** Rows per result group. */
const PER_GROUP = 5

/** Value prefix of the library results: `result:<kind>:<index>:<id>`. */
const RESULT_VALUE = 'result:'

/**
 * The score cmdk gives a library result, or undefined for any other entry.
 * The server already matched and ordered them, so they skip the menu's own
 * text matching and keep the server's order (cmdk sorts by score).
 */
export function resultScore(value: string): number | undefined {
  if (!value.startsWith(RESULT_VALUE)) return undefined
  const index = Number(value.split(':')[2] ?? 0)
  return 10 - index / 100
}

export function resultValue(kind: string, index: number, id: string): string {
  return `${RESULT_VALUE}${kind}:${index}:${id}`
}

function fold(text: string): string {
  return text.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLocaleLowerCase('de')
}

/** Playlists whose name contains the query; names starting with it first. */
export function matchPlaylists(playlists: Playlist[], query: string): Playlist[] {
  const needle = fold(query.trim())
  if (!needle) return []
  const hits = playlists.filter((playlist) => fold(playlist.name).includes(needle))
  const starts = hits.filter((playlist) => fold(playlist.name).startsWith(needle))
  const rest = hits.filter((playlist) => !fold(playlist.name).startsWith(needle))
  return [...starts, ...rest].slice(0, PER_GROUP)
}

type PlaylistState = { status: 'idle' | 'loading' | 'failed' } | { status: 'ready'; items: Playlist[] }

export interface CommandSearch {
  /** At least MIN_QUERY characters are typed. */
  active: boolean
  tracks: LibraryTrack[]
  artists: LibraryArtist[]
  releases: LibraryRelease[]
  playlists: Playlist[]
  /** Something is still on its way. */
  loading: boolean
  /** Everything for exactly this query has arrived. */
  settled: boolean
  /** Rows shown across the four groups. */
  hits: number
  /** Everything for this query has arrived and nothing matched. */
  empty: boolean
  /** The library search failed for this query. */
  failed: boolean
  /** Value of the first result row, if any. */
  firstValue: string | undefined
  /** The track a result value stands for, if it is a track row. */
  trackFor: (value: string) => LibraryTrack | undefined
  /** Call with every change of the typed text. */
  update: (query: string) => void
  /** Call when the menu has closed: drops results and the playlist list. */
  reset: () => void
}

/**
 * The library side of the command menu. Nothing is asked until the menu is
 * open and MIN_QUERY characters are typed; the playlists are fetched once
 * per opening and matched here, the rest comes from `/library/search`.
 */
export function useCommandSearch(query: string): CommandSearch {
  const library = useLibrarySearch({ minLength: MIN_QUERY, limit: PER_GROUP })
  const [playlistState, setPlaylistState] = useState<PlaylistState>({ status: 'idle' })
  const playlistAbort = useRef<AbortController | null>(null)

  const trimmed = query.trim()
  const active = trimmed.length >= MIN_QUERY

  const { search: searchLibrary, clear } = library
  const update = useCallback(
    (next: string) => {
      searchLibrary(next)
      if (next.trim().length < MIN_QUERY || playlistAbort.current) return
      const controller = new AbortController()
      playlistAbort.current = controller
      setPlaylistState({ status: 'loading' })
      listPlaylists(controller.signal).then(
        (items) => {
          if (!controller.signal.aborted) setPlaylistState({ status: 'ready', items })
        },
        () => {
          if (!controller.signal.aborted) setPlaylistState({ status: 'failed' })
        },
      )
    },
    [searchLibrary],
  )

  const reset = useCallback(() => {
    clear()
    playlistAbort.current?.abort()
    playlistAbort.current = null
    setPlaylistState({ status: 'idle' })
  }, [clear])

  const results = active ? library.results : null
  const tracks = (results?.tracks ?? []).slice(0, PER_GROUP)
  const artists = (results?.artists ?? []).slice(0, PER_GROUP)
  const releases = (results?.releases ?? []).slice(0, PER_GROUP)
  const playlists =
    active && playlistState.status === 'ready' ? matchPlaylists(playlistState.items, trimmed) : []

  const libraryLoading = active && library.isLoading
  const playlistsLoading = active && playlistState.status === 'loading'
  // The answer for exactly this text is in (not one still being typed).
  const settled = active && !libraryLoading && !playlistsLoading && library.resultQuery === trimmed
  const hits = tracks.length + artists.length + releases.length + playlists.length
  const nothing = hits === 0
  const failed = active && library.failed && library.resultQuery === trimmed

  const byValue = new Map(tracks.map((track, index) => [resultValue('track', index, track.id), track]))
  const firstValue = tracks[0]
    ? resultValue('track', 0, tracks[0].id)
    : artists[0]
      ? resultValue('artist', 0, artists[0].id)
      : releases[0]
        ? resultValue('release', 0, releases[0].id)
        : playlists[0]
          ? resultValue('playlist', 0, playlists[0].id)
          : undefined

  return {
    active,
    tracks,
    artists,
    releases,
    playlists,
    loading: libraryLoading || playlistsLoading,
    settled,
    hits,
    empty: settled && nothing && !failed,
    failed,
    firstValue,
    trackFor: (value: string) => byValue.get(value),
    update,
    reset,
  }
}

