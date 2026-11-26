import Foundation
import Testing
@testable import YTMDLCore

private func decoder() -> JSONDecoder {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase; return decoder
}
private func song(_ id: String, artists: [String] = [], albumArtist: String? = nil) -> Track {
    Track(id: id, title: id.uppercased(), artists: artists, album: "", durationMs: 200_000, albumArtist: albumArtist)
}
private func value(_ items: [URLQueryItem], _ name: String) -> String? { items.first { $0.name == name }?.value }

// MARK: Mix groups

@Test func mixGroupsFollowTheServerGroupThenTheKindAndKeepUnknownMixes() throws {
    let raw = Data(#"""
    {"data":[
      {"id":"on_repeat","kind":"on_repeat","title":"In Dauerschleife","track_count":20},
      {"id":"new","kind":"new","title":"Neu in der Mediathek","track_count":20},
      {"id":"genre-a","kind":"genre","title":"Dein Mix 1","genre":"Pop","track_count":20},
      {"id":"time_capsule","kind":"time_capsule","title":"Zeitkapsel","track_count":20,"group":"rewind"},
      {"id":"later","kind":"later","title":"Später","track_count":20,"group":"something_new"},
      {"id":"odd","kind":"unheard","title":"Noch nicht gehört","track_count":20,"group":42},
      {"id":"empty_group","kind":"rediscover","title":"Wiederentdecken","track_count":20,"group":""}
    ]}
    """#.utf8)
    let mixes = try decoder().decode(Envelope<[PersonalMix]>.self, from: raw).data
    #expect(mixes.count == 7, "An unknown kind or an unreadable group never drops a mix.")
    #expect(mixes[3].group == "rewind" && mixes[5].group == nil && mixes[6].group == nil)
    let sections = MixGroup.sections(mixes)
    #expect(sections.map(\.group) == [.madeForYou, .discover, .rewind, .more])
    #expect(sections[0].mixes.map(\.id) == ["genre-a", "on_repeat"], "Genre mixes first, then the server's order.")
    #expect(sections[1].mixes.map(\.id) == ["new", "odd", "empty_group"])
    #expect(sections[2].mixes.map(\.id) == ["time_capsule"])
    #expect(sections[3].mixes.map(\.id) == ["later"], "An unknown group falls back to the kind, then to the generic section.")
    #expect(MixGroup.of(PersonalMix(id: "x", kind: "genre", title: "X", trackCount: 1, group: "discover")) == .discover, "The server's group wins over the kind.")
    #expect(mixes[4].label == "Mix" && mixes[4].symbol == "music.note.list")
    #expect(MixGroup.sections([]).isEmpty)
}

// MARK: Radio

@Test func trackAndGenreRadiosUseTheServersSeeds() {
    let track = RadioStation(.track(id: "t1", title: "Nachtfahrt"))
    #expect(track.title == "Radio: Nachtfahrt" && track.kindLabel == "Song-Radio" && !track.needsSeedTrack)
    let query = track.query(nonce: "n")
    #expect(query.flatMap { value($0, "seed") } == "t1" && query.flatMap { value($0, "nonce") } == "n" && query.flatMap { value($0, "genre") } == nil)
    let genre = RadioStation(.genre("Pop"))
    #expect(genre.query(nonce: "n").flatMap { value($0, "genre") } == "Pop")
    #expect(genre.query(nonce: "n").flatMap { value($0, "seed") } == nil)
    #expect(genre.context == QueueContext(kind: "radio", id: "genre:Pop", title: "Radio: Pop"))
}

@Test func artistAndAlbumRadiosAreSeededOnTheDeviceAndKeepTheirSeed() {
    let waiting = RadioStation(.artist(id: "a0", name: "Nordlicht"))
    #expect(waiting.needsSeedTrack && waiting.query(nonce: "n") == nil, "An artist radio needs a title of the artist first.")
    let artist = RadioStation(.artist(id: "a0", name: "Nordlicht"), seedTrackID: "t2")
    #expect(!artist.needsSeedTrack && artist.query(nonce: "n").flatMap { value($0, "seed") } == "t2")
    #expect(artist.context == QueueContext(kind: "radio", id: "artist:a0|t2", title: "Radio: Nordlicht"))
    #expect(artist.kindLabel == "Künstler-Radio")
    let album = RadioStation(.album(id: "r0", title: "Blaue Stunde"), seedTrackID: "t3")
    #expect(album.context.id == "album:r0|t3" && album.kindLabel == "Album-Radio")
    // The most played title first, else a random one; nothing without titles.
    let top = [song("top")], all = [song("a"), song("b"), song("c")]
    #expect(RadioStation.pickSeed(preferred: top, fallback: all)?.id == "top")
    #expect(RadioStation.pickSeed(preferred: [], fallback: all, random: { _ in 2 })?.id == "c")
    #expect(RadioStation.pickSeed(preferred: [], fallback: all, random: { _ in 99 })?.id == "c", "An index out of range is clamped.")
    #expect(RadioStation.pickSeed(preferred: [], fallback: []) == nil)
}

@Test func radioStationsComeBackFromTheirQueueContext() {
    let stations = [RadioStation(.track(id: "t1", title: "Nachtfahrt")), RadioStation(.genre("Hip-Hop: Deutsch")),
                    RadioStation(.artist(id: "a0", name: "Nordlicht"), seedTrackID: "t2"), RadioStation(.album(id: "r0", title: "Radio: Live"), seedTrackID: "t4"),
                    RadioStation(.artist(id: "a1", name: "Mira"))]
    for station in stations {
        #expect(RadioStation(context: station.context) == station, "\(station.context.id ?? "")")
    }
    // Plain radio contexts of older app versions and other queues are not stations.
    #expect(RadioStation(context: QueueContext(kind: "radio", id: "t1", title: "Radio: Nachtfahrt")) == nil)
    #expect(RadioStation(context: QueueContext(kind: "radio", id: "podcast:1", title: "Radio: X")) == nil)
    #expect(RadioStation(context: QueueContext(kind: "radio", id: "track:", title: "Radio: X")) == nil)
    #expect(RadioStation(context: QueueContext(kind: "album", id: "track:t1", title: "Album")) == nil)
    #expect(RadioStation(context: nil) == nil)
}

@Test func radioQueueStartsWithTheSeedAndNeverRepeatsTheSession() {
    let results = [song("a"), song("seed"), song("b"), song("a"), song("played"), song("c")]
    let queue = RadioStation.startQueue(seed: song("seed"), results: results, excluding: ["played", "seed"])
    #expect(queue.map(\.id) == ["seed", "a", "b", "c"], "The seed plays first even when it was heard before; nothing else repeats.")
    #expect(RadioStation.startQueue(seed: nil, results: results, excluding: []).map(\.id) == ["a", "seed", "b", "played", "c"])
    let many = (0..<600).map { song("s\($0)") }
    #expect(RadioStation.startQueue(seed: song("x"), results: many, excluding: []).count == PlaybackQueue.limit)
}

@Test func radioTopUpsTryTheStationSeedThenDriftFromTheLastTitle() {
    let track = RadioStation(.track(id: "t1", title: "Nachtfahrt"))
    let queries = track.refillQueries(lastTrackID: "t9", nonce: "n")
    #expect(queries.map { value($0, "seed") } == ["t1", "t9"])
    #expect(track.refillQueries(lastTrackID: "t1", nonce: "n").count == 1, "The seed is not asked twice.")
    let genre = RadioStation(.genre("Pop")).refillQueries(lastTrackID: "t9", nonce: "n")
    #expect(genre.count == 2 && value(genre[0], "genre") == "Pop" && value(genre[0], "seed") == nil)
    #expect(value(genre[1], "genre") == "Pop" && value(genre[1], "seed") == "t9", "A genre radio stays in its genre while it drifts.")
    #expect(RadioStation(.artist(id: "a", name: "A")).refillQueries(lastTrackID: nil, nonce: "n").isEmpty)
    // Top-ups of a radio queue are headed as part of the radio.
    let context = track.context
    #expect(QueueSection.context.heading(context) == "Radio: Nachtfahrt")
    #expect(QueueSection.autoplay.heading(context) == "Mehr aus diesem Radio")
    #expect(QueueSection.autoplay.heading(QueueContext(kind: "album", title: "A")) == "Ähnliche Titel")
}

// MARK: Top artists

@Test func topArtistsSumPlaysPerCreditedArtist() {
    let history = [HistoryTrack(track: song("a", artists: ["Mira"]), playCount: 3),
                   HistoryTrack(track: song("b", artists: ["Nordlicht", "Mira"]), playCount: 5),
                   HistoryTrack(track: song("c", artists: ["nordlicht "]), playCount: 1),
                   HistoryTrack(track: song("d", artists: [], albumArtist: "Östlich"), playCount: 6),
                   HistoryTrack(track: song("e", artists: ["  "]), playCount: 9)]
    let ranked = TopArtist.rank(history)
    #expect(ranked.map(\.name) == ["Mira", "Nordlicht", "Östlich"])
    #expect(ranked.map(\.plays) == [8, 6, 6], "Equal counts keep the order of first appearance; names compare without case.")
    #expect(TopArtist.rank(history, limit: 1).count == 1)
    #expect(TopArtist.rank([]).isEmpty)
}

// MARK: Start layout

@Test func homeLayoutDefaultsShowTheClassicShelvesAndWaitWithTheOptionalOnes() {
    let layout = HomeLayout.standard
    #expect(layout.shelves == HomeShelf.allCases)
    #expect(layout.visibleShelves == [.quickAccess, .mixes, .jumpBackIn, .recent, .newReleases, .frequent])
    #expect(layout.pins.isEmpty && layout.isStandard)
}

@Test func homeLayoutHidesReordersAndResets() {
    var layout = HomeLayout.standard
    layout.setVisible(.recent, false)
    layout.setVisible(.genres, true)
    #expect(!layout.isVisible(.recent) && layout.isVisible(.genres))
    // Drag "Neu in der Mediathek" (4) above "Deine Mixe" (1).
    layout.move(fromOffsets: IndexSet(integer: 4), toOffset: 1)
    #expect(Array(layout.shelves.prefix(3)) == [.quickAccess, .newReleases, .mixes])
    // Dragging down counts the destination before the move.
    layout.move(fromOffsets: IndexSet(integer: 0), toOffset: 3)
    #expect(Array(layout.shelves.prefix(3)) == [.newReleases, .mixes, .quickAccess])
    #expect(layout.move(.newReleases, by: -1) == false, "The first shelf cannot move up.")
    #expect(layout.move(.newReleases, by: 1) && layout.shelves.first == .mixes)
    #expect(!layout.isStandard)
    layout.reset()
    #expect(layout == .standard)
}

@Test func homeLayoutMovesShownShelvesAroundHiddenOnes() {
    var layout = HomeLayout.standard
    layout.setVisible(.jumpBackIn, false); layout.setVisible(.recent, false)
    #expect(layout.visibleShelves == [.quickAccess, .mixes, .newReleases, .frequent])
    // In the editor "Neu in der Mediathek" is the third shown shelf; drag it to the top.
    layout.moveVisible(fromOffsets: IndexSet(integer: 2), toOffset: 0)
    #expect(layout.visibleShelves == [.newReleases, .quickAccess, .mixes, .frequent])
    #expect(Array(layout.shelves.prefix(5)) == [.newReleases, .quickAccess, .jumpBackIn, .recent, .mixes], "Hidden shelves keep their places.")
    #expect(layout.moveVisible(.frequent, by: -1))
    #expect(layout.visibleShelves == [.newReleases, .quickAccess, .frequent, .mixes])
    #expect(layout.moveVisible(.newReleases, by: 1))
    #expect(layout.visibleShelves == [.quickAccess, .newReleases, .frequent, .mixes])
    #expect(!layout.moveVisible(.mixes, by: 1) && !layout.moveVisible(.recent, by: 1), "Not past the end, not a hidden shelf.")
}

@Test func homeLayoutPinsUpToEightTilesOnce() {
    var layout = HomeLayout.standard
    #expect(layout.pin(HomePin(kind: .playlist, itemID: "p0", title: "Abends unterwegs")))
    #expect(!layout.pin(HomePin(kind: .playlist, itemID: "p0", title: "Doppelt")), "A tile is pinned once.")
    #expect(!layout.pin(HomePin(kind: .album, itemID: "", title: "Ohne ID")))
    for index in 1..<HomeLayout.pinLimit { layout.pin(HomePin(kind: .album, itemID: "r\(index)", title: "Album \(index)")) }
    #expect(layout.pins.count == HomeLayout.pinLimit && !layout.canPin)
    #expect(!layout.pin(HomePin(kind: .genre, itemID: "Pop", title: "Pop")), "Quick access holds eight tiles.")
    #expect(layout.isPinned(.playlist, "p0") && !layout.isPinned(.album, "p0"))
    layout.movePins(fromOffsets: IndexSet(integer: 0), toOffset: HomeLayout.pinLimit)
    #expect(layout.pins.last?.itemID == "p0")
    layout.unpin(.playlist, "p0")
    #expect(!layout.isPinned(.playlist, "p0") && layout.canPin)
}

@Test func homeLayoutSurvivesStorageAndReadsOldOrForeignDataTolerantly() throws {
    var layout = HomeLayout.standard
    layout.setVisible(.frequent, false); layout.setVisible(.playlists, true)
    layout.move(fromOffsets: IndexSet(integer: 10), toOffset: 0)
    layout.pin(HomePin(kind: .album, itemID: "r1", title: "Zeitlos", subtitle: "Mira"))
    let restored = HomeLayout.decode(layout.encoded())
    #expect(restored == layout)
    let json = try #require(layout.encoded()).utf8String
    #expect(json.contains(#""version":1"#) && json.contains(#""quick_access""#))

    // A layout from an older app: unknown shelves and pins are ignored, shelves
    // it does not know join at their default place with their default visibility.
    let old = Data(#"""
    {"version":1,"shelves":[{"id":"recent","visible":false},{"id":"podcasts","visible":true},{"id":"quick_access","visible":true},
     {"id":"mixes"},{"id":7},"broken",{"id":"recent","visible":true},{"id":"new_releases","visible":true},{"id":"frequent","visible":true},
     {"id":"discover","visible":false},{"id":"top_artists","visible":false},{"id":"favorites","visible":false},{"id":"playlists","visible":false}],
     "pins":[{"kind":"podcast","id":"x","title":"X"},{"kind":"genre","id":"Pop"},{"kind":"mix","id":"","title":"Leer"},42]}
    """#.utf8)
    let read = HomeLayout.decode(old)
    #expect(read.shelves.prefix(3) == [.recent, .quickAccess, .mixes], "The saved order stays; a repeated shelf counts once.")
    #expect(!read.isVisible(.recent) && read.isVisible(.mixes))
    #expect(read.shelves.count == HomeShelf.allCases.count)
    #expect(read.shelves.firstIndex(of: .jumpBackIn) == 3, "A new shelf joins after the one it follows by default.")
    #expect(read.isVisible(.jumpBackIn) && !read.isVisible(.genres))
    #expect(read.shelves.firstIndex(of: .genres) == read.shelves.firstIndex(of: .topArtists).map { $0 + 1 })
    #expect(read.pins == [HomePin(kind: .genre, itemID: "Pop", title: "Pop")])
    // A newer version, garbage and nothing all read without failing.
    let newer = HomeLayout.decode(Data(#"{"version":9,"shelves":[{"id":"genres","visible":true,"size":"large"}],"pins":[],"theme":"x"}"#.utf8))
    #expect(newer.isVisible(.genres) && newer.shelves.count == HomeShelf.allCases.count)
    #expect(HomeLayout.decode(Data("not json".utf8)) == .standard)
    #expect(HomeLayout.decode(nil) == .standard)
    #expect(HomeLayout.storageKey(server: "HTTPS://Music.Example", userID: "u1") == "homeLayout.https://music.example|u1")
}

// MARK: Recently started sources

@Test func recentSourcesRememberStartedCollectionsNewestFirst() {
    var recent = RecentSources()
    #expect(recent.record(QueueContext(kind: "album", id: "r0", title: "Nachtfahrt")))
    #expect(recent.record(QueueContext(kind: "playlist", id: "p0", title: "Abends unterwegs")))
    #expect(!recent.record(QueueContext(kind: "search", title: "Suche: „x“")), "Search results have no place to return to.")
    #expect(!recent.record(QueueContext(kind: "history", title: "Zuletzt gehört")))
    #expect(!recent.record(QueueContext(kind: "album", title: "Ohne ID")))
    #expect(recent.record(QueueContext(kind: "favorites", title: "Lieblingstitel")))
    #expect(recent.record(QueueContext(kind: "album", id: "r0", title: "Nachtfahrt")))
    #expect(recent.entries.map(\.id) == ["album:r0", "favorites:", "playlist:p0"])
    #expect(recent.entries[0].label == "Album" && recent.entries[1].context == QueueContext(kind: "favorites", title: "Lieblingstitel"))
    for index in 0..<20 { recent.record(QueueContext(kind: "mix", id: "m\(index)", title: "Mix \(index)")) }
    #expect(recent.entries.count == RecentSources.limit && recent.entries.first?.itemID == "m19")
    let restored = RecentSources.decode(recent.encoded())
    #expect(restored == recent)
    let foreign = Data(#"[{"kind":"album","id":"r1","title":"Zeitlos"},{"kind":"podcast","id":"x","title":"X"},{"broken":true},3]"#.utf8)
    #expect(RecentSources.decode(foreign).entries.map(\.id) == ["album:r1"])
    #expect(RecentSources.decode(Data("{}".utf8)).entries.isEmpty)
    recent.remove("mix:m19")
    #expect(recent.entries.first?.itemID == "m18")
}

private extension Data {
    var utf8String: String { String(decoding: self, as: UTF8.self) }
}
