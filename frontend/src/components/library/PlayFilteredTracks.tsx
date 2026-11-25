import { useEffect, useRef, useState } from 'react'
import { Loader2, Play, Shuffle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { usePlayerActions } from '@/hooks/usePlayer'
import type { QueueContext } from '@/lib/audio/types'
import { loadFilteredTracks, PLAY_ALL_CAP } from '@/lib/library-play-all'
import type { TrackQuery } from '@/lib/library-play-all'
import { notifyError } from '@/lib/notify'
import { formatNumber } from '@/lib/utils/format'

/**
 * "Alle abspielen" and "Zufällig" above the library's title list: they play
 * what the current filter returns across all pages, not only the page shown,
 * up to PLAY_ALL_CAP titles.
 */
export function PlayFilteredTracks({ query, total, context }: { query: TrackQuery; total: number; context: QueueContext }) {
  const { playAlbum, playShuffled } = usePlayerActions()
  const [busy, setBusy] = useState<'play' | 'shuffle' | null>(null)
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])

  const start = async (mode: 'play' | 'shuffle') => {
    request.current?.abort()
    const controller = new AbortController()
    request.current = controller
    setBusy(mode)
    try {
      const tracks = await loadFilteredTracks(query, controller.signal)
      if (controller.signal.aborted || tracks.length === 0) return
      if (mode === 'shuffle') playShuffled(tracks, context)
      else playAlbum(tracks, 0, context)
    } catch (error) {
      if (!controller.signal.aborted) notifyError('Die Titel konnten nicht geladen werden', error)
    } finally {
      if (request.current === controller) {
        request.current = null
        setBusy(null)
      }
    }
  }

  const capped = total > PLAY_ALL_CAP
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="default" size="sm" disabled={busy !== null || total === 0} onClick={() => void start('play')}>
        {busy === 'play' ? <Loader2 className="animate-spin" aria-hidden /> : <Play className="fill-current" aria-hidden />}
        Alle abspielen
      </Button>
      <Button size="sm" disabled={busy !== null || total === 0} onClick={() => void start('shuffle')}>
        {busy === 'shuffle' ? <Loader2 className="animate-spin" aria-hidden /> : <Shuffle aria-hidden />}
        Zufällig
      </Button>
      <span className="figures text-xs text-muted-foreground">
        {capped
          ? `Spielt die ersten ${formatNumber(PLAY_ALL_CAP)} von ${formatNumber(total)} Titeln`
          : `${formatNumber(total)} Titel`}
      </span>
    </div>
  )
}
