import Foundation
import CoreGraphics
import ImageIO
import MediaPlayer
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

// Isolated network and metadata checks. No AVPlayerItem is installed or played.
private final class AuditProtocol: URLProtocol, @unchecked Sendable {
    private let lock = NSLock()
    private var cancelled = false
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasSuffix("audit.example") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() { lock.lock(); cancelled = true; lock.unlock() }
    override func startLoading() {
        let path = request.url!.path
        let delay = path.hasSuffix("/auth/logout") || path.hasSuffix("/favorites/t0") || path.contains("/slow/") ? 0.25 : 0.01
        DispatchQueue.global().asyncAfter(deadline: .now() + delay) { [self] in
            lock.lock(); defer { lock.unlock() }; guard !cancelled else { return }
            let payload: Data
            let status: Int
            if path.hasSuffix("/artwork") {
                if path.contains("/missing/") { status = 404; payload = Data() }
                else {
                    status = 200
                    let context = CGContext(data: nil, width: 24, height: 24, bitsPerComponent: 8, bytesPerRow: 96,
                        space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
                    context.setFillColor(red: 0.1, green: path.contains("/slow/") ? 0.8 : 0.2, blue: 0.8, alpha: 1)
                    context.fill(CGRect(x: 0, y: 0, width: 24, height: 24))
                    let data = NSMutableData()
                    let output = CGImageDestinationCreateWithData(data, "public.png" as CFString, 1, nil)!
                    CGImageDestinationAddImage(output, context.makeImage()!, nil); CGImageDestinationFinalize(output)
                    payload = data as Data
                }
            } else {
                status = 200
                let json: String
                if path.hasSuffix("/auth/status") {
                    json = #"{"data":{"authenticated":false,"setup_required":false,"user":{"id":"old","username":"old","display_name":"Old","role":"user"}}}"#
                } else if path.hasSuffix("/lyrics") { json = #"{"data":{"state":"available_plain","content":"Fixture lyrics"}}"# }
                else { json = #"{"data":{}}"# }
                payload = Data(json.utf8)
            }
            let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil,
                headerFields: ["Content-Type": path.hasSuffix("/artwork") ? "image/png" : "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: payload); client?.urlProtocolDidFinishLoading(self)
        }
    }
}
@MainActor private func auditClient() throws -> APIClient {
    let config = URLSessionConfiguration.ephemeral; config.protocolClasses = [AuditProtocol.self]
    let server = try ServerAddress("https://player.audit.example")
    config.httpCookieStorage!.setCookie(HTTPCookie(properties: [.domain: "player.audit.example", .path: "/", .name: "ytmdl_csrf", .value: "fixture-only", .secure: "TRUE"])!)
    return try APIClient(server: server, persist: false, configuration: config)
}
@MainActor private func waitForArtwork(_ player: PlayerModel) async throws {
    for _ in 0..<100 {
        if player.artwork != nil { return }
        try await Task.sleep(for: .milliseconds(20))
    }
    #expect(player.artwork != nil)
}

@MainActor @Test func auditLockScreenArtworkSurvivesMetadataRefreshAndClearsOnMissingCoverAndStop() async throws {
    let suite = "audit-player-" + UUID().uuidString, preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let client = try auditClient(), player = PlayerModel(volumePreferences: preferences)
    defer { player.stop(); client.invalidate() }
    let first = Track(id: "t0", title: "First", artists: ["Artist"], album: "Album", durationMs: 169000)
    player.previewPaused([first], client: client)
    try await waitForArtwork(player)
    let center = MPNowPlayingInfoCenter.default()
    let art = try #require(center.nowPlayingInfo?[MPMediaItemPropertyArtwork] as? MPMediaItemArtwork)
    #expect(art.bounds.width == 24)
    #expect(art.image(at: CGSize(width: 24, height: 24)) != nil)
    #expect(center.nowPlayingInfo?[MPMediaItemPropertyPlaybackDuration] as? Double == 169)
    player.pause(); player.setPlaybackRate(1.5)
    #expect(center.nowPlayingInfo?[MPMediaItemPropertyArtwork] as? MPMediaItemArtwork === art)
    #expect(center.nowPlayingInfo?[MPNowPlayingInfoPropertyPlaybackRate] as? Double == 0)
    player.previewPaused([Track(id: "missing", title: "Missing", artists: [], album: "", durationMs: 1000)], client: client)
    #expect(center.nowPlayingInfo?[MPMediaItemPropertyTitle] as? String == "Missing")
    #expect(center.nowPlayingInfo?[MPMediaItemPropertyArtwork] == nil)
    player.previewPaused([Track(id: "slow", title: "Slow", artists: [], album: "", durationMs: 1000)], client: client)
    try await Task.sleep(for: .milliseconds(40))
    player.previewPaused([first], client: client)
    try await waitForArtwork(player)
    try await Task.sleep(for: .milliseconds(300))
    #expect(center.nowPlayingInfo?[MPMediaItemPropertyTitle] as? String == "First")
    #expect(player.artworkPalette?.accent.green ?? 1 < 0.4)
    player.play([], client: client)
    #expect(player.current == nil && !player.isPlaybackRequested)
    #expect(center.nowPlayingInfo == nil && player.artwork == nil)
}

@MainActor @Test func auditServerSwitchResetsFiltersHandoffAndPendingLogoutCannotClearNewSession() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false))
    let old = try auditClient(); model.client = old
    model.genre = "Old genre"; model.genres = ["Old genre"]; model.releaseOffset = 60; model.moreReleases = true
    model.handoff = try JSONDecoder().decode(PlaybackHandoff.self, from: Data(#"{"id":"old","queue":[],"queueIndex":0,"positionSeconds":0,"repeatMode":"off","sourceName":"Old account"}"#.utf8))
    let pending = Task { await model.logout() }
    try await Task.sleep(for: .milliseconds(40))
    try model.connect("https://replacement.audit.example", localHTTP: false, persist: false)
    let replacement = try #require(model.client)
    #expect(model.handoff == nil && model.genre.isEmpty && model.genres.isEmpty)
    #expect(model.releaseOffset == 0 && !model.moreReleases)
    await pending.value
    #expect(model.client === replacement)
    #expect(model.error == nil)
    replacement.invalidate()
}

@MainActor @Test func auditFavoritesSerializeRapidTapsAndUnauthenticatedStatusCannotRestoreUser() async throws {
    let root = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    defer { try? FileManager.default.removeItem(at: root) }
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false)), client = try auditClient()
    model.client = client; defer { client.invalidate() }
    let track = Track(id: "t0", title: "Fixture", artists: [], album: "", durationMs: 1000)
    let pending = Task { await model.toggleFavorite(track) }
    try await Task.sleep(for: .milliseconds(40))
    #expect(model.pendingFavorites.contains(track.id))
    await model.toggleFavorite(track)
    #expect(model.pendingFavorites.contains(track.id))
    await pending.value
    #expect(model.favoriteIDs == [track.id] && model.pendingFavorites.isEmpty)
    #expect(await model.restore())
    #expect(model.user == nil && model.offline.scope == nil)
}
