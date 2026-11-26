import Foundation
import Testing
@testable import YTMDLCore

// The shapes of docs/features/listening.md (server 1.11) and the app's rules
// for an older server.

private func decoder() -> JSONDecoder {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase; return decoder
}
private func read<T: Decodable & Sendable>(_ type: T.Type, _ json: String) throws -> T {
    try decoder().decode(Envelope<T>.self, from: Data(json.utf8)).data
}
private func value(_ items: [URLQueryItem], _ name: String) -> String? { items.first { $0.name == name }?.value }
private func song(_ id: String, title: String? = nil, artists: [String] = [], artistId: String? = nil) -> Track {
    Track(id: id, title: title ?? id.uppercased(), artists: artists, album: "", durationMs: 200_000, artistId: artistId)
}

// MARK: Features

@Test func healthFeaturesDecideWhatTheAppOffers() throws {
    let current = try read(ServerHealth.self, #"""
    {"data": {"status": "ok", "version": "1.11.0", "uptime_seconds": 42, "checks": {"database": "ok"},
      "features": ["mixes.groups", "mixes.artist", "mixes.decade", "mixes.favorites", "mixes.daypart",
                   "radio.seeds", "radio.station", "history.stats", "search.lyrics", "listening.preferences"]}}
    """#)
    #expect(current.version == "1.11.0" && current.features.announced)
    #expect(current.features.stations && current.features.statistics && current.features.lyrics && current.features.preferences && current.features.mixTimeZone)
    let old = try read(ServerHealth.self, #"{"data": {"status": "ok", "version": "1.10.1", "uptime_seconds": 42}}"#)
    #expect(!old.features.announced && old.features == .none)
    #expect(!old.features.stations && !old.features.statistics && !old.features.lyrics && !old.features.preferences)
    // Only some features, an unreadable name and an explicit null.
    let partial = try read(ServerHealth.self, #"{"data": {"status": "ok", "features": ["history.stats", 7, " search.lyrics "]}}"#)
    #expect(partial.features.announced && partial.features.statistics && partial.features.lyrics && !partial.features.stations)
    #expect(partial.version == nil)
    let null = try read(ServerHealth.self, #"{"data": {"status": "ok", "version": "1.10.0", "features": null}}"#)
    #expect(!null.features.announced)
    #expect(HomeShelf.unavailable(on: old.features) == [.stats] && HomeShelf.unavailable(on: current.features).isEmpty)
}

// MARK: Mixes

@Test func newMixKindsReadWithTheirDetailsAndGroups() throws {
    let mixes = try read([PersonalMix].self, #"""
    {"data": [
      {"id": "genre-3f1a9c0b2d4e", "kind": "genre", "title": "Dein Mix 1", "description": "Deine Favoriten aus Rock und Titel, die du kaum kennst",
       "group": "made_for_you", "genre": "Rock", "track_count": 50, "cover_track_ids": ["t1", "t2", "t3", "t4"], "cover_release_ids": ["r1", "r2", "r3", "r4"]},
      {"id": "daypart-evening", "kind": "daypart", "title": "Dein Abend-Mix", "description": "Was du abends gern hörst",
       "group": "made_for_you", "daypart": "evening", "track_count": 38, "cover_track_ids": ["t9"], "cover_release_ids": ["r9"]},
      {"id": "artist-9b0c11d2e3f4", "kind": "artist", "title": "Mix: Daft Punk", "description": "Daft Punk und ähnliche Künstler",
       "group": "made_for_you", "artist_id": "a1", "track_count": 50, "cover_track_ids": ["t5"], "cover_release_ids": ["r5"]},
      {"id": "decade-1990", "kind": "decade", "title": "Deine 90er", "description": "Deine Titel aus den 1990er-Jahren und mehr aus dem Jahrzehnt",
       "group": "rewind", "decade": 1990, "track_count": 50, "cover_track_ids": ["t7"], "cover_release_ids": ["r7"]},
      {"id": "favorites", "kind": "favorites", "title": "Favoriten-Mix", "description": "", "track_count": 20, "cover_track_ids": null, "cover_release_ids": null},
      {"id": "forgotten_favorites", "kind": "forgotten_favorites", "title": "Vergessene Favoriten", "track_count": 12, "decade": "odd"}
    ]}
    """#)
    #expect(mixes.count == 6)
    #expect(mixes[1].daypart == "evening" && mixes[1].label == "Abends" && mixes[1].symbol == "sunset")
    #expect(mixes[2].artistId == "a1" && mixes[2].artistName == "Daft Punk" && mixes[2].label == "Künstler-Mix")
    #expect(mixes[3].decade == 1990 && mixes[3].label == "1990er-Jahre")
    #expect(mixes[4].label == "Deine Favoriten" && mixes[4].coverTrackIds.isEmpty)
    #expect(mixes[5].decade == nil && mixes[5].label == "Lange nicht gehört", "An unreadable detail never drops the mix.")
    #expect(mixes[0].artistName == nil)
    // Without `group` (an older server) the kind decides.
    let grouped = MixGroup.sections(mixes.map { PersonalMix(id: $0.id, kind: $0.kind, title: $0.title, trackCount: $0.trackCount) })
    #expect(grouped.map(\.group) == [.madeForYou, .rewind])
    #expect(grouped[0].mixes.map(\.id) == ["genre-3f1a9c0b2d4e", "daypart-evening", "artist-9b0c11d2e3f4", "favorites"])
    #expect(grouped[1].mixes.map(\.id) == ["decade-1990", "forgotten_favorites"])
    #expect(PersonalMix.daypartLabel("morning") == "Morgens" && PersonalMix.daypartLabel("dawn") == nil)
}

// MARK: Radio stations

@Test func serverStationsSeedEveryKindAndPageWithTheCursor() throws {
    let track = RadioStation(.track(id: "t1", title: "Nachtfahrt"))
    let first = track.stationQuery(nonce: "n", limit: 50)
    #expect(value(first, "seed") == "t1" && value(first, "nonce") == "n" && value(first, "limit") == "50")
    #expect(value(first, "cursor") == nil && value(first, "exclude") == nil)
    let artist = RadioStation(.artist(id: "a1", name: "Daft Punk"))
    #expect(value(artist.stationQuery(nonce: "n", limit: 12), "artist") == "a1", "The server seeds an artist radio itself: no title needed.")
    #expect(value(RadioStation(.album(id: "r5", title: "Discovery")).stationQuery(nonce: "n", limit: 12), "release") == "r5")
    #expect(value(RadioStation(.genre("Pop")).stationQuery(nonce: "n", limit: 12), "genre") == "Pop")
    let next = artist.stationQuery(nonce: "n", limit: 500, cursor: "abc.25", exclude: ["t1", "", "t2"])
    #expect(value(next, "cursor") == "abc.25" && value(next, "exclude") == "t1,t2" && value(next, "limit") == "100")
    #expect(value(next, "artist") == "a1", "Every batch repeats the seed, so an expired station is rebuilt.")
    #expect(value(artist.stationQuery(nonce: "n", limit: 0), "limit") == "1")
    let many = (0..<700).map { "t\($0)" }
    #expect(value(track.stationQuery(nonce: "n", limit: 12, exclude: many), "exclude")?.split(separator: ",").count == 500)
    // The server's title names the queue; the id stays the station's.
    #expect(artist.context(serverTitle: "Radio: Daft Punk") == QueueContext(kind: "radio", id: "artist:a1", title: "Radio: Daft Punk"))
    #expect(artist.context(serverTitle: " ").title == "Radio: Daft Punk")
    #expect(RadioStation(context: artist.context(serverTitle: "Radio: Daft Punk")) == artist)

    let page = try read(RadioStationPage.self, #"""
    {"data": {"title": "Radio: Daft Punk", "seed": {"kind": "artist", "id": "a1", "name": "Daft Punk"},
      "tracks": [{"id": "t5", "title": "Digital Love", "artists": ["Daft Punk"], "duration_ms": 301000, "artist_id": "a1", "release_id": "r5"},
                 {"id": "t6", "title": "Ohne Länge"}],
      "next_cursor": "5f2c0e9a1b7d3c44aa01e2f3.25", "remaining": 212}}
    """#)
    #expect(page.title == "Radio: Daft Punk" && page.seed?.kind == "artist" && page.remaining == 212)
    #expect(page.tracks.map(\.id) == ["t5", "t6"] && page.tracks[0].artistId == "a1" && page.tracks[0].releaseId == "r5")
    #expect(page.tracks[1].durationMs == 0, "A short track object still reads.")
    var paging = StationPaging(key: "artist:a1", nonce: "n")
    #expect(paging.cursor == nil && !paging.exhausted)
    paging.advance(page)
    #expect(paging.cursor == "5f2c0e9a1b7d3c44aa01e2f3.25" && !paging.exhausted)
    let last = try read(RadioStationPage.self, #"{"data": {"title": "Radio: Daft Punk", "tracks": null, "next_cursor": null, "remaining": 0}}"#)
    paging.advance(last)
    #expect(paging.exhausted && paging.cursor == nil && last.tracks.isEmpty)
    #expect(StationPaging.exclusions(queue: ["a", "b"], played: ["x", "b", "y"]) == ["a", "b", "y", "x"])
    #expect(StationPaging.exclusions(queue: ["a", "b", "c"], played: [], limit: 2) == ["a", "b"])
}

@Test func olderServersKeepTheDeviceSeededRadio() {
    // Without `radio.station` the 0.6.0 queries stay exactly as they were.
    let artist = RadioStation(.artist(id: "a0", name: "Nordlicht"))
    #expect(artist.needsSeedTrack && artist.query(nonce: "n") == nil)
    let seeded = RadioStation(.artist(id: "a0", name: "Nordlicht"), seedTrackID: "t2")
    #expect(seeded.query(nonce: "n").flatMap { value($0, "seed") } == "t2")
    #expect(seeded.query(nonce: "n").flatMap { value($0, "artist") } == nil, "An old server would read artist= as the personal radio.")
    #expect(seeded.query(nonce: "n").flatMap { value($0, "limit") } == nil)
    #expect(value(seeded.stationQuery(nonce: "n", limit: 5), "artist") == "a0" && value(seeded.stationQuery(nonce: "n", limit: 5), "seed") == nil)
}

// MARK: Statistics

private let weekJSON = #"""
{"data": {"period": "week", "from": "2026-11-19T13:00:00Z", "to": "2026-11-26T13:00:00Z",
  "since": "2026-02-03T18:21:07Z", "complete": true, "source": "events", "time_zone": "Europe/Berlin",
  "totals": {"plays": 41, "distinct_tracks": 33, "distinct_artists": 19, "listening_ms": 9512000, "untimed_plays": 0},
  "top_tracks": [{"track": {"id": "t5", "title": "Digital Love"}, "plays": 4}],
  "top_artists": [{"id": "a1", "name": "Daft Punk", "plays": 9, "distinct_tracks": 6}],
  "top_releases": [{"id": "r5", "title": "Discovery", "artist_id": "a1", "artist_name": "Daft Punk", "plays": 7}],
  "top_genres": [{"name": "Electronic", "plays": 15}],
  "activity": {"bucket": "day", "points": [{"start": "2026-11-19", "plays": 3, "listening_ms": 690000}]}}}
"""#

@Test func statisticsReadTheContractExample() throws {
    let stats = try read(ListeningStats.self, weekJSON)
    #expect(stats.period == "week" && stats.complete && stats.source == "events" && stats.timeZone == "Europe/Berlin")
    #expect(stats.totals.plays == 41 && stats.totals.distinctTracks == 33 && stats.totals.distinctArtists == 19 && stats.totals.listeningMs == 9_512_000)
    #expect(stats.topTracks.first?.track.title == "Digital Love" && stats.topTracks.first?.plays == 4)
    #expect(stats.topArtists.first?.name == "Daft Punk" && stats.topArtists.first?.distinctTracks == 6)
    #expect(stats.topReleases.first?.artistId == "a1" && stats.topReleases.first?.artistName == "Daft Punk")
    #expect(stats.topGenres.first?.name == "Electronic" && stats.topGenres.first?.plays == 15)
    #expect(stats.activity.bucket == "day" && !stats.activity.isMonthly && stats.activity.points.first?.plays == 3)
    #expect(stats.since != nil && stats.from != nil && !stats.isEmpty)
    #expect(StatsFormat.note(stats) == nil)
    let berlin = try #require(TimeZone(identifier: "Europe/Berlin"))
    let point = try #require(stats.activity.points.first)
    #expect(point.day == DateComponents(year: 2026, month: 11, day: 19))
    let noon = try #require(point.date(in: berlin))
    var calendar = Calendar(identifier: .gregorian); calendar.timeZone = berlin
    #expect(calendar.component(.day, from: noon) == 19 && calendar.component(.hour, from: noon) == 12)
    #expect(ListeningStats.ActivityPoint(start: "2026-13-01", plays: 1).day == nil)
}

@Test func statisticsExplainIncompleteWindowsAndEmptyOnes() throws {
    let partial = try read(ListeningStats.self, #"""
    {"data": {"period": "year", "from": "2025-11-26T13:00:00Z", "to": "2026-11-26T13:00:00Z", "since": "2026-02-03T18:21:07Z",
      "complete": false, "source": "events", "time_zone": "Europe/Berlin",
      "totals": {"plays": 2, "distinct_tracks": 2, "distinct_artists": 1, "listening_ms": 400000, "untimed_plays": 0},
      "top_tracks": null, "top_artists": [{"id": "a1", "name": "Daft Punk", "plays": 2, "distinct_tracks": 2}, {"name": "kein id"}],
      "top_releases": null, "top_genres": null, "activity": {"bucket": "month", "points": [{"start": "2026-02-01", "plays": 2, "listening_ms": 400000}]}}}
    """#)
    #expect(partial.topTracks.isEmpty && partial.topArtists.count == 1, "A row without an id is left out, null lists read as empty.")
    #expect(partial.activity.isMonthly)
    #expect(StatsFormat.note(partial) == "Dein Verlauf beginnt am 3. Februar 2026. Die Zahlen zählen ab dann.")
    let all = try read(ListeningStats.self, #"""
    {"data": {"period": "all", "from": null, "to": "2026-11-26T13:00:00Z", "since": null, "complete": false, "source": "aggregate",
      "totals": {"plays": 12, "distinct_tracks": 4, "distinct_artists": 2, "listening_ms": 3600000, "untimed_plays": 3},
      "activity": {"bucket": "month", "points": []}}}
    """#)
    #expect(all.from == nil && all.totals.untimedPlays == 3)
    #expect(StatsFormat.note(all) == "3 Wiedergaben ohne Zeitangabe zählen mit, fehlen aber im Verlauf.")
    let empty = try read(ListeningStats.self, #"{"data": {"period": "month", "complete": true, "totals": {"plays": 0}, "activity": null}}"#)
    #expect(empty.isEmpty && empty.activity.points.isEmpty)
    #expect(StatsFormat.listeningTime(9_512_000) == "ca. 2 Std. 38 Min.")
    #expect(StatsFormat.listeningTime(42 * 60_000) == "ca. 42 Min." && StatsFormat.listeningTime(3_600_000) == "ca. 1 Std.")
    #expect(StatsFormat.listeningTime(312 * 3_600_000 + 5 * 60_000) == "ca. 312 Std.")
    #expect(StatsFormat.plays(1) == "1 Wiedergabe" && StatsFormat.plays(41) == "41 Wiedergaben")
    #expect(StatsFormat.share(9, of: 9) == 1 && StatsFormat.share(1, of: 1000) == 0.04 && StatsFormat.share(0, of: 9) == 0)
    #expect(StatsPeriod.allCases.map(\.title) == ["Woche", "Monat", "Jahr", "Gesamt"])
}

// MARK: Lyrics

@Test func lyricsHitsReadAndHighlightWithoutCaseOrAccents() throws {
    let results = try read(SearchResults.self, #"""
    {"data": {"artists": [], "releases": [], "tracks": [],
      "lyrics": [{"track": {"id": "t9", "title": "Über den Wolken"},
                  "snippet": "… Abend / Über den Wolken muss die Freiheit wohl grenzenlos sein …",
                  "match": "Über den Wolken"}]}}
    """#)
    let hit = try #require(results.lyrics.first)
    #expect(hit.track.title == "Über den Wolken" && hit.id == "t9")
    let marked = LyricsHighlight.ranges(in: hit.snippet, match: hit.match, query: "über den wol")
    #expect(marked.map { String(hit.snippet[$0]) } == ["Über den Wolken"])
    // When the match is not in the snippet, the typed words are marked where a word starts.
    let words = LyricsHighlight.ranges(in: hit.snippet, match: "", query: "uber WOL")
    #expect(words.map { String(hit.snippet[$0]) } == ["Über", "Wol"])
    let snippet = "Jeden Abend den Weg"
    #expect(LyricsHighlight.ranges(in: snippet, match: "", query: "den").map { String(snippet[$0]) } == ["den"], "Not inside another word.")
    #expect(LyricsHighlight.ranges(in: snippet, match: "nicht da", query: "").isEmpty)
    #expect(LyricsHighlight.ranges(in: "Mädchen", match: "madchen", query: "").map { String("Mädchen"[$0]) } == ["Mädchen"])
    // An older server sends no lyrics group, a newer one may send null.
    #expect(try read(SearchResults.self, #"{"data": {"artists": [], "releases": [], "tracks": []}}"#).lyrics.isEmpty)
    #expect(try read(SearchResults.self, #"{"data": {"artists": null, "releases": [], "tracks": [], "lyrics": null}}"#).artists.isEmpty)
}

// MARK: Preferences

@Test func preferencesReadAndUpdateOptimistically() throws {
    let list = try read([ListeningPreference].self, #"""
    {"data": [
      {"id": "9f1c", "kind": "track", "target": "t5", "name": "Digital Love", "artist_name": "Daft Punk", "created_at": "2026-11-26T12:00:00Z"},
      {"id": "77ab", "kind": "genre", "target": "Schlager", "name": "Schlager", "created_at": "2026-11-25T09:30:00Z"},
      {"id": "03de", "kind": "artist", "target": "a7", "name": "DJ Ötzi", "created_at": "2026-11-24T20:00:00Z"},
      {"id": "x", "kind": "mood", "target": "laut", "name": "Laut"}
    ]}
    """#)
    var preferences = ListeningPreferences(list)
    #expect(preferences.entries.count == 3, "An unknown kind is left out.")
    #expect(preferences.entries(of: .track).first?.artistName == "Daft Punk")
    #expect(preferences.contains(.genre, "schlager") && preferences.contains(.artist, "a7") && !preferences.contains(.artist, "A7"))
    #expect(preferences.entries[0].createdAt != nil)

    // Exclude: shown at once, confirmed by the server's answer.
    let pending = try #require(preferences.addPending(.artist, target: "a1", name: "Daft Punk", id: "pending-1"))
    #expect(pending.isPending && preferences.entries.first?.id == "pending-1")
    let again = preferences.addPending(.artist, target: "a1", name: "Daft Punk")
    #expect(again == nil, "The same exclusion is not added twice.")
    preferences.confirm("pending-1", with: ListeningPreference(id: "aa11", kind: .artist, target: "a1", name: "Daft Punk"))
    #expect(preferences.entries.first?.id == "aa11" && preferences.entries.count == 4)
    // A failed exclusion is rolled back.
    preferences.addPending(.track, target: "t9", name: "Fehler", id: "pending-2")
    let rolledBack = preferences.remove(id: "pending-2")
    #expect(rolledBack?.index == 0 && !preferences.contains(.track, "t9"))
    // Allow again: removed at once, restored at its place when the server refuses.
    let allowed = try #require(preferences.remove(.genre, "SCHLAGER"))
    #expect(allowed.index == 2 && !preferences.contains(.genre, "Schlager"))
    preferences.restore(allowed.entry, at: allowed.index)
    #expect(preferences.entries.map(\.id) == ["aa11", "9f1c", "77ab", "03de"])
    preferences.restore(allowed.entry, at: 0)
    #expect(preferences.entries.count == 4, "Restoring twice keeps one entry.")
}

@Test func exclusionsMatchQueuedTitlesOnTheDevice() {
    let reference = song("t5", title: "Digital Love", artists: ["Daft Punk"], artistId: "a1")
    let live = song("t5-live", title: "digital love", artists: ["daft punk"])
    let other = song("t6", title: "Aerodynamic", artists: ["Daft Punk"], artistId: "a1")
    let stranger = song("t7", title: "Digital Love", artists: ["Cover Band"])
    #expect(SuggestionFilter.matches(reference, kind: .track, target: "t5", name: "Digital Love"))
    #expect(SuggestionFilter.matches(live, kind: .track, target: "t5", name: "Digital Love", reference: reference), "Every version of the recording.")
    #expect(!SuggestionFilter.matches(stranger, kind: .track, target: "t5", name: "Digital Love", reference: reference))
    #expect(SuggestionFilter.matches(other, kind: .artist, target: "a1", name: "Daft Punk"))
    #expect(SuggestionFilter.matches(live, kind: .artist, target: "a1", name: "Daft Punk"), "By name when the title has no artist id.")
    #expect(!SuggestionFilter.matches(stranger, kind: .artist, target: "a1", name: "Daft Punk"))
    let genres = ["daft punk": ["Electronic"], "cover band": ["Pop"]]
    #expect(SuggestionFilter.matches(other, kind: .genre, target: "electronic", name: "Electronic", artistGenres: genres))
    #expect(!SuggestionFilter.matches(stranger, kind: .genre, target: "Electronic", name: "Electronic", artistGenres: genres))
}

@Test func excludingRemovesOnlySuggestedUpcomingTitles() {
    let tracks = (0..<6).map { song("t\($0)", artists: [$0 % 2 == 0 ? "A" : "B"]) }
    var radio = PlaybackQueue()
    radio.replace(tracks, start: 0, context: QueueContext(kind: "radio", id: "track:t0", title: "Radio: T0"))
    radio.insertNext(song("u1", artists: ["A"]))
    radio.appendAutoplay([song("auto", artists: ["A"])])
    #expect(radio.playsSuggestions)
    let removed = radio.removeUpcomingSuggestions { $0.artists == ["A"] }
    #expect(removed == 3, "t2, t4 and the autoplay title; the queued one and the current one stay.")
    #expect(radio.tracks.map(\.id) == ["t0", "u1", "t1", "t3", "t5"])
    #expect(radio.current?.id == "t0")

    var album = PlaybackQueue()
    album.replace(tracks, start: 1, context: QueueContext(kind: "album", id: "r1", title: "Album"))
    album.appendAutoplay([song("auto", artists: ["A"])])
    #expect(!album.playsSuggestions)
    let fromAlbum = album.removeUpcomingSuggestions { $0.artists == ["A"] }
    #expect(fromAlbum == 1 && album.tracks.map(\.id) == ["t0", "t1", "t2", "t3", "t4", "t5"], "An album the listener chose keeps its titles.")
    #expect(album.index == 1)
    var played = PlaybackQueue()
    played.replace(tracks, start: 5, context: QueueContext(kind: "mix", id: "m", title: "Mix"))
    let nothing = played.removeUpcomingSuggestions { _ in true }
    #expect(nothing == 0 && played.tracks.count == 6, "Played titles stay in the history part.")
}

// MARK: Start

@Test func statisticsShelfIsOptionalAndSkippedWhereTheServerLacksIt() {
    var layout = HomeLayout.standard
    #expect(!layout.isVisible(.stats) && layout.shelves.last == .stats)
    layout.setVisible(.stats, true)
    let shown = layout.visibleShelves
    #expect(shown.last == .stats)
    // The editor of an older server leaves the shelf out; moving the rest keeps it in place.
    let editable = layout.visibleShelves(excluding: [.stats])
    #expect(!editable.contains(.stats) && editable.count == shown.count - 1)
    layout.moveVisible(fromOffsets: IndexSet(integer: 0), toOffset: editable.count, excluding: [.stats])
    #expect(layout.visibleShelves.last == .stats && layout.visibleShelves(excluding: [.stats]).last == editable.first)
    let moved = layout.moveVisible(.stats, by: -1, excluding: [.stats])
    #expect(!moved)
    let pin = HomePin(kind: .stats, itemID: "stats", title: "Statistik")
    let pinned = layout.pin(pin)
    #expect(pinned && layout.isPinned(.stats, "stats"))
    #expect(HomeLayout.decode(layout.encoded()).isPinned(.stats, "stats"))
}
