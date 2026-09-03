import { useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { MenuIcon, XIcon } from 'lucide-react'

import { ConnectionBadge } from '@/components/layout/ConnectionBadge'
import { Sidebar } from '@/components/layout/Sidebar'
import { MiniPlayer } from '@/components/player/MiniPlayer'
import { Button } from '@/components/ui/button'
import { usePlayer } from '@/hooks/usePlayer'
import type { Route } from '@/lib/router'
import { cn } from '@/lib/utils'

interface AppShellProps {
  route: Route
  activeDownloads: number
  children: ReactNode
}

/**
 * The frame every page renders inside: a fixed sidebar on desktop, a drawer
 * below it, and a header that carries the mobile menu and the live-stream
 * state.
 *
 * In Now Playing (/player) mode, the shell provides an immersive full-viewport
 * music experience with custom player controls and fullscreen capabilities.
 */
function AppShell({ route, activeDownloads, children }: AppShellProps) {
  const [drawerOpen, setDrawerOpen] = useState(false)
  const { currentTrack } = usePlayer()
  const isPlayerRoute = route.name === 'player'
  const showMiniPlayer = Boolean(currentTrack) && !isPlayerRoute

  // A route change closes the drawer; leaving it open over the new page would
  // hide what the user just navigated to.
  useEffect(() => setDrawerOpen(false), [route])

  // Escape closes the drawer, matching every other overlay in the application.
  useEffect(() => {
    if (!drawerOpen) return
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === 'Escape') setDrawerOpen(false)
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [drawerOpen])

  return (
    <div
      className={cn(
        'min-h-dvh',
        isPlayerRoute
          ? 'flex flex-col bg-[#07090e]'
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
          <Sidebar route={route} activeDownloads={activeDownloads} />
        </aside>
      )}

      {/* Mobile drawer (Hidden in player mode) */}
      {!isPlayerRoute && drawerOpen && (
        <div className="fixed inset-0 z-50 md:hidden">
          <button
            type="button"
            aria-label="Navigation schließen"
            onClick={() => setDrawerOpen(false)}
            className="absolute inset-0 bg-[#05060b]/70 backdrop-blur-sm"
          />
          <aside className="panel-blur absolute inset-y-0 left-0 w-[17rem] rounded-r-2xl border-y-0 border-l-0 bg-[#0f1220]/95">
            <Sidebar
              route={route}
              activeDownloads={activeDownloads}
              onNavigate={() => setDrawerOpen(false)}
            />
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Top header (Hidden in player mode) */}
        {!isPlayerRoute && (
          <header
            className={cn(
              'sticky top-0 z-40 flex h-14 items-center gap-3 border-b border-border px-4 sm:px-6 lg:px-8',
              'panel-blur rounded-none border-x-0 border-t-0 bg-[#0b0d14]/70',
            )}
          >
            <Button
              variant="ghost"
              size="icon-sm"
              className="md:hidden"
              aria-label={drawerOpen ? 'Navigation schließen' : 'Navigation öffnen'}
              aria-expanded={drawerOpen}
              onClick={() => setDrawerOpen((open) => !open)}
            >
              {drawerOpen ? <XIcon /> : <MenuIcon />}
            </Button>

            <span className="font-heading text-sm font-semibold text-foreground md:hidden">
              YTMDL
            </span>

            <div className="ml-auto flex items-center gap-4">
              <ConnectionBadge />
            </div>
          </header>
        )}

        <main
          id="main"
          className={cn(
            'min-w-0 flex-1 transition-all',
            isPlayerRoute
              ? 'p-0 flex flex-col'
              : showMiniPlayer
                ? 'px-4 py-6 sm:px-6 lg:px-8 pb-28 sm:pb-32'
                : 'px-4 py-6 sm:px-6 lg:px-8 pb-8',
          )}
        >
          <div className={isPlayerRoute ? 'w-full flex-1 flex flex-col' : 'mx-auto w-full max-w-[80rem]'}>
            {children}
          </div>
        </main>
      </div>

      {showMiniPlayer && <MiniPlayer />}
    </div>
  )
}

export { AppShell }
