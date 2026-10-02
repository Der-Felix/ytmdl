import { afterEach, beforeEach, describe, expect, it } from 'bun:test'
import { AudioEngine } from './engine'

function createMockAudioParam(initial = 0) {
  return {
    value: initial,
    setValueAtTime: () => {},
    linearRampToValueAtTime: () => {},
    cancelScheduledValues: () => {},
  }
}

function createMockNode() {
  return {
    connect: () => createMockNode(),
    disconnect: () => {},
    gain: createMockAudioParam(1),
    pan: createMockAudioParam(0),
    frequency: createMockAudioParam(1000),
    Q: createMockAudioParam(1),
    threshold: createMockAudioParam(-0.5),
    knee: createMockAudioParam(3),
    ratio: createMockAudioParam(20),
    attack: createMockAudioParam(0.003),
    release: createMockAudioParam(0.1),
    fftSize: 2048,
    smoothingTimeConstant: 0.8,
  }
}

describe('AudioEngine Metadata, Deck Isolation and State Transitions', () => {
  let originalAudioContext: typeof window.AudioContext
  let engine: AudioEngine
  let timeUpdates: { currentTime: number; duration: number }[]

  beforeEach(() => {
    originalAudioContext = window.AudioContext
    window.AudioContext = class {
      currentTime = 0
      state = 'running' as const
      destination = createMockNode()
      createMediaElementSource = () => createMockNode()
      createGain = () => createMockNode()
      createChannelSplitter = () => createMockNode()
      createChannelMerger = () => createMockNode()
      createBiquadFilter = () => createMockNode()
      createStereoPanner = () => createMockNode()
      createDynamicsCompressor = () => createMockNode()
      createAnalyser = () => createMockNode()
      resume = async () => {}
    } as unknown as typeof AudioContext

    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    engine = AudioEngine.getInstance()
    timeUpdates = []
    engine.setCallbacks({
      onTimeUpdate: (currentTime, duration) => {
        timeUpdates.push({ currentTime, duration })
      },
    })
  })

  afterEach(() => {
    engine.cancelCrossfade()
    ;(AudioEngine as unknown as { instance: AudioEngine | null }).instance = null
    window.AudioContext = originalAudioContext
  })

  it('preserves restored playback position when loadedmetadata fires without clobbering to 0', () => {
    engine.load('/api/v1/tracks/1/stream', 45)

    const activeDeck = engine.getActiveDeckElement()
    Object.defineProperty(activeDeck, 'duration', { value: 180, configurable: true, writable: true })

    // Simulate loadedmetadata event from browser audio decoder
    activeDeck.dispatchEvent(new Event('loadedmetadata'))

    // Position 45 is preserved and emitted with real duration 180
    expect(activeDeck.currentTime).toBe(45)
    expect(timeUpdates.length).toBeGreaterThan(0)
    const latest = timeUpdates[timeUpdates.length - 1]
    expect(latest.currentTime).toBe(45)
    expect(latest.duration).toBe(180)
  })

  it('ignores metadata and timeupdate events originating from the inactive deck', () => {
    engine.load('/api/v1/tracks/1/stream', 10)
    timeUpdates = []

    const inactiveDeck = engine.getInactiveDeckElement()
    Object.defineProperty(inactiveDeck, 'duration', { value: 300, configurable: true, writable: true })
    inactiveDeck.currentTime = 50

    // Fire events on inactive deck (e.g. background preload or previous deck)
    inactiveDeck.dispatchEvent(new Event('loadedmetadata'))
    inactiveDeck.dispatchEvent(new Event('durationchange'))
    inactiveDeck.dispatchEvent(new Event('timeupdate'))

    // No updates from inactive deck should leak through
    expect(timeUpdates).toEqual([])
  })

  it('guards against delayed metadata events from a previous track after track switch', () => {
    // 1. Load Track 1
    engine.load('/api/v1/tracks/track-1/stream', 0)
    const activeDeck = engine.getActiveDeckElement()

    // 2. Switch to Track 2 on the same deck
    engine.load('/api/v1/tracks/track-2/stream', 0)
    timeUpdates = []

    // 3. Simulate delayed metadata event referencing the old Track 1 URL
    const oldSrc = activeDeck.src
    Object.defineProperty(activeDeck, 'src', {
      value: 'http://localhost/api/v1/tracks/track-1/stream',
      configurable: true,
      writable: true,
    })
    Object.defineProperty(activeDeck, 'duration', { value: 999, configurable: true, writable: true })
    activeDeck.dispatchEvent(new Event('loadedmetadata'))

    // Event should be rejected because src does not match the active track URL
    expect(timeUpdates).toEqual([])

    // Restore real active track src and trigger metadata for Track 2
    Object.defineProperty(activeDeck, 'src', { value: oldSrc, configurable: true, writable: true })
    Object.defineProperty(activeDeck, 'duration', { value: 150, configurable: true, writable: true })
    activeDeck.dispatchEvent(new Event('loadedmetadata'))

    expect(timeUpdates.length).toBe(1)
    expect(timeUpdates[0].duration).toBe(150)
  })

  it('correctly handles reloading the same track with a new initial position', () => {
    // 1. Initial load at position 10
    engine.load('/api/v1/tracks/same-track/stream', 10)
    const activeDeck = engine.getActiveDeckElement()
    Object.defineProperty(activeDeck, 'duration', { value: 200, configurable: true, writable: true })
    activeDeck.dispatchEvent(new Event('loadedmetadata'))
    expect(activeDeck.currentTime).toBe(10)

    // 2. Reload the same track at position 85
    engine.load('/api/v1/tracks/same-track/stream', 85)
    timeUpdates = []

    // Fire loadedmetadata
    activeDeck.dispatchEvent(new Event('loadedmetadata'))

    // New position 85 must be applied and emitted
    expect(activeDeck.currentTime).toBe(85)
    expect(timeUpdates.length).toBeGreaterThan(0)
    const latest = timeUpdates[timeUpdates.length - 1]
    expect(latest.currentTime).toBe(85)
    expect(latest.duration).toBe(200)
  })

  it('keeps active playback isolated during gapless preloading on the secondary deck', () => {
    engine.load('/api/v1/tracks/current-playing/stream', 25)
    const activeDeck = engine.getActiveDeckElement()
    Object.defineProperty(activeDeck, 'duration', { value: 240, configurable: true, writable: true })
    activeDeck.dispatchEvent(new Event('loadedmetadata'))
    timeUpdates = []

    // Preload next track on inactive deck
    engine.preloadNext('/api/v1/tracks/next-upcoming/stream')
    const inactiveDeck = engine.getInactiveDeckElement()
    Object.defineProperty(inactiveDeck, 'duration', { value: 180, configurable: true, writable: true })
    inactiveDeck.currentTime = 0

    // Metadata fires on inactive deck for upcoming track
    inactiveDeck.dispatchEvent(new Event('loadedmetadata'))
    inactiveDeck.dispatchEvent(new Event('durationchange'))

    // Active track playback on Deck A must remain untouched
    expect(timeUpdates).toEqual([])
    expect(activeDeck.currentTime).toBe(25)
  })

  function prepareTransition() {
    engine.load('/current')
    engine.setCrossfade(0.05)
    engine.preloadNext('/next')
    const current = engine.getActiveDeckElement()
    const next = engine.getInactiveDeckElement()
    for (const deck of [current, next]) {
      Object.defineProperty(deck, 'duration', { value: 240, configurable: true })
      Object.defineProperty(deck, 'readyState', { value: 4, configurable: true })
      Object.defineProperty(deck, 'paused', { value: false, writable: true, configurable: true })
      deck.play = async () => { Object.defineProperty(deck, 'paused', { value: false, writable: true, configurable: true }) }
      deck.pause = () => { Object.defineProperty(deck, 'paused', { value: true, writable: true, configurable: true }) }
    }
    current.currentTime = 239.96
    return { current, next }
  }

  it('adopts the playing deck once and defers the next preload until outgoing audio stops', async () => {
    const { current, next } = prepareTransition()
    const started: string[] = []
    let ended = 0
    engine.setCallbacks({ onNextDeckCrossfadeStart: url => started.push(url), onTrackEnded: () => ended++ })
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    expect(started).toEqual(['/next'])
    expect(engine.getActiveDeckElement()).toBe(next)
    engine.preloadNext('/third')
    expect(current.src).toContain('/current')
    next.currentTime = 0.02
    next.dispatchEvent(new Event('timeupdate'))
    await new Promise(resolve => setTimeout(resolve, 70))
    expect(current.paused).toBe(true)
    expect(current.src).toContain('/third')
    expect(ended).toBe(0)
    expect(started.length).toBe(1)
    next.dispatchEvent(new Event('ended'))
    expect(ended).toBe(1)
  })

  it('keeps the current track and queue unchanged if incoming playback is rejected', async () => {
    const { current, next } = prepareTransition()
    next.play = async () => { throw new Error('decoder failure') }
    let started = 0
    engine.setCallbacks({ onNextDeckCrossfadeStart: () => started++ })
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    expect(engine.getActiveDeckElement()).toBe(current)
    expect(current.paused).toBe(false)
    expect(started).toBe(0)
    expect(engine.getStatus()).not.toBe('error')
  })

  it('stops both decks when pausing an overlap without advancing again', async () => {
    const { current, next } = prepareTransition()
    let started = 0
    engine.setCallbacks({ onNextDeckCrossfadeStart: () => started++ })
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    engine.pause()
    await new Promise(resolve => setTimeout(resolve, 70))
    expect(current.paused).toBe(true)
    expect(next.paused).toBe(true)
    expect(engine.getStatus()).toBe('paused')
    expect(started).toBe(1)
  })

  it('cancels a pending handoff when the queue is cleared or a new track is loaded', async () => {
    const { current, next } = prepareTransition()
    let resolvePlay: (() => void) | undefined
    next.play = () => new Promise<void>(resolve => { resolvePlay = resolve })
    let started = 0
    engine.setCallbacks({ onNextDeckCrossfadeStart: () => started++ })
    current.dispatchEvent(new Event('timeupdate'))
    engine.preloadNext(null)
    engine.load('/manual')
    resolvePlay?.()
    await Promise.resolve()
    expect(started).toBe(0)
    expect(engine.getActiveDeckElement().src).toContain('/manual')
    expect(next.paused).toBe(true)
  })

  it('cancels outgoing audio and stale timers when seeking or manually replacing a fading track', async () => {
    const { current, next } = prepareTransition()
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    engine.preloadNext('/third')
    engine.seek(10)
    expect(current.paused).toBe(true)
    expect(next.currentTime).toBe(10)
    expect(current.src).toContain('/third')
    engine.load('/manual')
    await new Promise(resolve => setTimeout(resolve, 70))
    expect(engine.getActiveDeckElement().src).toContain('/manual')
    expect(engine.getInactiveDeckElement().paused).toBe(true)
  })

  it('does not fade while paused or when the next decoder has insufficient data', async () => {
    const { current, next } = prepareTransition()
    let started = 0
    engine.setCallbacks({ onNextDeckCrossfadeStart: () => started++ })
    Object.defineProperty(next, 'readyState', { value: 1, configurable: true })
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    Object.defineProperty(next, 'readyState', { value: 4, configurable: true })
    current.pause()
    current.dispatchEvent(new Event('timeupdate'))
    await Promise.resolve()
    expect(started).toBe(0)
  })
})
