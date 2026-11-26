import Foundation

// "Für dich", radio and the customizable Start of the iPhone and iPad app.
// Pure models without UI or networking, unit-tested in YTMDLCoreTests.

// MARK: - Mix groups

/// The sections of "Für dich". Newer servers send each mix's `group`; older
/// ones only its `kind`, from which the group follows. An unknown group falls
/// back to the kind, an unknown kind to "Weitere Mixe", so a mix the server
/// adds later appears without an app update.
public enum MixGroup: String, CaseIterable, Identifiable, Sendable {
    case madeForYou = "made_for_you", discover, rewind, more
    public var id: String { rawValue }
    public var title: String {
        switch self {
        case .madeForYou: "Für dich gemacht"
        case .discover: "Entdecken"
        case .rewind: "Rückblick"
        case .more: "Weitere Mixe"
        }
    }
    public var subtitle: String {
        switch self {
        case .madeForYou: "Täglich neu aus dem, was du hörst"
        case .discover: "Was in deiner Mediathek auf dich wartet"
        case .rewind: "Was du früher gern gehört hast"
        case .more: "Neue Mixe von deinem Server"
        }
    }
    public static func of(_ mix: PersonalMix) -> MixGroup {
        if let group = mix.group, let known = MixGroup(rawValue: group), known != .more { return known }
        switch mix.kind {
        case "genre", "on_repeat", "artist", "favorites", "daypart": return .madeForYou
        case "rediscover", "unheard", "new": return .discover
        case "forgotten_favorites", "decade": return .rewind
        default: return .more
        }
    }
    /// The mixes of each group in the order of `allCases`, empty groups left
    /// out. Within a group genre mixes come first, then the server's order.
    public static func sections(_ mixes: [PersonalMix]) -> [MixSection] {
        let ordered = PersonalMix.ordered(mixes)
        return allCases.compactMap { group in
            let members = ordered.filter { of($0) == group }
            return members.isEmpty ? nil : MixSection(group: group, mixes: members)
        }
    }
}

public struct MixSection: Identifiable, Sendable {
    public let group: MixGroup
    public let mixes: [PersonalMix]
    public var id: String { group.rawValue }
}

extension PersonalMix {
    /// The symbol of a mix without covers.
    public var symbol: String {
        switch kind {
        case "genre": "waveform"
        case "on_repeat": "repeat"
        case "rediscover": "clock.arrow.circlepath"
        case "unheard": "sparkle.magnifyingglass"
        case "new": "sparkles"
        case "artist": "person.crop.circle"
        case "favorites": "heart.fill"
        case "forgotten_favorites": "heart.text.square"
        case "decade": "calendar"
        case "daypart":
            switch daypart {
            case "morning": "sunrise"
            case "afternoon": "sun.max"
            case "evening": "sunset"
            case "night": "moon.stars"
            default: "clock"
            }
        default: "music.note.list"
        }
    }
}

// MARK: - Radio

/// What a radio starts from.
public enum RadioSeed: Hashable, Sendable {
    case track(id: String, title: String)
    case artist(id: String, name: String)
    case album(id: String, title: String)
    case genre(String)
}

