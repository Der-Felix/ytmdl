import Foundation

// Cover artwork for genre tiles, built on the device so it works with every
// server: a genre's newest few releases (`/library/releases?genre=…&limit=4`)
// lend their covers. The mapping genre → release IDs is remembered per account
// and server, so tiles show their covers at once on the next launch.

public enum GenreCovers {
    /// Releases asked per genre: as few as a 2 × 2 mosaic needs.
    public static let requestLimit = 4
    /// Requests running at the same time.
    public static let concurrency = 4
    /// Genres remembered across launches; the least recently loaded go first.
    public static let storedLimit = 400
    /// A failed genre is asked again after this long, not on every appearance.
    public static let retryInterval: TimeInterval = 60

    /// The key a genre is stored under: case, diacritics and spaces do not matter.
    public static func key(_ genre: String) -> String { LibraryMatch.normalized(genre) }
    /// Genres that never get covers: the placeholder for albums without genre.
    public static func isBrowsable(_ genre: String) -> Bool { !key(genre).isEmpty && genre != "__none__" }

    /// Up to `limit` release IDs, each once; releases of different artists
    /// first, so a mosaic does not show four covers of one artist when the
    /// genre has more to offer.
    public static func pick(_ releases: [Release], limit: Int = requestLimit) -> [String] {
        var ids = Set<String>(), artists = Set<String>()
        var first: [String] = [], later: [String] = []
        for release in releases where !release.id.isEmpty && ids.insert(release.id).inserted {
            let artist = LibraryMatch.normalized(release.albumArtist ?? release.artists.first ?? "")
            if artist.isEmpty || artists.insert(artist).inserted { first.append(release.id) } else { later.append(release.id) }
        }
        return Array((first + later).prefix(limit))
    }

    public static func storageKey(server: String, userID: String) -> String {
        "genreCovers." + server.lowercased() + "|" + userID
    }
}

/// How a genre tile lays out its covers.
public enum GenreCoverLayout: Equatable, Sendable {
    /// No covers yet or none at all: the genre's own colour.
    case tint
    case single(String)
    /// Two covers side by side.
    case pair(String, String)
    /// Four covers in a 2 × 2 mosaic.
    case mosaic([String])

    public init(_ ids: [String]?) {
        let ids = ids ?? []
        switch ids.count {
        case 0: self = .tint
        case 1: self = .single(ids[0])
        case 2, 3: self = .pair(ids[0], ids[1])
        default: self = .mosaic(Array(ids.prefix(4)))
        }
    }
    public var ids: [String] {
        switch self {
        case .tint: []
        case .single(let id): [id]
        case .pair(let a, let b): [a, b]
        case .mosaic(let ids): ids
        }
    }
}

/// The remembered covers: genre key → release IDs, oldest first so the
/// list can be capped. An empty list means the genre has no releases.
public struct GenreCoverIndex: Equatable, Sendable {
    public private(set) var covers: [String: [String]] = [:]
    private var order: [String] = []
    public init() {}
    public subscript(key: String) -> [String]? { covers[key] }
    public var count: Int { covers.count }
    /// Stores `ids` for `key` as the most recent entry. Returns false when nothing changed.
    @discardableResult public mutating func set(_ key: String, _ ids: [String], limit: Int = GenreCovers.storedLimit) -> Bool {
        guard !key.isEmpty else { return false }
        let changed = covers[key] != ids
        covers[key] = ids
        order.removeAll { $0 == key }; order.append(key)
        while order.count > max(1, limit) { covers[order.removeFirst()] = nil }
        return changed
    }
    private struct Stored: Codable {
        var version = 1
        var entries: [Entry]
        struct Entry: Codable { var key: String; var ids: [String] }
    }
    public func encoded() -> Data? {
        try? JSONEncoder().encode(Stored(entries: order.compactMap { key in covers[key].map { Stored.Entry(key: key, ids: $0) } }))
    }
    /// Tolerant: unreadable data is an empty index, odd entries are dropped.
    public static func decode(_ data: Data?) -> GenreCoverIndex {
        var index = GenreCoverIndex()
        guard let data, let stored = try? JSONDecoder().decode(Stored.self, from: data) else { return index }
        for entry in stored.entries where !entry.key.isEmpty {
            var seen = Set<String>()
            index.set(entry.key, Array(entry.ids.filter { !$0.isEmpty && seen.insert($0).inserted }.prefix(GenreCovers.requestLimit)))
        }
        return index
    }
}

