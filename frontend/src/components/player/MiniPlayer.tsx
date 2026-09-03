import { useState } from 'react'
import {
  ListMusic,
  Maximize2,
  Pause,
  Play,
  Repeat,
  Repeat1,
  Shuffle,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { Button } from '@/components/ui/button'
import { usePlayer } from '@/hooks/usePlayer'
import { Link, paths } from '@/lib/router'
import { formatDuration, joinArtists } from '@/lib/utils/format'

export function MiniPlayer() {
  const {
    currentTrack,
    status,
    currentTime,
    duration,
    volume,
    muted,
    shuffle,
    repeatMode,
    togglePlayPause,
    previous,
    next,
    seek,
    setVolume,
    toggleMute,
    toggleShuffle,
    cycleRepeatMode,
  } = usePlayer()

  const [seekingValue, setSeekingValue] = useState<number | null>(null)

  if (!currentTrack) {
    return null
  }

  const isPlaying = status === 'playing'
  const isBuffering = status === 'buffering'
  const displayTime = seekingValue ?? currentTime
  const progressPercent = duration > 0 ? Math.min(100, (displayTime / duration) * 100) : 0
  const artistText =
    currentTrack.artists?.length > 0
      ? joinArtists(currentTrack.artists)
      : currentTrack.album_artist || ''

  return (
    <aside
      aria-label="Audioplayer"
      className="fixed bottom-0 left-0 right-0 z-40 h-[68px] sm:h-[86px] border-t border-white/[0.06] bg-[#080a12]/94 backdrop-blur-xl shadow-[0_-8px_32px_rgba(0,0,0,0.6)] transition-all"
    >
      {/* Mobile Top Thin Progress Bar */}
      <div
        className="sm:hidden absolute top-0 left-0 right-0 h-1 bg-white/10 cursor-pointer"
        onClick={(e) => {
          const rect = e.currentTarget.getBoundingClientRect()
          const clickX = e.clientX - rect.left
          const ratio = Math.max(0, Math.min(1, clickX / rect.width))
          seek(ratio * (duration || 0))
        }}
      >
        <div
          className="h-full bg-primary transition-all"
          style={{ width: `${progressPercent}%` }}
        />
      </div>

      <div className="mx-auto flex h-full max-w-[94rem] items-center justify-between px-3 sm:px-6 gap-3 sm:gap-6">
        
        {/* ======================================================== */}
        {/* LEFT ZONE (~25%): Cover + Track Metadata (Click -> /player) */}
        {/* ======================================================== */}
        <div className="flex min-w-0 items-center gap-3 sm:gap-3.5 flex-1 sm:flex-initial sm:w-64 lg:w-72">
          <Link
            href={paths.player ? paths.player() : '/player'}
            className="group relative shrink-0 overflow-hidden rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary"
            title="Now Playing öffnen"
          >
            <Cover
              src={currentTrack.cover_url}
              alt={currentTrack.title}
              shape="square"
              className="size-11 sm:size-14 rounded-xl border border-white/10 shadow-md transition-transform duration-200 group-hover:scale-105"
            />
            <div className="absolute inset-0 flex items-center justify-center rounded-xl bg-black/40 opacity-0 backdrop-blur-[1px] transition-opacity group-hover:opacity-100">
              <Maximize2 className="size-4 text-white" />
            </div>
          </Link>

          <div className="min-w-0 flex-1">
            <Link
              href={paths.player ? paths.player() : '/player'}
              className="block truncate text-sm sm:text-[15px] font-semibold text-foreground hover:text-primary transition-colors leading-snug"
              title={currentTrack.title}
            >
              {currentTrack.title}
            </Link>
            <p className="truncate text-xs text-neutral-400 mt-0.5" title={artistText}>
              {artistText}
            </p>
          </div>
        </div>

        {/* ======================================================== */}
        {/* CENTER ZONE (~50%): Centered Playback Controls + Seekbar */}
        {/* ======================================================== */}
        <div className="flex flex-col items-center justify-center flex-1 max-w-2xl px-2 sm:px-4">
          
          {/* Top Controls Row */}
          <div className="flex items-center gap-3 sm:gap-4">
            {/* Shuffle */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={toggleShuffle}
              className={`hidden sm:inline-flex size-8 rounded-full transition-colors ${
                shuffle
                  ? 'text-primary hover:text-primary/80'
                  : 'text-neutral-400 hover:text-white'
              }`}
              title={shuffle ? 'Zufallswiedergabe: Ein' : 'Zufallswiedergabe: Aus'}
              aria-label="Zufallswiedergabe"
            >
              <Shuffle className="size-4" strokeWidth={1.8} />
            </Button>

            {/* Previous */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={previous}
              className="size-8.5 sm:size-9 rounded-full text-neutral-300 hover:text-white hover:bg-white/8 transition-colors"
              title="Vorheriger Titel / Neustart"
              aria-label="Vorheriger Titel"
            >
              <SkipBack className="size-4.5 sm:size-5" strokeWidth={1.8} />
            </Button>

            {/* Play / Pause Main Action Button (Only Filled Accent Button, 44px) */}
            <Button
              size="icon"
              onClick={togglePlayPause}
              disabled={isBuffering}
              className="size-10 sm:size-11 rounded-full bg-primary text-primary-foreground shadow-md shadow-primary/20 hover:scale-105 active:scale-95 transition-all duration-150"
              title={isPlaying ? 'Pause' : 'Wiedergabe'}
              aria-label={isPlaying ? 'Pause' : 'Wiedergabe'}
            >
              {isPlaying ? (
                <Pause className="size-5 fill-current" strokeWidth={1.8} />
              ) : (
                <Play className="size-5 fill-current ml-0.5" strokeWidth={1.8} />
              )}
            </Button>

            {/* Next */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => next(true)}
              className="size-8.5 sm:size-9 rounded-full text-neutral-300 hover:text-white hover:bg-white/8 transition-colors"
              title="Nächster Titel"
              aria-label="Nächster Titel"
            >
              <SkipForward className="size-4.5 sm:size-5" strokeWidth={1.8} />
            </Button>

            {/* Repeat */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={cycleRepeatMode}
              className={`hidden sm:inline-flex size-8 rounded-full transition-colors ${
                repeatMode !== 'off'
                  ? 'text-primary hover:text-primary/80'
                  : 'text-neutral-400 hover:text-white'
              }`}
              title={`Wiederholen: ${
                repeatMode === 'off' ? 'Aus' : repeatMode === 'queue' ? 'Queue' : 'Titel'
              }`}
              aria-label="Wiederholen"
            >
              {repeatMode === 'track' ? (
                <Repeat1 className="size-4" strokeWidth={1.8} />
              ) : (
                <Repeat className="size-4" strokeWidth={1.8} />
              )}
            </Button>
          </div>

          {/* Bottom Seekbar Row (Desktop & Tablet with hidden thumb by default) */}
          <div className="hidden sm:flex w-full items-center justify-between gap-3 text-xs text-neutral-400 font-mono mt-1">
            <span className="w-10 text-right text-[11px] tabular-nums select-none">
              {formatDuration(displayTime * 1000)}
            </span>
            <div className="relative flex-1 group py-1.5 flex items-center">
              <input
                type="range"
                min={0}
                max={duration || 1}
                step={0.1}
                value={displayTime}
                onMouseDown={() => setSeekingValue(currentTime)}
                onTouchStart={() => setSeekingValue(currentTime)}
                onChange={(e) => setSeekingValue(parseFloat(e.target.value))}
                onMouseUp={() => {
                  if (seekingValue !== null) {
                    seek(seekingValue)
                    setSeekingValue(null)
                  }
                }}
                onTouchEnd={() => {
                  if (seekingValue !== null) {
                    seek(seekingValue)
                    setSeekingValue(null)
                  }
                }}
                className="slider-quiet w-full outline-none"
                style={{
                  background: `linear-gradient(to right, #ce3463 0%, #ce3463 ${progressPercent}%, rgba(255,255,255,0.15) ${progressPercent}%, rgba(255,255,255,0.15) 100%)`,
                }}
                aria-label="Fortschritt"
              />
            </div>
            <span className="w-10 text-left text-[11px] tabular-nums select-none">
              {formatDuration(duration * 1000)}
            </span>
          </div>
        </div>

        {/* ======================================================== */}
        {/* RIGHT ZONE (~25%): Volume, Queue & Clean Expand Icon     */}
        {/* ======================================================== */}
        <div className="flex items-center justify-end gap-1.5 sm:gap-3 sm:w-64 lg:w-72">
          
          {/* Volume Control (Slider with quiet thumb) */}
          <div className="hidden lg:flex items-center gap-2 pr-1">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={toggleMute}
              className="size-8 rounded-lg text-neutral-400 hover:text-white"
              title={muted ? 'Stummschaltung aufheben' : 'Stummschalten'}
              aria-label="Lautstärke"
            >
              {muted || volume === 0 ? (
                <VolumeX className="size-4 text-destructive" strokeWidth={1.8} />
              ) : (
                <Volume2 className="size-4" strokeWidth={1.8} />
              )}
            </Button>
            <div className="relative flex items-center py-1">
              <input
                type="range"
                min={0}
                max={1}
                step={0.01}
                value={muted ? 0 : volume}
                onChange={(e) => setVolume(parseFloat(e.target.value))}
                className="slider-quiet w-20 outline-none"
                style={{
                  background: `linear-gradient(to right, rgba(255,255,255,0.7) 0%, rgba(255,255,255,0.7) ${(muted ? 0 : volume) * 100}%, rgba(255,255,255,0.15) ${(muted ? 0 : volume) * 100}%, rgba(255,255,255,0.15) 100%)`,
                }}
                aria-label="Lautstärkeregler"
              />
            </div>
          </div>

          {/* Queue Link */}
          <Link
            href="/player?tab=queue"
            className="flex items-center justify-center size-8 rounded-lg text-neutral-400 hover:text-white hover:bg-white/5 transition-colors"
            title="Wiedergabeliste / Queue"
          >
            <ListMusic className="size-4" strokeWidth={1.8} />
          </Link>

          {/* Expand Button: Clean Icon Button */}
          <Link
            href="/player"
            className="flex items-center justify-center size-8.5 rounded-xl bg-white/[0.06] hover:bg-white/[0.12] border border-white/8 text-neutral-300 hover:text-white transition-all shadow-sm"
            title="Player öffnen"
            aria-label="Player öffnen"
          >
            <Maximize2 className="size-4" strokeWidth={1.8} />
          </Link>
        </div>

      </div>
    </aside>
  )
}
