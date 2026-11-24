/**
 * The page name each route shows in the browser tab, beside the area table in
 * areas.ts. A Record over the route names, so a new route without a title does
 * not compile. Detail pages replace theirs with the entity's own name once it
 * has loaded (usePageTitle).
 */

import type { Route } from '@/lib/router'

export const ROUTE_TITLES: Record<Route['name'], string> = {
  home: 'Start',
  listenHome: 'Hören',
  downloadHome: 'Suche',
  login: 'Anmelden',
  profile: 'Profil & Sicherheit',
  users: 'Benutzerverwaltung',
  player: 'Player',
  mixes: 'Für dich',
  mix: 'Mix',
  offline: 'Offline-Musik',
  discover: 'Entdecken',
  artist: 'Künstler',
  release: 'Release',
  downloads: 'Downloads',
  library: 'Bibliothek',
  libraryArtist: 'Künstler',
  libraryRelease: 'Album',
  playlists: 'Playlists',
  playlistDetail: 'Playlist',
  favorites: 'Favoriten',
  subscriptions: 'Abonnements',
  settings: 'Servereinstellungen',
  notFound: 'Seite nicht gefunden',
}

/** The product name every page title ends with. */
export const APP_NAME = 'YTMDL'
