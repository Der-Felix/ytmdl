import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

// Scripted loopback-free fixture (URLProtocol, no sockets): device-code polling,
// session expiry and collection limits. No audio is created or played.
private final class FlowScript: @unchecked Sendable {
    let lock = NSLock()
    var polls: [String] = []
    var total = 0
    func nextPoll() -> String { lock.lock(); defer { lock.unlock() }; return polls.isEmpty ? "pending" : polls.removeFirst() }
    func reset(polls: [String] = [], total: Int = 0) { lock.lock(); self.polls = polls; self.total = total; lock.unlock() }
    func trackTotal() -> Int { lock.lock(); defer { lock.unlock() }; return total }
}
private final class FlowProtocol: URLProtocol, @unchecked Sendable {
    static let script = FlowScript()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "flow.fixture.example" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}
    override func startLoading() {
        let url = request.url!, path = url.path
        var status = 200
        var headers = ["Content-Type": "application/json"]
        var json = #"{"data":[]}"#
        func failure(_ code: Int, _ name: String) { status = code; json = #"{"error":{"code":"\#(name)","message":"Fixture \#(name)"}}"# }
        if path.hasSuffix("/auth/device/poll") {
            switch Self.script.nextPoll() {
            case "timeout": client?.urlProtocol(self, didFailWithError: URLError(.timedOut)); return
            case "503": failure(503, "UNAVAILABLE")
            case "400": failure(400, "INVALID_REQUEST")
            case "404": failure(404, "NOT_FOUND")
            case "authorized":
                headers["Set-Cookie"] = "ytmdl_session=fixture-session; Path=/; Max-Age=3600; HttpOnly"
                json = #"{"data":{"status":"authorized"}}"#
            case let state: json = #"{"data":{"status":"\#(state)"}}"#
            }
        } else if path.hasSuffix("/auth/status") {
            json = #"{"data":{"authenticated":true,"setup_required":false,"user":{"id":"flow","username":"fixture_user","display_name":"Fixture","role":"user"}}}"#
        } else if path.hasSuffix("/library/tracks") {
            let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
            let limit = Int(items.first { $0.name == "limit" }?.value ?? "") ?? 100, offset = Int(items.first { $0.name == "offset" }?.value ?? "") ?? 0
            let tracks = (offset..<min(offset + limit, Self.script.trackTotal())).map { #"{"id":"t\#($0)","title":"T","artists":["A"],"album":"B","duration_ms":1000}"# }
            json = #"{"data":[\#(tracks.joined(separator: ","))]}"#
        }
        let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: headers)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(json.utf8)); client?.urlProtocolDidFinishLoading(self)
    }
}

private let flowUser = try! JSONDecoder().decode(User.self, from: Data(#"{"id":"flow","username":"fixture_user","displayName":"Fixture","role":"user"}"#.utf8))

/// A signed-in model whose client talks to the scripted fixture. Persistence is off.
@MainActor private func flowModel(root: URL) throws -> (AppModel, APIClient) {
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [FlowProtocol.self]
    configuration.httpCookieStorage!.setCookie(HTTPCookie(properties: [.domain: "flow.fixture.example", .path: "/", .name: "ytmdl_csrf", .value: "fixture-only", .secure: "TRUE"])!)
    let client = try APIClient(server: ServerAddress("https://flow.fixture.example"), persist: false, configuration: configuration)
    let model = AppModel(offlineLibrary: OfflineLibrary(root: root, startTransfers: false))
    try model.connect("https://flow.fixture.example", localHTTP: false, persist: false)
    model.client?.invalidate(); model.client = client
    return (model, client)
}
private func scratchRoot() -> URL { FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString) }
private let started = DeviceStart(deviceCode: "fixture-only-opaque-secret", userCode: "ABCD-EFGH", expiresIn: 300, interval: 5)

extension DeviceStart {
    fileprivate init(deviceCode: String, userCode: String, expiresIn: Int, interval: Int) {
        self = try! JSONDecoder().decode(DeviceStart.self, from: Data(#"{"deviceCode":"\#(deviceCode)","userCode":"\#(userCode)","expiresIn":\#(expiresIn),"interval":\#(interval)}"#.utf8))
    }
}

@MainActor @Test func deviceSignInRetriesTransientFailuresBacksOffAndRestoresTheSession() async throws {
    let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
    FlowProtocol.script.reset(polls: ["timeout", "503", "pending", "slow_down", "authorized"])
    let (model, client) = try flowModel(root: root); defer { client.invalidate() }
    var waits: [Int] = []
    let authorized = try await model.completeDeviceSignIn(started) { waits.append($0) }
    #expect(authorized)
    #expect(waits == [5, 5, 5, 5, 10])
    #expect(model.user?.id == "flow")
    #expect(client.authenticationCookies.contains { $0.name == "ytmdl_session" })
}

@MainActor @Test func deviceSignInEndsOnDefinitiveAnswersAndExpiry() async throws {
    let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
    let (model, client) = try flowModel(root: root); defer { client.invalidate() }
    FlowProtocol.script.reset(polls: ["pending", "400"])
    await #expect(throws: PlayerError.server(status: 400, code: "INVALID_REQUEST", message: "Fixture INVALID_REQUEST")) {
        _ = try await model.completeDeviceSignIn(started) { _ in }
    }
    FlowProtocol.script.reset(polls: ["404"])
    await #expect(throws: PlayerError.server(status: 404, code: "NOT_FOUND", message: "Fixture NOT_FOUND")) {
        _ = try await model.completeDeviceSignIn(started) { _ in }
    }
    FlowProtocol.script.reset(polls: ["authorized"])
    let expired = DeviceStart(deviceCode: "x", userCode: "ABCD-EFGH", expiresIn: 0, interval: 5)
    #expect(try await model.completeDeviceSignIn(expired) { _ in } == false)
    #expect(model.user == nil)
}

@MainActor @Test func anExpiredOrRevokedSessionReturnsToSignInButOtherFailuresDoNot() throws {
    let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
    let (model, client) = try flowModel(root: root); defer { client.invalidate() }
    model.user = flowUser
    model.report(PlayerError.server(status: 503, code: "UNAVAILABLE", message: "Fixture"))
    #expect(model.user != nil && model.client === client)
    model.report(PlayerError.server(status: 401, code: "UNAUTHENTICATED", message: "Fixture"))
    #expect(model.user == nil && model.client == nil)
    #expect(model.error?.contains("abgelaufen") == true)
}

@MainActor @Test func collectionLimitNoticeAppearsOnlyWhenTracksWereCutAndNeverForTheSilentSync() async throws {
    let root = scratchRoot(); defer { try? FileManager.default.removeItem(at: root) }
    let (model, client) = try flowModel(root: root); defer { client.invalidate() }
    model.user = flowUser; model.offline.configure(client: client, user: flowUser)
    for (total, announce, expectsNotice) in [(500, true, false), (501, true, true), (501, false, false)] {
        FlowProtocol.script.reset(total: total); model.error = nil
        await model.downloadCollection(.favorites, announceLimit: announce)
        #expect((model.error != nil) == expectsNotice, "total \(total), announce \(announce)")
        #expect(model.offline.records.count == 500)
    }
}
