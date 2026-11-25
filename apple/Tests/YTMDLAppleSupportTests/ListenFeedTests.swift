import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

private final class ListenFixtureState: @unchecked Sendable {
    let lock = NSLock()
    var posts: [(String, [String: Any])] = []
    var historyStatus = 200
    var skipStatus = 204
    var playStatus = 204
    func reset(history: Int = 200, skip: Int = 204, play: Int = 204) {
        lock.lock(); defer { lock.unlock() }; posts = []; historyStatus = history; skipStatus = skip; playStatus = play
    }
    func snapshot() -> [(String, [String: Any])] { lock.lock(); defer { lock.unlock() }; return posts }
}
private final class ListenProtocol: URLProtocol, @unchecked Sendable {
    static let state = ListenFixtureState()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "listen.fixture.example" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() { }
    override func startLoading() {
        let state = Self.state
        var bytes = request.httpBody ?? Data()
        if let stream = request.httpBodyStream, bytes.isEmpty {
            stream.open(); defer { stream.close() }
            var buffer = [UInt8](repeating: 0, count: 1024)
            while stream.hasBytesAvailable { let n = stream.read(&buffer, maxLength: buffer.count); if n <= 0 { break }; bytes.append(contentsOf: buffer.prefix(n)) }
        }
        let body = (try? JSONSerialization.jsonObject(with: bytes)) as? [String: Any] ?? [:]
        let path = request.url!.path, method = request.httpMethod!
        state.lock.lock(); defer { state.lock.unlock() }
        var status = 200
        var result: Any = [:]
        var headers = ["Content-Type": "application/json"]
        let track: [String: Any] = ["id": "t1", "title": "Eins", "artists": ["Fixture"], "album": "Album", "duration_ms": 200000]
        switch (method, path) {
        case (_, "/api/v1/auth/status"):
            headers["Set-Cookie"] = "ytmdl_csrf=fixture-only; Secure; Path=/; Max-Age=3600"
            result = ["authenticated": false, "setup_required": false]
        case ("POST", _) where request.value(forHTTPHeaderField: "X-CSRF-Token") != "fixture-only":
            status = 403
        case ("POST", "/api/v1/history"):
            state.posts.append((path, body)); status = state.playStatus
        case ("POST", "/api/v1/history/skips"):
            state.posts.append((path, body)); status = state.skipStatus
        case ("GET", "/api/v1/library/mixes"):
            result = [["id": "genre-abc", "kind": "genre", "title": "Dein Mix 1", "description": "Pop", "genre": "Pop", "track_count": 1, "cover_track_ids": ["t1"], "cover_release_ids": NSNull()],
                      ["id": "on_repeat", "kind": "on_repeat", "title": "In Dauerschleife", "description": "", "track_count": 1, "cover_track_ids": [], "cover_release_ids": []]]
        case ("GET", "/api/v1/library/mixes/on_repeat"):
            result = ["id": "on_repeat", "kind": "on_repeat", "title": "In Dauerschleife", "description": "", "track_count": 1, "cover_track_ids": [], "cover_release_ids": [], "tracks": [track]]
        case ("GET", "/api/v1/history"):
            status = state.historyStatus
            result = [track.merging(["play_count": 3, "last_played_at": "2026-11-24T18:03:04.5Z"]) { $1 }]
        case ("GET", "/api/v1/library/releases"):
            result = [["id": "r1", "title": "Neu", "artists": ["Fixture"], "year": 2026, "track_count_in_library": 1]]
        case ("GET", "/api/v1/library/artists/a1/top-tracks"):
            result = [track.merging(["play_count": 9, "last_played_at": "2026-11-24T18:03:04Z"]) { $1 }]
        default:
            status = 404
        }
        let object: [String: Any] = status >= 400 ? ["error": ["code": "FIXTURE_FAILURE", "message": "Fixture failure"]] : ["data": result]
        let data = status == 204 ? Data() : try! JSONSerialization.data(withJSONObject: object)
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data); client?.urlProtocolDidFinishLoading(self)
    }
}

