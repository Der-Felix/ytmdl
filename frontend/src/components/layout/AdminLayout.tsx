import { type ReactNode } from 'react'
import {
  ArrowLeftIcon,
  HardDriveIcon,
  LogOutIcon,
  RadioIcon,
  ServerCogIcon,
  UsersIcon,
  ZapIcon,
} from 'lucide-react'

import { ConnectionBadge } from '@/components/layout/ConnectionBadge'
import { Button } from '@/components/ui/button'
import { useAuth } from '@/hooks/useAuth'
import { Link, paths, type Route } from '@/lib/router'
import { cn } from '@/lib/utils'

interface AdminLayoutProps {
  route: Route
  children: ReactNode
}

interface AdminTab {
  id: string
  label: string
  href: string
  icon: React.ComponentType<{ className?: string }>
  active: (route: Route, hash: string) => boolean
}

export function AdminLayout({ route, children }: AdminLayoutProps) {
  const { user, logout } = useAuth()
  const currentHash = typeof window !== 'undefined' ? window.location.hash : ''

  const tabs: AdminTab[] = [
    {
      id: 'general',
      label: 'Allgemein & Diagnose',
      href: paths.settings() + '#health',
      icon: ServerCogIcon,
      active: (r, hash) => r.name === 'settings' && (hash === '#health' || hash === '' || hash === '#startup'),
    },
    {
      id: 'users',
      label: 'Benutzerverwaltung',
      href: paths.users(),
      icon: UsersIcon,
      active: (r) => r.name === 'users',
    },
    {
      id: 'downloads',
      label: 'Downloads & Automation',
      href: paths.settings() + '#downloads',
      icon: ZapIcon,
      active: (r, hash) => r.name === 'settings' && hash === '#downloads',
    },
    {
      id: 'storage',
      label: 'Speicher & Storage',
      href: paths.settings() + '#storage',
      icon: HardDriveIcon,
      active: (r, hash) => r.name === 'settings' && hash === '#storage',
    },
    {
      id: 'providers',
      label: 'Provider',
      href: paths.settings() + '#providers',
      icon: RadioIcon,
      active: (r, hash) => r.name === 'settings' && hash === '#providers',
    },
  ]

  return (
    <div className="min-h-dvh flex flex-col bg-background text-foreground">
      {/* Top Admin Header Bar */}
      <header className="sticky top-0 z-40 border-b border-border bg-[#0b0d14]/90 backdrop-blur-md">
        <div className="mx-auto flex h-14 w-full max-w-[1500px] items-center justify-between px-4 sm:px-6 lg:px-8">
          {/* Back link & Title */}
          <div className="flex items-center gap-4">
            <Link
              href={paths.dashboard()}
              className="group flex items-center gap-2 text-xs font-medium text-muted-foreground hover:text-foreground transition-colors"
            >
              <div className="flex size-7 items-center justify-center rounded-lg border border-border bg-white/3 transition-colors group-hover:bg-white/6 group-hover:border-primary/40">
                <ArrowLeftIcon className="size-3.5" />
              </div>
              <span className="hidden sm:inline">Zurück zu YTMDL</span>
            </Link>

            <div className="h-4 w-px bg-border" />

            <div className="flex items-center gap-2">
              <span className="font-heading text-base font-semibold tracking-tight text-foreground">
                Verwaltung
              </span>
              <span className="rounded-md border border-primary/30 bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold text-primary uppercase">
                Admin
              </span>
            </div>
          </div>

          {/* Connection badge & User control */}
          <div className="flex items-center gap-3">
            <ConnectionBadge />

            {user && (
              <div className="flex items-center gap-2 pl-2 border-l border-border">
                <div className="hidden text-right md:block">
                  <div className="text-xs font-medium text-foreground truncate max-w-[120px]">
                    {user.display_name || user.username}
                  </div>
                  <div className="text-[10px] text-muted-foreground">Administrator</div>
                </div>

                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={() => void logout()}
                  title="Abmelden"
                  className="h-8 w-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                >
                  <LogOutIcon className="size-4" />
                </Button>
              </div>
            )}
          </div>
        </div>

        {/* Horizontal Navigation Tabs */}
        <div className="border-t border-border/40 bg-white/1">
          <div className="mx-auto flex w-full max-w-[1500px] items-center gap-1 overflow-x-auto px-4 sm:px-6 lg:px-8 py-1.5 scrollbar-none">
            {tabs.map((tab) => {
              const isActive = tab.active(route, currentHash)
              const Icon = tab.icon
              return (
                <Link
                  key={tab.id}
                  href={tab.href}
                  className={cn(
                    'flex shrink-0 items-center gap-2 rounded-xl px-3.5 py-2 text-xs font-medium transition-colors',
                    isActive
                      ? 'border border-primary/30 bg-primary/10 text-primary shadow-xs'
                      : 'text-muted-foreground hover:bg-white/4 hover:text-foreground',
                  )}
                >
                  <Icon className={cn('size-4', isActive ? 'text-primary' : 'text-muted-foreground')} />
                  <span>{tab.label}</span>
                </Link>
              )
            })}
          </div>
        </div>
      </header>

      {/* Main Full-Width Admin Content */}
      <main className="flex-1 px-4 py-8 sm:px-6 lg:px-8">
        <div className="mx-auto w-full max-w-[1500px]">{children}</div>
      </main>
    </div>
  )
}