/// A radio: the seed, the queue context it is shown as ("Radio: Nachtfahrt")
/// and the requests for `GET /library/radio`.
///
/// The server (1.10) seeds a radio with one track (`seed`) or a genre
/// (`genre`). Track and genre radios use those directly. Artist and album
/// radios are built on the device: the app picks one track of the artist or
/// album as the seed (`pickSeed`: the most played, else a random one) and the
/// seed radio keeps close to that artist; the chosen track is kept in the
/// context so top-ups use the same seed. A server with artist or album seeds
/// only needs `query` to change.
public struct RadioStation: Hashable, Sendable {
    public let seed: RadioSeed
    /// The track an artist or album radio is seeded with.
    public let seedTrackID: String?
    public init(_ seed: RadioSeed, seedTrackID: String? = nil) {
        self.seed = seed
        if case .track(let id, _) = seed { self.seedTrackID = id } else { self.seedTrackID = seedTrackID?.isEmpty == false ? seedTrackID : nil }
    }
    public var name: String {
        switch seed {
        case .track(_, let title), .album(_, let title): title
        case .artist(_, let name): name
        case .genre(let genre): genre
        }
    }
    public var title: String { "Radio: \(name)" }
    /// "Song-Radio", "Künstler-Radio" …, for confirmations.
    public var kindLabel: String {
        switch seed {
        case .track: "Song-Radio"
        case .artist: "Künstler-Radio"
        case .album: "Album-Radio"
        case .genre: "Genre-Radio"
        }
    }
    /// Whether the station needs a seed track picked on the device first.
    public var needsSeedTrack: Bool {
        switch seed {
        case .artist, .album: seedTrackID == nil
        default: false
        }
    }
    /// How the queue shows the radio; it also brings the station back after
    /// a relaunch (`init?(context:)`).
    public var context: QueueContext {
        let id: String
        switch seed {
        case .track(let track, _): id = "track:" + track
        case .artist(let artist, _): id = "artist:" + artist + (seedTrackID.map { "|" + $0 } ?? "")
        case .album(let album, _): id = "album:" + album + (seedTrackID.map { "|" + $0 } ?? "")
        case .genre(let genre): id = "genre:" + genre
        }
        return QueueContext(kind: "radio", id: id, title: title)
    }
    /// The station of a queue's context; nil for anything that is not a
    /// radio of this kind (also the plain radio contexts of older app versions).
    public init?(context: QueueContext?) {
        guard let context, context.kind == "radio", let id = context.id, let colon = id.firstIndex(of: ":") else { return nil }
        let type = String(id[..<colon]), value = String(id[id.index(after: colon)...])
        let name = context.title.hasPrefix("Radio: ") ? String(context.title.dropFirst(7)) : context.title
        guard !value.isEmpty else { return nil }
        func split() -> (String, String?) {
            guard let bar = value.lastIndex(of: "|") else { return (value, nil) }
            return (String(value[..<bar]), String(value[value.index(after: bar)...]))
        }
        switch type {
        case "track": self.init(.track(id: value, title: name))
        case "genre": self.init(.genre(value))
        case "artist": let (artist, seed) = split(); guard !artist.isEmpty else { return nil }; self.init(.artist(id: artist, name: name), seedTrackID: seed)
        case "album": let (album, seed) = split(); guard !album.isEmpty else { return nil }; self.init(.album(id: album, title: name), seedTrackID: seed)
        default: return nil
        }
    }
    /// The station of a server with `radio.station` (1.11): the server seeds
    /// every kind itself, titles it and serves it in batches without repeats.
    /// `cursor` continues where the previous batch ended; `exclude` (at most
    /// 500 ids) keeps out the queue and what this session played. The seed
    /// parameters are sent with every batch, so an expired station is rebuilt.
    public func stationQuery(nonce: String, limit: Int, cursor: String? = nil, exclude: [String] = []) -> [URLQueryItem] {
        var items: [URLQueryItem]
        switch seed {
        case .track(let id, _): items = [.init(name: "seed", value: id)]
        case .artist(let id, _): items = [.init(name: "artist", value: id)]
        case .album(let id, _): items = [.init(name: "release", value: id)]
        case .genre(let genre): items = [.init(name: "genre", value: genre)]
        }
        items += [.init(name: "nonce", value: nonce), .init(name: "limit", value: String(min(100, max(1, limit))))]
        if let cursor, !cursor.isEmpty { items.append(.init(name: "cursor", value: cursor)) }
        let excluded = Array(exclude.filter { !$0.isEmpty }.prefix(500))
        if !excluded.isEmpty { items.append(.init(name: "exclude", value: excluded.joined(separator: ","))) }
        return items
    }
    /// The queue context of a server station, named as the server titles it
    /// ("Radio: Daft Punk"); the station's own title when the server sends none.
    public func context(serverTitle: String) -> QueueContext {
        let base = context
        let title = serverTitle.trimmingCharacters(in: .whitespacesAndNewlines)
        return QueueContext(kind: base.kind, id: base.id, title: title.isEmpty ? base.title : title)
    }
    /// The query of the first batch; nil while an artist or album radio has no seed track yet.
    public func query(nonce: String) -> [URLQueryItem]? {
        switch seed {
        case .genre(let genre): return [.init(name: "genre", value: genre), .init(name: "nonce", value: nonce)]
        default:
            guard let seedTrackID else { return nil }
            return [.init(name: "seed", value: seedTrackID), .init(name: "nonce", value: nonce)]
        }
    }
    /// The queries of a top-up when the queue runs low, tried in order until
    /// one brings titles not heard yet: the station's own seed first, then the
    /// last title of the queue, so a long radio drifts slowly instead of
    /// running dry. A genre radio stays in its genre either way.
    public func refillQueries(lastTrackID: String?, nonce: String) -> [[URLQueryItem]] {
        var result: [[URLQueryItem]] = []
        if let first = query(nonce: nonce) { result.append(first) }
        if let last = lastTrackID, !last.isEmpty, last != seedTrackID {
            var drift: [URLQueryItem] = [.init(name: "seed", value: last), .init(name: "nonce", value: nonce)]
            if case .genre(let genre) = seed { drift.insert(.init(name: "genre", value: genre), at: 1) }
            result.append(drift)
        }
        return result
    }
    /// The new queue: the seed title (when there is one) followed by the
    /// radio's titles, each once, without the titles played earlier in this
    /// session, at most `PlaybackQueue.limit`.
    public static func startQueue(seed: Track?, results: [Track], excluding played: [String]) -> [Track] {
        var known = Set(played)
        var queue: [Track] = []
        if let seed { queue.append(seed); known.insert(seed.id) }
        for track in results where known.insert(track.id).inserted {
            queue.append(track)
            if queue.count == PlaybackQueue.limit { break }
        }
        return queue
    }
    /// The seed of an artist or album radio: the listener's most played title
    /// when there is one, else a random title (`random` picks an index below its argument).
    public static func pickSeed(preferred: [Track], fallback: [Track], random: (Int) -> Int = { Int.random(in: 0..<$0) }) -> Track? {
        if let first = preferred.first { return first }
        guard !fallback.isEmpty else { return nil }
        let index = random(fallback.count)
        return fallback[min(max(0, index), fallback.count - 1)]
    }
}

