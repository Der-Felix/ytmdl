import { useEffect, useRef } from 'react'

import { toast } from '@/lib/toast-manager'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'

/**
 * Reports a playback failure once, wherever the listener is in the app. The
 * player keeps the error in its state; this only turns a new error into a
 * toast with a retry, and closes it again when the track changes.
 */
export function PlaybackErrorToast() {
  const { error, currentTrack } = usePlayerState()
  const { playTrack } = usePlayerActions()
  const shown = useRef<string | null>(null)

  useEffect(() => {
    if (shown.current) {
      toast.close(shown.current)
      shown.current = null
    }
    if (!error || !currentTrack) return
    const track = currentTrack
    shown.current = toast.add({
      type: 'error',
      priority: 'high',
      title: 'Wiedergabe fehlgeschlagen',
      description: `${track.title}: ${error}`,
      timeout: 0,
      actionProps: {
        children: 'Erneut versuchen',
        onClick: () => playTrack(track),
      },
    })
  }, [error, currentTrack, playTrack])

  return null
}
