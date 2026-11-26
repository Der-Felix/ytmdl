import Foundation

// MARK: - German dates

/// Date texts of the app. The interface is German only, so dates are German
/// whatever language the device uses ("vor 3 Tagen", never "3 days ago").
public enum GermanDates {
    public static let locale = Locale(identifier: "de_DE")
    private static func calendar(_ base: Calendar) -> Calendar { var value = base; value.locale = locale; return value }
    /// "gestern", "vor 3 Tagen", "vor 1 Stunde", "in 2 Minuten".
    public static func relative(_ date: Date, to now: Date = .now, calendar: Calendar = .current) -> String {
        let formatter = RelativeDateTimeFormatter()
        formatter.locale = locale
        formatter.calendar = Self.calendar(calendar)
        formatter.dateTimeStyle = .named
        formatter.unitsStyle = .full
        return formatter.localizedString(for: date, relativeTo: now)
    }
    /// "15:04".
    public static func time(_ date: Date, timeZone: TimeZone = .current) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale; formatter.timeZone = timeZone
        formatter.dateFormat = "HH:mm"
        return formatter.string(from: date)
    }
    /// "Donnerstag, 26. November".
    public static func day(_ date: Date, timeZone: TimeZone = .current) -> String {
        let formatter = DateFormatter()
        formatter.locale = locale; formatter.timeZone = timeZone
        formatter.setLocalizedDateFormatFromTemplate("EEEEdMMMM")
        return formatter.string(from: date)
    }
}

// MARK: - Library preferences

/// How the album list of the library is ordered. The server sorts, so the
/// order also holds for albums that are not loaded yet.
public enum LibraryAlbumSort: String, CaseIterable, Identifiable, Sendable {
    case recent, title, artist, year
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .recent: "Zuletzt hinzugefügt"
        case .title: "Titel"
        case .artist: "Künstler"
        case .year: "Jahr"
        }
    }
    /// Newest first for dates, A–Z for names.
    public var order: String { self == .recent || self == .year ? "desc" : "asc" }
    public var queryItems: [URLQueryItem] { [.init(name: "sort", value: rawValue), .init(name: "order", value: order)] }
}

/// Grid of covers or a compact list.
public enum LibraryAlbumLayout: String, CaseIterable, Sendable {
    case grid, list
    public var toggled: Self { self == .grid ? .list : .grid }
}

/// The remembered choices of the library page, stored per device.
public struct LibraryPreferences: Sendable {
    public static let sortKey = "librarySort"
    public static let layoutKey = "libraryAlbumLayout"
    public static func sort(in defaults: UserDefaults = .standard) -> LibraryAlbumSort {
        defaults.string(forKey: sortKey).flatMap(LibraryAlbumSort.init(rawValue:)) ?? .recent
    }
    public static func setSort(_ sort: LibraryAlbumSort, in defaults: UserDefaults = .standard) {
        defaults.set(sort.rawValue, forKey: sortKey)
    }
    public static func layout(in defaults: UserDefaults = .standard) -> LibraryAlbumLayout {
        defaults.string(forKey: layoutKey).flatMap(LibraryAlbumLayout.init(rawValue:)) ?? .grid
    }
    public static func setLayout(_ layout: LibraryAlbumLayout, in defaults: UserDefaults = .standard) {
        defaults.set(layout.rawValue, forKey: layoutKey)
    }
}

/// Order of an artist's albums on the artist page.
public enum ArtistAlbumSort: String, CaseIterable, Identifiable, Sendable {
    case newest, oldest, title
    public var id: String { rawValue }
    public var label: String {
        switch self { case .newest: "Neueste zuerst"; case .oldest: "Älteste zuerst"; case .title: "Titel A–Z" }
    }
    /// Albums without a known year (0) go last in both year orders.
    public func apply(_ releases: [Release]) -> [Release] {
        releases.enumerated().sorted { lhs, rhs in
            let a = lhs.element, b = rhs.element
            switch self {
            case .title:
                let result = a.title.localizedStandardCompare(b.title)
                return result == .orderedSame ? lhs.offset < rhs.offset : result == .orderedAscending
            case .newest, .oldest:
                if (a.year == 0) != (b.year == 0) { return b.year == 0 }
                if a.year != b.year { return self == .newest ? a.year > b.year : a.year < b.year }
                let result = a.title.localizedStandardCompare(b.title)
                return result == .orderedSame ? lhs.offset < rhs.offset : result == .orderedAscending
            }
        }.map(\.element)
    }
}

// MARK: - A–Z index

