import Foundation
import AVFoundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

final class FixtureProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let path = request.url!.path
        var headers = ["Content-Type": "application/json"]
        let data: String
        var status = 200
        if request.url?.host == "unavailable.fixture.example" {
            status = 503
            data = #"{"error":{"code":"INTERNAL_ERROR","message":"Fixture unavailable"}}"#
        } else if path.hasSuffix("/auth/status") {
            headers["Set-Cookie"] = "ytmdl_csrf=fixture-csrf; Path=/; Max-Age=3600"
            data = #"{"data":{"authenticated":false,"setup_required":false}}"#
        } else if path.hasSuffix("/auth/login") {
            guard request.value(forHTTPHeaderField: "X-CSRF-Token") == "fixture-csrf" else { fatalError("missing CSRF") }
            var body = request.httpBody ?? Data()
            if body.isEmpty, let stream = request.httpBodyStream {
                stream.open(); defer { stream.close() }
                var buffer = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&buffer, maxLength: buffer.count)
                    if count <= 0 { break }
                    body.append(contentsOf: buffer.prefix(count))
                }
            }
            let object = try? JSONSerialization.jsonObject(with: body) as? [String: String]
            guard object?["username"] == "fixture_user" else { fatalError("incorrect login payload") }
            if object?["password"] == "fixture-wrong" {
                status = 401
                data = #"{"error":{"code":"INVALID_CREDENTIALS","message":"Invalid credentials"}}"#
            } else {
                headers["Set-Cookie"] = "ytmdl_session=fixture-session; Path=/; Max-Age=3600; HttpOnly"
                data = #"{"data":{"id":"fixture","username":"fixture_user","display_name":"Fixture","role":"user"}}"#
            }
        } else if path.hasSuffix("/library/tracks") {
            data = #"{"data":[{"id":"a","title":"Track","artists":["Artist"],"album":"Album","duration_ms":3000}],"meta":{"total":1}}"#
        } else {
            status = 401
            data = #"{"error":{"code":"UNAUTHENTICATED","message":"Session expired"}}"#
        }
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(data.utf8)); client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@MainActor @Test func loginCSRFEnvelopeExpiryAndLogoutIsolation() async throws {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [FixtureProtocol.self]
    let client = try APIClient(server: ServerAddress("https://fixture.example"), persist: false, configuration: configuration)
    defer { client.invalidate() }
    let user = try await client.login(username: "fixture_user", password: "fixture-only-not-a-real-password")
    #expect(user.username == "fixture_user")
    #expect(client.authenticationCookies.contains(where: { $0.name == "ytmdl_session" }))
    let tracks: [Track] = try await client.get("/library/tracks")
    #expect(tracks.count == 1); #expect(tracks.first?.duration == 3)
    do { let _: User = try await client.get("/auth/me"); Issue.record("expired session accepted") }
    catch let error as PlayerError { #expect(error.localizedDescription.contains("abgelaufen")) }
    try client.forget(); #expect(client.authenticationCookies.isEmpty)
}

@MainActor @Test func mutationRequiresCSRFAndArtworkUsesOnlyOrigin() throws {
    let client = try APIClient(server: ServerAddress("https://fixture.example"), persist: false)
    defer { client.invalidate() }
    #expect(throws: PlayerError.self) { try client.request("/favorites/a", method: "PUT") }
    let artwork = try client.artworkRequest(kind: "artists", id: "a")
    #expect(artwork.url?.host == "fixture.example")
    #expect(artwork.url?.path == "/api/v1/library/artists/a/artwork")
    #expect(client.session.configuration.urlCache == nil)
    #expect(client.session.configuration.httpCookieStorage !== HTTPCookieStorage.shared)
}

@MainActor @Test func restoredCookiesKeepTransportSecurityAndLegacyCompatibility() throws {
    let http = try ServerAddress("http://127.0.0.1:59584", allowLocalHTTP: true)
    let https = try ServerAddress("https://fixture.example")
    let legacy = Data(#"{"name":"ytmdl_csrf","value":"fixture-csrf","expiresAt":4102444800}"#.utf8)
    let stored = try JSONDecoder().decode(StoredCookie.self, from: legacy)
    let localCookie = try #require(stored.cookie(for: http))
    #expect(!localCookie.isSecure)
    #expect(stored.cookie(for: https)?.isSecure == true)
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [FixtureProtocol.self]
    configuration.httpCookieStorage!.setCookie(localCookie)
    let client = try APIClient(server: http, persist: false, configuration: configuration)
    defer { client.invalidate() }
    let request = try client.request("/auth/login", method: "POST")
    #expect(request.value(forHTTPHeaderField: "X-CSRF-Token") == "fixture-csrf")
    let protected = StoredCookie(name: "ytmdl_session", value: "fixture-session", expiresAt: Date().addingTimeInterval(3600), secure: true)
    let decoded = try JSONDecoder().decode(StoredCookie.self, from: JSONEncoder().encode(protected))
    #expect(decoded.cookie(for: http)?.isSecure == true)
    #expect(StoredCookie(name: "ytmdl_csrf", value: "expired", expiresAt: .distantPast).cookie(for: http) == nil)
    #expect(StoredCookie(name: "unrelated", value: "fixture-only", expiresAt: .distantFuture).cookie(for: http) == nil)
}

@MainActor @Test func wrongPasswordIsNotReportedAsAnExpiredSession() async throws {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [FixtureProtocol.self]
    let client = try APIClient(server: ServerAddress("https://fixture.example"), persist: false, configuration: configuration)
    defer { client.invalidate() }
    do {
        _ = try await client.login(username: "fixture_user", password: "fixture-wrong")
        Issue.record("incorrect fixture password accepted")
    } catch let error as PlayerError { #expect(error == .invalidCredentials) }
}

@MainActor @Test func failedServerCheckDoesNotEnableSignIn() async throws {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [FixtureProtocol.self]
    let client = try APIClient(server: ServerAddress("https://unavailable.fixture.example"), persist: false, configuration: configuration)
    defer { client.invalidate() }
    let offlineRoot = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: offlineRoot) }
    let model = AppModel(offlineLibrary: OfflineLibrary(root: offlineRoot, startTransfers: false)); model.client = client
    let connected = await model.restore()
    #expect(!connected)
    #expect(model.user == nil)
    #expect(model.error != nil)
}

#if os(macOS)
@MainActor @Test func appVolumeControlsAudioAndRestoresWithoutChangingPlayback() throws {
    let suite = "org.ytmdl.tests.volume.\(UUID().uuidString)"
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let audio = AVPlayer()
    let player = PlayerModel(volumePreferences: preferences, audio: audio)
    #expect(player.volume == 1)
    player.setVolume(0.35)
    #expect(abs(audio.volume - 0.35) < 0.001)
    #expect(!player.isPlaying)
    player.toggleMute()
    #expect(player.isMuted && audio.isMuted)
    #expect(player.volume == 0.35)
    let restoredAudio = AVPlayer()
    let restored = PlayerModel(volumePreferences: preferences, audio: restoredAudio)
    #expect(restored.isMuted && restoredAudio.isMuted)
    #expect(abs(restoredAudio.volume - 0.35) < 0.001)
    restored.setVolume(0.6)
    #expect(!restored.isMuted && !restoredAudio.isMuted)
    #expect(abs(restoredAudio.volume - 0.6) < 0.001)
    player.stop(); restored.stop()
}
@MainActor @Test func appVolumeRejectsNonfiniteValuesAndClampsPersistedLevels() throws {
    let suite = "org.ytmdl.tests.volume.\(UUID().uuidString)"
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    preferences.set(8.0, forKey: "playerVolume")
    let audio = AVPlayer()
    let player = PlayerModel(volumePreferences: preferences, audio: audio)
    #expect(player.volume == 1 && audio.volume == 1)
    player.setVolume(-5)
    #expect(player.volume == 0 && audio.volume == 0)
    player.setVolume(.nan); player.setVolume(.infinity)
    #expect(player.volume == 0 && audio.volume == 0)
    player.setVolume(9)
    #expect(player.volume == 1 && audio.volume == 1)
    player.stop()
}
#endif

@MainActor @Test func listeningHistoryIsBoundedDeduplicatedAndScopedToAccountAndOrigin() throws {
    let suite = "org.ytmdl.tests.history.\(UUID().uuidString)"
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let store = ListeningHistory(preferences: preferences)
    let first = try ServerAddress("https://fixture.example")
    store.configure(server: first, userID: "alice", persist: true)
    for index in 0..<45 { store.record(Track(id: "t\(index)", title: "Fixture", artists: [], album: "", durationMs: 1000)) }
    #expect(store.tracks.count == 40 && store.tracks.first?.id == "t44")
    store.record(store.tracks[5])
    #expect(store.tracks.count == 40 && store.tracks.first?.id == "t39")
    let restored = ListeningHistory(preferences: preferences)
    restored.configure(server: try ServerAddress("https://fixture.example/"), userID: "alice", persist: true)
    #expect(restored.tracks == store.tracks)
    restored.configure(server: first, userID: "bob", persist: true)
    #expect(restored.tracks.isEmpty)
    restored.configure(server: try ServerAddress("https://other.fixture.example"), userID: "alice", persist: true)
    #expect(restored.tracks.isEmpty)
    restored.configure(server: first, userID: "alice", persist: true)
    #expect(restored.tracks.count == 40)
    restored.clear()
    let cleared = ListeningHistory(preferences: preferences)
    cleared.configure(server: first, userID: "alice", persist: true)
    #expect(cleared.tracks.isEmpty)
}
@MainActor @Test func listeningHistoryCanStopRecordingAndFixturesNeverPersist() throws {
    let suite = "org.ytmdl.tests.history.\(UUID().uuidString)"
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let store = ListeningHistory(preferences: preferences)
    let server = try ServerAddress("https://fixture.example")
    let track = Track(id: "one", title: "Fixture", artists: [], album: "", durationMs: 1000)
    store.configure(server: server, userID: "alice", persist: false)
    store.record(track)
    #expect(store.tracks.count == 1)
    let restored = ListeningHistory(preferences: preferences)
    restored.configure(server: server, userID: "alice", persist: true)
    #expect(restored.tracks.isEmpty)
    store.setEnabled(false); store.record(Track(id: "two", title: "Fixture", artists: [], album: "", durationMs: 1000))
    #expect(store.tracks.count == 1)
    #expect(!ListeningHistory(preferences: preferences).enabled)
    store.configure(server: nil, userID: nil, persist: false)
    #expect(store.tracks.isEmpty)
    store.setEnabled(true); store.record(track)
    #expect(store.tracks.isEmpty)
}
