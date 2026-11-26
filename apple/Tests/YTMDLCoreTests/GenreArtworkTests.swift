import Foundation
import Testing
@testable import YTMDLCore

// Genre tiles with covers, and the request rules of the 0.6.2 stability pass.
// Mutating calls stay outside the macros; only their results are checked.

private func album(_ id: String, _ artist: String) -> Release { Release(id: id, title: id.uppercased(), artists: [artist]) }

@Test func genreCoversPreferDifferentArtistsAndStayFew() {
    let releases = [album("r1", "Mira"), album("r2", "Mira"), album("r3", "Nordlicht"), album("r1", "Mira"), album("r4", "Östlich"), album("r5", "Ankerplatz")]
    #expect(GenreCovers.pick(releases) == ["r1", "r3", "r4", "r5"], "One per artist first, each release once, four at most.")
    #expect(GenreCovers.pick([album("a", "X"), album("b", "x")]) == ["a", "b"], "The same artist fills up when there is nothing else.")
    #expect(GenreCovers.pick([]).isEmpty)
    #expect(GenreCovers.key(" Électro ") == GenreCovers.key("electro"))
    #expect(!GenreCovers.isBrowsable("__none__") && !GenreCovers.isBrowsable("  ") && GenreCovers.isBrowsable("Pop"))
    #expect(GenreCovers.requestLimit == 4 && GenreCovers.concurrency == 4)
}

@Test func genreCoverLayoutFollowsTheNumberOfCovers() {
    #expect(GenreCoverLayout(nil) == .tint && GenreCoverLayout([]) == .tint)
    #expect(GenreCoverLayout(["a"]) == .single("a"))
    #expect(GenreCoverLayout(["a", "b", "c"]) == .pair("a", "b"))
    #expect(GenreCoverLayout(["a", "b", "c", "d", "e"]) == .mosaic(["a", "b", "c", "d"]))
    #expect(GenreCoverLayout(["a", "b"]).ids == ["a", "b"])
}

