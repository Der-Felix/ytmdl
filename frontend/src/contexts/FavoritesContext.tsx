import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import type { ReactNode } from 'react'

import { useOptionalAuth } from '@/hooks/useAuth'
import {
  favoriteTrack,
  listFavoriteIDs,
  unfavoriteTrack,
} from '@/lib/api/playlists'

interface FavoritesContextValue {
  favoriteIds: Set<string>
  isFavorite: (trackId: string) => boolean
  toggleFavorite: (trackId: string) => Promise<boolean>
  refresh: () => Promise<void>
  loading: boolean
}

export const FavoritesContext = createContext<FavoritesContextValue | null>(null)

export function FavoritesProvider({ children }: { children: ReactNode }) {
  const auth = useOptionalAuth()
  const [favoriteIds, setFavoriteIds] = useState<Set<string>>(new Set())
  const [loading, setLoading] = useState(false)

  const refresh = useCallback(async () => {
    if (!auth?.user) {
      setFavoriteIds(new Set())
      return
    }
    try {
      setLoading(true)
      const ids = await listFavoriteIDs()
      setFavoriteIds(new Set(ids))
    } catch {
      // Ignore background refresh failure
    } finally {
      setLoading(false)
    }
  }, [auth?.user])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const isFavorite = useCallback(
    (trackId: string) => {
      return favoriteIds.has(trackId)
    },
    [favoriteIds],
  )

  const toggleFavorite = useCallback(
    async (trackId: string): Promise<boolean> => {
      const currentlyFav = favoriteIds.has(trackId)
      const nextFav = !currentlyFav

      // Optimistic update
      setFavoriteIds((prev) => {
        const next = new Set(prev)
        if (nextFav) {
          next.add(trackId)
        } else {
          next.delete(trackId)
        }
        return next
      })

      try {
        if (nextFav) {
          await favoriteTrack(trackId)
        } else {
          await unfavoriteTrack(trackId)
        }
        return nextFav
      } catch (err) {
        // Rollback on failure
        setFavoriteIds((prev) => {
          const next = new Set(prev)
          if (currentlyFav) {
            next.add(trackId)
          } else {
            next.delete(trackId)
          }
          return next
        })
        throw err
      }
    },
    [favoriteIds],
  )

  const value = useMemo(
    () => ({
      favoriteIds,
      isFavorite,
      toggleFavorite,
      refresh,
      loading,
    }),
    [favoriteIds, isFavorite, toggleFavorite, refresh, loading],
  )

  return (
    <FavoritesContext.Provider value={value}>
      {children}
    </FavoritesContext.Provider>
  )
}

export type { FavoritesContextValue }

export function useFavorites(): FavoritesContextValue {
  const context = useContext(FavoritesContext)
  if (!context) {
    throw new Error('useFavorites must be used within a FavoritesProvider')
  }
  return context
}

export function useOptionalFavorites(): FavoritesContextValue | null {
  return useContext(FavoritesContext)
}
