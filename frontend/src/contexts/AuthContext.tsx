import {
  useCallback,
  useEffect,
  useState,
  type ReactNode,
} from 'react'

import { getAuthStatus, login as apiLogin, logout as apiLogout, setupAdmin } from '@/lib/api/auth'
import { errorMessage } from '@/lib/api/client'
import type { LoginRequest, SetupRequest, UserSummary } from '@/types/api'
import { AuthContext, type AuthContextValue } from './auth-context'

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<UserSummary | null>(null)
  const [setupRequired, setSetupRequired] = useState<boolean>(false)
  const [loading, setLoading] = useState<boolean>(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      setLoading(true)
      setError(null)
      const status = await getAuthStatus()
      setSetupRequired(status.setup_required)
      if (status.authenticated && status.user) {
        setUser(status.user)
      } else {
        setUser(null)
      }
    } catch (err) {
      setError(errorMessage(err))
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    let active = true
    getAuthStatus()
      .then((status) => {
        if (!active) return
        setSetupRequired(status.setup_required)
        if (status.authenticated && status.user) {
          setUser(status.user)
        } else {
          setUser(null)
        }
        setError(null)
      })
      .catch((err) => {
        if (!active) return
        setError(errorMessage(err))
        setUser(null)
      })
      .finally(() => {
        if (active) setLoading(false)
      })

    return () => {
      active = false
    }
  }, [])

  const login = useCallback(
    async (req: LoginRequest): Promise<UserSummary> => {
      const u = await apiLogin(req)
      setUser(u)
      setSetupRequired(false)
      return u
    },
    [],
  )

  const setup = useCallback(
    async (req: SetupRequest): Promise<UserSummary> => {
      const u = await setupAdmin(req)
      setUser(u)
      setSetupRequired(false)
      return u
    },
    [],
  )

  const logout = useCallback(async () => {
    try {
      await apiLogout()
    } finally {
      setUser(null)
      void refresh()
    }
  }, [refresh])

  const role = user?.role ?? null
  const isAdmin = role === 'admin'

  const value: AuthContextValue = {
    user,
    role,
    isAdmin,
    setupRequired,
    loading,
    error,
    login,
    setup,
    logout,
    refresh,
    setUser,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
