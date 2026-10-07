import Foundation
import Testing
@testable import YTMDLCore

@Test func serverOriginAndTransportPolicy() throws {
    for invalid in ["https://user:secret@example.com", "https://example.com/path", "https://example.com?token=x", "file:///tmp/music", "http://example.com", "http://172.32.0.1", "http://127.0.0.1.evil.example", "http://192.168.2.999", "http://+127.0.0.1", "http://0127.0.0.1"] {
        #expect(throws: PlayerError.self) { try ServerAddress(invalid, allowLocalHTTP: true) }
    }
    #expect(throws: PlayerError.self) { try ServerAddress("http://192.168.50.10:8080") }
    let server = try ServerAddress("http://192.168.50.10:8080", allowLocalHTTP: true)
    #expect(!server.isSecure)
    #expect(try server.endpoint("/auth/status").host == "192.168.50.10")
    #expect(throws: PlayerError.self) { try server.itemPath("tracks", id: "../auth/logout") }
    #expect(throws: PlayerError.self) { try server.endpoint("//evil.example/%2e%2e") }
}

@Test func selectedTrackBeyondQueueLimitStartsTheCorrectWindow() {
    let tracks = (0..<1100).map { Track(id: String($0), title: String($0), artists: [], album: "", durationMs: 100) }
    var queue = PlaybackQueue()
    queue.replace(tracks, start: 555)
    #expect(queue.tracks.count == 500)
    #expect(queue.current?.id == "555")
    let advanced = queue.next(repeatAll: false)
    #expect(advanced)
    #expect(queue.current?.id == "556")
}

@Test func decodesRealServerEnvelopes() throws {
    let decoder = JSONDecoder(); decoder.keyDecodingStrategy = .convertFromSnakeCase
    let raw = Data(#"{"data":[{"id":"a","title":"Test","artists":["Artist"],"album":"Album","duration_ms":150000,"codec":"opus"}],"meta":{"total":109223}}"#.utf8)
    let result = try decoder.decode(Envelope<[Track]>.self, from: raw)
    #expect(result.meta?.total == 109223); #expect(result.data[0].duration == 150)
}

@Test func queueDoesNotLoseCurrentTrackOrAutoplayPastEnd() {
    let a = Track(id: "a", title: "A", artists: [], album: "", durationMs: 100)
    let b = Track(id: "b", title: "B", artists: [], album: "", durationMs: 100)
    var queue = PlaybackQueue()
    queue.replace([a,b], start: 1); queue.shuffleUpcoming()
    #expect(queue.current == b); let didAdvance = queue.next(repeatAll: false); #expect(!didAdvance); #expect(queue.current == b)
    let didRepeat = queue.next(repeatAll: true); #expect(didRepeat); #expect(queue.current == a)
    queue.replace([]); queue.previous(); #expect(queue.current == nil); let emptyAdvance = queue.next(repeatAll: true); #expect(!emptyAdvance)
    queue.replace(Array(repeating: a, count: 600)); #expect(queue.tracks.count == 500)
    queue.replace(Array(repeating: a, count: 600), start: 599); #expect(queue.tracks.count == 1); #expect(queue.current == a)
}

@Test func queueEditsPreserveCurrentOccurrenceAndDuplicateEntries() {
    let a = Track(id: "a", title: "A", artists: [], album: "", durationMs: 100)
    let b = Track(id: "b", title: "B", artists: [], album: "", durationMs: 100)
    let c = Track(id: "c", title: "C", artists: [], album: "", durationMs: 100)
    var queue = PlaybackQueue()
    queue.replace([a, b, a, c], start: 1)
    let currentRemoval = queue.remove(at: 1), negativeRemoval = queue.remove(at: -1), invalidRemoval = queue.remove(at: 99)
    #expect(!currentRemoval && !negativeRemoval && !invalidRemoval)
    let moved = queue.playNext(at: 3)
    #expect(moved && queue.tracks == [a, b, c, a] && queue.current == b)
    let earlierRemoved = queue.remove(at: 0)
    #expect(earlierRemoved && queue.index == 0 && queue.current == b)
    let cleared = queue.clearUpcoming()
    #expect(cleared && queue.tracks == [b])
    let noUpcoming = queue.clearUpcoming(), noMove = queue.playNext(at: 0)
    #expect(!noUpcoming && !noMove)
    queue.replace([a, b, a], start: 0)
    let duplicateRemoved = queue.remove(at: 2)
    #expect(duplicateRemoved && queue.tracks == [a, b] && queue.current == a)
    queue.replace([])
    let emptyClear = queue.clearUpcoming(), emptyRemove = queue.remove(at: 0), emptyMove = queue.playNext(at: 0)
    #expect(!emptyClear && !emptyRemove && !emptyMove)
}

@Test func queueReorderingPreservesCurrentOccurrenceWithDuplicateTracks() {
    let tracks = ["same", "b", "same", "c"].map { Track(id: $0, title: $0, artists: [], album: "", durationMs: 1000) }
    var queue = PlaybackQueue(); queue.replace(tracks, start: 2)
    let movedFirst = queue.move(from: 0, to: 3); #expect(movedFirst)
    #expect(queue.index == 1 && queue.current == tracks[2])
    let movedBack = queue.move(from: 3, to: 0); #expect(movedBack)
    #expect(queue.index == 2)
    let movedCurrent = queue.move(from: 2, to: 0); #expect(movedCurrent)
    #expect(queue.index == 0 && queue.tracks.map(\.id) == ["same", "same", "b", "c"])
    let invalidSource = queue.move(from: -1, to: 0), invalidTarget = queue.move(from: 0, to: 4); #expect(!invalidSource && !invalidTarget)
    let unchanged = queue.move(from: 0, to: 0); #expect(!unchanged)
}

@Test func insertingNextRetainsDuplicatesCurrentAndQueueBound() {
    let track = Track(id: "same", title: "Fixture", artists: [], album: "", durationMs: 1000)
    var queue = PlaybackQueue(); queue.replace([track, track], start: 1)
    let inserted = queue.insertNext(track)
    #expect(inserted && queue.index == 1 && queue.tracks.count == 3)
    queue.replace(Array(repeating: track, count: 500))
    let overLimit = queue.insertNext(track); #expect(!overLimit && queue.tracks.count == 500)
    queue.replace([]); let first = queue.insertNext(track)
    #expect(first && queue.current == track && queue.index == 0)
}
