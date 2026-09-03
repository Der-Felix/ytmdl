import { useContext } from 'react'

import { AuthContext, type AuthContextValue } from '@/contexts/auth-context'

export { AuthProvider } from '@/contexts/AuthContext'
export type { AuthContextValue } from '@/contexts/auth-context'

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return ctx
}

export function useOptionalAuth(): AuthContextValue | null {
  return useContext(AuthContext)
}
