import AVFoundation
import Foundation
import Synchronization
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

@Test func equalizerNeutralBypassFrequencyResponseAndChannelIsolation() {
    let parameters = EqualizerParameters(), kernel = EqualizerKernel()
    kernel.prepare(rate: 48000, channels: 2)
    parameters.publish(gains: Array(repeating: 0, count: 10), enabled: true, preamp: 0, headroom: false)
    kernel.refresh(parameters)
    for index in 0..<5000 {
        let value = 0.1 * sin(2 * Double.pi * Double(index) / 48)
        #expect(abs(kernel.sample(value, channel: 0) - value) < 1e-8)
        #expect(kernel.sample(0, channel: 1) == 0)
    }
    var gains = Array(repeating: 0.0, count: 10); gains[5] = 6
    parameters.publish(gains: gains, enabled: true, preamp: 0, headroom: false); kernel.refresh(parameters); kernel.reset()
    var inputEnergy = 0.0, outputEnergy = 0.0
    for index in 0..<10000 {
        let value = 0.1 * sin(2 * Double.pi * Double(index) / 48), output = kernel.sample(value, channel: 0)
        if index > 4000 { inputEnergy += value * value; outputEnergy += output * output }
    }
    #expect(abs(10 * log10(outputEnergy / inputEnergy) - 6) < 0.1)
    parameters.publish(gains: gains, enabled: false, preamp: 6, headroom: false); kernel.refresh(parameters)
    #expect(kernel.sample(0.2, channel: 0) == 0.2)
    parameters.publish(gains: gains, enabled: true, preamp: 6, headroom: true); kernel.refresh(parameters)
    var peak = 0.0
    for index in 0..<48000 {
        let output = kernel.sample(0.95 * sin(2 * Double.pi * Double(index) / 48), channel: 0)
        #expect(output.isFinite); peak = max(peak, abs(output))
    }
    #expect(peak <= 1)
    kernel.prepare(rate: 8000, channels: 1)
    gains = Array(repeating: 0, count: 10); gains[9] = 12
    parameters.publish(gains: gains, enabled: true, preamp: 0, headroom: false); kernel.refresh(parameters)
    for index in 0..<1000 {
        let input = 0.1 * sin(2 * Double.pi * Double(index) / 8)
        #expect(abs(kernel.sample(input, channel: 0) - input) < 1e-8)
    }
}

@MainActor @Test func equalizerPreferencesClampAndRestoreWithoutAffectingBypass() throws {
    let suite = "org.ytmdl.tests.eq.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let eq = EqualizerModel(preferences: defaults)
    #expect(!eq.enabled)
    eq.select(.bass); eq.setGain(99, band: 5); eq.setPreamp(-100); eq.setEnabled(true); eq.setHeadroom(false)
    let restored = EqualizerModel(preferences: defaults)
    #expect(restored.gains[5] == 12 && restored.preamp == -12 && restored.enabled && !restored.headroom)
    restored.setGain(.nan, band: 5); restored.setPreamp(.infinity)
    #expect(restored.gains[5] == 12 && restored.preamp == -12)
    restored.setEnabled(false); #expect(restored.gains[0] == 5)
}

private func syntheticWave(in folder: URL, seconds: Double = 6) throws -> URL {
    let url = folder.appendingPathComponent("fixture.wav")
    let format = AVAudioFormat(standardFormatWithSampleRate: 48000, channels: 1)!
    let file = try AVAudioFile(forWriting: url, settings: format.settings)
    let count = AVAudioFrameCount(seconds * 48000)
    let buffer = AVAudioPCMBuffer(pcmFormat: format, frameCapacity: count)!
    buffer.frameLength = count
    for index in 0..<Int(count) { buffer.floatChannelData![0][index] = Float(0.1 * sin(2 * Double.pi * Double(index) / 48)) }
    try file.write(from: buffer)
    return url
}
@MainActor private func fixtureClient() throws -> APIClient {
    let configuration = URLSessionConfiguration.ephemeral; configuration.protocolClasses = [FixtureProtocol.self]
    return try APIClient(server: ServerAddress("https://fixture.example"), persist: false, configuration: configuration)
}
@MainActor private func waitUntil(_ predicate: () -> Bool, timeout: Double = 8) async throws {
    let deadline = ContinuousClock.now.advanced(by: .seconds(timeout))
    while !predicate() && ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(20)) }
    #expect(predicate())
}

