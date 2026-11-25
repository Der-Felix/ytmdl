import type { ComponentType } from 'react'

import { BOTTOM_NAV } from '@/components/layout/navigation'
import type { Area } from '@/lib/areas'
import { Link } from '@/lib/router'
import type { Route } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/utils/format'

interface BottomNavProps {
  route: Route
  area: Area
  /** Open download jobs; counted on the Downloads entry, as in the sidebar. */
  activeDownloads?: number
  /** Opens the command menu; the search entry of the listening area. */
  onOpenCommandMenu: () => void
}

const entryClass =
  'focus-ring group flex h-full w-full flex-col items-center justify-center gap-1 rounded-xl text-[0.6875rem] leading-none font-medium transition-colors duration-(--duration-fast)'

/**
 * The bar at the bottom of phones: the main destinations of the current area
 * within thumb reach. Below the md breakpoint only; wider screens have the
 * sidebar. It carries the bottom safe area, and the player bar docks on top.
 */
function BottomNav({ route, area, activeDownloads, onOpenCommandMenu }: BottomNavProps) {
  const pending = activeDownloads !== undefined && activeDownloads > 0 ? activeDownloads : undefined

  return (
    <nav
      aria-label="Schnellzugriff"
      data-slot="bottom-nav"
      className="fixed inset-x-0 bottom-0 z-40 border-t border-border bg-surface pr-[env(safe-area-inset-right)] pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)] md:hidden"
    >
      <ul className="mx-auto flex h-14 max-w-lg items-stretch px-1">
        {BOTTOM_NAV[area].map((entry) => {
          if (entry.kind === 'search') {
            return (
              <li key="search" className="flex min-w-0 flex-1 p-0.5">
                <button
                  type="button"
                  onClick={onOpenCommandMenu}
                  aria-haspopup="dialog"
                  className={cn(entryClass, 'text-muted-foreground')}
                >
                  <EntryIcon icon={entry.icon} active={false} />
                  <span className="max-w-full truncate px-0.5">{entry.label}</span>
                </button>
              </li>
            )
          }

          const active = entry.matches.includes(route.name)
          const count = entry.label === 'Downloads' ? pending : undefined
          return (
            <li key={entry.href} className="flex min-w-0 flex-1 p-0.5">
              <Link
                href={entry.href}
                aria-current={active ? 'page' : undefined}
                className={cn(entryClass, active ? 'text-foreground' : 'text-muted-foreground')}
              >
                <EntryIcon icon={entry.icon} active={active} count={count} />
                <span className="max-w-full truncate px-0.5">{entry.label}</span>
                {count !== undefined && (
                  <span className="sr-only">
                    {`, ${formatNumber(count)} ${count === 1 ? 'offener Download' : 'offene Downloads'}`}
                  </span>
                )}
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}

/** The icon on a pill that marks the current page, as the sidebar does. */
function EntryIcon({
  icon: Icon,
  active,
  count,
}: {
  icon: ComponentType<{ className?: string }>
  active: boolean
  count?: number
}) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'relative flex h-7 w-12 items-center justify-center rounded-full border transition-colors duration-(--duration-fast)',
        active ? 'border-primary/25 bg-accent text-primary' : 'border-transparent',
      )}
    >
      <Icon className="size-5" />
      {count !== undefined && (
        <span className="figures absolute -top-1 right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[0.625rem] font-semibold text-primary-foreground">
          {formatNumber(count)}
        </span>
      )}
    </span>
  )
}

export { BottomNav }
