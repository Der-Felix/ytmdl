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
    #expect(ListeningTracker.playThreshold(duration: 40, rate: 0.1) == 40)
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

private func order(_ section: QueueSection) -> Int {
    switch section { case .played: 0; case .current: 1; case .user: 2; case .context: 3; case .autoplay: 4 }
}
