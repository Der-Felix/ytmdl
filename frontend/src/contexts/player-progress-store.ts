import { createContext, useContext, useSyncExternalStore } from 'react'

export interface PlayerProgress {
  currentTime: number
  duration: number
}

/**
 * The playback position as a subscribable store, next to the progress
 * context. Readers that only need a value derived from the position, such as
 * the active lyric line, select it here and redraw only when it changes.
 */
export interface ProgressStore {
  get: () => PlayerProgress
  set: (value: PlayerProgress) => void
  subscribe: (listener: () => void) => () => void
}

export function createProgressStore(initial: PlayerProgress): ProgressStore {
  let current = initial
  const listeners = new Set<() => void>()
  return {
    get: () => current,
    set: (value) => {
      if (value === current) return
      current = value
      listeners.forEach((listener) => listener())
    },
    subscribe: (listener) => {
      listeners.add(listener)
      return () => listeners.delete(listener)
    },
  }
}

export const PlayerProgressStoreContext = createContext<ProgressStore | null>(null)

/**
 * A value derived from the playback position. The caller redraws only when
 * the derived value changes, so the selector must return a primitive or a
 * stable reference.
 */
export function usePlayerProgressSelector<T>(selector: (progress: PlayerProgress) => T): T {
  const store = useContext(PlayerProgressStoreContext)
  if (!store) {
    throw new Error('usePlayerProgressSelector must be used within a PlayerProvider')
  }
  return useSyncExternalStore(store.subscribe, () => selector(store.get()))
}
