import Foundation
import Testing
@testable import YTMDLCore

private func decoder() -> JSONDecoder {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase; return decoder
}
private func track(_ id: String) -> Track { Track(id: id, title: id.uppercased(), artists: [], album: "", durationMs: 200_000) }

@Test func decodesPersonalMixesAsTheServerSendsThem() throws {
    let raw = Data(#"""
    {"data":[
      {"id":"mix_on_repeat_1","kind":"on_repeat","title":"In Dauerschleife","description":"Deine meistgespielten Titel der letzten 30 Tage","track_count":25,"cover_track_ids":["t1","t2"],"cover_release_ids":["r1"]},
      {"id":"mix_genre_1","kind":"genre","title":"Dein Mix 1","description":"Deine Favoriten aus Pop","genre":"Pop","track_count":50,"cover_track_ids":null,"cover_release_ids":null},
      {"id":"mix_new","kind":"new","title":"Neu für dich","description":"","track_count":12}
    ]}
    """#.utf8)
    let mixes = try decoder().decode(Envelope<[PersonalMix]>.self, from: raw).data
    #expect(mixes.count == 3)
    #expect(mixes[0].trackCount == 25 && mixes[0].coverTrackIds == ["t1", "t2"] && mixes[0].coverReleaseIds == ["r1"])
    #expect(mixes[1].genre == "Pop" && mixes[1].coverTrackIds.isEmpty && mixes[1].label == "Pop")
    #expect(mixes[0].label == "Meistgehört" && mixes[2].label == "Neuzugänge" && mixes[2].genre == nil)
    #expect(PersonalMix.ordered(mixes).map(\.id) == ["mix_genre_1", "mix_on_repeat_1", "mix_new"])
    #expect(mixes[1].queueContext == QueueContext(kind: "mix", id: "mix_genre_1", title: "Dein Mix 1"))
    let genreWithoutName = try decoder().decode(PersonalMix.self, from: Data(#"{"id":"g","kind":"genre","title":"Dein Mix 2","genre":"","track_count":10}"#.utf8))
    #expect(genreWithoutName.label == "Genre-Mix")
}

@Test func decodesOneMixWithItsTracks() throws {
    let raw = Data(#"{"data":{"id":"mix_genre_1","kind":"genre","title":"Dein Mix 1","description":"x","genre":"Rock","track_count":2,"cover_track_ids":["a"],"cover_release_ids":[],"tracks":[{"id":"a","title":"A","artists":["X"],"album":"Al","duration_ms":1000},{"id":"b","title":"B","artists":null,"duration_ms":2000}]}}"#.utf8)
    let detail = try decoder().decode(Envelope<PersonalMixDetail>.self, from: raw).data
    #expect(detail.mix.title == "Dein Mix 1" && detail.mix.genre == "Rock")
    #expect(detail.tracks.map(\.id) == ["a", "b"] && detail.tracks[1].artists.isEmpty)
    let empty = try decoder().decode(PersonalMixDetail.self, from: Data(#"{"id":"m","kind":"new","title":"Neu","track_count":0,"tracks":null}"#.utf8))
    #expect(empty.tracks.isEmpty)
}

@Test func decodesHistoryAndTopTracksWithGoTimestamps() throws {
    let raw = Data(#"""
    {"data":[
      {"id":"a","title":"A","artists":["X"],"album":"Al","duration_ms":1000,"play_count":12,"last_played_at":"2026-11-24T18:03:04.123456789Z"},
      {"id":"b","title":"B","artists":["X"],"album":"Al","duration_ms":1000,"play_count":3,"last_played_at":"2026-11-24T19:03:04+01:00"},
      {"id":"c","title":"C","duration_ms":1000,"play_count":1,"last_played_at":"not a date"}
    ]}
    """#.utf8)
    let history = try decoder().decode(Envelope<[HistoryTrack]>.self, from: raw).data
    #expect(history.map(\.id) == ["a", "b", "c"] && history.map(\.playCount) == [12, 3, 1])
    let first = try #require(history[0].lastPlayedAt), second = try #require(history[1].lastPlayedAt)
    #expect(abs(first.timeIntervalSince1970 - 1_795_543_384.123) < 0.01)
    #expect(second.timeIntervalSince1970 == 1_795_543_384)
    #expect(history[2].lastPlayedAt == nil)
    let half = try #require(HistoryTrack.parseTimestamp("2026-11-24T18:03:04.5Z"))
    #expect(abs(half.timeIntervalSince1970 - 1_795_543_384.5) < 0.01)
}

@Test func aPlayCountsAfterHalfTheTrackOrThirtySecondsOfActualListening() {
    var tracker = ListeningTracker()
    #expect(ListeningTracker.playThreshold(duration: 200) == 30)
    #expect(ListeningTracker.playThreshold(duration: 40) == 20)
    #expect(ListeningTracker.playThreshold(duration: 40, rate: 2) == 10)
    #expect(ListeningTracker.playThreshold(duration: 40, rate: 0.1) == 30)
    #expect(ListeningTracker.playThreshold(duration: 20, rate: 0.1) == 20)
    var counted = 0
    for second in 0...40 { if tracker.tick(key: "a", playing: true, now: Double(second), duration: 200) { counted += 1 } }
    #expect(counted == 1 && tracker.listenedSeconds >= 30)
    // A seek or a long stall never adds more than one second per tick.
    tracker.reset()
    _ = tracker.tick(key: "b", playing: true, now: 0, duration: 200)
    let countedAfterStall = tracker.tick(key: "b", playing: true, now: 500, duration: 200)
    #expect(!countedAfterStall)
    #expect(tracker.listenedSeconds == 1)
    // Paused time does not count.
    _ = tracker.tick(key: "b", playing: false, now: 501, duration: 200)
    _ = tracker.tick(key: "b", playing: true, now: 900, duration: 200)
    #expect(tracker.listenedSeconds == 1)
    // A new key starts over.
    _ = tracker.tick(key: "c", playing: true, now: 901, duration: 200)
    #expect(tracker.listenedSeconds == 0)
}

@Test func skipNeedsTwoHeardSecondsNoPlayAndNoLongPause() {
    var tracker = ListeningTracker()
    _ = tracker.tick(key: "a", playing: true, now: 0, duration: 200)
    _ = tracker.tick(key: "a", playing: true, now: 1, duration: 200)
    let skipAfterOneSecond = tracker.takeSkip(key: "a", now: 1, playing: true)
    #expect(!skipAfterOneSecond, "One second is no judgement of the track.")
    _ = tracker.tick(key: "a", playing: true, now: 2, duration: 200)
    _ = tracker.tick(key: "a", playing: true, now: 3, duration: 200)
    let skipOfOtherKey = tracker.takeSkip(key: "other", now: 3, playing: true)
    #expect(!skipOfOtherKey)
    let firstSkip = tracker.takeSkip(key: "a", now: 3, playing: true)
    #expect(firstSkip)
    let secondSkip = tracker.takeSkip(key: "a", now: 3, playing: true)
    #expect(!secondSkip, "At most once per track start.")

    // Paused for longer than three minutes: the user left and came back.
    tracker.reset()
    for second in 0...5 { _ = tracker.tick(key: "b", playing: true, now: Double(second), duration: 200) }
    _ = tracker.tick(key: "b", playing: false, now: 6, duration: 200)
    let skipAfterLongPause = tracker.takeSkip(key: "b", now: 5 + 181, playing: false)
    #expect(!skipAfterLongPause)
    let skipAfterShortPause = tracker.takeSkip(key: "b", now: 5 + 179, playing: false)
    #expect(skipAfterShortPause)

    // Once the track counted as a play, moving on is no skip.
    tracker.reset()
    for second in 0...31 { _ = tracker.tick(key: "c", playing: true, now: Double(second), duration: 200) }
    let skipAfterPlay = tracker.takeSkip(key: "c", now: 31, playing: true)
    #expect(!skipAfterPlay)

    #expect(ListeningTracker.movesOn(from: "a", to: "b", failed: false))
    #expect(!ListeningTracker.movesOn(from: "a", to: "a", failed: false))
    #expect(!ListeningTracker.movesOn(from: "a", to: nil, failed: false))
    #expect(!ListeningTracker.movesOn(from: "a", to: "b", failed: true))
    #expect(ListeningEventKind.play.path == "/history" && ListeningEventKind.skip.path == "/history/skips")
}

@Test func queueKeepsUserContextAndAutoplaySections() {
    let context = QueueContext(kind: "album", id: "r1", title: "Album")
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c"].map(track), start: 0, context: context)
    #expect(queue.context == context && queue.origins == [.context, .context, .context])
    queue.append(track("u1")); queue.append(track("u2"))
    #expect(queue.tracks.map(\.id) == ["a", "u1", "u2", "b", "c"], "Queued tracks play before the rest of the album, in order.")
    queue.insertNext(track("n"))
    #expect(queue.tracks.map(\.id) == ["a", "n", "u1", "u2", "b", "c"])
    #expect((0..<queue.tracks.count).map { queue.section(at: $0) } == [.current, .user, .user, .user, .context, .context])
    let added = queue.appendAutoplay(["c", "x", "y", "x"].map(track))
    #expect(added == 2 && queue.tracks.suffix(2).map(\.id) == ["x", "y"] && queue.section(at: 7) == .autoplay)
    #expect(queue.autoplaySeed?.id == "c")
    #expect(queue.remaining == 7)
    // Moving an autoplay track directly after the current one queues it.
    let moved = queue.move(from: 7, to: 1)
    #expect(moved && queue.tracks[1].id == "y" && queue.origin(at: 1) == .user)
    // Moving into the context run takes the context origin.
    let movedBack = queue.move(from: 1, to: 6)
    #expect(movedBack && queue.tracks[6].id == "y" && queue.origin(at: 6) == .context)
    // "Als Nächstes" from the queue menu.
    let promoted = queue.playNext(at: 7)
    #expect(promoted && queue.tracks[1].id == "x" && queue.origin(at: 1) == .user)
    let removed = queue.remove(at: 1)
    #expect(removed && queue.tracks.count == queue.origins.count)
    queue.shuffleUpcoming()
    let sections = (1..<queue.tracks.count).map { queue.section(at: $0) }
    #expect(sections == sections.sorted { order($0) < order($1) }, "Shuffle keeps queued tracks first and autoplay last.")
    #expect(queue.tracks[1...3].map(\.id) == ["n", "u1", "u2"], "Only the started context is shuffled.")
    queue.select(2)
    #expect(queue.section(at: 0) == .played && queue.section(at: 1) == .played && queue.section(at: 2) == .current)
    let cleared = queue.clearUpcoming()
    #expect(cleared && queue.origins.count == 3)
    #expect(QueueSection.context.heading(context) == "Weiter aus: Album")
    #expect(QueueSection.context.heading(nil) == "Weiter in der Warteschlange")
    #expect(QueueSection.user.heading(nil) == "Als Nächstes" && QueueSection.autoplay.heading(nil) == "Ähnliche Titel")
}

@Test func queueAdditionsRespectTheLimitAndStartAnEmptyQueue() {
    var queue = PlaybackQueue()
    let first = queue.append(["a", "b"].map(track))
    #expect(first == 2 && queue.current?.id == "a" && queue.context == nil && queue.origins == [.context, .context])
    queue.replace(Array(repeating: track("z"), count: 499))
    let bounded = queue.append(["a", "b"].map(track))
    #expect(bounded == 1 && queue.tracks.count == PlaybackQueue.limit)
    let fullAutoplay = queue.appendAutoplay([track("q")]), fullNext = queue.insertNext([track("q")])
    #expect(fullAutoplay == 0 && fullNext == 0)
    var empty = PlaybackQueue()
    let idleAutoplay = empty.appendAutoplay([track("a")])
    #expect(idleAutoplay == 0, "Autoplay only extends a queue that plays.")
    let multi = empty.insertNext(["a", "b"].map(track))
    #expect(multi == 2 && empty.current?.id == "a")
    let next = empty.insertNext(["c", "d"].map(track))
    #expect(next == 2 && empty.tracks.map(\.id) == ["a", "c", "d", "b"] && empty.origin(at: 1) == .user)
}

@Test func queueRowsStartAHeadingForEachVisibleSection() {
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c", "d"].map(track), start: 1, context: QueueContext(kind: "playlist", id: "p", title: "Abends"))
    queue.append(track("u"))
    _ = queue.appendAutoplay([track("x")])
    let rows = queue.rows()
    #expect(rows.map(\.track.id) == ["a", "b", "u", "c", "d", "x"])
    #expect(rows.map(\.section) == [.played, .current, .user, .context, .context, .autoplay])
    #expect(rows.map(\.startsSection) == [true, true, true, true, false, true])
    let filtered = queue.rows { $0.id != "c" }
    #expect(filtered.map(\.position) == [0, 1, 2, 4, 5] && filtered[3].startsSection)
    #expect(queue.count(of: .context) == 2 && queue.count(of: .user) == 1 && queue.count(of: .played) == 1)
}

@Test func autoplayAppendsOneSmallBatchWithoutQueuedOrSessionTracks() {
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c"].map(track), start: 2, context: QueueContext(kind: "album", id: "r", title: "Album"))
    let similar = ["a", "p1", "d"] + (0..<30).map { "s\($0)" }
    // "p1" and "d" were played earlier in this session (before this queue).
    let first = queue.appendAutoplay(similar.map(track), excluding: ["p1", "d", "c"])
    #expect(first == PlaybackQueue.autoplayBatch)
    #expect(queue.tracks.suffix(12).map(\.id) == (0..<12).map { "s\($0)" })
    #expect(queue.count(of: .autoplay) == 12 && queue.tracks.count == queue.origins.count)
    // The next top-up continues with what is not in the queue yet.
    let second = queue.appendAutoplay(similar.map(track), excluding: ["p1", "d"])
    #expect(second == 12 && queue.tracks.suffix(12).map(\.id) == (12..<24).map { "s\($0)" })
    // Duplicates within one answer count once; a smaller batch is honoured.
    let small = queue.appendAutoplay(["x", "x", "y", "z"].map(track), batch: 2)
    #expect(small == 2 && queue.tracks.suffix(2).map(\.id) == ["x", "y"])
    #expect(queue.autoplaySeed?.id == "c", "Autoplay keeps seeding from the last title the user chose.")
    // Only excluded or known tracks: nothing is added.
    let nothing = queue.appendAutoplay(["a", "p1"].map(track), excluding: ["p1"])
    #expect(nothing == 0)
}

@Test func sessionPlayHistoryKeepsTheLast200AndStartsOverForAnotherScope() {
    var session = SessionPlayHistory()
    session.setScope("https://music.example\nanna")
    for index in 0..<250 { session.record("t\(index)") }
    #expect(session.ids.count == SessionPlayHistory.limit && session.ids.first == "t50" && session.ids.last == "t249")
    // Playing a track again moves it to the end instead of duplicating it.
    session.record("t60")
    #expect(session.ids.count == 200 && session.ids.last == "t60" && session.ids.filter { $0 == "t60" }.count == 1)
    session.record("")
    #expect(session.ids.last == "t60")
    // The same scope keeps the session; another account or server resets it.
    session.setScope("https://music.example\nanna")
    #expect(session.ids.count == 200)
    session.setScope("https://music.example\nben")
    #expect(session.ids.isEmpty && session.scope == "https://music.example\nben")
    session.record("a"); session.setScope(nil)
    #expect(session.ids.isEmpty && session.scope == nil)
}

@Test func restoringAQueueKeepsItsOriginsAndAWrongOriginListFallsBackToContext() {
    let context = QueueContext(kind: "mix", id: "m1", title: "Dein Mix 1")
    var original = PlaybackQueue()
    original.replace(["a", "b", "c"].map(track), start: 0, context: context)
    original.append(track("u"))
    _ = original.appendAutoplay([track("x")])
    original.select(1)
    var restored = PlaybackQueue()
    restored.replace(original.tracks, start: original.index, context: original.context, origins: original.entryOrigins)
    #expect(restored == original)
    #expect(restored.rows().map(\.section) == [.played, .current, .context, .context, .autoplay])
    #expect(QueueSection.context.heading(restored.context) == "Weiter aus: Dein Mix 1")
    var mismatched = PlaybackQueue()
    mismatched.replace(original.tracks, start: 1, context: context, origins: [.user])
    #expect(mismatched.origins == [QueueOrigin](repeating: .context, count: 5))
    // A selection beyond the 500 window keeps the origins of the window.
    let long = (0..<600).map { track("l\($0)") }
    var window = PlaybackQueue()
    window.replace(long, start: 550, origins: (0..<600).map { $0 == 551 ? QueueOrigin.user : QueueOrigin.context })
    #expect(window.tracks.first?.id == "l550" && window.origin(at: 1) == .user && window.origins.count == window.tracks.count)
}

@Test func mixPlaylistNameCarriesTheDateAndFitsTheServerLimit() throws {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = try #require(TimeZone(identifier: "Europe/Berlin"))
    let date = try #require(calendar.date(from: DateComponents(year: 2026, month: 3, day: 7, hour: 12)))
    #expect(PersonalMix.playlistName("Dein Mix 1", date: date, calendar: calendar) == "Dein Mix 1 · 07.03.2026")
    #expect(PersonalMix.playlistName("  ", date: date, calendar: calendar) == "Mix · 07.03.2026")
    let long = PersonalMix.playlistName(String(repeating: "Ä", count: 100), date: date, calendar: calendar)
    #expect(long.utf8.count <= 128 && long.hasSuffix(" · 07.03.2026") && long.hasPrefix("ÄÄ"))
}

private func order(_ section: QueueSection) -> Int {
    switch section { case .played: 0; case .current: 1; case .user: 2; case .context: 3; case .autoplay: 4 }
}

@Test func clearingUpcomingEntriesCanBeUndoneWithTheirOrigins() {
    let context = QueueContext(kind: "album", id: "r1", title: "Album")
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c"].map(track), start: 0, context: context)
    queue.insertNext(track("n"))
    _ = queue.appendAutoplay([track("x")])
    let before = queue
    let cleared = queue.clearUpcoming()
    #expect(cleared)
    let undo = QueueClearUndo(before: before, after: queue)
    #expect(undo.removedCount == 4 && !undo.clearedAll)
    let restored = undo.restore(from: queue)
    #expect(restored == before)
    #expect(restored?.entryOrigins == [.context, .user, .context, .context, .autoplay] && restored?.context == context)
    // Similar tracks autoplay appended after the clear give way to the removed entries.
    var topped = queue
    _ = topped.appendAutoplay([track("y"), track("z")])
    #expect(undo.restore(from: topped) == before)
    // Anything the user changed since ends the offer.
    var queued = queue
    queued.append(track("u"))
    #expect(undo.restore(from: queued) == nil)
    var replaced = queue
    replaced.replace([track("q")])
    #expect(undo.restore(from: replaced) == nil)
    var advanced = before
    _ = advanced.next(repeatAll: false)
    _ = advanced.clearUpcoming()
    #expect(undo.restore(from: advanced) == nil)
}

@Test func clearingTheWholeQueueCanBeUndoneUntilSomethingElseStarts() {
    let context = QueueContext(kind: "mix", id: "m", title: "Dein Mix 1")
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c"].map(track), start: 1, context: context)
    queue.append(track("u"))
    let before = queue
    queue.replace([])
    let undo = QueueClearUndo(before: before, after: queue)
    #expect(undo.clearedAll && undo.removedCount == 4)
    let restored = undo.restore(from: queue)
    #expect(restored?.current?.id == "b" && restored?.index == 1 && restored?.context == context)
    #expect(restored?.entryOrigins == [.context, .context, .user, .context])
    var started = queue
    started.append(track("z"))
    #expect(undo.restore(from: started) == nil, "A new queue is never replaced by the old one.")
    #expect(QueueClearUndo(before: queue, after: queue).restore(from: queue) == nil, "Nothing removed, nothing to undo.")
}

@Test func onlyUpcomingEntriesAreEditableAndFocusStaysNearby() {
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c", "d", "e"].map(track), start: 1)
    #expect(!queue.isUpcoming(0) && !queue.isUpcoming(1) && queue.isUpcoming(2) && queue.isUpcoming(4) && !queue.isUpcoming(5))
    #expect(!queue.canMoveUp(2) && queue.canMoveUp(3), "Nothing moves above the playing entry.")
    #expect(queue.canMoveDown(3) && !queue.canMoveDown(4) && !queue.canMoveDown(1) && !queue.canMoveDown(0))
    #expect(!queue.canPlayNext(2) && queue.canPlayNext(3) && !queue.canPlayNext(0))
    let first = queue.remove(at: 3)
    #expect(first && queue.focusAfterRemoving(3) == 3 && queue.tracks[3].id == "e", "Focus moves to the entry that took its place.")
    let second = queue.remove(at: 3)
    #expect(second && queue.focusAfterRemoving(3) == 2, "After the last row, focus moves to the one before.")
    var single = PlaybackQueue()
    #expect(single.focusAfterRemoving(0) == nil)
    single.replace([track("a")])
    #expect(!single.canMoveDown(0) && !single.isUpcoming(0))
}

@Test func mixTracksSortLikeTheWebMixPage() throws {
    let raw = Data(#"[{"id":"1","title":"beta","artists":["Zed"],"album":"B","duration_ms":3000,"track_number":2},{"id":"2","title":"Alpha","artists":null,"album_artist":"Ann","album":"a","duration_ms":1000,"track_number":null},{"id":"3","title":"gamma","artists":["bob"],"album":"b","duration_ms":3000,"track_number":1}]"#.utf8)
    let tracks = try decoder().decode([Track].self, from: raw)
    #expect(tracks[0].trackNumber == 2 && tracks[1].albumArtist == "Ann" && tracks[1].trackNumber == nil)
    func ids(_ sort: MixTrackSort, _ descending: Bool = false) -> [String] { sort.apply(tracks, descending: descending).map(\.id) }
    #expect(ids(.mix) == ["1", "2", "3"] && ids(.mix, true) == ["1", "2", "3"])
    #expect(ids(.title) == ["2", "1", "3"] && ids(.title, true) == ["3", "1", "2"], "Case does not matter.")
    #expect(ids(.artist) == ["2", "3", "1"], "Without artists the album artist counts.")
    #expect(ids(.album) == ["2", "1", "3"] && ids(.album, true) == ["1", "3", "2"], "Equal values keep the mix order.")
    #expect(ids(.duration) == ["2", "1", "3"] && ids(.duration, true) == ["1", "3", "2"])
    #expect(ids(.trackNumber) == ["2", "3", "1"])
    #expect(MixTrackSort.allCases.map(\.label) == ["Mix-Reihenfolge", "Titel", "Künstler", "Album", "Dauer", "Titelnummer"])
    // Older saved queues without the new details still decode.
    let old = try JSONDecoder().decode(Track.self, from: Data(#"{"id":"t","title":"T","artists":[],"album":"","durationMs":1}"#.utf8))
    #expect(old.albumArtist == nil && old.trackNumber == nil)
    let roundTrip = try JSONDecoder().decode(Track.self, from: JSONEncoder().encode(tracks[0]))
    #expect(roundTrip == tracks[0])
}
