import Foundation

// The listening endpoints of server 1.11 (docs/features/listening.md): what a
// server offers, radio stations served in batches, listening statistics,
// lyrics hits and the listener's exclusions. Pure models without UI or
// networking, unit-tested in YTMDLCoreTests. Every part degrades to "not
// offered" on an older server.

// MARK: - Server capabilities

/// What the connected server offers beyond 1.10, from `GET /health`'s
/// `features`. A server without the array (1.10.1 and older) offers none;
/// the app decides by these names and never probes an endpoint, because an
/// older server answers an unknown path with 400, not 404.
public struct ServerFeatures: Equatable, Sendable {
    public static let mixGroups = "mixes.groups"
    public static let artistMixes = "mixes.artist"
    public static let decadeMixes = "mixes.decade"
    public static let favoriteMixes = "mixes.favorites"
    public static let daypartMixes = "mixes.daypart"
    public static let radioSeeds = "radio.seeds"
    public static let radioStation = "radio.station"
    public static let historyStats = "history.stats"
    public static let lyricsSearch = "search.lyrics"
    public static let listeningPreferences = "listening.preferences"

    public let names: Set<String>
    /// The server sent the array at all (1.11 and newer), even an empty one.
    public let announced: Bool
    public init(_ names: [String]?) {
        self.names = Set((names ?? []).map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty })
        announced = names != nil
    }
    /// Before the server answered, and on a server without `features`.
    public static let none = ServerFeatures(nil)
    public func has(_ name: String) -> Bool { names.contains(name) }
    /// `GET /library/radio/station` serves titled stations for every seed.
    public var stations: Bool { has(Self.radioStation) }
    /// `GET /history/stats` ("Deine Statistik").
    public var statistics: Bool { has(Self.historyStats) }
    /// `/library/search` answers a `lyrics` group.
    public var lyrics: Bool { has(Self.lyricsSearch) }
    /// `/listening-preferences` ("Weniger davon", exclusions).
    public var preferences: Bool { has(Self.listeningPreferences) }
    /// Mixes take the listener's time zone (`tz`) and carry their group.
    public var mixTimeZone: Bool { has(Self.mixGroups) }
}

/// `GET /health`: only what the app reads. Every field is optional, so a
/// health answer of any version reads.
public struct ServerHealth: Decodable, Sendable {
    public let status: String?
    public let version: String?
    public let features: ServerFeatures
    public init(status: String? = "ok", version: String? = nil, features: [String]? = nil) {
        self.status = status; self.version = version; self.features = ServerFeatures(features)
    }
    enum CodingKeys: String, CodingKey { case status, version, features }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        status = (try? c.decodeIfPresent(String.self, forKey: .status)) ?? nil
        let version = (try? c.decodeIfPresent(String.self, forKey: .version)) ?? nil
        self.version = version?.isEmpty == false ? version : nil
        // Names the app cannot read are left out; the array itself still
        // marks a server that announces its features.
        if c.contains(.features), (try? c.decodeNil(forKey: .features)) == false {
            let raw = (try? c.decode([LenientString].self, forKey: .features)) ?? []
            features = ServerFeatures(raw.compactMap(\.value))
        } else {
            features = .none
        }
    }
    private struct LenientString: Decodable {
        let value: String?
        init(from decoder: any Decoder) throws { value = try? decoder.singleValueContainer().decode(String.self) }
    }
}

// MARK: - Radio stations

/// One batch of `GET /library/radio/station`.
public struct RadioStationPage: Decodable, Sendable {
    public struct Seed: Decodable, Sendable {
        public let kind: String
        public let id: String?
        public let name: String
    }
    public let title: String
    public let seed: Seed?
    public let tracks: [Track]
    /// Pass as `cursor` for the next batch; nil once the station is used up.
    public let nextCursor: String?
    public let remaining: Int
    public init(title: String, tracks: [Track], nextCursor: String?, remaining: Int = 0) {
        self.title = title; self.seed = nil; self.tracks = tracks; self.nextCursor = nextCursor; self.remaining = remaining
    }
    enum CodingKeys: String, CodingKey { case title, seed, tracks, nextCursor, remaining }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        title = ((try? c.decodeIfPresent(String.self, forKey: .title)) ?? nil) ?? ""
        seed = (try? c.decodeIfPresent(Seed.self, forKey: .seed)) ?? nil
        tracks = (try c.decodeIfPresent([LenientTrack].self, forKey: .tracks) ?? []).map(\.track)
        let cursor = (try? c.decodeIfPresent(String.self, forKey: .nextCursor)) ?? nil
        nextCursor = cursor?.isEmpty == false ? cursor : nil
        remaining = ((try? c.decodeIfPresent(Int.self, forKey: .remaining)) ?? nil) ?? 0
    }
}