// MARK: - Top artists

/// One artist of "Deine Top-Künstler", from the listener's most played titles.
public struct TopArtist: Identifiable, Hashable, Sendable {
    public let name: String
    public let plays: Int
    public var id: String { LibraryMatch.normalized(name) }
    public init(name: String, plays: Int) { self.name = name; self.plays = plays }
    /// Plays summed per artist (every credited artist of a title, else its
    /// album artist), most played first; equal counts keep the order of
    /// first appearance. Names compare like `LibraryMatch`.
    public static func rank(_ history: [HistoryTrack], limit: Int = 10) -> [TopArtist] {
        var order: [String] = []
        var names: [String: String] = [:]
        var plays: [String: Int] = [:]
        for entry in history {
            var credited = entry.track.artists.filter { !$0.trimmingCharacters(in: .whitespaces).isEmpty }
            if credited.isEmpty, let album = entry.track.albumArtist, !album.isEmpty { credited = [album] }
            for name in credited {
                let key = LibraryMatch.normalized(name)
                if names[key] == nil { names[key] = name.trimmingCharacters(in: .whitespacesAndNewlines); order.append(key) }
                plays[key, default: 0] += max(1, entry.playCount)
            }
        }
        let ranked = order.enumerated().sorted { a, b in
            let pa = plays[a.element] ?? 0, pb = plays[b.element] ?? 0
            return pa != pb ? pa > pb : a.offset < b.offset
        }
        return ranked.prefix(max(0, limit)).map { TopArtist(name: names[$0.element] ?? $0.element, plays: plays[$0.element] ?? 0) }
    }
}

