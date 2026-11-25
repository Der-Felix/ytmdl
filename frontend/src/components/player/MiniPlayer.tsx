import { libraryArtwork } from '@/lib/artwork'
import { useState } from 'react'
import {
  Heart,
  ListMusic,
  Loader2,
  Maximize2,
  Pause,
  Play,
  SkipBack,
  SkipForward,
  Volume2,
  VolumeX,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { RepeatButton, ShuffleButton } from '@/components/player/PlayerModeButtons'
import { QueueDrawer } from '@/components/player/QueueDrawer'
import { Button } from '@/components/ui/button'
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { Link, paths } from '@/lib/router'
import { joinArtists } from '@/lib/utils/format'
import { sliderFill } from '@/lib/utils/slider'
import { MobileProgress, SeekBar } from './SeekBar'

/*
 * The bar reads the player state and its actions; only the two progress
 * displays below subscribe to the playback position. A progress tick thereby
 * redraws a time label and a slider, not the bar and its open queue.
 */
export function MiniPlayer() {
  const { currentTrack, queue, status, volume, muted, shuffle, repeatMode } = usePlayerState()
  const { togglePlayPause, previous, next, setVolume, toggleMute, toggleShuffle, cycleRepeatMode } =
    usePlayerActions()

  const [queueOpen, setQueueOpen] = useState(false)
  const favorites = useOptionalFavorites()

  if (!currentTrack) {
    return null
  }

  const isPlaying = status === 'playing'
  const isBuffering = status === 'buffering'
  const artistText =
    currentTrack.artists?.length > 0
      ? joinArtists(currentTrack.artists)
      : currentTrack.album_artist || ''

  return (
    <>
      <aside
        aria-label="Audioplayer"
        data-slot="mini-player"
        className="fixed bottom-0 left-0 right-0 z-40 h-[80px] sm:h-[86px] border-t border-border bg-surface pb-[env(safe-area-inset-bottom)] box-content"
    >
      <MobileProgress />

      <div className="mx-auto flex h-full max-w-[94rem] items-center justify-between px-3 sm:px-6 gap-2 sm:gap-6 pt-6 sm:pt-0">
        
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
              src={libraryArtwork('tracks', currentTrack.id)} fallbackSrc={currentTrack.cover_url}
              alt={currentTrack.title}
              shape="square"
              className="size-11 sm:size-14 rounded-xl border border-input shadow-md transition-transform duration-200 group-hover:scale-105"
            />
            <div className="absolute inset-0 flex items-center justify-center rounded-xl bg-overlay/40 opacity-0-[1px] transition-opacity group-hover:opacity-100">
              <Maximize2 className="size-4 text-foreground" />
            </div>
          </Link>

          <div className="min-w-0 flex-1">
            <Link
              href={paths.player ? paths.player() : '/player'}
              className="group/meta block min-w-0 focus-visible:outline-none"
              title={`${currentTrack.title} · ${artistText}`}
            >
              <span className="block truncate text-sm sm:text-[15px] font-semibold text-foreground group-hover/meta:text-primary transition-colors leading-snug">
                {currentTrack.title}
              </span>
              <p className="truncate text-xs text-muted-foreground mt-0.5">
                {artistText}
              </p>
            </Link>
          </div>

          {favorites && (
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => void favorites.toggleFavorite(currentTrack.id)}
              className={`hidden sm:inline-flex size-8 shrink-0 rounded-full transition-colors ${
                favorites.isFavorite(currentTrack.id)
                  ? 'text-primary hover:text-accent-foreground'
                  : 'text-muted-foreground hover:text-accent-foreground'
              }`}
              aria-pressed={favorites.isFavorite(currentTrack.id)}
              title={
                favorites.isFavorite(currentTrack.id)
                  ? 'Aus Favoriten entfernen'
                  : 'Zu Favoriten hinzufügen'
              }
              aria-label={
                favorites.isFavorite(currentTrack.id)
                  ? 'Aus Favoriten entfernen'
                  : 'Zu Favoriten hinzufügen'
              }
            >
              <Heart
                className={`size-4 ${
                  favorites.isFavorite(currentTrack.id)
                    ? 'fill-current'
                    : ''
                }`}
              />
            </Button>
          )}
        </div>

        {/* ======================================================== */}
        {/* CENTER ZONE (~50%): Centered Playback Controls + Seekbar */}
        {/* ======================================================== */}
        <div className="flex flex-col items-center justify-center shrink-0 sm:flex-1 sm:max-w-2xl px-1 sm:px-4 gap-1">
          
          {/* Top Controls Row */}
          <div className="flex items-center gap-2 sm:gap-4">
            <ShuffleButton shuffle={shuffle} onToggle={toggleShuffle} className="hidden sm:inline-flex size-8" />

            {/* Previous */}
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={previous}
              className="hidden sm:inline-flex size-8.5 sm:size-9 rounded-full text-foreground/80 hover:text-foreground hover:bg-secondary transition-colors"
              title="Vorheriger Titel / Neustart"
              aria-label="Vorheriger Titel"
            >
              <SkipBack className="size-4.5 sm:size-5" strokeWidth={1.8} />
            </Button>

            {/* Play / Pause Main Action Button (Only Filled Accent Button, 44px) */}
            {/* While buffering it stays a pause button: a click stops the pending start. */}
            <Button
              size="icon"
              onClick={togglePlayPause}
              aria-busy={isBuffering || undefined}
              className="size-10 sm:size-11 rounded-full bg-primary text-primary-foreground shadow-md shadow-primary/20 hover:bg-primary/90 transition-colors duration-150"
              title={isBuffering ? 'Wird geladen – anhalten' : isPlaying ? 'Pause' : 'Wiedergabe'}
              aria-label={isPlaying || isBuffering ? 'Pause' : 'Wiedergabe'}
            >
              {isBuffering ? (
                <Loader2 className="size-5 animate-spin" strokeWidth={2} />
              ) : isPlaying ? (
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
              className="size-8.5 sm:size-9 rounded-full text-foreground/80 hover:text-foreground hover:bg-secondary transition-colors"
              title="Nächster Titel"
              aria-label="Nächster Titel"
            >
              <SkipForward className="size-4.5 sm:size-5" strokeWidth={1.8} />
            </Button>

            <RepeatButton mode={repeatMode} onCycle={cycleRepeatMode} className="hidden sm:inline-flex size-8" />
          </div>

          {/* Seek bar on tablets and wider; phones get the line on top. */}
          <div className="hidden w-full sm:block">
            <SeekBar layout="inline" />
          </div>
        </div>

        {/* ======================================================== */}
        {/* RIGHT ZONE (~25%): Volume, Queue & Clean Expand Icon     */}
        {/* ======================================================== */}
        <div className="flex items-center justify-end shrink-0 gap-1.5 sm:gap-3 sm:w-64 lg:w-72">
          
          {/* Volume Control (Slider with quiet thumb) */}
          <div className="hidden lg:flex items-center gap-2 pr-1">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={toggleMute}
              className="size-8 rounded-lg text-muted-foreground hover:text-foreground"
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
                className="slider-quiet w-20"
                style={sliderFill(muted ? 0 : volume, 0, 1, 'var(--muted-foreground)')}
                aria-label="Lautstärkeregler"
              />
            </div>
          </div>

          {/* Queue Toggle */}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => setQueueOpen((prev) => !prev)}
            className={`relative flex items-center justify-center size-8 rounded-lg transition-colors ${
              queueOpen
                ? 'text-primary bg-primary/15'
                : 'text-muted-foreground hover:text-foreground hover:bg-muted'
            }`}
            title="Wiedergabeliste / Queue öffnen"
            aria-label="Wiedergabeliste / Queue öffnen"
          >
            <ListMusic className="size-4" strokeWidth={1.8} />
            {queue.length > 1 && (
              <span className="absolute -top-1 -right-1 flex h-3.5 min-w-3.5 items-center justify-center rounded-full bg-primary px-0.5 figures text-[9px] font-bold text-primary-foreground">
                {queue.length}
              </span>
            )}
          </Button>

          {/* Expand Button: Clean Icon Button */}
          <Link
            href="/player"
            className="hidden sm:flex items-center justify-center size-8.5 rounded-xl bg-secondary hover:bg-input border border-border text-foreground/80 hover:text-foreground transition shadow-sm"
            title="Player öffnen"
            aria-label="Player öffnen"
          >
            <Maximize2 className="size-4" strokeWidth={1.8} />
          </Link>
        </div>

      </div>
    </aside>

    <QueueDrawer open={queueOpen} onOpenChange={setQueueOpen} />
  </>
)
}
