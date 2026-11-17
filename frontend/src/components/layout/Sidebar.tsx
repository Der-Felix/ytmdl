import { LogOutIcon, SearchIcon, UserIcon } from 'lucide-react'

import { ADMIN, PRIMARY } from '@/components/layout/navigation'
import type { NavItem } from '@/components/layout/navigation'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/hooks/useAuth'
import { Link, paths } from '@/lib/router'
import type { Route } from '@/lib/router'
import { commandShortcutLabel } from '@/lib/shortcut'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/utils/format'

interface SidebarProps {
  route: Route
  /** Number of jobs currently queued or running; hidden when zero or undefined. */
  activeDownloads?: number
  /** Closes the mobile drawer after a navigation. */
  onNavigate?: () => void
  /** Opens the command menu; the button is left out when not given. */
  onOpenCommandMenu?: () => void
}

function Sidebar({ route, activeDownloads, onNavigate, onOpenCommandMenu }: SidebarProps) {
  const { user, isAdmin, logout } = useAuth()

  const adminItems: NavItem[] = isAdmin ? ADMIN : []

  return (
    <div className="flex h-full flex-col gap-6 px-3 py-5">
      <Link
        href={paths.dashboard()}
        onClick={onNavigate}
        className="focus-ring flex items-center gap-2.5 rounded-xl px-2.5 py-1.5"
      >
        <Wordmark />
      </Link>

      {onOpenCommandMenu && (
        <button
          type="button"
          onClick={onOpenCommandMenu}
          aria-label="Suchen und Befehle"
          aria-keyshortcuts="Control+K Meta+K"
          className="-mt-2 flex h-9 items-center gap-2.5 rounded-lg border border-border bg-muted px-3 text-sm text-muted-foreground transition-colors duration-(--duration-fast) hover:border-input hover:text-foreground"
        >
          <SearchIcon className="size-4 shrink-0" aria-hidden="true" />
          <span className="flex-1 text-left">Suchen …</span>
          <kbd className="font-mono text-[11px] text-muted-foreground">{commandShortcutLabel()}</kbd>
        </button>
      )}

      <nav aria-label="Hauptnavigation" className="flex-1 overflow-y-auto space-y-6">
        <ul className="space-y-1">
          {PRIMARY.map((item) => (
            <li key={item.href}>
              <NavLink
                item={item}
                route={route}
                onNavigate={onNavigate}
                badge={
                  item.label === 'Downloads' && activeDownloads !== undefined && activeDownloads > 0
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

          <div className="flex items-center justify-between px-3 py-2 rounded-xl bg-muted">
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
              aria-label="Abmelden"
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
          : 'border border-transparent text-muted-foreground hover:bg-muted hover:text-foreground',
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
        <span className="figures min-w-5 rounded-md bg-primary px-1.5 py-0.5 text-center text-[0.6875rem] font-semibold text-primary-foreground">
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