/// Sections by first letter for long name lists (Künstler). Letters are
/// folded ("Ärzte" under A, "Østlich" under O); digits and signs go under "#",
/// which comes last like in the system apps.
public enum LibraryIndex {
    public static let letters: [String] = (65...90).map { String(UnicodeScalar(UInt8($0))) } + ["#"]
    public static func letter(for name: String) -> String {
        let folded = name.trimmingCharacters(in: .whitespacesAndNewlines)
            .folding(options: [.diacriticInsensitive, .caseInsensitive, .widthInsensitive], locale: Locale(identifier: "de_DE"))
        guard let first = folded.unicodeScalars.first else { return "#" }
        var scalar = first
        // Letters without a decomposition the folding knows.
        switch scalar { case "ø", "Ø": scalar = "o"; case "æ", "Æ": scalar = "a"; case "ß": scalar = "s"; case "ł", "Ł": scalar = "l"; default: break }
        let upper = String(scalar).uppercased()
        return letters.dropLast().contains(upper) ? upper : "#"
    }
    public struct Section<Item>: Identifiable {
        public let letter: String
        public let items: [Item]
        public var id: String { letter }
    }
    /// Grouped and sorted A–Z, "#" last; names compare like Finder (numbers by value).
    public static func sections<Item>(_ items: [Item], name: (Item) -> String) -> [Section<Item>] {
        var groups: [String: [(String, Item)]] = [:]
        for item in items {
            let value = name(item)
            groups[letter(for: value), default: []].append((value, item))
        }
        return letters.compactMap { letter in
            guard let group = groups[letter] else { return nil }
            let sorted = group.sorted { $0.0.localizedStandardCompare($1.0) == .orderedAscending }
            return Section(letter: letter, items: sorted.map(\.1))
        }
    }
}
extension LibraryIndex.Section: Sendable where Item: Sendable {}

// MARK: - Artist placeholder

/// A generated placeholder for an artist without a picture: up to two
/// initials on a color that stays the same for a name.
public enum ArtistMonogram {
    public static func initials(_ name: String) -> String {
        let words = name.split(whereSeparator: { $0.isWhitespace || $0 == "-" }).filter { $0.first?.isLetter == true || $0.first?.isNumber == true }
        let letters = words.prefix(2).compactMap { $0.first.map { String($0).uppercased() } }
        return letters.isEmpty ? "#" : letters.joined()
    }
    /// A hue between 0 and 1, stable across launches (no `hashValue`).
    public static func hue(_ name: String) -> Double {
        var value: UInt32 = 2166136261
        for byte in name.lowercased().utf8 { value = (value ^ UInt32(byte)) &* 16777619 }
        return Double(value % 360) / 360
    }
}

// MARK: - Album pages

/// Tracks of an album by disc. One disc (or none known) is one group, and the
/// page then leaves out the disc headings.
public struct DiscGroup: Identifiable, Sendable {
    public let disc: Int
    /// Positions in the album's track list, in its order.
    public let positions: [Int]
    public var id: Int { disc }
    public static func groups(_ tracks: [Track]) -> [DiscGroup] {
        var order: [Int] = []
        var members: [Int: [Int]] = [:]
        for (position, track) in tracks.enumerated() {
            let disc = max(1, track.discNumber ?? 1)
            if members[disc] == nil { order.append(disc) }
            members[disc, default: []].append(position)
        }
        return order.map { DiscGroup(disc: $0, positions: members[$0] ?? []) }
    }
}

/// The summary line under a collection title: "2026 · 12 Titel · 48 Min.".
public enum CollectionSummary {
    public static func duration(milliseconds: Int) -> String {
        let minutes = max(0, milliseconds / 60_000)
        return minutes >= 60 ? "\(minutes / 60) Std. \(minutes % 60) Min." : "\(minutes) Min."
    }
    public static func tracks(_ count: Int) -> String { "\(count) Titel" }
    public static func line(year: Int? = nil, trackCount: Int, durationMs: Int, partial: Bool = false) -> String {
        var parts: [String] = []
        if let year, year > 0 { parts.append(String(year)) }
        parts.append(partial ? "\(trackCount) geladene Titel" : tracks(trackCount))
        if durationMs > 0 { parts.append(duration(milliseconds: durationMs)) }
        return parts.joined(separator: " · ")
    }
}

extension Release {
    /// Singles and EPs; the artist page lists them apart from the albums.
    public var isSingleOrEP: Bool { releaseType == "single" || releaseType == "ep" }
    /// "Album", "Single", "EP", "Live-Album", "Kompilation" or "Remix".
    public var kindLabel: String {
        switch releaseType ?? "" {
        case "single": "Single"
        case "ep": "EP"
        case "live": "Live-Album"
        case "compilation": "Kompilation"
        case "remix": "Remix"
        default: "Album"
        }
    }
}
