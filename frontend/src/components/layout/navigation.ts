import type { ComponentType } from 'react'
import {
  BellIcon,
  CompassIcon,
  DownloadIcon,
  HeadphonesIcon,
  HeartIcon,
  LibraryIcon,
  ListMusicIcon,
  RadioIcon,
  SearchIcon,
  ServerCogIcon,
  SparklesIcon,
  UsersIcon,
  WifiOff,
} from 'lucide-react'

import type { Area } from '@/lib/areas'
import { paths } from '@/lib/router'
import type { Route } from '@/lib/router'

export interface NavItem {
  label: string
  href: string
  icon: ComponentType<{ className?: string }>
  /** Which routes render this entry as the current one. */
  matches: Route['name'][]
  /** Further words the command menu finds the entry by. */
  keywords?: string[]
}

/** The listening area, in the order of the sidebar. */
export const LISTEN_NAV: NavItem[] = [
  {
    label: 'Für dich',
    href: paths.mixes(),
    icon: SparklesIcon,
    matches: ['mixes', 'mix'],
  },
  {
    label: 'Player',
    href: paths.player(),
    icon: RadioIcon,
    matches: ['player'],
  },
  {
    label: 'Bibliothek',
    href: paths.library(),
    icon: LibraryIcon,
    matches: ['library', 'libraryArtist', 'libraryRelease'],
  },
  {
    label: 'Playlists',
    href: paths.playlists(),
    icon: ListMusicIcon,
    matches: ['playlists', 'playlistDetail'],
  },
  {
    label: 'Favoriten',
    href: paths.favorites(),
    icon: HeartIcon,
    matches: ['favorites'],
  },
  // A plain link on purpose: /offline renders outside the signed-in shell, so
  // it works without a server connection.
  {
    label: 'Offline-Musik',
    href: '/offline',
    icon: WifiOff,
    matches: ['offline'],
  },
]

/** The download area, in the order of the sidebar; "Suche" is its start page. */
export const DOWNLOAD_NAV: NavItem[] = [
  {
    label: 'Suche',
    href: paths.downloadHome(),
    icon: SearchIcon,
    matches: ['downloadHome'],
    keywords: ['start', 'startseite', 'dashboard', 'laden'],
  },
  {
    label: 'Entdecken',
    href: paths.discover(),
    icon: CompassIcon,
    matches: ['discover', 'artist', 'release'],
  },
  {
    label: 'Downloads',
    href: paths.downloads(),
    icon: DownloadIcon,
    matches: ['downloads'],
  },
  {
    label: 'Abonnements',
    href: paths.subscriptions(),
    icon: BellIcon,
    matches: ['subscriptions'],
  },
]

/** The sidebar entries of each area. */
export const AREA_NAV: Record<Area, NavItem[]> = {
  listen: LISTEN_NAV,
  download: DOWNLOAD_NAV,
}

/**
 * The listening start page. The sidebar leaves it out (the switch and the
 * wordmark lead there); the command menu offers it.
 */
export const LISTEN_HOME: NavItem = {
  label: 'Übersicht',
  href: paths.listenHome(),
  icon: HeadphonesIcon,
  matches: ['listenHome'],
  keywords: ['start', 'startseite', 'hören', 'weiterhören'],
}

/** Views only administrators can open; the routes enforce the same split. */
export const ADMIN: NavItem[] = [
  {
    label: 'Benutzerverwaltung',
    href: paths.users(),
    icon: UsersIcon,
    matches: ['users'],
  },
  {
    label: 'Servereinstellungen',
    href: paths.settings(),
    icon: ServerCogIcon,
    matches: ['settings'],
  },
]
