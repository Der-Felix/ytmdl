import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

private final class PlaylistFixtureState: @unchecked Sendable {
    let lock = NSLock()
    var requests: [(String, String, [String: Any])] = []
    var rules: [String: Any]?
    var title = "Fixture"
    var ids: [String] = []
    var failRules = false
    var failBulkAt = 0
    var bulkCount = 0
    var delayCreate = false
    func snapshot() -> [(String, String, [String: Any])] { lock.lock(); defer { lock.unlock() }; return requests }
    func setPreviewTracks() { lock.lock(); defer { lock.unlock() }; ids = ["t0", "t1", "t2"] }
    func configureFailure(rules: Bool = false, bulk: Int = 0, delay: Bool = false) {
        lock.lock(); defer { lock.unlock() }; failRules = rules; failBulkAt = bulk; bulkCount = 0; delayCreate = delay
    }
    func reset() { lock.lock(); defer { lock.unlock() }; requests = []; rules = nil; title = "Fixture"; ids = []; failRules = false; failBulkAt = 0; bulkCount = 0; delayCreate = false }
}
private final class PlaylistProtocol: URLProtocol, @unchecked Sendable {
    static let state = PlaylistFixtureState()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "playlists.fixture.example" }
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
        state.requests.append((method, path, body))
        var status = 200
        var result: Any = [:]
        var headers = ["Content-Type": "application/json"]
        if path.hasSuffix("/auth/status") {
            headers["Set-Cookie"] = "ytmdl_csrf=fixture-only; Secure; Path=/; Max-Age=3600"
            result = ["authenticated": false, "setup_required": false]
        } else if method != "GET" && request.value(forHTTPHeaderField: "X-CSRF-Token") != "fixture-only" {
            status = 403
        } else {
            if method == "POST" && path.hasSuffix("/playlists") { state.title = body["name"] as! String; state.rules = body["smart_rules"] as? [String: Any] }
            if method == "PATCH" { state.title = body["name"] as! String }
            if path.hasSuffix("/rules") { if state.failRules { status = 500 } else { state.rules = body["smart_rules"] as? [String: Any] } }
            if path.hasSuffix("/bulk") {
                state.bulkCount += 1
                if state.bulkCount == state.failBulkAt { status = 500 }
                else { for id in body["track_ids"] as! [String] where !state.ids.contains(id) { state.ids.append(id) } }
            }
            if path.hasSuffix("/reorder") { state.ids = body["track_ids"] as! [String] }
            if method == "DELETE" && path.contains("/tracks/") { state.ids.removeAll { $0 == path.components(separatedBy: "/").last } }
            let tracks = state.ids.map { ["id": $0, "title": $0, "album": "Fixture", "artists": ["Fixture"], "duration_ms": 1000] as [String: Any] }
            var playlist: [String: Any] = ["id": "p0", "name": state.title, "description": body["description"] ?? "", "track_count": tracks.count, "duration_ms": tracks.count * 1000, "tracks": tracks]
            if let rules = state.rules { playlist["smart_rules"] = rules }
            result = playlist
        }
        let object: [String: Any] = status >= 400 ? ["error": ["code": "FIXTURE_FAILURE", "message": "Fixture mutation failed"]] : ["data": result]
        let data = try! JSONSerialization.data(withJSONObject: object)
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
        if state.delayCreate && method == "POST" && path.hasSuffix("/playlists") {
            DispatchQueue.global().asyncAfter(deadline: .now() + 0.15) { [self] in
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data); client?.urlProtocolDidFinishLoading(self)
            }
        } else {
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data); client?.urlProtocolDidFinishLoading(self)
        }
    }
}