// MARK: - Start layout

/// A shelf of the iPhone and iPad Start screen.
public enum HomeShelf: String, CaseIterable, Codable, Identifiable, Sendable {
    case quickAccess = "quick_access"
    case mixes
    case jumpBackIn = "jump_back_in"
    case recent
    case newReleases = "new_releases"
    case frequent
    case discover
    case topArtists = "top_artists"
    case genres
    case favorites
    case playlists
    /// "Statistik": this week's plays and top artist (server 1.11, `history.stats`).
    case stats
    public var id: String { rawValue }
    public var title: String {
        switch self {
        case .quickAccess: "Schnellzugriff"
        case .mixes: "Deine Mixe"
        case .jumpBackIn: "Wieder einsteigen"
        case .recent: "Zuletzt gehört"
        case .newReleases: "Neu in der Mediathek"
        case .frequent: "Am meisten gehört"
        case .discover: "Entdecken"
        case .topArtists: "Deine Top-Künstler"
        case .genres: "Deine Genres"
        case .favorites: "Favoriten"
        case .playlists: "Playlists"
        case .stats: "Statistik"
        }
    }
    public var subtitle: String {
        switch self {
        case .quickAccess: "Angeheftetes und was du oft brauchst"
        case .mixes: "Täglich neu aus deiner Mediathek"
        case .jumpBackIn: "Alben, Playlists, Mixe und Radios, die du zuletzt gestartet hast"
        case .recent: "Deine zuletzt gehörten Titel"
        case .newReleases: "Zuletzt hinzugefügte Alben"
        case .frequent: "Deine meistgespielten Titel"
        case .discover: "Mixe mit Musik, die du selten hörst"
        case .topArtists: "Wen du am häufigsten hörst"
        case .genres: "Alben und Radio nach Genre"
        case .favorites: "Deine Lieblingstitel"
        case .playlists: "Deine Playlists"
        case .stats: "Was du diese Woche gehört hast"
        }
    }
    public var symbol: String {
        switch self {
        case .quickAccess: "square.grid.2x2"
        case .mixes: "sparkles"
        case .jumpBackIn: "arrow.uturn.backward.circle"
        case .recent: "clock"
        case .newReleases: "square.stack"
        case .frequent: "chart.bar"
        case .discover: "sparkle.magnifyingglass"
        case .topArtists: "person.2"
        case .genres: "guitars"
        case .favorites: "heart"
        case .playlists: "music.note.list"
        case .stats: "chart.bar.xaxis"
        }
    }
    /// Shown on a new Start; the optional shelves wait in "Start anpassen".
    public var visibleByDefault: Bool {
        switch self {
        case .quickAccess, .mixes, .jumpBackIn, .recent, .newReleases, .frequent: true
        case .discover, .topArtists, .genres, .favorites, .playlists, .stats: false
        }
    }
    /// Shelves that need a capability of the server; without it the shelf is
    /// not shown on Start nor offered in "Start anpassen".
    public static func unavailable(on features: ServerFeatures) -> Set<HomeShelf> {
        features.statistics ? [] : [.stats]
    }
}

/// Something pinned to Start's quick access ("An Start anheften").
public struct HomePin: Codable, Hashable, Identifiable, Sendable {
    public enum Kind: String, Codable, CaseIterable, Sendable { case playlist, album, artist, mix, genre, favorites, stats }
    public let kind: Kind
    /// The playlist, album, artist or mix ID; the name of a genre.
    public let itemID: String
    public let title: String
    /// The artists of an album; empty elsewhere.
    public let subtitle: String
    public var id: String { kind.rawValue + ":" + itemID }
    public init(kind: Kind, itemID: String, title: String, subtitle: String = "") {
        self.kind = kind; self.itemID = itemID; self.title = title; self.subtitle = subtitle
    }
    enum CodingKeys: String, CodingKey { case kind, itemID = "id", title, subtitle }
}