@MainActor @Test func nativeAudioTapActuallyChangesDecodedSamples() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder), suite = "org.ytmdl.tests.tap.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let audio = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: audio, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0.001)
    player.equalizer.setHeadroom(false); player.equalizer.setGain(6, band: 5)
    player.play([Track(id: "one", title: "Fixture", artists: [], album: "", durationMs: 6000)], client: client)
    try await waitUntil { player.isPlaying }
    #expect(audio.currentItem?.audioMix == nil)
    player.equalizer.setEnabled(true)
    try await waitUntil { player.position > 0.5 && player.equalizer.parameters.frames.load(ordering: .relaxed) > 4096 }
    let parameters = player.equalizer.parameters
    let input = Double(bitPattern: parameters.inputEnergy.load(ordering: .relaxed)), output = Double(bitPattern: parameters.outputEnergy.load(ordering: .relaxed))
    #expect(input > 0)
    #expect(10 * log10(output / max(1e-20, input)) > 5.5)
    #expect(10 * log10(output / max(1e-20, input)) < 6.5)
    #expect(player.equalizerFormat == 1)
    player.equalizer.setEnabled(false); #expect(audio.currentItem?.audioMix == nil)
    player.setPlaybackRate(1.5); #expect(abs(audio.rate - 1.5) < 0.01)
    player.pause(); #expect(audio.rate == 0 && !player.isPlaying && !player.isPlaybackRequested)
}

@MainActor @Test func crossfadeUsesBothPlayersAndPauseSeekStopCancelIncomingAudio() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder), suite = "org.ytmdl.tests.crossfade.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let a = AVPlayer(), b = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: a, standby: b, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0.001); player.setCrossfade(2)
    let first = Track(id: "a", title: "A", artists: ["Fixture"], album: "A", durationMs: 6000)
    let second = Track(id: "b", title: "B", artists: ["Fixture"], album: "B", durationMs: 6000)
    player.play([first, second], client: client)
    try await waitUntil { player.isPlaying && b.currentItem?.status == .readyToPlay }
    player.seek(4.2)
    try await waitUntil { player.isCrossfading && a.volume < 0.00095 && b.volume > 0.00005 }
    #expect(abs(a.volume + b.volume - 0.001) < 0.00001)
    // Simulate the outgoing player waiting: the incoming title must not run ahead.
    a.pause(); try await waitUntil { b.rate == 0 && player.loading }
    a.playImmediately(atRate: 1); try await waitUntil { b.rate > 0 && player.isPlaying }
    player.pause(); #expect(a.rate == 0 && b.rate == 0)
    player.resume(); try await waitUntil { a.rate > 0 && b.rate > 0 }
    player.seek(1)
    #expect(!player.isCrossfading && b.currentItem == nil && abs(a.volume - 0.001) < 0.00001)
    try await waitUntil { b.currentItem?.status == .readyToPlay }
    player.next(); #expect(player.current?.id == "b" && a.currentItem == nil)
    try await waitUntil { player.isPlaying }
    player.stop(); #expect(a.rate == 0 && b.rate == 0 && a.currentItem == nil && b.currentItem == nil)
}

@MainActor @Test func albumProtectionAndEndOfTrackTimerPreventOverlapAndAutoplay() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder, seconds: 3), suite = "org.ytmdl.tests.boundary.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let a = AVPlayer(), b = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: a, standby: b, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0); player.setCrossfade(2)
    let tracks = ["a", "b"].map { Track(id: $0, title: $0, artists: ["Fixture"], album: "Same album", durationMs: 3000) }
    player.play(tracks, client: client)
    try await waitUntil { player.isPlaying && b.currentItem?.status == .readyToPlay }
    player.seek(2); try await Task.sleep(for: .milliseconds(200))
    #expect(!player.isCrossfading && b.rate == 0)
    player.setSleepMode(.endOfTrack)
    #expect(b.currentItem == nil)
    try await waitUntil { !player.isPlaybackRequested }
    #expect(player.current?.id == "a" && player.sleepMode == .off && a.rate == 0 && b.rate == 0)
    player.resume(); try await waitUntil { player.isPlaying && player.position > 0.05 }
    #expect(player.current?.id == "a")
}