@MainActor @Test func nativePlaylistMutationsUseTypedRulesCSRFAndOrderedTrackIDs() async throws {
    PlaylistProtocol.state.reset()
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [PlaylistProtocol.self]
    let client = try APIClient(server: ServerAddress("https://playlists.fixture.example"), persist: false, configuration: configuration)
    defer { client.invalidate() }
    let _: AuthStatus = try await client.get("/auth/status")
    let offlineRoot = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: offlineRoot) }
    let model = AppModel(offlineLibrary: OfflineLibrary(root: offlineRoot, startTransfers: false)); model.client = client
    let rules = SmartPlaylistRules(genre: "Pop", artistId: "artist_1", favorites: true, addedDays: 30, sort: "frequent", limit: 50)
    let smart = try await model.createPlaylist(name: "Intelligent", description: "Rules", rules: rules)
    #expect(smart.smartRules == rules)
    var requests = PlaylistProtocol.state.snapshot()
    let body = try #require(requests.first(where: { $0.0 == "POST" })?.2["smart_rules"] as? [String: Any])
    #expect(body["favorites"] as? Bool == true)
    #expect(body["artist_id"] as? String == "artist_1")
    #expect(body["added_days"] as? Int == 30)
    let manual = try await model.editPlaylist(smart, name: "Manual", description: "Edited", rules: nil)
    #expect(manual.smartRules == nil && model.playlists.count == 1)
    requests = PlaylistProtocol.state.snapshot()
    #expect(requests.first(where: { $0.1.hasSuffix("/rules") })?.2["smart_rules"] is NSNull)
    let added = try await model.changePlaylistTracks(manual, ids: ["t0", "t1", "t2"])
    #expect(added.tracks.map(\.id) == ["t0", "t1", "t2"])
    let reordered = try await model.changePlaylistTracks(manual, ids: ["t2", "t0", "t1"], reorder: true)
    #expect(reordered.tracks.map(\.id) == ["t2", "t0", "t1"])
    let removed = try await model.changePlaylistTracks(manual, ids: [], removing: "t0")
    #expect(removed.tracks.map(\.id) == ["t2", "t1"])
    // Queue-sized additions are chunked at the existing 100-item transaction boundary.
    let many = try await model.changePlaylistTracks(manual, ids: (0..<205).map { "new\($0)" })
    #expect(many.tracks.count == 207)
    let chunks = PlaylistProtocol.state.snapshot().filter { $0.1.hasSuffix("/bulk") }.suffix(3)
    #expect(chunks.map { ($0.2["track_ids"] as! [String]).count } == [100, 100, 5])
    PlaylistProtocol.state.configureFailure(bulk: 2)
    do {
        _ = try await model.changePlaylistTracks(manual, ids: (0..<205).map { "partial\($0)" })
        Issue.record("Partial add must be reported")
    } catch PlayerError.server(_, "PLAYLIST_PARTIALLY_UPDATED", _) { }
    #expect(model.playlists.first?.trackCount == 307)
    PlaylistProtocol.state.configureFailure(rules: true)
    do {
        _ = try await model.editPlaylist(manual, name: "Acknowledged name", description: "", rules: rules)
        Issue.record("Rule failure must not report the whole edit as successful")
    } catch PlayerError.server(_, "PLAYLIST_RULES_NOT_SAVED", _) { }
    #expect(model.playlists.first?.name == "Acknowledged name")
    #expect(!model.playlistBusy)
    try await model.deletePlaylist(manual)
    #expect(model.playlists.isEmpty)
    #expect(throws: PlayerError.invalidID) { try client.playlistPath("../foreign") }
    #expect(!PlaylistProtocol.state.snapshot().contains { $0.1.contains("/stream") })
    PlaylistProtocol.state.configureFailure(delay: true)
    let pending = Task { try await model.createPlaylist(name: "Old account", description: "", rules: nil) }
    try await Task.sleep(for: .milliseconds(30))
    try model.connect("https://replacement.fixture.example", localHTTP: false, persist: false)
    do { _ = try await pending.value; Issue.record("Old session mutation must be discarded") }
    catch { #expect(error is CancellationError || (error as? URLError)?.code == .cancelled) }
    #expect(model.playlists.isEmpty && !model.playlistBusy)
    model.client?.invalidate()
}

@MainActor @Test func playlistCoverPreviewsAreBoundedSharedAndClearedAfterAccountChanges() async throws {
    PlaylistProtocol.state.reset(); PlaylistProtocol.state.setPreviewTracks()
    let configuration = URLSessionConfiguration.ephemeral; configuration.protocolClasses = [PlaylistProtocol.self]
    let client = try APIClient(server: ServerAddress("https://playlists.fixture.example"), persist: false, configuration: configuration)
    defer { client.invalidate() }
    let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: folder) }
    let model = AppModel(offlineLibrary: OfflineLibrary(root: folder, startTransfers: false)); model.client = client
    let candidates = (0..<20).map { Playlist(id: "p\($0)", name: "Fixture", trackCount: 3, durationMs: 3000) }
    await model.loadPlaylistPreviews(candidates)
    #expect(model.playlistPreviews.count == 12)
    #expect(model.playlistPreviews.values.allSatisfy { $0.count == 1 }, "Same-album tracks use one cover.")
    let count = PlaylistProtocol.state.snapshot().count
    await model.loadPlaylistPreviews(candidates)
    #expect(PlaylistProtocol.state.snapshot().count == count, "Start and Playlists reuse the same previews.")
    model.rememberPlaylist(candidates[0]); await model.loadPlaylistPreviews([candidates[0]])
    #expect(model.playlistPreviews.count == 1)
    try model.connect("https://replacement.fixture.example", localHTTP: false, persist: false)
    #expect(model.playlistPreviews.isEmpty)
    model.client?.invalidate()
}

@Test func smartRulesDecodeOmittedDefaultsAndLegacyPlaylistDetails() throws {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
    let rules = try decoder.decode(SmartPlaylistRules.self, from: Data(#"{"sort":"recent","limit":20}"#.utf8))
    #expect(!rules.favorites && rules.addedDays == 0 && rules.artistId == nil)
    let detail = try decoder.decode(PlaylistDetail.self, from: Data(#"{"tracks":[]}"#.utf8))
    let legacy = Playlist(id: "p0", name: "Fixture", trackCount: 0, durationMs: 0)
    #expect(detail.playlist(fallback: legacy).name == "Fixture")
}