/// The order and visibility of Start's shelves and the pinned tiles of one
/// account on one server. Saved as small, versioned JSON; reading is
/// tolerant: unknown shelves and pins are dropped, shelves the saved layout
/// does not know yet (added by an app update) join at their default place.
public struct HomeLayout: Equatable, Sendable {
    public static let version = 1
    /// Quick access shows at most this many tiles; pinning stops there.
    public static let pinLimit = 8
    public struct Entry: Equatable, Sendable {
        public let shelf: HomeShelf
        public var visible: Bool
        public init(shelf: HomeShelf, visible: Bool) { self.shelf = shelf; self.visible = visible }
    }
    public private(set) var entries: [Entry]
    public private(set) var pins: [HomePin]
    public init(entries: [Entry] = [], pins: [HomePin] = []) {
        self.entries = Self.normalized(entries)
        var seen = Set<String>()
        self.pins = Array(pins.filter { !$0.itemID.isEmpty && seen.insert($0.id).inserted }.prefix(Self.pinLimit))
    }
    public static var standard: HomeLayout { HomeLayout() }
    public var isStandard: Bool { self == .standard }
    public var shelves: [HomeShelf] { entries.map(\.shelf) }
    public var visibleShelves: [HomeShelf] { entries.filter(\.visible).map(\.shelf) }
    public func isVisible(_ shelf: HomeShelf) -> Bool { entries.first { $0.shelf == shelf }?.visible ?? false }
    public mutating func setVisible(_ shelf: HomeShelf, _ visible: Bool) {
        guard let index = entries.firstIndex(where: { $0.shelf == shelf }) else { return }
        entries[index].visible = visible
    }
    /// A drag in the editor's list (`onMove`): the offsets before the move,
    /// the destination counted in the list before the move.
    public mutating func move(fromOffsets source: IndexSet, toOffset destination: Int) {
        entries = Self.moved(entries, source, destination)
    }
    /// One step up (-1) or down (+1), for VoiceOver's actions.
    @discardableResult public mutating func move(_ shelf: HomeShelf, by step: Int) -> Bool {
        guard let index = entries.firstIndex(where: { $0.shelf == shelf }) else { return false }
        let target = index + step
        guard entries.indices.contains(target), target != index else { return false }
        entries.swapAt(index, target)
        return true
    }
    /// The shown shelves without those the server cannot fill (`excluding`).
    public func visibleShelves(excluding unavailable: Set<HomeShelf>) -> [HomeShelf] {
        visibleShelves.filter { !unavailable.contains($0) }
    }
    /// A drag among the shown shelves only (the editor lists hidden ones
    /// apart): the hidden shelves, and the shown ones left out of the editor
    /// (`excluding`), keep their places in between.
    public mutating func moveVisible(fromOffsets source: IndexSet, toOffset destination: Int, excluding unavailable: Set<HomeShelf> = []) {
        let slots = entries.indices.filter { entries[$0].visible && !unavailable.contains(entries[$0].shelf) }
        let reordered = Self.moved(slots.map { entries[$0] }, source, destination)
        for (slot, entry) in zip(slots, reordered) { entries[slot] = entry }
    }
    /// One step up (-1) or down (+1) among the shown shelves, for VoiceOver.
    @discardableResult public mutating func moveVisible(_ shelf: HomeShelf, by step: Int, excluding unavailable: Set<HomeShelf> = []) -> Bool {
        let shown = visibleShelves(excluding: unavailable)
        guard let index = shown.firstIndex(of: shelf), shown.indices.contains(index + step), step != 0 else { return false }
        moveVisible(fromOffsets: IndexSet(integer: index), toOffset: step > 0 ? index + step + 1 : index + step, excluding: unavailable)
        return true
    }
    public func isPinned(_ kind: HomePin.Kind, _ itemID: String) -> Bool { pins.contains { $0.kind == kind && $0.itemID == itemID } }
    public var canPin: Bool { pins.count < Self.pinLimit }
    /// Adds a tile at the end; false when it is already pinned or quick access is full.
    @discardableResult public mutating func pin(_ pin: HomePin) -> Bool {
        guard !pin.itemID.isEmpty, canPin, !isPinned(pin.kind, pin.itemID) else { return false }
        pins.append(pin)
        return true
    }
    public mutating func unpin(_ kind: HomePin.Kind, _ itemID: String) { pins.removeAll { $0.kind == kind && $0.itemID == itemID } }
    public mutating func movePins(fromOffsets source: IndexSet, toOffset destination: Int) { pins = Self.moved(pins, source, destination) }
    /// "Zurücksetzen": the default shelves, nothing pinned.
    public mutating func reset() { self = .standard }

