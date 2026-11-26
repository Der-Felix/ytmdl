import SwiftUI
import YTMDLCore
#if os(iOS)

/// Search on iPhone and iPad: recent searches and genres to browse before
/// typing, results while typing (debounced, stale requests cancelled) grouped
/// as Top-Treffer, Titel, Alben, Künstler and Playlists. Offline browsing has
/// its own local search in `OfflineLibraryView`.
struct MobileSearchView: View {
    @Bindable var model: AppModel
    /// Grows when the Search tab is chosen again: the field takes focus.
    var focusRequest = 0
    /// Shows the library filtered by a genre.
    var openGenre: (String) -> Void = { _ in }
    @State private var query = ""
    @State private var results: SearchResults?
    /// The text `results` answer; the field may already hold more.
    @State private var resultsQuery = ""
    @State private var loading = false
    @State private var failure: String?
    @State private var attempt = 0
    @State private var expanded: Set<ResultGroup> = []
    @State private var recent = RecentSearches()
    @State private var allGenres = false
    @FocusState private var fieldFocused: Bool
    @Environment(\.dynamicTypeSize) private var textSize

    private enum ResultGroup: String, Hashable { case tracks, releases, artists, playlists }
    private struct SearchKey: Equatable { let text: String; let attempt: Int }
    private static let debounce = Duration.milliseconds(250)
    /// Rows shown per group before "Alle anzeigen".
    private static let collapsed: [ResultGroup: Int] = [.tracks: 5, .releases: 4, .artists: 3, .playlists: 3]
    /// The server answers at most this many per group.
    private static let serverLimit = 25

    private var text: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var searching: Bool { text.count >= 2 }
    private var recentKey: String? {
        guard let client = model.client, let user = model.user else { return nil }
        return RecentSearches.storageKey(server: client.server.url.absoluteString, userID: user.id)
    }
    private var genres: [String] { model.genres.filter { $0 != "__none__" && !$0.isEmpty } }
    /// The server's search has no playlists; the loaded list is searched here.
    private var matchingPlaylists: [Playlist] {
        guard searching else { return [] }
        return model.playlists.filter { SearchMatch.score($0.name, query: text) > 0 }
    }

    var body: some View {
        List {
            if searching { resultSections } else { startSections }
        }
        .listStyle(.plain)
        .scrollDismissesKeyboard(.immediately)
        .navigationTitle("Suche")
        // Always visible under the title. The automatic placement hid the
        // field in the bar until the list was pulled down, also in the
        // search tab, which is why search looked broken on the iPhone.
        .searchable(text: $query, placement: .navigationBarDrawer(displayMode: .always), prompt: "Titel, Alben, Künstler")
        .searchFocused($fieldFocused)
        .textInputAutocapitalization(.never)
        .autocorrectionDisabled()
        .onSubmit(of: .search) { remember(text) }
        .onChange(of: focusRequest) { fieldFocused = true }
        // Leaving the results for a pushed page or another tab keeps the search.
        .onDisappear { if searching && results != nil { remember(text) } }
        .task(id: recentKey) { loadRecent() }
        .task(id: SearchKey(text: text, attempt: attempt)) { await runSearch() }
        .task(id: matchingPlaylists.prefix(3).map(\.id)) {
            if !matchingPlaylists.isEmpty { await model.loadPlaylistPreviews(Array(matchingPlaylists.prefix(3))) }
        }
    }

    // MARK: Before typing