/// Where a running server station continues: the station (its queue
/// context's id), the shuffle of this start (`nonce`) and the cursor of the
/// next batch. A queue restored after a relaunch has none; its next batch
/// starts the station anew and `exclude` keeps out what is already queued.
public struct StationPaging: Equatable, Sendable {
    public let key: String
    public let nonce: String
    public private(set) var cursor: String?
    /// The server said the station is used up (at most 500 titles).
    public private(set) var exhausted = false
    public init(key: String, nonce: String) { self.key = key; self.nonce = nonce }
    public mutating func advance(_ page: RadioStationPage) {
        cursor = page.nextCursor
        exhausted = page.nextCursor == nil
    }
    /// The ids a batch must leave out: the queue, then what this session
    /// played, each once, newest played first, at most `limit`.
    public static func exclusions(queue: [String], played: [String], limit: Int = 500) -> [String] {
        var seen = Set<String>()
        var result: [String] = []
        for id in queue + played.reversed() where !id.isEmpty && seen.insert(id).inserted {
            result.append(id)
            if result.count == limit { break }
        }
        return result
    }
}

// MARK: - Statistics

/// The windows of "Deine Statistik" (`period`).
public enum StatsPeriod: String, CaseIterable, Identifiable, Sendable {
    case week, month, year, all
    public var id: String { rawValue }
    /// The segment's label.
    public var title: String {
        switch self {
        case .week: "Woche"
        case .month: "Monat"
        case .year: "Jahr"
        case .all: "Gesamt"
        }
    }
    /// What the window covers, under the figures.
    public var caption: String {
        switch self {
        case .week: "Letzte 7 Tage"
        case .month: "Letzte 30 Tage"
        case .year: "Letzte 12 Monate"
        case .all: "Seit Beginn deines Verlaufs"
        }
    }
}

