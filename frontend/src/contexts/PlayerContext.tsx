import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useReducer,
  useRef,
  useState,
} from 'react'
import type { ReactNode } from 'react'

import { useOptionalAuth } from '@/hooks/useAuth'
import { ListeningTracker, movesOnToAnotherTrack } from '@/lib/audio/listeningTracker'
import { analyzeLoudness, localRadio, recordPlayback, recordSkip } from '@/lib/api/libraryTools'
import { AudioEngine } from '@/lib/audio/engine'
import { offlineAudioURL, releaseOfflineAudioURLs, trimOfflineAudioURLs } from '@/lib/offline-store'
import { libraryArtwork } from '@/lib/artwork'
import { playerShortcutFor } from '@/lib/player-shortcuts'
import { openShortcutHelp } from '@/lib/shortcut-help'
import {
  loadCustomPresets,
  loadHistory,
  loadPlayerState,
  saveCustomPresets,
  saveHistory,
  savePlayerState,
} from '@/lib/audio/player-storage'
import type { PersistedPlayerState } from '@/lib/audio/player-storage'
import type {
  EQMode,
  ParametricFilter,
  PlayerState,
  QueueContext,
  RepeatMode,
  SleepTimerOption,
  StopAfterOption,
  VisualizerMode,
} from '@/lib/audio/types'
import { notifyError, notifySuccess, notifyWithAction } from '@/lib/notify'
import { joinArtists } from '@/lib/utils/format'
import type { LibraryTrack } from '@/types/api'
import {
  autoplaySeed,
  INITIAL_PLAYER_STATE,
  playArtistAction,
  playerReducer,
  radioContext,
  wantsAutoplay,
} from './player-reducer'
import type { PlayerAction, QueueSnapshot, RadioSource } from './player-reducer'
import { createProgressStore, PlayerProgressStoreContext } from './player-progress-store'

export interface PlayerProgressContextValue {
  currentTime: number
  duration: number
}

export type PlayerStateContextValue = Omit<PlayerState, 'currentTime' | 'duration'>

export interface PlayerActionsContextValue {
  engine: AudioEngine
  playTrack: (
    track: LibraryTrack,
    queue?: LibraryTrack[],
    queueIndex?: number,
    context?: QueueContext | null,
  ) => void
  resumeSession: (
    tracks: LibraryTrack[],
    index: number,
    position: number,
    repeatMode: RepeatMode,
  ) => void
  playAlbum: (tracks: LibraryTrack[], startIndex?: number, context?: QueueContext | null) => void
  playArtist: (tracks: LibraryTrack[], shuffle?: boolean, context?: QueueContext | null) => void
  /** Plays the list with the player's shuffle on, so turning it off restores the order. */
  playShuffled: (tracks: LibraryTrack[], context?: QueueContext | null) => void
  /** Replaces the queue with radio tracks already fetched (seed first when given). */
  playRadio: (tracks: LibraryTrack[], source: RadioSource) => void
  /** Fetches a radio from the library and plays it; reports the result as a toast. */
  startRadio: (source: RadioSource) => Promise<boolean>
  setAutoplay: (enabled: boolean) => void
  /** Puts the tracks right after the current one, in their order. */
  playNext: (tracks: LibraryTrack | LibraryTrack[]) => void
  playQueueIndex: (index: number) => void
  addToQueue: (tracks: LibraryTrack[] | LibraryTrack) => void
  removeFromQueue: (index: number) => void
  reorderQueue: (fromIndex: number, toIndex: number) => void
  clearQueue: () => void
  clearUpcomingQueue: () => void
  togglePlayPause: () => void
  play: () => void
  pause: () => void
  seek: (seconds: number) => void
  previous: () => void
  next: (manual?: boolean) => void
  setVolume: (volume: number) => void
  setMuted: (muted: boolean) => void
  toggleMute: () => void
  setShuffle: (shuffle: boolean) => void
  toggleShuffle: () => void
  setRepeatMode: (mode: RepeatMode) => void
  cycleRepeatMode: () => void
  setPlaybackRate: (rate: number) => void
  setCrossfade: (seconds: number) => void
  setSmartAlbumTransition: (enabled: boolean) => void
  setSleepTimer: (option: SleepTimerOption) => void
  setStopAfter: (option: StopAfterOption) => void
  setEQEnabled: (enabled: boolean) => void
  toggleEQ: () => void
  setEQMode: (mode: EQMode) => void
  setEQPreset: (presetId: string) => void
  setEQBand: (index: number, gain: number) => void
  saveCustomPreset: (name: string) => void
  deleteCustomPreset: (id: string) => void
  setParametricFilter: (filter: ParametricFilter) => void
  addParametricFilter: (filter: ParametricFilter) => void
  deleteParametricFilter: (id: string) => void
  setPreamp: (db: number) => void
  setAutoHeadroom: (enabled: boolean) => void
  setNormalization: (enabled: boolean) => void
  setLimiter: (enabled: boolean) => void
  setBalance: (balance: number) => void
  setMono: (mono: boolean) => void
  setBassBoost: (boost: number) => void
  setVisualizerMode: (mode: VisualizerMode) => void
}

export interface PlayerContextValue extends PlayerState, PlayerActionsContextValue {}

export const PlayerStateContext = createContext<PlayerStateContextValue | null>(null)
export const PlayerProgressContext = createContext<PlayerProgressContextValue | null>(null)
export const PlayerActionsContext = createContext<PlayerActionsContextValue | null>(null)
export const PlayerContext = createContext<PlayerContextValue | null>(null)

function audioKey(track:LibraryTrack){return track.offline_blob_key?'offline:'+track.offline_blob_key:track.id}
function serverStream(track:LibraryTrack){return `/api/v1/library/tracks/${encodeURIComponent(track.id)}/stream`}
/** A random event id; a retried request reuses it, so the server counts once. */
function newEventID(){return Array.from(crypto.getRandomValues(new Uint8Array(16)),(v)=>v.toString(16).padStart(2,'0')).join('')}

