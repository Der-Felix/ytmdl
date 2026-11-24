import { DownloadIcon, HeadphonesIcon } from 'lucide-react'
import type { ComponentType } from 'react'

import { AREA_START } from '@/lib/areas'
import type { Area } from '@/lib/areas'
import { Link } from '@/lib/router'
import { cn } from '@/lib/utils'
import { formatNumber } from '@/lib/utils/format'

const SEGMENTS: { area: Area; label: string; icon: ComponentType<{ className?: string }> }[] = [
  { area: 'listen', label: 'Hören', icon: HeadphonesIcon },
  { area: 'download', label: 'Laden', icon: DownloadIcon },
]

interface AreaSwitchProps {
  area: Area
  /** Open download jobs; shown on "Laden" while the user is listening. */
  activeDownloads?: number
  /** Closes an overlay after a navigation, as the sidebar links do. */
  onNavigate?: () => void
  className?: string
}

/**
 * Hören | Laden: switches the application between its two areas.
 *
 * Each segment navigates to the start page of its area, so the segments are
 * links rather than tabs: Tab reaches both, Enter follows one, and the
 * selected one is marked with aria-current. A count on "Laden" tells a
 * listener that downloads are running without leaving the area.
 */
function AreaSwitch({ area, activeDownloads, onNavigate, className }: AreaSwitchProps) {
  const pending = area === 'listen' && activeDownloads !== undefined && activeDownloads > 0 ? activeDownloads : undefined

  return (
    <nav aria-label="Bereich" className={cn('inline-flex', className)}>
      <ul className="flex items-center gap-1 rounded-xl border border-border bg-surface p-1">
        {SEGMENTS.map(({ area: segment, label, icon: Icon }) => {
          const selected = segment === area
          const count = segment === 'download' ? pending : undefined
          return (
            <li key={segment}>
              <Link
                href={AREA_START[segment]}
                onClick={onNavigate}
                aria-current={selected ? 'true' : undefined}
                data-area={segment}
                className={cn(
                  'focus-ring flex h-10 items-center gap-2 rounded-lg border px-3.5 text-sm font-medium transition-colors duration-(--duration-fast) md:h-9',
                  selected
                    ? 'border-primary/25 bg-accent text-foreground'
                    : 'border-transparent text-muted-foreground hover:text-foreground',
                )}
              >
                <Icon
                  aria-hidden="true"
                  className={cn('hidden size-4 shrink-0 sm:block', selected ? 'text-primary' : 'text-muted-foreground')}
                />
                <span>{label}</span>
                {count !== undefined && (
                  <>
                    {/* A dot where there is no room for the number. */}
                    <span aria-hidden="true" className="size-1.5 rounded-full bg-primary sm:hidden" />
                    <span
                      aria-hidden="true"
                      className="figures hidden rounded-md bg-muted px-1.5 py-0.5 text-[0.6875rem] font-semibold text-muted-foreground sm:inline"
                    >
                      {formatNumber(count)}
                    </span>
                    <span className="sr-only">
                      {`, ${formatNumber(count)} ${count === 1 ? 'offener Download' : 'offene Downloads'}`}
                    </span>
                  </>
                )}
              </Link>
            </li>
          )
        })}
      </ul>
    </nav>
  )
}

export { AreaSwitch }