/// `GET /history/stats`. Lists the server writes as null read as empty; a
/// row the app cannot read is left out without dropping the rest.
public struct ListeningStats: Decodable, Sendable {
    public struct Totals: Decodable, Sendable {
        public let plays: Int
        public let distinctTracks: Int
        public let distinctArtists: Int
        /// An estimate: every play counted with the title's full length.
        public let listeningMs: Int
        /// `all` only: plays without a timestamp (not in `activity`).
        public let untimedPlays: Int
        public init(plays: Int = 0, distinctTracks: Int = 0, distinctArtists: Int = 0, listeningMs: Int = 0, untimedPlays: Int = 0) {
            self.plays = plays; self.distinctTracks = distinctTracks; self.distinctArtists = distinctArtists
            self.listeningMs = listeningMs; self.untimedPlays = untimedPlays
        }
        enum CodingKeys: String, CodingKey { case plays, distinctTracks, distinctArtists, listeningMs, untimedPlays }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            func int(_ key: CodingKeys) -> Int { max(0, ((try? c.decodeIfPresent(Int.self, forKey: key)) ?? nil) ?? 0) }
            plays = int(.plays); distinctTracks = int(.distinctTracks); distinctArtists = int(.distinctArtists)
            listeningMs = int(.listeningMs); untimedPlays = int(.untimedPlays)
        }
    }
    public struct RankedTrack: Decodable, Identifiable, Sendable {
        public let track: Track
        public let plays: Int
        public var id: String { track.id }
        enum CodingKeys: String, CodingKey { case track, plays }
        public init(track: Track, plays: Int) { self.track = track; self.plays = plays }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            track = try c.decode(LenientTrack.self, forKey: .track).track
            plays = ((try? c.decodeIfPresent(Int.self, forKey: .plays)) ?? nil) ?? 0
        }
    }
    public struct RankedArtist: Decodable, Identifiable, Sendable {
        public let id: String
        public let name: String
        public let plays: Int
        public let distinctTracks: Int
        enum CodingKeys: String, CodingKey { case id, name, plays, distinctTracks }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            name = ((try? c.decodeIfPresent(String.self, forKey: .name)) ?? nil) ?? ""
            plays = ((try? c.decodeIfPresent(Int.self, forKey: .plays)) ?? nil) ?? 0
            distinctTracks = ((try? c.decodeIfPresent(Int.self, forKey: .distinctTracks)) ?? nil) ?? 0
        }
    }
    public struct RankedRelease: Decodable, Identifiable, Sendable {
        public let id: String
        public let title: String
        public let artistId: String?
        public let artistName: String?
        public let plays: Int
        enum CodingKeys: String, CodingKey { case id, title, artistId, artistName, plays }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            id = try c.decode(String.self, forKey: .id)
            title = ((try? c.decodeIfPresent(String.self, forKey: .title)) ?? nil) ?? ""
            let artistId = (try? c.decodeIfPresent(String.self, forKey: .artistId)) ?? nil
            self.artistId = artistId?.isEmpty == false ? artistId : nil
            let artistName = (try? c.decodeIfPresent(String.self, forKey: .artistName)) ?? nil
            self.artistName = artistName?.isEmpty == false ? artistName : nil
            plays = ((try? c.decodeIfPresent(Int.self, forKey: .plays)) ?? nil) ?? 0
        }
    }
    public struct RankedGenre: Decodable, Identifiable, Sendable {
        public let name: String
        public let plays: Int
        public var id: String { name }
    }
    /// One day or month of `activity`, zeros included.
    public struct ActivityPoint: Decodable, Identifiable, Sendable {
        /// "YYYY-MM-DD", the first day of a month for monthly buckets.
        public let start: String
        public let plays: Int
        public let listeningMs: Int
        public var id: String { start }
        public init(start: String, plays: Int, listeningMs: Int = 0) { self.start = start; self.plays = plays; self.listeningMs = listeningMs }
        enum CodingKeys: String, CodingKey { case start, plays, listeningMs }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            start = try c.decode(String.self, forKey: .start)
            plays = max(0, ((try? c.decodeIfPresent(Int.self, forKey: .plays)) ?? nil) ?? 0)
            listeningMs = max(0, ((try? c.decodeIfPresent(Int.self, forKey: .listeningMs)) ?? nil) ?? 0)
        }
        /// The calendar day the point starts, as year, month and day.
        public var day: DateComponents? {
            let parts = start.split(separator: "-").compactMap { Int($0) }
            guard parts.count == 3, (1...12).contains(parts[1]), (1...31).contains(parts[2]) else { return nil }
            return DateComponents(year: parts[0], month: parts[1], day: parts[2])
        }
        /// Noon of that day in `timeZone`, for a chart's time axis.
        public func date(in timeZone: TimeZone) -> Date? {
            guard var parts = day else { return nil }
            var calendar = Calendar(identifier: .gregorian); calendar.timeZone = timeZone
            parts.hour = 12
            return calendar.date(from: parts)
        }
    }
    public struct Activity: Decodable, Sendable {
        /// `day` (week, month) or `month` (year, all).
        public let bucket: String
        public let points: [ActivityPoint]
        public init(bucket: String, points: [ActivityPoint]) { self.bucket = bucket; self.points = points }
        enum CodingKeys: String, CodingKey { case bucket, points }
        public init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            bucket = ((try? c.decodeIfPresent(String.self, forKey: .bucket)) ?? nil) ?? "day"
            points = LenientList.list(c, .points)
        }
        public var isMonthly: Bool { bucket == "month" }
    }

    public let period: String
    public let from: Date?
    public let to: Date?
    /// The listener's first timestamped play.
    public let since: Date?
    /// False when the history starts inside the window: the figures then
    /// cover `since` to now only.
    public let complete: Bool
    public let source: String
    public let timeZone: String?
    public let totals: Totals
    public let topTracks: [RankedTrack]
    public let topArtists: [RankedArtist]
    public let topReleases: [RankedRelease]
    public let topGenres: [RankedGenre]
    public let activity: Activity

    enum CodingKeys: String, CodingKey { case period, from, to, since, complete, source, timeZone, totals, topTracks, topArtists, topReleases, topGenres, activity }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        func date(_ key: CodingKeys) -> Date? {
            ((try? c.decodeIfPresent(String.self, forKey: key)) ?? nil).flatMap(HistoryTrack.parseTimestamp)
        }
        period = ((try? c.decodeIfPresent(String.self, forKey: .period)) ?? nil) ?? ""
        from = date(.from); to = date(.to); since = date(.since)
        complete = ((try? c.decodeIfPresent(Bool.self, forKey: .complete)) ?? nil) ?? true
        source = ((try? c.decodeIfPresent(String.self, forKey: .source)) ?? nil) ?? ""
        let zone = (try? c.decodeIfPresent(String.self, forKey: .timeZone)) ?? nil
        timeZone = zone?.isEmpty == false ? zone : nil
        totals = ((try? c.decodeIfPresent(Totals.self, forKey: .totals)) ?? nil) ?? Totals()
        topTracks = LenientList.list(c, .topTracks)
        topArtists = LenientList.list(c, .topArtists)
        topReleases = LenientList.list(c, .topReleases)
        topGenres = LenientList.list(c, .topGenres)
        activity = ((try? c.decodeIfPresent(Activity.self, forKey: .activity)) ?? nil) ?? Activity(bucket: "day", points: [])
    }
    /// No play in the window.
    public var isEmpty: Bool { totals.plays == 0 && topTracks.isEmpty }
    /// The zone days and months are counted in; the device's when unknown.
    public var zone: TimeZone { timeZone.flatMap(TimeZone.init(identifier:)) ?? .current }
}

