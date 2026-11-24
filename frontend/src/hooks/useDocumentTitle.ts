import { useEffect, useSyncExternalStore } from 'react'

import { joinArtists } from '@/lib/utils/format'
import { APP_NAME } from '@/lib/route-titles'
import type { LibraryTrack } from '@/types/api'

/*
 * The browser tab's title. While something plays it names the track, so the
 * tab can be found among many; otherwise it names the page. Detail pages give
 * their entity's name through usePageTitle; every other page is named by the
 * route table in lib/route-titles.ts.
 */

let pageTitle: string | null = null
let pageTitleOwner: symbol | null = null
const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

function readPageTitle(): string | null {
  return pageTitle
}

function setPageTitle(owner: symbol, title: string | null): void {
  // A page that unmounts only clears the name it set itself.
  if (title === null && pageTitleOwner !== owner) return
  pageTitle = title
  pageTitleOwner = title === null ? null : owner
  for (const listener of listeners) listener()
}

/** Names the current page after its entity (an artist, album, playlist, mix) once it is known. */
export function usePageTitle(name: string | null | undefined): void {
  const trimmed = name?.trim() || null
  useEffect(() => {
    if (!trimmed) return
    const owner = Symbol('page-title')
    setPageTitle(owner, trimmed)
    return () => setPageTitle(owner, null)
  }, [trimmed])
}

export type TitleTrack = Pick<LibraryTrack, 'title' | 'artists' | 'album_artist'>

/** The tab title: "Titel · Künstler" while playing, otherwise "Seite – YTMDL". */
export function documentTitle(options: { pageName: string; playingTrack?: TitleTrack | null }): string {
  const track = options.playingTrack
  if (track) {
    const artist = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist
    return artist ? `${track.title} · ${artist}` : track.title
  }
  return options.pageName ? `${options.pageName} – ${APP_NAME}` : APP_NAME
}

/**
 * Keeps document.title in step with the page and the track that is playing
 * (null while nothing plays or playback is paused).
 */
export function useDocumentTitle(routePageName: string, playingTrack: TitleTrack | null): void {
  const entityName = useSyncExternalStore(subscribe, readPageTitle, readPageTitle)
  const title = documentTitle({ pageName: entityName ?? routePageName, playingTrack })

  useEffect(() => {
    document.title = title
  }, [title])
}