@Test func artworkPaletteUsesActualColorsAndFallsBackForPlainOrInvalidImages() throws {
    func image(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat) throws -> CGImage {
        let context = try #require(CGContext(data: nil, width: 32, height: 32, bitsPerComponent: 8, bytesPerRow: 128,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue))
        context.setFillColor(red: red, green: green, blue: blue, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: 32, height: 32))
        return try #require(context.makeImage())
    }
    let blue = try #require(ArtworkPalette.extract(image(0.1, 0.3, 0.8)))
    let green = try #require(ArtworkPalette.extract(image(0.1, 0.8, 0.3)))
    #expect(ArtworkColor(red: 1, green: 1, blue: 0.6).button.luminance <= 0.18)
    #expect(blue.accent.blue > 0.7 && blue.accent.red < 0.2)
    #expect(green.accent.green > 0.7 && green != blue)
    #expect(try ArtworkPalette.extract(image(1, 1, 1)) == nil)
    #expect(try ArtworkPalette.extract(image(0.5, 0.5, 0.5)) == nil)
    #expect(ArtworkPalette.thumbnail(Data([1, 2, 3])) == nil)
}

@MainActor @Test func immediateStopAndInvalidationDoNotStartLateMetadataRequests() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder)
    let suite = "org.ytmdl.tests.stop.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let client = try fixtureClient(), audio = AVPlayer(), standby = AVPlayer()
    let player = PlayerModel(volumePreferences: defaults, audio: audio, standby: standby, itemFactory: { _, _ in AVPlayerItem(url: url) })
    player.setVolume(0)
    player.play([Track(id: "one", title: "Fixture", artists: [], album: "", durationMs: 6000)], client: client)
    player.stop(); client.invalidate()
    try await Task.sleep(for: .milliseconds(200))
    #expect(player.current == nil && audio.currentItem == nil && standby.currentItem == nil && player.artwork == nil)
    do { let _: AuthStatus = try await client.get("/auth/status"); Issue.record("Invalidated client accepted a request") }
    catch { #expect(error is CancellationError) }
}

@MainActor @Test func endOfAlbumStopsAtQueueBoundaryEvenWithRepeatEnabled() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder, seconds: 2)
    let suite = "org.ytmdl.tests.album.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let a = AVPlayer(), b = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: a, standby: b, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0); player.repeatAll = true; player.setSleepMode(.endOfAlbum)
    player.play([Track(id: "one", title: "Fixture", artists: ["Fixture"], album: "Album", durationMs: 2000)], client: client)
    try await waitUntil { player.isPlaying }
    player.seek(1.7)
    try await waitUntil { !player.isPlaybackRequested }
    #expect(player.current?.id == "one" && player.sleepMode == .off && b.currentItem == nil && a.rate == 0)
}

@MainActor @Test func naturalCrossfadePromotesIncomingPositionAndRepeatOneRestartsCurrentTrack() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder, seconds: 3)
    let suite = "org.ytmdl.tests.natural.\(UUID().uuidString)", defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let a = AVPlayer(), b = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: a, standby: b, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0); player.setCrossfade(1)
    let tracks = ["a", "b"].map { Track(id: $0, title: $0, artists: ["Fixture"], album: $0, durationMs: 3000) }
    player.play(tracks, client: client)
    try await waitUntil { player.isPlaying && b.currentItem?.status == .readyToPlay }
    player.seek(1.9)
    try await waitUntil { player.isCrossfading }
    try await waitUntil { player.current?.id == "b" }
    #expect(player.position > 0.3 && a.currentItem == nil && b.rate > 0 && !player.isCrossfading)
    player.setRepeatOne(true); player.seek(2.6)
    try await waitUntil { player.position < 0.5 && player.isPlaying }
    #expect(player.current?.id == "b" && a.currentItem == nil)
}

