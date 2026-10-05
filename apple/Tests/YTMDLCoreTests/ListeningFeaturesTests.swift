import Foundation
import Testing
@testable import YTMDLCore

@Test func timedLyricsFractionsMultipleTimesAndBoundarySeeking() {
    let timeline = LyricsTimeline("[ar:Artist]\n[00:01.5]First\n[00:03.25][00:10.005]Repeat\n[00:60]Invalid\nUntimed\n[00:00.050]Intro")
    #expect(timeline.lines.map(\.text) == ["Intro", "First", "Repeat", "Repeat"])
    #expect(timeline.lines.map(\.seconds) == [0.05, 1.5, 3.25, 10.005])
    #expect(timeline.activeIndex(at: 0) == nil)
    #expect(timeline.activeIndex(at: 1.5) == 1)
    #expect(timeline.activeIndex(at: 100) == 3)
    #expect(timeline.activeIndex(at: .nan) == nil)
    #expect(LyricsTimeline(String(repeating: "x", count: 300000)).lines.isEmpty)
}

@Test func deviceHandoffUsesBoundedQueueAndNativeWireKeys() throws {
    let track = Track(id: "song", title: "Fixture", artists: [], album: "", durationMs: 1000)
    var queue = PlaybackQueue(); queue.replace([track, track], start: 1)
    let payload = HandoffPayload(queue: queue, position: 0.5, repeatMode: "track", sourceName: "Fixture")
    let encoder = JSONEncoder(); encoder.keyEncodingStrategy = .convertToSnakeCase
    let object = try #require(JSONSerialization.jsonObject(with: encoder.encode(payload)) as? [String: Any])
    #expect(object["queue_ids"] as? [String] == ["song", "song"])
    #expect(object["queue_index"] as? Int == 1)
    #expect(object["position_seconds"] as? Double == 0.5)
    #expect(object["repeat_mode"] as? String == "track")
}
