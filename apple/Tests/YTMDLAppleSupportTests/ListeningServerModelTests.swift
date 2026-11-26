import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

// The app model against a server with and without the 1.11 listening
// features: what `/health` announces decides the radio requests and the
// exclusions; an older server keeps the 0.6.0 behaviour.

private final class ServerFixtureState: @unchecked Sendable {
    let lock = NSLock()
    var modern = false
    var preferenceStatus = 200
    var requests: [(method: String, path: String, query: [String: String])] = []
    func reset(modern: Bool) {
        lock.lock(); defer { lock.unlock() }
        self.modern = modern; preferenceStatus = 200; requests = []
    }
    func snapshot() -> [(method: String, path: String, query: [String: String])] { lock.lock(); defer { lock.unlock() }; return requests }
}

private final class ServerProtocol: URLProtocol, @unchecked Sendable {
    static let state = ServerFixtureState()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "server.fixture.example" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() { }
    override func startLoading() {
        let state = Self.state
        let url = request.url!, method = request.httpMethod ?? "GET"
        var query: [String: String] = [:]
        for item in URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? [] { query[item.name] = item.value ?? "" }
        state.lock.lock(); defer { state.lock.unlock() }
        state.requests.append((method, url.path, query))
        func track(_ id: String, _ title: String) -> [String: Any] {
            ["id": id, "title": title, "artists": ["Fixture"], "album": "Album", "duration_ms": 200000, "artist_id": "a1"]
        }
        var status = 200
        var result: Any = [:]
        var headers = ["Content-Type": "application/json"]
        switch (method, url.path) {
        case (_, "/api/v1/auth/status"):
            headers["Set-Cookie"] = "ytmdl_csrf=fixture-only; Secure; Path=/; Max-Age=3600"
            result = ["authenticated": false, "setup_required": false]
        case ("GET", "/api/v1/health"):
            result = state.modern
                ? ["status": "ok", "version": "1.11.0", "features": ["radio.station", "radio.seeds", "history.stats", "listening.preferences"]] as [String: Any]
                : ["status": "ok", "version": "1.10.1"]
        case ("GET", "/api/v1/library/radio"):
            result = [track("r1", "Eins"), track("r2", "Zwei")]
        case ("GET", "/api/v1/library/radio/station") where state.modern:
            let cursor = query["cursor"] ?? ""
            result = cursor.isEmpty
                ? ["title": "Radio: Fixture (Server)", "seed": ["kind": "artist", "id": "a1", "name": "Fixture"],
                   "tracks": [track("s1", "Eins"), track("s2", "Zwei"), track("s3", "Drei")], "next_cursor": "c1", "remaining": 2]
                : ["title": "Radio: Fixture (Server)", "tracks": [track("s3", "Drei"), track("s4", "Vier"), track("s5", "Fünf")], "next_cursor": NSNull(), "remaining": 0]
        case ("PUT", "/api/v1/listening-preferences") where state.modern:
            status = request.value(forHTTPHeaderField: "X-CSRF-Token") == "fixture-only" ? state.preferenceStatus : 403
            result = ["id": "p1", "kind": "artist", "target": "a1", "name": "Fixture", "created_at": "2026-11-26T12:00:00Z"]
        case ("GET", "/api/v1/listening-preferences") where state.modern:
            result = [] as [Any]
        default:
            // An older server answers an unknown path with 400, not 404.
            status = 400
        }
        let object: [String: Any] = status >= 400 ? ["error": ["code": "INVALID_REQUEST", "message": "The requested endpoint does not exist."]] : ["data": result]
        let data = try! JSONSerialization.data(withJSONObject: object)
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data); client?.urlProtocolDidFinishLoading(self)
    }
}