@Test func genreCoverIndexSurvivesARelaunchAndStaysBounded() {
    var index = GenreCoverIndex()
    let first = index.set("pop", ["r1", "r3"])
    let same = index.set("pop", ["r1", "r3"])
    #expect(first && !same, "The same covers are no change.")
    index.set("jazz", [])
    let restored = GenreCoverIndex.decode(index.encoded())
    #expect(restored == index && restored["pop"] == ["r1", "r3"] && restored["jazz"] == [], "A genre without albums is remembered as such.")
    #expect(GenreCoverIndex.decode(Data("nonsense".utf8)).count == 0)
    #expect(GenreCoverIndex.decode(nil).count == 0)
    var small = GenreCoverIndex()
    for number in 0..<5 { small.set("g\(number)", ["r\(number)"], limit: 3) }
    small.set("g2", ["r9"], limit: 3)
    #expect(small.count == 3 && small["g0"] == nil && small["g1"] == nil && small["g2"] == ["r9"], "The least recently loaded genres go first.")
    let odd = Data(#"{"version":1,"entries":[{"key":"","ids":["x"]},{"key":"rock","ids":["a","a","","b","c","d","e"]}]}"#.utf8)
    #expect(GenreCoverIndex.decode(odd)["rock"] == ["a", "b", "c", "d"])
    #expect(GenreCovers.storageKey(server: "HTTPS://Music.Example", userID: "u1") == "genreCovers.https://music.example|u1")
}

@Test func genreCoverQueueLoadsEachGenreOnceWithLimitedConcurrency() {
    var queue = GenreCoverQueue(limit: 2, backlog: 3)
    let now = Date()
    let pop = queue.enqueue("pop", now: now), rock = queue.enqueue("rock", now: now), jazz = queue.enqueue("jazz", now: now)
    #expect(pop && rock && jazz)
    let popAgain = queue.enqueue("pop", now: now)
    #expect(!popAgain, "A waiting genre is not asked twice.")
    // The most recently asked first: what is on screen now.
    let firstRun = queue.next(), secondRun = queue.next(), thirdRun = queue.next()
    #expect(firstRun == "pop", "Seen again, pop moved to the front.")
    #expect(secondRun == "jazz")
    #expect(thirdRun == nil, "Never more than the limit at a time.")
    let whileRunning = queue.enqueue("pop", now: now)
    #expect(!whileRunning, "A running genre is not asked again.")
    queue.finish("pop", succeeded: true, now: now)
    let afterLoad = queue.enqueue("pop", now: now)
    #expect(!afterLoad, "A loaded genre is never fetched again in this session (no refetch per scroll).")
    let rockRun = queue.next()
    #expect(rockRun == "rock")
    queue.finish("rock", succeeded: false, now: now)
    let tooSoon = queue.enqueue("rock", now: now.addingTimeInterval(5))
    let later = queue.enqueue("rock", now: now.addingTimeInterval(GenreCovers.retryInterval + 1))
    #expect(!tooSoon && later, "A failure waits before the next try.")
    // Fast scrolling: the backlog keeps the newest.
    for genre in ["a", "b", "c", "d"] { queue.enqueue(genre, now: now) }
    #expect(queue.waiting == ["b", "c", "d"])
    queue.withdraw("d")
    #expect(queue.waiting == ["b", "c"], "A tile that left the screen before its turn is not loaded.")
    queue.markLoaded("b")
    let loadedB = queue.enqueue("b", now: now)
    #expect(queue.waiting == ["c"] && !loadedB, "Covers from the genre page count as loaded.")
    // A reloaded library: loaded genres may be asked once more, a waiting one stays.
    queue.forgetLoaded()
    let popRefresh = queue.enqueue("pop", now: now)
    #expect(queue.waiting == ["c", "pop"] && popRefresh)
    queue.reset()
    let afterReset = queue.enqueue("pop", now: now)
    #expect(queue.running.isEmpty && queue.waiting == ["pop"] && afterReset)
}

@Test func onlyTheNewestRequestMayAnswer() {
    var requests = LatestRequests<StatsPeriod>()
    let week = requests.begin(.week)
    let month = requests.begin(.month)
    let weekAgain = requests.begin(.week)
    #expect(!requests.isCurrent(week), "The first week request is stale once the week is asked again.")
    #expect(requests.isCurrent(weekAgain) && requests.isCurrent(month), "Other keys are not affected.")
    requests.end(week)
    #expect(requests.isRunning(.week), "A stale request ending does not clear the newer one.")
    requests.end(weekAgain)
    #expect(!requests.isRunning(.week) && requests.isRunning(.month))
    var search = LatestRequests<String>()
    let slow = search.begin("search")
    let fast = search.begin("search")
    #expect(!search.isCurrent(slow) && search.isCurrent(fast), "A late answer for an old text is dropped.")
}

@Test func theFeedReloadsAfterAWhileOrANewDay() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = try #require(TimeZone(identifier: "Europe/Berlin"))
    let morning = try #require(calendar.date(from: DateComponents(year: 2026, month: 11, day: 26, hour: 9)))
    #expect(!FeedFreshness.needsRefresh(loadedAt: nil, now: morning, calendar: calendar), "Nothing loaded: the page loads by itself.")
    #expect(!FeedFreshness.needsRefresh(loadedAt: morning, now: morning.addingTimeInterval(10 * 60), calendar: calendar))
    #expect(FeedFreshness.needsRefresh(loadedAt: morning, now: morning.addingTimeInterval(31 * 60), calendar: calendar))
    let late = try #require(calendar.date(from: DateComponents(year: 2026, month: 11, day: 26, hour: 23, minute: 55)))
    #expect(FeedFreshness.needsRefresh(loadedAt: late, now: late.addingTimeInterval(10 * 60), calendar: calendar), "After midnight the mixes are new.")
    #expect(FeedFreshness.needsRefresh(loadedAt: morning, now: morning.addingTimeInterval(-60), calendar: calendar), "A clock set back counts as stale.")
}

@Test func aTopUpNeverOverwritesTheCursorOfANewerStation() {
    let before = StationPaging(key: "artist:a1", nonce: "n1")
    var advanced = before
    advanced.advance(RadioStationPage(title: "Radio", tracks: [], nextCursor: "c2"))
    #expect(StationPaging.committing(advanced, startedFrom: before, current: before) == advanced)
    let replaced = StationPaging(key: "genre:Pop", nonce: "n2")
    #expect(StationPaging.committing(advanced, startedFrom: before, current: replaced) == nil, "A station started meanwhile keeps its own cursor.")
    #expect(StationPaging.committing(advanced, startedFrom: nil, current: nil) == advanced, "A restored queue without cursor starts paging.")
}

@Test func liftedExclusionsDoNotComeBackFromAListLoadedMeanwhile() {
    let server = [ListeningPreference(id: "p1", kind: .genre, target: "Schlager", name: "Schlager"),
                  ListeningPreference(id: "p2", kind: .artist, target: "a1", name: "Mira")]
    var preferences = ListeningPreferences(server)
    preferences.addPending(.track, target: "t9", name: "Neu", id: "pending-9")
    let removed = preferences.beginRemoval(.genre, "schlager")
    #expect(removed?.entry.id == "p1" && !preferences.contains(.genre, "Schlager"))
    // The page reloads before the server confirmed the removal: the old list still has it.
    let reloaded = preferences.merged(with: server)
    #expect(!reloaded.contains(.genre, "Schlager"), "The lifted exclusion stays out.")
    #expect(reloaded.contains(.track, "t9") && reloaded.contains(.artist, "a1"), "The pending exclusion stays, the others come from the server.")
    preferences = reloaded
    preferences.endRemoval(.genre, "schlager")
    #expect(preferences.removing.isEmpty)
    #expect(preferences.merged(with: server).contains(.genre, "Schlager"), "Once answered, the server's list decides again.")
}
