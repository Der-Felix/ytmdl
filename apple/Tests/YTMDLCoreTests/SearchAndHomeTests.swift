import Testing
@testable import YTMDLCore

@Test func searchMatchPrefersWholeNamesThenPrefixesThenContains() {
    #expect(SearchMatch.score("Nordlicht", query: "nordlicht") == 3)
    #expect(SearchMatch.score("Björk", query: "bjork") == 3, "Diacritics are ignored.")
    #expect(SearchMatch.score("Nordlicht", query: "Nord") == 2)
    #expect(SearchMatch.score("Die Ärzte", query: "arz") == 2, "A word start counts as a prefix.")
    #expect(SearchMatch.score("Nachtfahrt", query: "fahrt") == 1)
    #expect(SearchMatch.score("Zeitlos", query: "xy") == 0)
    #expect(SearchMatch.score("Zeitlos", query: "   ") == 0)
}

@Test func topHitPicksTheBestNameAndBreaksTiesByKind() {
    // The artist named exactly like the query wins over a title containing it.
    let artist = SearchMatch.topHit(query: "Mira", artists: ["Nordlicht", "Mira"], releases: ["Mirage"], tracks: ["Mira Mira"], playlists: [])
    #expect(artist?.kind == .artist && artist?.index == 1)
    // Equal scores: album before title.
    let release = SearchMatch.topHit(query: "Zeit", artists: [], releases: ["Zeitlos"], tracks: ["Zeitreise"], playlists: [])
    #expect(release?.kind == .release && release?.index == 0)
    // A playlist only wins when it matches best.
    let playlist = SearchMatch.topHit(query: "abends unterwegs", artists: ["Abend"], releases: [], tracks: ["Unterwegs"], playlists: ["Abends unterwegs"])
    #expect(playlist?.kind == .playlist)
    // The server matched another field: the first title it sent.
    let fallback = SearchMatch.topHit(query: "qq", artists: ["A"], releases: ["B"], tracks: ["C", "D"], playlists: [])
    #expect(fallback?.kind == .track && fallback?.index == 0)
    #expect(SearchMatch.topHit(query: "qq", artists: [], releases: [], tracks: [], playlists: []) == nil)
}

@Test func recentSearchesKeepTenUniqueEntriesNewestFirst() {
    var recent = RecentSearches()
    recent.record("Nordlicht"); recent.record("Mira"); recent.record(" nordlicht ")
    #expect(recent.entries == ["nordlicht", "Mira"], "The same search moves up instead of repeating.")
    recent.record("a")
    #expect(recent.entries.count == 2, "One character is not a search.")
    for index in 0..<12 { recent.record("Suche \(index)") }
    #expect(recent.entries.count == RecentSearches.limit)
    #expect(recent.entries.first == "Suche 11")
    recent.remove("Suche 11")
    #expect(recent.entries.first == "Suche 10")
    recent.clear()
    #expect(recent.entries.isEmpty)
    // A saved list is restored in its order and cleaned up.
    let restored = RecentSearches(["Neu", "x", "Alt", "neu"])
    #expect(restored.entries == ["Neu", "Alt"])
    #expect(RecentSearches.storageKey(server: "HTTPS://Music.Example", userID: "u1") != RecentSearches.storageKey(server: "https://music.example", userID: "u2"))
}

@Test func greetingFollowsTheTimeOfDay() {
    #expect(HomeGreeting.text(hour: 6) == "Guten Morgen")
    #expect(HomeGreeting.text(hour: 12) == "Guten Tag")
    #expect(HomeGreeting.text(hour: 20) == "Guten Abend")
    #expect(HomeGreeting.text(hour: 2) == "Guten Abend")
}
