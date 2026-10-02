import { Pause, Play } from 'lucide-react'
import type { MouseEventHandler, ReactNode } from 'react'

interface TrackPlaybackButtonProps {
  number: ReactNode
  title: string
  isCurrent: boolean
  isPlaying: boolean
  onClick: MouseEventHandler<HTMLButtonElement>
}

export function TrackPlaybackButton({
  number,
  title,
  isCurrent,
  isPlaying,
  onClick,
}: TrackPlaybackButtonProps) {
  return (
    <div className="group/play-control relative mx-auto flex size-7 items-center justify-center">
      {!isCurrent && (
        <span
          aria-hidden="true"
          className="text-neutral-500 group-hover/play-control:opacity-0 group-focus-within/play-control:opacity-0 [@media(hover:none)]:opacity-0"
        >
          {number}
        </span>
      )}
      <button
        type="button"
        onClick={onClick}
        aria-label={`${title} ${isPlaying ? 'pausieren' : 'abspielen'}`}
        title={isPlaying ? 'Pause' : 'Abspielen'}
        className={`absolute inset-0 flex items-center justify-center rounded-full transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring ${
          isCurrent
            ? 'bg-primary text-white hover:bg-primary/90'
            : 'text-neutral-300 hover:bg-white/8 opacity-0 group-hover/play-control:opacity-100 group-focus-within/play-control:opacity-100 [@media(hover:none)]:opacity-100'
        }`}
      >
        {isPlaying ? (
          <Pause className="size-3.5 fill-current" />
        ) : (
          <Play className="size-3.5 fill-current ml-0.5" />
        )}
      </button>
    </div>
  )
}
