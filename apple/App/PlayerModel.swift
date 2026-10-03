import AVFoundation
import Foundation
import MediaPlayer
import Observation
import YTMDLCore
#if os(tvOS)
import UIKit
#endif

@MainActor @Observable final class PlayerModel {
    private(set) var queue = PlaybackQueue()
    private(set) var isPlaying = false
    private(set) var loading = false
    private(set) var position: Double = 0
    var repeatAll = false
    var error: String?
    var lyrics = ""
    var current: Track? { queue.current }
    var duration: Double { current?.duration ?? 0 }
    @ObservationIgnored private let audio = AVPlayer()
    @ObservationIgnored private var client: APIClient?
    @ObservationIgnored private var loadTask: Task<Void, Never>?
    @ObservationIgnored private var lyricsTask: Task<Void, Never>?
    @ObservationIgnored private var observer: Any?
    @ObservationIgnored private var endObserver: NSObjectProtocol?
    @ObservationIgnored private var failureObserver: NSObjectProtocol?
    @ObservationIgnored private var interruptionObserver: NSObjectProtocol?
    @ObservationIgnored private var routeObserver: NSObjectProtocol?
    @ObservationIgnored private var statusObserver: NSKeyValueObservation?
    @ObservationIgnored private var generation = UUID()
    @ObservationIgnored private var wantsPlayback = false
    @ObservationIgnored private var commands: [(MPRemoteCommand, Any)] = []

