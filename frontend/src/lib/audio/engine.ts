import {
  calculateAutoHeadroom,
  EQ_FREQUENCIES,
} from './eqPresets'
import type {
  EQMode,
  ParametricFilter,
  PlaybackStatus,
} from './types'

function dbToLinear(db: number): number {
  return Math.pow(10, db / 20)
}

export interface AudioEngineCallbacks {
  onTimeUpdate?: (currentTime: number, duration: number) => void
  onStatusChange?: (status: PlaybackStatus) => void
  onError?: (error: string) => void
  onTrackEnded?: () => void
  onNextDeckCrossfadeStart?: () => void
}

export class AudioEngine {
  private static instance: AudioEngine | null = null

  private ctx: AudioContext | null = null
  private deckA: HTMLAudioElement
  private deckB: HTMLAudioElement
  private activeDeck: 'A' | 'B' = 'A'

  // Web Audio Nodes
  private sourceA: MediaElementAudioSourceNode | null = null
  private sourceB: MediaElementAudioSourceNode | null = null
  private gainA: GainNode | null = null
  private gainB: GainNode | null = null
  private mixBus: GainNode | null = null
  private preampNode: GainNode | null = null

  // Mono Matrix
  private monoSplitter: ChannelSplitterNode | null = null
  private monoSumL: GainNode | null = null
  private monoSumR: GainNode | null = null
  private monoMerger: ChannelMergerNode | null = null
  private monoBypassGain: GainNode | null = null
  private monoActiveGain: GainNode | null = null
  private monoOutput: GainNode | null = null

  // Graphic EQ (10 BiquadFilterNodes)
  private graphicFilterNodes: BiquadFilterNode[] = []

  // Parametric EQ (Up to 10 BiquadFilterNodes)
  private parametricFilterNodes: BiquadFilterNode[] = []

  // Post DSP
  private balanceNode: StereoPannerNode | null = null
  private limiterNode: DynamicsCompressorNode | null = null
  private limiterBypassGain: GainNode | null = null
  private limiterActiveGain: GainNode | null = null
  private limiterOutput: GainNode | null = null
  private analyserNode: AnalyserNode | null = null
  private masterGain: GainNode | null = null

  // Engine Settings State
  private volume = 1.0
  private muted = false
  private playbackRate = 1.0
  private preamp = 0
  private autoHeadroom = true
  private eqEnabled = true
  private eqMode: EQMode = 'graphic'
  private graphicBands: number[] = [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]
  private parametricFilters: ParametricFilter[] = []
  private balance = 0
  private mono = false
  private limiterEnabled = true
  private crossfadeSeconds = 0
  private smartAlbumTransition = true
  private isCrossfading = false
  private crossfadeTimer: number | null = null

  private callbacks: AudioEngineCallbacks = {}
  private status: PlaybackStatus = 'idle'
  private isPreloadingNext = false
  private nextTrackUrl: string | null = null

  private constructor() {
    this.deckA = new Audio()
    this.deckB = new Audio()
    this.configureDeck(this.deckA, 'A')
    this.configureDeck(this.deckB, 'B')
  }

  public static getInstance(): AudioEngine {
    if (!AudioEngine.instance) {
      AudioEngine.instance = new AudioEngine()
    }
    return AudioEngine.instance
  }

  public setCallbacks(callbacks: AudioEngineCallbacks): void {
    this.callbacks = callbacks
  }

