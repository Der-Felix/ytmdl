import Foundation
import MediaPlayer
import AVFoundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

private struct OfflineTestManifest: Encodable {
    let profiles: [OfflineProfile]
    let tracks: [OfflineTrack]
}
private func fixtureUser(_ id: String) throws -> User {
    try JSONDecoder().decode(User.self, from: Data("{\"id\":\"\(id)\",\"username\":\"fixture\",\"displayName\":\"Fixture\",\"role\":\"user\"}".utf8))
}
@MainActor @Test func offlineRestartAccountIsolationAndLocalRemoval() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let server = try ServerAddress("https://fixture.example")
    let first = try fixtureUser("first"), second = try fixtureUser("second")
    let a = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: first.id), origin: server.url.absoluteString, user: first)
    let b = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: second.id), origin: server.url.absoluteString, user: second)
    #expect(a.id != b.id)
    let track = Track(id: "same-song", title: "Fixture", artists: ["Artist"], album: "Album", durationMs: 1000)
    let id = OfflineLibrary.digest(a.id + "\n" + track.id)
    let file = id + ".ogg"
    let directory = root.appendingPathComponent(a.id)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    let bytes = Data("OggSfixture-media".utf8)
    try bytes.write(to: directory.appendingPathComponent(file))
    let record = OfflineTrack(id: id, scope: a.id, track: track, state: .ready, bytes: Int64(bytes.count), fileName: file)
    let manifest = OfflineTestManifest(profiles: [a, b], tracks: [record])
    try JSONEncoder().encode(manifest).write(to: root.appendingPathComponent("manifest.json"))
    let store = OfflineLibrary(root: root, startTransfers: false)
    store.selectProfile(a)
    #expect(store.audioURL(track.id) != nil)
    #expect(store.readyTracks == [track])
    store.selectProfile(b)
    #expect(store.audioURL(track.id) == nil)
    #expect(store.readyTracks.isEmpty)
    #expect(store.availableTracks(in: [record]).isEmpty)
    store.selectProfile(a); store.detach()
    #expect(store.readyTracks.isEmpty)
    let restart = OfflineLibrary(root: root, startTransfers: false)
    restart.selectProfile(a)
    #expect(restart.audioURL(track.id) != nil)
    restart.remove(track.id)
    #expect(!FileManager.default.fileExists(atPath: directory.appendingPathComponent(file).path))
    let afterDelete = OfflineLibrary(root: root, startTransfers: false); afterDelete.selectProfile(a)
    #expect(afterDelete.readyTracks.isEmpty)
}

@MainActor @Test func offlineRejectsErrorPagesPartialFilesAndSymlinks() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let html = root.appendingPathComponent("html")
    try Data("<html>login</html>".utf8).write(to: html)
    #expect(throws: OfflineError.self) { try OfflineLibrary.mediaExtension(html, mime: "audio/mpeg") }
    #expect(throws: OfflineError.self) { try OfflineLibrary.mediaExtension(html, mime: "text/html") }
    let valid = root.appendingPathComponent("valid")
    try Data("OggSfixture-media".utf8).write(to: valid)
    #expect(try OfflineLibrary.mediaExtension(valid, mime: "audio/ogg") == "ogg")
    #expect(throws: OfflineError.self) { try OfflineLibrary.mediaExtension(valid, mime: "video/ogg") }
    let server = try ServerAddress("https://fixture.example"), user = try fixtureUser("first")
    let profile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: user.id), origin: server.url.absoluteString, user: user)
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 1000)
    let id = OfflineLibrary.digest(profile.id + "\n" + track.id)
    let directory = root.appendingPathComponent(profile.id)
    try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
    try FileManager.default.createSymbolicLink(at: directory.appendingPathComponent(id + ".ogg"), withDestinationURL: valid)
    let record = OfflineTrack(id: id, scope: profile.id, track: track, state: .ready, bytes: 16, fileName: id + ".ogg")
    try JSONEncoder().encode(OfflineTestManifest(profiles: [profile], tracks: [record])).write(to: root.appendingPathComponent("manifest.json"))
    let store = OfflineLibrary(root: root, startTransfers: false); store.selectProfile(profile)
    #expect(store.audioURL(track.id) == nil)
    #expect(store.readyTracks.isEmpty)
}

