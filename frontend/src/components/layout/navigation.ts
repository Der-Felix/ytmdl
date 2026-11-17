import type { ComponentType } from 'react'
import {
  BellIcon,
  CompassIcon,
  DownloadIcon,
  HeartIcon,
  LayoutDashboardIcon,
  LibraryIcon,
  ListMusicIcon,
  RadioIcon,
  ServerCogIcon,
  UsersIcon,
  WifiOff,
} from 'lucide-react'

import { paths } from '@/lib/router'
import type { Route } from '@/lib/router'

export interface NavItem {
  label: string
  href: string
  icon: ComponentType<{ className?: string }>
  /** Which routes render this entry as the current one. */
  matches: Route['name'][]
}

/** The main views, in the order of the sidebar; the command menu reuses it. */
export const PRIMARY: NavItem[] = [
  {label:'Offline-Musik',href:'/offline',icon:WifiOff,matches:['offline']},
  {
    label: 'Dashboard',
    href: paths.dashboard(),
    icon: LayoutDashboardIcon,
    matches: ['dashboard'],
  },
  {
    label: 'Player',
    href: paths.player(),
    icon: RadioIcon,
    matches: ['player'],
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
]

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