  private configureDeck(deck: HTMLAudioElement, deckId: 'A' | 'B'): void {
    deck.crossOrigin = 'anonymous'
    deck.preload = 'auto'
    deck.preservesPitch = true
    // Vendor pitch preservation prefixes
    // @ts-expect-error browser compatibility prefix
    deck.mozPreservesPitch = true
    // @ts-expect-error browser compatibility prefix
    deck.webkitPreservesPitch = true

    deck.addEventListener('timeupdate', () => {
      if (this.activeDeck === deckId && !this.isCrossfading) {
        this.callbacks.onTimeUpdate?.(deck.currentTime || 0, deck.duration || 0)
        this.checkCrossfadeTrigger(deck)
      }
    })

    deck.addEventListener('play', () => {
      if (this.activeDeck === deckId) {
        this.setStatus('playing')
      }
    })

    deck.addEventListener('pause', () => {
      if (this.activeDeck === deckId && !this.isCrossfading && !deck.ended) {
        this.setStatus('paused')
      }
    })

    deck.addEventListener('waiting', () => {
      if (this.activeDeck === deckId) {
        this.setStatus('buffering')
      }
    })

    deck.addEventListener('playing', () => {
      if (this.activeDeck === deckId) {
        this.setStatus('playing')
      }
    })

    deck.addEventListener('ended', () => {
      if (this.activeDeck === deckId && !this.isCrossfading) {
        this.callbacks.onTrackEnded?.()
      }
    })

    deck.addEventListener('error', () => {
      if (this.activeDeck === deckId) {
        this.setStatus('error')
        this.callbacks.onError?.('Audiodatei konnte nicht geladen werden (404 oder inkompatibles Format).')
      }
    })
  }

  public ensureAudioContext(): AudioContext {
    if (!this.ctx) {
      const AudioCtx = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext
      this.ctx = new AudioCtx()
      this.buildAudioGraph()
    }
    if (this.ctx.state === 'suspended') {
      void this.ctx.resume()
    }
    return this.ctx
  }

