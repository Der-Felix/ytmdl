import Foundation
import CryptoKit
import Observation
import YTMDLCore

@MainActor @Observable final class ListeningHistory {
    private(set) var tracks: [Track] = []
    private(set) var enabled: Bool
    @ObservationIgnored private let preferences: UserDefaults
    @ObservationIgnored private var key: String?
    @ObservationIgnored private var persistent = false
    init(preferences: UserDefaults = .standard) {
        self.preferences = preferences
        enabled = preferences.object(forKey: "rememberListening") as? Bool ?? true
    }
    func configure(server: ServerAddress?, userID: String?, persist: Bool) {
        guard let server, let userID, !userID.isEmpty else { key = nil; persistent = false; tracks = []; return }
        var origin = URLComponents(url: server.url, resolvingAgainstBaseURL: false)!
        origin.path = ""; origin.host = origin.host?.lowercased()
        // Scope metadata to both the server origin and authenticated account.
        let scope = (origin.string ?? "") + "\n" + userID
        let digest = SHA256.hash(data: Data(scope.utf8)).map { String(format: "%02x", $0) }.joined()
        let nextKey = "listeningHistory." + digest
        guard key != nextKey || persistent != persist else { return }
        key = nextKey; persistent = persist; tracks = []
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
    }
    func clear() { tracks = []; if persistent, let key { preferences.removeObject(forKey: key) } }
    private func save() {
        guard persistent, let key, let data = try? JSONEncoder().encode(tracks) else { return }
        preferences.set(data, forKey: key)
    }
}
