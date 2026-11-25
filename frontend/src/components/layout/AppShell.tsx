import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Dialog as DialogPrimitive } from '@base-ui/react/dialog'
import { MenuIcon, SearchIcon, XIcon } from 'lucide-react'

import { AreaSwitch } from '@/components/layout/AreaSwitch'
import { CommandMenu } from '@/components/layout/CommandMenu'
import { ShortcutHelpDialog } from '@/components/layout/ShortcutHelpDialog'
import { Sidebar } from '@/components/layout/Sidebar'
import { MiniPlayer } from '@/components/player/MiniPlayer'
import { Button } from '@/components/ui/button'
import { usePlayerState } from '@/hooks/usePlayer'
import { AREA_START, useArea } from '@/lib/areas'
import { Link } from '@/lib/router'
import type { Route } from '@/lib/router'
import { cn } from '@/lib/utils'

interface AppShellProps {
  route: Route
  activeDownloads?: number
  children: ReactNode
}

/**
 * The frame every page renders inside: a fixed sidebar on desktop, a drawer
 * below it, and a minimal mobile header for navigation.
 *
 * In Now Playing (/player) mode, the shell provides an immersive full-viewport
 * music experience with custom player controls and fullscreen capabilities.
 */
function AppShell({ route, activeDownloads, children }: AppShellProps) {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [commandOpen, setCommandOpen] = useState(false)
  const { currentTrack } = usePlayerState()
  const area = useArea(route)
  const isPlayerRoute = route.name === 'player'
  const showMiniPlayer = Boolean(currentTrack) && !isPlayerRoute

  // A route change closes the drawer; leaving it open over the new page would
  // hide what the user just navigated to.
  useEffect(() => setDrawerOpen(false), [route])

  // The drawer is only meant for narrow widths. If the window grows past the
  // md breakpoint while it is open, close it; otherwise the page would stay
  // inert behind a drawer that is no longer visible.
  useEffect(() => {
    if (!drawerOpen || typeof window.matchMedia !== 'function') return
    const query = window.matchMedia('(min-width: 768px)')
    function onChange(event: MediaQueryListEvent) {
      if (event.matches) setDrawerOpen(false)
    }
    query.addEventListener('change', onChange)
    return () => query.removeEventListener('change', onChange)
  }, [drawerOpen])

  return (
    <div
      className={cn(
        'min-h-dvh',
        isPlayerRoute
          ? 'flex flex-col bg-background'
          : 'md:grid md:grid-cols-[15rem_1fr] xl:grid-cols-[16.5rem_1fr]',
      )}
    >
      <a
        href="#main"
        className="focus-ring sr-only focus:not-sr-only focus:fixed focus:top-4 focus:left-4 focus:z-100 focus:rounded-xl focus:bg-popover focus:px-4 focus:py-2 focus:text-sm"
      >
        Zum Inhalt springen
      </a>

      {/* Desktop sidebar (Hidden in player mode) */}
      {!isPlayerRoute && (
        <aside className="sticky top-0 hidden h-dvh border-r border-border md:block">
          <Sidebar route={route} area={area} activeDownloads={activeDownloads} onOpenCommandMenu={() => setCommandOpen(true)} />
        </aside>
      )}

      {/*
        Mobile drawer (hidden in player mode). A modal Base UI dialog: focus
        moves in on open, Tab stays inside, Escape and the backdrop close it,
        focus returns to the opening button and the page behind is inert.
      */}
      {!isPlayerRoute && (
        <DialogPrimitive.Root open={drawerOpen} onOpenChange={setDrawerOpen}>
          <DialogPrimitive.Portal>
            <DialogPrimitive.Backdrop className="fixed inset-0 z-50 bg-overlay/70 transition-opacity duration-(--duration-base) ease-(--ease-out) data-[starting-style]:opacity-0 data-[ending-style]:opacity-0 md:hidden" />
            <DialogPrimitive.Popup
              data-slot="mobile-nav-drawer"
              aria-label="Navigation"
              className="panel-blur fixed inset-y-0 left-0 z-50 w-[17rem] rounded-r-2xl border-y-0 border-l-0 bg-popover outline-none transition-transform duration-(--duration-slow) ease-(--ease-drawer) data-[starting-style]:-translate-x-full data-[ending-style]:-translate-x-full md:hidden"
            >
              <DialogPrimitive.Close
                render={
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label="Navigation schließen"
                    className="absolute top-4 right-3"
                  />
                }
              >
                <XIcon />
              </DialogPrimitive.Close>
              <Sidebar
                route={route}
                area={area}
                activeDownloads={activeDownloads}
                onNavigate={() => setDrawerOpen(false)}
                onOpenCommandMenu={() => {
                  setDrawerOpen(false)
                  setCommandOpen(true)
                }}
              />
            </DialogPrimitive.Popup>
          </DialogPrimitive.Portal>
        </DialogPrimitive.Root>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile header (Hidden on desktop and in player mode) */}
        {!isPlayerRoute && (
          <header
            className={cn(
              // Three columns with equal outer tracks keep the switch centred.
              'sticky top-0 z-40 grid h-14 grid-cols-[1fr_auto_1fr] items-center gap-2 border-b border-border px-3 sm:px-6',
              'panel-blur rounded-none border-x-0 border-t-0 bg-background md:hidden',
            )}
          >
            <div className="flex min-w-0 items-center gap-2">
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label="Navigation öffnen"
                aria-expanded={drawerOpen}
                onClick={() => setDrawerOpen(true)}
              >
                <MenuIcon />
              </Button>

              <Link
                href={AREA_START[area]}
                aria-label="YTMDL"
                className="focus-ring flex shrink-0 items-center gap-2 rounded-lg"
              >
                <img
                  src="/logo-mark.png"
                  alt=""
                  aria-hidden="true"
                  data-testid="brand-logo-mobile"
                  className="size-6 shrink-0 object-contain"
                />
                {/* The name only where it fits beside the switch. */}
                <span aria-hidden="true" className="hidden font-heading text-sm font-semibold text-foreground sm:inline">
                  YTMDL
                </span>
              </Link>
            </div>

            <AreaSwitch area={area} activeDownloads={activeDownloads} />

            <Button
              variant="ghost"
              size="icon-sm"
              className="justify-self-end"
              aria-label="Suchen und Befehle"
              onClick={() => setCommandOpen(true)}
            >
              <SearchIcon />
            </Button>
          </header>
        )}

        <main
          id="main"
          className={cn(
            'min-w-0 flex-1 transition-[padding]',
            isPlayerRoute
              ? 'p-0 flex flex-col'
              : showMiniPlayer
                ? 'px-4 pt-6 sm:px-6 sm:pt-8 lg:px-8 lg:pt-8 pb-32 sm:pb-32'
                : 'px-4 pt-6 sm:px-6 sm:pt-8 lg:px-8 lg:pt-8 pb-8',
          )}
        >
          <div className={isPlayerRoute ? 'w-full flex-1 flex flex-col' : 'mx-auto w-full max-w-[80rem]'}>
            {/* Desktop: the switch in its own row above every page, so it
                never overlaps a heading and every page moves down alike. */}
            {!isPlayerRoute && (
              <div className="mb-6 hidden justify-center md:flex lg:mb-8">
                <AreaSwitch area={area} activeDownloads={activeDownloads} />
              </div>
            )}
            {children}
          </div>
        </main>
      </div>

      {showMiniPlayer && <MiniPlayer />}

      <CommandMenu open={commandOpen} onOpenChange={setCommandOpen} />
      <ShortcutHelpDialog />
    </div>
  )
}

export { AppShell }
