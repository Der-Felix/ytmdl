import Foundation
import Testing
@testable import YTMDLCore

private func track(_ id: String) -> Track { Track(id: id, title: id.uppercased(), artists: [], album: "", durationMs: 200_000) }
private func moved(_ offset: Int, _ insertion: Int, _ positions: [Int]) -> [Int]? {
    PlaybackQueue.listMove(from: offset, to: insertion, positions: positions).map { [$0.source, $0.destination] }
}

@Test func shuffleModeShufflesTheContextAndTurnsBackToItsOrder() {
    var queue = PlaybackQueue()
    let ids = (0..<30).map { "c\($0)" }
    queue.replace(ids.map(track), start: 0, context: QueueContext(kind: "album", id: "r", title: "Album"))
    queue.insertNext(track("u"))
    _ = queue.appendAutoplay([track("x")])
    #expect(!queue.isShuffled)
    queue.setShuffle(true)
    #expect(queue.isShuffled)
    // Queued stays next, autoplay stays last, the context is mixed.
    #expect(queue.tracks[1].id == "u" && queue.tracks.last?.id == "x")
    #expect(Set(queue.tracks[2..<31].map(\.id)) == Set(ids.dropFirst()))
    #expect(Array(queue.tracks[2..<31].map(\.id)) != Array(ids.dropFirst()), "29 titles keeping their order is practically impossible")
    queue.setShuffle(false)
    #expect(!queue.isShuffled)
    #expect(queue.tracks.map(\.id) == ["c0", "u"] + Array(ids.dropFirst()) + ["x"])
}

@Test func shuffleOffKeepsLaterChangesAndANewQueueStartsUnshuffled() {
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c", "d", "a"].map(track), start: 0, context: QueueContext(kind: "playlist", id: "p", title: "P"))
    queue.setShuffle(true)
    // Played on by one, removed one and queued one: the rest of the context
    // returns to its order, the queued title stays next.
    _ = queue.next(repeatAll: false)
    let removed = queue.remove(at: 2)
    #expect(removed)
    queue.append(track("q"))
    queue.setShuffle(false)
    let upcomingContext = queue.tracks.indices.filter { $0 > queue.index && queue.origin(at: $0) == .context }.map { queue.tracks[$0].id }
    #expect(upcomingContext.count == 2)
    let original = ["b", "c", "d", "a"]
    var cursor = original.startIndex
    for id in upcomingContext {
        guard let found = original[cursor...].firstIndex(of: id) else { Issue.record("\(upcomingContext) is out of order"); break }
        cursor = original.index(after: found)
    }
    #expect(queue.tracks[queue.index + 1].id == "q" && queue.origin(at: queue.index + 1) == .user)
    queue.setShuffle(true)
    queue.replace(["n"].map(track))
    #expect(!queue.isShuffled)
}

@Test func repeatCyclesOffAllOneAndReadsTheFlagsOfThePlayer() {
    #expect(RepeatMode(all: false, one: false) == .off)
    #expect(RepeatMode(all: true, one: false) == .all)
    #expect(RepeatMode(all: true, one: true) == .one && RepeatMode(all: false, one: true) == .one)
    #expect(RepeatMode.off.next == .all && RepeatMode.all.next == .one && RepeatMode.one.next == .off)
    #expect(RepeatMode.one.symbol == "repeat.1" && RepeatMode.all.symbol == "repeat" && !RepeatMode.off.isActive)
    #expect(RepeatMode.allCases.map(\.label) == ["Wiederholung aus", "Warteschlange wiederholen", "Einzeltitel wiederholen"])
}

@Test func playbackClockClampsTheScrubberAndItsLabels() {
    #expect(PlaybackClock.fraction(30, of: 120) == 0.25)
    #expect(PlaybackClock.fraction(200, of: 120) == 1 && PlaybackClock.fraction(-3, of: 120) == 0)
    #expect(PlaybackClock.fraction(30, of: 0) == 0 && PlaybackClock.fraction(30, of: .nan) == 0)
    #expect(PlaybackClock.position(atFraction: 0.5, of: 200) == 100)
    #expect(PlaybackClock.position(atFraction: 1.4, of: 200) == 200 && PlaybackClock.position(atFraction: 0.5, of: 0) == 0)
    #expect(PlaybackClock.remaining(50, of: 200) == 150 && PlaybackClock.remaining(250, of: 200) == 0)
    #expect(PlaybackClock.remaining(50, of: .infinity) == 0)
    #expect(PlaybackClock.stepped(195, by: 10, of: 200) == 200 && PlaybackClock.stepped(4, by: -10, of: 200) == 0)
    #expect(PlaybackClock.stepped(4, by: 10, of: 0) == 14)
}

@Test func libraryMatchFindsArtistsAndAlbumsByName() {
    #expect(LibraryMatch.index(of: " beyonce ", in: ["Adele", "Beyoncé"]) == 1)
    #expect(LibraryMatch.index(of: "", in: [""]) == nil && LibraryMatch.index(of: "X", in: ["Y"]) == nil)
    let albums: [(title: String, artists: [String])] = [("Live", ["Other"]), ("Live", ["Band"]), ("Studio", [])]
    #expect(LibraryMatch.releaseIndex(title: "live", artists: ["Band"], in: albums) == 1)
    #expect(LibraryMatch.releaseIndex(title: "Live", artists: ["Nobody"], in: albums) == nil)
    #expect(LibraryMatch.releaseIndex(title: "Live", artists: [], in: albums) == 0)
    #expect(LibraryMatch.releaseIndex(title: "Studio", artists: ["Band"], in: albums) == 2)
}

@Test func listMovesTranslateToQueueMoves() {
    let positions = [4, 5, 6]
    #expect(moved(0, 2, positions) == [4, 5])
    #expect(moved(0, 3, positions) == [4, 6])
    #expect(moved(2, 0, positions) == [6, 4])
    #expect(moved(1, 1, positions) == nil && moved(1, 2, positions) == nil && moved(3, 0, positions) == nil)
    // Applied to a queue, the dragged row lands where it was dropped.
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c", "d"].map(track), start: 0)
    let move = PlaybackQueue.listMove(from: 0, to: 3, positions: [1, 2, 3])
    #expect(move != nil)
    if let move {
        let applied = queue.move(from: move.source, to: move.destination)
        #expect(applied)
    }
    #expect(queue.tracks.map(\.id) == ["a", "c", "d", "b"])
}