    /// The saved entries in their order, each shelf once, then the shelves
    /// they lack, each after the shelf that precedes it by default.
    static func normalized(_ saved: [Entry]) -> [Entry] {
        var seen = Set<HomeShelf>()
        var result = saved.filter { seen.insert($0.shelf).inserted }
        let defaults = HomeShelf.allCases
        for (position, shelf) in defaults.enumerated() where !seen.contains(shelf) {
            let before = defaults[..<position].reversed().first { known in result.contains { $0.shelf == known } }
            let index = before.flatMap { known in result.firstIndex { $0.shelf == known } }.map { $0 + 1 } ?? 0
            result.insert(Entry(shelf: shelf, visible: shelf.visibleByDefault), at: index)
            seen.insert(shelf)
        }
        return result
    }
    static func moved<T>(_ list: [T], _ source: IndexSet, _ destination: Int) -> [T] {
        let offsets = source.filter { list.indices.contains($0) }.sorted()
        guard !offsets.isEmpty else { return list }
        let moving = offsets.map { list[$0] }
        var rest = list
        for offset in offsets.reversed() { rest.remove(at: offset) }
        let target = destination - offsets.filter { $0 < destination }.count
        rest.insert(contentsOf: moving, at: min(max(0, target), rest.count))
        return rest
    }

    // MARK: Storage

    /// The preferences key: one layout per server address and account.
    public static func storageKey(server: String, userID: String) -> String {
        "homeLayout." + server.lowercased() + "|" + userID
    }
    private struct Stored: Codable {
        struct Shelf: Codable { let id: String; let visible: Bool }
        let version: Int
        let shelves: [Shelf]
        let pins: [HomePin]
    }
    /// Reads anything without failing: unreadable data is the default layout.
    public static func decode(_ data: Data?) -> HomeLayout {
        guard let data, let raw = try? JSONDecoder().decode(Lenient.self, from: data) else { return .standard }
        let entries = raw.shelves.compactMap { shelf -> Entry? in
            guard let id = shelf.id, let known = HomeShelf(rawValue: id) else { return nil }
            return Entry(shelf: known, visible: shelf.visible ?? known.visibleByDefault)
        }
        let pins = raw.pins.compactMap { pin -> HomePin? in
            guard let kind = pin.kind.flatMap(HomePin.Kind.init(rawValue:)), let id = pin.id, !id.isEmpty else { return nil }
            return HomePin(kind: kind, itemID: id, title: pin.title ?? id, subtitle: pin.subtitle ?? "")
        }
        return HomeLayout(entries: entries, pins: pins)
    }
    public func encoded() -> Data? {
        try? JSONEncoder().encode(Stored(version: Self.version, shelves: entries.map { .init(id: $0.shelf.rawValue, visible: $0.visible) }, pins: pins))
    }
    /// Every field optional and every element on its own, so one odd value
    /// (or a layout written by a newer app) never discards the rest.
    private struct Lenient: Decodable {
        struct Shelf: Decodable {
            let id: String?, visible: Bool?
            enum CodingKeys: String, CodingKey { case id, visible }
            init(from decoder: any Decoder) throws {
                let c = try? decoder.container(keyedBy: CodingKeys.self)
                id = (try? c?.decodeIfPresent(String.self, forKey: .id)) ?? nil
                visible = (try? c?.decodeIfPresent(Bool.self, forKey: .visible)) ?? nil
            }
        }
        struct Pin: Decodable {
            let kind: String?, id: String?, title: String?, subtitle: String?
            enum CodingKeys: String, CodingKey { case kind, id, title, subtitle }
            init(from decoder: any Decoder) throws {
                let c = try? decoder.container(keyedBy: CodingKeys.self)
                kind = (try? c?.decodeIfPresent(String.self, forKey: .kind)) ?? nil
                id = (try? c?.decodeIfPresent(String.self, forKey: .id)) ?? nil
                title = (try? c?.decodeIfPresent(String.self, forKey: .title)) ?? nil
                subtitle = (try? c?.decodeIfPresent(String.self, forKey: .subtitle)) ?? nil
            }
        }
        let shelves: [Shelf]
        let pins: [Pin]
        enum CodingKeys: String, CodingKey { case shelves, pins }
        init(from decoder: any Decoder) throws {
            let c = try decoder.container(keyedBy: CodingKeys.self)
            shelves = (try? c.decodeIfPresent([Shelf].self, forKey: .shelves)) ?? []
            pins = (try? c.decodeIfPresent([Pin].self, forKey: .pins)) ?? []
        }
    }
}