@MainActor @Test func visualizationMetersActualAudioAndBypassesDisabledEqualizer() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder), suite = "org.ytmdl.tests.meter.\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let audio = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: audio, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0.001)
    player.equalizer.select(.bass)
    player.play([Track(id: "one", title: "Fixture", artists: [], album: "", durationMs: 6000)], client: client)
    try await waitUntil { player.isPlaying }
    #expect(audio.currentItem?.audioMix == nil)
    player.setVisualization(true)
    try await waitUntil { player.position > 0.5 && player.levelHistory.contains { $0 > 0.05 } }
    let params = player.equalizer.parameters
    let rms = Double(bitPattern: params.meterRMS.load(ordering: .relaxed))
    #expect(abs(rms - 0.1 * player.volume / sqrt(2)) < 0.000003)
    let input = Double(bitPattern: params.inputEnergy.load(ordering: .relaxed))
    let output = Double(bitPattern: params.outputEnergy.load(ordering: .relaxed))
    #expect(input == output && !player.equalizer.enabled)
    player.pause()
    try await Task.sleep(for: .milliseconds(300))
    #expect(player.levelHistory.last == 0)
    let liveMix = audio.currentItem?.audioMix
    player.equalizer.setEnabled(true)
    player.setVisualization(false)
    #expect(audio.currentItem?.audioMix === liveMix && player.levelHistory.isEmpty)
    player.equalizer.setEnabled(false)
    #expect(audio.currentItem?.audioMix == nil)
    #expect(!PlayerModel(volumePreferences: defaults).visualizationEnabled)
}

@MainActor @Test func queueEditsReplacePrefetchWithoutInterruptingCurrentAudio() async throws {
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
    defer { try? FileManager.default.removeItem(at: folder) }
    let url = try syntheticWave(in: folder, seconds: 10), suite = "org.ytmdl.tests.queue-edit.\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    let audio = AVPlayer(), standby = AVPlayer(), client = try fixtureClient()
    let player = PlayerModel(volumePreferences: defaults, audio: audio, standby: standby, itemFactory: { _, _ in AVPlayerItem(url: url) })
    defer { player.stop(); client.invalidate() }
    player.setVolume(0.001)
    let tracks = ["a", "b", "c"].map { Track(id: $0, title: $0, artists: [], album: "", durationMs: 10000) }
    player.play(tracks, client: client)
    try await waitUntil { player.isPlaying && player.preparedTrackID == "b" }
    let currentItem = audio.currentItem
    player.playNextInQueue(2)
    #expect(player.preparedTrackID == "c" && player.current?.id == "a" && audio.currentItem === currentItem)
    player.removeFromQueue(1)
    #expect(player.preparedTrackID == "b" && player.current?.id == "a" && audio.currentItem === currentItem)
    player.removeFromQueue(0)
    #expect(player.queue.tracks.count == 2)
    player.clearUpcoming()
    #expect(player.queue.tracks == [tracks[0]] && standby.currentItem == nil && player.preparedTrackID == nil)
    #expect(audio.currentItem === currentItem && player.isPlaying)
}

@MainActor @Test func libraryDurationWinsOverInflatedStreamEstimateAndUnknownDurationFallsBack() {
    // An Opus stream can report 278 minutes for a track whose library says 2:49.
    #expect(PlayerModel.resolvedDuration(metadata: 169, stream: 16680) == 169)
    #expect(PlayerModel.resolvedDuration(metadata: 169, stream: .nan) == 169)
    #expect(PlayerModel.resolvedDuration(metadata: 0, stream: 169) == 169)
    #expect(PlayerModel.resolvedDuration(metadata: -1, stream: 169) == 169)
    #expect(PlayerModel.resolvedDuration(metadata: .infinity, stream: 169) == 169)
    #expect(PlayerModel.resolvedDuration(metadata: 0, stream: .infinity) == 0)
    #expect(PlayerModel.resolvedDuration(metadata: 0, stream: -1) == 0)
}
