import type { MouseEvent } from 'react'
import { Loader2, Pause, Play } from 'lucide-react'

import type { CollectionPlayback } from '@/hooks/useCollectionPlayback'
import { cn } from '@/lib/utils'

interface CardPlayButtonProps {
  /** What the button plays, used in its accessible name: „{title}“ abspielen. */
  title: string
  playback: CollectionPlayback
  className?: string
}

/**
 * The round play button on a card's cover. It sits above the card's stretched
 * link, so a click plays without navigating while the rest of the card still
 * opens the page.
 *
 * Pointer devices see it on hover or when anything in the card has keyboard
 * focus; touch screens always see it, there is no hover to reveal it. While
 * the card is what plays, it stays visible and shows pause.
 */
export function CardPlayButton({ title, playback, className }: CardPlayButtonProps) {
  const { active, playing, loading, toggle } = playback
  const label = playing ? `„${title}“ pausieren` : active ? `„${title}“ fortsetzen` : `„${title}“ abspielen`

  const handleClick = (event: MouseEvent) => {
    event.preventDefault()
    event.stopPropagation()
    if (!loading) toggle()
  }

  return (
    <button
      type="button"
      aria-label={label}
      aria-busy={loading || undefined}
      onClick={handleClick}
      data-active={active || undefined}
      className={cn(
        'absolute right-2 bottom-2 z-20 flex size-11 items-center justify-center rounded-full',
        'bg-primary text-primary-foreground shadow-lg shadow-overlay/50',
        'outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring',
        'transition-[opacity,transform,background-color] duration-(--duration-fast) ease-(--ease-out)',
        'hover:bg-[color-mix(in_oklab,var(--primary),white_12%)] active:scale-[0.96]',
        // Hidden until hover or focus on pointer devices; always there on touch.
        'pointer-fine:translate-y-1 pointer-fine:opacity-0',
        'pointer-fine:group-hover:translate-y-0 pointer-fine:group-hover:opacity-100',
        'pointer-fine:group-focus-within:translate-y-0 pointer-fine:group-focus-within:opacity-100',
        'pointer-fine:data-active:translate-y-0 pointer-fine:data-active:opacity-100',
        'motion-reduce:translate-y-0 pointer-fine:motion-reduce:translate-y-0',
        className,
      )}
    >
      {loading ? (
        <Loader2 className="size-5 animate-spin" aria-hidden />
      ) : playing ? (
        <Pause className="size-5 fill-current" aria-hidden />
      ) : (
        <Play className="size-5 translate-x-px fill-current" aria-hidden />
      )}
    </button>
  )
}