@MainActor private func serverModel(_ suite: String) async throws -> (AppModel, APIClient, URL) {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [ServerProtocol.self]
    let client = try APIClient(server: ServerAddress("https://server.fixture.example"), persist: false, configuration: configuration)
    let _: AuthStatus = try await client.get("/auth/status")
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false))
    model.player = PlayerModel(volumePreferences: try #require(UserDefaults(suiteName: suite)))
    model.client = client
    model.user = try JSONDecoder().decode(User.self, from: Data(#"{"id":"listener","username":"listener","displayName":"Listener","role":"user"}"#.utf8))
    return (model, client, root)
}

@MainActor @Test func olderServerKeepsTheDeviceSeededRadioAndHidesTheNewFeatures() async throws {
    ServerProtocol.state.reset(modern: false)
    let suite = "server-old-" + UUID().uuidString
    defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
    let (model, client, root) = try await serverModel(suite)
    defer { model.player.stop(); client.invalidate(); try? FileManager.default.removeItem(at: root) }
    await model.refreshServerFeatures()
    #expect(model.serverHealthKnown && model.serverVersion == "1.10.1" && !model.serverFeatures.announced)
    #expect(!model.serverFeatures.stations && !model.serverFeatures.statistics && !model.serverFeatures.preferences)
    let seed = Track(id: "t1", title: "Nachtfahrt", artists: ["Fixture"], album: "", durationMs: 200000)
    await model.startRadio(seed)
    let requests = ServerProtocol.state.snapshot()
    #expect(requests.contains { $0.path == "/api/v1/library/radio" && $0.query["seed"] == "t1" })
    #expect(!requests.contains { $0.path.hasSuffix("/radio/station") }, "No probing of endpoints an old server lacks.")
    #expect(model.player.queue.tracks.map(\.id) == ["t1", "r1", "r2"])
    #expect(model.player.queue.context?.title == "Radio: Nachtfahrt")
    // Exclusions and statistics do nothing without the feature.
    await model.excludeArtist(id: "a1", name: "Fixture")
    #expect(model.preferences.isEmpty && !ServerProtocol.state.snapshot().contains { $0.path.hasSuffix("/listening-preferences") })
    await #expect(throws: CancellationError.self) { _ = try await model.listeningStats(.week) }
    #expect(model.mixTimeZone.isEmpty, "An old server is not sent a time zone.")
    #expect(model.error == nil)
}

@MainActor @Test func newerServerServesStationsWithACursorAndConfirmsExclusions() async throws {
    ServerProtocol.state.reset(modern: true)
    let suite = "server-new-" + UUID().uuidString
    defer { UserDefaults(suiteName: suite)?.removePersistentDomain(forName: suite) }
    let (model, client, root) = try await serverModel(suite)
    defer { model.player.stop(); client.invalidate(); try? FileManager.default.removeItem(at: root) }
    await model.refreshServerFeatures()
    #expect(model.serverFeatures.stations && model.serverFeatures.preferences && model.serverVersion == "1.11.0")

    // An artist radio without picking a seed title on the device.
    await model.startArtistRadio(Artist(id: "a1", name: "Fixture"))
    let start = try #require(ServerProtocol.state.snapshot().last { $0.path == "/api/v1/library/radio/station" })
    #expect(start.query["artist"] == "a1" && start.query["limit"] == "50" && start.query["cursor"] == nil && start.query["nonce"]?.isEmpty == false)
    #expect(!ServerProtocol.state.snapshot().contains { $0.path.hasSuffix("/top-tracks") || $0.path == "/api/v1/library/radio" })
    #expect(model.player.queue.tracks.map(\.id) == ["s1", "s2", "s3"])
    #expect(model.player.queue.context == QueueContext(kind: "radio", id: "artist:a1", title: "Radio: Fixture (Server)"), "The server names the station.")
    #expect(model.stationPaging?.cursor == "c1")

    // The top-up continues at the cursor with the same nonce and leaves out what is queued.
    let queue = model.player.queue.tracks
    await model.extendAutoplay(seed: queue[0], expectedCount: queue.count, lastID: queue[queue.count - 1].id)
    let next = try #require(ServerProtocol.state.snapshot().last { $0.path == "/api/v1/library/radio/station" })
    #expect(next.query["cursor"] == "c1" && next.query["nonce"] == start.query["nonce"] && next.query["artist"] == "a1")
    #expect(next.query["exclude"]?.split(separator: ",").map(String.init).starts(with: ["s1", "s2", "s3"]) == true)
    #expect(model.player.queue.tracks.map(\.id) == ["s1", "s2", "s3", "s4", "s5"], "Nothing repeats.")
    #expect(model.stationPaging?.exhausted == true)

    // "Künstler nicht mehr vorschlagen": shown at once, upcoming titles leave the radio, the server confirms.
    await model.excludeArtist(id: "a1", name: "Fixture")
    #expect(model.preferences.entries.map(\.id) == ["p1"])
    #expect(model.player.queue.tracks.map(\.id) == ["s1"], "Every upcoming title of the artist leaves the radio; the current one plays on.")
    let put = try #require(ServerProtocol.state.snapshot().last { $0.method == "PUT" })
    #expect(put.path == "/api/v1/listening-preferences")
    #expect(model.notice?.action?.title == "Rückgängig")

    // A refusal rolls the exclusion back.
    ServerProtocol.state.lock.lock(); ServerProtocol.state.preferenceStatus = 500; ServerProtocol.state.lock.unlock()
    await model.excludeGenre("Pop")
    #expect(!model.preferences.contains(.genre, "Pop") && model.preferences.entries.count == 1)
}
