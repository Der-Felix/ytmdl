import { useState } from 'react'

import { usePlayerActions, usePlayerProgress } from '@/hooks/usePlayer'
import { cn } from '@/lib/utils'
import { formatDuration, formatPlaybackTime } from '@/lib/utils/format'
import { sliderFill } from '@/lib/utils/slider'

/*
 * The only parts of the player views that follow the playback position.
 * Each subscribes to the progress context on its own, so a tick redraws the
 * slider and the two time labels and nothing around them.
 */

const SEEK_KEYS = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End', 'PageUp', 'PageDown']

function percent(time: number, duration: number) {
  return duration > 0 ? Math.min(100, (time / duration) * 100) : 0
}

/** Slider plus elapsed and total time. `layout` decides where the times sit. */
export function SeekBar({ layout }: { layout: 'inline' | 'stacked' }) {
  const { currentTime, duration } = usePlayerProgress()
  const { seek } = usePlayerActions()
  const [seekingValue, setSeekingValue] = useState<number | null>(null)

  const displayTime = seekingValue ?? currentTime
    const commit = () => {
    if (seekingValue !== null) seek(seekingValue)
    setSeekingValue(null)
  }

  const slider = (
    <input
      type="range"
      min={0}
      max={duration || 1}
      step={0.1}
      value={displayTime}
      onPointerDown={() => setSeekingValue(currentTime)}
      onChange={(e) => setSeekingValue(parseFloat(e.target.value))}
      onPointerUp={commit}
      onKeyUp={(e) => {
        if (SEEK_KEYS.includes(e.key)) {
          seek(Number(e.currentTarget.value))
          setSeekingValue(null)
        }
      }}
      onBlur={commit}
      className="slider-quiet w-full"
      style={sliderFill(displayTime, 0, duration || 1)}
      aria-label="Fortschritt"
      aria-valuetext={`${formatPlaybackTime(displayTime)} von ${formatDuration(duration * 1000)}`}
    />
  )

  const elapsed = <span className="figures select-none">{formatPlaybackTime(displayTime)}</span>
  const total = <span className="figures select-none">{formatDuration(duration * 1000)}</span>

  if (layout === 'inline') {
    return (
      <div className="flex w-full items-center justify-between gap-3 text-[11px] text-muted-foreground">
        <span className="w-10 text-right">{elapsed}</span>
        <div className="relative flex flex-1 items-center py-1.5">{slider}</div>
        <span className="w-10 text-left">{total}</span>
      </div>
    )
  }

  return (
    <div className="w-full space-y-1.5 pt-1">
      <div className="relative flex items-center py-1">{slider}</div>
      <div className="flex items-center justify-between text-[11px] text-muted-foreground">
        {elapsed}
        {total}
      </div>
    </div>
  )
}

/** The thin progress line on top of the mini player on phones; tap to seek. */
export function MobileProgress({ className }: { className?: string }) {
  const { currentTime, duration } = usePlayerProgress()
  const { seek } = usePlayerActions()
  return (
    <div
      aria-hidden="true"
      className={cn('absolute inset-x-0 top-0 z-10 flex h-6 cursor-pointer items-start', className)}
      onClick={(e) => {
        const rect = e.currentTarget.getBoundingClientRect()
        const ratio = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width))
        seek(ratio * (duration || 0))
      }}
    >
      <div className="h-1 w-full bg-input">
        <div className="h-full bg-primary" style={{ width: `${percent(currentTime, duration)}%` }} />
      </div>
    </div>
  )
}
