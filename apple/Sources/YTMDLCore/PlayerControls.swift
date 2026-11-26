import Foundation

/// The repeat button's three states, in the order a tap cycles through them.
public enum RepeatMode: String, Sendable, CaseIterable {
    case off, all, one
    /// The player keeps `repeatOne` on top of `repeatAll`; one wins.
    public init(all: Bool, one: Bool) { self = one ? .one : all ? .all : .off }
    /// The state after a tap: off, whole queue, this title, off again.
    public var next: RepeatMode {
        switch self { case .off: .all; case .all: .one; case .one: .off }
    }
    public var isActive: Bool { self != .off }
    public var symbol: String { self == .one ? "repeat.1" : "repeat" }
    /// What the button says to VoiceOver: the current state.
    public var label: String {
        switch self {
        case .off: "Wiederholung aus"
        case .all: "Warteschlange wiederholen"
        case .one: "Einzeltitel wiederholen"
        }
    }
}

/// The arithmetic of the player's scrubber and its time labels.
public enum PlaybackClock {
    /// How far `position` is into `duration`, 0…1. Unknown durations give 0.
    public static func fraction(_ position: Double, of duration: Double) -> Double {
        guard duration.isFinite, duration > 0, position.isFinite else { return 0 }
        return min(1, max(0, position / duration))
    }
    /// The position at `fraction` of `duration`, clamped to the title.
    public static func position(atFraction fraction: Double, of duration: Double) -> Double {
        guard duration.isFinite, duration > 0, fraction.isFinite else { return 0 }
        return min(1, max(0, fraction)) * duration
    }
    /// Time left, never negative; an unknown duration leaves nothing.
    public static func remaining(_ position: Double, of duration: Double) -> Double {
        guard duration.isFinite, duration > 0 else { return 0 }
        let clamped = position.isFinite ? min(max(0, position), duration) : 0
        return duration - clamped
    }
    /// A VoiceOver swipe on the scrubber: `step` seconds on, clamped to the title.
    public static func stepped(_ position: Double, by step: Double, of duration: Double) -> Double {
        let start = position.isFinite ? position : 0
        guard duration.isFinite, duration > 0 else { return max(0, start + step) }
        return min(duration, max(0, start + step))
    }
}

/// Finding a library page for the names a track carries. Tracks know their
/// artists and album by name only; the player looks those names up.
public enum LibraryMatch {
    /// Names compare without case, diacritics and surrounding spaces.
    public static func normalized(_ name: String) -> String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
            .folding(options: [.caseInsensitive, .diacriticInsensitive, .widthInsensitive], locale: nil)
    }
    /// The first candidate with the same name, or nil.
    public static func index(of name: String, in candidates: [String]) -> Int? {
        let wanted = normalized(name)
        guard !wanted.isEmpty else { return nil }
        return candidates.firstIndex { normalized($0) == wanted }
    }
    /// The album named `title` among `candidates` (title and artists each).
    /// An album that shares an artist with the track wins over one that
    /// only shares the title; an album without artists counts as sharing.
    public static func releaseIndex(title: String, artists: [String], in candidates: [(title: String, artists: [String])]) -> Int? {
        let wanted = normalized(title)
        guard !wanted.isEmpty else { return nil }
        let names = Set(artists.map(normalized))
        let sameTitle = candidates.indices.filter { normalized(candidates[$0].title) == wanted }
        return sameTitle.first { candidates[$0].artists.isEmpty || !names.isDisjoint(with: candidates[$0].artists.map(normalized)) }
            ?? (names.isEmpty ? sameTitle.first : nil)
    }
}

extension PlaybackQueue {
    /// A drag in a list that shows the rows at `positions` (queue positions,
    /// in order): the row at `offset` dropped before the row at `insertion`
    /// (`positions.count` drops it after the last). Returns the arguments for
    /// `move(from:to:)`, or nil when the row stays where it is.
    public static func listMove(from offset: Int, to insertion: Int, positions: [Int]) -> (source: Int, destination: Int)? {
        guard positions.indices.contains(offset), insertion >= 0, insertion <= positions.count,
              insertion != offset, insertion != offset + 1 else { return nil }
        let destination = insertion > offset ? positions[insertion - 1] : positions[insertion]
        return (positions[offset], destination)
    }
}
