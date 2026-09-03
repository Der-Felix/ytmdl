import { createContext } from 'react'
import type { LoginRequest, Role, SetupRequest, UserSummary } from '@/types/api'

export interface AuthContextValue {
  user: UserSummary | null
  role: Role | null
  isAdmin: boolean
  setupRequired: boolean
  loading: boolean
  error: string | null
  login: (req: LoginRequest) => Promise<UserSummary>
  setup: (req: SetupRequest) => Promise<UserSummary>
  logout: () => Promise<void>
  refresh: () => Promise<void>
  setUser: (u: UserSummary | null) => void
}

export const AuthContext = createContext<AuthContextValue | null>(null)