    init() {
        audio.automaticallyWaitsToMinimizeStalling = true
        observer = audio.addPeriodicTimeObserver(forInterval: CMTime(seconds: 0.5, preferredTimescale: 600), queue: .main) { [weak self] time in
            Task { @MainActor in
                guard let self else { return }
                let value = time.seconds
                if value.isFinite { self.position = max(0, value) }
                self.isPlaying = self.audio.rate > 0
                self.updateNowPlaying()
            }
        }
        setupCommands()
        #if os(iOS) || os(tvOS)
        interruptionObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.didBecomeInactiveNotification, object: nil, queue: .main) { [weak self] _ in
            Task { @MainActor in self?.pause() }
        }
        routeObserver = NotificationCenter.default.addObserver(forName: AVAudioSession.routeChangeNotification, object: nil, queue: .main) { [weak self] notification in
            let reason = notification.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
            if reason == AVAudioSession.RouteChangeReason.oldDeviceUnavailable.rawValue { Task { @MainActor in self?.pause() } }
        }
        #endif
    }
    func play(_ tracks: [Track], start: Int = 0, client: APIClient) {
        self.client = client; queue.replace(tracks, start: start); loadCurrent()
    }
    func append(_ track: Track, client: APIClient) { self.client = client; queue.append(track) }
    func select(_ index: Int) { queue.select(index); loadCurrent() }
    func shuffle() { queue.shuffleUpcoming() }
    func next() {
        if queue.next(repeatAll: repeatAll) { loadCurrent() } else { pause() }
    }
    func previous() {
        if position > 3 { seek(0) } else { queue.previous(); loadCurrent() }
    }
    func toggle() { wantsPlayback ? pause() : resume() }
    func pause() { wantsPlayback = false; audio.pause(); isPlaying = false; updateNowPlaying() }
    func resume() {
        guard current != nil else { return }
        wantsPlayback = true
        if error != nil || (audio.currentItem == nil && !loading) { loadCurrent(); return }
        guard !loading else { return }
        let generation = generation
        loadTask = Task { [weak self] in
            guard let self else { return }
            do {
                try await activateAudio()
                guard self.generation == generation, self.wantsPlayback else { return }
                audio.play(); isPlaying = true; updateNowPlaying()
            } catch { if self.generation == generation { self.error = "Die Audioausgabe konnte nicht aktiviert werden." } }
        }
    }
    func seek(_ seconds: Double) {
        guard seconds.isFinite, current != nil else { return }
        let target = min(max(0, seconds), max(0, duration))
        audio.seek(to: CMTime(seconds: target, preferredTimescale: 600), toleranceBefore: .zero, toleranceAfter: .zero)
        position = target; updateNowPlaying()
    }
    func stop() {
        generation = UUID(); loadTask?.cancel(); lyricsTask?.cancel(); removeItemObservers()
        pause(); audio.replaceCurrentItem(with: nil); queue.replace([]); position = 0; loading = false
        error = nil; lyrics = ""; client = nil; MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
        #if os(iOS) || os(tvOS)
        AVAudioSession.sharedInstance().deactivate(options: .notifyOthersOnDeactivation) { _, _ in }
        #endif
    }
    private func loadCurrent() {
        loadTask?.cancel(); lyricsTask?.cancel(); removeItemObservers()
        generation = UUID(); let generation = generation
        audio.pause(); audio.replaceCurrentItem(with: nil); isPlaying = false; position = 0; error = nil; lyrics = ""
        guard let current, let client else { loading = false; return }
        loading = true; wantsPlayback = true
        loadTask = Task { [weak self] in
            guard let self else { return }
            do {
                let path = try client.server.itemPath("tracks", id: current.id, suffix: "/stream")
                let asset = AVURLAsset(url: try client.server.endpoint(path), options: [AVURLAssetHTTPCookiesKey: client.authenticationCookies])
                let playable = try await asset.load(.isPlayable)
                try Task.checkCancellation()
                guard self.generation == generation else { return }
                guard playable else { self.failPlayback(); return }
                let item = AVPlayerItem(asset: asset)
                self.audio.replaceCurrentItem(with: item)
                self.statusObserver = item.observe(\.status, options: [.initial, .new]) { [weak self] item, _ in
                    let failed = item.status == .failed
                    if failed { Task { @MainActor in guard self?.generation == generation else { return }; self?.failPlayback() } }
                }
                self.endObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.didPlayToEndTimeNotification, object: item, queue: .main) { [weak self] _ in
                    Task { @MainActor in guard self?.generation == generation else { return }; self?.next() }
                }
                self.failureObserver = NotificationCenter.default.addObserver(forName: AVPlayerItem.failedToPlayToEndTimeNotification, object: item, queue: .main) { [weak self] _ in
                    Task { @MainActor in guard self?.generation == generation else { return }; self?.failPlayback() }
                }
                if self.wantsPlayback { try await self.activateAudio() }
                guard self.generation == generation else { return }
                self.loading = false
                if self.wantsPlayback { self.audio.play() }
                self.updateNowPlaying()
            } catch is CancellationError { }
            catch {
                guard self.generation == generation else { return }
                self.failPlayback()
            }
        }
        lyricsTask = Task { [weak self] in
            do {
                let lyrics: Lyrics = try await client.get(client.server.itemPath("tracks", id: current.id, suffix: "/lyrics"))
                guard self?.generation == generation else { return }
                self?.lyrics = lyrics.content ?? "Für diesen Titel sind keine Lyrics gespeichert."
            } catch {
                guard self?.generation == generation else { return }
                self?.lyrics = "Lyrics konnten nicht geladen werden."
            }
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
        pause(); loading = false
        error = "Dieser Titel konnte nicht abgespielt werden. Bitte Verbindung und Audioformat prüfen. Opus in Ogg/WebM kann auf Apple-Geräten eine kompatible Wiedergabevariante benötigen."
    }
    private func removeItemObservers() {
        statusObserver?.invalidate(); statusObserver = nil
        if let endObserver { NotificationCenter.default.removeObserver(endObserver) }
        if let failureObserver { NotificationCenter.default.removeObserver(failureObserver) }
        endObserver = nil; failureObserver = nil
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
        guard let current else { return }
        MPNowPlayingInfoCenter.default().nowPlayingInfo = [
            MPMediaItemPropertyTitle: current.title, MPMediaItemPropertyArtist: current.artistText,
            MPMediaItemPropertyAlbumTitle: current.album, MPMediaItemPropertyPlaybackDuration: duration,
            MPNowPlayingInfoPropertyElapsedPlaybackTime: position,
            MPNowPlayingInfoPropertyPlaybackRate: isPlaying ? 1.0 : 0.0,
        ]
        #if os(macOS)
        MPNowPlayingInfoCenter.default().playbackState = isPlaying ? .playing : .paused
        #endif
    }
}
