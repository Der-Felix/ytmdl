import Foundation
import MediaPlayer
import AVFoundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

private struct OfflineTestManifest: Encodable {
    let profiles: [OfflineProfile]
    let tracks: [OfflineTrack]
    var collections: [OfflineCollection]? = nil
}
private func fixtureUser(_ id: String) throws -> User {
    try JSONDecoder().decode(User.self, from: Data("{\"id\":\"\(id)\",\"username\":\"fixture\",\"displayName\":\"Fixture\",\"role\":\"user\"}".utf8))
}
@MainActor @Test func offlineCollectionsSurviveRestartWithOriginalOrderAndSharedTracks() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let server = try ServerAddress("https://fixture.example"), user = try fixtureUser("first"), other = try fixtureUser("second")
    let profile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: user.id), origin: server.url.absoluteString, user: user)
    let otherProfile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: other.id), origin: server.url.absoluteString, user: other)
    let entries = [
        Track(id: "a", title: "Track 10", artists: ["Zulu"], album: "A", durationMs: 1000),
        Track(id: "b", title: "Track 2", artists: ["Alpha"], album: "B", durationMs: 1000),
        Track(id: "c", title: "Track 1", artists: ["Alpha"], album: "A", durationMs: 1000)
    ].enumerated().map { index, track in
        OfflineTrack(id: OfflineLibrary.digest(profile.id + "\n" + track.id), scope: profile.id,
                     track: track, state: .paused, date: Date(timeIntervalSince1970: Double(index)))
    }
    let playlist = OfflineCollection(id: "playlist", scope: profile.id, kind: "playlist", sourceID: "p1", name: "Road trip", trackIDs: ["b", "a", "b", "missing"], keepUpdated: true)
    let favorites = OfflineCollection(id: "favorites", scope: profile.id, kind: "favorites", sourceID: "", name: "Favoriten", trackIDs: ["a", "c"], keepUpdated: false)
    try JSONEncoder().encode(OfflineTestManifest(profiles: [profile, otherProfile], tracks: entries, collections: [playlist, favorites]))
        .write(to: root.appendingPathComponent("manifest.json"))
    let store = OfflineLibrary(root: root, startTransfers: false); store.selectProfile(profile)
    #expect(store.currentCollections.count == 2)
    #expect(OfflineCatalog.records(store.currentRecords, collection: playlist, sort: .collection).map(\.track.id) == ["b", "a"])
    #expect(OfflineCatalog.records(store.currentRecords, sort: .artist).map(\.track.id) == ["c", "b", "a"])
    #expect(OfflineCatalog.records(store.currentRecords, sort: .title).map(\.track.id) == ["c", "b", "a"])
    #expect(OfflineCatalog.records(store.currentRecords, sort: .album).map(\.track.id) == ["c", "a", "b"])
    #expect(OfflineCatalog.records(store.currentRecords, sort: .date).map(\.track.id) == ["c", "b", "a"])
    #expect(OfflineCatalog.records(store.currentRecords, collection: playlist, sort: .collection, query: " ZULU ").map(\.track.id) == ["a"])
    #expect(OfflineCatalog.records(store.currentRecords, sort: .title, onlyReady: true).isEmpty)
    store.selectProfile(otherProfile)
    #expect(store.currentCollections.isEmpty)
    #expect(OfflineCatalog.records(entries, collection: OfflineCollection(id: "other", scope: otherProfile.id, kind: "playlist", sourceID: "p1", name: "Other", trackIDs: ["a"], keepUpdated: false), sort: .collection).isEmpty)
    store.selectProfile(profile); store.setKeepUpdated(playlist.id, enabled: false)
    let restart = OfflineLibrary(root: root, startTransfers: false); restart.selectProfile(profile)
    #expect(restart.currentCollections.first { $0.id == playlist.id }?.keepUpdated == false)
    #expect(OfflineCatalog.records(restart.currentRecords, collection: favorites, sort: .collection).map(\.track.id) == ["a", "c"])
    // Removing one local file never rewrites the playlist membership or removes
    // another song. Missing downloads remain honest in the collection count.
    restart.remove("a")
    #expect(restart.currentRecords.map(\.track.id).sorted() == ["b", "c"])
    #expect(restart.currentCollections.first { $0.id == playlist.id }?.trackIDs == playlist.trackIDs)
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


@MainActor @Test func offlineLogoutInvalidatesCompletionButUserPauseCanFinish() throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    let server = try ServerAddress("https://fixture.example"), user = try fixtureUser("first")
    let profile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: user.id), origin: server.url.absoluteString, user: user)
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 1000)
    let id = OfflineLibrary.digest(profile.id + "\n" + track.id)
    let record = OfflineTrack(id: id, scope: profile.id, track: track, state: .downloading, transferID: 42)
    let manifest = OfflineTestManifest(profiles: [profile], tracks: [record])
    try JSONEncoder().encode(manifest).write(to: root.appendingPathComponent("manifest.json"))
    let response = HTTPURLResponse(url: URL(string: "https://fixture.example/media")!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "audio/ogg"])!
    let temp = root.appendingPathComponent("transfer")
    let store = OfflineLibrary(root: root, startTransfers: false); store.selectProfile(profile)
    store.detach()
    try Data("OggSfixture-media".utf8).write(to: temp)
    store.finish(id, location: temp, response: response, taskID: 42)
    store.selectProfile(profile)
    #expect(store.entry(track.id)?.transferID == nil && store.entry(track.id)?.state == .paused)
    #expect(store.audioURL(track.id) == nil && !FileManager.default.fileExists(atPath: temp.path))
    let restart = OfflineLibrary(root: root, startTransfers: false); restart.selectProfile(profile)
    #expect(restart.entry(track.id)?.transferID == nil)
    // A normal pause is different: bytes already received can finish safely.
    try JSONEncoder().encode(manifest).write(to: root.appendingPathComponent("manifest.json"))
    let paused = OfflineLibrary(root: root, startTransfers: false); paused.selectProfile(profile)
    paused.pause(track.id)
    try Data("OggSfixture-media".utf8).write(to: temp)
    paused.finish(id, location: temp, response: response, taskID: 42)
    #expect(paused.audioURL(track.id) != nil)
}
