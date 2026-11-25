import { libraryArtwork } from '@/lib/artwork'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Tabs } from '@base-ui/react/tabs'
import {
  ArrowLeft,
  FolderPlus,
  Heart,
  ListMusic,
  Loader2,
  Maximize2,
  Mic2,
  MonitorSmartphone,
  Radio,
  Minimize2,
  MoreHorizontal,
  Music2,
  Pause,
  Play,
  Plus,
  RefreshCw,
  RotateCcw,
  Save,
  Shield,
  SkipBack,
  SkipForward,
  Sliders,
  Sparkles,
  Trash2,
  Volume2,
  VolumeX,
} from 'lucide-react'

import { AddToPlaylistDialog } from '@/components/music/AddToPlaylistDialog'
import { Cover } from '@/components/music/Cover'
import { VisualizerCanvas } from '@/components/player/VisualizerCanvas'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useOptionalFavorites } from '@/hooks/useFavorites'
import { usePlayerProgressSelector } from '@/contexts/player-progress-store'
import { usePlayerActions, usePlayerState } from '@/hooks/usePlayer'
import { RepeatButton, ShuffleButton } from '@/components/player/PlayerModeButtons'
import { QueueList } from '@/components/player/QueueList'
import { SeekBar } from '@/components/player/SeekBar'
import { notifyError } from '@/lib/notify'
import { refreshTrackLyrics, trackLyrics } from '@/lib/api/library'
import {
  BUILTIN_PRESETS,
  EQ_FREQUENCIES,
  formatFrequency,
} from '@/lib/audio/eqPresets'
import type { LrcLine } from '@/lib/utils/lrc'
import type {
  ParametricFilter,
  ParametricFilterType,
  SleepTimerOption,
  VisualizerMode,
} from '@/lib/audio/types'
import { Link, goBack, navigate, paths, useLocation } from '@/lib/router'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import { parseLrc } from '@/lib/utils/lrc'
import { sliderFill } from '@/lib/utils/slider'
import type { TrackLyrics } from '@/types/api'

type PrimaryTab = 'lyrics' | 'queue' | 'equalizer' | 'audio'

/** The panel tabs: an underline marks the selected one. */
function playerTabClass({ active }: { active: boolean }) {
  return `flex items-center gap-2 px-4 py-2.5 text-xs sm:text-sm font-semibold transition border-b-2 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring ${
    active
      ? 'border-primary text-foreground bg-muted'
      : 'border-transparent text-muted-foreground hover:text-foreground'
  }`
}

/**
 * Leaves the large player: back to the in-app page it was opened from, or to
 * the listening start page when it was opened directly (bookmark, reload of a
 * fresh tab).
 */
function leavePlayer() {
  goBack(paths.listenHome())
}

/** A unique id for a new parametric filter; filters are stored with the player state. */
function newFilterId() {
  return `filter-${Date.now()}`
}

