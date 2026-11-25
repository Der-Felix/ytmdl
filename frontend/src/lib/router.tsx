/**
 * The application's navigation, built on the History API.
 *
 * This is deliberately not a router library and not a re-implementation of
 * one: it publishes the current location as an external store, pushes new
 * entries onto the history stack, and lets App.tsx match the path itself.
 * Back and forward work because popstate is what drives it.
 */

import { useCallback, useMemo, useSyncExternalStore } from 'react'
import type { AnchorHTMLAttributes, MouseEvent, ReactNode } from 'react'

/** Fired after a programmatic navigation; popstate only covers the buttons. */
const NAVIGATION_EVENT = 'ytmdl:navigation'

const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  window.addEventListener('popstate', listener)
  window.addEventListener(NAVIGATION_EVENT, listener)
  return () => {
    listeners.delete(listener)
    window.removeEventListener('popstate', listener)
    window.removeEventListener(NAVIGATION_EVENT, listener)
  }
}

function currentHref(): string {
  return window.location.pathname + window.location.search
}

/** The server render has no location; the root is the honest stand-in. */
function serverHref(): string {
  return '/'
}

/**
 * How many in-app entries lie behind the current one. It travels in the
 * history entry's state, so Back, Forward and a reload all restore it; an
 * entry the app did not create (the first page load, a bookmark) has none
 * and counts as 0, which means Back would leave the application.
 */
const DEPTH_KEY = 'ytmdlDepth'

function entryDepth(state: unknown): number {
  if (state && typeof state === 'object' && DEPTH_KEY in state) {
    const depth = (state as Record<string, unknown>)[DEPTH_KEY]
    if (typeof depth === 'number' && Number.isFinite(depth) && depth >= 0) return depth
  }
  return 0
}

/** The history state for an entry at the given depth, keeping what else is stored there. */
export function historyStateWithDepth(state: unknown, depth: number): Record<string, unknown> {
  const base = state && typeof state === 'object' ? (state as Record<string, unknown>) : {}
  return { ...base, [DEPTH_KEY]: depth }
}

/** Navigates to a new path and notifies every subscriber. */
export function navigate(to: string, options: { replace?: boolean } = {}): void {
  if (to === currentHref()) return

  const fromPath = window.location.pathname
  const depth = entryDepth(window.history.state)
  if (options.replace) window.history.replaceState(historyStateWithDepth(null, depth), '', to)
  else window.history.pushState(historyStateWithDepth(null, depth + 1), '', to)

  window.dispatchEvent(new Event(NAVIGATION_EVENT))

  // A new page starts at its top, as a page load would. A changed query on
  // the same page (tab, filter, sort) keeps the position, and back/forward
  // are left to the browser's own scroll restoration.
  if (!options.replace && window.location.pathname !== fromPath) window.scrollTo({ top: 0, left: 0 })
}

/** True when the previous history entry is a page of this application. */
export function canGoBackInApp(): boolean {
  return entryDepth(window.history.state) > 0
}

/**
 * Back to the previous page when it belongs to the application, otherwise to
 * the fallback (replacing the current entry, so Back does not return here).
 */
export function goBack(fallback: string): void {
  if (canGoBackInApp()) window.history.back()
  else navigate(fallback, { replace: true })
}

export interface Location {
  /** The path without query, e.g. "/artists/UC123". */
  pathname: string
  /** The parsed query string. */
  params: URLSearchParams
  /** Path and query together, as it appears in the address bar. */
  href: string
}

/** The current location, re-rendering the caller whenever it changes. */
export function useLocation(): Location {
  const href = useSyncExternalStore(subscribe, currentHref, serverHref)

  return useMemo(() => {
    const [pathname = '/', search = ''] = href.split('?')
    return { pathname, params: new URLSearchParams(search), href }
  }, [href])
}

/** navigate() as a stable callback. */
export function useNavigate(): typeof navigate {
  return useCallback((to: string, options?: { replace?: boolean }) => navigate(to, options), [])
}

interface LinkProps extends Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> {
  href: string
  replace?: boolean
  children?: ReactNode
}

/**
 * An anchor that navigates without a reload. It stays a real link — the href
 * is set, so middle-click, "open in new tab" and the status bar all work — and
 * only intercepts the plain left click.
 */
export function Link({ href, replace, onClick, ...props }: LinkProps) {
  function handleClick(event: MouseEvent<HTMLAnchorElement>) {
    onClick?.(event)
    if (event.defaultPrevented) return
    // Let the browser handle anything that is not a plain left click.
    if (event.button !== 0) return
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return

    event.preventDefault()
    navigate(href, { replace })
  }

  return <a href={href} onClick={handleClick} {...props} />
}

/* ------------------------------------------------------------------- routes */

export type Route =
  /** "/" renders nothing itself: it resolves to the start of the last area. */
  | { name: 'home' }
  /** The start page of the listening area. */
  | { name: 'listenHome' }
  /** The start page of the download area: search, downloads, server. */
  | { name: 'downloadHome' }
  | { name: 'login' }
  | { name: 'profile' }
  | { name: 'users' }
  | { name: 'player' }
  | { name: 'mixes' }
  | { name: 'mix'; id: string }
  | { name: 'offline' }
  | { name: 'discover'; query: string }
  | { name: 'artist'; id: string; provider?: string }
  | { name: 'release'; id: string; provider?: string }
  | { name: 'downloads'; jobId?: string }
  | { name: 'library'; artistId?: string; view?: string; trackId?: string }
  | { name: 'libraryArtist'; id: string }
  | { name: 'libraryRelease'; id: string }
  | { name: 'playlists' }
  | { name: 'playlistDetail'; id: string }
  | { name: 'favorites' }
  | { name: 'subscriptions' }
  | { name: 'settings' }
  | { name: 'notFound'; pathname: string }