@MainActor @Test func offlineImportValidatesOriginBudgetAndPublishesAtomically() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let server = try ServerAddress("https://fixture.example"), user = try fixtureUser("first")
    let profile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: user.id), origin: server.url.absoluteString, user: user)
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 1000)
    let id = OfflineLibrary.digest(profile.id + "\n" + track.id)
    let record = OfflineTrack(id: id, scope: profile.id, track: track, state: .downloading, transferID: 42)
    try JSONEncoder().encode(OfflineTestManifest(profiles: [profile], tracks: [record])).write(to: root.appendingPathComponent("manifest.json"))
    let store = OfflineLibrary(root: root, startTransfers: false); store.selectProfile(profile)
    let temp = root.appendingPathComponent("transfer")
    try Data("OggSfixture-media".utf8).write(to: temp)
    let bad = HTTPURLResponse(url: URL(string: "https://other.example/media")!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "audio/ogg"])!
    store.finish(id, location: temp, response: bad)
    #expect(store.entry(track.id)?.state == .failed)
    #expect(store.audioURL(track.id) == nil)
    #expect(!FileManager.default.fileExists(atPath: temp.path))
    // A cold start imports a completed file using only its saved profile; it
    // must not need a keychain cookie or make a request to another host.
    try JSONEncoder().encode(OfflineTestManifest(profiles: [profile], tracks: [record])).write(to: root.appendingPathComponent("manifest.json"))
    let restart = OfflineLibrary(root: root, startTransfers: false); restart.selectProfile(profile)
    try Data("OggSfixture-media".utf8).write(to: temp)
    let good = HTTPURLResponse(url: URL(string: "https://fixture.example/media")!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "audio/ogg"])!
    // A cancelled earlier transfer must not publish over a newer attempt.
    restart.finish(id, location: temp, response: good, taskID: 999)
    #expect(restart.entry(track.id)?.state == .downloading)
    #expect(restart.audioURL(track.id) == nil)
    #expect(!FileManager.default.fileExists(atPath: temp.path))
    try Data("OggSfixture-media".utf8).write(to: temp)
    restart.finish(id, location: temp, response: good, taskID: 42)
    #expect(restart.entry(track.id)?.state == .ready)
    #expect(restart.audioURL(track.id) != nil)
    let after = OfflineLibrary(root: root, startTransfers: false); after.selectProfile(profile)
    #expect(after.readyTracks == [track])
}

@MainActor @Test(.enabled(if: ProcessInfo.processInfo.environment["YTMDL_OFFLINE_FIXTURE_URL"] != nil))
func offlineAuthenticatedTransferCachesSidecarsAndPlayerUsesLocalFile() async throws {
    let url = try #require(ProcessInfo.processInfo.environment["YTMDL_OFFLINE_FIXTURE_URL"])
    #expect(URL(string: url)?.host == "127.0.0.1")
    guard URL(string: url)?.host == "127.0.0.1" else { return }
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let suite = "offline-transfer-" + UUID().uuidString
    let preferences = try #require(UserDefaults(suiteName: suite))
    preferences.set(false, forKey: "offlineWiFiOnly")
    let store = OfflineLibrary(root: root, preferences: preferences)
    let client = try APIClient(server: ServerAddress(url, allowLocalHTTP: true), persist: false)
    defer { store.closeTransfers(); client.invalidate(); preferences.removePersistentDomain(forName: suite); try? FileManager.default.removeItem(at: root) }
    let status: AuthStatus = try await client.get("/auth/status")
    let user = try #require(status.user)
    store.configure(client: client, user: user)
    let track = Track(id: "t0", title: "Fixture", artists: ["Nordlicht"], album: "Album", durationMs: 30000, codec: "pcm_s16le")
    store.enqueue([track])
    for _ in 0..<150 {
        if store.entry(track.id)?.state == .ready && store.artworkURL(track.id) != nil && store.lyrics(track.id) != nil { break }
        try await Task.sleep(for: .milliseconds(100))
    }
    #expect(store.entry(track.id)?.state == .ready)
    #expect(store.audioURL(track.id)?.pathExtension == "wav")
    #expect(store.artworkURL(track.id) != nil)
    #expect(store.lyrics(track.id)?.contains("Stadt") == true)
    // Inspect the source without starting AVPlayer or activating an audio session.
    let player = PlayerModel(volumePreferences: preferences)
    player.offlineLibrary = store; player.offlineOnly = true
    let item = try player.makeItem(track, client: client)
    #expect((item.asset as? AVURLAsset)?.url.isFileURL == true)
    let missing = Track(id: "missing", title: "Missing", artists: [], album: "", durationMs: 1000)
    #expect(throws: OfflineError.self) { try player.makeItem(missing, client: client) }
    store.detach()
    store.selectProfile(try #require(store.profiles.first))
    #expect(store.audioURL(track.id) != nil)
    player.previewPaused([track], client: client)
    #expect(player.artwork != nil)
    #expect(MPNowPlayingInfoCenter.default().nowPlayingInfo?[MPMediaItemPropertyArtwork] is MPMediaItemArtwork)
    #expect(MPNowPlayingInfoCenter.default().nowPlayingInfo?[MPMediaItemPropertyTitle] as? String == track.title)
    player.stop()
}

@MainActor @Test func listeningQueueAndPendingEventsRemainAccountScoped() throws {
    let suite = "history-" + UUID().uuidString
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let server = try ServerAddress("https://fixture.example")
    let history = ListeningHistory(preferences: preferences)
    history.configure(server: server, userID: "first", persist: true)
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 10000)
    var queue = PlaybackQueue(); queue.replace([track])
    history.saveSnapshot(queue: queue, position: 3, repeatMode: "track", force: true)
    history.enqueueEvent(track)
    let event = try #require(history.pendingEvents.first)
    history.configure(server: server, userID: "second", persist: true)
    #expect(history.snapshot == nil)
    #expect(history.pendingEvents.isEmpty)
    history.configure(server: server, userID: "first", persist: true)
    #expect(history.snapshot?.position == 3)
    #expect(history.snapshot?.repeatMode == "track")
    #expect(history.pendingEvents.first?.eventID == event.eventID)
    history.acknowledgeEvent(event.eventID)
    #expect(history.pendingEvents.isEmpty)
    history.setEnabled(false)
    #expect(history.snapshot == nil)
    history.enqueueEvent(track)
    #expect(history.pendingEvents.isEmpty)
}
