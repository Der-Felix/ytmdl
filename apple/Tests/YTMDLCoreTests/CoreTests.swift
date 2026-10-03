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