// MARK: - Recently started sources ("Wieder einsteigen")

/// An album, playlist, mix, artist, radio or the favorites the listener started.
public struct RecentSource: Codable, Hashable, Identifiable, Sendable {
    public let kind: String
    public let itemID: String
    public let title: String
    public var id: String { kind + ":" + itemID }
    public init(kind: String, itemID: String, title: String) { self.kind = kind; self.itemID = itemID; self.title = title }
    enum CodingKeys: String, CodingKey { case kind, itemID = "id", title }
    /// The small line under the title.
    public var label: String {
        switch kind {
        case "album": "Album"
        case "playlist": "Playlist"
        case "mix": "Mix"
        case "artist": "Künstler"
        case "radio": "Radio"
        case "favorites": "Lieblingstitel"
        default: ""
        }
    }
    public var context: QueueContext { QueueContext(kind: kind, id: itemID.isEmpty ? nil : itemID, title: title) }
}

/// The last sources started on this device for one account, newest first.
/// It is filled from the queue's context whenever a queue starts.
public struct RecentSources: Equatable, Sendable {
    public static let limit = 12
    static let kinds: Set<String> = ["album", "playlist", "mix", "artist", "radio", "favorites"]
    public private(set) var entries: [RecentSource]
    public init(_ saved: [RecentSource] = []) {
        entries = []
        for entry in saved.reversed() { record(entry) }
    }
    /// Remembers what a new queue was started from. Track lists without a
    /// place to return to (search results, history shelves) are left out.
    @discardableResult public mutating func record(_ context: QueueContext?) -> Bool {
        guard let context else { return false }
        return record(RecentSource(kind: context.kind, itemID: context.id ?? "", title: context.title))
    }
    @discardableResult public mutating func record(_ source: RecentSource) -> Bool {
        guard Self.kinds.contains(source.kind), !source.title.isEmpty, source.kind == "favorites" || !source.itemID.isEmpty else { return false }
        entries.removeAll { $0.id == source.id }
        entries.insert(source, at: 0)
        if entries.count > Self.limit { entries.removeLast(entries.count - Self.limit) }
        return true
    }
    public mutating func remove(_ id: String) { entries.removeAll { $0.id == id } }
    public mutating func clear() { entries = [] }
    public static func storageKey(server: String, userID: String) -> String {
        "recentSources." + server.lowercased() + "|" + userID
    }
    public static func decode(_ data: Data?) -> RecentSources {
        guard let data, let list = try? JSONDecoder().decode([Lenient].self, from: data) else { return RecentSources() }
        return RecentSources(list.compactMap(\.source))
    }
    public func encoded() -> Data? { try? JSONEncoder().encode(entries) }
    private struct Lenient: Decodable {
        let source: RecentSource?
        init(from decoder: any Decoder) throws { source = try? RecentSource(from: decoder) }
    }
}