/// Reads each element of a list on its own: one the app cannot read is left out.
enum LenientList {
    private struct Element<T: Decodable>: Decodable {
        let value: T?
        init(from decoder: any Decoder) throws { value = try? T(from: decoder) }
    }
    static func list<T: Decodable, K: CodingKey>(_ c: KeyedDecodingContainer<K>, _ key: K) -> [T] {
        let raw = (try? c.decodeIfPresent([Element<T>].self, forKey: key)) ?? nil
        return (raw ?? []).compactMap(\.value)
    }
}

/// Wording and measures of "Deine Statistik".
public enum StatsFormat {
    /// "1 Wiedergabe", "41 Wiedergaben".
    public static func plays(_ count: Int) -> String { count == 1 ? "1 Wiedergabe" : "\(count) Wiedergaben" }
    /// The estimated listening time: "ca. 2 Std. 38 Min.", "ca. 42 Min.",
    /// whole hours from 100 hours on ("ca. 312 Std.").
    public static func listeningTime(_ milliseconds: Int) -> String {
        let minutes = max(0, milliseconds) / 60_000
        if minutes < 60 { return "ca. \(minutes) Min." }
        let hours = minutes / 60
        if hours >= 100 { return "ca. \(hours) Std." }
        return minutes % 60 == 0 ? "ca. \(hours) Std." : "ca. \(hours) Std. \(minutes % 60) Min."
    }
    /// The length of a ranking bar: `value` against the largest value of the
    /// list, at least `minimum` so a row with plays never shows an empty bar.
    public static func share(_ value: Int, of largest: Int, minimum: Double = 0.04) -> Double {
        guard largest > 0, value > 0 else { return 0 }
        return min(1, max(minimum, Double(value) / Double(largest)))
    }
    private static let months = ["Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"]
    /// "3. Februar 2026" in `timeZone`.
    public static func day(_ date: Date, timeZone: TimeZone) -> String {
        var calendar = Calendar(identifier: .gregorian); calendar.timeZone = timeZone
        let parts = calendar.dateComponents([.day, .month, .year], from: date)
        return "\(parts.day ?? 1). \(months[max(0, min(11, (parts.month ?? 1) - 1))]) \(parts.year ?? 2000)"
    }
    /// The note for figures that do not cover the whole window, or nil.
    public static func note(_ stats: ListeningStats) -> String? {
        if stats.totals.untimedPlays > 0 {
            let count = stats.totals.untimedPlays
            return count == 1 ? "1 Wiedergabe ohne Zeitangabe zählt mit, fehlt aber im Verlauf."
                : "\(count) Wiedergaben ohne Zeitangabe zählen mit, fehlen aber im Verlauf."
        }
        guard !stats.complete else { return nil }
        if let since = stats.since { return "Dein Verlauf beginnt am \(day(since, timeZone: stats.zone)). Die Zahlen zählen ab dann." }
        return "Dein Verlauf beginnt innerhalb dieses Zeitraums. Die Zahlen zählen ab dann."
    }
}

// MARK: - Lyrics search

