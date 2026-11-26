import Foundation

// Wire models and pure listening rules shared with the web player (v1.10).
// Kept free of UI and networking so they are unit-tested in YTMDLCoreTests.

/// One automatic mix of the signed-in user (`GET /library/mixes`). Mixes are
/// stable for a calendar day and change the next.
public struct PersonalMix: Decodable, Identifiable, Hashable, Sendable {
    public let id: String
    /// `genre`, `on_repeat`, `rediscover`, `unheard` or `new`.
    public let kind: String
    public let title: String
    public let description: String
    public let genre: String?
    public let trackCount: Int
    /// Up to four tracks for a cover collage, different releases first.
    public let coverTrackIds: [String]
    public let coverReleaseIds: [String]
    /// Where "Für dich" shows the mix (`made_for_you`, `discover`, `rewind`);
    /// newer servers send it, older ones leave it out. See `MixGroup`.
    public let group: String?
    public init(id: String, kind: String, title: String, description: String = "", genre: String? = nil,
                trackCount: Int, coverTrackIds: [String] = [], coverReleaseIds: [String] = [], group: String? = nil) {
        self.id = id; self.kind = kind; self.title = title; self.description = description; self.genre = genre
        self.trackCount = trackCount; self.coverTrackIds = coverTrackIds; self.coverReleaseIds = coverReleaseIds
        self.group = group?.isEmpty == false ? group : nil
    }
    enum CodingKeys: String, CodingKey { case id, kind, title, description, genre, trackCount, coverTrackIds, coverReleaseIds, group }
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        kind = try c.decodeIfPresent(String.self, forKey: .kind) ?? ""
        title = try c.decode(String.self, forKey: .title)
        description = try c.decodeIfPresent(String.self, forKey: .description) ?? ""
        let genre = try c.decodeIfPresent(String.self, forKey: .genre)
        self.genre = genre?.isEmpty == false ? genre : nil
        trackCount = try c.decodeIfPresent(Int.self, forKey: .trackCount) ?? 0
        // Go serializes an empty slice as null.
        coverTrackIds = try c.decodeIfPresent([String].self, forKey: .coverTrackIds) ?? []
        coverReleaseIds = try c.decodeIfPresent([String].self, forKey: .coverReleaseIds) ?? []
        // Optional and new: a value the app cannot read never drops the mix.
        let group = (try? c.decodeIfPresent(String.self, forKey: .group)) ?? nil
        self.group = group?.isEmpty == false ? group : nil
    }
    public var isGenreMix: Bool { kind == "genre" }
    /// The small line above a mix title: its genre or where it comes from (as on the web).
    public var label: String {
        switch kind {
        case "genre": genre ?? "Genre-Mix"
        case "on_repeat": "Meistgehört"
        case "rediscover": "Aus deinem Verlauf"
        case "unheard": "Entdecken"
        case "new": "Neuzugänge"
        default: "Mix"
        }
    }
    /// How a mix is shown in the queue: "Weiter aus: {Mix}".
    public var queueContext: QueueContext { QueueContext(kind: "mix", id: id, title: title) }
    /// The suggested name when a mix is saved as a playlist, as on the web:
    /// "{title} · dd.mm.yyyy", shortened to the server's 128 bytes.
    public static func playlistName(_ title: String, date: Date, calendar: Calendar = .current) -> String {
        let parts = calendar.dateComponents([.day, .month, .year], from: date)
        func two(_ value: Int?) -> String { let text = String(value ?? 1); return text.count < 2 ? "0" + text : text }
        let suffix = " · \(two(parts.day)).\(two(parts.month)).\(parts.year ?? 2000)"
        var name = title.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.isEmpty { name = "Mix" }
        while !name.isEmpty && (name + suffix).utf8.count > 128 { name.removeLast() }
        return name + suffix
    }
    /// Genre mixes ("Dein Mix 1…n") first, then the mixes from listening, in server order.
    public static func ordered(_ mixes: [PersonalMix]) -> [PersonalMix] {
        mixes.filter(\.isGenreMix) + mixes.filter { !$0.isGenreMix }
    }
}