    @ViewBuilder private var startSections: some View {
        if recent.entries.isEmpty {
            Section {
                Text("Suche nach Titeln, Alben, Künstlern und Playlists in deiner Bibliothek.")
                    .font(.subheadline).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                    .listRowSeparator(.hidden)
            }
        } else {
            Section {
                ForEach(recent.entries, id: \.self) { entry in recentRow(entry) }
            } header: {
                HStack(alignment: .firstTextBaseline) {
                    Text("Zuletzt gesucht").font(.title3.bold()).foregroundStyle(.primary).accessibilityAddTraits(.isHeader)
                    Spacer(minLength: 8)
                    Button("Alle löschen") { recent.clear(); saveRecent() }
                        .font(.subheadline.weight(.semibold)).frame(minHeight: 44)
                        .accessibilityLabel("Suchverlauf löschen").accessibilityIdentifier("search-recent-clear")
                }.textCase(nil)
            }
        }
        if !genres.isEmpty {
            Section {
                let shown = allGenres ? genres : Array(genres.prefix(12))
                LazyVGrid(columns: [GridItem(.adaptive(minimum: textSize.isAccessibilitySize ? 280 : 150), spacing: ListenMetrics.tileGap)],
                          spacing: ListenMetrics.tileGap) {
                    ForEach(shown, id: \.self) { genre in
                        Button { openGenre(genre) } label: { GenreTile(name: genre) }
                            .buttonStyle(ListenPressStyle())
                            .accessibilityLabel(genre).accessibilityHint("Zeigt die Alben dieses Genres in der Bibliothek")
                            .accessibilityIdentifier("search-genre-" + genre)
                    }
                }
                .listRowInsets(EdgeInsets(top: 4, leading: ListenMetrics.pageMargin, bottom: 4, trailing: ListenMetrics.pageMargin))
                .listRowSeparator(.hidden).listRowBackground(Color.clear)
                if genres.count > 12 {
                    Button(allGenres ? "Weniger Genres" : "Alle \(genres.count) Genres") { allGenres.toggle() }
                        .font(.subheadline.weight(.semibold)).frame(minHeight: 44).listRowSeparator(.hidden)
                }
            } header: {
                Text("Stöbern").font(.title3.bold()).foregroundStyle(.primary).textCase(nil).accessibilityAddTraits(.isHeader)
            }
        }
    }
    private func recentRow(_ entry: String) -> some View {
        HStack(spacing: 8) {
            Button { query = entry; remember(entry) } label: {
                Label(entry, systemImage: "clock.arrow.circlepath").foregroundStyle(.primary).lineLimit(1)
                    .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading).contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityLabel("Erneut suchen: \(entry)")
            Button { forget(entry) } label: {
                Image(systemName: "xmark").font(.footnote.weight(.semibold)).foregroundStyle(.secondary)
                    .frame(width: 44, height: 44).contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityLabel("„\(entry)“ aus dem Suchverlauf entfernen")
        }
        .swipeActions { Button("Entfernen", systemImage: "trash", role: .destructive) { forget(entry) } }
    }

    // MARK: Results

    @ViewBuilder private var resultSections: some View {
        if let failure {
            Section {
                ContentUnavailableView {
                    Label("Suche fehlgeschlagen", systemImage: "exclamationmark.triangle")
                } description: { Text(failure) } actions: {
                    Button("Erneut versuchen", systemImage: "arrow.clockwise") { attempt += 1 }
                        .buttonStyle(.borderedProminent).accessibilityIdentifier("search-retry")
                }.listRowSeparator(.hidden)
            }
        } else if let results {
            let playlists = matchingPlaylists
            if results.tracks.isEmpty && results.releases.isEmpty && results.artists.isEmpty && playlists.isEmpty {
                if loading { skeleton } else {
                    Section {
                        ContentUnavailableView {
                            Label("Nichts gefunden für „\(resultsQuery)“", systemImage: "magnifyingglass")
                        } description: {
                            Text("Prüfe die Schreibweise oder suche nach einem Künstler, Album oder Titel.")
                        }.listRowSeparator(.hidden).accessibilityIdentifier("search-no-results")
                    }
                }
            } else {
                groups(results, playlists: playlists)
            }
        } else {
            skeleton
        }
    }
    @ViewBuilder private func groups(_ results: SearchResults, playlists: [Playlist]) -> some View {
        if let hit = SearchMatch.topHit(query: resultsQuery, artists: results.artists.map(\.name), releases: results.releases.map(\.title),
                                        tracks: results.tracks.map(\.title), playlists: playlists.map(\.name)) {
            Section { topHit(hit, results: results, playlists: playlists) } header: {
                HStack(spacing: 8) {
                    Text("Top-Treffer").font(.title3.bold()).foregroundStyle(.primary).accessibilityAddTraits(.isHeader)
                    // A newer search is on its way: the old results stay in place.
                    if loading { ProgressView().controlSize(.small).accessibilityLabel("Suche läuft") }
                    Spacer()
                }.textCase(nil)
            }
        }
        if !results.tracks.isEmpty {
            Section {
                ForEach(Array(shown(results.tracks, .tracks).enumerated()), id: \.offset) { index, track in
                    TrackRow(model: model, track: track) { play(results.tracks, start: index) }
                }
            } header: { header("Titel", count: results.tracks.count, group: .tracks) }
        }
        if !results.releases.isEmpty {
            Section {
                ForEach(shown(results.releases, .releases)) { release in
                    NavigationLink(value: CollectionKind.release(release)) {
                        ResultRow(title: release.title, detail: releaseDetail(release)) {
                            ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: 8)
                        }
                    }
                }
            } header: { header("Alben", count: results.releases.count, group: .releases) }
        }
        if !results.artists.isEmpty {
            Section {
                ForEach(shown(results.artists, .artists)) { artist in
                    NavigationLink(value: CollectionKind.artist(artist)) {
                        ResultRow(title: artist.name, detail: artist.trackCount.map { "Künstler · \($0) Titel" } ?? "Künstler") {
                            ArtworkView(model: model, kind: "artists", id: artist.id).clipShape(Circle())
                        }
                    }
                }
            } header: { header("Künstler", count: results.artists.count, group: .artists) }
        }
        if !playlists.isEmpty {
            Section {
                ForEach(shown(playlists, .playlists)) { playlist in
                    NavigationLink(value: CollectionKind.playlist(playlist)) {
                        ResultRow(title: playlist.name, detail: "Playlist · \(playlist.trackCount) Titel") {
                            MobileCollectionArtwork(model: model, tracks: model.playlistPreviews[playlist.id] ?? [], size: ListenMetrics.rowArtwork)
                        }
                    }
                }
            } header: { header("Playlists", count: playlists.count, group: .playlists, server: false) }
        }
    }
    @ViewBuilder private func topHit(_ hit: (kind: SearchMatch.Kind, index: Int), results: SearchResults, playlists: [Playlist]) -> some View {
        switch hit.kind {
        case .artist:
            let artist = results.artists[hit.index]
            NavigationLink(value: CollectionKind.artist(artist)) {
                TopHitLabel(title: artist.name, detail: "Künstler") {
                    ArtworkView(model: model, kind: "artists", id: artist.id).clipShape(Circle())
                }
            }.accessibilityIdentifier("search-top-hit")
        case .release:
            let release = results.releases[hit.index]
            NavigationLink(value: CollectionKind.release(release)) {
                TopHitLabel(title: release.title, detail: "Album · " + release.artists.joined(separator: " · ")) {
                    ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: 10)
                }
            }.accessibilityIdentifier("search-top-hit")
        case .track:
            let track = results.tracks[hit.index]
            Button { play(results.tracks, start: hit.index) } label: {
                HStack(spacing: 12) {
                    TopHitLabel(title: track.title, detail: "Titel · " + track.artistText) {
                        ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 10)
                    }
                    Image(systemName: "play.fill").font(.system(size: 17, weight: .semibold)).foregroundStyle(.white)
                        .frame(width: 44, height: 44).background(.tint, in: Circle()).accessibilityHidden(true)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain)
                .accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
                .accessibilityIdentifier("search-top-hit")
                .contextMenu { TrackQuickActions(model: model, track: track) }
        case .playlist:
            let playlist = playlists[hit.index]
            NavigationLink(value: CollectionKind.playlist(playlist)) {
                TopHitLabel(title: playlist.name, detail: "Playlist · \(playlist.trackCount) Titel") {
                    MobileCollectionArtwork(model: model, tracks: model.playlistPreviews[playlist.id] ?? [], size: 80)
                }
            }.accessibilityIdentifier("search-top-hit")
        }
    }
    private func releaseDetail(_ release: Release) -> String {
        var parts = ["Album"]
        let artists = release.artists.joined(separator: " · ")
        if !artists.isEmpty { parts.append(artists) }
        if release.year > 0 { parts.append(String(release.year)) }
        return parts.joined(separator: " · ")
    }
    private func shown<Item>(_ items: [Item], _ group: ResultGroup) -> [Item] {
        expanded.contains(group) ? items : Array(items.prefix(Self.collapsed[group] ?? 5))
    }
    private func header(_ title: String, count: Int, group: ResultGroup, server: Bool = true) -> some View {
        let truncated = count > (Self.collapsed[group] ?? 5)
        let countText = server && count >= Self.serverLimit ? "\(count)+" : "\(count)"
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(title).font(.title3.bold()).foregroundStyle(.primary).accessibilityAddTraits(.isHeader)
                .accessibilityLabel("\(title), \(countText) Treffer")
            Text(countText).font(.subheadline).monospacedDigit().foregroundStyle(.secondary).accessibilityHidden(true)
            Spacer(minLength: 8)
            if truncated {
                Button(expanded.contains(group) ? "Weniger" : "Alle anzeigen") {
                    if expanded.contains(group) { expanded.remove(group) } else { expanded.insert(group) }
                }.font(.subheadline.weight(.semibold)).frame(minHeight: 44)
                    .accessibilityIdentifier("search-show-all-" + group.rawValue)
            }
        }.textCase(nil)
    }
    /// Placeholder rows in the size of the results, so nothing jumps when they arrive.
    private var skeleton: some View {
        Section {
            ForEach(0..<6, id: \.self) { _ in
                HStack(spacing: 14) {
                    RoundedRectangle(cornerRadius: 8, style: .continuous).fill(ListenMetrics.surface)
                        .frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
                    VStack(alignment: .leading, spacing: 8) {
                        Capsule().fill(ListenMetrics.surface).frame(width: 180, height: 12)
                        Capsule().fill(ListenMetrics.surface).frame(width: 110, height: 10)
                    }
                    Spacer()
                }.padding(.vertical, 4).listRowSeparator(.hidden)
                    .accessibilityElement(children: .ignore).accessibilityLabel("Suche läuft")
            }
        }
    }

    // MARK: Actions

    private func runSearch() async {
        let current = text
        guard current.count >= 2 else {
            results = nil; resultsQuery = ""; failure = nil; loading = false; expanded = []
            return
        }
        loading = true; failure = nil
        do {
            try await Task.sleep(for: Self.debounce)
            guard let answer = try await model.search(current) else {
                failure = "Die Suche braucht eine Verbindung zum Server."; loading = false
                return
            }
            try Task.checkCancellation()
            if resultsQuery != current { expanded = [] }
            results = answer; resultsQuery = current; loading = false
        } catch is CancellationError {
        } catch {
            // A newer search cancelled this one: its answer no longer matters.
            guard !Task.isCancelled else { return }
            failure = (error as? PlayerError)?.errorDescription ?? "Die Suche konnte nicht geladen werden. Bitte Verbindung prüfen und erneut versuchen."
            loading = false
        }
    }
    private func play(_ tracks: [Track], start: Int) {
        guard let client = model.client else { return }
        remember(resultsQuery)
        model.player.play(tracks, start: start, client: client, context: QueueContext(kind: "search", title: "Suche: „\(resultsQuery)“"))
    }
    private func remember(_ value: String) { recent.record(value); saveRecent() }
    private func forget(_ value: String) { recent.remove(value); saveRecent() }
    private func loadRecent() {
        recent = RecentSearches(recentKey.flatMap { UserDefaults.standard.stringArray(forKey: $0) } ?? [])
    }
    private func saveRecent() {
        guard let recentKey else { return }
        UserDefaults.standard.set(recent.entries, forKey: recentKey)
    }
}

