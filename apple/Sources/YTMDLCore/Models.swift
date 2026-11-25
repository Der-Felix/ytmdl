import Foundation

public struct Envelope<T: Decodable & Sendable>: Decodable, Sendable {
    public let data: T
    public let meta: ListMeta?
}
public struct ListMeta: Decodable, Sendable { public let total: Int? }
public struct User: Codable, Sendable {
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
extension Release {
    private enum CodingKeys: String, CodingKey { case id, title, artists, year, trackCountInLibrary }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        artists = try c.decodeIfPresent([String].self, forKey: .artists) ?? []
        year = try c.decode(Int.self, forKey: .year)
        trackCountInLibrary = try c.decodeIfPresent(Int.self, forKey: .trackCountInLibrary)
    }
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
    enum CodingKeys: String, CodingKey { case id, title, artists, album, durationMs, codec }
    // The server serializes a track without artist tags as `"artists": null`.
    // One such row must not make a whole page of tracks undecodable.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        title = try c.decode(String.self, forKey: .title)
        artists = try c.decodeIfPresent([String].self, forKey: .artists) ?? []
        album = try c.decodeIfPresent(String.self, forKey: .album) ?? ""
        durationMs = try c.decode(Int.self, forKey: .durationMs)
        codec = try c.decodeIfPresent(String.self, forKey: .codec)
    }
}
public struct SmartPlaylistRules: Codable, Equatable, Sendable {
    public var genre: String?
    public var artistId: String?
    public var favorites: Bool
    public var addedDays: Int
    public var sort: String
    public var limit: Int
    public init(genre: String? = nil, artistId: String? = nil, favorites: Bool = false,
                addedDays: Int = 0, sort: String = "recent", limit: Int = 50) {
        self.genre = genre; self.artistId = artistId; self.favorites = favorites
        self.addedDays = addedDays; self.sort = sort; self.limit = limit
    }
    enum CodingKeys: String, CodingKey { case genre, artistId, favorites, addedDays, sort, limit }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        genre = try c.decodeIfPresent(String.self, forKey: .genre)
        artistId = try c.decodeIfPresent(String.self, forKey: .artistId)
        favorites = try c.decodeIfPresent(Bool.self, forKey: .favorites) ?? false
        addedDays = try c.decodeIfPresent(Int.self, forKey: .addedDays) ?? 0
        sort = try c.decode(String.self, forKey: .sort); limit = try c.decode(Int.self, forKey: .limit)
    }
}
public struct Playlist: Decodable, Identifiable, Sendable {
    public let id: String
    public let name: String
    public let trackCount: Int
    public let durationMs: Int
    public let description: String?
    public let smartRules: SmartPlaylistRules?
    public init(id: String, name: String, trackCount: Int, durationMs: Int,
                description: String? = nil, smartRules: SmartPlaylistRules? = nil) {
        self.id = id; self.name = name; self.trackCount = trackCount; self.durationMs = durationMs
        self.description = description; self.smartRules = smartRules
    }
}
public struct PlaylistDetail: Decodable, Sendable {
    public let tracks: [Track]
    public let id: String?
    public let name: String?
    public let description: String?
    public let smartRules: SmartPlaylistRules?
    public func playlist(fallback: Playlist) -> Playlist {
        Playlist(id: id ?? fallback.id, name: name ?? fallback.name, trackCount: tracks.count,
                 durationMs: tracks.reduce(0) { $0 + $1.durationMs }, description: description ?? fallback.description,
                 smartRules: id == nil ? fallback.smartRules : smartRules)
    }
}
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
    case csrfUnavailable, secureCookieRequiresHTTPS, invalidCredentials, responseTooLarge
    case server(status: Int, code: String, message: String)
    public var errorDescription: String? {
        switch self {
        case .invalidServer: "Bitte eine Server-Adresse ohne Zugangsdaten, Pfad oder Suchparameter eingeben."
        case .insecureServer: "Bitte eine HTTPS-Adresse verwenden. HTTP ist nur im Entwicklungsbuild für lokale Adressen möglich."
        case .insecureConsent: "Für diesen lokalen HTTP-Test bitte die unverschlüsselte Verbindung ausdrücklich erlauben."
        case .invalidID: "Dieser Bibliothekseintrag hat keine gültige ID."
        case .badResponse: "Die Antwort des Servers ist ungültig. Bitte Server-Adresse und Server-Version prüfen."
        case .csrfUnavailable: "Das Sicherheitscookie für die Anmeldung fehlt. Bitte den Server erneut verbinden und die Anmeldung wiederholen."
        case .secureCookieRequiresHTTPS: "Der Server verlangt eine verschlüsselte Anmeldung. Bitte seine HTTPS-Adresse verwenden."
        case .invalidCredentials: "Benutzername oder Passwort ist falsch. Bitte die Eingaben prüfen."
        case .responseTooLarge: "Die Server-Antwort ist zu groß für die App. Bitte Server-Version und Seitengröße prüfen."
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

/// The bounded play queue. Each entry carries its origin (see `QueueOrigin`);
/// the upcoming part is kept in three runs like the web player: first what the
/// user queued, then the rest of the started context, then autoplay tracks.
public struct PlaybackQueue: Equatable, Sendable {
    public static let limit = 500
    /// How many tracks may be left before autoplay looks for similar ones.
    public static let autoplayLookahead = 2
    public private(set) var tracks: [Track] = []
    public private(set) var origins: [QueueOrigin] = []
    /// What the context entries were started from; nil when unknown.
    public private(set) var context: QueueContext?
    public private(set) var index: Int = 0
    public var current: Track? { tracks.indices.contains(index) ? tracks[index] : nil }
    /// Tracks after the current one.
    public var remaining: Int { max(0, tracks.count - 1 - index) }
    public init() {}
    public func origin(at position: Int) -> QueueOrigin { origins.indices.contains(position) ? origins[position] : .context }
    public func section(at position: Int) -> QueueSection {
        if position < index { return .played }
        if position == index { return .current }
        switch origin(at: position) { case .user: return .user; case .context: return .context; case .autoplay: return .autoplay }
    }
    /// Autoplay seeds with the last track the user chose (context or queued).
    public var autoplaySeed: Track? {
        tracks.indices.reversed().first { origin(at: $0) != .autoplay }.map { tracks[$0] } ?? tracks.last
    }
    public mutating func replace(_ tracks: [Track], start: Int = 0, context: QueueContext? = nil) {
        let selected = tracks.indices.contains(start) ? start : 0
        // Large artist/favorites lists must still play the selected track.
        // Begin a new bounded window when selection falls beyond the first 500.
        let lowerBound = selected >= Self.limit ? selected : 0
        self.tracks = Array(tracks.dropFirst(lowerBound).prefix(Self.limit))
        origins = Array(repeating: .context, count: self.tracks.count)
        self.context = self.tracks.isEmpty ? nil : context
        index = selected - lowerBound
    }
    /// Where "Zur Warteschlange" inserts: after the user entries that directly
    /// follow the current track, so queued tracks play before the rest of the
    /// album or playlist, in the order they were queued.
    public var userInsertIndex: Int {
        var position = min(tracks.count, index + 1)
        while position < tracks.count && origin(at: position) == .user { position += 1 }
        return position
    }
    /// "Zur Warteschlange". On an empty queue the track becomes the current one.
    public mutating func append(_ track: Track) { append([track]) }
    @discardableResult public mutating func append(_ list: [Track]) -> Int {
        let added = Array(list.prefix(Self.limit - tracks.count))
        guard !added.isEmpty else { return 0 }
        if tracks.isEmpty { replace(added); return added.count }
        let position = userInsertIndex
        tracks.insert(contentsOf: added, at: position)
        origins.insert(contentsOf: Array(repeating: .user, count: added.count), at: position)
        return added.count
    }
    /// "Als Nächstes": directly after the current track, ahead of anything queued before.
    @discardableResult public mutating func insertNext(_ track: Track) -> Bool { insertNext([track]) > 0 }
    @discardableResult public mutating func insertNext(_ list: [Track]) -> Int {
        let added = Array(list.prefix(Self.limit - tracks.count))
        guard !added.isEmpty else { return 0 }
        if tracks.isEmpty { replace(added); return added.count }
        tracks.insert(contentsOf: added, at: index + 1)
        origins.insert(contentsOf: Array(repeating: .user, count: added.count), at: index + 1)
        return added.count
    }
    /// Similar tracks at the end of a queue that would otherwise run out.
    /// Tracks already in the queue are left out.
    @discardableResult public mutating func appendAutoplay(_ list: [Track]) -> Int {
        guard current != nil else { return 0 }
        var known = Set(tracks.map(\.id))
        let added = list.filter { known.insert($0.id).inserted }.prefix(Self.limit - tracks.count)
        tracks += added; origins += Array(repeating: .autoplay, count: added.count)
        return added.count
    }
    /// Address queue occurrences by position, so duplicate IDs remain independent.
    @discardableResult public mutating func remove(at position: Int) -> Bool {
        guard tracks.indices.contains(position), position != index else { return false }
        alignOrigins()
        tracks.remove(at: position); origins.remove(at: position)
        if position < index { index -= 1 }
        return true
    }
    @discardableResult public mutating func playNext(at position: Int) -> Bool {
        guard position > index + 1, tracks.indices.contains(position) else { return false }
        alignOrigins()
        tracks.insert(tracks.remove(at: position), at: index + 1)
        origins.remove(at: position); origins.insert(.user, at: index + 1)
        return true
    }
    /// Move an occurrence while preserving the currently playing occurrence,
    /// including when another queue entry has the same track ID. An entry
    /// dropped into the upcoming part takes the origin of the entry it now
    /// follows; directly after the current track that is "Als Nächstes".
    @discardableResult public mutating func move(from source: Int, to destination: Int) -> Bool {
        guard tracks.indices.contains(source), tracks.indices.contains(destination), source != destination else { return false }
        alignOrigins()
        let movedCurrent = source == index
        tracks.insert(tracks.remove(at: source), at: destination)
        origins.insert(origins.remove(at: source), at: destination)
        if movedCurrent { index = destination }
        else if source < index && destination >= index { index -= 1 }
        else if source > index && destination <= index { index += 1 }
        if !movedCurrent, destination > index {
            origins[destination] = destination - 1 == index ? .user : origins[destination - 1]
        }
        return true
    }
    @discardableResult public mutating func clearUpcoming() -> Bool {
        guard index + 1 < tracks.count else { return false }
        alignOrigins()
        tracks.removeSubrange((index + 1)...); origins.removeSubrange((index + 1)...)
        return true
    }
    public mutating func next(repeatAll: Bool) -> Bool {
        if index + 1 < tracks.count { index += 1; return true }
        if repeatAll && !tracks.isEmpty { index = 0; return true }
        return false
    }
    public mutating func previous() { index = max(0, index - 1) }
    public mutating func select(_ index: Int) { if tracks.indices.contains(index) { self.index = index } }
    /// Shuffles the upcoming tracks within their sections: queued tracks stay
    /// next, autoplay tracks stay at the end.
    public mutating func shuffleUpcoming() {
        guard index + 1 < tracks.count else { return }
        alignOrigins()
        let upcoming = (index + 1..<tracks.count).map { (tracks[$0], origins[$0]) }
        let ordered = [QueueOrigin.user, .context, .autoplay].flatMap { origin in upcoming.filter { $0.1 == origin }.shuffled() }
        tracks.replaceSubrange((index + 1)..., with: ordered.map { $0.0 })
        origins.replaceSubrange((index + 1)..., with: ordered.map { $0.1 })
    }
    private mutating func alignOrigins() {
        if origins.count < tracks.count { origins += Array(repeating: .context, count: tracks.count - origins.count) }
        else if origins.count > tracks.count { origins.removeLast(origins.count - tracks.count) }
    }
}
