import { useMemo } from 'react'

import { AdminLayout } from '@/components/layout/AdminLayout'
import { AppShell } from '@/components/layout/AppShell'
import { PlayerProvider } from '@/contexts/PlayerContext'
import { AuthProvider, useAuth } from '@/hooks/useAuth'
import { useJobs } from '@/hooks/useJobs'
import { section } from '@/lib/api/jobs'
import { matchRoute, useLocation } from '@/lib/router'
import { Artist } from '@/pages/Artist'
import { Dashboard } from '@/pages/Dashboard'
import { Discover } from '@/pages/Discover'
import { Downloads } from '@/pages/Downloads'
import { Library } from '@/pages/Library'
import { LibraryArtist } from '@/pages/LibraryArtist'
import { LibraryRelease } from '@/pages/LibraryRelease'
import { Login } from '@/pages/Login'
import { NotFound } from '@/pages/NotFound'
import { NowPlaying } from '@/pages/NowPlaying'
import { Profile } from '@/pages/Profile'
import { Release } from '@/pages/Release'
import { Settings } from '@/pages/Settings'
import { Subscriptions } from '@/pages/Subscriptions'
import { Users } from '@/pages/Users'

function AppContent() {
  const location = useLocation()
  const route = useMemo(() => matchRoute(location), [location])
  const { user, loading, setupRequired, isAdmin } = useAuth()
  const { state: jobsState } = useJobs({ limit: 50 })

  const activeDownloads = useMemo(() => {
    if (jobsState.status !== 'success') return 0
    return jobsState.data.filter((j) => {
      const s = section(j)
      return s === 'active' || s === 'queued'
    }).length
  }, [jobsState])

  if (loading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background text-foreground">
        <div className="flex flex-col items-center gap-3">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
          <span className="text-xs text-muted-foreground">Wird geladen...</span>
        </div>
      </div>
    )
  }

  if (!user || setupRequired) {
    return (
      <div className="min-h-screen bg-background text-foreground">
        <Login />
      </div>
    )
  }

  // Admin routes render in dedicated AdminLayout without standard app sidebar
  if (isAdmin && (route.name === 'users' || route.name === 'settings')) {
    return (
      <AdminLayout route={route}>
        {route.name === 'users' && <Users />}
        {route.name === 'settings' && <Settings />}
      </AdminLayout>
    )
  }

  return (
    <AppShell route={route} activeDownloads={activeDownloads}>
      {route.name === 'dashboard' && <Dashboard />}
      {route.name === 'login' && <Dashboard />}
      {route.name === 'player' && <NowPlaying />}
      {route.name === 'profile' && <Profile />}
      {route.name === 'users' && <NotFound pathname="/users" />}
      {route.name === 'discover' && <Discover query={route.query} />}
      {route.name === 'artist' && <Artist id={route.id} provider={route.provider} />}
      {route.name === 'release' && <Release id={route.id} provider={route.provider} />}
      {route.name === 'downloads' && <Downloads jobId={route.jobId} />}
      {route.name === 'library' && <Library />}
      {route.name === 'libraryArtist' && <LibraryArtist id={route.id} />}
      {route.name === 'libraryRelease' && <LibraryRelease id={route.id} />}
      {route.name === 'subscriptions' && <Subscriptions />}
      {route.name === 'settings' && (isAdmin ? <Settings /> : <NotFound pathname="/settings/server" />)}
      {route.name === 'notFound' && <NotFound pathname={route.pathname} />}
    </AppShell>
  )
}

function App() {
  return (
    <AuthProvider>
      <PlayerProvider>
        <AppContent />
      </PlayerProvider>
    </AuthProvider>
  )
}

export default App