  private buildAudioGraph(): void {
    if (!this.ctx) return

    // 1. Deck sources & gains
    this.sourceA = this.ctx.createMediaElementSource(this.deckA)
    this.sourceB = this.ctx.createMediaElementSource(this.deckB)
    this.gainA = this.ctx.createGain()
    this.gainB = this.ctx.createGain()
    this.gainA.gain.setValueAtTime(1.0, this.ctx.currentTime)
    this.gainB.gain.setValueAtTime(0.0, this.ctx.currentTime)

    this.sourceA.connect(this.gainA)
    this.sourceB.connect(this.gainB)

    // 2. Mix bus
    this.mixBus = this.ctx.createGain()
    this.gainA.connect(this.mixBus)
    this.gainB.connect(this.mixBus)

    // 3. Preamp
    this.preampNode = this.ctx.createGain()
    this.mixBus.connect(this.preampNode)

    // 4. Mono Matrix Node
    this.monoSplitter = this.ctx.createChannelSplitter(2)
    this.monoSumL = this.ctx.createGain()
    this.monoSumR = this.ctx.createGain()
    this.monoMerger = this.ctx.createChannelMerger(2)
    this.monoBypassGain = this.ctx.createGain()
    this.monoActiveGain = this.ctx.createGain()
    this.monoOutput = this.ctx.createGain()

    // 0.5 gain for summing to prevent +3dB volume doubling in mono mode
    this.monoSumL.gain.setValueAtTime(0.5, this.ctx.currentTime)
    this.monoSumR.gain.setValueAtTime(0.5, this.ctx.currentTime)

    this.preampNode.connect(this.monoBypassGain)
    this.preampNode.connect(this.monoSplitter)

    // Split L/R, sum each into merger inputs
    this.monoSplitter.connect(this.monoSumL, 0)
    this.monoSplitter.connect(this.monoSumR, 1)
    this.monoSumL.connect(this.monoMerger, 0, 0)
    this.monoSumL.connect(this.monoMerger, 0, 1)
    this.monoSumR.connect(this.monoMerger, 0, 0)
    this.monoSumR.connect(this.monoMerger, 0, 1)

    this.monoMerger.connect(this.monoActiveGain)
    this.monoBypassGain.connect(this.monoOutput)
    this.monoActiveGain.connect(this.monoOutput)

    // 5. Graphic EQ Filter Cascade (10 bands)
    this.graphicFilterNodes = EQ_FREQUENCIES.map((freq, idx) => {
      const node = this.ctx!.createBiquadFilter()
      node.frequency.setValueAtTime(freq, this.ctx!.currentTime)
      if (idx === 0) {
        node.type = 'lowshelf'
      } else if (idx === EQ_FREQUENCIES.length - 1) {
        node.type = 'highshelf'
      } else {
        node.type = 'peaking'
        node.Q.setValueAtTime(1.41, this.ctx!.currentTime)
      }
      node.gain.setValueAtTime(0, this.ctx!.currentTime)
      return node
    })

    // Chain graphic EQ nodes
    let lastNode: AudioNode = this.monoOutput
    for (const filter of this.graphicFilterNodes) {
      lastNode.connect(filter)
      lastNode = filter
    }

    // 6. Parametric EQ Filter Cascade (10 dynamic slots)
    this.parametricFilterNodes = Array.from({ length: 10 }, () => {
      const node = this.ctx!.createBiquadFilter()
      node.type = 'peaking'
      node.frequency.setValueAtTime(1000, this.ctx!.currentTime)
      node.gain.setValueAtTime(0, this.ctx!.currentTime)
      node.Q.setValueAtTime(1.41, this.ctx!.currentTime)
      return node
    })

    for (const filter of this.parametricFilterNodes) {
      lastNode.connect(filter)
      lastNode = filter
    }

    // 7. Balance (StereoPannerNode)
    if (typeof this.ctx.createStereoPanner === 'function') {
      this.balanceNode = this.ctx.createStereoPanner()
      this.balanceNode.pan.setValueAtTime(this.balance, this.ctx.currentTime)
      lastNode.connect(this.balanceNode)
      lastNode = this.balanceNode
    }

    // 8. Safety Limiter (DynamicsCompressorNode)
    this.limiterNode = this.ctx.createDynamicsCompressor()
    this.limiterNode.threshold.setValueAtTime(-0.5, this.ctx.currentTime)
    this.limiterNode.knee.setValueAtTime(3.0, this.ctx.currentTime)
    this.limiterNode.ratio.setValueAtTime(20.0, this.ctx.currentTime)
    this.limiterNode.attack.setValueAtTime(0.003, this.ctx.currentTime)
    this.limiterNode.release.setValueAtTime(0.1, this.ctx.currentTime)

    this.limiterBypassGain = this.ctx.createGain()
    this.limiterActiveGain = this.ctx.createGain()
    this.limiterOutput = this.ctx.createGain()

    lastNode.connect(this.limiterBypassGain)
    lastNode.connect(this.limiterNode)
    this.limiterNode.connect(this.limiterActiveGain)

    this.limiterBypassGain.connect(this.limiterOutput)
    this.limiterActiveGain.connect(this.limiterOutput)
    lastNode = this.limiterOutput

    // 9. AnalyserNode
    this.analyserNode = this.ctx.createAnalyser()
    this.analyserNode.fftSize = 2048
    this.analyserNode.smoothingTimeConstant = 0.8
    lastNode.connect(this.analyserNode)

    // 10. Master Gain -> Destination
    this.masterGain = this.ctx.createGain()
    this.analyserNode.connect(this.masterGain)
    this.masterGain.connect(this.ctx.destination)

    this.applyAllDSP()
  }

  private setStatus(newStatus: PlaybackStatus): void {
    if (this.status !== newStatus) {
      this.status = newStatus
      this.callbacks.onStatusChange?.(newStatus)
    }
  }

  public getStatus(): PlaybackStatus {
    return this.status
  }

  public getActiveDeckElement(): HTMLAudioElement {
    return this.activeDeck === 'A' ? this.deckA : this.deckB
  }

  public getInactiveDeckElement(): HTMLAudioElement {
    return this.activeDeck === 'A' ? this.deckB : this.deckA
  }

  public load(streamUrl: string, initialPosition = 0): void {
    this.ensureAudioContext()
    this.cancelCrossfade()

    const active = this.getActiveDeckElement()
    const inactive = this.getInactiveDeckElement()

    inactive.pause()
    inactive.src = ''

    this.setDeckGains(this.activeDeck === 'A' ? 1.0 : 0.0, this.activeDeck === 'B' ? 1.0 : 0.0)

    if (active.src !== streamUrl) {
      active.src = streamUrl
    }
    active.playbackRate = this.playbackRate
    if (initialPosition > 0) {
      active.currentTime = initialPosition
    }
    this.setStatus('paused')
  }

