import AVFoundation
import Foundation
import MediaPlayer
import Observation
import CoreGraphics
import YTMDLCore
#if os(iOS) || os(tvOS)
import UIKit
#elseif os(macOS)
import AppKit
#endif

enum SleepMode: String, CaseIterable, Identifiable {
    case off, minutes15, minutes30, minutes60, endOfTrack, endOfAlbum
    var id: String { rawValue }
    var name: String { switch self { case .off: "Aus"; case .minutes15: "15 Minuten"; case .minutes30: "30 Minuten"; case .minutes60: "60 Minuten"; case .endOfTrack: "Nach diesem Titel"; case .endOfAlbum: "Nach diesem Album" } }
    var minutes: Double? { switch self { case .minutes15: 15; case .minutes30: 30; case .minutes60: 60; default: nil } }
}

@MainActor @Observable final class PlayerModel {
    private(set) var queue = PlaybackQueue()
    private(set) var isPlaying = false
    private(set) var loading = false
    private(set) var position: Double = 0
    private(set) var volume: Double = 1
    private(set) var isMuted = false
    private(set) var crossfadeSeconds: Double = 0
    private(set) var smartAlbumTransition = true
    private(set) var preloadEnabled = true
    private(set) var fastStart = true
    private(set) var playbackRate: Double = 1
    private(set) var isCrossfading = false
    private(set) var repeatOne = false
    private(set) var sleepMode = SleepMode.off
    private(set) var sleepDeadline: Date?
    private(set) var startupMilliseconds: Double?
    private(set) var visualizationEnabled = false
    private(set) var levelHistory: [Double] = []
    private(set) var spectrumLevels = Array(repeating: 0.0, count: 32)
    private(set) var spectrumPeaks = Array(repeating: 0.0, count: 32)
    @ObservationIgnored private var visualizerTask: Task<Void, Never>?
    @ObservationIgnored private var spectrumSequence: UInt64 = 0
    @ObservationIgnored private var spectrumReceivedAt = ContinuousClock.now
    @ObservationIgnored private var spectrumTarget = Array(repeating: 0.0, count: 32)
    @ObservationIgnored private var meteredFrames: UInt64 = 0
    private(set) var equalizerFormat = 0
    private(set) var artwork: CGImage? {
        didSet { systemArtwork = artwork.map(Self.nowPlayingArtwork) }
    }
    @ObservationIgnored private var systemArtwork: MPMediaItemArtwork?
    private(set) var artworkPalette: ArtworkPalette?
    let equalizer: EqualizerModel
    var offlineLibrary: OfflineLibrary?
    var offlineOnly = false
    private(set) var autoplay = false
    @ObservationIgnored var onQueueEnded: ((Track) -> Void)?
    @ObservationIgnored var onSnapshot: ((PlaybackQueue, Double, String, Bool) -> Void)?
    var repeatAll = false { didSet { cancelPrepared(); prepareNext() } }
    var error: String?
    var soundError: String?
    var lyrics = ""
    var current: Track? { queue.current }
    var duration: Double {
        Self.resolvedDuration(metadata: current?.duration ?? 0, stream: audio.currentItem?.duration.seconds ?? 0)
    }
    static func resolvedDuration(metadata: Double, stream: Double) -> Double {
        // Library duration is authoritative for known files. AVFoundation may
        // report an inflated estimate for streamed Opus; use it only as fallback.
        if metadata.isFinite && metadata > 0 { return metadata }
        return stream.isFinite && stream > 0 ? stream : 0
    }
    var isPlaybackRequested: Bool { wantsPlayback }
    var preparedTrackID: String? { pendingID }
    @ObservationIgnored private let volumePreferences: UserDefaults
    @ObservationIgnored var onTrackPlayed: ((Track) -> Void)?
    @ObservationIgnored private var recordedGeneration: UUID?
    @ObservationIgnored private var audio: AVPlayer
    @ObservationIgnored private var standby: AVPlayer
    @ObservationIgnored private var pendingID: String?
    @ObservationIgnored private var fadeStart: Double?
    @ObservationIgnored private var fadeDuration: Double = 0
    @ObservationIgnored private var fadeFraction: Double = 0
    @ObservationIgnored private var client: APIClient?
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var lyricsTask: Task<Void, Never>?
    @ObservationIgnored private var artworkTask: Task<Void, Never>?
    @ObservationIgnored private var sleepTask: Task<Void, Never>?
    @ObservationIgnored private var observer: Any?
    @ObservationIgnored private var fadeObserver: Any?
    @ObservationIgnored private var prefetchAfter = ContinuousClock.now
    @ObservationIgnored private var endObserver: NSObjectProtocol?
    @ObservationIgnored private var failureObserver: NSObjectProtocol?
    @ObservationIgnored private var interruptionObserver: NSObjectProtocol?
    @ObservationIgnored private var routeObserver: NSObjectProtocol?
    @ObservationIgnored private var statusObserver: NSKeyValueObservation?
    @ObservationIgnored private var timeObserver: NSKeyValueObservation?
    @ObservationIgnored private var generation = UUID()
    private var wantsPlayback = false
    @ObservationIgnored private var startedAt: ContinuousClock.Instant?
    @ObservationIgnored private var lastNowPlaying = Date.distantPast
    @ObservationIgnored private var commands: [(MPRemoteCommand, Any)] = []
    @ObservationIgnored private let itemFactory: ((Track, APIClient) throws -> AVPlayerItem)?