/** Maps a location onto the view that should render. */
export function matchRoute(location: Location): Route {
  const { pathname, params } = location
  const segments = pathname.split('/').filter(Boolean)
  const provider = params.get('provider') ?? undefined

  if (segments.length === 0) return { name: 'home' }

  const [first, second, third] = segments

  switch (first) {
    case 'offline':
      if(segments.length===1)return {name:'offline'}
      break
    case 'player':
      if (segments.length === 1) return { name: 'player' }
      break

    case 'mixes':
      if (segments.length === 1) return { name: 'mixes' }
      if (second && segments.length === 2) {
        return { name: 'mix', id: decodeURIComponent(second) }
      }
      break

    case 'login':
      if (segments.length === 1) return { name: 'login' }
      break

    case 'profile':
      if (segments.length === 1) return { name: 'profile' }
      break

    case 'users':
      if (segments.length === 1) return { name: 'users' }
      break

    case 'listen':
      if (segments.length === 1) return { name: 'listenHome' }
      break

    case 'search':
    // The former dashboard: bookmarks to it keep opening the same content.
    case 'dashboard':
      if (segments.length === 1) return { name: 'downloadHome' }
      break

    case 'discover':
      if (segments.length === 1) {
        return { name: 'discover', query: params.get('q') ?? '' }
      }
      break

    case 'artists':
      if (second && segments.length === 2) {
        return { name: 'artist', id: decodeURIComponent(second), provider }
      }
      break

    case 'releases':
      if (second && segments.length === 2) {
        return { name: 'release', id: decodeURIComponent(second), provider }
      }
      break

    case 'downloads':
      if (segments.length === 1) return { name: 'downloads' }
      if (second && segments.length === 2) {
        return { name: 'downloads', jobId: decodeURIComponent(second) }
      }
      break

    case 'library':
      if (segments.length === 1) {
        return {
          name: 'library',
          artistId: params.get('artist') ?? undefined,
          view: params.get('view') ?? undefined,
          trackId: params.get('track') ?? undefined,
        }
      }
      if (second === 'artists' && third && segments.length === 3) {
        return { name: 'libraryArtist', id: decodeURIComponent(third) }
      }
      if (second === 'releases' && third && segments.length === 3) {
        return { name: 'libraryRelease', id: decodeURIComponent(third) }
      }
      break

    case 'playlists':
      if (segments.length === 1) return { name: 'playlists' }
      if (second && segments.length === 2) {
        return { name: 'playlistDetail', id: decodeURIComponent(second) }
      }
      break

    case 'favorites':
      if (segments.length === 1) return { name: 'favorites' }
      break

    case 'subscriptions':
      if (segments.length === 1) return { name: 'subscriptions' }
      break

    case 'settings':
      if (second === 'server' && segments.length === 2) {
        return { name: 'settings' }
      }
      break
  }

  // "third" only exists to keep the destructuring honest about depth.
  void third
  return { name: 'notFound', pathname }
}

/* --------------------------------------------------------------- path helpers */

/** The canonical paths, so no route string is spelled out twice. */
export const paths = {
  /** Resolves to the start page of the area the user was last in. */
  home: () => '/',
  listenHome: () => '/listen',
  downloadHome: () => '/search',
  player: () => '/player',
  mixes: () => '/mixes',
  mix: (id: string) => `/mixes/${encodeURIComponent(id)}`,
  login: () => '/login',
  profile: () => '/profile',
  users: () => '/users',
  discover: (query?: string) =>
    query ? `/discover?q=${encodeURIComponent(query)}` : '/discover',
  artist: (id: string, provider?: string) =>
    provider
      ? `/artists/${encodeURIComponent(id)}?provider=${encodeURIComponent(provider)}`
      : `/artists/${encodeURIComponent(id)}`,
  release: (id: string, provider?: string) =>
    provider
      ? `/releases/${encodeURIComponent(id)}?provider=${encodeURIComponent(provider)}`
      : `/releases/${encodeURIComponent(id)}`,
  downloads: (jobId?: string) =>
    jobId ? `/downloads/${encodeURIComponent(jobId)}` : '/downloads',
  library: (queryOrArtistId?: string | Record<string, string | number | undefined>) => {
    if (!queryOrArtistId) return '/library'
    if (typeof queryOrArtistId === 'string') {
      return `/library?artist=${encodeURIComponent(queryOrArtistId)}`
    }
    const sp = new URLSearchParams()
    for (const [k, v] of Object.entries(queryOrArtistId)) {
      if (v !== undefined && v !== '') {
        sp.set(k, String(v))
      }
    }
    const qs = sp.toString()
    return qs ? `/library?${qs}` : '/library'
  },
  libraryArtist: (id: string) => `/library/artists/${encodeURIComponent(id)}`,
  libraryRelease: (id: string) => `/library/releases/${encodeURIComponent(id)}`,
  playlists: () => '/playlists',
  playlist: (id: string) => `/playlists/${encodeURIComponent(id)}`,
  favorites: () => '/favorites',
  subscriptions: () => '/subscriptions',
  settings: () => '/settings/server',
} as const