/// Which genre covers to load next. Tiles ask as they appear; each genre is
/// loaded at most once per session, at most `limit` at a time, the most
/// recently asked first (what is on screen now), and a long backlog from
/// fast scrolling is cut. A failed genre waits `GenreCovers.retryInterval`.
public struct GenreCoverQueue: Sendable {
    public let limit: Int
    public let backlog: Int
    public private(set) var waiting: [String] = []
    public private(set) var running: Set<String> = []
    public private(set) var loaded: Set<String> = []
    private var failedAt: [String: Date] = [:]
    public init(limit: Int = GenreCovers.concurrency, backlog: Int = 48) {
        self.limit = max(1, limit); self.backlog = max(1, backlog)
    }
    /// Asks for `key`; false when it waits, runs, was loaded or failed just now.
    @discardableResult public mutating func enqueue(_ key: String, now: Date = Date()) -> Bool {
        guard !key.isEmpty, !running.contains(key), !loaded.contains(key) else { return false }
        if let failed = failedAt[key], now.timeIntervalSince(failed) < GenreCovers.retryInterval { return false }
        if let index = waiting.firstIndex(of: key) {
            // Seen again: it moves to the front.
            waiting.remove(at: index); waiting.append(key); return false
        }
        waiting.append(key)
        if waiting.count > backlog { waiting.removeFirst(waiting.count - backlog) }
        return true
    }
    /// A tile left the screen before its turn.
    public mutating func withdraw(_ key: String) { waiting.removeAll { $0 == key } }
    /// The next genre to load, or nil when `limit` run already or none waits.
    public mutating func next() -> String? {
        guard running.count < limit, let key = waiting.popLast() else { return nil }
        running.insert(key)
        return key
    }
    public mutating func finish(_ key: String, succeeded: Bool, now: Date = Date()) {
        running.remove(key)
        if succeeded { loaded.insert(key); failedAt[key] = nil } else { failedAt[key] = now }
    }
    /// The covers came another way (the genre page loaded its albums).
    public mutating func markLoaded(_ key: String) {
        guard !key.isEmpty else { return }
        waiting.removeAll { $0 == key }; loaded.insert(key); failedAt[key] = nil
    }
    /// The library was loaded anew: loaded and failed genres may be asked once
    /// more; what waits or runs carries on.
    public mutating func forgetLoaded() { loaded = []; failedAt = [:] }
    public mutating func reset() { waiting = []; running = []; loaded = []; failedAt = [:] }
}

/// The newest of several overlapping requests for the same thing wins: an
/// answer that arrives after a newer request started is dropped, whatever
/// order the server answers in (fast typing, switching a period back and forth).
public struct LatestRequests<Key: Hashable & Sendable>: Sendable {
    public struct Ticket: Equatable, Sendable {
        public let key: Key
        fileprivate let number: Int
    }
    private var counter = 0
    private var current: [Key: Int] = [:]
    public init() {}
    /// Starts a request for `key`; any older one for the same key is now stale.
    public mutating func begin(_ key: Key) -> Ticket {
        counter += 1; current[key] = counter
        return Ticket(key: key, number: counter)
    }
    public func isCurrent(_ ticket: Ticket) -> Bool { current[ticket.key] == ticket.number }
    /// Whether a request for `key` is still outstanding.
    public func isRunning(_ key: Key) -> Bool { current[key] != nil }
    /// The request ended; only the current one clears the key.
    public mutating func end(_ ticket: Ticket) { if isCurrent(ticket) { current[ticket.key] = nil } }
}

/// When the start screen's server parts are worth loading again after the
/// app returns to the foreground: after `maxAge`, or once the day changed
/// (the daily mixes are new then).
public enum FeedFreshness {
    public static let maxAge: TimeInterval = 30 * 60
    public static func needsRefresh(loadedAt: Date?, now: Date, maxAge: TimeInterval = maxAge, calendar: Calendar = .current) -> Bool {
        guard let loadedAt else { return false }
        if now.timeIntervalSince(loadedAt) >= maxAge || now < loadedAt { return true }
        return !calendar.isDate(loadedAt, inSameDayAs: now)
    }
}

extension StationPaging {
    /// The paging a finished top-up may store: only while the paging it
    /// started from is still the current one. A station started or replaced
    /// meanwhile keeps its own cursor.
    public static func committing(_ updated: StationPaging, startedFrom before: StationPaging?, current: StationPaging?) -> StationPaging? {
        current == before ? updated : nil
    }
}