/// The track order on a mix page, as on the web's mix page (`ForYou.tsx`):
/// the mix's own order, or one column ascending or descending. Equal values
/// keep the mix order; text compares case-insensitively.
public enum MixTrackSort: String, CaseIterable, Identifiable, Sendable {
    case mix, title, artist, album, duration, trackNumber
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .mix: "Mix-Reihenfolge"
        case .title: "Titel"
        case .artist: "Künstler"
        case .album: "Album"
        case .duration: "Dauer"
        case .trackNumber: "Titelnummer"
        }
    }
    private enum Key: Comparable {
        case text(String), number(Int)
        static func < (a: Key, b: Key) -> Bool {
            switch (a, b) {
            case (.text(let x), .text(let y)): x < y
            case (.number(let x), .number(let y)): x < y
            default: false
            }
        }
    }
    private func key(_ track: Track) -> Key {
        switch self {
        case .mix: .number(0)
        case .title: .text(track.title.lowercased())
        case .artist: .text(Self.artist(track).lowercased())
        case .album: .text(track.album.lowercased())
        case .duration: .number(track.durationMs)
        case .trackNumber: .number(track.trackNumber ?? 0)
        }
    }
    /// The first artist, else the album artist, like the web.
    private static func artist(_ track: Track) -> String {
        if let first = track.artists.first, !first.isEmpty { return first }
        return track.albumArtist ?? ""
    }
    public func apply(_ tracks: [Track], descending: Bool = false) -> [Track] {
        guard self != .mix else { return tracks }
        return tracks.enumerated().map { (offset: $0.offset, track: $0.element, key: key($0.element)) }
            .sorted { a, b in
                if a.key == b.key { return a.offset < b.offset }
                return descending ? b.key < a.key : a.key < b.key
            }
            .map { $0.track }
    }
}

/// One mix with its tracks (`GET /library/mixes/{id}`, up to 50 tracks).
public struct PersonalMixDetail: Decodable, Sendable {
    public let mix: PersonalMix
    public let tracks: [Track]
    enum CodingKeys: String, CodingKey { case tracks }
    public init(from decoder: any Decoder) throws {
        mix = try PersonalMix(from: decoder)
        tracks = try decoder.container(keyedBy: CodingKeys.self).decodeIfPresent([Track].self, forKey: .tracks) ?? []
    }
}

