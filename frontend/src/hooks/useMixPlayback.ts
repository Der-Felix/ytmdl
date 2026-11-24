import { useCallback, useEffect, useRef, useState } from 'react'
import type { MouseEvent } from 'react'

import { usePlayerActions } from '@/hooks/usePlayer'
import { personalMix } from '@/lib/api/mixes'
import type { PersonalMix } from '@/lib/api/mixes'

/**
 * Plays a mix from its card: loads its tracks, then hands them to the player.
 * Leaving the page or starting another mix aborts the request, so a late
 * answer never starts playback on its own.
 */
export function useMixPlayback() {
  const { playAlbum } = usePlayerActions()
  const [playing, setPlaying] = useState<string | null>(null)
  const [playError, setPlayError] = useState('')

  const playRequest = useRef<AbortController | null>(null)
  useEffect(() => () => playRequest.current?.abort(), [])

  const play = useCallback(
    async (event: MouseEvent, mix: PersonalMix) => {
      event.preventDefault()
      event.stopPropagation()
      playRequest.current?.abort()
      const request = new AbortController()
      playRequest.current = request
      setPlayError('')
      setPlaying(mix.id)
      try {
        const detail = await personalMix(mix.id, request.signal)
        if (request.signal.aborted) return
        if (detail.tracks.length > 0) playAlbum(detail.tracks)
      } catch (error) {
        if (request.signal.aborted) return
        setPlayError(error instanceof Error ? error.message : 'Mix konnte nicht geladen werden.')
      } finally {
        if (playRequest.current === request) {
          playRequest.current = null
          setPlaying(null)
        }
      }
    },
    [playAlbum],
  )

  const onPlay = useCallback((event: MouseEvent, mix: PersonalMix) => void play(event, mix), [play])

  return { playing, playError, onPlay }
}