  public async loadAndPlay(streamUrl: string, initialPosition = 0): Promise<void> {
    this.ensureAudioContext()
    this.cancelCrossfade()

    const active = this.getActiveDeckElement()
    const inactive = this.getInactiveDeckElement()

    // Stop inactive deck
    inactive.pause()
    inactive.src = ''

    this.setDeckGains(this.activeDeck === 'A' ? 1.0 : 0.0, this.activeDeck === 'B' ? 1.0 : 0.0)

    active.src = streamUrl
    active.playbackRate = this.playbackRate
    if (initialPosition > 0) {
      active.currentTime = initialPosition
    }

    try {
      this.setStatus('buffering')
      await active.play()
      this.setStatus('playing')
    } catch (err) {
      if (err instanceof Error && (err.name === 'AbortError' || err.name === 'NotAllowedError')) {
        this.setStatus('paused')
        return
      }
      this.setStatus('error')
      this.callbacks.onError?.('Wiedergabe konnte nicht gestartet werden.')
    }
  }

  public preloadNext(streamUrl: string): void {
    if (this.nextTrackUrl === streamUrl && this.isPreloadingNext) return
    this.nextTrackUrl = streamUrl
    this.isPreloadingNext = true

    const inactive = this.getInactiveDeckElement()
    inactive.src = streamUrl
    inactive.preload = 'auto'
    inactive.playbackRate = this.playbackRate
  }

  private checkCrossfadeTrigger(deck: HTMLAudioElement): void {
    if (
      this.crossfadeSeconds <= 0 ||
      this.isCrossfading ||
      !this.nextTrackUrl ||
      !deck.duration ||
      deck.duration <= this.crossfadeSeconds * 2
    ) {
      return
    }

    const timeLeft = deck.duration - deck.currentTime
    if (timeLeft <= this.crossfadeSeconds && timeLeft > 0) {
      this.startCrossfade()
    }
  }

  private startCrossfade(): void {
    if (this.isCrossfading || !this.ctx) return
    this.isCrossfading = true
    this.callbacks.onNextDeckCrossfadeStart?.()

    const currentDeckId = this.activeDeck
    const nextDeckId = currentDeckId === 'A' ? 'B' : 'A'
    const currentDeck = currentDeckId === 'A' ? this.deckA : this.deckB
    const nextDeck = nextDeckId === 'A' ? this.deckA : this.deckB
    const currentGain = currentDeckId === 'A' ? this.gainA : this.gainB
    const nextGain = nextDeckId === 'A' ? this.gainA : this.gainB

    if (!currentGain || !nextGain) return

    const now = this.ctx.currentTime
    const duration = this.crossfadeSeconds

    nextDeck.currentTime = 0
    nextDeck.play().catch((err) => {
      if (err instanceof Error && err.name === 'AbortError') return
      this.setStatus('error')
      this.callbacks.onError?.('Nächster Titel konnte nicht gestartet werden.')
    })

    // Smooth linear crossfade ramp
    currentGain.gain.setValueAtTime(1.0, now)
    currentGain.gain.linearRampToValueAtTime(0.0, now + duration)

    nextGain.gain.setValueAtTime(0.0, now)
    nextGain.gain.linearRampToValueAtTime(1.0, now + duration)

    this.crossfadeTimer = window.setTimeout(() => {
      currentDeck.pause()
      currentDeck.src = ''
      this.activeDeck = nextDeckId
      this.isCrossfading = false
      this.isPreloadingNext = false
      this.nextTrackUrl = null
      this.callbacks.onTrackEnded?.()
    }, duration * 1000)
  }

  public cancelCrossfade(): void {
    if (this.crossfadeTimer !== null) {
      clearTimeout(this.crossfadeTimer)
      this.crossfadeTimer = null
    }
    this.isCrossfading = false
    this.isPreloadingNext = false
    this.nextTrackUrl = null
  }

