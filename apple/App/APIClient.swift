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
    private var valid = true
    init(server: ServerAddress, persist: Bool = true, configuration: URLSessionConfiguration = .ephemeral) throws {
        self.server = server; self.persist = persist
        configuration.urlCache = nil
        configuration.timeoutIntervalForRequest = 20
        configuration.timeoutIntervalForResource = 60
        configuration.httpAdditionalHeaders = ["User-Agent": "YTMDL-Apple/0.2"]
        configuration.httpCookieAcceptPolicy = .always
        cookies = configuration.httpCookieStorage!
        decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
        session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        if persist, let data = try SessionVault.read(server.url.absoluteString) {
            for value in try JSONDecoder().decode([StoredCookie].self, from: data) {
                if let cookie = value.cookie(for: server) { cookies.setCookie(cookie) }
            }
        }
    }
    func invalidate() { valid = false; session.invalidateAndCancel() }
    var authenticationCookies: [HTTPCookie] {
        (cookies.cookies(for: server.url) ?? []).filter { ["ytmdl_session", "ytmdl_csrf"].contains($0.name) }
    }
    private func saveCookies() throws {
        guard persist else { return }
        let stored = authenticationCookies.map { StoredCookie(name: $0.name, value: $0.value, expiresAt: $0.expiresDate, secure: $0.isSecure) }
        try SessionVault.write(JSONEncoder().encode(stored), origin: server.url.absoluteString)
    }
    func request(_ path: String, method: String = "GET", body: [String: String]? = nil, query: [URLQueryItem] = []) throws -> URLRequest {
        guard valid else { throw CancellationError() }
        var request = URLRequest(url: try server.endpoint(path, query: query))
        request.httpMethod = method
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if method != "GET" && method != "HEAD" {
            guard let csrf = authenticationCookies.first(where: { $0.name == "ytmdl_csrf" }) else { throw PlayerError.csrfUnavailable }
            guard server.isSecure || !csrf.isSecure else { throw PlayerError.secureCookieRequiresHTTPS }
            request.setValue(csrf.value, forHTTPHeaderField: "X-CSRF-Token")
        }
        if let body {
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
            request.httpBody = try JSONSerialization.data(withJSONObject: body)
        }
        return request
    }
    private func perform(_ request: URLRequest) async throws -> Data {
        try Task.checkCancellation()
        guard valid else { throw CancellationError() }
        let (data, response) = try await session.data(for: request)
        try Task.checkCancellation()
        guard valid else { throw CancellationError() }
        guard let http = response as? HTTPURLResponse else { throw PlayerError.badResponse }
        guard data.count <= 8 * 1024 * 1024 else { throw PlayerError.responseTooLarge }
        // Adopt same-origin auth cookies before the next CSRF request, including
        // transports that do not update their cookie storage automatically.
        let headers = http.allHeaderFields.reduce(into: [String: String]()) { fields, item in
            if let name = item.key as? String, let value = item.value as? String { fields[name] = value }
        }
        if let url = request.url {
            let host = server.url.host!.lowercased()
            for cookie in HTTPCookie.cookies(withResponseHeaderFields: headers, for: url) {
                let domain = cookie.domain.lowercased().trimmingCharacters(in: CharacterSet(charactersIn: "."))
                guard ["ytmdl_session", "ytmdl_csrf"].contains(cookie.name), domain == host else { continue }
                cookies.setCookie(cookie)
            }
        }
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
    // Typed payloads retain booleans, nested rules and arrays; all mutations use
    // the same session validity, origin checks and CSRF protection as sign-in.
    private func jsonRequest<Body: Encodable>(_ path: String, method: String, body: Body) throws -> URLRequest {
        var request = try request(path, method: method)
        let encoder = JSONEncoder(); encoder.keyEncodingStrategy = .convertToSnakeCase
        request.httpBody = try encoder.encode(body)
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        return request
    }
    func sendJSON<T: Decodable & Sendable, Body: Encodable>(_ path: String, method: String, body: Body) async throws -> T {
        let data = try await perform(jsonRequest(path, method: method, body: body))
        return try decoder.decode(Envelope<T>.self, from: data).data
    }
    func mutateJSON<Body: Encodable>(_ path: String, method: String, body: Body) async throws {
        _ = try await perform(jsonRequest(path, method: method, body: body))
    }
    func playlistPath(_ id: String, suffix: String = "") throws -> String {
        // Reuse the strict ID policy without accepting arbitrary path fragments.
        let libraryPath = try server.itemPath("playlists", id: id, suffix: suffix)
        return String(libraryPath.dropFirst("/library".count))
    }
    func login(username: String, password: String) async throws -> User {
        let _: AuthStatus = try await get("/auth/status")
        do {
            return try await send("/auth/login", body: ["username": username, "password": password])
        } catch PlayerError.server(_, "INVALID_CREDENTIALS", _) {
            throw PlayerError.invalidCredentials
        } catch PlayerError.server(_, "CSRF_INVALID", _) {
            throw PlayerError.csrfUnavailable
        }
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