export function NowPlaying() {
  const location = useLocation()
  const tabParam = location.params.get('tab')

  const {
    currentTrack,
    queue,
    queueIndex,
    status,
    volume,
    muted,
    shuffle,
    repeatMode,
    playbackRate,
    crossfadeSeconds,
    smartAlbumTransition,
    sleepTimer,
    eqEnabled,
    eqMode,
    selectedPresetId,
    graphicBands,
    parametricFilters,
    customPresets,
    preamp,
    autoHeadroom,
    limiterEnabled,
    normalizationEnabled,
    normalizationMessage,
    balance,
    mono,
    visualizerMode,
    history,
    togglePlayPause,
    playTrack,
    previous,
    next,
    seek,
    setVolume,
    toggleMute,
    toggleShuffle,
    cycleRepeatMode,
    setPlaybackRate,
    setCrossfade,
    setSmartAlbumTransition,
    setSleepTimer,
    setEQEnabled,
    setEQMode,
    setEQPreset,
    setEQBand,
    saveCustomPreset,
    setParametricFilter,
    addParametricFilter,
    deleteParametricFilter,
    setPreamp,
    setAutoHeadroom,
    setLimiter,
    setNormalization,
    setBalance,
    setMono,
    setVisualizerMode,
    clearUpcomingQueue,
    clearQueue,
  } = { ...usePlayerState(), ...usePlayerActions() }

  const initialTab: PrimaryTab =
    tabParam === 'eq' || tabParam === 'parametric'
      ? 'equalizer'
      : tabParam === 'dsp'
        ? 'audio'
        : tabParam === 'queue'
          ? 'queue'
          : 'lyrics'

  const [activeTab, setActiveTab] = useState<PrimaryTab>(initialTab)
  const [savePresetOpen, setSavePresetOpen] = useState(false)
  const [customPresetName, setCustomPresetName] = useState('')
  const [showHistory, setShowHistory] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [visualizerMenuOpen, setVisualizerMenuOpen] = useState(false)
  const [optionsMenuOpen, setOptionsMenuOpen] = useState(false)
  const favorites = useOptionalFavorites()
  const [playlistDialogOpen, setPlaylistDialogOpen] = useState(false)

  // Lyrics state
  const [lyricsData, setLyricsData] = useState<TrackLyrics | null>(null)
  const [lyricsLoading, setLyricsLoading] = useState(false)
  const [lyricsRefreshing, setLyricsRefreshing] = useState(false)
  const [followLyrics, setFollowLyrics] = useState(true)
  const activeLyricRef = useRef<HTMLDivElement | null>(null)
  const lyricsContainerRef = useRef<HTMLDivElement | null>(null)

  // Fullscreen Change Listener
  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement))
    }
    document.addEventListener('fullscreenchange', handleFullscreenChange)
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange)
  }, [])

  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || event.defaultPrevented) return
      // Native fullscreen consumes Escape itself; a second Escape closes the large view.
      if (document.fullscreenElement) return
      if (playlistDialogOpen || savePresetOpen) return
      // Escape that closes the command menu or another dialog stays there.
      // Toasts are dialogs too, but not modal ones; they do not count.
      if (document.querySelector('[role="dialog"]:not([data-slot="toast"]), [role="alertdialog"]:not([data-slot="toast"])')) return
      event.preventDefault()
      if (optionsMenuOpen || visualizerMenuOpen) {
        setOptionsMenuOpen(false)
        setVisualizerMenuOpen(false)
        return
      }
      leavePlayer()
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [optionsMenuOpen, visualizerMenuOpen, playlistDialogOpen, savePresetOpen])

  const toggleFullscreen = async () => {
    try {
      if (!document.fullscreenElement) {
        await document.documentElement.requestFullscreen()
      } else {
        await document.exitFullscreen()
      }
    } catch {
      // Fullscreen API unavailable or denied
    }
  }

  // Load lyrics when currentTrack changes
  useEffect(() => {
    if (!currentTrack) {
      setLyricsData(null)
      return
    }

    let active = true
    setLyricsData(null)
    setLyricsLoading(true)

    trackLyrics(currentTrack.id)
      .then((data) => {
        if (active) {
          setLyricsData(data)
          setLyricsLoading(false)
        }
      })
      .catch(() => {
        if (active) {
          setLyricsData(null)
          setLyricsLoading(false)
        }
      })

    return () => {
      active = false
    }
  }, [currentTrack?.id])

  const lyricsTrackIdRef = useRef(currentTrack?.id)
  lyricsTrackIdRef.current = currentTrack?.id

  // Manual Lyrics Refresh Handler
  const handleRefreshLyrics = async () => {
    if (!currentTrack || lyricsRefreshing) return
    const requestedTrackId = currentTrack.id
    setLyricsRefreshing(true)
    try {
      const refreshed = await refreshTrackLyrics(requestedTrackId)
      // The track may have changed while the lookup ran. Lyrics of the
      // previous track must not replace those of the one playing now.
      if (lyricsTrackIdRef.current === requestedTrackId) setLyricsData(refreshed)
    } catch (error) {
      notifyError('Lyrics konnten nicht nachgeladen werden', error)
    } finally {
      setLyricsRefreshing(false)
    }
  }

  // Parse synchronized LRC lines
  const parsedLrcLines = useMemo<LrcLine[] | null>(() => {
    if (!lyricsData?.content || !lyricsData.synced) return null
    const parsed = parseLrc(lyricsData.content)
    return parsed.lines.length > 0 ? parsed.lines : null
  }, [lyricsData?.content, lyricsData?.synced])

  // The active synced line. Derived from the position through a selector, so
  // the page redraws when the line changes, not on every progress tick.
  const activeLrcIndex = usePlayerProgressSelector(({ currentTime }) => {
    if (!parsedLrcLines) return -1
    let activeIdx = -1
    for (let i = 0; i < parsedLrcLines.length; i++) {
      const line = parsedLrcLines[i]
      if (line && line.timeSeconds <= currentTime) activeIdx = i
      else break
    }
    return activeIdx
  })

  // Scroll only the panel; scrollIntoView also moves the page and hides controls.
  useEffect(() => {
    const panel = lyricsContainerRef.current
    const line = activeLyricRef.current
    if (!panel || !line || !followLyrics || activeTab !== 'lyrics' || lyricsLoading) return
    const top = panel.scrollTop + line.getBoundingClientRect().top - panel.getBoundingClientRect().top
    panel.scrollTo({
      top: Math.max(0, top - (panel.clientHeight - line.clientHeight) / 2),
      behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
    })
  }, [activeLrcIndex, activeTab, followLyrics, lyricsLoading])

  if (!currentTrack) {
    return (
      <div className="flex min-h-[75vh] flex-col items-center justify-center p-6 text-center">
        <div className="relative mb-6">
          <div className="flex size-20 items-center justify-center rounded-3xl bg-primary/10 text-primary">
            <Music2 className="size-10" />
          </div>
        </div>
        <h2 className="font-heading text-xl font-semibold text-foreground">
          Kein Titel in Wiedergabe
        </h2>
        <p className="mt-2 max-w-sm text-sm text-muted-foreground">
          Wähle ein Album oder einen Titel aus deiner Musikbibliothek aus, um die Wiedergabe zu starten.
        </p>
        <div className="mt-6 flex items-center gap-3">
          <Link
            href={paths.library ? paths.library() : '/library'}
            className="inline-flex items-center gap-2 rounded-xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow-lg shadow-primary/20 hover:bg-primary/90 transition"
          >
            Zur Musikbibliothek
          </Link>
        </div>
      </div>
    )
  }

  const isPlaying = status === 'playing'
  const isBuffering = status === 'buffering'
  const artistText =
    currentTrack.artists?.length > 0
      ? joinArtists(currentTrack.artists)
      : currentTrack.album_artist || ''

  const speedOptions = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]

  const handleAddParametricFilter = () => {
    const newFilter: ParametricFilter = {
      id: newFilterId(),
      type: 'peaking',
      frequency: 1000,
      gain: 0,
      q: 1.0,
      enabled: true,
    }
    addParametricFilter(newFilter)
  }

  return (
    <div className="player-page player-enter relative min-h-dvh flex flex-col bg-background text-foreground select-none overflow-x-hidden pb-[env(safe-area-inset-bottom)]">
      
      {/* Dynamic Immersive Background Atmosphere from Cover Art */}
      <div className="pointer-events-none absolute inset-0 overflow-hidden opacity-25 filter blur-3xl scale-125 transition duration-1000">
        <div
          className="absolute inset-0 bg-cover bg-center"
          style={{
            backgroundImage: currentTrack.cover_url ? `url("${currentTrack.cover_url}")` : undefined,
          }}
        />
        <div className="absolute inset-0 bg-radial from-transparent via-background/70 to-background" />
      </div>

      {/* Top Header Bar (Clean & Minimal) */}
      {/* The safe areas of an installed app: status bar above, notch beside. */}
      <header className="relative z-20 flex h-[calc(3.5rem+env(safe-area-inset-top))] shrink-0 items-center justify-between pt-[env(safe-area-inset-top)] pr-[max(1rem,env(safe-area-inset-right))] pl-[max(1rem,env(safe-area-inset-left))] sm:pr-[max(2rem,env(safe-area-inset-right))] sm:pl-[max(2rem,env(safe-area-inset-left))]">
        {/* Back to where the player was opened from; Escape does the same. */}
        <button
          type="button"
          onClick={leavePlayer}
          className="flex items-center gap-2 rounded-xl bg-muted hover:bg-secondary px-3 py-1.5 pointer-coarse:min-h-10 text-xs font-medium text-foreground/80 hover:text-foreground transition border border-border shadow-sm focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
          title="Zurück (Esc)"
        >
          <ArrowLeft className="size-4" strokeWidth={1.8} />
          <span>Zurück</span>
        </button>

        <div className="flex items-center gap-2">
          <Button
            variant="ghost" size="icon-sm" aria-label="Song-Radio" title="Song-Radio"
            onClick={() => navigate('/library?view=tools&tool=radio')}
            className="size-8 pointer-coarse:size-10 rounded-xl bg-muted text-muted-foreground hover:bg-secondary hover:text-foreground border border-border"
          ><Radio className="size-4" /></Button>
          <Button
            variant="ghost" size="icon-sm" aria-label="Geräteübergabe" title="Geräteübergabe"
            onClick={() => navigate('/library?view=tools&tool=handoff')}
            className="size-8 pointer-coarse:size-10 rounded-xl bg-muted text-muted-foreground hover:bg-secondary hover:text-foreground border border-border"
          ><MonitorSmartphone className="size-4" /></Button>
          {/* Visualizer Menu Button */}
          <div className="relative">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setVisualizerMenuOpen(!visualizerMenuOpen)}
              className={`size-8 pointer-coarse:size-10 rounded-xl transition-colors ${
                visualizerMode !== 'off'
                  ? 'bg-primary/20 text-primary border border-primary/30'
                  : 'bg-muted text-muted-foreground hover:text-foreground hover:bg-secondary border border-border'
              }`}
              title="Visualizer Modus"
              aria-label="Visualizer Modus"
            >
              <Sparkles className="size-4" strokeWidth={1.8} />
            </Button>

            {visualizerMenuOpen && (
              <div className="absolute right-0 top-10 z-50 w-36 rounded-2xl bg-popover border border-input p-1.5 shadow-2xl space-y-1">
                {(['off', 'spectrum', 'waveform'] as VisualizerMode[]).map((mode) => (
                  <button
                    key={mode}
                    type="button"
                    onClick={() => {
                      setVisualizerMode(mode)
                      setVisualizerMenuOpen(false)
                    }}
                    className={`w-full text-left px-3 py-1.5 rounded-xl text-xs font-medium transition-colors ${
                      visualizerMode === mode
                        ? 'bg-primary text-primary-foreground'
                        : 'text-foreground/80 hover:bg-input hover:text-foreground'
                    }`}
                  >
                    {mode === 'off' ? 'Deaktiviert' : mode === 'spectrum' ? 'Spektrum' : 'Oszilloskop'}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Fullscreen Button */}
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={toggleFullscreen}
            className="size-8 pointer-coarse:size-10 rounded-xl text-muted-foreground hover:text-foreground bg-muted hover:bg-secondary border border-border"
            title={isFullscreen ? 'Vollbildmodus beenden' : 'Vollbildmodus aktivieren'}
            aria-label="Vollbild"
          >
            {isFullscreen ? (
              <Minimize2 className="size-4" strokeWidth={1.8} />
            ) : (
              <Maximize2 className="size-4" strokeWidth={1.8} />
            )}
          </Button>
        </div>
      </header>

      {/* Main Responsive Grid Layout (Top-Aligned, Viewport-Constrained) */}
      <div className="player-content relative z-10 min-h-0 flex-1 flex items-start justify-center px-4 py-2 sm:px-8 lg:px-12 max-w-[1440px] mx-auto w-full">
        <div className="player-grid w-full grid grid-cols-1 lg:grid-cols-12 gap-6 lg:gap-10 xl:gap-14 items-start">
          
          {/* ======================================================== */}
          {/* LEFT COLUMN (42%): Height-Aware Artwork + Meta + Controls */}
          {/* ======================================================== */}
          <div className="player-controls lg:col-span-5 min-w-0 flex flex-col items-center lg:items-start space-y-4 max-w-md mx-auto lg:mx-0 w-full">
            
            {/* Artwork Cover (Height-Responsive, Max 420px, Fits on 768p/900p/1080p) */}
            <div className="relative group player-artwork w-full max-w-[clamp(180px,32dvh,380px)] aspect-square rounded-[22px] overflow-hidden shadow-[0_20px_50px_rgba(0,0,0,0.65)] bg-overlay/40 border border-border mx-auto lg:mx-0">
              <Cover
                src={libraryArtwork('tracks', currentTrack.id)} fallbackSrc={currentTrack.cover_url}
                alt={currentTrack.title}
                shape="square"
                className="size-full object-cover transition-transform duration-500 group-hover:scale-[1.02]"
              />

              {/* Visualizer Floating Overlay */}
              {visualizerMode !== 'off' && (
                <div className="absolute inset-x-0 bottom-0 h-14 bg-gradient-to-t from-black/85 via-black/40 to-transparent p-2">
                  <VisualizerCanvas
                    mode={visualizerMode}
                    height={36}
                    barCount={28}
                    showPeakWarning={false}
                  />
                </div>
              )}
            </div>

            {/* Track Title & Artist Metadata (Above the fold) */}
            <div className="w-full text-center lg:text-left space-y-0.5">
              <h1
                className="text-xl sm:text-2xl xl:text-3xl font-bold tracking-tight text-foreground line-clamp-2 leading-tight"
                title={currentTrack.title}
              >
                {currentTrack.title}
              </h1>
              <p className="text-sm sm:text-base text-foreground/80 font-medium line-clamp-1">
                {artistText}
              </p>
              <div className="flex items-center justify-center lg:justify-start gap-2 pt-0.5 text-xs text-muted-foreground">
                {currentTrack.album && (
                  <span className="truncate max-w-[220px]">
                    {currentTrack.album} {currentTrack.year ? `(${currentTrack.year})` : ''}
                  </span>
                )}
                {currentTrack.codec && (
                  <Badge variant="outline" className="font-mono text-[9px] uppercase border-input text-muted-foreground px-1.5 py-0">
                    {currentTrack.codec}
                  </Badge>
                )}
              </div>
            </div>

            {/* Seek bar: the only part of this page that follows the position. */}
            <SeekBar layout="stacked" />

            {/* Primary Playback Controls (Centered, Tight 20-24px Gap) */}
            <div className="flex items-center justify-center lg:justify-start gap-4 sm:gap-5 w-full pt-0.5">
              <ShuffleButton shuffle={shuffle} onToggle={toggleShuffle} className="size-9" iconClassName="size-4.5" />

              {/* Previous */}
              <Button
                variant="ghost"
                size="icon"
                onClick={previous}
                className="size-10 rounded-full text-foreground/80 hover:text-foreground hover:bg-input transition-colors"
                title="Vorheriger Titel / Neustart"
                aria-label="Vorheriger Titel"
              >
                <SkipBack className="size-5.5" strokeWidth={1.8} />
              </Button>

              {/* Play / Pause Main Action (Only Filled Accent Button, 54px) */}
              {/* While buffering it stays a pause button: a click stops the pending start. */}
              <Button
                size="icon"
                onClick={togglePlayPause}
                aria-busy={isBuffering || undefined}
                className="size-13 sm:size-14 rounded-full bg-primary text-primary-foreground shadow-lg shadow-primary/25 hover:bg-primary/90 transition-colors duration-150"
                title={isBuffering ? 'Wird geladen – anhalten' : isPlaying ? 'Pause' : 'Wiedergabe'}
                aria-label={isPlaying || isBuffering ? 'Pause' : 'Wiedergabe'}
              >
                {isBuffering ? (
                  <Loader2 className="size-6 animate-spin" strokeWidth={2} />
                ) : isPlaying ? (
                  <Pause className="size-6 fill-current" strokeWidth={1.8} />
                ) : (
                  <Play className="size-6 fill-current ml-0.5" strokeWidth={1.8} />
                )}
              </Button>

              {/* Next */}
              <Button
                variant="ghost"
                size="icon"
                onClick={() => next(true)}
                className="size-10 rounded-full text-foreground/80 hover:text-foreground hover:bg-input transition-colors"
                title="Nächster Titel"
                aria-label="Nächster Titel"
              >
                <SkipForward className="size-5.5" strokeWidth={1.8} />
              </Button>

              <RepeatButton mode={repeatMode} onCycle={cycleRepeatMode} className="size-9" iconClassName="size-4.5" />
            </div>

            {/* Secondary Controls Bar: Volume & Options Menu */}
            <div className="w-full flex items-center justify-between pointer-coarse:justify-center gap-4 pt-3 border-t border-border">
              {/* Volume Slider (Quiet). Not on touch screens: the device
                  buttons set the volume there, and iOS ignores it. */}
              <div data-slot="volume-control" className="flex pointer-coarse:hidden items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={toggleMute}
                  className="size-7 rounded-lg text-muted-foreground hover:text-foreground"
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
                    className="slider-quiet w-24 outline-none"
                    style={sliderFill(muted ? 0 : volume, 0, 1, 'color-mix(in oklab, var(--foreground) 70%, transparent)')}
                    aria-label="Lautstärkeregler"
                  />
                </div>
              </div>

              {/* Secondary Actions: Favorite, AddToPlaylist, Options Popover */}
              <div className="flex items-center gap-1.5">
                {favorites && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => void favorites.toggleFavorite(currentTrack.id)}
                    className={`h-7 w-7 pointer-coarse:size-10 p-0 rounded-lg transition-colors ${
                      favorites.isFavorite(currentTrack.id)
                        ? 'text-rose-500 hover:text-rose-400 bg-rose-500/10'
                        : 'text-muted-foreground hover:text-rose-400 hover:bg-muted'
                    }`}
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
                      className={`size-3.5 ${
                        favorites.isFavorite(currentTrack.id)
                          ? 'fill-rose-500 text-rose-500'
                          : ''
                      }`}
                    />
                  </Button>
                )}

                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setPlaylistDialogOpen(true)}
                  className="h-7 w-7 pointer-coarse:size-10 p-0 rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted"
                  title="Zur Playlist hinzufügen"
                  aria-label="Zur Playlist hinzufügen"
                >
                  <FolderPlus className="size-3.5" />
                </Button>

                {/* Options Popover (Speed & Sleep Timer) */}
                <div className="relative">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => setOptionsMenuOpen(!optionsMenuOpen)}
                    className="h-7 px-2 pointer-coarse:h-10 pointer-coarse:px-3 rounded-lg bg-muted hover:bg-secondary text-xs text-foreground/80 hover:text-foreground border border-border gap-1.5"
                    title="Wiedergabeoptionen"
                  >
                    <MoreHorizontal className="size-3.5" />
                    <span className="figures text-[11px]">{playbackRate}x</span>
                    {sleepTimer !== 'off' && (
                      <span className="size-1.5 rounded-full bg-primary" />
                    )}
                  </Button>

                  {optionsMenuOpen && (
                    <div className="absolute right-0 bottom-9 pointer-coarse:bottom-12 z-50 w-52 rounded-2xl bg-popover border border-input p-3 shadow-2xl space-y-3">
                      {/* Speed Selection */}
                      <div>
                        <label className="text-[10px] font-semibold uppercase text-muted-foreground tracking-wider block mb-1.5">
                          Geschwindigkeit
                        </label>
                        <div className="grid grid-cols-4 gap-1">
                          {speedOptions.map((rate) => (
                            <button
                              key={rate}
                              type="button"
                              onClick={() => {
                                setPlaybackRate(rate)
                                setOptionsMenuOpen(false)
                              }}
                              className={`py-1 text-xs figures rounded-lg transition-colors ${
                                playbackRate === rate
                                  ? 'bg-primary text-primary-foreground font-bold'
                                  : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                              }`}
                            >
                              {rate}x
                            </button>
                          ))}
                        </div>
                      </div>

                      {/* Sleep Timer */}
                      <div>
                        <label className="text-[10px] font-semibold uppercase text-muted-foreground tracking-wider block mb-1.5">
                          Sleep Timer
                        </label>
                        <div className="grid grid-cols-2 gap-1">
                          {(['off', '15', '30', '45', '60', 'end_of_track'] as SleepTimerOption[]).map(
                            (option) => (
                              <button
                                key={option}
                                type="button"
                                onClick={() => {
                                  setSleepTimer(option)
                                  setOptionsMenuOpen(false)
                                }}
                                className={`py-1 px-2 text-xs rounded-lg text-left transition-colors truncate ${
                                  sleepTimer === option
                                    ? 'bg-primary text-primary-foreground font-semibold'
                                    : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                                }`}
                              >
                                {option === 'off' ? 'Aus' : option === 'end_of_track' ? 'Track-Ende' : `${option} min`}
                              </button>
                            ),
                          )}
                        </div>
                      </div>

                      {/* Sleep Timer Extended Select */}
                      <div>
                        <label htmlFor="sleep-timer-select" className="sr-only">
                          Erweiterte Sleep-Timer Optionen
                        </label>
                        <select
                          id="sleep-timer-select"
                          value={sleepTimer}
                          onChange={(e) => {
                            setSleepTimer(e.target.value as SleepTimerOption)
                            setOptionsMenuOpen(false)
                          }}
                          className="w-full h-7 rounded-lg bg-secondary border border-input px-2 text-xs text-foreground outline-none cursor-pointer"
                        >
                          <option value="off" className="bg-popover">Aus</option>
                          <option value="15" className="bg-popover">15 Minuten</option>
                          <option value="30" className="bg-popover">30 Minuten</option>
                          <option value="45" className="bg-popover">45 Minuten</option>
                          <option value="60" className="bg-popover">60 Minuten</option>
                          <option value="end_of_track" className="bg-popover">Nach aktuellem Titel</option>
                          <option value="end_of_album" className="bg-popover">Nach aktuellem Album</option>
                        </select>
                      </div>
                    </div>
                  )}
                </div>
              </div>
            </div>

          </div>


          {/* ======================================================== */}
          {/* RIGHT COLUMN (58%): Soft Glass Panel (Top-Aligned)       */}
          {/* ======================================================== */}
          <Tabs.Root
            value={activeTab}
            onValueChange={(value) => setActiveTab(value as PrimaryTab)}
            className="player-panel lg:col-span-7 min-w-0 min-h-0 flex flex-col rounded-[22px] bg-muted border border-border shadow-2xl overflow-hidden"
          >

            {/* Panel Tabs Navigation (Subtle Underline Active). Base UI gives
                the tab roles, aria-selected and the arrow keys (activateOnFocus
                selects the tab the arrows land on). */}
            <Tabs.List
              aria-label="Player-Bereiche"
              activateOnFocus
              className="player-tabs grid grid-cols-4 shrink-0 items-center border-b border-border px-2 sm:px-4 pt-2 bg-muted"
            >
              <Tabs.Tab value="lyrics" className={playerTabClass}>
                <Mic2 className="size-4" strokeWidth={1.8} />
                <span>Lyrics</span>
              </Tabs.Tab>

              <Tabs.Tab value="queue" className={playerTabClass}>
                <ListMusic className="size-4" strokeWidth={1.8} />
                <span>Queue</span>
                {queue.length > 0 && (
                  <Badge variant="outline" className="py-0 px-1.5 text-[10px] figures border-input text-muted-foreground">
                    {queue.length}
                  </Badge>
                )}
              </Tabs.Tab>

              <Tabs.Tab value="equalizer" className={playerTabClass}>
                <Sliders className="size-4" strokeWidth={1.8} />
                <span>Equalizer</span>
                {eqEnabled && (
                  <>
                    <span aria-hidden="true" className="size-1.5 rounded-full bg-primary" />
                    <span className="sr-only">(an)</span>
                  </>
                )}
              </Tabs.Tab>

              <Tabs.Tab value="audio" className={playerTabClass}>
                <Shield className="size-4" strokeWidth={1.8} />
                <span>Audio</span>
              </Tabs.Tab>
            </Tabs.List>

            {/* Panel Tab Content (Internally Scrollable) */}
            {activeTab === 'lyrics' && parsedLrcLines && (
              <button type="button" aria-pressed={followLyrics} onClick={() => setFollowLyrics(!followLyrics)}
                className="shrink-0 self-end mx-4 mt-2 rounded-lg px-2 py-1 text-xs text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-primary">
                {followLyrics ? 'Lyrics folgen · Ein' : 'Lyrics folgen · Aus'}
              </button>
            )}
            {/* One panel whose value follows the selected tab: the content
                below switches inside it, and it stays labelled by that tab. */}
            <Tabs.Panel value={activeTab} ref={lyricsContainerRef} className="p-5 sm:p-6 min-h-0 flex-1 overflow-y-auto overscroll-contain outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
              onWheel={() => { if (activeTab === 'lyrics') setFollowLyrics(false) }}
              onTouchMove={() => { if (activeTab === 'lyrics') setFollowLyrics(false) }}>
              
              {/* ---------------------------------------------------- */}
              {/* TAB 1: LYRICS                                        */}
              {/* ---------------------------------------------------- */}
              {activeTab === 'lyrics' && (
                <div className="space-y-4 py-2">
                  {lyricsLoading && (
                    <div className="flex flex-col items-center justify-center py-20 text-muted-foreground">
                      <RefreshCw className="size-6 animate-spin mb-3 text-primary" />
                      <p className="text-sm">Songtext wird geladen...</p>
                    </div>
                  )}

                  {!lyricsLoading && (!lyricsData || (!parsedLrcLines && !lyricsData.content)) && (
                    <div className="flex flex-col items-center justify-center py-16 text-center text-muted-foreground">
                      <Mic2 className="size-10 mb-3 text-muted-foreground/60" />
                      <p className="text-base font-medium text-foreground">Keine Lyrics verfügbar</p>
                      <p className="text-xs text-muted-foreground/80 mt-1 max-w-xs">
                        Für diesen Titel wurden noch keine Liedtexte in der Bibliothek gefunden.
                      </p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRefreshLyrics}
                        disabled={lyricsRefreshing}
                        className="mt-4 h-8 px-3 text-xs rounded-xl bg-muted hover:bg-secondary border-input text-foreground"
                      >
                        {lyricsRefreshing ? (
                          <>
                            <RefreshCw className="size-3.5 mr-1.5 animate-spin" />
                            Suche läuft...
                          </>
                        ) : (
                          <>
                            <RefreshCw className="size-3.5 mr-1.5" />
                            Lyrics nachladen
                          </>
                        )}
                      </Button>
                    </div>
                  )}

                  {!lyricsLoading && lyricsData && parsedLrcLines && parsedLrcLines.length > 0 && (
                    <div className="space-y-5 py-4 text-center lg:text-left">
                      {parsedLrcLines.map((line, idx) => {
                        const isActive = idx === activeLrcIndex
                        return (
                          <div
                            key={idx}
                            ref={isActive ? activeLyricRef : null}
                            onClick={() => seek(line.timeSeconds)}
                            className={`cursor-pointer transition duration-200 select-none py-1 px-3 rounded-xl ${
                              isActive
                                ? 'text-foreground font-bold text-lg sm:text-xl xl:text-2xl bg-muted'
                                : 'text-muted-foreground hover:text-foreground font-medium text-sm sm:text-base xl:text-lg'
                            }`}
                          >
                            {line.text || '♪'}
                          </div>
                        )
                      })}
                    </div>
                  )}

                  {!lyricsLoading && lyricsData && (!parsedLrcLines || parsedLrcLines.length === 0) && lyricsData.content && (
                    <div className="space-y-4 p-2">
                      <div className="whitespace-pre-line text-sm sm:text-base leading-relaxed text-foreground/80">
                        {lyricsData.content}
                      </div>
                      {lyricsData.provider && (
                        <p className="pt-2 text-[11px] text-muted-foreground/80 text-center lg:text-left">
                          Quelle: {lyricsData.provider.toLowerCase() === 'genius' ? 'Genius' : lyricsData.provider.toUpperCase()}
                        </p>
                      )}
                    </div>
                  )}
                </div>
              )}


              {/* ---------------------------------------------------- */}
              {/* TAB 2: QUEUE & HISTORY                               */}
              {/* ---------------------------------------------------- */}
              {activeTab === 'queue' && (
                <div className="space-y-3">
                  <div className="flex items-center justify-between pb-2 border-b border-border">
                    <div className="flex items-center gap-1 bg-muted p-1 rounded-xl">
                      <button
                        type="button"
                        onClick={() => setShowHistory(false)}
                        className={`text-xs font-semibold px-2.5 py-1 rounded-lg transition-colors ${
                          !showHistory ? 'bg-input text-foreground' : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        Queue ({queue.length})
                      </button>
                      <button
                        type="button"
                        onClick={() => setShowHistory(true)}
                        className={`text-xs font-semibold px-2.5 py-1 rounded-lg transition-colors ${
                          showHistory ? 'bg-input text-foreground' : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        Verlauf ({history.length})
                      </button>
                    </div>

                    {!showHistory && (
                      <div className="flex items-center gap-1.5">
                        {queueIndex >= 0 && queueIndex < queue.length - 1 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={clearUpcomingQueue}
                            className="h-7 px-2 text-xs text-muted-foreground hover:text-foreground rounded-lg hover:bg-input"
                            title="Kommende Titel entfernen (bereits gespielte und aktuellen Titel beibehalten)"
                          >
                            Nächste leeren
                          </Button>
                        )}
                        {queue.length > 0 && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={clearQueue}
                            className="h-7 px-2 text-xs text-destructive hover:bg-destructive/10 rounded-lg"
                            title="Komplette Warteschlange leeren und Wiedergabe stoppen"
                          >
                            <Trash2 className="size-3.5 mr-1" />
                            Leeren
                          </Button>
                        )}
                      </div>
                    )}
                  </div>

                  {!showHistory && queue.length === 0 && (
                    <div className="py-16 text-center text-muted-foreground">
                      <ListMusic className="size-8 mx-auto mb-2 text-muted-foreground/60" />
                      <p className="text-xs">Die Warteschlange ist leer</p>
                    </div>
                  )}

                  {/* Active Queue List */}
                  {!showHistory && queue.length > 0 && <QueueList />}

                  {/* History List */}
                  {showHistory && history.length > 0 && (
                    <div className="space-y-1">
                      {history.map((item, idx) => (
                        <div
                          key={`${item.track.id}-${idx}`}
                          onClick={() => playTrack(item.track)}
                          className="flex items-center justify-between p-2 rounded-xl hover:bg-muted cursor-pointer transition-colors"
                        >
                          <div className="flex items-center gap-2.5 min-w-0">
                            <Cover
                              src={libraryArtwork('tracks', item.track.id)} fallbackSrc={item.track.cover_url}
                              alt={item.track.title}
                              shape="square"
                              className="size-9 rounded-lg shrink-0 border border-input"
                            />
                            <div className="min-w-0">
                              <p className="truncate text-xs font-medium text-foreground">
                                {item.track.title}
                              </p>
                              <p className="truncate text-[11px] text-muted-foreground">
                                {item.track.artists?.length > 0 ? joinArtists(item.track.artists) : item.track.album_artist}
                              </p>
                            </div>
                          </div>
                          <span className="figures text-[11px] text-muted-foreground/80">
                            {formatDuration(item.track.duration_ms)}
                          </span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}


              {/* ---------------------------------------------------- */}
              {/* TAB 3: EQUALIZER (Graphic 10-Band with Pill Handles) */}
              {/* ---------------------------------------------------- */}
              {activeTab === 'equalizer' && (
                <div className="space-y-5">
                  {/* Mode & Power Header */}
                  <div className="flex flex-wrap items-center justify-between gap-3 pb-3 border-b border-border">
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant={eqEnabled ? 'secondary' : 'outline'}
                        onClick={() => setEQEnabled(!eqEnabled)}
                        className={`h-7 rounded-lg text-xs font-medium ${
                          eqEnabled ? 'bg-primary text-primary-foreground border-primary shadow-sm shadow-primary/20' : ''
                        }`}
                      >
                        {eqEnabled ? 'Aktiviert' : 'Bypass'}
                      </Button>

                      {/* Switch between Graphic & Parametric */}
                      <div className="flex items-center rounded-lg bg-muted p-0.5 border border-border text-xs">
                        <button
                          type="button"
                          onClick={() => setEQMode('graphic')}
                          className={`rounded-md px-2.5 py-0.5 text-[11px] transition-colors ${
                            eqMode === 'graphic' ? 'bg-input text-foreground font-semibold' : 'text-muted-foreground hover:text-foreground'
                          }`}
                        >
                          10-Band
                        </button>
                        <button
                          type="button"
                          onClick={() => setEQMode('parametric')}
                          className={`rounded-md px-2.5 py-0.5 text-[11px] transition-colors ${
                            eqMode === 'parametric' ? 'bg-input text-foreground font-semibold' : 'text-muted-foreground hover:text-foreground'
                          }`}
                        >
                          Parametrisch
                        </button>
                      </div>
                    </div>

                    {/* Presets dropdown for Graphic mode */}
                    {eqMode === 'graphic' && (
                      <div className="flex items-center gap-1.5">
                        <select
                          value={selectedPresetId}
                          onChange={(e) => setEQPreset(e.target.value)}
                          className="h-7 rounded-lg bg-muted border border-input px-2 text-xs text-foreground hover:text-foreground outline-none cursor-pointer"
                          aria-label="Preset"
                        >
                          <optgroup label="Werkspresets">
                            {BUILTIN_PRESETS.map((p) => (
                              <option key={p.id} value={p.id} className="bg-popover">
                                {p.name}
                              </option>
                            ))}
                          </optgroup>
                          {customPresets.length > 0 && (
                            <optgroup label="Eigene Presets">
                              {customPresets.map((p) => (
                                <option key={p.id} value={p.id} className="bg-popover">
                                  {p.name}
                                </option>
                              ))}
                            </optgroup>
                          )}
                          {selectedPresetId === 'custom' && (
                            <option value="custom" className="bg-popover">Benutzerdefiniert</option>
                          )}
                        </select>

                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setSavePresetOpen(true)}
                          className="h-7 px-2 text-xs rounded-lg bg-muted hover:bg-input border border-border"
                          title="Als Preset speichern"
                        >
                          <Save className="size-3.5 mr-1" />
                          Speichern
                        </Button>

                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setEQPreset('flat')}
                          className="h-7 px-1.5 text-xs rounded-lg text-muted-foreground hover:text-foreground hover:bg-muted"
                          title="Auf 0 dB zurücksetzen"
                        >
                          <RotateCcw className="size-3.5" />
                        </Button>
                      </div>
                    )}
                  </div>

                  {/* Save Preset Dialog */}
                  {savePresetOpen && (
                    <div className="flex items-center gap-2 p-2.5 bg-muted rounded-xl border border-input">
                      <Input
                        type="text"
                        placeholder="Preset Name..."
                        value={customPresetName}
                        onChange={(e) => setCustomPresetName(e.target.value)}
                        className="h-7 text-xs rounded-lg bg-overlay/40"
                      />
                      <Button
                        size="sm"
                        onClick={() => {
                          if (customPresetName.trim()) {
                            saveCustomPreset(customPresetName)
                            setCustomPresetName('')
                            setSavePresetOpen(false)
                          }
                        }}
                        className="h-7 text-xs rounded-lg bg-primary text-primary-foreground"
                      >
                        Speichern
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => setSavePresetOpen(false)}
                        className="h-7 text-xs rounded-lg"
                      >
                        Abbrechen
                      </Button>
                    </div>
                  )}

                  {/* 10-Band Graphic EQ View with Pill Handles (NO Round Balls!) */}
                  {eqMode === 'graphic' && (
                    <div className="grid grid-cols-10 gap-1 sm:gap-2 py-2 px-1">
                      {EQ_FREQUENCIES.map((freq, idx) => {
                        const gain = graphicBands[idx] ?? 0
                        return (
                          <div key={freq} className="flex flex-col items-center gap-2 group">
                            <span className="figures text-[10px] text-foreground/80">
                              {gain > 0 ? `+${gain.toFixed(1)}` : gain.toFixed(1)}
                            </span>

                            {/* Vertical Slider with Pill Handle */}
                            <div className="relative h-40 sm:h-48 flex items-center justify-center">
                              <input
                                type="range"
                                min={-12}
                                max={12}
                                step={0.5}
                                value={gain}
                                onChange={(e) => setEQBand(idx, parseFloat(e.target.value))}
                                className="slider-eq h-40 sm:h-48 w-6 outline-none"
                                aria-label={`Band ${formatFrequency(freq)}`}
                              />
                            </div>

                            <span className="text-[9px] sm:text-[10px] figures text-muted-foreground text-center select-none">
                              {formatFrequency(freq)}
                            </span>
                          </div>
                        )
                      })}
                    </div>
                  )}

                  {/* Parametric EQ View */}
                  {eqMode === 'parametric' && (
                    <div className="space-y-3">
                      <div className="flex items-center justify-between">
                        <p className="text-xs text-muted-foreground">
                          Präzise parametrische Filter mit Frequenz, Gain und Gütefaktor (Q).
                        </p>
                        <Button
                          size="sm"
                          onClick={handleAddParametricFilter}
                          disabled={parametricFilters.length >= 10}
                          className="h-7 text-xs rounded-lg bg-primary text-primary-foreground gap-1"
                        >
                          <Plus className="size-3.5" />
                          Filter
                        </Button>
                      </div>

                      <div className="space-y-2.5">
                        {parametricFilters.map((filter, index) => (
                          <div
                            key={filter.id}
                            className="p-3 rounded-xl bg-muted border border-border space-y-2"
                          >
                            <div className="flex items-center justify-between gap-2">
                              <div className="flex items-center gap-2">
                                <span className="text-xs figures text-muted-foreground">#{index + 1}</span>
                                <select
                                  value={filter.type}
                                  onChange={(e) =>
                                    setParametricFilter({
                                      ...filter,
                                      type: e.target.value as ParametricFilterType,
                                    })
                                  }
                                  className="h-6 rounded bg-secondary border border-input px-1.5 text-xs text-foreground outline-none"
                                >
                                  <option value="peaking" className="bg-popover">Peaking</option>
                                  <option value="lowshelf" className="bg-popover">Low Shelf</option>
                                  <option value="highshelf" className="bg-popover">High Shelf</option>
                                  <option value="lowpass" className="bg-popover">Low Pass</option>
                                  <option value="highpass" className="bg-popover">High Pass</option>
                                </select>
                              </div>

                              <div className="flex items-center gap-2">
                                <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
                                  <input
                                    type="checkbox"
                                    checked={filter.enabled}
                                    onChange={(e) =>
                                      setParametricFilter({
                                        ...filter,
                                        enabled: e.target.checked,
                                      })
                                    }
                                    className="rounded size-3.5 border-input bg-muted text-primary"
                                  />
                                  <span>Aktiv</span>
                                </label>
                                <button
                                  type="button"
                                  onClick={() => deleteParametricFilter(filter.id)}
                                  className="p-1 rounded hover:bg-input text-muted-foreground hover:text-destructive transition-colors"
                                  title="Filter löschen"
                                >
                                  <Trash2 className="size-3.5" />
                                </button>
                              </div>
                            </div>

                            {/* Sliders with Quiet Thumb Styling */}
                            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 pt-1">
                              <div className="space-y-1">
                                <div className="flex justify-between text-[10px] text-muted-foreground">
                                  <span>Frequenz</span>
                                  <span className="figures text-foreground">{filter.frequency} Hz</span>
                                </div>
                                <input
                                  type="range"
                                  min={20}
                                  max={20000}
                                  step={10}
                                  value={filter.frequency}
                                  onChange={(e) =>
                                    setParametricFilter({
                                      ...filter,
                                      frequency: parseFloat(e.target.value),
                                    })
                                  }
                                  className="slider-quiet w-full outline-none"
                                />
                              </div>

                              <div className="space-y-1">
                                <div className="flex justify-between text-[10px] text-muted-foreground">
                                  <span>Gain</span>
                                  <span className="figures text-foreground">
                                    {filter.gain > 0 ? `+${filter.gain}` : filter.gain} dB
                                  </span>
                                </div>
                                <input
                                  type="range"
                                  min={-12}
                                  max={12}
                                  step={0.5}
                                  value={filter.gain}
                                  onChange={(e) =>
                                    setParametricFilter({
                                      ...filter,
                                      gain: parseFloat(e.target.value),
                                    })
                                  }
                                  className="slider-quiet w-full outline-none"
                                />
                              </div>

                              <div className="space-y-1">
                                <div className="flex justify-between text-[10px] text-muted-foreground">
                                  <span>Güte (Q)</span>
                                  <span className="figures text-foreground">{filter.q.toFixed(2)}</span>
                                </div>
                                <input
                                  type="range"
                                  min={0.1}
                                  max={18}
                                  step={0.1}
                                  value={filter.q}
                                  onChange={(e) =>
                                    setParametricFilter({
                                      ...filter,
                                      q: parseFloat(e.target.value),
                                    })
                                  }
                                  className="slider-quiet w-full outline-none"
                                />
                              </div>
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}


              {/* ---------------------------------------------------- */}
              {/* TAB 4: AUDIO & DSP PROCESSING                        */}
              {/* ---------------------------------------------------- */}
              {activeTab === 'audio' && (
                <div className="space-y-4">
                  {/* Section 4: Crossfade Transition */}
                  <div className="p-3.5 rounded-xl bg-muted border border-border space-y-2.5">
                    <div className="space-y-2 rounded-lg border border-border p-3">
                      <label className="flex items-center justify-between gap-3 text-sm">
                        <span>Lautstärke angleichen</span>
                        <input type="checkbox" checked={normalizationEnabled}
                          onChange={(e) => setNormalization(e.target.checked)} />
                      </label>
                      <p className="text-xs text-muted-foreground">
                        Passt die Wiedergabe pro Titel an. Aktuelle und nächste Titel werden einmal
                        gemessen; Audiodateien bleiben unverändert.
                      </p>
                      {normalizationEnabled && <p role="status" className="text-xs text-muted-foreground">{normalizationMessage}</p>}
                    </div>
                    <div className="flex items-center justify-between text-xs text-foreground/80">
                      <label htmlFor="player-crossfade">Überblendung zwischen Liedern</label>
                      <span className="figures text-foreground">{crossfadeSeconds === 0 ? 'Aus' : `${crossfadeSeconds} s`}</span>
                    </div>
                    <input id="player-crossfade" type="range" min={0} max={12} step={1}
                      value={crossfadeSeconds} onChange={(e) => setCrossfade(Number(e.target.value))}
                      aria-valuetext={crossfadeSeconds === 0 ? 'Aus' : `${crossfadeSeconds} Sekunden`}
                      className="slider-quiet w-full" />
                    <p className="text-[11px] text-muted-foreground">Der nächste Titel beginnt leise, während der aktuelle ausklingt. Diese Einstellung wird in diesem Browser gespeichert.</p>
                    <div className="flex items-center gap-1.5">
                      {[0, 3, 6, 12].map((s) => (
                        <button
                          key={s}
                          type="button"
                          onClick={() => setCrossfade(s)}
                          aria-pressed={crossfadeSeconds === s}
                          className={`flex-1 py-1 rounded-lg text-xs font-medium transition ${
                            crossfadeSeconds === s
                              ? 'bg-primary text-primary-foreground shadow-sm shadow-primary/20'
                              : 'bg-muted text-muted-foreground hover:text-foreground hover:bg-secondary'
                          }`}
                        >
                          {s === 0 ? 'Aus' : `${s}s`}
                        </button>
                      ))}
                    </div>

                    <div className="pt-2 border-t border-border flex items-center justify-between">
                      <div>
                        <p className="text-xs font-medium text-foreground">Album-Reihenfolge erhalten</p>
                        <p className="text-[11px] text-muted-foreground">
                          Deaktiviert Crossfade automatisch bei aufeinanderfolgenden Tracks desselben Albums.
                        </p>
                      </div>
                      <input
                        type="checkbox"
                        aria-label="Aufeinanderfolgende Albumtitel ohne Überblendung"
                        checked={smartAlbumTransition}
                        onChange={(e) => setSmartAlbumTransition(e.target.checked)}
                        className="rounded size-4 border-input bg-muted text-primary"
                      />
                    </div>
                  </div>

                  {/* Section 1: Preamp & Headroom Schutz */}
                  <div className="p-3.5 rounded-xl bg-muted border border-border space-y-3">
                    <h3 className="text-[11px] font-semibold text-foreground/80 uppercase tracking-wider">Klang & Pegelschutz</h3>
                    
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between text-xs text-foreground/80">
                        <span>Vorverstärkung (Preamp)</span>
                        <span className="figures font-medium text-foreground">
                          {preamp > 0 ? `+${preamp}` : preamp} dB
                        </span>
                      </div>
                      <input
                        type="range"
                        min={-12}
                        max={6}
                        step={0.5}
                        value={preamp}
                        onChange={(e) => setPreamp(parseFloat(e.target.value))}
                        className="slider-quiet w-full outline-none"
                        aria-label="Preamp"
                      />
                    </div>

                    <div className="pt-2 border-t border-border flex items-center justify-between">
                      <div>
                        <p className="text-xs font-medium text-foreground">Auto Headroom</p>
                        <p className="text-[11px] text-muted-foreground">
                          Senkt den Preamp automatisch ab, um digitales Clipping bei aktiven EQ-Bands zu verhindern.
                        </p>
                      </div>
                      <input
                        type="checkbox"
                        checked={autoHeadroom}
                        onChange={(e) => setAutoHeadroom(e.target.checked)}
                        className="rounded size-4 border-input bg-muted text-primary focus:ring-primary"
                      />
                    </div>
                  </div>

                  {/* Section 2: Safety Limiter & Channel Mode */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    {/* Limiter Safety */}
                    <div className="p-3.5 rounded-xl bg-muted border border-border space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-foreground">Audio Safety Limiter</span>
                        <input
                          type="checkbox"
                          checked={limiterEnabled}
                          onChange={(e) => setLimiter(e.target.checked)}
                          className="rounded size-4 border-input bg-muted text-primary"
                        />
                      </div>
                      <p className="text-[11px] text-muted-foreground">
                        Schützt vor Übersteuerung mit einem Hard-Limiter bei -0.5 dBFS.
                      </p>
                    </div>

                    {/* Mono Summierung */}
                    <div className="p-3.5 rounded-xl bg-muted border border-border space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-foreground">Mono Summierung</span>
                        <input
                          type="checkbox"
                          checked={mono}
                          onChange={(e) => setMono(e.target.checked)}
                          className="rounded size-4 border-input bg-muted text-primary"
                        />
                      </div>
                      <p className="text-[11px] text-muted-foreground">
                        Summiert L/R mit kompensiertem -3dB Pegel für Mono-Lautsprecher.
                      </p>
                    </div>
                  </div>

                  {/* Section 3: Stereo Balance */}
                  <div className="p-3.5 rounded-xl bg-muted border border-border space-y-2">
                    <div className="flex items-center justify-between text-xs text-foreground/80">
                      <span>Stereo Balance (L/R)</span>
                      <span className="figures text-foreground">
                        {balance === 0 ? 'Mitte' : balance < 0 ? `L ${Math.abs(Math.round(balance * 100))}%` : `R ${Math.round(balance * 100)}%`}
                      </span>
                    </div>
                    <div className="relative flex items-center py-1">
                      {/* Center Notch Indicator */}
                      <div className="absolute left-1/2 -translate-x-1/2 w-0.5 h-2 bg-input rounded pointer-events-none" />
                      <input
                        type="range"
                        min={-1}
                        max={1}
                        step={0.05}
                        value={balance}
                        onChange={(e) => setBalance(parseFloat(e.target.value))}
                        className="slider-quiet w-full outline-none"
                      />
                    </div>
                  </div>


                </div>
              )}

            </Tabs.Panel>
          </Tabs.Root>

        </div>
      </div>
      <AddToPlaylistDialog
        track={currentTrack}
        open={playlistDialogOpen}
        onOpenChange={setPlaylistDialogOpen}
      />
    </div>
  )
}
