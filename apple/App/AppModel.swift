import Foundation
import Observation
import YTMDLCore

/// "Zur Warteschlange hinzugefügt" and similar: what just happened, in one line.
struct AppNotice: Identifiable, Equatable, Sendable {
    /// How long a confirmation stays on screen.
    static let seconds = 2.4
    let id = UUID()
    let text: String
    let symbol: String
}

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
    private(set) var playlistPreviews: [String: [Track]] = [:]
    @ObservationIgnored private var previewRequests: Set<String> = []
    @ObservationIgnored private var previewRevision: UUID?
    var playlistBusy = false
    var favoriteIDs: Set<String> = []
    private(set) var pendingFavorites: Set<String> = []
    var genres: [String] = []
    var query = ""
    var genre = ""
    var releaseOffset = 0
    var moreReleases = false
    /// Order of the album list, remembered on this device (iPhone and iPad library).
    private(set) var albumSort = LibraryPreferences.sort()
    /// Every artist A–Z for the iPhone and iPad artist list, loaded page by page.
    let artistDirectory = ArtistDirectory()
    var device: DeviceStart?
    var player = PlayerModel()
    var listeningHistory = ListeningHistory()
    /// Personal mixes, server listening history and new releases for the start screen.
    var feed = ListenFeed()
    var offline: OfflineLibrary
    var offlineMode = false
    var handoff: PlaybackHandoff?
    var listeningBusy = false
    /// A short confirmation the iPhone and iPad show for a moment.
    var notice: AppNotice?
    /// Grows with every favorite the server confirmed: a light tap on iPhone.
    private(set) var favoriteConfirmations = 0
    @ObservationIgnored private var noticeTask: Task<Void, Never>?
    /// Shows `text` briefly; a newer confirmation replaces the shown one.
    func announce(_ text: String, symbol: String) {
        let shown = AppNotice(text: text, symbol: symbol)
        notice = shown
        noticeTask?.cancel()
        noticeTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(AppNotice.seconds))
            guard !Task.isCancelled, let self, self.notice?.id == shown.id else { return }
            self.notice = nil
        }
    }
    var syncHistory = UserDefaults.standard.object(forKey: "syncListeningHistory") as? Bool ?? true
    private var persistsSession = true
    init(offlineLibrary: OfflineLibrary? = nil) {
        offline = offlineLibrary ?? OfflineLibrary.shared
        player.offlineLibrary = offline
        player.onTrackPlayed = { [weak self] track in
            guard let self else { return }
            listeningHistory.record(track)
            if !offlineMode && offline.cachePlayed { offline.enqueue([track], automatic: true) }
        }
        // Plays and skips use the web player's rules. Events from local files
        // wait in the account's queue until this account is online again.
        player.onListeningEvent = { [weak self] track, kind in
            guard let self, syncHistory else { return }
            listeningHistory.enqueueEvent(track, kind: kind)
            Task { await flushListeningHistory() }
        }
        player.onAutoplayNeeded = { [weak self] seed, count, last in
            Task { await self?.extendAutoplay(seed: seed, expectedCount: count, lastID: last) }
        }
        player.onSnapshot = { [weak self] queue, position, mode, force in
            self?.listeningHistory.saveSnapshot(queue: queue, position: position, repeatMode: mode, force: force)
            self?.offline.protectedTrackIDs = Set([queue.current?.id, self?.player.preparedTrackID].compactMap { $0 })
        }
        player.onQueueEnded = { [weak self] track in Task { await self?.continueRadio(after: track) } }
    }
    /// Binds the player's listening session (what autoplay leaves out) to this
    /// account on this server; a change of either starts a new session.
    private func scopeListeningSession() {
        guard let user, let client else { player.setSessionScope(nil); return }
        player.setSessionScope(client.server.url.absoluteString.lowercased() + "\n" + user.id)
    }
    private var generation = UUID()
    private var catalogGeneration = UUID()
    private var favoriteRevision = UUID()
    private(set) var playlistRevision = UUID()
    private var syncingHistory = false

    func connect(_ text: String, localHTTP: Bool, persist: Bool = true) throws {
        var allowHTTP = false
        #if DEBUG
        allowHTTP = localHTTP
        #endif
        let address = try ServerAddress(text, allowLocalHTTP: allowHTTP)
        let replacement = try APIClient(server: address, persist: persist)
        offline.detach(); offlineMode = false; player.offlineOnly = false
        client?.invalidate(); client = replacement; player.stop(); persistsSession = persist
        listeningHistory.configure(server: nil, userID: nil, persist: false)
        generation = UUID(); catalogGeneration = UUID(); clearLibrary(); user = nil; connecting = false; busy = false
        scopeListeningSession()
        if persist { UserDefaults.standard.set(text, forKey: "serverAddress") }
    }
    @discardableResult func restore() async -> Bool {
        guard let client else { return false }
        let generation = generation
        do {
            let status: AuthStatus = try await client.get("/auth/status")
            guard self.generation == generation else { return false }
            user = status.authenticated ? status.user : nil
            scopeListeningSession()
            if let user { offline.configure(client: client, user: user) }
            else { offline.detach(); player.stop() }
            listeningHistory.configure(server: client.server, userID: user?.id, persist: persistsSession)
            if status.authenticated { await loadLibrary() }
            return self.generation == generation
        } catch {
            if self.generation == generation { report(error) }
            return false
        }
    }
    func login(username: String, password: String) async {
        guard let client, !busy else { return }
        let generation = generation
        busy = true; error = nil; defer { if self.generation == generation { busy = false } }
        do {
            let result = try await client.login(username: username, password: password)
            guard self.generation == generation else { return }
            user = result
            scopeListeningSession()
            offline.configure(client: client, user: result)
            listeningHistory.configure(server: client.server, userID: result.id, persist: persistsSession)
            await loadLibrary()
        } catch { if self.generation == generation { report(error) } }
    }
    /// Polls until the displayed code is approved (then restores the session) or expires.
    /// A timeout, dropped connection or 5xx answer is retried at the next interval; only
    /// a definite server answer (invalid/expired code, unsupported server) ends the wait.
    func completeDeviceSignIn(_ start: DeviceStart, wait: (Int) async throws -> Void = { try await Task.sleep(for: .seconds($0)) }) async throws -> Bool {
        guard let client else { throw CancellationError() }
        let deadline = Date().addingTimeInterval(Double(start.expiresIn))
        var interval = max(5, start.interval)
        while Date() < deadline {
            try await wait(interval); try Task.checkCancellation()
            let state: DevicePoll
            do { state = try await client.send("/auth/device/poll", body: ["device_code": start.deviceCode]) }
            catch let error as PlayerError {
                if case .server(let status, _, _) = error, status >= 500 { continue }
                // A rate limit is an instruction to slow down, not a refusal.
                if case .server(let status, _, _) = error, status == 429 { interval = min(30, interval + 5); continue }
                throw error
            }
            catch is CancellationError { throw CancellationError() }
            catch { continue }
            try Task.checkCancellation()
            if state.status == "authorized" {
                // Approval only counts once the session is actually usable. A
                // failed restore is reported, so the screen does not keep
                // waiting for an approval that has already been given.
                guard await restore() else { throw PlayerError.badResponse }
                return true
            }
            if state.status == "slow_down" { interval = min(30, interval + 5) }
        }
        return false
    }
    /// Why the albums could not be loaded. Nil once the server answered, even
    /// with an empty list, so a failed load never reads as an empty library.
    private(set) var libraryError: String?
    /// The server has answered the album list at least once for this session.
    private(set) var releasesLoaded = false
    /// The running catalog load. The model owns it, not the view that asked:
    /// a restored session replaces the sign-in screen while the load runs, and
    /// the cancelled `.task` of that screen used to cancel the whole load.
    @ObservationIgnored private var libraryTask: Task<Void, Never>?
    @ObservationIgnored private var libraryReloadPending = false

    /// Loads albums, artists, playlists, favorites and genres. A request while a
    /// load runs is not dropped: the running load is followed by one more.
    func loadLibrary() async {
        guard !offlineMode, client != nil else { return }
        if let running = libraryTask {
            libraryReloadPending = true
            await running.value
            return
        }
        let task = Task { [weak self] in
            guard let self else { return }
            repeat {
                libraryReloadPending = false
                await performLibraryLoad()
            } while libraryReloadPending && !offlineMode && client != nil
            libraryTask = nil
        }
        libraryTask = task
        await task.value
    }
    private func fetch<T: Decodable & Sendable>(_ client: APIClient, _ path: String, query: [URLQueryItem] = []) async -> Result<T, any Error> {
        do { return .success(try await client.get(path, query: query)) } catch { return .failure(error) }
    }
    nonisolated private static func isCancellation(_ error: any Error) -> Bool {
        if error is CancellationError { return true }
        if let transport = error as? URLError, transport.code == .cancelled { return true }
        return false
    }
    /// Each part is loaded and applied on its own: one failing or undecodable
    /// answer leaves the other four in place.
    private func performLibraryLoad() async {
        guard !offlineMode, let client else { return }
        let generation = generation
        let catalogGeneration = UUID(); self.catalogGeneration = catalogGeneration
        let playlistRevision = playlistRevision
        let favoriteRevision = favoriteRevision
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        async let releasePage: Result<[Release], any Error> = fetch(client, "/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "genre", value: genre)] + albumSort.queryItems)
        async let artistPage: Result<[Artist], any Error> = fetch(client, "/library/artists", query: [.init(name: "limit", value: "60")])
        async let playlistPage: Result<[Playlist], any Error> = fetch(client, "/playlists")
        async let favoritePage: Result<[String], any Error> = fetch(client, "/favorites/ids")
        async let genrePage: Result<[String], any Error> = fetch(client, "/library/genres")
        let (releaseResult, artistResult, playlistResult, favoriteResult, genreResult) = await (releasePage, artistPage, playlistPage, favoritePage, genrePage)
        guard self.generation == generation, self.client === client else { return }
        var failures: [any Error] = []
        // A genre filter chosen meanwhile owns the album list; only the albums
        // depend on it, the other parts still apply.
        if self.catalogGeneration == catalogGeneration {
            switch releaseResult {
            case .success(let releases):
                self.releases = releases; releaseOffset = releases.count; moreReleases = releases.count == 60
                releasesLoaded = true; libraryError = nil
            case .failure(let error):
                if !Self.isCancellation(error) { libraryError = describe(error); failures.append(error) }
            }
        }
        switch artistResult {
        case .success(let artists): self.artists = artists
        case .failure(let error): failures.append(error)
        }
        switch playlistResult {
        case .success(let playlists):
            if self.playlistRevision == playlistRevision { self.playlists = playlists; self.playlistRevision = UUID() }
        case .failure(let error): failures.append(error)
        }
        switch favoriteResult {
        case .success(let favorites): if self.favoriteRevision == favoriteRevision { favoriteIDs = Set(favorites) }
        case .failure(let error): failures.append(error)
        }
        // Without an answer the genre row stays hidden (or keeps the last list).
        switch genreResult {
        case .success(let genres): self.genres = genres
        case .failure(let error): failures.append(error)
        }
        if self.catalogGeneration == catalogGeneration { connecting = false }
        // One message is enough; a 401 among them signs out.
        if let failure = failures.first(where: { !Self.isCancellation($0) }) { report(failure) }
        guard self.generation == generation, user != nil else { return }
        if offline.syncFavorites { await downloadCollection(.favorites, automatic: false, announceLimit: false) }
        await refreshOfflineCollections()
        await flushListeningHistory()
    }
    func loadMore() async {
        guard let client, !connecting, moreReleases else { return }
        let generation = generation
        let catalogGeneration = catalogGeneration
        connecting = true; defer { if self.catalogGeneration == catalogGeneration { connecting = false } }
        do {
            let next: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "offset", value: String(releaseOffset)), .init(name: "genre", value: genre)] + albumSort.queryItems)
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
            let result: [Release] = try await client.get("/library/releases", query: [.init(name: "limit", value: "60"), .init(name: "genre", value: value)] + albumSort.queryItems)
            guard self.generation == generation, self.catalogGeneration == catalogGeneration, genre == value else { return }
            releases = result; releaseOffset = result.count; moreReleases = result.count == 60
            releasesLoaded = true; libraryError = nil
        } catch {
            guard self.generation == generation, self.catalogGeneration == catalogGeneration else { return }
            if !Self.isCancellation(error) { libraryError = describe(error) }
            report(error)
        }
    }
    /// A new album order: remembered on this device and loaded right away for
    /// the chosen genre. The server sorts, so the order spans the whole library.
    func setAlbumSort(_ sort: LibraryAlbumSort) async {
        guard sort != albumSort else { return }
        albumSort = sort; LibraryPreferences.setSort(sort)
        await filterGenre(genre)
    }
    func search(_ value: String) async throws -> SearchResults? {
        guard let client, value.trimmingCharacters(in: .whitespacesAndNewlines).count >= 2 else { return nil }
        let result: SearchResults = try await client.get("/library/search", query: [.init(name: "q", value: value), .init(name: "limit", value: "25")])
        try Task.checkCancellation()
        guard self.client === client else { throw CancellationError() }
        return result
    }
    func toggleFavorite(_ track: Track) async {
        guard !offlineMode, let client, !pendingFavorites.contains(track.id) else { return }
        let wasFavorite = favoriteIDs.contains(track.id)
        let generation = generation
        pendingFavorites.insert(track.id)
        defer { if self.generation == generation { pendingFavorites.remove(track.id) } }
        do {
            try await client.mutate("/favorites/\(track.id)", method: wasFavorite ? "DELETE" : "PUT")
            guard self.generation == generation else { return }
            if wasFavorite { favoriteIDs.remove(track.id) } else { favoriteIDs.insert(track.id) }
            favoriteRevision = UUID()
            favoriteConfirmations &+= 1
        } catch { if self.generation == generation { report(error) } }
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
        var failure: Error?
        do { if !offlineMode { try await client.mutate("/auth/logout", method: "POST") } }
        catch { failure = error }
        guard self.client === client else { return }
        offline.detach()
        do { if !offlineMode { try client.forget() } } catch { failure = error }
        offlineMode = false; player.offlineOnly = false
        client.invalidate(); self.client = nil; user = nil; generation = UUID(); clearLibrary(); player.stop()
        scopeListeningSession()
        if let failure { report(failure) }
    }
    private func clearLibrary() {
        playlistPreviews = [:]; previewRequests = []; previewRevision = nil
        playlistBusy = false; playlistRevision = UUID(); catalogGeneration = UUID(); favoriteRevision = UUID()
        connecting = false; listeningBusy = false; handoff = nil; error = nil; pendingFavorites = []
        genres = []; genre = ""; releaseOffset = 0; moreReleases = false; libraryError = nil; releasesLoaded = false
        artistDirectory.reset()
        listeningHistory.configure(server: nil, userID: nil, persist: false)
        feed.clear()
        query = ""; releases = []; artists = []; tracks = []; playlists = []; favoriteIDs = []; device = nil
    }
    static let previewLimit = 48
    // Cover previews are shared by Start and Playlists. Fetch at most `previewLimit`
    // collections per revision (rows ask as they appear) and retain only four distinct covers.
    func loadPlaylistPreviews(_ candidates: [Playlist]) async {
        guard !offlineMode, let client else { return }
        let revision = playlistRevision
        if previewRevision != revision {
            playlistPreviews = [:]; previewRequests = []; previewRevision = revision
        }
        for playlist in candidates.prefix(Self.previewLimit) {
            guard !Task.isCancelled, self.client === client, playlistRevision == revision else { return }
            guard playlist.trackCount > 0, playlistPreviews[playlist.id] == nil,
                  !previewRequests.contains(playlist.id), playlistPreviews.count + previewRequests.count < Self.previewLimit else { continue }
            previewRequests.insert(playlist.id)
            defer { if self.client === client, playlistRevision == revision { previewRequests.remove(playlist.id) } }
            do {
                let detail: PlaylistDetail = try await client.get(try client.playlistPath(playlist.id))
                guard !Task.isCancelled, self.client === client, playlistRevision == revision else { return }
                var seen = Set<String>()
                var preview: [Track] = []
                for track in detail.tracks {
                    if seen.insert(track.album.isEmpty ? track.id : track.artistText + "\n" + track.album).inserted {
                        preview.append(track)
                        if preview.count == 4 { break }
                    }
                }
                playlistPreviews[playlist.id] = preview
            } catch {
                guard !Task.isCancelled, self.client === client, playlistRevision == revision else { return }
                playlistPreviews[playlist.id] = []
            }
        }
    }
    func openOffline(_ profile: OfflineProfile) {
        do {
            let address = try ServerAddress(profile.origin, allowLocalHTTP: true)
            let local = try APIClient(server: address, persist: false)
            player.stop(); client?.invalidate(); offline.detach(); clearLibrary()
            client = local; user = profile.user; offlineMode = true; player.offlineOnly = true
            scopeListeningSession()
            generation = UUID(); offline.selectProfile(profile)
            listeningHistory.configure(server: address, userID: profile.user.id, persist: true)
        } catch { report(error) }
    }
    // `announceLimit` is false for the silent favorites sync on every library load.
    func downloadCollection(_ kind: CollectionKind, automatic: Bool = false, announceLimit: Bool = true) async {
        guard !offlineMode, let client else { return }
        do {
            var collected: [Track] = []
            if case .playlist(let playlist) = kind {
                let result: PlaylistDetail = try await client.get(try client.playlistPath(playlist.id))
                collected = result.tracks
            } else {
                // Read one page past 500 so an exactly-500 collection is not reported as cut.
                while collected.count <= 500 {
                    var query: [URLQueryItem] = [.init(name: "limit", value: "100"), .init(name: "offset", value: String(collected.count))]
                    switch kind {
                    case .favorites: query.append(.init(name: "favorite", value: "true"))
                    case .release(let release): query += [.init(name: "release_id", value: release.id), .init(name: "sort", value: "track_number"), .init(name: "order", value: "asc")]
                    case .artist(let artist): query.append(.init(name: "artist_id", value: artist.id))
                    case .playlist: break
                    }
                    let page: [Track] = try await client.get("/library/tracks", query: query)
                    try Task.checkCancellation(); guard self.client === client else { return }
                    collected += page
                    if page.count < 100 { break }
                }
            }
            guard self.client === client, !offlineMode else { return }
            let truncated = collected.count > 500
            collected = Array(collected.prefix(500))
            offline.rememberCollection(kind, tracks: collected)
            offline.enqueue(collected, automatic: automatic)
            if truncated && announceLimit { error = "Die ersten 500 Titel wurden für offline vorgemerkt. Größere Sammlungen bitte in mehrere Playlists aufteilen." }
        } catch { if self.client === client { report(error) } }
    }
    func refreshOfflineCollections() async {
        guard !offlineMode, let client else { return }
        for collection in offline.currentCollections where collection.keepUpdated {
            do {
                let tracks: [Track]
                if collection.kind == "playlist" {
                    let result: PlaylistDetail = try await client.get(try client.playlistPath(collection.sourceID)); tracks = result.tracks
                } else {
                    var result: [Track] = []
                    while result.count < 500 {
                        var query: [URLQueryItem] = [.init(name: "limit", value: "100"), .init(name: "offset", value: String(result.count))]
                        switch collection.kind {
                        case "favorites": query.append(.init(name: "favorite", value: "true"))
                        case "release": query += [.init(name: "release_id", value: collection.sourceID), .init(name: "sort", value: "track_number"), .init(name: "order", value: "asc")]
                        case "artist": query.append(.init(name: "artist_id", value: collection.sourceID))
                        default: throw PlayerError.invalidID
                        }
                        let page: [Track] = try await client.get("/library/tracks", query: query)
                        try Task.checkCancellation(); guard self.client === client else { return }
                        result += page; if page.count < 100 { break }
                    }
                    tracks = result
                }
                guard self.client === client else { return }
                offline.updateCollection(collection.id, tracks: tracks)
            } catch { if self.client === client { offline.error = "Die Offline-Sammlung „\(collection.name)“ konnte nicht aktualisiert werden. Gespeicherte Titel bleiben erhalten." } }
        }
    }
    func playMix(_ name: String, genre: String = "") async {
        guard !listeningBusy else { return }
        if offlineMode, let client { player.play(offline.readyTracks.shuffled(), client: client); return }
        guard let client else { return }
        let playbackRevision = player.playbackRevision
        listeningBusy = true; defer { if self.client === client { listeningBusy = false } }
        do {
            var query: [URLQueryItem] = [.init(name: "limit", value: "100")]
            if name == "Favoriten" { query.append(.init(name: "favorite", value: "true")) }
            if !genre.isEmpty { query.append(.init(name: "genre", value: genre)) }
            query.append(.init(name: "sort", value: name == "Neu" ? "recent" : "title"))
            let result: [Track]
            if !genre.isEmpty { result = try await client.get("/library/radio", query: [.init(name: "genre", value: genre), .init(name: "nonce", value: UUID().uuidString)]) }
            else { result = try await client.get("/library/tracks", query: query) }
            guard self.client === client, player.playbackRevision == playbackRevision else { return }
            let context = !genre.isEmpty ? QueueContext(kind: "radio", id: genre, title: "Radio: \(genre)")
                : name == "Favoriten" ? QueueContext(kind: "favorites", title: "Lieblingstitel") : QueueContext(kind: "library", title: name)
            player.play(name == "Neu" ? result : result.shuffled(), client: client, context: context)
        } catch { if self.client === client { report(error) } }
    }
    func startRadio(_ track: Track) async {
        guard !offlineMode, let client, !listeningBusy else { return }
        let playbackRevision = player.playbackRevision
        listeningBusy = true; defer { if self.client === client { listeningBusy = false } }
        do {
            let result: [Track] = try await client.get("/library/radio", query: [.init(name: "seed", value: track.id), .init(name: "nonce", value: UUID().uuidString)])
            guard self.client === client, player.playbackRevision == playbackRevision else { return }
            player.play([track] + result.filter { $0.id != track.id }, client: client,
                        context: QueueContext(kind: "radio", id: track.id, title: "Radio: \(track.title)"))
        } catch { if self.client === client { report(error) } }
    }
    func continueRadio(after track: Track) async {
        guard player.autoplay, !player.isPlaybackRequested, player.current?.id == track.id, let client else { return }
        if offlineMode {
            let candidates = offline.readyTracks.filter { $0.id != track.id }.shuffled()
            if !candidates.isEmpty { player.play(candidates, client: client) }; return
        }
        let playbackRevision = player.playbackRevision
        do {
            let result: [Track] = try await client.get("/library/radio", query: [.init(name: "seed", value: track.id), .init(name: "nonce", value: UUID().uuidString)])
            guard self.client === client, player.playbackRevision == playbackRevision, player.autoplay, !player.isPlaybackRequested, player.current?.id == track.id else { return }
            // Like an autoplay top-up: one small batch without anything played
            // in this session; the lookahead asks for the next batch later.
            let played = Set(player.queue.tracks.map(\.id)).union(player.sessionPlayedIDs)
            var seen = Set<String>()
            let candidates = Array(result.filter { !played.contains($0.id) && seen.insert($0.id).inserted }.prefix(PlaybackQueue.autoplayBatch))
            if !candidates.isEmpty { player.play(candidates, client: client, context: QueueContext(kind: "radio", id: track.id, title: "Radio: \(track.title)")) }
        } catch { /* Autoplay must not turn normal queue completion into an alert. */ }
    }
    /// Restores the saved queue with its sections ("Als Nächstes", "Weiter aus: …",
    /// "Ähnliche Titel"). Offline only the titles with a local file come back.
    func resumeLastSession() {
        guard let snapshot = listeningHistory.snapshot, let client else { return }
        let localOnly = offlineMode, library = offline
        guard let restored = snapshot.restorable(keeping: { !localOnly || library.audioURL($0.id) != nil }) else { return }
        let selected = snapshot.tracks.indices.contains(snapshot.index) ? snapshot.tracks[snapshot.index].id : ""
        player.repeatAll = snapshot.repeatMode == "queue"; player.setRepeatOne(snapshot.repeatMode == "track")
        player.play(restored.tracks, start: restored.start, client: client, context: snapshot.context, origins: restored.origins)
        if player.current?.id == selected { player.seek(snapshot.position) }
    }
    func setSyncHistory(_ value: Bool) {
        syncHistory = value; UserDefaults.standard.set(value, forKey: "syncListeningHistory")
        if !value { listeningHistory.discardPendingEvents() }
    }
    func flushListeningHistory() async {
        guard syncHistory, !offlineMode, persistsSession, !syncingHistory, let client else { return }
        let generation = generation
        syncingHistory = true; defer { syncingHistory = false }
        for event in listeningHistory.pendingEvents {
            guard syncHistory, listeningHistory.enabled, self.generation == generation, self.client === client else { return }
            do {
                try await client.mutate(event.resolvedKind.path, method: "POST", body: ["event_id": event.eventID, "track_id": event.trackID])
                guard self.generation == generation, self.client === client else { return }
                listeningHistory.acknowledgeEvent(event.eventID)
            } catch {
                // A definite refusal of this one event (a server without skips
                // answers 404) must not hold back every later event.
                if Self.dropsListeningEvent(after: error), self.generation == generation, self.client === client {
                    listeningHistory.acknowledgeEvent(event.eventID); continue
                }
                return // Retain idempotent events until this same account reconnects.
            }
        }
    }
    /// Client errors other than an expired session, a CSRF problem, a timeout
    /// or a rate limit are permanent for that event.
    nonisolated static func dropsListeningEvent(after error: Error) -> Bool {
        guard case PlayerError.server(let status, _, _) = error else { return false }
        return (400..<500).contains(status) && ![401, 403, 408, 429].contains(status)
    }
    /// Autoplay lookahead: similar tracks for the last track the user chose.
    func extendAutoplay(seed: Track, expectedCount: Int, lastID: String) async {
        guard !offlineMode, let client else { return }
        do {
            let result: [Track] = try await client.get("/library/radio", query: [.init(name: "seed", value: seed.id), .init(name: "nonce", value: UUID().uuidString)])
            guard self.client === client else { return }
            player.appendAutoplay(result, expectedCount: expectedCount, lastID: lastID)
        } catch { /* Autoplay is a suggestion; the queue ends normally without it. */ }
    }
    func saveHandoff() async {
        guard !offlineMode, let client, player.current != nil else { return }
        let playbackRevision = player.playbackRevision
        do {
            let payload = HandoffPayload(queue: player.queue, position: player.position,
                repeatMode: player.repeatOne ? "track" : player.repeatAll ? "queue" : "off", sourceName: "YTMDL Apple")
            let _: PlaybackHandoff = try await client.sendJSON("/playback/handoff", method: "POST", body: payload)
            guard self.client === client, player.playbackRevision == playbackRevision else { return }
            player.pause()
        } catch { if self.client === client { report(error) } }
    }
    func checkHandoff() async {
        guard !offlineMode, let client else { return }
        do {
            let result: PlaybackHandoff? = try await client.get("/playback/handoff")
            if self.client === client { handoff = result }
        } catch { if self.client === client { report(error) } }
    }
    func acceptHandoff() {
        guard !offlineMode, let handoff, let client, let tracks = handoff.queue, tracks.indices.contains(handoff.queueIndex) else { return }
        player.repeatAll = handoff.repeatMode == "queue"; player.setRepeatOne(handoff.repeatMode == "track")
        player.play(tracks, start: handoff.queueIndex, client: client)
        player.seek(handoff.positionSeconds); self.handoff = nil
    }
    func report(_ failure: Error) {
        if failure is CancellationError { return }
        if let transport = failure as? URLError, transport.code == .cancelled { return }
        // An expired or revoked session cannot recover by itself: leave the library
        // UI and drop the dead cookies, like a local sign-out without a server call.
        if case PlayerError.server(let status, _, _) = failure, status == 401, !offlineMode, user != nil, let client {
            offline.detach()
            try? client.forget()
            client.invalidate(); self.client = nil; user = nil; generation = UUID(); clearLibrary(); player.stop()
            scopeListeningSession()
        }
        error = describe(failure)
    }
    func describe(_ failure: any Error) -> String {
        if failure is PlayerError || failure is VaultError { return failure.localizedDescription }
        if failure is DecodingError { return "Die Antwort des Servers passt nicht zur App. Bitte Server-Version prüfen." }
        return "Der Server ist nicht erreichbar oder die Antwort passt nicht zur App. Bitte Verbindung und Server-Version prüfen."
    }
    #if DEBUG
    // UI fixtures are accepted only on literal loopback. This cannot silently
    // authenticate against a real remote server or persist fixture credentials.
    func loadFixtureIfRequested() async {
        let args = ProcessInfo.processInfo.arguments
        guard let index = args.firstIndex(of: "--fixture-server"), args.indices.contains(index+1),
              let url = URL(string: args[index+1]), ["127.0.0.1", "localhost"].contains(url.host ?? "") else { return }
        do {
            offline = OfflineLibrary(root: FileManager.default.temporaryDirectory.appendingPathComponent("YTMDL-Fixture-" + UUID().uuidString))
            player.offlineLibrary = offline
            try connect(args[index+1], localHTTP: true, persist: false)
            await restore()
            if args.contains("--fixture-player"), let client, !releases.isEmpty {
                let list: [Track] = try await client.get("/library/tracks")
                player.play(list, client: client)
            }
            if args.contains("--fixture-paused-player"), let client {
                let list: [Track] = try await client.get("/library/tracks")
                player.previewPaused(list, client: client)
            }
        } catch { report(error) }
    }
    #endif
}