export function PlayerProvider({ children }: { children: ReactNode }) {
  const auth = useOptionalAuth()
  const user = auth?.user ?? null
  const [state, dispatch] = useReducer(playerReducer, INITIAL_PLAYER_STATE)
  const [loadRequest, requestLoad] = useReducer((value: number) => value + 1, 0)
  const engine = useMemo(() => AudioEngine.getInstance(), [])

  const listeningTracker = useRef(new ListeningTracker())
  const stateRef = useRef(state)
  stateRef.current = state

  const userRef = useRef(user)
  userRef.current = user

  // Track ID of the currently playing audio on the engine
  const currentEngineTrackIdRef = useRef<string | null>(null)
  const pendingSourceKeyRef=useRef<string|null>(null)
  const streamURLsRef=useRef(new Map<string,string>())
  useEffect(()=>()=>{releaseOfflineAudioURLs()},[])


  // A skip: the user moves on to another track after at least
  // SKIP_MIN_SECONDS but before the current one counted as a play. Called
  // before the action is dispatched, with the action it is about to run.
  const reportSkipBefore = useCallback((action: PlayerAction) => {
    const before = stateRef.current,
      u = userRef.current
    if (!u || !before.currentTrack) return
    if (!movesOnToAnotherTrack(before, playerReducer(before, action))) return
    if (!listeningTracker.current.takeSkip(u.id + ':' + before.currentTrack.id, performance.now(), before.status === 'playing')) return
    const id = newEventID(),
      trackID = before.currentTrack.id
    void recordSkip(trackID, id)
      .catch(() => recordSkip(trackID, id))
      .catch(() => {})
  }, [])

  const previousAction = useCallback(() => {
    reportSkipBefore({ type: 'PREVIOUS' })
    if (stateRef.current.currentTime > 3 && stateRef.current.currentTrack && currentEngineTrackIdRef.current===audioKey(stateRef.current.currentTrack)) {
      dispatch({ type: 'PREVIOUS' })
      engine.seek(0)
      engine.play()
    } else {
      currentEngineTrackIdRef.current = null
      requestLoad()
      dispatch({ type: 'PREVIOUS' })
    }
  }, [engine, reportSkipBefore])

  const nextAction = useCallback(
    (manual = true) => {
      if (manual) reportSkipBefore({ type: 'NEXT', payload: { manual } })
      const after = playerReducer(stateRef.current, { type: 'NEXT', payload: { manual } })
      if (after.status === 'paused') engine.pause()
      else {
        currentEngineTrackIdRef.current = null
        requestLoad()
      }
      dispatch({ type: 'NEXT', payload: { manual } })
    },
    [engine, reportSkipBefore],
  )

  // 1. Initial State Restoration from LocalStorage
  const sessionUserIdRef = useRef<string | null | undefined>(undefined)
  useEffect(() => {
    // The provider outlives a sign-out and a change of user. The queue and
    // the audio of the previous session end here, before anything of the new
    // user is restored; otherwise the next user would inherit them and have
    // them saved and counted as their own.
    const sessionUserId = user?.id ?? null
    if (sessionUserIdRef.current !== undefined && sessionUserIdRef.current !== sessionUserId) {
      engine.cancelCrossfade()
      engine.pause()
      engine.preloadNext(null)
      currentEngineTrackIdRef.current = null
      pendingSourceKeyRef.current = null
      dispatch({ type: 'RESET_SESSION' })
    }
    sessionUserIdRef.current = sessionUserId

    const customPresets = loadCustomPresets()
    const history = loadHistory(user?.id)
    listeningTracker.current.reset()
    const saved = loadPlayerState(user?.id)

    if (saved || customPresets.length > 0 || history.length > 0) {
      dispatch({
        type: 'RESTORE_STATE',
        payload: {
          ...(saved || {}),
          customPresets,
          history,
        },
      })
    }
  }, [user?.id, engine])

  // 2. Connect Engine Callbacks
  useEffect(() => {
    engine.setCallbacks({
      onTimeUpdate: (currentTime, duration) => {
        if(pendingSourceKeyRef.current)return
        const s = stateRef.current,
          u = userRef.current
        if (
          u &&
          s.currentTrack &&
          !s.currentTrack.offline_blob_key &&
          listeningTracker.current.tick(
            u.id + ':' + s.currentTrack.id,
            s.status === 'playing',
            performance.now(),
            duration,
            s.playbackRate,
          )
        ) {
          const id = newEventID(),
            trackID = s.currentTrack.id
          void recordPlayback(trackID, id)
            .catch(() => recordPlayback(trackID, id))
            .catch(() => {})
        }

        if(s.currentTrack?.offline_blob_key)listeningTracker.current.reset()
        dispatch({
          type: 'SET_CURRENT_TIME',
          payload: { currentTime, duration },
        })
      },
      onStatusChange: (status) => {
        if(pendingSourceKeyRef.current)return
        dispatch({ type: 'SET_STATUS', payload: status })
      },
      onError: (error) => {
        if(pendingSourceKeyRef.current)return
        dispatch({ type: 'SET_ERROR', payload: error })
      },
      onTrackEnded: () => {
        if(pendingSourceKeyRef.current)return
        listeningTracker.current.reset()
        const before = stateRef.current
        const after = playerReducer(before, { type: 'NEXT', payload: { manual: false } })
        if (after.status === 'paused') engine.pause()
        else if (after.currentTrack?.id === before.currentTrack?.id && after.currentTrack?.offline_blob_key===before.currentTrack?.offline_blob_key) {
          engine.seek(0)
          engine.play()
        }
        dispatch({ type: 'NEXT', payload: { manual: false } })
      },
      onNextDeckCrossfadeStart: (streamUrl) => {
        listeningTracker.current.reset()
        const after = playerReducer(stateRef.current, { type: 'NEXT', payload: { manual: false } })
        const expected=after.currentTrack?(after.currentTrack.offline_blob_key?streamURLsRef.current.get(audioKey(after.currentTrack)):serverStream(after.currentTrack)):null
        if (after.status !== 'buffering' || expected !== streamUrl) {
          // A boundary or queue edit may arrive while the incoming play promise settles.
          engine.pause()
          currentEngineTrackIdRef.current = null
          requestLoad()
          dispatch({ type: 'NEXT', payload: { manual: false } })
          return
        }
        currentEngineTrackIdRef.current = audioKey(after.currentTrack!)
        dispatch({ type: 'NEXT', payload: { manual: false } })
        dispatch({ type: 'SET_STATUS', payload: 'playing' })
      },
    })
  }, [engine])

  // 3. Sync Audio Engine DSP settings with State
  useEffect(() => {
    engine.setVolume(state.volume)
    engine.setMuted(state.muted)
    engine.setPlaybackRate(state.playbackRate)
    engine.setEQEnabled(state.eqEnabled)
    engine.setEQMode(state.eqMode)
    engine.setGraphicBands(state.graphicBands)
    engine.setParametricFilters(state.parametricFilters)
    engine.setPreamp(state.preamp)
    engine.setAutoHeadroom(state.autoHeadroom)
    engine.setLimiter(state.limiterEnabled)
    engine.setNormalization(state.normalizationEnabled)
    engine.setBalance(state.balance)
    engine.setMono(state.mono)
  }, [
    engine,
    state.volume,
    state.muted,
    state.playbackRate,
    state.eqEnabled,
    state.eqMode,
    state.graphicBands,
    state.parametricFilters,
    state.preamp,
    state.autoHeadroom,
    state.limiterEnabled,
    state.normalizationEnabled,
    state.balance,
    state.mono,
  ])

  // 4. Load the selected local or server source. Online paths stay synchronous;
  // stale IndexedDB resolutions cannot take over a newer queue or pause intent.
  useEffect(() => {
    const track=state.currentTrack
    let canceled=false
    if(!track){pendingSourceKeyRef.current=null;currentEngineTrackIdRef.current=null;engine.preloadNext(null);engine.pause();return}
    const key=audioKey(track)
    const trim=()=>void trimOfflineAudioURLs([engine.getActiveDeckElement().src,engine.getInactiveDeckElement().src])
    const resolve=(candidate:LibraryTrack)=>candidate.offline_blob_key?offlineAudioURL(candidate.offline_blob_key):Promise.resolve(serverStream(candidate))
    const load=(url:string)=>{
      if(canceled)return
      const latest=stateRef.current
      if(!latest.currentTrack||audioKey(latest.currentTrack)!==key)return
      pendingSourceKeyRef.current=null
      streamURLsRef.current.set(key,url)
      if(currentEngineTrackIdRef.current!==key){
        currentEngineTrackIdRef.current=key
        if(latest.status==='playing'||latest.status==='buffering')void engine.loadAndPlay(url,latest.currentTime)
        else engine.load(url,latest.currentTime)
      }
      const after=playerReducer(latest,{type:'NEXT',payload:{manual:false}})
      const nextTrack=after.status==='buffering'&&latest.repeatMode!=='track'?after.currentTrack:null
      if(!nextTrack){engine.preloadNext(null);trim();return}
      const isConsecutive=Boolean(track.release_id)&&track.release_id===nextTrack.release_id&&((nextTrack.disc_number===track.disc_number&&nextTrack.track_number===track.track_number+1)||(nextTrack.disc_number===track.disc_number+1&&nextTrack.track_number===1))
      engine.setCrossfade(latest.smartAlbumTransition&&isConsecutive?0:latest.crossfadeSeconds)
      const preload=(nextURL:string)=>{
        if(canceled)return
        const fresh=playerReducer(stateRef.current,{type:'NEXT',payload:{manual:false}})
        if(!fresh.currentTrack||audioKey(fresh.currentTrack)!==audioKey(nextTrack)||fresh.status!=='buffering')return
        streamURLsRef.current.set(audioKey(nextTrack),nextURL)
        engine.preloadNext(nextURL);trim()
      }
      if(nextTrack.offline_blob_key)void resolve(nextTrack).then(preload,()=>{if(!canceled)engine.preloadNext(null)})
      else preload(serverStream(nextTrack))
    }
    if(track.offline_blob_key){
      if(currentEngineTrackIdRef.current!==key){pendingSourceKeyRef.current=key;engine.pause();engine.preloadNext(null)}
      void resolve(track).then(load,error=>{if(!canceled){pendingSourceKeyRef.current=null;dispatch({type:'SET_ERROR',payload:error instanceof Error?error.message:'Offline-Kopie konnte nicht geladen werden.'});dispatch({type:'SET_STATUS',payload:'error'})}})
    }else{pendingSourceKeyRef.current=null;load(serverStream(track))}
    return()=>{canceled=true}
  },[engine,loadRequest,state.currentTrack,state.queueIndex,state.repeatMode,state.crossfadeSeconds,state.smartAlbumTransition,state.queue,state.stopAfter,state.sleepTimer])

  // Analyze only current and next tracks after explicit opt-in, sequentially.
  useEffect(() => {
    if (!state.normalizationEnabled || !user?.id || !state.currentTrack) return
    if(state.currentTrack.offline_blob_key){dispatch({type:'SET_NORMALIZATION_MESSAGE',payload:'Offline: keine Lautstärkeanalyse auf dem Server.'});return}
    const controller = new AbortController()
    const current = state.currentTrack,
      next = state.queue[state.queueIndex + 1]
    dispatch({ type: 'SET_NORMALIZATION_MESSAGE', payload: 'Lautstärke wird gemessen …' })
    void (async () => {
      for (const track of [current, next]) {
        if (!track || track.offline_blob_key || controller.signal.aborted) continue
        try {
          const measurement = await analyzeLoudness(track.id, controller.signal)
          if (controller.signal.aborted) return
          engine.setLoudnessGain(
            `/api/v1/library/tracks/${encodeURIComponent(track.id)}/stream`,
            measurement.gain_db,
          )
          if (track.id === current.id)
            dispatch({
              type: 'SET_NORMALIZATION_MESSAGE',
              payload: `Aktueller Titel: ${measurement.gain_db.toFixed(1)} dB`,
            })
        } catch (error) {
          if (!controller.signal.aborted && track.id === current.id)
            dispatch({
              type: 'SET_NORMALIZATION_MESSAGE',
              payload: error instanceof Error ? error.message : 'Analyse fehlgeschlagen.',
            })
        }
      }
    })()
    return () => controller.abort()
  }, [
    engine,
    state.normalizationEnabled,
    state.currentTrack?.id,
    state.queueIndex,
    state.queue,
    user?.id,
  ])

  // 5a. Media Session metadata: title, artist and the cover for the system's
  // now-playing display and lock screen. The cover comes from the server's
  // own artwork endpoint, so the OS does not fetch from the provider's CDN.
  // That endpoint answers with one image of whatever size and type the
  // library holds (JPEG, PNG or WebP), so the entry names neither; the
  // provider URL is used only when the server has no cover for the track.
  useEffect(() => {
    if (typeof window === 'undefined' || !('mediaSession' in navigator)) return

    const track = state.currentTrack
    if (!track) {
      navigator.mediaSession.metadata = null
      return
    }

    const artist = track.artists?.length > 0 ? joinArtists(track.artists) : track.album_artist
    const setArtwork = (artwork: MediaImage[]) => {
      navigator.mediaSession.metadata = new MediaMetadata({
        title: track.title,
        artist,
        album: track.album || '',
        artwork,
      })
    }

    const local = libraryArtwork('tracks', track.id)
    setArtwork([{ src: new URL(local, window.location.href).href }])

    const controller = new AbortController()
    fetch(local, { method: 'HEAD', credentials: 'same-origin', signal: controller.signal })
      .then((response) => {
        if (!response.ok && track.cover_url) setArtwork([{ src: track.cover_url }])
      })
      .catch(() => {
        if (!controller.signal.aborted && track.cover_url) setArtwork([{ src: track.cover_url }])
      })
    return () => controller.abort()
  }, [state.currentTrack])

  // 5b. Media Session playback state and actions
  useEffect(() => {
    if (typeof window === 'undefined' || !('mediaSession' in navigator)) return

    navigator.mediaSession.playbackState = state.currentTrack
      ? state.status === 'playing'
        ? 'playing'
        : 'paused'
      : 'none'

    const actionHandlers: [MediaSessionAction, MediaSessionActionHandler][] = [
      [
        'play',
        () => {
          // Only resume audio that belongs to the current track. While a new
          // source is still loading, the engine holds the previous track.
          const track = stateRef.current.currentTrack
          if (track && currentEngineTrackIdRef.current === audioKey(track)) engine.play()
          dispatch({ type: 'SET_STATUS', payload: 'playing' })
        },
      ],
      [
        'pause',
        () => {
          engine.pause()
          dispatch({ type: 'SET_STATUS', payload: 'paused' })
        },
      ],
      ['previoustrack', previousAction],
      ['nexttrack', () => nextAction(true)],
      [
        'seekto',
        (details) => {
          if (details.seekTime !== undefined) {
            engine.seek(details.seekTime)
            dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: details.seekTime } })
          }
        },
      ],
      [
        'seekbackward',
        (details) => {
          const skip = details.seekOffset || 5
          const newTime = Math.max(0, stateRef.current.currentTime - skip)
          engine.seek(newTime)
          dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
        },
      ],
      [
        'seekforward',
        (details) => {
          const skip = details.seekOffset || 5
          const newTime = Math.min(stateRef.current.duration, stateRef.current.currentTime + skip)
          engine.seek(newTime)
          dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
        },
      ],
      [
        'stop',
        () => {
          engine.pause()
          dispatch({ type: 'SET_STATUS', payload: 'paused' })
        },
      ],
    ]

    for (const [action, handler] of actionHandlers) {
      try {
        navigator.mediaSession.setActionHandler(action, handler)
      } catch {
        // Some actions might not be supported on all browsers
      }
    }

    return () => {
      for (const [action] of actionHandlers) {
        try {
          navigator.mediaSession.setActionHandler(action, null)
        } catch {
          // Ignore
        }
      }
    }
  }, [engine, nextAction, previousAction, state.currentTrack, state.status])

  // Update MediaSession Position State
  useEffect(() => {
    if (
      typeof window !== 'undefined' &&
      'mediaSession' in navigator &&
      'setPositionState' in navigator.mediaSession &&
      state.duration > 0
    ) {
      try {
        navigator.mediaSession.setPositionState({
          duration: state.duration,
          playbackRate: state.playbackRate,
          position: Math.max(0, Math.min(state.currentTime, state.duration)),
        })
      } catch {
        // Ignore invalid range errors during transitions
      }
    }
  }, [state.currentTime, state.duration, state.playbackRate])

  // 6. Global Keyboard Shortcuts
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent) {
      // Which key is ours, and when it belongs to the page instead, is
      // decided in one tested place; only a handled key loses its default.
      const shortcut = playerShortcutFor(e)
      if (!shortcut) return
      e.preventDefault()

      switch (shortcut) {
        case 'togglePlay':
          // Buffering counts as playing: the key then stops the pending start.
          if (stateRef.current.status === 'playing' || stateRef.current.status === 'buffering') {
            engine.pause()
            dispatch({ type: 'SET_STATUS', payload: 'paused' })
          } else {
            const track = stateRef.current.currentTrack
            if (track && currentEngineTrackIdRef.current === audioKey(track)) engine.play()
            dispatch({ type: 'SET_STATUS', payload: 'playing' })
          }
          break

        case 'previous':
          previousAction()
          break

        case 'next':
          nextAction(true)
          break

        case 'seekBackward': {
          const newTime = Math.max(0, stateRef.current.currentTime - 5)
          engine.seek(newTime)
          dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
          break
        }

        case 'seekForward': {
          const newTime = Math.min(stateRef.current.duration, stateRef.current.currentTime + 5)
          engine.seek(newTime)
          dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: newTime } })
          break
        }

        case 'volumeUp':
          dispatch({ type: 'SET_VOLUME', payload: stateRef.current.volume + 0.05 })
          break

        case 'volumeDown':
          dispatch({ type: 'SET_VOLUME', payload: stateRef.current.volume - 0.05 })
          break

        case 'toggleMute':
          dispatch({ type: 'SET_MUTED', payload: !stateRef.current.muted })
          break

        case 'toggleShuffle':
          dispatch({ type: 'SET_SHUFFLE', payload: !stateRef.current.shuffle })
          break

        case 'cycleRepeat': {
          const modes: RepeatMode[] = ['off', 'queue', 'track']
          const nextIdx = (modes.indexOf(stateRef.current.repeatMode) + 1) % modes.length
          const nextMode = modes[nextIdx] ?? 'off'
          dispatch({ type: 'SET_REPEAT_MODE', payload: nextMode })
          break
        }

        case 'showHelp':
          openShortcutHelp()
          break
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [engine, nextAction, previousAction])

  // 6b. Autoplay: when the queue is about to run out, similar tracks from the
  // library are appended (origin autoplay), so playback simply carries on.
  // One request per queue end; a failure or an empty answer leaves the
  // queue to end as before. The nonce makes every batch a fresh draw.
  const autoplayRef = useRef<{ key: string; controller: AbortController | null }>({ key: '', controller: null })
  const autoplayUserId = user?.id
  useEffect(() => {
    const pending = autoplayRef.current
    return () => pending.controller?.abort()
  }, [autoplayUserId])
  useEffect(() => {
    const s = stateRef.current
    const online = typeof navigator === 'undefined' || navigator.onLine !== false
    if (!autoplayUserId || !wantsAutoplay(s, online)) return
    const seed = autoplaySeed(s.queue)
    const last = s.queue[s.queue.length - 1]
    if (!seed || !last || seed.offline_blob_key) return
    const key = `${autoplayUserId}:${s.queue[0]?.id}:${s.queue.length}:${last.id}:${seed.id}`
    if (autoplayRef.current.key === key) return
    autoplayRef.current.key = key
    autoplayRef.current.controller?.abort()
    const controller = new AbortController()
    autoplayRef.current.controller = controller
    const length = s.queue.length
    void localRadio(seed.id, undefined, newEventID(), controller.signal).then(
      (tracks) => {
        if (controller.signal.aborted) return
        const now = stateRef.current
        // Something else was started, or the end moved, in the meantime.
        if (now.queue.length !== length || now.queue[length - 1]?.id !== last.id || !now.autoplay) return
        dispatch({ type: 'APPEND_AUTOPLAY', payload: { tracks: Array.isArray(tracks) ? tracks : [] } })
      },
      () => {},
    )
  }, [
    autoplayUserId,
    state.autoplay,
    state.repeatMode,
    state.stopAfter,
    state.sleepTimer,
    state.status,
    state.currentTrack,
    state.queue,
    state.queueIndex,
  ])

  // 7. Throttled LocalStorage Persistence
  useEffect(() => {
    const persisted = (s: PlayerState): Partial<PersistedPlayerState> => ({
      queue: s.queue,
      originalQueue: s.originalQueue,
      queueIndex: s.queueIndex,
      queueContext: s.queueContext,
      autoplay: s.autoplay,
      currentTime: s.currentTime,
      volume: s.volume,
      muted: s.muted,
      shuffle: s.shuffle,
      repeatMode: s.repeatMode,
      playbackRate: s.playbackRate,
      crossfadeSeconds: s.crossfadeSeconds,
      smartAlbumTransition: s.smartAlbumTransition,
      eqEnabled: s.eqEnabled,
      eqMode: s.eqMode,
      selectedPresetId: s.selectedPresetId,
      graphicBands: s.graphicBands,
      parametricFilters: s.parametricFilters,
      preamp: s.preamp,
      autoHeadroom: s.autoHeadroom,
      limiterEnabled: s.limiterEnabled,
      normalizationEnabled: s.normalizationEnabled,
      balance: s.balance,
      mono: s.mono,
      bassBoost: s.bassBoost,
      visualizerMode: s.visualizerMode,
    })
    const interval = window.setInterval(() => {
      const s = stateRef.current
      savePlayerState(userRef.current?.id, persisted(s))
      saveHistory(userRef.current?.id, s.history)
      saveCustomPresets(s.customPresets)
    }, 5000)

    const handleBeforeUnload = () => {
      savePlayerState(userRef.current?.id, persisted(stateRef.current))
    }

    window.addEventListener('beforeunload', handleBeforeUnload)
    return () => {
      clearInterval(interval)
      window.removeEventListener('beforeunload', handleBeforeUnload)
    }
  }, [])

  // 8. Sleep Timer Countdown Check
  useEffect(() => {
    if (!state.sleepTimerEndsAt) return

    const checkTimer = window.setInterval(() => {
      if (state.sleepTimerEndsAt && Date.now() >= state.sleepTimerEndsAt) {
        engine.pause()
        dispatch({ type: 'SET_STATUS', payload: 'paused' })
        dispatch({ type: 'SET_SLEEP_TIMER', payload: 'off' })
      }
    }, 1000)

    return () => clearInterval(checkTimer)
  }, [engine, state.sleepTimerEndsAt])

  // Context Actions
  const playTrack = useCallback(
    (track: LibraryTrack, queue?: LibraryTrack[], queueIndex?: number, context?: QueueContext | null) => {
      const action: PlayerAction = { type: 'PLAY_TRACK', payload: { track, queue, queueIndex, context } }
      reportSkipBefore(action)
      currentEngineTrackIdRef.current = null
      requestLoad()
      dispatch(action)
    },
    [reportSkipBefore],
  )

  const resumeSession = useCallback(
    (
      tracks: LibraryTrack[],
      index: number,
      position: number,
      repeatMode: RepeatMode,
    ) => {
      if (
        !tracks.length ||
        !Number.isInteger(index) ||
        index < 0 ||
        index >= tracks.length ||
        !Number.isFinite(position)
      )
        return
      engine.pause()
      currentEngineTrackIdRef.current = null
      requestLoad()
      dispatch({
        type: 'RESUME_SESSION',
        payload: { tracks, index, position, repeatMode },
      })
    },
    [engine],
  )

  const playAlbum = useCallback((tracks: LibraryTrack[], startIndex = 0, context?: QueueContext | null) => {
    const action: PlayerAction = { type: 'PLAY_ALBUM', payload: { tracks, startIndex, context } }
    if (tracks.length > 0) {
      reportSkipBefore(action)
      currentEngineTrackIdRef.current = null
      requestLoad()
    }
    dispatch(action)
  }, [reportSkipBefore])

  const playArtist = useCallback((tracks: LibraryTrack[], shuffle = false, context?: QueueContext | null) => {
    // The shuffle is drawn once, so the skip check sees the track that plays.
    const action = playArtistAction(tracks, shuffle, context)
    if (tracks.length > 0) {
      reportSkipBefore(action)
      currentEngineTrackIdRef.current = null
      requestLoad()
    }
    dispatch(action)
  }, [reportSkipBefore])

  const playShuffled = useCallback(
    (tracks: LibraryTrack[], context?: QueueContext | null) => playArtist(tracks, true, context),
    [playArtist],
  )

  const playRadio = useCallback(
    (tracks: LibraryTrack[], source: RadioSource) => {
      const action: PlayerAction = {
        type: 'START_RADIO',
        payload: { tracks, seed: source.seed, context: radioContext(source) },
      }
      const before = stateRef.current
      const after = playerReducer(before, action)
      if (after === before) return
      // A radio from the track that plays carries on with it: no skip, no reload.
      reportSkipBefore(action)
      if (after.currentTrack?.id !== before.currentTrack?.id || after.status !== before.status) {
        currentEngineTrackIdRef.current = null
        requestLoad()
      }
      dispatch(action)
    },
    [reportSkipBefore],
  )

  const startRadio = useCallback(
    async (source: RadioSource) => {
      const context = radioContext(source)
      let tracks: LibraryTrack[]
      try {
        tracks = await localRadio(source.seed?.id, source.genre, newEventID())
      } catch (error) {
        notifyError('Radio konnte nicht gestartet werden', error)
        return false
      }
      if (!tracks.filter((t) => t.id !== source.seed?.id).length) {
        notifyError(
          'Kein Radio möglich',
          'In deiner Bibliothek gibt es noch keine weiteren Titel, die dazu passen.',
        )
        return false
      }
      playRadio(tracks, source)
      notifySuccess(context.title, `${tracks.length} ähnliche Titel aus deiner Bibliothek`)
      return true
    },
    [playRadio],
  )

  const setAutoplayAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_AUTOPLAY', payload: enabled })
  }, [])

  const playNextAction = useCallback((tracks: LibraryTrack | LibraryTrack[]) => {
    const list = Array.isArray(tracks) ? tracks : [tracks]
    const first = list[0]
    if (!first) return
    if (list.length === 1) {
      dispatch({ type: 'PLAY_NEXT', payload: { track: first } })
      notifySuccess('Wird als Nächstes gespielt', first.title)
      return
    }
    const s = stateRef.current
    if (!s.currentTrack || s.queue.length === 0) {
      // Nothing plays yet: the list starts, as a single "Als Nächstes" does.
      dispatch({ type: 'ADD_TO_QUEUE', payload: { tracks: list } })
    } else {
      // Each one lands right after the current track, so the last goes first.
      for (const track of [...list].reverse()) dispatch({ type: 'PLAY_NEXT', payload: { track } })
    }
    notifySuccess(`${list.length} Titel werden als Nächstes gespielt`)
  }, [])

  const playQueueIndexAction = useCallback((index: number) => {
    if (index >= 0 && index < stateRef.current.queue.length) {
      reportSkipBefore({ type: 'PLAY_QUEUE_INDEX', payload: { index } })
      currentEngineTrackIdRef.current = null
      requestLoad()
    }
    dispatch({ type: 'PLAY_QUEUE_INDEX', payload: { index } })
  }, [reportSkipBefore])

  const addToQueueAction = useCallback((tracks: LibraryTrack[] | LibraryTrack) => {
    dispatch({ type: 'ADD_TO_QUEUE', payload: { tracks } })
    const list = Array.isArray(tracks) ? tracks : [tracks]
    if (list.length === 1 && list[0]) notifySuccess('Zur Warteschlange hinzugefügt', list[0].title)
    else if (list.length > 1) notifySuccess(`${list.length} Titel zur Warteschlange hinzugefügt`)
  }, [])

  const removeFromQueueAction = useCallback((index: number) => {
    dispatch({ type: 'REMOVE_FROM_QUEUE', payload: { index } })
  }, [])

  const reorderQueueAction = useCallback((fromIndex: number, toIndex: number) => {
    dispatch({ type: 'REORDER_QUEUE', payload: { fromIndex, toIndex } })
  }, [])

  const clearQueueAction = useCallback(() => {
    const s = stateRef.current
    const snapshot: QueueSnapshot = {
      queue: s.queue,
      originalQueue: s.originalQueue,
      queueIndex: s.queueIndex,
      currentTrack: s.currentTrack,
      queueContext: s.queueContext,
      shuffle: s.shuffle,
      currentTime: s.currentTime,
      status: s.status,
    }
    dispatch({ type: 'CLEAR_QUEUE' })
    engine.pause()
    if (snapshot.queue.length === 0) return
    notifyWithAction(
      'Warteschlange geleert',
      {
        label: 'Rückgängig',
        onClick: () => {
          // Only while nothing else was started since.
          if (stateRef.current.queue.length > 0) return
          currentEngineTrackIdRef.current = null
          requestLoad()
          dispatch({ type: 'RESTORE_QUEUE', payload: snapshot })
        },
      },
      { type: 'info', description: `${snapshot.queue.length} Titel entfernt` },
    )
  }, [engine])

  const clearUpcomingQueueAction = useCallback(() => {
    dispatch({ type: 'CLEAR_UPCOMING_QUEUE' })
  }, [])

  const togglePlayPause = useCallback(() => {
    // While a track is still buffering, the button acts as pause: it cancels
    // the pending start instead of asking for playback a second time.
    if (stateRef.current.status === 'playing' || stateRef.current.status === 'buffering') {
      engine.pause()
      dispatch({ type: 'SET_STATUS', payload: 'paused' })
    } else {
      const track=stateRef.current.currentTrack
      if(track&&currentEngineTrackIdRef.current===audioKey(track))engine.play()
      dispatch({ type: 'SET_STATUS', payload: 'playing' })
    }
  }, [engine])

  const playAction = useCallback(() => {
    const track=stateRef.current.currentTrack
    if(track&&currentEngineTrackIdRef.current===audioKey(track))engine.play()
    dispatch({ type: 'SET_STATUS', payload: 'playing' })
  }, [engine])

  const pauseAction = useCallback(() => {
    engine.pause()
    dispatch({ type: 'SET_STATUS', payload: 'paused' })
  }, [engine])

  const seekAction = useCallback(
    (seconds: number) => {
      if(!pendingSourceKeyRef.current)engine.seek(seconds)
      dispatch({ type: 'SET_CURRENT_TIME', payload: { currentTime: seconds } })
    },
    [engine],
  )

  const setVolumeAction = useCallback((vol: number) => {
    dispatch({ type: 'SET_VOLUME', payload: vol })
  }, [])

  const setMutedAction = useCallback((muted: boolean) => {
    dispatch({ type: 'SET_MUTED', payload: muted })
  }, [])

  const toggleMuteAction = useCallback(() => {
    dispatch({ type: 'SET_MUTED', payload: !stateRef.current.muted })
  }, [])

  const setShuffleAction = useCallback((shuffle: boolean) => {
    dispatch({ type: 'SET_SHUFFLE', payload: shuffle })
  }, [])

  const toggleShuffleAction = useCallback(() => {
    dispatch({ type: 'SET_SHUFFLE', payload: !stateRef.current.shuffle })
  }, [])

  const setRepeatModeAction = useCallback((mode: RepeatMode) => {
    dispatch({ type: 'SET_REPEAT_MODE', payload: mode })
  }, [])

  const cycleRepeatModeAction = useCallback(() => {
    const modes: RepeatMode[] = ['off', 'queue', 'track']
    const nextIdx = (modes.indexOf(stateRef.current.repeatMode) + 1) % modes.length
    const nextMode = modes[nextIdx] ?? 'off'
    dispatch({ type: 'SET_REPEAT_MODE', payload: nextMode })
  }, [])

  const setPlaybackRateAction = useCallback((rate: number) => {
    dispatch({ type: 'SET_PLAYBACK_RATE', payload: rate })
  }, [])

  const setCrossfadeAction = useCallback((seconds: number) => {
    dispatch({ type: 'SET_CROSSFADE', payload: seconds })
  }, [])

  const setSmartAlbumTransitionAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_SMART_ALBUM_TRANSITION', payload: enabled })
  }, [])

  const setSleepTimerAction = useCallback((option: SleepTimerOption) => {
    dispatch({ type: 'SET_SLEEP_TIMER', payload: option })
  }, [])

  const setStopAfterAction = useCallback((option: StopAfterOption) => {
    dispatch({ type: 'SET_STOP_AFTER', payload: option })
  }, [])

  const setEQEnabledAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_EQ_ENABLED', payload: enabled })
  }, [])

  const toggleEQAction = useCallback(() => {
    dispatch({ type: 'SET_EQ_ENABLED', payload: !stateRef.current.eqEnabled })
  }, [])

  const setEQModeAction = useCallback((mode: EQMode) => {
    dispatch({ type: 'SET_EQ_MODE', payload: mode })
  }, [])

  const setEQPresetAction = useCallback((presetId: string) => {
    dispatch({ type: 'SET_EQ_PRESET', payload: presetId })
  }, [])

  const setEQBandAction = useCallback((index: number, gain: number) => {
    dispatch({ type: 'SET_EQ_BAND', payload: { index, gain } })
  }, [])

  const saveCustomPresetAction = useCallback((name: string) => {
    dispatch({ type: 'SAVE_CUSTOM_PRESET', payload: { name } })
  }, [])

  const deleteCustomPresetAction = useCallback((id: string) => {
    dispatch({ type: 'DELETE_CUSTOM_PRESET', payload: id })
  }, [])

  const setParametricFilterAction = useCallback((filter: ParametricFilter) => {
    dispatch({ type: 'SET_PARAMETRIC_FILTER', payload: filter })
  }, [])

  const addParametricFilterAction = useCallback((filter: ParametricFilter) => {
    dispatch({ type: 'ADD_PARAMETRIC_FILTER', payload: filter })
  }, [])

  const deleteParametricFilterAction = useCallback((id: string) => {
    dispatch({ type: 'DELETE_PARAMETRIC_FILTER', payload: id })
  }, [])

  const setPreampAction = useCallback((db: number) => {
    dispatch({ type: 'SET_PREAMP', payload: db })
  }, [])

  const setAutoHeadroomAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_AUTO_HEADROOM', payload: enabled })
  }, [])

  const setLimiterAction = useCallback((enabled: boolean) => {
    dispatch({ type: 'SET_LIMITER', payload: enabled })
  }, [])

  const setBalanceAction = useCallback((balance: number) => {
    dispatch({ type: 'SET_BALANCE', payload: balance })
  }, [])

  const setMonoAction = useCallback((mono: boolean) => {
    dispatch({ type: 'SET_MONO', payload: mono })
  }, [])

  const setBassBoostAction = useCallback((boost: number) => {
    dispatch({ type: 'SET_BASS_BOOST', payload: boost })
  }, [])

  const setVisualizerModeAction = useCallback((mode: VisualizerMode) => {
    dispatch({ type: 'SET_VISUALIZER_MODE', payload: mode })
  }, [])

  const actionsValue = useMemo<PlayerActionsContextValue>(
    () => ({
      engine,
      playTrack,
      resumeSession,
      playAlbum,
      playArtist,
      playShuffled,
      playRadio,
      startRadio,
      setAutoplay: setAutoplayAction,
      playNext: playNextAction,
      playQueueIndex: playQueueIndexAction,
      addToQueue: addToQueueAction,
      removeFromQueue: removeFromQueueAction,
      reorderQueue: reorderQueueAction,
      clearQueue: clearQueueAction,
      clearUpcomingQueue: clearUpcomingQueueAction,
      togglePlayPause,
      play: playAction,
      pause: pauseAction,
      seek: seekAction,
      previous: previousAction,
      next: nextAction,
      setVolume: setVolumeAction,
      setMuted: setMutedAction,
      toggleMute: toggleMuteAction,
      setShuffle: setShuffleAction,
      toggleShuffle: toggleShuffleAction,
      setRepeatMode: setRepeatModeAction,
      cycleRepeatMode: cycleRepeatModeAction,
      setPlaybackRate: setPlaybackRateAction,
      setCrossfade: setCrossfadeAction,
      setSmartAlbumTransition: setSmartAlbumTransitionAction,
      setSleepTimer: setSleepTimerAction,
      setStopAfter: setStopAfterAction,
      setEQEnabled: setEQEnabledAction,
      toggleEQ: toggleEQAction,
      setEQMode: setEQModeAction,
      setEQPreset: setEQPresetAction,
      setEQBand: setEQBandAction,
      saveCustomPreset: saveCustomPresetAction,
      deleteCustomPreset: deleteCustomPresetAction,
      setParametricFilter: setParametricFilterAction,
      addParametricFilter: addParametricFilterAction,
      deleteParametricFilter: deleteParametricFilterAction,
      setPreamp: setPreampAction,
      setAutoHeadroom: setAutoHeadroomAction,
      setLimiter: setLimiterAction,
      setNormalization: (enabled) => dispatch({ type: 'SET_NORMALIZATION', payload: enabled }),
      setBalance: setBalanceAction,
      setMono: setMonoAction,
      setBassBoost: setBassBoostAction,
      setVisualizerMode: setVisualizerModeAction,
    }),
    [
      engine,
      playTrack,
      resumeSession,
      playAlbum,
      playArtist,
      playShuffled,
      playRadio,
      startRadio,
      setAutoplayAction,
      playNextAction,
      playQueueIndexAction,
      addToQueueAction,
      removeFromQueueAction,
      reorderQueueAction,
      clearQueueAction,
      clearUpcomingQueueAction,
      togglePlayPause,
      playAction,
      pauseAction,
      seekAction,
      previousAction,
      nextAction,
      setVolumeAction,
      setMutedAction,
      toggleMuteAction,
      setShuffleAction,
      toggleShuffleAction,
      setRepeatModeAction,
      cycleRepeatModeAction,
      setPlaybackRateAction,
      setCrossfadeAction,
      setSmartAlbumTransitionAction,
      setSleepTimerAction,
      setStopAfterAction,
      setEQEnabledAction,
      toggleEQAction,
      setEQModeAction,
      setEQPresetAction,
      setEQBandAction,
      saveCustomPresetAction,
      deleteCustomPresetAction,
      setParametricFilterAction,
      addParametricFilterAction,
      deleteParametricFilterAction,
      setPreampAction,
      setAutoHeadroomAction,
      setLimiterAction,
      setBalanceAction,
      setMonoAction,
      setBassBoostAction,
      setVisualizerModeAction,
    ],
  )

  const progressValue = useMemo<PlayerProgressContextValue>(
    () => ({
      currentTime: state.currentTime,
      duration: state.duration,
    }),
    [state.currentTime, state.duration],
  )

  // The same position as a subscribable store, for readers that only need a
  // value derived from it (the active lyric line) and should redraw only when
  // that value changes, not on every tick.
  const [progressStore] = useState(() => createProgressStore(progressValue))
  useLayoutEffect(() => progressStore.set(progressValue), [progressStore, progressValue])

  const stateWithoutProgress = useMemo<PlayerStateContextValue>(() => {
    const { currentTime: _c, duration: _d, ...rest } = state
    return rest
  }, [
    state.currentTrack,
    state.queue,
    state.originalQueue,
    state.queueIndex,
    state.queueContext,
    state.autoplay,
    state.status,
    state.volume,
    state.muted,
    state.shuffle,
    state.repeatMode,
    state.playbackRate,
    state.crossfadeSeconds,
    state.smartAlbumTransition,
    state.sleepTimer,
    state.sleepTimerEndsAt,
    state.stopAfter,
    state.error,
    state.eqEnabled,
    state.eqMode,
    state.selectedPresetId,
    state.graphicBands,
    state.customPresets,
    state.parametricFilters,
    state.preamp,
    state.autoHeadroom,
    state.limiterEnabled,
    state.normalizationEnabled,
    state.normalizationMessage,
    state.balance,
    state.mono,
    state.bassBoost,
    state.visualizerMode,
    state.history,
  ])

  const combinedValue = useMemo<PlayerContextValue>(
    () => ({
      ...stateWithoutProgress,
      ...progressValue,
      ...actionsValue,
    }),
    [stateWithoutProgress, progressValue, actionsValue],
  )

  return (
    <PlayerActionsContext.Provider value={actionsValue}>
      <PlayerProgressStoreContext.Provider value={progressStore}>
      <PlayerProgressContext.Provider value={progressValue}>
        <PlayerStateContext.Provider value={stateWithoutProgress}>
          <PlayerContext.Provider value={combinedValue}>{children}</PlayerContext.Provider>
        </PlayerStateContext.Provider>
      </PlayerProgressContext.Provider>
      </PlayerProgressStoreContext.Provider>
    </PlayerActionsContext.Provider>
  )
}

export function usePlayerState(): PlayerStateContextValue {
  const ctx = useContext(PlayerStateContext)
  if (!ctx) {
    throw new Error('usePlayerState must be used within a PlayerProvider')
  }
  return ctx
}

export function usePlayerProgress(): PlayerProgressContextValue {
  const ctx = useContext(PlayerProgressContext)
  if (!ctx) {
    throw new Error('usePlayerProgress must be used within a PlayerProvider')
  }
  return ctx
}

export function usePlayerActions(): PlayerActionsContextValue {
  const ctx = useContext(PlayerActionsContext)
  if (!ctx) {
    throw new Error('usePlayerActions must be used within a PlayerProvider')
  }
  return ctx
}

export function usePlayer(): PlayerContextValue {
  const ctx = useContext(PlayerContext)
  if (!ctx) {
    throw new Error('usePlayer must be used within a PlayerProvider')
  }
  return ctx
}
