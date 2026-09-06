import type { ComponentType } from 'react'
import {
  BellIcon,
  CompassIcon,
  DownloadIcon,
  LayoutDashboardIcon,
  LibraryIcon,
  LogOutIcon,
  RadioIcon,
  ServerCogIcon,
  UserIcon,
  UsersIcon,
} from 'lucide-react'

import { Button } from '@/components/ui/button'
import { useAuth } from '@/hooks/useAuth'
import { Link, paths } from '@/lib/router'
import type { Route } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/utils/format'

interface NavItem {
  label: string
  href: string
  icon: ComponentType<{ className?: string }>
  /** Which routes render this entry as the current one. */
  matches: Route['name'][]
}

const PRIMARY: NavItem[] = [
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
]

interface SidebarProps {
  route: Route
  /** Number of jobs currently queued or running; hidden when zero. */
  activeDownloads: number
  /** Closes the mobile drawer after a navigation. */
  onNavigate?: () => void
}

function Sidebar({ route, activeDownloads, onNavigate }: SidebarProps) {
  const { user, isAdmin, logout } = useAuth()

  const adminItems: NavItem[] = isAdmin
    ? [
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
    : []

  return (
    <div className="flex h-full flex-col gap-6 px-3 py-5">
      <Link
        href={paths.dashboard()}
        onClick={onNavigate}
        className="focus-ring flex items-center gap-2.5 rounded-xl px-2.5 py-1.5"
      >
        <Wordmark />
      </Link>

      <nav aria-label="Hauptnavigation" className="flex-1 overflow-y-auto space-y-6">
        <ul className="space-y-1">
          {PRIMARY.map((item) => (
            <li key={item.href}>
              <NavLink
                item={item}
                route={route}
                onNavigate={onNavigate}
                badge={
                  item.label === 'Downloads' && activeDownloads > 0
                    ? activeDownloads
                    : undefined
                }
              />
            </li>
          ))}
        </ul>

        {adminItems.length > 0 && (
          <div className="space-y-1">
            <div className="px-3 text-xs font-semibold uppercase text-muted-foreground tracking-wider">
              Administration
            </div>
            <ul className="space-y-1 pt-1">
              {adminItems.map((item) => (
                <li key={item.href}>
                  <NavLink
                    item={item}
                    route={route}
                    onNavigate={onNavigate}
                  />
                </li>
              ))}
            </ul>
          </div>
        )}
      </nav>

      {user && (
        <div className="space-y-2 border-t border-border pt-4">
          <NavLink
            item={{
              label: 'Profil & Sicherheit',
              href: paths.profile(),
              icon: UserIcon,
              matches: ['profile'],
            }}
            route={route}
            onNavigate={onNavigate}
          />

          <div className="flex items-center justify-between px-3 py-2 rounded-xl bg-white/3">
            <div className="min-w-0 pr-2">
              <div className="truncate text-xs font-medium text-foreground">
                {user.display_name || user.username}
              </div>
              <div className="text-[11px] text-muted-foreground capitalize">
                {user.role === 'admin' ? 'Administrator' : 'Benutzer'}
              </div>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void logout()}
              title="Abmelden"
              className="h-8 w-8 p-0 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
            >
              <LogOutIcon className="h-4 w-4" />
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

function NavLink({
  item,
  route,
  badge,
  onNavigate,
}: {
  item: NavItem
  route: Route
  badge?: number
  onNavigate?: () => void
}) {
  const active = item.matches.includes(route.name)
  const Icon = item.icon

  return (
    <Link
      href={item.href}
      onClick={onNavigate}
      aria-current={active ? 'page' : undefined}
      className={cn(
        'focus-ring group flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm font-medium transition-colors',
        active
          ? 'border border-primary/25 bg-accent text-foreground'
          : 'border border-transparent text-muted-foreground hover:bg-white/5 hover:text-foreground',
      )}
    >
      <Icon
        className={cn(
          'size-[1.125rem] shrink-0 transition-colors',
          active ? 'text-primary' : 'text-muted-foreground group-hover:text-foreground',
        )}
      />
      <span className="flex-1 truncate">{item.label}</span>
      {badge !== undefined && (
        <span className="min-w-5 rounded-md bg-primary px-1.5 py-0.5 text-center text-[0.6875rem] font-semibold text-primary-foreground tabular-nums">
          {formatNumber(badge)}
        </span>
      )}
    </Link>
  )
}

function Wordmark() {
  return (
    <>
      <img
        src="/logo-mark.png"
        alt=""
        aria-hidden="true"
        data-testid="brand-logo"
        className="size-8 shrink-0 object-contain"
      />
      <span className="font-heading text-[0.9375rem] font-semibold tracking-tight text-foreground">
        YTMDL
      </span>
    </>
  )
}

export { Sidebar }
