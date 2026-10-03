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
    var favoriteIDs: Set<String> = []
    var genres: [String] = []
    var query = ""
    var genre = ""
    var releaseOffset = 0
    var moreReleases = false
    var device: DeviceStart?
    var player = PlayerModel()
    private var generation = UUID()
    private var catalogGeneration = UUID()

    func connect(_ text: String, localHTTP: Bool, persist: Bool = true) throws {
        var allowHTTP = false
        #if DEBUG
        allowHTTP = localHTTP
        #endif
        let address = try ServerAddress(text, allowLocalHTTP: allowHTTP)
        let replacement = try APIClient(server: address, persist: persist)
        client?.invalidate(); client = replacement; player.stop()
        generation = UUID(); catalogGeneration = UUID(); clearLibrary(); user = nil; connecting = false; busy = false
        if persist { UserDefaults.standard.set(text, forKey: "serverAddress") }
    }
    func restore() async {
        guard let client else { return }
        let generation = generation
        do {
            let status: AuthStatus = try await client.get("/auth/status")
            guard self.generation == generation else { return }
            user = status.user
            if status.authenticated { await loadLibrary() }
        } catch { if self.generation == generation { report(error) } }
    }
    func login(username: String, password: String) async {
        guard let client else { return }
        let generation = generation
        busy = true; error = nil; defer { if self.generation == generation { busy = false } }
        do {
            let result = try await client.login(username: username, password: password)
            guard self.generation == generation else { return }
            user = result; await loadLibrary()
        } catch { if self.generation == generation { report(error) } }
    }
    func loadLibrary() async {
        guard let client, !connecting else { return }
        let generation = generation
        let catalogGeneration = UUID(); self.catalogGeneration = catalogGeneration
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        do {
            let releases: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc"), .init(name: "genre", value: genre)])
            let artists: [Artist] = try await client.get("/library/artists", query: [.init(name: "limit", value: "60")])
            let playlists: [Playlist] = try await client.get("/playlists")
            let favorites: [String] = try await client.get("/favorites/ids")
            let genres: [String] = try await client.get("/library/genres")
            guard self.generation == generation, self.catalogGeneration == catalogGeneration else { return }
            self.releases = releases; self.artists = artists; self.playlists = playlists
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
    func logout() async {
        guard let client else { return }
        do { try await client.mutate("/auth/logout", method: "POST") }
        catch { report(error) }
        do { try client.forget() } catch { report(error) }
        client.invalidate(); self.client = nil; user = nil; generation = UUID(); clearLibrary(); player.stop()
    }
    private func clearLibrary() { releases = []; artists = []; tracks = []; playlists = []; favoriteIDs = []; device = nil }
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