@MainActor private func listenModel(_ suite: String) async throws -> (AppModel, APIClient, URL) {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [ListenProtocol.self]
    let client = try APIClient(server: ServerAddress("https://listen.fixture.example"), persist: false, configuration: configuration)
    let _: AuthStatus = try await client.get("/auth/status")
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false))
    model.client = client
    model.user = try JSONDecoder().decode(User.self, from: Data(#"{"id":"listener","username":"listener","displayName":"Listener","role":"user"}"#.utf8))
    model.listeningHistory = ListeningHistory(preferences: try #require(UserDefaults(suiteName: suite)))
    model.listeningHistory.configure(server: client.server, userID: "listener", persist: true)
    model.setSyncHistory(true)
    return (model, client, root)
}

@MainActor @Test func pendingPlaysAndSkipsSurviveRestartAndOldEventsStayPlays() throws {
    let suite = "listen-events-" + UUID().uuidString
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let server = try ServerAddress("https://fixture.example")
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 10000)
    let history = ListeningHistory(preferences: preferences)
    history.configure(server: server, userID: "first", persist: true)
    history.enqueueEvent(track, kind: .skip)
    history.enqueueEvent(track)
    #expect(history.pendingEvents.map(\.resolvedKind) == [.skip, .play])
    // A restart reads the same queue for the same account and server only.
    let restarted = ListeningHistory(preferences: preferences)
    restarted.configure(server: server, userID: "first", persist: true)
    #expect(restarted.pendingEvents.map(\.resolvedKind) == [.skip, .play])
    #expect(Set(restarted.pendingEvents.map(\.eventID)).count == 2 && restarted.pendingEvents.allSatisfy { (16...100).contains($0.eventID.count) })
    restarted.configure(server: try ServerAddress("https://other.example"), userID: "first", persist: true)
    #expect(restarted.pendingEvents.isEmpty)
    // Events saved before skips existed carry no kind.
    let old = try JSONDecoder().decode([ListeningEvent].self, from: Data(#"[{"eventID":"0123456789abcdef0123","trackID":"song"}]"#.utf8))
    #expect(old.first?.resolvedKind == .play && old.first?.kind == nil)
}

@MainActor @Test func queuedPlaysAndSkipsSyncToTheirEndpointsAndRefusedEventsDoNotBlockTheQueue() async throws {
    ListenProtocol.state.reset(skip: 404)
    let suite = "listen-sync-" + UUID().uuidString
    defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
    let (model, client, root) = try await listenModel(suite)
    defer { client.invalidate(); try? FileManager.default.removeItem(at: root) }
    let track = Track(id: "t1", title: "Eins", artists: [], album: "", durationMs: 200000)
    // As queued while offline: a skip (refused by an older server) and a play.
    model.listeningHistory.enqueueEvent(track, kind: .skip)
    model.listeningHistory.enqueueEvent(track, kind: .play)
    await model.flushListeningHistory()
    let posts = ListenProtocol.state.snapshot()
    #expect(posts.map(\.0) == ["/api/v1/history/skips", "/api/v1/history"])
    #expect(posts.allSatisfy { $0.1["track_id"] as? String == "t1" && ($0.1["event_id"] as? String)?.isEmpty == false })
    #expect(model.listeningHistory.pendingEvents.isEmpty)

    // A server error keeps the event, with its id, for the next attempt.
    ListenProtocol.state.reset(play: 503)
    model.listeningHistory.enqueueEvent(track, kind: .play)
    let pending = try #require(model.listeningHistory.pendingEvents.first)
    await model.flushListeningHistory()
    #expect(model.listeningHistory.pendingEvents.map(\.eventID) == [pending.eventID])
    ListenProtocol.state.reset()
    await model.flushListeningHistory()
    #expect(ListenProtocol.state.snapshot().first?.1["event_id"] as? String == pending.eventID)
    #expect(model.listeningHistory.pendingEvents.isEmpty)
    #expect(AppModel.dropsListeningEvent(after: PlayerError.server(status: 404, code: "x", message: "x")))
    #expect(!AppModel.dropsListeningEvent(after: PlayerError.server(status: 401, code: "x", message: "x")))
    #expect(!AppModel.dropsListeningEvent(after: PlayerError.server(status: 500, code: "x", message: "x")))
    #expect(!AppModel.dropsListeningEvent(after: URLError(.notConnectedToInternet)))
}

@MainActor @Test func listenFeedLoadsSectionsIndependentlyAndPlaysAMixWithItsContext() async throws {
    ListenProtocol.state.reset(history: 404)
    let suite = "listen-feed-" + UUID().uuidString
    defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
    let (model, client, root) = try await listenModel(suite)
    defer { model.player.stop(); client.invalidate(); try? FileManager.default.removeItem(at: root) }
    await model.loadListenFeed()
    #expect(model.feed.mixesPhase == .loaded && model.feed.mixes.map(\.id) == ["genre-abc", "on_repeat"])
    #expect(model.feed.mixes[0].coverReleaseIds.isEmpty)
    #expect(model.feed.historyPhase == .unsupported && model.feed.history.isEmpty, "An older server without history only hides the section.")
    #expect(model.feed.releasesPhase == .loaded && model.feed.newReleases.map(\.id) == ["r1"])
    #expect(!model.feed.unreachable && model.error == nil)

    ListenProtocol.state.reset()
    await model.loadListenFeed()
    #expect(model.feed.historyPhase == .loaded && model.feed.history.first?.playCount == 3)

    let detail = try await model.loadPersonalMix("on_repeat")
    #expect(detail.mix.title == "In Dauerschleife" && detail.tracks.map(\.id) == ["t1"])
    await #expect(throws: PlayerError.self) { try await model.loadPersonalMix("gone") }
    let top = try await model.artistTopTracks("a1")
    #expect(top.map(\.playCount) == [9] && top.first?.track.title == "Eins")
    model.feed.clear()
    #expect(model.feed.mixes.isEmpty && model.feed.mixesPhase == .idle)
}