    init(volumePreferences: UserDefaults = .standard, audio: AVPlayer = AVPlayer(), standby: AVPlayer = AVPlayer(), itemFactory: ((Track, APIClient) throws -> AVPlayerItem)? = nil) {
        self.audio = audio; self.standby = standby; self.volumePreferences = volumePreferences; self.itemFactory = itemFactory
        equalizer = EqualizerModel(preferences: volumePreferences)
        if let value = volumePreferences.object(forKey: "crossfadeSeconds") as? Double, value.isFinite { crossfadeSeconds = min(12, max(0, value)) }
        smartAlbumTransition = volumePreferences.object(forKey: "smartAlbumTransition") as? Bool ?? true
        preloadEnabled = volumePreferences.object(forKey: "preloadNextTrack") as? Bool ?? true
        autoplay = volumePreferences.bool(forKey: "playerAutoplay")
        fastStart = volumePreferences.object(forKey: "fastPlaybackStart") as? Bool ?? true
        if let value = volumePreferences.object(forKey: "playbackRate") as? Double, value.isFinite { playbackRate = min(2, max(0.5, value)) }
        #if os(macOS)
        if let saved = volumePreferences.object(forKey: "playerVolume") as? Double, saved.isFinite { volume = min(1, max(0, saved)) }
        isMuted = volumePreferences.bool(forKey: "playerMuted")
        #endif
        configure(audio); configure(standby); applyVolume()
        visualizationEnabled = volumePreferences.bool(forKey: "playerVisualization")
        equalizer.parameters.visualizationEnabled.store(visualizationEnabled, ordering: .releasing)
        equalizer.onEnabledChange = { [weak self] _ in self?.updateEqualizerAttachment() }
        setupCommands()
        #if os(iOS) || os(tvOS)
        interruptionObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.didBecomeInactiveNotification, object: nil, queue: .main) { [weak self] _ in Task { @MainActor in self?.pause() } }
        routeObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] notification in
            let reason = notification.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
            if reason == AVAudioSession.RouteChangeReason.oldDeviceUnavailable.rawValue { Task { @MainActor in self?.pause() } }
        }
        #endif
    }
    private func configure(_ player: AVPlayer) { player.automaticallyWaitsToMinimizeStalling = !fastStart }
    func setVolume(_ value: Double) {
        guard value.isFinite else { return }; volume = min(1, max(0, value)); isMuted = false; applyVolume()
        #if os(macOS)
        volumePreferences.set(volume, forKey: "playerVolume"); volumePreferences.set(false, forKey: "playerMuted")
        #endif
    }
    func toggleMute() {
        isMuted.toggle(); applyVolume()
        #if os(macOS)
        volumePreferences.set(isMuted, forKey: "playerMuted")
        #endif
    }
    func setCrossfade(_ seconds: Double) {
        guard seconds.isFinite else { return }; crossfadeSeconds = min(12, max(0, seconds))
        volumePreferences.set(crossfadeSeconds, forKey: "crossfadeSeconds")
        cancelPrepared(); prepareNext()
    }
    func setSmartAlbumTransition(_ enabled: Bool) { smartAlbumTransition = enabled; volumePreferences.set(enabled, forKey: "smartAlbumTransition"); cancelPrepared(); prepareNext() }
    func setPreload(_ enabled: Bool) { preloadEnabled = enabled; volumePreferences.set(enabled, forKey: "preloadNextTrack"); cancelPrepared(); prepareNext() }
    func setAutoplay(_ value: Bool) { autoplay = value; volumePreferences.set(value, forKey: "playerAutoplay") }
    func setFastStart(_ enabled: Bool) {
        fastStart = enabled; volumePreferences.set(enabled, forKey: "fastPlaybackStart"); configure(audio); configure(standby)
        audio.currentItem?.preferredForwardBufferDuration = enabled ? 5 : 20
    }
    func setPlaybackRate(_ value: Double) {
        guard value.isFinite else { return }; playbackRate = min(2, max(0.5, value)); volumePreferences.set(playbackRate, forKey: "playbackRate")
        for item in [audio.currentItem, standby.currentItem].compactMap({ $0 }) {
            item.audioTimePitchAlgorithm = playbackRate == 1 ? .varispeed : .spectral
        }
        if wantsPlayback { audio.rate = Float(playbackRate); if isCrossfading { standby.rate = Float(playbackRate) } }
        updateNowPlaying()
    }
    func setRepeatOne(_ enabled: Bool) { repeatOne = enabled; cancelPrepared(); prepareNext() }
    func setSleepMode(_ mode: SleepMode) {
        sleepTask?.cancel(); sleepMode = mode; sleepDeadline = mode.minutes.map { Date().addingTimeInterval($0 * 60) }
        cancelPrepared(); prepareNext()
        if let deadline = sleepDeadline {
            sleepTask = Task { [weak self] in
                do { try await Task.sleep(for: .seconds(max(0, deadline.timeIntervalSinceNow))); try Task.checkCancellation() }
                catch { return }
                self?.pause(); self?.cancelPrepared(); self?.sleepMode = .off; self?.sleepDeadline = nil
            }
        }
    }
    func play(_ tracks: [Track], start: Int = 0, client: APIClient) {
        guard !tracks.isEmpty else { stop(); return }
        self.client = client; queue.replace(tracks, start: start); loadCurrent()
    }
    #if DEBUG
    // Loopback UI fixtures can inspect navigation with a selected title without
    // creating an audio item, activating the audio session or playing a tone.
    func previewPaused(_ tracks: [Track], client: APIClient) {
        generation = UUID(); self.client = client; queue.replace(tracks)
        if let current { loadDetails(current, client: client) }
        else { artwork = nil; MPNowPlayingInfoCenter.default().nowPlayingInfo = nil }
    }
    #endif
    func append(_ track: Track, client: APIClient) { self.client = client; queue.append(track); prepareNext() }
    func select(_ index: Int) { guard queue.tracks.indices.contains(index) else { return }; queue.select(index); loadCurrent() }
    func shuffle() { queue.shuffleUpcoming(); cancelPrepared(); prepareNext() }
    func removeFromQueue(_ index: Int) {
        guard queue.remove(at: index) else { return }
        cancelPrepared(); prepareNext()
    }
    func playNextInQueue(_ index: Int) {
        guard queue.playNext(at: index) else { return }
        cancelPrepared(); prepareNext()
    }
    func clearUpcoming() {
        guard queue.clearUpcoming() else { return }
        cancelPrepared(); prepareNext()
    }
    func setVisualization(_ value: Bool) {
        guard visualizationEnabled != value else { return }
        visualizationEnabled = value; levelHistory = []
        equalizer.parameters.visualizationEnabled.store(value, ordering: .releasing)
        resetSpectrum()
        meteredFrames = equalizer.parameters.frames.load(ordering: .acquiring)
        volumePreferences.set(value, forKey: "playerVisualization")
        updateEqualizerAttachment()
        if value { ensureVisualizerTask() } else { visualizerTask?.cancel(); visualizerTask = nil }
    }
    private func resetSpectrum() {
        spectrumLevels = Array(repeating: 0, count: 32); spectrumPeaks = spectrumLevels; spectrumTarget = spectrumLevels
        spectrumSequence = equalizer.parameters.spectrum.sequence.load(ordering: .acquiring)
    }
    private func ensureVisualizerTask() {
        guard visualizationEnabled, current != nil, visualizerTask == nil else { return }
        visualizerTask = Task { @MainActor [weak self] in
            while !Task.isCancelled {
                guard self != nil else { break }
                self?.updateSpectrum()
                do { try await Task.sleep(for: .milliseconds(33)) } catch { break }
            }
        }
    }
    private func updateSpectrum() {
        let snapshot = equalizer.parameters.spectrum
        let sequence = snapshot.sequence.load(ordering: .acquiring)
        if sequence != spectrumSequence {
            spectrumTarget = snapshot.read(); spectrumSequence = sequence; spectrumReceivedAt = .now
        }
        let fresh = isPlaying && spectrumReceivedAt.duration(to: .now) < .milliseconds(250)
        var levels = spectrumLevels, peaks = spectrumPeaks
        for band in levels.indices {
            let target = fresh ? spectrumTarget[band] : 0
            levels[band] += (target - levels[band]) * (target > levels[band] ? 0.65 : 0.18)
            if levels[band] < 0.001 { levels[band] = 0 }
            peaks[band] = max(levels[band], max(0, peaks[band] - 0.012))
        }
        if levels != spectrumLevels { spectrumLevels = levels }
        if peaks != spectrumPeaks { spectrumPeaks = peaks }
    }
    func next() {
        if promotePrepared() { return }
        if queue.next(repeatAll: repeatAll) { loadCurrent() }
        else { let ended = current; cancelPrepared(); pause(); seek(0); if autoplay, let ended { onQueueEnded?(ended) } }
    }
    func previous() { if position > 3 { seek(0) } else { queue.previous(); loadCurrent() } }
    func toggle() { wantsPlayback ? pause() : resume() }
    func pause() { wantsPlayback = false; audio.pause(); standby.pause(); isPlaying = false; loading = false; updateNowPlaying() }
    func resume() {
        guard current != nil else { return }
        if error != nil || audio.currentItem == nil { loadCurrent(); return }
        wantsPlayback = true
        let token = generation
        loadTask = Task { [weak self] in
            guard let self else { return }
            do {
                try await activateAudio(); try Task.checkCancellation()
                guard self.generation == token, self.wantsPlayback else { return }
                self.start(audio); if isCrossfading { self.start(standby) }; updateNowPlaying()
            } catch is CancellationError { }
            catch { if generation == token { self.error = "Die Audioausgabe konnte nicht aktiviert werden."; pause() } }
        }
    }
    func seek(_ seconds: Double) {
        guard seconds.isFinite, current != nil else { return }
        cancelPrepared()
        // Do not create a new incoming stream for every slider drag event.
        prefetchAfter = .now.advanced(by: .milliseconds(200))
        let target = min(max(0, seconds), max(0, duration))
        audio.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        position = target; updateNowPlaying()
    }
    func stop() {
        visualizerTask?.cancel(); visualizerTask = nil; resetSpectrum()
        generation = UUID(); loadTask?.cancel(); lyricsTask?.cancel(); artworkTask?.cancel(); sleepTask?.cancel()
        removeItemObservers(); cancelPrepared(); pause(); audio.replaceCurrentItem(with: nil)
        queue.replace([]); position = 0; loading = false; error = nil; soundError = nil; lyrics = ""; client = nil
        artwork = nil; artworkPalette = nil; levelHistory = []; sleepMode = .off; sleepDeadline = nil; startupMilliseconds = nil
        MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        #if os(iOS) || os(tvOS)
        AVAudioSession.sharedInstance().deactivate(options: .notifyOthersOnDeactivation) { _, _ in }
        #endif
    }
    func makeItem(_ track: Track, client: APIClient) throws -> AVPlayerItem {
        let item: AVPlayerItem
        if let itemFactory { item = try itemFactory(track, client) }
        else if let url = offlineLibrary?.audioURL(track.id) { item = AVPlayerItem(url: url) }
        else {
            if offlineOnly { throw OfflineError.unavailable }
            let path = try client.server.itemPath("tracks", id: track.id, suffix: "/stream")
            let asset = AVURLAsset(url: try client.server.endpoint(path), options: [AVURLAssetHTTPCookiesKey: client.authenticationCookies])
            item = AVPlayerItem(asset: asset)
        }
        item.preferredForwardBufferDuration = fastStart ? 5 : 20
        // Pitch correction adds latency and is only needed at a changed speed.
        item.audioTimePitchAlgorithm = playbackRate == 1 ? .varispeed : .spectral
        if equalizer.enabled || visualizationEnabled { attachSound(to: item) }
        return item
    }
    private func attachSound(to item: AVPlayerItem) {
        do { try attachEqualizer(to: item, parameters: equalizer.parameters) }
        catch { soundError = "Die Klangverarbeitung konnte nicht gestartet werden. Der Titel wird ohne Equalizer abgespielt." }
    }
    private func updateEqualizerAttachment() {
        let needsTap = equalizer.enabled || visualizationEnabled
        if !needsTap {
            soundError = nil; equalizerFormat = 0; equalizer.parameters.format.store(0, ordering: .releasing)
        }
        for item in [audio.currentItem, standby.currentItem].compactMap({ $0 }) {
            if needsTap {
                // EQ changes already reach the callback atomically. Keep a live
                // meter's tap rather than rebuilding the audio graph on toggles.
                if item.audioMix == nil { soundError = nil; attachSound(to: item) }
            } else { item.audioMix = nil }
        }
    }
    private func loadCurrent() {
        loadTask?.cancel(); removeItemObservers(); cancelPrepared(); generation = UUID()
        prefetchAfter = .now
        audio.pause(); audio.replaceCurrentItem(with: nil); isPlaying = false; position = 0; levelHistory = []; meteredFrames = equalizer.parameters.frames.load(ordering: .acquiring); error = nil; soundError = nil
        guard let current, let client else { loading = false; return }
        loading = true; wantsPlayback = true; startedAt = .now; startupMilliseconds = nil
        let token = generation
        do {
            audio.replaceCurrentItem(with: try makeItem(current, client: client)); observeCurrent()
            loadTask = Task { [weak self] in
                guard let self else { return }
                do {
                    try await activateAudio(); try Task.checkCancellation()
                    guard generation == token, wantsPlayback else { return }
                    start(audio)
                } catch is CancellationError { }
                catch { if generation == token { failPlayback() } }
            }
        } catch { failPlayback() }
        loadDetails(current, client: client)
    }
    private func start(_ player: AVPlayer) {
        if fastStart { player.playImmediately(atRate: Float(playbackRate)) } else { player.rate = Float(playbackRate) }
    }
    private func observeCurrent() {
        guard let item = audio.currentItem else { return }
        resetSpectrum(); ensureVisualizerTask()
        let token = generation
        statusObserver = item.observe(\.status, options: [.initial, .new]) { [weak self] item, _ in
            let failed = item.status == .failed
            if failed { Task { @MainActor in guard self?.generation == token else { return }; self?.failPlayback() } }
        }
        timeObserver = audio.observe(\.timeControlStatus, options: [.initial, .new]) { [weak self] _, _ in
            Task { @MainActor in guard self?.generation == token else { return }; self?.updatePlaybackState() }
        }
        observer = audio.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.1, preferredTimescale: 600), queue: .main) { [weak self] _ in
            Task { @MainActor in guard let self, self.generation == token else { return }; self.tick() }
        }
        endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in guard self?.generation == token else { return }; self?.didFinish() }
        }
        failureObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: item, queue: .main) { [weak self] _ in
            Task { @MainActor in guard self?.generation == token else { return }; self?.failPlayback() }
        }
    }
    private func updatePlaybackState() {
        isPlaying = audio.timeControlStatus == .playing
        loading = wantsPlayback && !isPlaying && error == nil
        if isCrossfading, wantsPlayback {
            if !isPlaying { standby.pause() }
            else if standby.rate == 0, standby.currentItem?.status == .readyToPlay { start(standby) }
        }
    }
    private func tick() {
        equalizerFormat = equalizer.parameters.format.load(ordering: .acquiring)
        // The OS mixed-output tap includes AVPlayer gain; this is a level
        // history, not an FFT or a calibrated measurement of speaker output.
        if visualizationEnabled {
            let frames = equalizer.parameters.frames.load(ordering: .acquiring)
            let rms = Double(bitPattern: equalizer.parameters.meterRMS.load(ordering: .relaxed))
            let level = isPlaying && frames != meteredFrames && rms.isFinite ? min(1, max(0, (20 * log10(max(1e-6, rms)) + 96) / 96)) : 0
            meteredFrames = frames
            levelHistory.append(level)
            if levelHistory.count > 48 { levelHistory.removeFirst(levelHistory.count - 48) }
        }
        let value = audio.currentTime().seconds
        if value.isFinite { position = max(0, value) }
        updatePlaybackState()
        if isPlaying, position > 0, startupMilliseconds == nil, let startedAt {
            let elapsed = startedAt.duration(to: .now).components
            startupMilliseconds = Double(elapsed.seconds) * 1000 + Double(elapsed.attoseconds) / 1e15
        }
        if isPlaying, position >= 1, recordedGeneration != generation, let current {
            recordedGeneration = generation; onTrackPlayed?(current)
        }
        if let sleepDeadline, Date() >= sleepDeadline { pause(); setSleepMode(.off); return }
        if isPlaying { prepareNext(); updateFade() }
        if Date().timeIntervalSince(lastNowPlaying) >= 0.5 { updateNowPlaying() }
    }
    private var upcomingIndex: Int? {
        if queue.index + 1 < queue.tracks.count { return queue.index + 1 }
        return repeatAll && !queue.tracks.isEmpty ? 0 : nil
    }
    private func sameAlbum(_ a: Track, _ b: Track) -> Bool { !a.album.isEmpty && a.album == b.album && a.artists == b.artists }
    private var shouldStopAtBoundary: Bool {
        if sleepMode == .endOfTrack { return true }
        if sleepMode == .endOfAlbum {
            if queue.index + 1 >= queue.tracks.count { return true }
            guard let current, let index = upcomingIndex else { return true }
            return !sameAlbum(current, queue.tracks[index])
        }
        return false
    }
    private func prepareNext() {
        guard wantsPlayback, ContinuousClock.now >= prefetchAfter, (preloadEnabled || crossfadeSeconds > 0), !repeatOne, !shouldStopAtBoundary, let client, let index = upcomingIndex else { return }
        let next = queue.tracks[index]
        guard pendingID != next.id else { return }
        cancelPrepared(); pendingID = next.id
        do {
            let item = try makeItem(next, client: client); item.preferredForwardBufferDuration = 3
            standby.replaceCurrentItem(with: item)
        } catch { /* A failed prefetch must not interrupt the current title. */ }
    }
    private func updateFade(playhead: Double? = nil) {
        if standby.currentItem?.status == .failed {
            removeFadeObserver(); standby.pause(); fadeStart = nil; fadeFraction = 0; isCrossfading = false; applyVolume(); return
        }
        guard crossfadeSeconds > 0, !shouldStopAtBoundary, !repeatOne, let current, let index = upcomingIndex,
              pendingID == queue.tracks[index].id, standby.currentItem?.status == .readyToPlay else { return }
        if smartAlbumTransition && sameAlbum(current, queue.tracks[index]) { return }
        let time = playhead ?? position
        guard time.isFinite else { return }
        let remaining = duration - time
        // Keep at least half of each short track outside the overlap.
        let preparedDuration = standby.currentItem?.duration.seconds ?? 0
        let nextDuration = Self.resolvedDuration(metadata: queue.tracks[index].duration, stream: preparedDuration)
        let window = min(crossfadeSeconds * playbackRate, duration / 2, nextDuration / 2)
        guard remaining <= window, window > 0, remaining > 0 else { return }
        if !isCrossfading { isCrossfading = true; fadeDuration = max(0.1, remaining); start(standby) }
        if fadeObserver == nil {
            let token = generation
            // Gain changes run at 50 Hz; the rest of the UI still ticks at 10 Hz.
            fadeObserver = audio.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.02, preferredTimescale: 600), queue: .main) { [weak self] time in
                Task { @MainActor in
                    guard let self, self.generation == token, self.isCrossfading, self.wantsPlayback, self.isPlaying else { return }
                    self.updateFade(playhead: time.seconds)
                }
            }
        }
        guard standby.timeControlStatus == .playing else {
            fadeFraction = 0; fadeStart = nil; applyVolume(); return
        }
        if fadeStart == nil { fadeStart = time; fadeDuration = max(0.1, duration - time) }
        fadeFraction = min(1, max(0, (time - fadeStart!) / fadeDuration))
        applyVolume()
    }
    private func applyVolume() {
        audio.volume = Float(volume * (1 - fadeFraction)); standby.volume = Float(volume * fadeFraction)
        audio.isMuted = isMuted; standby.isMuted = isMuted
    }
    @discardableResult private func promotePrepared() -> Bool {
        guard let index = upcomingIndex, pendingID == queue.tracks[index].id, standby.currentItem?.status == .readyToPlay else { return false }
        let play = wantsPlayback
        removeItemObservers(); audio.pause(); audio.replaceCurrentItem(with: nil)
        let previous = audio; audio = standby; standby = previous
        _ = queue.next(repeatAll: repeatAll); generation = UUID(); pendingID = nil; fadeStart = nil; fadeFraction = 0; isCrossfading = false
        position = max(0, audio.currentTime().seconds.isFinite ? audio.currentTime().seconds : 0)
        error = nil; loading = true; startedAt = .now; startupMilliseconds = nil
        applyVolume(); observeCurrent()
        if play { start(audio) } else { audio.pause() }
        if let current, let client { loadDetails(current, client: client) }
        return true
    }
    private func cancelPrepared() {
        removeFadeObserver()
        standby.pause(); standby.replaceCurrentItem(with: nil); pendingID = nil
        fadeStart = nil; fadeFraction = 0; isCrossfading = false; applyVolume()
    }
    private func didFinish() {
        if shouldStopAtBoundary { pause(); setSleepMode(.off); cancelPrepared(); seek(0); return }
        if repeatOne { seek(0); resume(); return }
        next()
    }
    private func loadDetails(_ track: Track, client: APIClient) {
        lyricsTask?.cancel(); artworkTask?.cancel(); lyrics = ""; artwork = nil; artworkPalette = nil
        if let localLyrics = offlineLibrary?.lyrics(track.id) { lyrics = localLyrics }
        if let url = offlineLibrary?.artworkURL(track.id), let data = try? Data(contentsOf: url), let image = ArtworkPalette.thumbnail(data) {
            artwork = image; artworkPalette = ArtworkPalette.extract(image)
        }
        // Publish the new title immediately, dropping the previous title's cover.
        // Publish again when the authenticated or offline artwork becomes ready.
        updateNowPlaying()
        if offlineOnly {
            if lyrics.isEmpty { lyrics = "Für diesen Titel sind keine Offline-Lyrics gespeichert." }
            return
        }
        let token = generation
        lyricsTask = Task { [weak self] in
            do {
                try Task.checkCancellation()
                let result: Lyrics = try await client.get(client.server.itemPath("tracks", id: track.id, suffix: "/lyrics"))
                try Task.checkCancellation(); guard self?.generation == token else { return }
                self?.lyrics = result.content ?? "Für diesen Titel sind keine Lyrics gespeichert."
            } catch { if self?.generation == token, !Task.isCancelled { self?.lyrics = "Lyrics konnten nicht geladen werden." } }
        }
        artworkTask = Task { [weak self] in
            do {
                try Task.checkCancellation()
                let (data, response) = try await client.session.data(for: client.artworkRequest(kind: "tracks", id: track.id))
                try Task.checkCancellation()
                guard (response as? HTTPURLResponse)?.statusCode == 200, data.count <= 8 * 1024 * 1024 else { return }
                let result = await Task.detached(priority: .utility) { () -> (CGImage?, ArtworkPalette?) in
                    let image = ArtworkPalette.thumbnail(data); return (image, image.flatMap(ArtworkPalette.extract))
                }.value
                try Task.checkCancellation(); guard self?.generation == token else { return }
                if let image = result.0 {
                    self?.artwork = image; self?.artworkPalette = result.1
                    self?.updateNowPlaying()
                }
            } catch { /* Keep the selected theme and the normal artwork placeholder. */ }
        }
    }
    private func activateAudio() async throws {
        #if os(iOS) || os(tvOS)
        try AVAudioSession.sharedInstance().setCategory(.playback, mode: .default)
        let activated = try await AVAudioSession.sharedInstance().activate(options: [])
        guard activated else { throw PlayerError.badResponse }
        #endif
    }
    private func failPlayback() {
        pause(); cancelPrepared(); loading = false
        error = "Dieser Titel konnte nicht abgespielt werden. Bitte Verbindung und Audioformat prüfen. Erneut versuchen startet nur diesen Titel."
    }
    private func removeItemObservers() {
        removeFadeObserver()
        if let observer { audio.removeTimeObserver(observer) }; observer = nil
        statusObserver?.invalidate(); statusObserver = nil; timeObserver?.invalidate(); timeObserver = nil
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        if let failureObserver { NotificationCenter.default.removeObserver(failureObserver) }
        endObserver = nil; failureObserver = nil
    }
    private func removeFadeObserver() {
        if let fadeObserver { audio.removeTimeObserver(fadeObserver) }; fadeObserver = nil
    }
    private func setupCommands() {
        let center = MPRemoteCommandCenter.shared()
        for (command, action) in [(center.playCommand, 0), (center.pauseCommand, 1), (center.togglePlayPauseCommand, 2), (center.nextTrackCommand, 3), (center.previousTrackCommand, 4)] {
            let token = command.addTarget { [weak self] _ in
                Task { @MainActor in
                    guard let self else { return }
                    // SwiftUI owns foreground TV remote presses. Let the media
                    // command center handle background presses without toggling twice.
                    #if os(tvOS)
                    if action == 2 && UIApplication.shared.applicationState == .active { return }
                    #endif
                    switch action { case 0: self.resume(); case 1: self.pause(); case 2: self.toggle(); case 3: self.next(); default: self.previous() }
                }
                return .success
            }
            commands.append((command, token))
        }
        let token = center.changePlaybackPositionCommand.addTarget { [weak self] event in
            guard let event = event as? MPChangePlaybackPositionCommandEvent else { return .commandFailed }
            let position = event.positionTime
            Task { @MainActor in self?.seek(position) }
            return .success
        }
        commands.append((center.changePlaybackPositionCommand, token))
    }
    private func updateNowPlaying() {
        lastNowPlaying = Date()
        guard let current else { return }
        onSnapshot?(queue, position, repeatOne ? "track" : repeatAll ? "queue" : "off", !isPlaying)
        var info: [String: Any] = [
            MPMediaItemPropertyTitle: current.title, MPMediaItemPropertyArtist: current.artistText,
            MPMediaItemPropertyAlbumTitle: current.album, MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: isPlaying ? playbackRate : 0.0,
        ]
        if let systemArtwork { info[MPMediaItemPropertyArtwork] = systemArtwork }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = info
        #if os(macOS)
        MPNowPlayingInfoCenter.default().playbackState = isPlaying ? .playing : .paused
        #endif
    }
    nonisolated private static func nowPlayingArtwork(_ image: CGImage) -> MPMediaItemArtwork {
        // The system can request this image outside the main actor. Capture only
        // immutable CGImage data, never the player or a UI-owned image instance.
        MPMediaItemArtwork(boundsSize: CGSize(width: image.width, height: image.height)) { size in
            #if os(macOS)
            NSImage(cgImage: image, size: size)
            #else
            UIImage(cgImage: image)
            #endif
        }
    }
}
