import { useEffect, useMemo, useRef, useState } from 'react'
import {
  ArrowLeft,
  ListMusic,
  Maximize2,
  Mic2,
  Minimize2,
  MoreHorizontal,
  Music2,
  Pause,
  Play,
  Plus,
  RefreshCw,
  Repeat,
  Repeat1,
  RotateCcw,
  Save,
  Shield,
  Shuffle,
  SkipBack,
  SkipForward,
  Sliders,
  Sparkles,
  Trash2,
  Volume2,
  VolumeX,
  X,
} from 'lucide-react'

import { Cover } from '@/components/music/Cover'
import { VisualizerCanvas } from '@/components/player/VisualizerCanvas'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { usePlayer } from '@/hooks/usePlayer'
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
import { Link, paths, useLocation } from '@/lib/router'
import { formatDuration, joinArtists } from '@/lib/utils/format'
import { parseLrc } from '@/lib/utils/lrc'
import type { TrackLyrics } from '@/types/api'

type PrimaryTab = 'lyrics' | 'queue' | 'equalizer' | 'audio'

export function NowPlaying() {
  const location = useLocation()
  const tabParam = location.params.get('tab')

  const {
    currentTrack,
    queue,
    queueIndex,
    status,
    error: playbackError,
    currentTime,
    duration,
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
    setBalance,
    setMono,
    setVisualizerMode,
    removeFromQueue,
    clearQueue,
  } = usePlayer()

  const initialTab: PrimaryTab =
    tabParam === 'eq' || tabParam === 'parametric'
      ? 'equalizer'
      : tabParam === 'dsp'
        ? 'audio'
        : tabParam === 'queue'
          ? 'queue'
          : 'lyrics'

  const [activeTab, setActiveTab] = useState<PrimaryTab>(initialTab)
  const [seekingValue, setSeekingValue] = useState<number | null>(null)
  const [savePresetOpen, setSavePresetOpen] = useState(false)
  const [customPresetName, setCustomPresetName] = useState('')
  const [showHistory, setShowHistory] = useState(false)
  const [isFullscreen, setIsFullscreen] = useState(false)
  const [visualizerMenuOpen, setVisualizerMenuOpen] = useState(false)
  const [optionsMenuOpen, setOptionsMenuOpen] = useState(false)
  const [errorDismissed, setErrorDismissed] = useState(false)

  // Lyrics state
  const [lyricsData, setLyricsData] = useState<TrackLyrics | null>(null)
  const [lyricsLoading, setLyricsLoading] = useState(false)
  const [lyricsRefreshing, setLyricsRefreshing] = useState(false)
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

  // Manual Lyrics Refresh Handler
  const handleRefreshLyrics = async () => {
    if (!currentTrack || lyricsRefreshing) return
    setLyricsRefreshing(true)
    try {
      const refreshed = await refreshTrackLyrics(currentTrack.id)
      setLyricsData(refreshed)
    } catch {
      // Refresh error handled safely
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

  // Current active synced line index
  const activeLrcIndex = useMemo(() => {
    if (!parsedLrcLines || parsedLrcLines.length === 0) return -1
    const time = currentTime
    let activeIdx = -1
    for (let i = 0; i < parsedLrcLines.length; i++) {
      const line = parsedLrcLines[i]
      if (line && line.timeSeconds <= time) {
        activeIdx = i
      } else {
        break
      }
    }
    return activeIdx
  }, [parsedLrcLines, currentTime])

  // Auto-scroll active lyric line into view smoothly
  useEffect(() => {
    if (activeLyricRef.current) {
      activeLyricRef.current.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
      })
    }
  }, [activeLrcIndex])

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
            className="inline-flex items-center gap-2 rounded-xl bg-primary px-4 py-2 text-sm font-medium text-primary-foreground shadow-lg shadow-primary/20 hover:bg-primary/90 transition-all"
          >
            Zur Musikbibliothek
          </Link>
        </div>
      </div>
    )
  }

  const isPlaying = status === 'playing'
  const isBuffering = status === 'buffering'
  const displayTime = seekingValue ?? currentTime
  const progressPercent = duration > 0 ? Math.min(100, (displayTime / duration) * 100) : 0
  const artistText =
    currentTrack.artists?.length > 0
      ? joinArtists(currentTrack.artists)
      : currentTrack.album_artist || ''

  const speedOptions = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]

  const handleAddParametricFilter = () => {
    const newFilter: ParametricFilter = {
      id: `filter-${Date.now()}`,
      type: 'peaking',
      frequency: 1000,
      gain: 0,
      q: 1.0,
      enabled: true,
    }
    addParametricFilter(newFilter)
  }

  return (
    <div className="player-enter relative min-h-dvh flex flex-col bg-[#05070d] text-foreground select-none overflow-x-hidden">
      
      {/* Dynamic Immersive Background Atmosphere from Cover Art */}
      <div className="pointer-events-none absolute inset-0 overflow-hidden opacity-25 filter blur-3xl scale-125 transition-all duration-1000">
        <div
          className="absolute inset-0 bg-cover bg-center"
          style={{
            backgroundImage: currentTrack.cover_url ? `url("${currentTrack.cover_url}")` : undefined,
          }}
        />
        <div className="absolute inset-0 bg-radial from-transparent via-[#05070d]/70 to-[#05070d]" />
      </div>

      {/* Floating Error Toast (Never Displaces Main Grid) */}
      {playbackError && !errorDismissed && (
        <div className="fixed top-16 right-6 z-50 max-w-sm rounded-2xl bg-destructive/20 border border-destructive/40 p-3.5 text-xs text-destructive-foreground backdrop-blur-xl shadow-2xl flex items-center justify-between gap-3 animate-in fade-in slide-in-from-top-2 duration-200">
          <div className="min-w-0">
            <p className="font-semibold">{playbackError}</p>
            <p className="text-neutral-400 text-[11px] mt-0.5">
              Der Titel konnte nicht geladen werden.
            </p>
          </div>
          <div className="flex items-center gap-1.5 shrink-0">
            <Button
              size="sm"
              variant="outline"
              onClick={() => playTrack(currentTrack)}
              className="h-7 px-2 text-xs border-destructive/40 text-foreground hover:bg-destructive/20"
            >
              Erneut
            </Button>
            <Button
              size="sm"
              onClick={() => next(true)}
              className="h-7 px-2 text-xs bg-destructive text-white hover:bg-destructive/80"
            >
              Weiter
            </Button>
            <button
              type="button"
              onClick={() => setErrorDismissed(true)}
              className="p-1 rounded-lg hover:bg-white/10 text-neutral-400 hover:text-white transition-colors"
              title="Meldung schließen"
            >
              <X className="size-3.5" />
            </button>
          </div>
        </div>
      )}

      {/* Top Header Bar (Clean & Minimal) */}
      <header className="relative z-20 flex h-14 shrink-0 items-center justify-between px-4 sm:px-8">
        <Link
          href={paths.library ? paths.library() : '/library'}
          className="flex items-center gap-2 rounded-xl bg-white/[0.04] hover:bg-white/[0.08] px-3 py-1.5 text-xs font-medium text-neutral-300 hover:text-white transition-all border border-white/5 shadow-sm"
          title="Zurück zur Bibliothek"
        >
          <ArrowLeft className="size-4" strokeWidth={1.8} />
          <span>Bibliothek</span>
        </Link>

        <div className="flex items-center gap-2">
          {/* Visualizer Menu Button */}
          <div className="relative">
            <Button
              variant="ghost"
              size="icon-sm"
              onClick={() => setVisualizerMenuOpen(!visualizerMenuOpen)}
              className={`size-8 rounded-xl transition-colors ${
                visualizerMode !== 'off'
                  ? 'bg-primary/20 text-primary border border-primary/30'
                  : 'bg-white/[0.04] text-neutral-400 hover:text-white hover:bg-white/[0.08] border border-white/5'
              }`}
              title="Visualizer Modus"
              aria-label="Visualizer Modus"
            >
              <Sparkles className="size-4" strokeWidth={1.8} />
            </Button>

            {visualizerMenuOpen && (
              <div className="absolute right-0 top-10 z-50 w-36 rounded-2xl bg-[#0f1220]/95 border border-white/10 p-1.5 shadow-2xl backdrop-blur-xl space-y-1">
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
                        ? 'bg-primary text-white'
                        : 'text-neutral-300 hover:bg-white/10 hover:text-white'
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
            className="size-8 rounded-xl text-neutral-400 hover:text-white bg-white/[0.04] hover:bg-white/[0.08] border border-white/5"
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
      <main className="relative z-10 flex-1 flex items-start justify-center px-4 py-2 sm:px-8 lg:px-12 max-w-[1440px] mx-auto w-full">
        <div className="w-full grid grid-cols-1 lg:grid-cols-12 gap-6 lg:gap-10 xl:gap-14 items-start">
          
          {/* ======================================================== */}
          {/* LEFT COLUMN (42%): Height-Aware Artwork + Meta + Controls */}
          {/* ======================================================== */}
          <div className="lg:col-span-5 flex flex-col items-center lg:items-start space-y-4 max-w-md mx-auto lg:mx-0 w-full">
            
            {/* Artwork Cover (Height-Responsive, Max 420px, Fits on 768p/900p/1080p) */}
            <div className="relative group w-full max-w-[clamp(240px,36vh,420px)] aspect-square rounded-[22px] overflow-hidden shadow-[0_20px_50px_rgba(0,0,0,0.65)] bg-black/40 border border-white/[0.06] mx-auto lg:mx-0">
              <Cover
                src={currentTrack.cover_url}
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
                className="text-xl sm:text-2xl xl:text-3xl font-bold tracking-tight text-white line-clamp-2 leading-tight"
                title={currentTrack.title}
              >
                {currentTrack.title}
              </h1>
              <p className="text-sm sm:text-base text-neutral-300 font-medium line-clamp-1">
                {artistText}
              </p>
              <div className="flex items-center justify-center lg:justify-start gap-2 pt-0.5 text-xs text-neutral-400">
                {currentTrack.album && (
                  <span className="truncate max-w-[220px]">
                    {currentTrack.album} {currentTrack.year ? `(${currentTrack.year})` : ''}
                  </span>
                )}
                {currentTrack.codec && (
                  <Badge variant="outline" className="font-mono text-[9px] uppercase border-white/10 text-neutral-400 px-1.5 py-0">
                    {currentTrack.codec}
                  </Badge>
                )}
              </div>
            </div>

            {/* Seekbar & Timestamps (Quiet Slider - Thumb on Hover only) */}
            <div className="w-full space-y-1.5 pt-1">
              <div className="relative flex items-center py-1 group">
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

              <div className="flex items-center justify-between text-[11px] font-mono text-neutral-400">
                <span className="tabular-nums">{formatDuration(displayTime * 1000)}</span>
                <span className="tabular-nums">{formatDuration(duration * 1000)}</span>
              </div>
            </div>

            {/* Primary Playback Controls (Centered, Tight 20-24px Gap) */}
            <div className="flex items-center justify-center lg:justify-start gap-4 sm:gap-5 w-full pt-0.5">
              {/* Shuffle */}
              <Button
                variant="ghost"
                size="icon"
                onClick={toggleShuffle}
                className={`size-9 rounded-full transition-colors ${
                  shuffle
                    ? 'text-primary hover:text-primary/80'
                    : 'text-neutral-400 hover:text-white'
                }`}
                title={shuffle ? 'Zufallswiedergabe: Ein' : 'Zufallswiedergabe: Aus'}
                aria-label="Zufallswiedergabe"
              >
                <Shuffle className="size-4.5" strokeWidth={1.8} />
              </Button>

              {/* Previous */}
              <Button
                variant="ghost"
                size="icon"
                onClick={previous}
                className="size-10 rounded-full text-neutral-300 hover:text-white hover:bg-white/10 transition-colors"
                title="Vorheriger Titel / Neustart"
                aria-label="Vorheriger Titel"
              >
                <SkipBack className="size-5.5" strokeWidth={1.8} />
              </Button>

              {/* Play / Pause Main Action (Only Filled Accent Button, 54px) */}
              <Button
                size="icon"
                onClick={togglePlayPause}
                disabled={isBuffering}
                className="size-13 sm:size-14 rounded-full bg-primary text-primary-foreground shadow-lg shadow-primary/25 hover:scale-[1.03] active:scale-[0.97] transition-all duration-150"
                title={isPlaying ? 'Pause' : 'Wiedergabe'}
                aria-label={isPlaying ? 'Pause' : 'Wiedergabe'}
              >
                {isPlaying ? (
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
                className="size-10 rounded-full text-neutral-300 hover:text-white hover:bg-white/10 transition-colors"
                title="Nächster Titel"
                aria-label="Nächster Titel"
              >
                <SkipForward className="size-5.5" strokeWidth={1.8} />
              </Button>

              {/* Repeat */}
              <Button
                variant="ghost"
                size="icon"
                onClick={cycleRepeatMode}
                className={`size-9 rounded-full transition-colors ${
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
                  <Repeat1 className="size-4.5" strokeWidth={1.8} />
                ) : (
                  <Repeat className="size-4.5" strokeWidth={1.8} />
                )}
              </Button>
            </div>

            {/* Secondary Controls Bar: Volume & Options Menu */}
            <div className="w-full flex items-center justify-between gap-4 pt-3 border-t border-white/[0.06]">
              {/* Volume Slider (Quiet) */}
              <div className="flex items-center gap-2">
                <Button
                  variant="ghost"
                  size="icon-sm"
                  onClick={toggleMute}
                  className="size-7 rounded-lg text-neutral-400 hover:text-white"
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
                    style={{
                      background: `linear-gradient(to right, rgba(255,255,255,0.7) 0%, rgba(255,255,255,0.7) ${(muted ? 0 : volume) * 100}%, rgba(255,255,255,0.15) ${(muted ? 0 : volume) * 100}%, rgba(255,255,255,0.15) 100%)`,
                    }}
                    aria-label="Lautstärkeregler"
                  />
                </div>
              </div>

              {/* Options Popover (Speed & Sleep Timer) */}
              <div className="relative">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setOptionsMenuOpen(!optionsMenuOpen)}
                  className="h-7 px-2 rounded-lg bg-white/[0.04] hover:bg-white/[0.08] text-xs text-neutral-300 hover:text-white border border-white/5 gap-1.5"
                  title="Wiedergabeoptionen"
                >
                  <MoreHorizontal className="size-3.5" />
                  <span className="font-mono text-[11px]">{playbackRate}x</span>
                  {sleepTimer !== 'off' && (
                    <span className="size-1.5 rounded-full bg-primary" />
                  )}
                </Button>

                {optionsMenuOpen && (
                  <div className="absolute right-0 bottom-9 z-50 w-52 rounded-2xl bg-[#0f1220]/95 border border-white/10 p-3 shadow-2xl backdrop-blur-xl space-y-3">
                    {/* Speed Selection */}
                    <div>
                      <label className="text-[10px] font-semibold uppercase text-neutral-400 tracking-wider block mb-1.5">
                        Geschwindigkeit
                      </label>
                      <div className="grid grid-cols-4 gap-1">
                        {speedOptions.map((rate) => (
                          <button
                            key={rate}
                            type="button"
                            onClick={() => setPlaybackRate(rate)}
                            className={`py-1 rounded-lg text-xs font-mono transition-colors ${
                              playbackRate === rate
                                ? 'bg-primary text-white font-bold'
                                : 'bg-white/5 text-neutral-300 hover:bg-white/10'
                            }`}
                          >
                            {rate}x
                          </button>
                        ))}
                      </div>
                    </div>

                    {/* Sleep Timer Selection */}
                    <div className="pt-2 border-t border-white/10">
                      <label className="text-[10px] font-semibold uppercase text-neutral-400 tracking-wider block mb-1.5">
                        Sleep Timer
                      </label>
                      <select
                        value={sleepTimer}
                        onChange={(e) => {
                          setSleepTimer(e.target.value as SleepTimerOption)
                        }}
                        className="w-full h-7 rounded-lg bg-white/[0.06] border border-white/10 px-2 text-xs text-white outline-none cursor-pointer"
                      >
                        <option value="off" className="bg-[#121524]">Aus</option>
                        <option value="15" className="bg-[#121524]">15 Minuten</option>
                        <option value="30" className="bg-[#121524]">30 Minuten</option>
                        <option value="45" className="bg-[#121524]">45 Minuten</option>
                        <option value="60" className="bg-[#121524]">60 Minuten</option>
                        <option value="end_of_track" className="bg-[#121524]">Nach aktuellem Titel</option>
                        <option value="end_of_album" className="bg-[#121524]">Nach aktuellem Album</option>
                      </select>
                    </div>
                  </div>
                )}
              </div>
            </div>

          </div>


          {/* ======================================================== */}
          {/* RIGHT COLUMN (58%): Soft Glass Panel (Top-Aligned)       */}
          {/* ======================================================== */}
          <div className="lg:col-span-7 flex flex-col rounded-[22px] bg-white/[0.02] border border-white/[0.05] backdrop-blur-xl shadow-2xl overflow-hidden max-h-[calc(100dvh-96px)] min-h-[500px]">
            
            {/* Panel Tabs Navigation (Subtle Underline Active) */}
            <div className="flex items-center border-b border-white/[0.05] px-4 pt-2 bg-white/[0.01]">
              <button
                type="button"
                onClick={() => setActiveTab('lyrics')}
                className={`flex items-center gap-2 px-4 py-2.5 text-xs sm:text-sm font-semibold transition-all border-b-2 ${
                  activeTab === 'lyrics'
                    ? 'border-primary text-white bg-white/[0.02]'
                    : 'border-transparent text-neutral-400 hover:text-neutral-200'
                }`}
              >
                <Mic2 className="size-4" strokeWidth={1.8} />
                <span>Lyrics</span>
              </button>

              <button
                type="button"
                onClick={() => setActiveTab('queue')}
                className={`flex items-center gap-2 px-4 py-2.5 text-xs sm:text-sm font-semibold transition-all border-b-2 ${
                  activeTab === 'queue'
                    ? 'border-primary text-white bg-white/[0.02]'
                    : 'border-transparent text-neutral-400 hover:text-neutral-200'
                }`}
              >
                <ListMusic className="size-4" strokeWidth={1.8} />
                <span>Queue</span>
                {queue.length > 0 && (
                  <Badge variant="outline" className="py-0 px-1.5 text-[10px] font-mono border-white/10 text-neutral-400">
                    {queue.length}
                  </Badge>
                )}
              </button>

              <button
                type="button"
                onClick={() => setActiveTab('equalizer')}
                className={`flex items-center gap-2 px-4 py-2.5 text-xs sm:text-sm font-semibold transition-all border-b-2 ${
                  activeTab === 'equalizer'
                    ? 'border-primary text-white bg-white/[0.02]'
                    : 'border-transparent text-neutral-400 hover:text-neutral-200'
                }`}
              >
                <Sliders className="size-4" strokeWidth={1.8} />
                <span>Equalizer</span>
                {eqEnabled && (
                  <span className="size-1.5 rounded-full bg-primary" />
                )}
              </button>

              <button
                type="button"
                onClick={() => setActiveTab('audio')}
                className={`flex items-center gap-2 px-4 py-2.5 text-xs sm:text-sm font-semibold transition-all border-b-2 ${
                  activeTab === 'audio'
                    ? 'border-primary text-white bg-white/[0.02]'
                    : 'border-transparent text-neutral-400 hover:text-neutral-200'
                }`}
              >
                <Shield className="size-4" strokeWidth={1.8} />
                <span>Audio</span>
              </button>
            </div>

            {/* Panel Tab Content (Internally Scrollable) */}
            <div className="p-5 sm:p-6 flex-1 overflow-y-auto">
              
              {/* ---------------------------------------------------- */}
              {/* TAB 1: LYRICS                                        */}
              {/* ---------------------------------------------------- */}
              {activeTab === 'lyrics' && (
                <div ref={lyricsContainerRef} className="lyrics-fade-mask space-y-4 py-2 min-h-[380px]">
                  {lyricsLoading && (
                    <div className="flex flex-col items-center justify-center py-20 text-neutral-400">
                      <RefreshCw className="size-6 animate-spin mb-3 text-primary" />
                      <p className="text-sm">Songtext wird geladen...</p>
                    </div>
                  )}

                  {!lyricsLoading && (!lyricsData || (!parsedLrcLines && !lyricsData.content)) && (
                    <div className="flex flex-col items-center justify-center py-16 text-center text-neutral-400">
                      <Mic2 className="size-10 mb-3 text-neutral-600" />
                      <p className="text-base font-medium text-neutral-200">Keine Lyrics verfügbar</p>
                      <p className="text-xs text-neutral-500 mt-1 max-w-xs">
                        Für diesen Titel wurden noch keine Liedtexte in der Bibliothek gefunden.
                      </p>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={handleRefreshLyrics}
                        disabled={lyricsRefreshing}
                        className="mt-4 h-8 px-3 text-xs rounded-xl bg-white/[0.04] hover:bg-white/[0.08] border-white/10 text-neutral-200"
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
                            className={`cursor-pointer transition-all duration-200 select-none py-1 px-3 rounded-xl ${
                              isActive
                                ? 'text-white font-bold text-lg sm:text-xl xl:text-2xl bg-white/[0.04]'
                                : 'text-neutral-400/50 hover:text-neutral-200 font-medium text-sm sm:text-base xl:text-lg'
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
                      <div className="whitespace-pre-line text-sm sm:text-base leading-relaxed text-neutral-300">
                        {lyricsData.content}
                      </div>
                      {lyricsData.provider && (
                        <p className="pt-2 text-[11px] text-neutral-500 text-center lg:text-left">
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
                  <div className="flex items-center justify-between pb-2 border-b border-white/[0.05]">
                    <div className="flex items-center gap-1 bg-white/[0.04] p-1 rounded-xl">
                      <button
                        type="button"
                        onClick={() => setShowHistory(false)}
                        className={`text-xs font-semibold px-2.5 py-1 rounded-lg transition-colors ${
                          !showHistory ? 'bg-white/10 text-white' : 'text-neutral-400 hover:text-white'
                        }`}
                      >
                        Queue ({queue.length})
                      </button>
                      <button
                        type="button"
                        onClick={() => setShowHistory(true)}
                        className={`text-xs font-semibold px-2.5 py-1 rounded-lg transition-colors ${
                          showHistory ? 'bg-white/10 text-white' : 'text-neutral-400 hover:text-white'
                        }`}
                      >
                        Verlauf ({history.length})
                      </button>
                    </div>

                    {!showHistory && queue.length > 0 && (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={clearQueue}
                        className="h-7 px-2 text-xs text-destructive hover:bg-destructive/10 rounded-lg"
                      >
                        <Trash2 className="size-3.5 mr-1" />
                        Leeren
                      </Button>
                    )}
                  </div>

                  {!showHistory && queue.length === 0 && (
                    <div className="py-16 text-center text-neutral-400">
                      <ListMusic className="size-8 mx-auto mb-2 text-neutral-600" />
                      <p className="text-xs">Die Warteschlange ist leer</p>
                    </div>
                  )}

                  {/* Active Queue List */}
                  {!showHistory && queue.length > 0 && (
                    <div className="space-y-1">
                      {queue.map((track, idx) => {
                        const isCurrent = idx === queueIndex
                        return (
                          <div
                            key={`${track.id}-${idx}`}
                            className={`group flex items-center justify-between gap-3 p-2 rounded-xl transition-all cursor-pointer ${
                              isCurrent
                                ? 'bg-white/[0.05] border-l-2 border-primary'
                                : 'hover:bg-white/[0.02]'
                            }`}
                            onClick={() => playTrack(track, queue, idx)}
                          >
                            <div className="flex items-center gap-2.5 min-w-0 flex-1">
                              <Cover
                                src={track.cover_url}
                                alt={track.title}
                                shape="square"
                                className="size-9 rounded-lg shrink-0 border border-white/10"
                              />
                              <div className="min-w-0 flex-1">
                                <p className={`truncate text-xs sm:text-sm font-semibold ${isCurrent ? 'text-primary' : 'text-white'}`}>
                                  {track.title}
                                </p>
                                <p className="truncate text-[11px] text-neutral-400">
                                  {track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist}
                                </p>
                              </div>
                            </div>

                            <div className="flex items-center gap-2 shrink-0">
                              <span className="text-[11px] font-mono text-neutral-400 tabular-nums">
                                {formatDuration(track.duration_ms)}
                              </span>
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.stopPropagation()
                                  removeFromQueue(idx)
                                }}
                                className="opacity-0 group-hover:opacity-100 p-1 rounded-md hover:bg-white/10 text-neutral-400 hover:text-destructive transition-all"
                                title="Aus Warteschlange entfernen"
                              >
                                <X className="size-3.5" />
                              </button>
                            </div>
                          </div>
                        )
                      })}
                    </div>
                  )}

                  {/* History List */}
                  {showHistory && history.length > 0 && (
                    <div className="space-y-1">
                      {history.map((item, idx) => (
                        <div
                          key={`${item.track.id}-${idx}`}
                          onClick={() => playTrack(item.track)}
                          className="flex items-center justify-between p-2 rounded-xl hover:bg-white/[0.02] cursor-pointer transition-colors"
                        >
                          <div className="flex items-center gap-2.5 min-w-0">
                            <Cover
                              src={item.track.cover_url}
                              alt={item.track.title}
                              shape="square"
                              className="size-9 rounded-lg shrink-0 border border-white/10"
                            />
                            <div className="min-w-0">
                              <p className="truncate text-xs font-medium text-white">
                                {item.track.title}
                              </p>
                              <p className="truncate text-[11px] text-neutral-400">
                                {item.track.artists?.length > 0 ? joinArtists(item.track.artists) : item.track.album_artist}
                              </p>
                            </div>
                          </div>
                          <span className="text-[11px] font-mono text-neutral-500 tabular-nums">
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
                  <div className="flex flex-wrap items-center justify-between gap-3 pb-3 border-b border-white/[0.05]">
                    <div className="flex items-center gap-2">
                      <Button
                        size="sm"
                        variant={eqEnabled ? 'secondary' : 'outline'}
                        onClick={() => setEQEnabled(!eqEnabled)}
                        className={`h-7 rounded-lg text-xs font-medium ${
                          eqEnabled ? 'bg-primary text-white border-primary shadow-sm shadow-primary/20' : ''
                        }`}
                      >
                        {eqEnabled ? 'Aktiviert' : 'Bypass'}
                      </Button>

                      {/* Switch between Graphic & Parametric */}
                      <div className="flex items-center rounded-lg bg-white/[0.04] p-0.5 border border-white/5 text-xs">
                        <button
                          type="button"
                          onClick={() => setEQMode('graphic')}
                          className={`rounded-md px-2.5 py-0.5 text-[11px] transition-colors ${
                            eqMode === 'graphic' ? 'bg-white/10 text-white font-semibold' : 'text-neutral-400 hover:text-white'
                          }`}
                        >
                          10-Band
                        </button>
                        <button
                          type="button"
                          onClick={() => setEQMode('parametric')}
                          className={`rounded-md px-2.5 py-0.5 text-[11px] transition-colors ${
                            eqMode === 'parametric' ? 'bg-white/10 text-white font-semibold' : 'text-neutral-400 hover:text-white'
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
                          className="h-7 rounded-lg bg-white/[0.04] border border-white/10 px-2 text-xs text-neutral-200 hover:text-white outline-none cursor-pointer"
                          aria-label="Preset"
                        >
                          <optgroup label="Werkspresets">
                            {BUILTIN_PRESETS.map((p) => (
                              <option key={p.id} value={p.id} className="bg-[#121524]">
                                {p.name}
                              </option>
                            ))}
                          </optgroup>
                          {customPresets.length > 0 && (
                            <optgroup label="Eigene Presets">
                              {customPresets.map((p) => (
                                <option key={p.id} value={p.id} className="bg-[#121524]">
                                  {p.name}
                                </option>
                              ))}
                            </optgroup>
                          )}
                          {selectedPresetId === 'custom' && (
                            <option value="custom" className="bg-[#121524]">Benutzerdefiniert</option>
                          )}
                        </select>

                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setSavePresetOpen(true)}
                          className="h-7 px-2 text-xs rounded-lg bg-white/[0.04] hover:bg-white/10 border border-white/5"
                          title="Als Preset speichern"
                        >
                          <Save className="size-3.5 mr-1" />
                          Speichern
                        </Button>

                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => setEQPreset('flat')}
                          className="h-7 px-1.5 text-xs rounded-lg text-neutral-400 hover:text-white hover:bg-white/5"
                          title="Auf 0 dB zurücksetzen"
                        >
                          <RotateCcw className="size-3.5" />
                        </Button>
                      </div>
                    )}
                  </div>

                  {/* Save Preset Dialog */}
                  {savePresetOpen && (
                    <div className="flex items-center gap-2 p-2.5 bg-white/[0.05] rounded-xl border border-white/10">
                      <Input
                        type="text"
                        placeholder="Preset Name..."
                        value={customPresetName}
                        onChange={(e) => setCustomPresetName(e.target.value)}
                        className="h-7 text-xs rounded-lg bg-black/40"
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
                        className="h-7 text-xs rounded-lg bg-primary text-white"
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
                            <span className="text-[10px] font-mono text-neutral-300 tabular-nums">
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

                            <span className="text-[9px] sm:text-[10px] font-mono text-neutral-400 text-center select-none">
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
                        <p className="text-xs text-neutral-400">
                          Präzise parametrische Filter mit Frequenz, Gain und Gütefaktor (Q).
                        </p>
                        <Button
                          size="sm"
                          onClick={handleAddParametricFilter}
                          disabled={parametricFilters.length >= 10}
                          className="h-7 text-xs rounded-lg bg-primary text-white gap-1"
                        >
                          <Plus className="size-3.5" />
                          Filter
                        </Button>
                      </div>

                      <div className="space-y-2.5">
                        {parametricFilters.map((filter, index) => (
                          <div
                            key={filter.id}
                            className="p-3 rounded-xl bg-white/[0.03] border border-white/[0.06] space-y-2"
                          >
                            <div className="flex items-center justify-between gap-2">
                              <div className="flex items-center gap-2">
                                <span className="text-xs font-mono text-neutral-400">#{index + 1}</span>
                                <select
                                  value={filter.type}
                                  onChange={(e) =>
                                    setParametricFilter({
                                      ...filter,
                                      type: e.target.value as ParametricFilterType,
                                    })
                                  }
                                  className="h-6 rounded bg-white/[0.06] border border-white/10 px-1.5 text-xs text-white outline-none"
                                >
                                  <option value="peaking" className="bg-[#121524]">Peaking</option>
                                  <option value="lowshelf" className="bg-[#121524]">Low Shelf</option>
                                  <option value="highshelf" className="bg-[#121524]">High Shelf</option>
                                  <option value="lowpass" className="bg-[#121524]">Low Pass</option>
                                  <option value="highpass" className="bg-[#121524]">High Pass</option>
                                </select>
                              </div>

                              <div className="flex items-center gap-2">
                                <label className="flex items-center gap-1.5 text-xs text-neutral-400 cursor-pointer">
                                  <input
                                    type="checkbox"
                                    checked={filter.enabled}
                                    onChange={(e) =>
                                      setParametricFilter({
                                        ...filter,
                                        enabled: e.target.checked,
                                      })
                                    }
                                    className="rounded size-3.5 border-white/20 bg-white/5 text-primary"
                                  />
                                  <span>Aktiv</span>
                                </label>
                                <button
                                  type="button"
                                  onClick={() => deleteParametricFilter(filter.id)}
                                  className="p-1 rounded hover:bg-white/10 text-neutral-400 hover:text-destructive transition-colors"
                                  title="Filter löschen"
                                >
                                  <Trash2 className="size-3.5" />
                                </button>
                              </div>
                            </div>

                            {/* Sliders with Quiet Thumb Styling */}
                            <div className="grid grid-cols-1 sm:grid-cols-3 gap-2 pt-1">
                              <div className="space-y-1">
                                <div className="flex justify-between text-[10px] text-neutral-400">
                                  <span>Frequenz</span>
                                  <span className="font-mono text-white">{filter.frequency} Hz</span>
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
                                <div className="flex justify-between text-[10px] text-neutral-400">
                                  <span>Gain</span>
                                  <span className="font-mono text-white">
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
                                <div className="flex justify-between text-[10px] text-neutral-400">
                                  <span>Güte (Q)</span>
                                  <span className="font-mono text-white">{filter.q.toFixed(2)}</span>
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
                  {/* Section 1: Preamp & Headroom Schutz */}
                  <div className="p-3.5 rounded-xl bg-white/[0.03] border border-white/[0.05] space-y-3">
                    <h3 className="text-[11px] font-semibold text-neutral-300 uppercase tracking-wider">Klang & Pegelschutz</h3>
                    
                    <div className="space-y-1.5">
                      <div className="flex items-center justify-between text-xs text-neutral-300">
                        <span>Vorverstärkung (Preamp)</span>
                        <span className="font-mono font-medium text-white">
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

                    <div className="pt-2 border-t border-white/5 flex items-center justify-between">
                      <div>
                        <p className="text-xs font-medium text-neutral-200">Auto Headroom</p>
                        <p className="text-[11px] text-neutral-400">
                          Senkt den Preamp automatisch ab, um digitales Clipping bei aktiven EQ-Bands zu verhindern.
                        </p>
                      </div>
                      <input
                        type="checkbox"
                        checked={autoHeadroom}
                        onChange={(e) => setAutoHeadroom(e.target.checked)}
                        className="rounded size-4 border-white/20 bg-white/5 text-primary focus:ring-primary"
                      />
                    </div>
                  </div>

                  {/* Section 2: Safety Limiter & Channel Mode */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    {/* Limiter Safety */}
                    <div className="p-3.5 rounded-xl bg-white/[0.03] border border-white/[0.05] space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-white">Audio Safety Limiter</span>
                        <input
                          type="checkbox"
                          checked={limiterEnabled}
                          onChange={(e) => setLimiter(e.target.checked)}
                          className="rounded size-4 border-white/20 bg-white/5 text-primary"
                        />
                      </div>
                      <p className="text-[11px] text-neutral-400">
                        Schützt vor Übersteuerung mit einem Hard-Limiter bei -0.5 dBFS.
                      </p>
                    </div>

                    {/* Mono Summierung */}
                    <div className="p-3.5 rounded-xl bg-white/[0.03] border border-white/[0.05] space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-semibold text-white">Mono Summierung</span>
                        <input
                          type="checkbox"
                          checked={mono}
                          onChange={(e) => setMono(e.target.checked)}
                          className="rounded size-4 border-white/20 bg-white/5 text-primary"
                        />
                      </div>
                      <p className="text-[11px] text-neutral-400">
                        Summiert L/R mit kompensiertem -3dB Pegel für Mono-Lautsprecher.
                      </p>
                    </div>
                  </div>

                  {/* Section 3: Stereo Balance */}
                  <div className="p-3.5 rounded-xl bg-white/[0.03] border border-white/[0.05] space-y-2">
                    <div className="flex items-center justify-between text-xs text-neutral-300">
                      <span>Stereo Balance (L/R)</span>
                      <span className="font-mono text-white">
                        {balance === 0 ? 'Mitte' : balance < 0 ? `L ${Math.abs(Math.round(balance * 100))}%` : `R ${Math.round(balance * 100)}%`}
                      </span>
                    </div>
                    <div className="relative flex items-center py-1">
                      {/* Center Notch Indicator */}
                      <div className="absolute left-1/2 -translate-x-1/2 w-0.5 h-2 bg-white/30 rounded pointer-events-none" />
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

                  {/* Section 4: Crossfade Transition */}
                  <div className="p-3.5 rounded-xl bg-white/[0.03] border border-white/[0.05] space-y-2.5">
                    <div className="flex items-center justify-between text-xs text-neutral-300">
                      <span>Crossfade Überblendung</span>
                      <span className="font-mono text-white">{crossfadeSeconds}s</span>
                    </div>
                    <div className="flex items-center gap-1.5">
                      {[0, 3, 6, 12].map((s) => (
                        <button
                          key={s}
                          type="button"
                          onClick={() => setCrossfade(s)}
                          className={`flex-1 py-1 rounded-lg text-xs font-medium transition-all ${
                            crossfadeSeconds === s
                              ? 'bg-primary text-white shadow-sm shadow-primary/20'
                              : 'bg-white/[0.04] text-neutral-400 hover:text-white hover:bg-white/[0.08]'
                          }`}
                        >
                          {s === 0 ? 'Aus' : `${s}s`}
                        </button>
                      ))}
                    </div>

                    <div className="pt-2 border-t border-white/5 flex items-center justify-between">
                      <div>
                        <p className="text-xs font-medium text-neutral-200">Smart Album Bypass</p>
                        <p className="text-[11px] text-neutral-400">
                          Deaktiviert Crossfade automatisch bei aufeinanderfolgenden Tracks desselben Albums.
                        </p>
                      </div>
                      <input
                        type="checkbox"
                        checked={smartAlbumTransition}
                        onChange={(e) => setSmartAlbumTransition(e.target.checked)}
                        className="rounded size-4 border-white/20 bg-white/5 text-primary"
                      />
                    </div>
                  </div>
                </div>
              )}

            </div>
          </div>

        </div>
      </main>
    </div>
  )
}