/// A title whose stored lyrics contain what was searched (`lyrics` group).
public struct LyricsHit: Decodable, Identifiable, Sendable {
    public let track: Track
    /// About 45 characters around the match; line breaks as " / ", "…" for cut text.
    public let snippet: String
    /// The matched text exactly as it appears in `snippet`.
    public let match: String
    public var id: String { track.id }
    public init(track: Track, snippet: String, match: String) { self.track = track; self.snippet = snippet; self.match = match }
    enum CodingKeys: String, CodingKey { case track, snippet, match }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        track = try c.decode(LenientTrack.self, forKey: .track).track
        snippet = ((try? c.decodeIfPresent(String.self, forKey: .snippet)) ?? nil) ?? ""
        match = ((try? c.decodeIfPresent(String.self, forKey: .match)) ?? nil) ?? ""
    }
}

/// Which parts of a lyrics snippet to emphasise. Like the server, case,
/// accents and umlauts do not matter ("uber" finds "Über").
public enum LyricsHighlight {
    private static let options: String.CompareOptions = [.caseInsensitive, .diacriticInsensitive]
    /// The server's `match` where the snippet contains it; else every word of
    /// `query` where a word of the snippet starts with it (the last word of a
    /// search is a prefix: "wol" marks "Wol" in "Wolken"). Sorted, never
    /// overlapping.
    public static func ranges(in snippet: String, match: String, query: String) -> [Range<String.Index>] {
        let wanted = match.trimmingCharacters(in: .whitespacesAndNewlines)
        if !wanted.isEmpty, let range = snippet.range(of: wanted, options: options) { return [range] }
        var found: [Range<String.Index>] = []
        let words = query.split { $0.isWhitespace }.map(String.init).filter { !$0.isEmpty }
        for word in words {
            var start = snippet.startIndex
            while start < snippet.endIndex, let range = snippet.range(of: word, options: options, range: start..<snippet.endIndex) {
                if range.lowerBound == snippet.startIndex || !isWordCharacter(snippet[snippet.index(before: range.lowerBound)]) {
                    found.append(range)
                }
                start = range.upperBound > range.lowerBound ? range.upperBound : snippet.index(after: range.lowerBound)
            }
        }
        // Merge overlapping or touching ranges into one.
        var merged: [Range<String.Index>] = []
        for range in found.sorted(by: { $0.lowerBound < $1.lowerBound }) {
            if let last = merged.last, range.lowerBound <= last.upperBound {
                merged[merged.count - 1] = last.lowerBound..<max(last.upperBound, range.upperBound)
            } else {
                merged.append(range)
            }
        }
        return merged
    }
    private static func isWordCharacter(_ character: Character) -> Bool { character.isLetter || character.isNumber }
}

// MARK: - Listening preferences

/// What an exclusion keeps out of mixes and radio.
public enum PreferenceKind: String, CaseIterable, Identifiable, Sendable {
    case artist, genre, track
    public var id: String { rawValue }
    /// The section of "Empfehlungen".
    public var title: String {
        switch self {
        case .artist: "Künstler"
        case .genre: "Genres"
        case .track: "Titel"
        }
    }
}

/// One exclusion (`GET /listening-preferences`).
public struct ListeningPreference: Decodable, Identifiable, Hashable, Sendable {
    /// Ids of exclusions the server has not confirmed yet start with this.
    public static let pendingPrefix = "pending-"
    public let id: String
    public let kind: String
    /// The artist id, the genre name or the track id.
    public let target: String
    /// The artist's name, the track's title or the genre.
    public let name: String
    /// The artist of an excluded title.
    public let artistName: String?
    public let createdAt: Date?
    public init(id: String, kind: PreferenceKind, target: String, name: String, artistName: String? = nil, createdAt: Date? = nil) {
        self.id = id; self.kind = kind.rawValue; self.target = target; self.name = name
        self.artistName = artistName?.isEmpty == false ? artistName : nil; self.createdAt = createdAt
    }
    enum CodingKeys: String, CodingKey { case id, kind, target, name, artistName, createdAt }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decode(String.self, forKey: .kind)
        target = try c.decode(String.self, forKey: .target)
        let name = ((try? c.decodeIfPresent(String.self, forKey: .name)) ?? nil) ?? ""
        self.name = name.isEmpty ? target : name
        let artistName = (try? c.decodeIfPresent(String.self, forKey: .artistName)) ?? nil
        self.artistName = artistName?.isEmpty == false ? artistName : nil
        createdAt = ((try? c.decodeIfPresent(String.self, forKey: .createdAt)) ?? nil).flatMap(HistoryTrack.parseTimestamp)
    }
    public var preferenceKind: PreferenceKind? { PreferenceKind(rawValue: kind) }
    public var isPending: Bool { id.hasPrefix(Self.pendingPrefix) }
    /// Genres compare without case, like the server; ids exactly.
    public func targets(_ kind: PreferenceKind, _ target: String) -> Bool {
        guard self.kind == kind.rawValue else { return false }
        return kind == .genre ? LibraryMatch.normalized(self.target) == LibraryMatch.normalized(target) : self.target == target
    }
}

