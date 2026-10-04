import Foundation
import Observation
import YTMDLCore

@MainActor @Observable final class AppModel {
    var user: User?
    var client: APIClient?
    var error: String?
    var busy = false
    var connecting = false
    var releases: [Release] = []
    var artists: [Artist] = []
    var tracks: [Track] = []
    var playlists: [Playlist] = []
    var playlistBusy = false
    var favoriteIDs: Set<String> = []
    var genres: [String] = []
    var query = ""
    var genre = ""
    var releaseOffset = 0
    var moreReleases = false
    var device: DeviceStart?
    var player = PlayerModel()
    var listeningHistory = ListeningHistory()
    private var persistsSession = true
    init() {
        #if os(macOS)
        player.onTrackPlayed = { [weak self] track in self?.listeningHistory.record(track) }
        #endif
    }
    private var generation = UUID()
    private var catalogGeneration = UUID()
    private var playlistRevision = UUID()

    func connect(_ text: String, localHTTP: Bool, persist: Bool = true) throws {
        var allowHTTP = false
        #if DEBUG
        allowHTTP = localHTTP
        #endif
        let address = try ServerAddress(text, allowLocalHTTP: allowHTTP)
        let replacement = try APIClient(server: address, persist: persist)
        client?.invalidate(); client = replacement; player.stop(); persistsSession = persist
        listeningHistory.configure(server: nil, userID: nil, persist: false)
        generation = UUID(); catalogGeneration = UUID(); clearLibrary(); user = nil; connecting = false; busy = false
        if persist { UserDefaults.standard.set(text, forKey: "serverAddress") }
    }
    @discardableResult func restore() async -> Bool {
        guard let client else { return false }
        let generation = generation
        do {
            let status: AuthStatus = try await client.get("/auth/status")
            guard self.generation == generation else { return false }
            user = status.user
            listeningHistory.configure(server: client.server, userID: user?.id, persist: persistsSession)
            if status.authenticated { await loadLibrary() }
            return self.generation == generation
        } catch {
            if self.generation == generation { report(error) }
            return false
        }
    }
    func login(username: String, password: String) async {
        guard let client else { return }
        let generation = generation
        busy = true; error = nil; defer { if self.generation == generation { busy = false } }
        do {
            let result = try await client.login(username: username, password: password)
            guard self.generation == generation else { return }
            user = result
            listeningHistory.configure(server: client.server, userID: result.id, persist: persistsSession)
            await loadLibrary()
        } catch { if self.generation == generation { report(error) } }
    }
    func loadLibrary() async {
        guard let client, !connecting else { return }
        let generation = generation
        let catalogGeneration = UUID(); self.catalogGeneration = catalogGeneration
        let playlistRevision = playlistRevision
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        do {
            let releases: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc"), .init(name: "genre", value: genre)])
            let artists: [Artist] = try await client.get("/library/artists", query: [.init(name: "limit", value: "60")])
            let playlists: [Playlist] = try await client.get("/playlists")
            let favorites: [String] = try await client.get("/favorites/ids")
            let genres: [String] = try await client.get("/library/genres")
            guard self.generation == generation, self.catalogGeneration == catalogGeneration else { return }
            self.releases = releases; self.artists = artists
            if self.playlistRevision == playlistRevision { self.playlists = playlists }
            favoriteIDs = Set(favorites); releaseOffset = releases.count; moreReleases = releases.count == 60
            self.genres = genres
        } catch { if self.generation == generation, self.catalogGeneration == catalogGeneration { report(error) } }
    }
    func loadMore() async {
        guard let client, !connecting, moreReleases else { return }
        let generation = generation
        let catalogGeneration = catalogGeneration
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        do {
            let next: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "offset", value: String(releaseOffset)), .init(name: "genre", value: genre), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc")])
            guard self.generation == generation, self.catalogGeneration == catalogGeneration else { return }
            let existing = Set(releases.map(\.id)); releases += next.filter { !existing.contains($0.id) }
            releaseOffset += next.count; moreReleases = next.count == 60
        } catch { if self.generation == generation, self.catalogGeneration == catalogGeneration { report(error) } }
    }
    func filterGenre(_ value: String) async {
        guard let client else { return }
        let generation = generation
        let catalogGeneration = UUID(); self.catalogGeneration = catalogGeneration
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        do {
            let result: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "genre", value: value), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc")])
            guard self.generation == generation, self.catalogGeneration == catalogGeneration, genre == value else { return }
            releases = result; releaseOffset = result.count; moreReleases = result.count == 60
        } catch { if self.generation == generation, self.catalogGeneration == catalogGeneration { report(error) } }
    }
    func search(_ value: String) async throws -> SearchResults? {
        guard let client, value.trimmingCharacters(in: .whitespacesAndNewlines).count >= 2 else { return nil }
        return try await client.get("/library/search", query: [.init(name: "q", value: value), .init(name: "limit", value: "25")])
    }
    func toggleFavorite(_ track: Track) async {
        guard let client else { return }
        let wasFavorite = favoriteIDs.contains(track.id)
        let generation = generation
        do {
            try await client.mutate("/favorites/\(track.id)", method: wasFavorite ? "DELETE" : "PUT")
            guard self.generation == generation else { return }
            if wasFavorite { favoriteIDs.remove(track.id) } else { favoriteIDs.insert(track.id) }
        } catch { report(error) }
    }
    private struct PlaylistInput: Encodable { let name: String; let description: String; let smartRules: SmartPlaylistRules? }
    private struct TrackIDs: Encodable { let trackIds: [String] }
    private struct RulesInput: Encodable {
        let smartRules: SmartPlaylistRules?
        enum CodingKeys: String, CodingKey { case smartRules }
        func encode(to encoder: any Encoder) throws {
            var c = encoder.container(keyedBy: CodingKeys.self)
            if let smartRules { try c.encode(smartRules, forKey: .smartRules) }
            else { try c.encodeNil(forKey: .smartRules) }
        }
    }
    func rememberPlaylist(_ playlist: Playlist) {
        playlistRevision = UUID()
        if let index = playlists.firstIndex(where: { $0.id == playlist.id }) { playlists[index] = playlist }
        else { playlists.insert(playlist, at: 0) }
    }
    func createPlaylist(name: String, description: String, rules: SmartPlaylistRules?) async throws -> Playlist {
        try await playlistMutation { client in
            let result: Playlist = try await client.sendJSON("/playlists", method: "POST",
                body: PlaylistInput(name: name, description: description, smartRules: rules))
            return result
        }
    }
    // Server metadata and rule changes are separate requests. Preserve an
    // acknowledged metadata update even if the subsequent rule request fails.
    func editPlaylist(_ playlist: Playlist, name: String, description: String, rules: SmartPlaylistRules?) async throws -> Playlist {
        try await playlistMutation { client in
            let result: Playlist = try await client.send(try client.playlistPath(playlist.id), method: "PATCH",
                body: ["name": name, "description": description])
            guard self.client === client else { throw CancellationError() }
            self.rememberPlaylist(Playlist(id: result.id, name: result.name, trackCount: result.trackCount,
                durationMs: result.durationMs, description: result.description, smartRules: playlist.smartRules))
            if rules != playlist.smartRules {
                do { try await client.mutateJSON(try client.playlistPath(playlist.id, suffix: "/rules"), method: "PUT", body: RulesInput(smartRules: rules)) }
                catch is CancellationError { throw CancellationError() }
                catch { throw PlayerError.server(status: 0, code: "PLAYLIST_RULES_NOT_SAVED", message: "Name und Beschreibung sind gespeichert. Die Regeln konnten nicht gespeichert werden. Bitte erneut versuchen.") }
            }
            let detail: PlaylistDetail = try await client.get(try client.playlistPath(playlist.id))
            return detail.playlist(fallback: result)
        }
    }
    private func playlistMutation(_ action: (APIClient) async throws -> Playlist) async throws -> Playlist {
        guard let client, !playlistBusy else { throw CancellationError() }
        playlistBusy = true
        defer { if self.client === client { playlistBusy = false } }
        let result = try await action(client)
        try Task.checkCancellation()
        guard self.client === client else { throw CancellationError() }
        rememberPlaylist(result)
        return result
    }
    func deletePlaylist(_ playlist: Playlist) async throws {
        guard let client, !playlistBusy else { throw CancellationError() }
        playlistBusy = true; defer { if self.client === client { playlistBusy = false } }
        try await client.mutate(try client.playlistPath(playlist.id), method: "DELETE")
        guard self.client === client else { throw CancellationError() }
        playlistRevision = UUID(); playlists.removeAll { $0.id == playlist.id }
    }
    func changePlaylistTracks(_ playlist: Playlist, ids: [String], removing: String? = nil, reorder: Bool = false) async throws -> PlaylistDetail {
        guard let client, !playlistBusy else { throw CancellationError() }
        playlistBusy = true; defer { if self.client === client { playlistBusy = false } }
        let detail: PlaylistDetail
        if let removing {
            _ = try client.server.itemPath("tracks", id: removing)
            detail = try await client.sendJSON(try client.playlistPath(playlist.id, suffix: "/tracks/" + removing), method: "DELETE", body: TrackIDs(trackIds: []))
        } else if reorder {
            detail = try await client.sendJSON(try client.playlistPath(playlist.id, suffix: "/tracks/reorder"), method: "PUT", body: TrackIDs(trackIds: ids))
        } else {
            guard !ids.isEmpty, ids.count <= 500 else { throw PlayerError.server(status: 0, code: "PLAYLIST_SELECTION_LIMIT", message: "Bitte 1–500 verschiedene Titel auswählen.") }
            var confirmed = 0
            var latest: PlaylistDetail?
            do {
                for start in stride(from: 0, to: ids.count, by: 100) {
                    latest = try await client.sendJSON(try client.playlistPath(playlist.id, suffix: "/tracks/bulk"), method: "POST",
                        body: TrackIDs(trackIds: Array(ids[start..<min(start + 100, ids.count)])))
                    guard self.client === client else { throw CancellationError() }
                    if let latest { rememberPlaylist(latest.playlist(fallback: playlist)) }
                    confirmed += min(100, ids.count - start)
                }
            } catch is CancellationError { throw CancellationError() }
            catch {
                if confirmed > 0 { throw PlayerError.server(status: 0, code: "PLAYLIST_PARTIALLY_UPDATED", message: "Für \(confirmed) ausgewählte Titel wurde das Hinzufügen bestätigt. Weitere Änderungen sind nicht bestätigt. Playlist neu laden und fehlende Titel erneut hinzufügen.") }
                throw error
            }
            guard let latest else { throw PlayerError.badResponse }; detail = latest
        }
        try Task.checkCancellation()
        guard self.client === client else { throw CancellationError() }
        rememberPlaylist(detail.playlist(fallback: playlist))
        return detail
    }
    func logout() async {
        guard let client else { return }
        do { try await client.mutate("/auth/logout", method: "POST") }
        catch { report(error) }
        do { try client.forget() } catch { report(error) }
        client.invalidate(); self.client = nil; user = nil; generation = UUID(); clearLibrary(); player.stop()
    }
    private func clearLibrary() { playlistBusy = false; playlistRevision = UUID(); listeningHistory.configure(server: nil, userID: nil, persist: false); query = ""; releases = []; artists = []; tracks = []; playlists = []; favoriteIDs = []; device = nil }
    func report(_ failure: Error) {
        if failure is CancellationError { return }
        if let transport = failure as? URLError, transport.code == .cancelled { return }
        if failure is PlayerError || failure is VaultError { error = failure.localizedDescription }
        else { error = "Der Server ist nicht erreichbar oder die Antwort passt nicht zur App. Bitte Verbindung und Server-Version prüfen." }
    }
    #if DEBUG
    // UI fixtures are accepted only on literal loopback. This cannot silently
    // authenticate against a real remote server or persist fixture credentials.
    func loadFixtureIfRequested() async {
        let args = ProcessInfo.processInfo.arguments
        guard let index = args.firstIndex(of: "--fixture-server"), args.indices.contains(index+1),
              let url = URL(string: args[index+1]), ["127.0.0.1", "localhost"].contains(url.host ?? "") else { return }
        do {
            try connect(args[index+1], localHTTP: true, persist: false)
            await restore()
            if args.contains("--fixture-player"), let client, !releases.isEmpty {
                let list: [Track] = try await client.get("/library/tracks")
                player.play(list, client: client)
            }
        } catch { report(error) }
    }
    #endif
}
