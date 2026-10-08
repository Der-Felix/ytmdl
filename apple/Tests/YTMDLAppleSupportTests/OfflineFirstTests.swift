import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

// Start-up and reconnect behavior when the server cannot be reached. A scripted URLProtocol
// stands in for the server (no sockets, no audio), and nothing is persisted.
private final class ServerScript: @unchecked Sendable {
    private let lock = NSLock()
    private var up = false
    var reachable: Bool {
        get { lock.lock(); defer { lock.unlock() }; return up }
        set { lock.lock(); up = newValue; lock.unlock() }
    }
}
private final class FirstProtocol: URLProtocol, @unchecked Sendable {
    static let server = ServerScript()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "first.fixture.example" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        guard Self.server.reachable else { client?.urlProtocol(self, didFailWithError: URLError(.notConnectedToInternet)); return }
        let url = request.url!
        let json = url.path.hasSuffix("/auth/status")
            ? #"{"data":{"authenticated":true,"setup_required":false,"user":{"id":"first","username":"fixture_user","display_name":"Fixture","role":"user"}}}"#
            : #"{"data":[]}"#
        let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(json.utf8)); client?.urlProtocolDidFinishLoading(self)
    }
}

private struct ManifestFixture: Codable { var profiles: [OfflineProfile]; var tracks: [OfflineTrack] }
private let target = ResumeTarget(text: "https://first.fixture.example", localHTTP: false)

/// A model that talks to the scripted server, with one saved account and, optionally, one stored song.
@MainActor private func model(root: URL, storedSong: Bool = true) throws -> AppModel {
    let server = try ServerAddress(target.text)
    let user = try JSONDecoder().decode(User.self, from: Data(#"{"id":"first","username":"fixture_user","displayName":"Fixture","role":"user"}"#.utf8))
    let profile = OfflineProfile(id: OfflineLibrary.profileID(server: server, userID: user.id), origin: server.url.absoluteString, user: user)
    var records: [OfflineTrack] = []
    if storedSong {
        let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 1000)
        let id = OfflineLibrary.digest(profile.id + "\n" + track.id)
        let directory = root.appendingPathComponent(profile.id)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try Data("OggSfixture-media".utf8).write(to: directory.appendingPathComponent(id + ".ogg"))
        records = [OfflineTrack(id: id, scope: profile.id, track: track, state: .ready, bytes: 17, fileName: id + ".ogg")]
    }
    try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
    try JSONEncoder().encode(ManifestFixture(profiles: [profile], tracks: records)).write(to: root.appendingPathComponent("manifest.json"))
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false))
    model.preferences = UserDefaults(suiteName: "ytmdl.offline-first.\(UUID().uuidString)")!
    model.watchesNetwork = false
    model.makeClient = { address, _ in
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [FirstProtocol.self]
        return try APIClient(server: address, persist: false, configuration: configuration)
    }
    return model
}
private func scratchRoot() -> URL { FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString) }

@Suite(.serialized) @MainActor struct OfflineFirstTests {
    @Test func connectivityFailuresAreUnreachableButAnswersAreNot() {
        for code in [URLError.Code.notConnectedToInternet, .timedOut, .cannotConnectToHost, .cannotFindHost, .networkConnectionLost, .dnsLookupFailed] {
            #expect(AppModel.isUnreachable(URLError(code)), "\(code)")
        }
        // A certificate problem or a cancelled request is not "offline": it must stay visible.
        #expect(!AppModel.isUnreachable(URLError(.serverCertificateUntrusted)))
        #expect(!AppModel.isUnreachable(URLError(.cancelled)))
        #expect(AppModel.isUnreachable(PlayerError.server(status: 503, code: "UNAVAILABLE", message: "down")))
        #expect(!AppModel.isUnreachable(PlayerError.server(status: 401, code: "UNAUTHORIZED", message: "expired")))
        #expect(!AppModel.isUnreachable(PlayerError.server(status: 404, code: "NOT_FOUND", message: "none")))
    }

