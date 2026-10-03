import Foundation

public struct Envelope<T: Decodable & Sendable>: Decodable, Sendable {
    public let data: T
    public let meta: ListMeta?
}
public struct ListMeta: Decodable, Sendable { public let total: Int? }
public struct User: Decodable, Sendable {
    public let id: String
    public let username: String
    public let displayName: String
    public let role: String
}
public struct AuthStatus: Decodable, Sendable {
    public let authenticated: Bool
    public let setupRequired: Bool
    public let user: User?
}
public struct Artist: Decodable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let genres: [String]?
    public let trackCount: Int?
}
public struct Release: Decodable, Identifiable, Sendable {
    public let id: String
    public let title: String
    public let artists: [String]
    public let year: Int
    public let trackCountInLibrary: Int?
}
public struct Track: Codable, Identifiable, Equatable, Sendable {
    public let id: String
    public let title: String
    public let artists: [String]
    public let album: String
    public let durationMs: Int
    public let codec: String?
    public var artistText: String { artists.joined(separator: " · ") }
    public var duration: Double { Double(durationMs) / 1000 }
    public init(id: String, title: String, artists: [String], album: String, durationMs: Int, codec: String? = nil) {
        self.id = id; self.title = title; self.artists = artists
        self.album = album; self.durationMs = durationMs; self.codec = codec
    }
}
public struct Playlist: Decodable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let trackCount: Int
    public let durationMs: Int
}
public struct PlaylistDetail: Decodable, Sendable { public let tracks: [Track] }
public struct SearchResults: Decodable, Sendable {
    public let artists: [Artist]
    public let releases: [Release]
    public let tracks: [Track]
}
public struct Lyrics: Decodable, Sendable { public let state: String; public let content: String? }
public struct DeviceStart: Decodable, Sendable {
    public let deviceCode: String
    public let userCode: String
    public let expiresIn: Int
    public let interval: Int
}
public struct DevicePoll: Decodable, Sendable { public let status: String }
public struct DevicePreview: Decodable, Sendable { public let deviceName: String; public let expiresAt: String }

public enum PlayerError: LocalizedError, Equatable {
    case invalidServer, insecureServer, insecureConsent, invalidID, badResponse
    case server(status: Int, code: String, message: String)
    public var errorDescription: String? {
        switch self {
        case .invalidServer: "Bitte eine Server-Adresse ohne Zugangsdaten, Pfad oder Suchparameter eingeben."
        case .insecureServer: "Bitte eine HTTPS-Adresse verwenden. HTTP ist nur im Entwicklungsbuild für lokale Adressen möglich."
        case .insecureConsent: "Für diesen lokalen HTTP-Test bitte die unverschlüsselte Verbindung ausdrücklich erlauben."
        case .invalidID: "Dieser Bibliothekseintrag hat keine gültige ID."
        case .badResponse: "Der Server hat eine unerwartete Antwort gesendet."
        case let .server(status, code, message):
            status == 401 ? "Deine Sitzung ist abgelaufen. Bitte erneut anmelden." : "\(message) (\(code))"
        }
    }
}

public struct ServerAddress: Equatable, Sendable {
    public let url: URL
    public var isSecure: Bool { url.scheme == "https" }
    public init(_ text: String, allowLocalHTTP: Bool = false) throws {
        guard let components = URLComponents(string: text.trimmingCharacters(in: .whitespacesAndNewlines)),
              let host = components.host, !host.isEmpty, components.user == nil, components.password == nil,
              components.query == nil, components.fragment == nil,
              components.path.isEmpty || components.path == "/",
              components.scheme == "https" || components.scheme == "http", let url = components.url,
              components.port == nil || (1...65535).contains(components.port!) else { throw PlayerError.invalidServer }
        if components.scheme == "http" {
            guard allowLocalHTTP, Self.isLocalHost(host) else { throw PlayerError.insecureServer }
        }
        self.url = url
    }
    public static func isLocalHost(_ host: String) -> Bool {
        let host = host.lowercased()
        if host == "localhost" || host == "[::1]" || host == "::1" { return true }
        let parts = host.split(separator: ".", omittingEmptySubsequences: false)
        guard parts.count == 4, parts.allSatisfy({ !$0.isEmpty && $0.allSatisfy({ $0 >= "0" && $0 <= "9" }) && ($0.count == 1 || !$0.hasPrefix("0")) }) else { return false }
        let octets = parts.compactMap { Int($0) }
        guard octets.count == 4, octets.allSatisfy({ (0...255).contains($0) }) else { return false }
        return octets[0] == 10 || octets[0] == 127 || (octets[0] == 192 && octets[1] == 168) || (octets[0] == 172 && (16...31).contains(octets[1]))
    }
    public func endpoint(_ path: String, query: [URLQueryItem] = []) throws -> URL {
        guard path.hasPrefix("/"), !path.contains(".."), !path.contains("?"), !path.contains("#"), !path.contains("%"), !path.contains("\\") else { throw PlayerError.invalidID }
        var components = URLComponents(url: url, resolvingAgainstBaseURL: false)!
        components.path = "/api/v1" + path
        components.queryItems = query.isEmpty ? nil : query
        guard let result = components.url else { throw PlayerError.invalidID }
        return result
    }
    public func itemPath(_ kind: String, id: String, suffix: String = "") throws -> String {
        guard !id.isEmpty, id.count <= 200, id.unicodeScalars.allSatisfy({ CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "_-" )).contains($0) }) else { throw PlayerError.invalidID }
        return "/library/\(kind)/\(id)\(suffix)"
    }
}

public struct PlaybackQueue: Equatable, Sendable {
    public private(set) var tracks: [Track] = []
    public private(set) var index: Int = 0
    public var current: Track? { tracks.indices.contains(index) ? tracks[index] : nil }
    public init() {}
    public mutating func replace(_ tracks: [Track], start: Int = 0) {
        let selected = tracks.indices.contains(start) ? start : 0
        // Large artist/favorites lists must still play the selected track.
        // Begin a new bounded window when selection falls beyond the first 500.
        let lowerBound = selected >= 500 ? selected : 0
        self.tracks = Array(tracks.dropFirst(lowerBound).prefix(500))
        index = selected - lowerBound
    }
    public mutating func append(_ track: Track) { if tracks.count < 500 { tracks.append(track) } }
    public mutating func next(repeatAll: Bool) -> Bool {
        if index + 1 < tracks.count { index += 1; return true }
        if repeatAll && !tracks.isEmpty { index = 0; return true }
        return false
    }
    public mutating func previous() { index = max(0, index - 1) }
    public mutating func select(_ index: Int) { if tracks.indices.contains(index) { self.index = index } }
    public mutating func shuffleUpcoming() { if index+1 < tracks.count { tracks.replaceSubrange((index+1)..., with: tracks[(index+1)...].shuffled()) } }
}
