import Foundation
import Testing
import YTMDLCore
@testable import YTMDLAppleSupport

private func song(_ id: String) -> Track { Track(id: id, title: id.uppercased(), artists: [], album: "", durationMs: 200_000) }

@MainActor @Test func savedQueueKeepsSectionsAndContextAcrossARestartForTheSameAccount() throws {
    let suite = "queue-restore-" + UUID().uuidString
    let preferences = try #require(UserDefaults(suiteName: suite))
    defer { preferences.removePersistentDomain(forName: suite) }
    let server = try ServerAddress("https://fixture.example")
    let context = QueueContext(kind: "mix", id: "genre-1", title: "Dein Mix 1")
    var queue = PlaybackQueue()
    queue.replace(["a", "b", "c"].map(song), start: 0, context: context)
    queue.append(song("u"))
    _ = queue.appendAutoplay([song("x"), song("y")])
    queue.select(1)
    let history = ListeningHistory(preferences: preferences)
    history.configure(server: server, userID: "first", persist: true)
    history.saveSnapshot(queue: queue, position: 12, repeatMode: "off", force: true)

    // A restart reads the queue back for the same account and server only.
    let restarted = ListeningHistory(preferences: preferences)
    restarted.configure(server: server, userID: "second", persist: true)
    #expect(restarted.snapshot == nil)
    restarted.configure(server: server, userID: "first", persist: true)
    let snapshot = try #require(restarted.snapshot)
    #expect(snapshot.context == context)
    #expect(snapshot.origins == [.context, .user, .context, .context, .autoplay, .autoplay])
    let restored = try #require(snapshot.restorable { _ in true })
    #expect(restored.start == 1)
    var again = PlaybackQueue()
    again.replace(restored.tracks, start: restored.start, context: snapshot.context, origins: restored.origins)
    #expect(again == queue)
    #expect(again.rows().filter(\.startsSection).map { $0.section.heading(again.context) }
            == ["Zuvor gespielt", "Aktueller Titel", "Weiter aus: Dein Mix 1", "Ähnliche Titel"])
    #expect(again.autoplaySeed?.id == "c", "Autoplay continues from the last title the user chose.")

    // Offline only local files come back; each keeps its own origin, and the
    // queue starts at the next title that is still there.
    let local = try #require(snapshot.restorable { !["u", "a"].contains($0.id) })
    #expect(local.tracks.map(\.id) == ["b", "c", "x", "y"])
    #expect(local.origins == [.context, .context, .autoplay, .autoplay] && local.start == 0)
    #expect(snapshot.restorable { _ in false } == nil)
}

@MainActor @Test func queuesSavedBeforeOriginsExistedStillRestoreAsAPlainContext() throws {
    // The format of builds up to 34: no origins and no context.
    let old = Data(#"{"tracks":[{"id":"a","title":"A","artists":[],"album":"","durationMs":1000},{"id":"b","title":"B","artists":null,"durationMs":2000}],"index":1,"position":4.5,"repeatMode":"queue"}"#.utf8)
    let snapshot = try JSONDecoder().decode(ListeningSnapshot.self, from: old)
    #expect(snapshot.origins == nil && snapshot.context == nil && snapshot.position == 4.5)
    let restored = try #require(snapshot.restorable { _ in true })
    #expect(restored.tracks.map(\.id) == ["a", "b"] && restored.origins == [.context, .context] && restored.start == 1)
    var queue = PlaybackQueue()
    queue.replace(restored.tracks, start: restored.start, context: snapshot.context, origins: restored.origins)
    #expect(queue.current?.id == "b" && queue.section(at: 0) == .played)
    // Origins that do not match the tracks are ignored rather than misassigned.
    let broken = ListeningSnapshot(tracks: snapshot.tracks, index: 0, position: 0, repeatMode: "off", origins: [.user])
    #expect(broken.restorable { _ in true }?.origins == [.context, .context])
    // The new format round-trips.
    let current = ListeningSnapshot(tracks: snapshot.tracks, index: 0, position: 1, repeatMode: "off",
                                    origins: [.user, .autoplay], context: QueueContext(kind: "playlist", id: "p", title: "Abends"))
    let decoded = try JSONDecoder().decode(ListeningSnapshot.self, from: JSONEncoder().encode(current))
    #expect(decoded.origins == [.user, .autoplay] && decoded.context?.title == "Abends")
}
