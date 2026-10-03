import Foundation
import YTMDLCore

// Never follow an API or artwork redirect carrying a session to a new origin.
// YTMDL's library stream and artwork endpoints serve bytes at their own origin.
final class OriginSessionDelegate: NSObject, URLSessionTaskDelegate, @unchecked Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

@MainActor final class APIClient {
    let server: ServerAddress
    let session: URLSession
    private let delegate = OriginSessionDelegate()
    private let decoder: JSONDecoder
    private let persist: Bool
    private let cookies: HTTPCookieStorage
    init(server: ServerAddress, persist: Bool = true, configuration: URLSessionConfiguration = .ephemeral) throws {
        self.server = server; self.persist = persist
        configuration.urlCache = nil
        configuration.timeoutIntervalForRequest = 20
        configuration.timeoutIntervalForResource = 60
        configuration.httpAdditionalHeaders = ["User-Agent": "YTMDL-Apple/0.1"]
        configuration.httpCookieAcceptPolicy = .always
        cookies = configuration.httpCookieStorage!
        decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
        session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        if persist, let data = try SessionVault.read(server.url.absoluteString) {
            for value in try JSONDecoder().decode([StoredCookie].self, from: data) {
                guard ["ytmdl_session", "ytmdl_csrf"].contains(value.name), value.expiresAt.map({ $0 > Date() }) ?? false else { continue }
                var properties: [HTTPCookiePropertyKey: Any] = [.name: value.name, .value: value.value,
                    .domain: server.url.host!, .path: "/", .expires: value.expiresAt!, .secure: server.isSecure ? "TRUE" : "FALSE"]
                properties[.originURL] = server.url
                if let cookie = HTTPCookie(properties: properties) { cookies.setCookie(cookie) }
            }
        }
    }
    func invalidate() { session.invalidateAndCancel() }
    var authenticationCookies: [HTTPCookie] {
        (cookies.cookies(for: server.url) ?? []).filter { ["ytmdl_session", "ytmdl_csrf"].contains($0.name) }
    }
    private func saveCookies() throws {
        guard persist else { return }
        let stored = authenticationCookies.map { StoredCookie(name: $0.name, value: $0.value, expiresAt: $0.expiresDate) }
        try SessionVault.write(JSONEncoder().encode(stored), origin: server.url.absoluteString)
    }
    func request(_ path: String, method: String = "GET", body: [String: String]? = nil, query: [URLQueryItem] = []) throws -> URLRequest {
        var request = URLRequest(url: try server.endpoint(path, query: query))
        request.httpMethod = method
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if method != "GET" && method != "HEAD" {
            guard let csrf = authenticationCookies.first(where: { $0.name == "ytmdl_csrf" }) else { throw PlayerError.badResponse }
            request.setValue(csrf.value, forHTTPHeaderField: "X-CSRF-Token")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        return request
    }
    private func perform(_ request: URLRequest) async throws -> Data {
        let (data, response) = try await session.data(for: request)
        guard let http = response as? HTTPURLResponse, data.count <= 8 * 1024 * 1024 else { throw PlayerError.badResponse }
        if !(200..<300).contains(http.statusCode) {
            struct Failure: Decodable { let error: Detail; struct Detail: Decodable { let code: String; let message: String } }
            let failure = try? decoder.decode(Failure.self, from: data)
            throw PlayerError.server(status: http.statusCode, code: failure?.error.code ?? "HTTP_\(http.statusCode)",
                                    message: failure?.error.message ?? "Die Anfrage konnte nicht abgeschlossen werden.")
        }
        try saveCookies()
        return data
    }
    func get<T: Decodable & Sendable>(_ path: String, query: [URLQueryItem] = []) async throws -> T {
        let data = try await perform(request(path, query: query))
        return try decoder.decode(Envelope<T>.self, from: data).data
    }
    func send<T: Decodable & Sendable>(_ path: String, method: String = "POST", body: [String: String]) async throws -> T {
        let data = try await perform(request(path, method: method, body: body))
        return try decoder.decode(Envelope<T>.self, from: data).data
    }
    func mutate(_ path: String, method: String, body: [String: String]? = nil) async throws {
        _ = try await perform(request(path, method: method, body: body))
    }
    func login(username: String, password: String) async throws -> User {
        let _: AuthStatus = try await get("/auth/status")
        return try await send("/auth/login", body: ["username": username, "password": password])
    }
    func forget() throws {
        for cookie in cookies.cookies ?? [] { cookies.deleteCookie(cookie) }
        if persist { try SessionVault.remove(server.url.absoluteString) }
    }
    func artworkRequest(kind: String, id: String) throws -> URLRequest {
        var request = try request(server.itemPath(kind, id: id, suffix: "/artwork"))
        request.setValue("image/*", forHTTPHeaderField: "Accept")
        return request
    }
}
