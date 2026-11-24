/**
 * The two areas of the application: listening to the library and loading new
 * music into it. Every route belongs to one of them or to neither; the switch
 * at the top of the shell, the sidebar and "/" all read this one table.
 */

import { useEffect } from 'react'

import { paths } from '@/lib/router'
import type { Route } from '@/lib/router'

export type Area = 'listen' | 'download'

/** Where a route belongs. A neutral route keeps the area the user came from. */
export type RouteArea = Area | 'neutral'

/**
 * The area of every route. A Record over the route names, so a new route
 * without an area does not compile (and the test over it fails).
 */
export const ROUTE_AREAS: Record<Route['name'], RouteArea> = {
  home: 'neutral',
  listenHome: 'listen',
  player: 'listen',
  mixes: 'listen',
  mix: 'listen',
  offline: 'listen',
  library: 'listen',
  libraryArtist: 'listen',
  libraryRelease: 'listen',
  playlists: 'listen',
  playlistDetail: 'listen',
  favorites: 'listen',
  downloadHome: 'download',
  discover: 'download',
  artist: 'download',
  release: 'download',
  downloads: 'download',
  subscriptions: 'download',
  login: 'neutral',
  profile: 'neutral',
  users: 'neutral',
  settings: 'neutral',
  notFound: 'neutral',
}

/** The start page of each area; the switch and the wordmark lead there. */
export const AREA_START: Record<Area, string> = {
  listen: paths.listenHome(),
  download: paths.downloadHome(),
}

/** Without a stored choice the application opens on listening. */
export const DEFAULT_AREA: Area = 'listen'

const STORAGE_KEY = 'ytmdl.area'

// Kept in memory as well, so the choice holds for this tab when the browser
// refuses storage (private mode, blocked site data).
let remembered: Area | null = null

function isArea(value: unknown): value is Area {
  return value === 'listen' || value === 'download'
}

/** The area the user was last in, in this browser. */
export function loadArea(): Area {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (isArea(stored)) return stored
  } catch {
    // Storage unavailable: fall back to this tab's memory.
  }
  return remembered ?? DEFAULT_AREA
}

export function saveArea(area: Area): void {
  remembered = area
  try {
    localStorage.setItem(STORAGE_KEY, area)
  } catch {
    // Quota or access denied: the in-memory value still applies.
  }
}

/** Forgets the stored area; for tests. */
export function resetStoredArea(): void {
  remembered = null
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Nothing to forget.
  }
}

/**
 * The area to show for a route. A route that belongs to an area wins over
 * the stored choice, so a link to /downloads always opens loading; a neutral
 * route (profile, administration) shows the area the user came from.
 */
export function areaFor(route: Route): Area {
  const area = ROUTE_AREAS[route.name]
  return area === 'neutral' ? loadArea() : area
}

/** areaFor(), and remembers the area of every non-neutral route visited. */
export function useArea(route: Route): Area {
  const own = ROUTE_AREAS[route.name]
  useEffect(() => {
    if (own !== 'neutral') saveArea(own)
  }, [own])
  return own === 'neutral' ? loadArea() : own
}