    @Test func unreachableServerOpensTheSavedMusicWithoutAnError() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root)
        await app.resumeLastSession(target, persist: false)
        #expect(app.offlineMode)
        #expect(app.canReconnect)
        #expect(app.user?.id == "first")
        #expect(app.error == nil)
        #expect(app.offline.readyTracks.count == 1)
    }

    @Test func aLostSessionStillOpensTheSavedMusicWhenOffline() async throws {
        // An expired or revoked session drops the cookies, which is not a sign-out.
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root)
        await app.resumeLastSession(target, persist: false)
        #expect(app.offlineMode && app.canReconnect)
    }

    @Test func signingOutOnPurposeKeepsTheMusicFromReopeningByItself() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root)
        await app.resumeLastSession(target, persist: false)
        #expect(app.offlineMode)
        await app.logout()
        #expect(!app.offlineMode && app.user == nil)
        // The next start with the server away stays on the connection screen.
        await app.resumeLastSession(target, persist: false)
        #expect(!app.offlineMode)
        #expect(app.user == nil)
    }

    @Test func theRememberedServerKeepsTheDebugHTTPChoice() throws {
        let defaults = UserDefaults(suiteName: "ytmdl.saved-target.\(UUID().uuidString)")!
        #expect(AppModel.savedTarget(defaults) == nil)
        // Builds before 34 saved only the address; plain http can only have come from the debug choice.
        defaults.set("http://172.20.21.4:8080", forKey: "serverAddress")
        #expect(AppModel.savedTarget(defaults) == ResumeTarget(text: "http://172.20.21.4:8080", localHTTP: true))
        defaults.set("https://music.example", forKey: "serverAddress")
        #expect(AppModel.savedTarget(defaults) == ResumeTarget(text: "https://music.example", localHTTP: false))
        defaults.set(false, forKey: "serverAllowHTTP")
        defaults.set("http://172.20.21.4:8080", forKey: "serverAddress")
        #expect(AppModel.savedTarget(defaults)?.localHTTP == false)
    }

    @Test func anAccountWithoutStoredMusicHasNothingToOpen() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root, storedSong: false)
        await app.resumeLastSession(target, persist: false)
        #expect(!app.offlineMode)
        #expect(app.user == nil)
        #expect(app.error != nil)
    }

    @Test func aReachableServerOpensTheLibraryNotTheOfflineMusic() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = true
        let app = try model(root: root)
        let restored = await app.resumeLastSession(target, persist: false)
        #expect(restored)
        #expect(!app.offlineMode)
        #expect(!app.canReconnect)
        #expect(app.user?.id == "first")
        #expect(app.error == nil)
    }

    @Test func reconnectingWhileTheServerIsStillAwayChangesNothing() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root)
        await app.resumeLastSession(target, persist: false)
        await app.reconnect()
        #expect(app.offlineMode && app.canReconnect)
        #expect(app.user?.id == "first")
        #expect(app.error == nil)
        #expect(!app.reconnecting)
    }

    @Test func reconnectingOnceTheServerIsBackLeavesTheOfflineMusic() async throws {
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        FirstProtocol.server.reachable = false
        let app = try model(root: root)
        await app.resumeLastSession(target, persist: false)
        #expect(app.canReconnect)
        FirstProtocol.server.reachable = true
        await app.reconnectIfIdle()
        #expect(!app.offlineMode)
        #expect(!app.canReconnect)
        #expect(app.user?.id == "first")
    }

    @Test func aManuallyOpenedOfflineLibraryOffersNoReconnect() async throws {
        // Opened from the sign-in screen there is no server session to return to.
        let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
        let app = try model(root: root)
        let profile = try #require(app.offline.profiles.first)
        app.openOffline(profile)
        #expect(app.offlineMode)
        #expect(!app.canReconnect)
    }
}
