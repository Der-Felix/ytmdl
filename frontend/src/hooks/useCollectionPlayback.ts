import { useCallback, useEffect, useRef, useState } from 'react'

import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import type { QueueContext } from '@/lib/audio/types'
import { notifyError, notifyInfo } from '@/lib/notify'
import type { LibraryTrack } from '@/types/api'

/**
 * The one request a play button may have in flight: starting a second card
 * cancels the first, so a late answer never takes over the queue.
 */
let pendingRequest: AbortController | null = null

export interface CollectionPlayback {
  /** This collection is what the queue was started from. */
  active: boolean
  /** Active and currently playing (or buffering). */
  playing: boolean
  /** Its tracks are being fetched. */
  loading: boolean
  /** Starts the collection, or toggles play/pause when it is already active. */
  toggle: () => void
}

/**
 * Plays a release or an artist from a card without opening it: fetches its
 * tracks on the first click and hands them to the player under `context`.
 * While the collection is the playing context the same button pauses and
 * resumes. Unmounting or starting another card aborts the request.
 */
export function useCollectionPlayback(
  context: QueueContext,
  load: (signal: AbortSignal) => Promise<LibraryTrack[]>,
  mode: 'album' | 'artist' = 'album',
): CollectionPlayback {
  const { queueContext, status, currentTrack } = usePlayerState()
  const { playAlbum, playArtist, togglePlayPause } = usePlayerActions()
  const [loading, setLoading] = useState(false)
  const own = useRef<AbortController | null>(null)

  useEffect(
    () => () => {
      own.current?.abort()
      if (pendingRequest === own.current) pendingRequest = null
    },
    [],
  )

  const active =
    currentTrack !== null && queueContext?.kind === context.kind && queueContext.id === context.id
  const playing = active && (status === 'playing' || status === 'buffering')

  const { kind, id, title } = context
  const toggle = useCallback(() => {
    if (active) {
      togglePlayPause()
      return
    }
    pendingRequest?.abort()
    const request = new AbortController()
    pendingRequest = request
    own.current = request
    setLoading(true)
    load(request.signal)
      .then((tracks) => {
        if (request.signal.aborted) return
        if (tracks.length === 0) {
          notifyInfo('Nichts zum Abspielen', `In „${title}“ liegt noch kein Titel in der Mediathek.`)
          return
        }
        const ctx: QueueContext = { kind, id, title }
        if (mode === 'artist') playArtist(tracks, false, ctx)
        else playAlbum(tracks, 0, ctx)
      })
      .catch((error: unknown) => {
        if (request.signal.aborted) return
        notifyError(`„${title}“ konnte nicht abgespielt werden`, error)
      })
      .finally(() => {
        if (pendingRequest === request) pendingRequest = null
        if (own.current === request) {
          own.current = null
          setLoading(false)
        }
      })
  }, [active, kind, id, title, load, mode, playAlbum, playArtist, togglePlayPause])

  return { active, playing, loading, toggle }
}