  public switchDeckImmediate(): void {
    this.cancelCrossfade()
    const nextDeckId = this.activeDeck === 'A' ? 'B' : 'A'
    const currentDeck = this.getActiveDeckElement()
    currentDeck.pause()
    currentDeck.src = ''

    this.activeDeck = nextDeckId
    this.setDeckGains(this.activeDeck === 'A' ? 1.0 : 0.0, this.activeDeck === 'B' ? 1.0 : 0.0)
  }

  private setDeckGains(gainA: number, gainB: number): void {
    if (!this.ctx || !this.gainA || !this.gainB) return
    this.gainA.gain.setValueAtTime(gainA, this.ctx.currentTime)
    this.gainB.gain.setValueAtTime(gainB, this.ctx.currentTime)
  }

  public play(): void {
    this.ensureAudioContext()
    const active = this.getActiveDeckElement()
    if (active.src) {
      this.setStatus('buffering')
      active.play()
        .then(() => {
          this.setStatus('playing')
        })
        .catch((err) => {
          if (err instanceof Error && (err.name === 'AbortError' || err.name === 'NotAllowedError')) {
            this.setStatus('paused')
            return
          }
          this.setStatus('error')
          this.callbacks.onError?.('Wiedergabe konnte nicht gestartet werden.')
        })
    }
  }

  public pause(): void {
    this.getActiveDeckElement().pause()
  }

  public seek(seconds: number): void {
    const active = this.getActiveDeckElement()
    if (Number.isFinite(seconds) && active.duration) {
      active.currentTime = Math.max(0, Math.min(seconds, active.duration))
    }
  }

  public setVolume(vol: number): void {
    this.volume = Math.max(0, Math.min(1, vol))
    this.updateMasterGain()
  }

  public setMuted(muted: boolean): void {
    this.muted = muted
    this.updateMasterGain()
  }

  private updateMasterGain(): void {
    if (!this.ctx || !this.masterGain) return
    const targetGain = this.muted ? 0 : this.volume
    this.masterGain.gain.setValueAtTime(targetGain, this.ctx.currentTime)
  }

  public setPlaybackRate(rate: number): void {
    this.playbackRate = Math.max(0.5, Math.min(2.0, rate))
    this.deckA.playbackRate = this.playbackRate
    this.deckB.playbackRate = this.playbackRate
  }

  public setCrossfadeSeconds(seconds: number): void {
    this.crossfadeSeconds = Math.max(0, Math.min(12, seconds))
  }

  public setCrossfade(seconds: number): void {
    this.setCrossfadeSeconds(seconds)
  }

  public setSmartAlbumTransition(enabled: boolean): void {
    this.smartAlbumTransition = enabled
  }

  public getSmartAlbumTransition(): boolean {
    return this.smartAlbumTransition
  }

  // DSP Controls
  public setEQEnabled(enabled: boolean): void {
    this.eqEnabled = enabled
    this.applyAllDSP()
  }

  public setEQMode(mode: EQMode): void {
    this.eqMode = mode
    this.applyAllDSP()
  }

  public setGraphicBands(bands: number[]): void {
    this.graphicBands = [...bands]
    this.applyGraphicEQ()
    this.updatePreamp()
  }

  public setParametricFilters(filters: ParametricFilter[]): void {
    this.parametricFilters = [...filters]
    this.applyParametricEQ()
    this.updatePreamp()
  }

  public setPreamp(db: number): void {
    this.preamp = Math.max(-12, Math.min(6, db))
    this.updatePreamp()
  }

  public setAutoHeadroom(enabled: boolean): void {
    this.autoHeadroom = enabled
    this.updatePreamp()
  }

  private updatePreamp(): void {
    if (!this.ctx || !this.preampNode) return
    let headroom = 0
    if (this.autoHeadroom && this.eqEnabled) {
      headroom = calculateAutoHeadroom(
        this.graphicBands,
        this.parametricFilters,
        this.eqMode === 'graphic' ? 'graphic' : 'parametric',
      )
    }
    const totalPreampDB = this.preamp + headroom
    const linear = dbToLinear(totalPreampDB)
    this.preampNode.gain.setValueAtTime(linear, this.ctx.currentTime)
  }

