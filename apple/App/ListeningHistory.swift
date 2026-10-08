import Foundation
import CryptoKit
import Observation
import YTMDLCore

struct ListeningSnapshot: Codable, Sendable {
    let tracks: [Track]
    let index: Int
    let position: Double
    let repeatMode: String
}
struct ListeningEvent: Codable, Sendable { let eventID: String; let trackID: String }
@MainActor @Observable final class ListeningHistory {
    private(set) var tracks: [Track] = []
    private(set) var enabled: Bool
    private(set) var snapshot: ListeningSnapshot?
    private(set) var pendingEvents: [ListeningEvent] = []
    @ObservationIgnored private var lastSnapshot = Date.distantPast
    @ObservationIgnored private let preferences: UserDefaults
    @ObservationIgnored private var key: String?
    @ObservationIgnored private var persistent = false
    init(preferences: UserDefaults = .standard) {
        self.preferences = preferences
        enabled = preferences.object(forKey: "rememberListening") as? Bool ?? true
    }
    func configure(server: ServerAddress?, userID: String?, persist: Bool) {
        guard let server, let userID, !userID.isEmpty else { key = nil; persistent = false; tracks = []; snapshot = nil; pendingEvents = []; return }
        var origin = URLComponents(url: server.url, resolvingAgainstBaseURL: false)!
        origin.path = ""; origin.host = origin.host?.lowercased()
        // Scope metadata to both the server origin and authenticated account.
        let scope = (origin.string ?? "") + "\n" + userID
        let digest = SHA256.hash(data: Data(scope.utf8)).map { String(format: "%02x", $0) }.joined()
        let nextKey = "listeningHistory." + digest
        guard key != nextKey || persistent != persist else { return }
        key = nextKey; persistent = persist; tracks = []; snapshot = nil; pendingEvents = []
        if persist, enabled, let data = preferences.data(forKey: nextKey + ".queue"), data.count <= 1024 * 1024,
           let saved = try? JSONDecoder().decode(ListeningSnapshot.self, from: data), saved.tracks.count <= 500, saved.tracks.indices.contains(saved.index), saved.position.isFinite { snapshot = saved }
        if persist, let data = preferences.data(forKey: nextKey + ".events"), data.count <= 128 * 1024,
           let saved = try? JSONDecoder().decode([ListeningEvent].self, from: data) { pendingEvents = Array(saved.prefix(500)) }
        if persist, let data = preferences.data(forKey: nextKey), data.count <= 256_000,
           let saved = try? JSONDecoder().decode([Track].self, from: data) {
            var seen = Set<String>()
            tracks = Array(saved.filter { seen.insert($0.id).inserted }.prefix(40))
        }
    }
    func record(_ track: Track) {
        guard enabled, key != nil else { return }
        tracks.removeAll { $0.id == track.id }; tracks.insert(track, at: 0)
        tracks = Array(tracks.prefix(40)); save()
    }
    func setEnabled(_ value: Bool) {
        enabled = value; preferences.set(value, forKey: "rememberListening")
        if !value { snapshot = nil; discardPendingEvents(); if persistent, let key { preferences.removeObject(forKey: key + ".queue") } }
    }
    func clear() {
        tracks = []; snapshot = nil; pendingEvents = []
        if persistent, let key { for suffix in ["", ".queue", ".events"] { preferences.removeObject(forKey: key + suffix) } }
    }
    func saveSnapshot(queue: PlaybackQueue, position: Double, repeatMode: String, force: Bool) {
        guard enabled, let key, position.isFinite else { return }
        guard queue.current != nil else {
            if force { snapshot = nil; if persistent { preferences.removeObject(forKey: key + ".queue") } }
            return
        }
        guard force || snapshot?.tracks != queue.tracks || Date().timeIntervalSince(lastSnapshot) >= 5 else { return }
        snapshot = ListeningSnapshot(tracks: queue.tracks, index: queue.index, position: max(0, position), repeatMode: repeatMode); lastSnapshot = Date()
        if persistent, let data = try? JSONEncoder().encode(snapshot), data.count <= 1024 * 1024 { preferences.set(data, forKey: key + ".queue") }
    }
    func enqueueEvent(_ track: Track) {
        guard enabled, persistent else { return }
        pendingEvents.append(ListeningEvent(eventID: UUID().uuidString, trackID: track.id))
        pendingEvents = Array(pendingEvents.suffix(500)); saveEvents()
    }
    func discardPendingEvents() { pendingEvents = []; saveEvents() }
    func acknowledgeEvent(_ id: String) { pendingEvents.removeAll { $0.eventID == id }; saveEvents() }
    private func saveEvents() {
        if persistent, let key, let data = try? JSONEncoder().encode(pendingEvents) { preferences.set(data, forKey: key + ".events") }
    }
    private func save() {
        guard persistent, let key, let data = try? JSONEncoder().encode(tracks) else { return }
        preferences.set(data, forKey: key)
    }
}