/// The large first result.
private struct TopHitLabel<Artwork: View>: View {
    var title: String
    var detail: String
    @ViewBuilder var artwork: Artwork
    var body: some View {
        HStack(spacing: 14) {
            artwork.frame(width: 80, height: 80)
            VStack(alignment: .leading, spacing: 4) {
                Text(title).font(.title3.bold()).foregroundStyle(.primary).lineLimit(2)
                Text(detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
            }.frame(maxWidth: .infinity, alignment: .leading)
        }.padding(.vertical, 6)
    }
}

/// An album, artist or playlist result: cover, name and what it is.
private struct ResultRow<Artwork: View>: View {
    var title: String
    var detail: String
    @ViewBuilder var artwork: Artwork
    var body: some View {
        HStack(spacing: 14) {
            artwork.frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
            VStack(alignment: .leading, spacing: 3) {
                Text(title).font(.headline).foregroundStyle(.primary).lineLimit(1)
                Text(detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
            }.frame(maxWidth: .infinity, alignment: .leading)
        }.padding(.vertical, 2)
    }
}

/// A genre to browse: a deep color derived from its name (stable across
/// launches), white text with at least 4.5:1 contrast.
private struct GenreTile: View {
    var name: String
    var body: some View {
        Text(name).font(.headline).foregroundStyle(.white).lineLimit(2).multilineTextAlignment(.leading)
            .padding(12)
            .frame(maxWidth: .infinity, minHeight: 72, alignment: .bottomLeading)
            .background(Self.color(for: name).gradient, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
    }
    static func color(for name: String) -> Color {
        var hash: UInt32 = 2_166_136_261
        for byte in name.lowercased().utf8 { hash = (hash ^ UInt32(byte)) &* 16_777_619 }
        return Color(hue: Double(hash % 360) / 360, saturation: 0.6, brightness: 0.42)
    }
}

#endif