  public setBalance(pan: number): void {
    this.balance = Math.max(-1, Math.min(1, pan))
    if (this.balanceNode && this.ctx) {
      this.balanceNode.pan.setValueAtTime(this.balance, this.ctx.currentTime)
    }
  }

  public setMono(mono: boolean): void {
    this.mono = mono
    if (!this.ctx || !this.monoBypassGain || !this.monoActiveGain) return
    if (this.mono) {
      this.monoBypassGain.gain.setValueAtTime(0, this.ctx.currentTime)
      this.monoActiveGain.gain.setValueAtTime(1, this.ctx.currentTime)
    } else {
      this.monoBypassGain.gain.setValueAtTime(1, this.ctx.currentTime)
      this.monoActiveGain.gain.setValueAtTime(0, this.ctx.currentTime)
    }
  }

  public setLimiter(enabled: boolean): void {
    this.limiterEnabled = enabled
    if (!this.ctx || !this.limiterBypassGain || !this.limiterActiveGain) return
    if (this.limiterEnabled) {
      this.limiterBypassGain.gain.setValueAtTime(0, this.ctx.currentTime)
      this.limiterActiveGain.gain.setValueAtTime(1, this.ctx.currentTime)
    } else {
      this.limiterBypassGain.gain.setValueAtTime(1, this.ctx.currentTime)
      this.limiterActiveGain.gain.setValueAtTime(0, this.ctx.currentTime)
    }
  }

  private applyGraphicEQ(): void {
    if (!this.ctx || this.graphicFilterNodes.length === 0) return
    const isGraphicActive = this.eqEnabled && this.eqMode === 'graphic'
    this.graphicFilterNodes.forEach((node, i) => {
      const targetGain = isGraphicActive ? (this.graphicBands[i] ?? 0) : 0
      node.gain.setValueAtTime(targetGain, this.ctx!.currentTime)
    })
  }

  private applyParametricEQ(): void {
    if (!this.ctx || this.parametricFilterNodes.length === 0) return
    const isParametricActive = this.eqEnabled && this.eqMode === 'parametric'
    this.parametricFilterNodes.forEach((node, i) => {
      const filter = this.parametricFilters[i]
      if (isParametricActive && filter && filter.enabled) {
        node.type = filter.type
        node.frequency.setValueAtTime(filter.frequency, this.ctx!.currentTime)
        node.gain.setValueAtTime(filter.gain, this.ctx!.currentTime)
        node.Q.setValueAtTime(filter.q, this.ctx!.currentTime)
      } else {
        node.gain.setValueAtTime(0, this.ctx!.currentTime)
      }
    })
  }

  public applyAllDSP(): void {
    this.updatePreamp()
    this.applyGraphicEQ()
    this.applyParametricEQ()
    this.setBalance(this.balance)
    this.setMono(this.mono)
    this.setLimiter(this.limiterEnabled)
    this.updateMasterGain()
  }

  // Visualizer / Analysis API
  public getFrequencyData(array: Uint8Array): void {
    if (this.analyserNode) {
      this.analyserNode.getByteFrequencyData(array as unknown as Uint8Array<ArrayBuffer>)
    }
  }

  public getTimeDomainData(array: Uint8Array): void {
    if (this.analyserNode) {
      this.analyserNode.getByteTimeDomainData(array as unknown as Uint8Array<ArrayBuffer>)
    }
  }

  /** Check for potential digital peak / clip near 0 dBFS */
  public isNearPeak(): boolean {
    if (!this.analyserNode) return false
    const data = new Uint8Array(32)
    this.analyserNode.getByteTimeDomainData(data as unknown as Uint8Array<ArrayBuffer>)
    for (let i = 0; i < data.length; i++) {
      // 128 is center; 254 or 1 is peak ceiling/floor
      const val = data[i]
      if (val !== undefined && (val >= 253 || val <= 3)) {
        return true
      }
    }
    return false
  }
}