/// A library track with the user's play statistics (`GET /history`,
/// `GET /library/artists/{id}/top-tracks`).
public struct HistoryTrack: Decodable, Identifiable, Sendable {
    public let track: Track
    public let playCount: Int
    public let lastPlayedAt: Date?
    public var id: String { track.id }
    public init(track: Track, playCount: Int, lastPlayedAt: Date? = nil) {
        self.track = track; self.playCount = playCount; self.lastPlayedAt = lastPlayedAt
    }
    enum CodingKeys: String, CodingKey { case playCount, lastPlayedAt }
    public init(from decoder: any Decoder) throws {
        track = try Track(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        playCount = (try? c.decodeIfPresent(Int.self, forKey: .playCount)) ?? 0
        // A timestamp the app cannot read only hides the "when"; it never drops the row.
        lastPlayedAt = ((try? c.decodeIfPresent(String.self, forKey: .lastPlayedAt)) ?? nil).flatMap(Self.parseTimestamp)
    }
    /// Go writes RFC 3339 with up to nine fractional digits; Foundation reads three.
    public static func parseTimestamp(_ text: String) -> Date? {
        var value = text
        if let dot = value.firstIndex(of: "."), let end = value[dot...].firstIndex(where: { !$0.isNumber && $0 != "." }) {
            let digits = value[value.index(after: dot)..<end]
            value.replaceSubrange(dot..<end, with: "." + String(digits.prefix(3)).padding(toLength: 3, withPad: "0", startingAt: 0))
        }
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = value.contains(".") ? [.withInternetDateTime, .withFractionalSeconds] : [.withInternetDateTime]
        return formatter.date(from: value)
    }
}

/// What the server learns from: a counted play or a skip.
public enum ListeningEventKind: String, Codable, Sendable {
    case play, skip
    /// `POST /history` and `POST /history/skips`, both `{track_id, event_id}`.
    public var path: String { self == .play ? "/history" : "/history/skips" }
}

/// The play/skip rules of the web player (`listeningTracker.ts`). Only actual
/// playing wall time counts, so seeks and buffering never create plays.
///
/// - A play is counted once per track start after `min(30 s, half the
///   duration / rate)` of listening.
/// - Moving on actively to another track is a skip when at least two seconds
///   were heard and the track had not counted as a play yet. Not after the
///   track stood paused for more than three minutes: the user left and came back.
public struct ListeningTracker: Sendable {
    public static let skipMinimumSeconds = 2.0
    public static let skipMaximumPauseSeconds = 180.0
    public static func playThreshold(duration: Double, rate: Double = 1) -> Double {
        min(30, duration / 2 / max(0.5, rate.isFinite ? rate : 1))
    }
    private var key = ""
    private var last: Double?
    private var elapsed = 0.0
    private var sent = false
    private var skipped = false
    private var threshold: Double?
    private var heardAt: Double?
    public init() {}
    public var listenedSeconds: Double { elapsed }
    public mutating func reset() { self = ListeningTracker() }
    /// Feed the playback state; `now` is a monotonic clock in seconds. Returns
    /// true exactly once per track start, when the play counts.
    public mutating func tick(key: String, playing: Bool, now: Double, duration: Double, rate: Double = 1) -> Bool {
        if key != self.key { reset(); self.key = key }
        guard now.isFinite else { return false }
        if let last, playing { elapsed += max(0, min(1, now - last)) }
        last = playing ? now : nil
        if playing { heardAt = now }
        if duration.isFinite, duration > 0 { threshold = Self.playThreshold(duration: duration, rate: rate) }
        guard playing, !sent, let threshold else { return false }
        if elapsed >= threshold { sent = true; return true }
        return false
    }
    /// The user actively moves on from the track `key`. True at most once per
    /// track start; see the type documentation for the rules.
    public mutating func takeSkip(key: String, now: Double, playing: Bool) -> Bool {
        guard !key.isEmpty, key == self.key, !sent, !skipped else { return false }
        guard elapsed >= Self.skipMinimumSeconds else { return false }
        if !playing {
            guard let heardAt, now - heardAt <= Self.skipMaximumPauseSeconds else { return false }
        }
        if let threshold, elapsed >= threshold { return false }
        skipped = true
        return true
    }
    /// Whether a change of the current track may be a skip at all: a track
    /// that failed to play, or staying on the same track (restart, pause, a
    /// cleared queue), never is. Natural ends reset the tracker instead.
    public static func movesOn(from: String?, to: String?, failed: Bool) -> Bool {
        guard let from, let to, !failed else { return false }
        return from != to
    }
}

/// Where a queue entry came from: `context` is what was started (an album, a
/// playlist, a mix …), `user` what was queued on top of it ("Als Nächstes"),
/// `autoplay` similar tracks appended when the queue was about to run out.
public enum QueueOrigin: String, Codable, Sendable { case user, context, autoplay }

/// The tracks played in this listening session, oldest first, so autoplay
/// top-ups do not bring them back after a new queue replaced the old one
/// (web: the last 200 played IDs). A session belongs to one account on one
/// server; another scope starts a new, empty session.
public struct SessionPlayHistory: Equatable, Sendable {
    public static let limit = 200
    public private(set) var scope: String?
    public private(set) var ids: [String] = []
    public init() {}
    public mutating func setScope(_ value: String?) {
        guard value != scope else { return }
        scope = value; ids = []
    }
    public mutating func record(_ id: String) {
        guard !id.isEmpty else { return }
        ids.removeAll { $0 == id }
        ids.append(id)
        if ids.count > Self.limit { ids.removeFirst(ids.count - Self.limit) }
    }
}

/// What the queue was started from, shown as "Weiter aus: {title}".
public struct QueueContext: Codable, Hashable, Sendable {
    /// `album`, `artist`, `playlist`, `mix`, `favorites`, `radio`, `history` …
    public let kind: String
    public let id: String?
    public let title: String
    public init(kind: String, id: String? = nil, title: String) { self.kind = kind; self.id = id; self.title = title }
}

/// One row of a queue list: the entry's position, its section and whether a
/// section heading is drawn above it.
public struct QueueRow: Identifiable, Sendable {
    public let position: Int
    public let track: Track
    public let section: QueueSection
    public let startsSection: Bool
    public var id: Int { position }
}

extension PlaybackQueue {
    /// The rows of a queue list, optionally filtered. A heading starts each
    /// run of visible rows of the same section.
    public func rows(matching include: (Track) -> Bool = { _ in true }) -> [QueueRow] {
        var rows: [QueueRow] = []
        var previous: QueueSection?
        for position in tracks.indices where include(tracks[position]) {
            let section = self.section(at: position)
            rows.append(QueueRow(position: position, track: tracks[position], section: section, startsSection: section != previous))
            previous = section
        }
        return rows
    }
    public func count(of section: QueueSection) -> Int {
        tracks.indices.filter { self.section(at: $0) == section }.count
    }
}

/// The sections a queue list is drawn in, in order.
public enum QueueSection: String, Sendable {
    case played, current, user, context, autoplay
    public func heading(_ context: QueueContext?) -> String {
        switch self {
        case .played: "Zuvor gespielt"
        case .current: "Aktueller Titel"
        case .user: "Als Nächstes"
        // A radio names itself ("Radio: Nachtfahrt"); its top-ups continue it.
        case .context: context.map { $0.kind == "radio" ? $0.title : "Weiter aus: \($0.title)" } ?? "Weiter in der Warteschlange"
        case .autoplay: context?.kind == "radio" ? "Mehr aus diesem Radio" : "Ähnliche Titel"
        }
    }
}
