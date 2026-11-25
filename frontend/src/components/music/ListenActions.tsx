import { ListPlus, ListStart, Pause, Play, Shuffle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import type { QueueContext } from '@/lib/audio/types'
import type { LibraryTrack } from '@/types/api'

interface ListenActionsProps {
  tracks: LibraryTrack[]
  context: QueueContext
  /** Accessible name of the play button, e.g. "Album abspielen"; it reads "Abspielen". */
  playLabel: string
  /** An artist plays like the artist page always did (playArtist), a release in order. */
  mode?: 'album' | 'artist'
  /** Also offer "Als Nächstes" and "Zur Warteschlange". */
  queueActions?: boolean
}

/**
 * The action row of a release or artist page: one primary "Abspielen", which
 * pauses and resumes while this page is what plays, then "Zufällig" and the
 * queue actions. One primary per view; the others are outline buttons.
 *
 * Inside a narrow container (an ancestor with the @container class) the
 * secondary buttons drop their labels and keep icon and tooltip, so the row
 * stays one line instead of wrapping one button per line.
 */
export function ListenActions({ tracks, context, playLabel, mode = 'album', queueActions = true }: ListenActionsProps) {
  const { queueContext, status, currentTrack } = usePlayerState()
  const { playAlbum, playArtist, playShuffled, playNext, addToQueue, togglePlayPause } = usePlayerActions()
  const empty = tracks.length === 0

  const active = currentTrack !== null && queueContext?.kind === context.kind && queueContext.id === context.id
  const playing = active && (status === 'playing' || status === 'buffering')

  const play = () => {
    if (active) return togglePlayPause()
    if (empty) return
    if (mode === 'artist') playArtist(tracks, false, context)
    else playAlbum(tracks, 0, context)
  }

  return (
    <>
      <Button
        variant="default"
        disabled={empty}
        onClick={play}
        aria-label={playing ? 'Pause' : active ? 'Fortsetzen' : playLabel}
        className="min-w-36 font-semibold"
      >
        {playing ? <Pause className="size-4 fill-current" aria-hidden /> : <Play className="size-4 fill-current" aria-hidden />}
        {playing ? 'Pause' : active ? 'Fortsetzen' : 'Abspielen'}
      </Button>
      <Button disabled={empty} onClick={() => playShuffled(tracks, context)} title="Zufällig" className="@max-sm:w-10 @max-sm:px-0">
        <Shuffle className="size-4" aria-hidden />
        <span className="@max-sm:sr-only">Zufällig</span>
      </Button>
      {queueActions && (
        <>
          <Button variant="ghost" disabled={empty} onClick={() => playNext(tracks)} title="Als Nächstes" className="@max-xl:w-10 @max-xl:px-0">
            <ListStart className="size-4" aria-hidden />
            <span className="@max-xl:sr-only">Als Nächstes</span>
          </Button>
          <Button variant="ghost" disabled={empty} onClick={() => addToQueue(tracks)} title="Zur Warteschlange" className="@max-xl:w-10 @max-xl:px-0">
            <ListPlus className="size-4" aria-hidden />
            <span className="@max-xl:sr-only">Zur Warteschlange</span>
          </Button>
        </>
      )}
    </>
  )
}
