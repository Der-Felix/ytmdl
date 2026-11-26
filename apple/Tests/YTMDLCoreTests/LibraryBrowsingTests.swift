import Foundation
import Testing
@testable import YTMDLCore

private func plain(_ text: String) -> String {
    text.replacingOccurrences(of: "\u{00A0}", with: " ").replacingOccurrences(of: "\u{202F}", with: " ")
}

@Test func relativeDatesAreGermanWhateverTheDeviceLanguage() {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = TimeZone(identifier: "Europe/Berlin")!
    let now = Date(timeIntervalSince1970: 1_795_000_000)
    let threeDays = plain(GermanDates.relative(now.addingTimeInterval(-3 * 86_400), to: now, calendar: calendar))
    #expect(threeDays == "vor 3 Tagen")
    let hour = plain(GermanDates.relative(now.addingTimeInterval(-3_600), to: now, calendar: calendar))
    #expect(hour == "vor 1 Stunde")
    let yesterday = GermanDates.relative(now.addingTimeInterval(-86_400), to: now, calendar: calendar)
    #expect(yesterday == "gestern")
    #expect(!threeDays.contains("ago") && !hour.contains("ago"))
}

@Test func timesAndDaysAreGerman() {
    let berlin = TimeZone(identifier: "Europe/Berlin")!
    // 26 November 2026, 15:04 in Berlin (UTC+1).
    let date = Date(timeIntervalSince1970: 1_795_701_840)
    #expect(GermanDates.time(date, timeZone: berlin) == "15:04")
    let day = GermanDates.day(date, timeZone: berlin)
    #expect(day.contains("Donnerstag") && day.contains("November"))
}

@Test func librarySortAndLayoutPersist() throws {
    let suite = "LibraryBrowsingTests-\(UUID().uuidString)"
    let defaults = try #require(UserDefaults(suiteName: suite))
    defer { defaults.removePersistentDomain(forName: suite) }
    #expect(LibraryPreferences.sort(in: defaults) == .recent, "Newly added first by default.")
    #expect(LibraryPreferences.layout(in: defaults) == .grid)
    LibraryPreferences.setSort(.year, in: defaults)
    LibraryPreferences.setLayout(.list, in: defaults)
    let reopened = try #require(UserDefaults(suiteName: suite))
    #expect(LibraryPreferences.sort(in: reopened) == .year)
    #expect(LibraryPreferences.layout(in: reopened) == .list)
    defaults.set("unknown", forKey: LibraryPreferences.sortKey)
    #expect(LibraryPreferences.sort(in: defaults) == .recent, "An unknown stored value falls back.")
}

@Test func librarySortAsksTheServerInTheRightDirection() {
    #expect(LibraryAlbumSort.recent.queryItems == [.init(name: "sort", value: "recent"), .init(name: "order", value: "desc")])
    #expect(LibraryAlbumSort.year.order == "desc")
    #expect(LibraryAlbumSort.title.order == "asc")
    #expect(LibraryAlbumSort.artist.queryItems.first?.value == "artist")
    #expect(LibraryAlbumSort.allCases.map(\.label) == ["Zuletzt hinzugefügt", "Titel", "Künstler", "Jahr"])
}

@Test func artistIndexFoldsLettersAndPutsOthersLast() {
    #expect(LibraryIndex.letter(for: "Ärzte") == "A")
    #expect(LibraryIndex.letter(for: "østlich") == "O")
    #expect(LibraryIndex.letter(for: "  mira") == "M")
    #expect(LibraryIndex.letter(for: "42 Grad") == "#")
    #expect(LibraryIndex.letter(for: "") == "#")
    let sections = LibraryIndex.sections(["Nordlicht", "42 Grad", "Mira", "Ankerplatz", "Ärzte", "mars"]) { $0 }
    #expect(sections.map(\.letter) == ["A", "M", "N", "#"])
    #expect(sections[0].items == ["Ankerplatz", "Ärzte"])
    #expect(sections[1].items == ["mars", "Mira"])
}

@Test func monogramIsStableAndShort() {
    #expect(ArtistMonogram.initials("Nordlicht") == "N")
    #expect(ArtistMonogram.initials("Blaue Stunde Orchester") == "BS")
    #expect(ArtistMonogram.initials("  ") == "#")
    #expect(ArtistMonogram.hue("Mira") == ArtistMonogram.hue("mira"))
    #expect((0..<1).contains(ArtistMonogram.hue("Nordlicht")))
}

@Test func albumTracksGroupByDiscOnlyWhenThereAreSeveral() {
    func track(_ id: String, disc: Int?) -> Track { Track(id: id, title: id, artists: [], album: "A", durationMs: 1000, discNumber: disc) }
    let single = DiscGroup.groups([track("a", disc: nil), track("b", disc: 1)])
    #expect(single.count == 1 && single[0].positions == [0, 1])
    let double = DiscGroup.groups([track("a", disc: 1), track("b", disc: 1), track("c", disc: 2)])
    #expect(double.map(\.disc) == [1, 2])
    #expect(double[1].positions == [2])
    #expect(DiscGroup.groups([]).isEmpty)
}

@Test func collectionSummaryReadsLikeTheApp() {
    #expect(CollectionSummary.line(year: 2026, trackCount: 12, durationMs: 48 * 60_000) == "2026 · 12 Titel · 48 Min.")
    #expect(CollectionSummary.line(year: 0, trackCount: 1, durationMs: 125 * 60_000) == "1 Titel · 2 Std. 5 Min.")
    #expect(CollectionSummary.line(trackCount: 100, durationMs: 0, partial: true) == "100 geladene Titel")
}

@Test func artistAlbumsSortByYearWithUnknownYearsLast() {
    let releases = [Release(id: "a", title: "Beta", artists: [], year: 2020), Release(id: "b", title: "Alpha", artists: [], year: 0),
                    Release(id: "c", title: "Gamma", artists: [], year: 2024), Release(id: "d", title: "Delta", artists: [], year: 2020)]
    #expect(ArtistAlbumSort.newest.apply(releases).map(\.id) == ["c", "a", "d", "b"])
    #expect(ArtistAlbumSort.oldest.apply(releases).map(\.id) == ["a", "d", "c", "b"])
    #expect(ArtistAlbumSort.title.apply(releases).map(\.id) == ["b", "a", "d", "c"])
}

@Test func releaseTypesDecodeAndSplitSinglesFromAlbums() throws {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
    let json = #"[{"id":"r1","title":"Eins","artists":["Mira"],"year":2026,"release_type":"single","album_artist":"Mira"},{"id":"r2","title":"Zwei","artists":null,"year":null,"release_type":""}]"#
    let releases = try decoder.decode([Release].self, from: Data(json.utf8))
    #expect(releases[0].isSingleOrEP && releases[0].kindLabel == "Single" && releases[0].albumArtist == "Mira")
    #expect(!releases[1].isSingleOrEP && releases[1].releaseType == nil && releases[1].kindLabel == "Album")
    let track = try decoder.decode(Track.self, from: Data(#"{"id":"t","title":"T","artists":[],"duration_ms":1,"disc_number":2,"track_number":3}"#.utf8))
    #expect(track.discNumber == 2 && track.trackNumber == 3)
}
