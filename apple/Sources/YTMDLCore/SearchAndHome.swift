import Foundation

/// How well a name matches what was typed, for the search's "Top-Treffer".
/// Case and diacritics are ignored: "bjork" finds "Björk".
public enum SearchMatch {
    /// 3 the whole name, 2 the start of the name or of one of its words,
    /// 1 anywhere in the name, 0 not at all.
    public static func score(_ text: String, query: String) -> Int {
        let needle = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !needle.isEmpty, !text.isEmpty else { return 0 }
        let options: String.CompareOptions = [.caseInsensitive, .diacriticInsensitive]
        let name = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if name.compare(needle, options: options) == .orderedSame { return 3 }
        if name.range(of: needle, options: options.union(.anchored)) != nil { return 2 }
        let words = name.split { $0.isWhitespace || "-/(&,.".contains($0) }
        if words.contains(where: { String($0).range(of: needle, options: options.union(.anchored)) != nil }) { return 2 }
        return name.range(of: needle, options: options) != nil ? 1 : 0
    }

    /// The kinds of answers, in the order a tie is decided.
    public enum Kind: Int, Comparable, Sendable {
        case artist, release, track, playlist
        public static func < (lhs: Kind, rhs: Kind) -> Bool { lhs.rawValue < rhs.rawValue }
    }

    /// The best single answer: the highest score, then artist before album
    /// before title before playlist, then the server's order. When no name
    /// contains the query (the server matched another field), the first title,
    /// album or artist the server sent, in that order.
    public static func topHit(query: String, artists: [String], releases: [String], tracks: [String], playlists: [String]) -> (kind: Kind, index: Int)? {
        var best: (kind: Kind, index: Int, score: Int)?
        let groups: [(Kind, [String])] = [(.artist, artists), (.release, releases), (.track, tracks), (.playlist, playlists)]
        for (kind, names) in groups {
            for (index, name) in names.enumerated() {
                let value = score(name, query: query)
                guard value > 0 else { continue }
                if best == nil || value > best!.score { best = (kind, index, value) }
            }
        }
        if let best { return (best.kind, best.index) }
        if !tracks.isEmpty { return (.track, 0) }
        if !releases.isEmpty { return (.release, 0) }
        if !artists.isEmpty { return (.artist, 0) }
        return nil
    }
}

/// The last searches of one account on one server, newest first.
public struct RecentSearches: Equatable, Sendable {
    public static let limit = 10
    public private(set) var entries: [String]
    /// Restores a saved list, dropping what `record` would not have kept.
    public init(_ saved: [String] = []) {
        entries = []
        for entry in saved.reversed() { record(entry) }
    }
    /// Adds a search at the top; the same text in other case moves up instead
    /// of appearing twice. Shorter than two characters is not a search.
    public mutating func record(_ query: String) {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard text.count >= 2, text.count <= 200 else { return }
        entries.removeAll { $0.compare(text, options: [.caseInsensitive, .diacriticInsensitive]) == .orderedSame }
        entries.insert(text, at: 0)
        if entries.count > Self.limit { entries.removeLast(entries.count - Self.limit) }
    }
    public mutating func remove(_ query: String) { entries.removeAll { $0 == query } }
    public mutating func clear() { entries = [] }
    /// The preferences key: one list per server address and account.
    public static func storageKey(server: String, userID: String) -> String {
        "recentSearches." + server.lowercased() + "|" + userID
    }
}

/// The start screen's title by time of day.
public enum HomeGreeting {
    public static func text(hour: Int) -> String {
        switch hour {
        case 5..<11: "Guten Morgen"
        case 11..<18: "Guten Tag"
        default: "Guten Abend"
        }
    }
}