/// The listener's exclusions, newest first, with the optimistic steps of the
/// app: an exclusion shows at once and is confirmed or rolled back when the
/// server answers; an allowed one leaves at once and comes back on failure.
public struct ListeningPreferences: Equatable, Sendable {
    public private(set) var entries: [ListeningPreference]
    public init(_ entries: [ListeningPreference] = []) {
        var seen = Set<String>()
        self.entries = entries.filter { $0.preferenceKind != nil && seen.insert($0.id).inserted }
    }
    public func contains(_ kind: PreferenceKind, _ target: String) -> Bool { entry(kind, target) != nil }
    public func entry(_ kind: PreferenceKind, _ target: String) -> ListeningPreference? { entries.first { $0.targets(kind, target) } }
    public func entries(of kind: PreferenceKind) -> [ListeningPreference] { entries.filter { $0.kind == kind.rawValue } }
    public var isEmpty: Bool { entries.isEmpty }
    /// Shows an exclusion before the server confirmed it; nil when it exists already.
    @discardableResult public mutating func addPending(_ kind: PreferenceKind, target: String, name: String, artistName: String? = nil,
                                                       id: String = ListeningPreference.pendingPrefix + UUID().uuidString) -> ListeningPreference? {
        guard !target.isEmpty, !contains(kind, target) else { return nil }
        let entry = ListeningPreference(id: id, kind: kind, target: target, name: name.isEmpty ? target : name, artistName: artistName, createdAt: Date())
        entries.insert(entry, at: 0)
        return entry
    }
    /// The server's answer replaces the pending entry (also when the server
    /// returns an exclusion that existed already).
    public mutating func confirm(_ pendingID: String, with saved: ListeningPreference) {
        entries.removeAll { $0.id == saved.id && $0.id != pendingID }
        if let index = entries.firstIndex(where: { $0.id == pendingID }) { entries[index] = saved }
        else if !entries.contains(where: { $0.id == saved.id }) { entries.insert(saved, at: 0) }
    }
    /// Takes an exclusion out and says where it was, for a rollback.
    @discardableResult public mutating func remove(id: String) -> (entry: ListeningPreference, index: Int)? {
        guard let index = entries.firstIndex(where: { $0.id == id }) else { return nil }
        return (entries.remove(at: index), index)
    }
    @discardableResult public mutating func remove(_ kind: PreferenceKind, _ target: String) -> (entry: ListeningPreference, index: Int)? {
        guard let index = entries.firstIndex(where: { $0.targets(kind, target) }) else { return nil }
        return (entries.remove(at: index), index)
    }
    /// Puts back what a failed request took out, at its old place.
    public mutating func restore(_ entry: ListeningPreference, at index: Int) {
        guard !entries.contains(where: { $0.id == entry.id }) else { return }
        entries.insert(entry, at: min(max(0, index), entries.count))
    }
}

/// Which queued titles an exclusion affects, judged on the device: a title
/// in every version (same title and artists), an artist by id or name, a
/// genre through the genres the app knows of the title's artists.
public enum SuggestionFilter {
    public static func matches(_ track: Track, kind: PreferenceKind, target: String, name: String,
                               reference: Track? = nil, artistGenres: [String: [String]] = [:]) -> Bool {
        switch kind {
        case .track:
            if track.id == target { return true }
            guard let reference, !reference.title.isEmpty else { return false }
            return LibraryMatch.normalized(track.title) == LibraryMatch.normalized(reference.title)
                && Set(track.artists.map(LibraryMatch.normalized)) == Set(reference.artists.map(LibraryMatch.normalized))
        case .artist:
            if let id = track.artistId, id == target { return true }
            let wanted = LibraryMatch.normalized(name)
            guard !wanted.isEmpty else { return false }
            return track.artists.contains { LibraryMatch.normalized($0) == wanted }
        case .genre:
            let wanted = LibraryMatch.normalized(target)
            guard !wanted.isEmpty else { return false }
            return track.artists.contains { artist in
                (artistGenres[LibraryMatch.normalized(artist)] ?? []).contains { LibraryMatch.normalized($0) == wanted }
            }
        }
    }
}
