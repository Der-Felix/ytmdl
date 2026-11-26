import SwiftUI
import YTMDLCore

/// Every artist of the library for the A–Z list, loaded page by page in name
/// order while the list is shown. Grouping runs off the main thread and only
/// every few pages, so even thousands of artists never stall scrolling.
@MainActor @Observable final class ArtistDirectory {
    static let pageSize = 120
    /// A ceiling against endless paging; the filter still finds every artist.
    static let ceiling = 20_000
    private(set) var artists: [Artist] = []
    private(set) var sections: [LibraryIndex.Section<Artist>] = []
    private(set) var complete = false
    private(set) var loading = false
    private(set) var failure: String?
    @ObservationIgnored private var generation = UUID()
    @ObservationIgnored private var pages = 0
    /// The directory owns its loading, so a list that is rebuilt or closed
    /// mid-request (the iPad sidebar does both) never strands it half-loaded.
    @ObservationIgnored private var worker: Task<Void, Never>?

    func reset() {
        worker?.cancel(); worker = nil
        artists = []; sections = []; complete = false; loading = false; failure = nil; pages = 0; generation = UUID()
    }
    func retry(_ model: AppModel) async { failure = nil; await load(model) }
    /// Continues where it stopped until every artist is there; returns when
    /// that load ends. Calling it again while it runs just waits for it.
    func load(_ model: AppModel) async {
        if worker == nil {
            guard !complete, failure == nil, !model.offlineMode, model.client != nil else { return }
            let generation = generation
            worker = Task { [weak self] in
                await self?.run(model, generation)
                if let self, self.generation == generation { self.worker = nil }
            }
        }
        await worker?.value
    }
    private func run(_ model: AppModel, _ generation: UUID) async {
        guard let client = model.client else { return }
        loading = true
        defer { if self.generation == generation { loading = false } }
        while !Task.isCancelled && artists.count < Self.ceiling {
            do {
                let page: [Artist] = try await client.get("/library/artists", query: [
                    .init(name: "limit", value: String(Self.pageSize)), .init(name: "offset", value: String(artists.count)),
                    .init(name: "sort", value: "name"), .init(name: "order", value: "asc")])
                guard self.generation == generation, model.client === client else { return }
                var known = Set(artists.map(\.id))
                artists += page.filter { known.insert($0.id).inserted }
                pages += 1
                let finished = page.count < Self.pageSize
                if finished || pages <= 3 || pages % 5 == 0 { await regroup(generation) }
                if finished { complete = true; return }
            } catch {
                guard self.generation == generation else { return }
                if error is CancellationError || (error as? URLError)?.code == .cancelled { await regroup(generation); return }
                failure = model.describe(error); await regroup(generation); return
            }
        }
        await regroup(generation)
    }
    private func regroup(_ generation: UUID) async {
        let snapshot = artists
        let grouped = await Task.detached(priority: .userInitiated) { LibraryIndex.sections(snapshot) { $0.name } }.value
        if self.generation == generation { sections = grouped }
    }
}

#if os(iOS)
// MARK: - Bibliothek

/// "Bibliothek" on iPhone and iPad: shortcuts to the other views of the
/// collection, then the albums with sort, layout, filter and genre chips.
struct MobileLibraryView: View {
    @Bindable var model: AppModel
    /// The first page of the iPhone's Bibliothek tab, with its own large title.
    var tabRoot = false
    @AppStorage(LibraryPreferences.layoutKey) private var layoutName = LibraryAlbumLayout.grid.rawValue
    @State private var filterText = ""
    @State private var filtered: [Release]?
    @State private var filterFailure: String?
    @State private var filtering = false
    @FocusState private var filterFocused: Bool
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var textSize
    private var layout: LibraryAlbumLayout { LibraryAlbumLayout(rawValue: layoutName) ?? .grid }
    private var query: String { filterText.trimmingCharacters(in: .whitespacesAndNewlines) }
    private struct FilterKey: Equatable { let text: String; let genre: String; let sort: LibraryAlbumSort }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                if tabRoot { MobilePageHeader(title: "Bibliothek").padding(.bottom, ListenMetrics.headerGap) }
                LibraryShortcuts().padding(.horizontal, ListenMetrics.pageMargin)
                albumsHeader.padding(.top, ListenMetrics.sectionSpacing)
                filterField.padding(.horizontal, ListenMetrics.pageMargin).padding(.top, ListenMetrics.headerGap)
                if !visibleGenres.isEmpty { genreChips.padding(.top, ListenMetrics.headerGap) }
                albums.padding(.top, ListenMetrics.shelfGap + 4)
            }
            .padding(.top, 4).padding(.bottom, 32)
            .frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .scrollDismissesKeyboard(.interactively)
        .navigationTitle("Bibliothek")
        .modifier(TabRootChrome(active: tabRoot))
        .refreshable { await model.loadLibrary() }
        // Nothing has answered yet and nothing is running: load instead of
        // waiting for a load that was never started.
        .task { if !model.releasesLoaded && model.libraryError == nil && !model.connecting { await model.loadLibrary() } }
        .task(id: FilterKey(text: query, genre: model.genre, sort: model.albumSort)) { await runFilter() }
    }

    // MARK: Albums header, filter, genres

    private var albumsHeader: some View {
        HStack(alignment: .center, spacing: 4) {
            Text("Alben").font(.title2.bold()).accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            Menu {
                Picker("Sortieren nach", selection: Binding(get: { model.albumSort }, set: { sort in Task { await model.setAlbumSort(sort) } })) {
                    ForEach(LibraryAlbumSort.allCases) { Text($0.label).tag($0) }
                }.pickerStyle(.inline)
            } label: {
                Image(systemName: "arrow.up.arrow.down").font(.body.weight(.semibold))
                    .frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .accessibilityLabel("Alben sortieren").accessibilityValue(model.albumSort.label)
            .accessibilityIdentifier("library-sort")
            Button {
                layoutName = layout.toggled.rawValue
            } label: {
                Image(systemName: layout == .grid ? "list.bullet" : "square.grid.2x2").font(.body.weight(.semibold))
                    .frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .accessibilityLabel(layout == .grid ? "Als Liste anzeigen" : "Als Raster anzeigen")
            .accessibilityIdentifier("library-layout")
        }
        .padding(.leading, ListenMetrics.pageMargin).padding(.trailing, ListenMetrics.pageMargin - 10)
    }

    private var filterField: some View {
        HStack(spacing: 10) {
            Image(systemName: "line.3.horizontal.decrease").foregroundStyle(.secondary).accessibilityHidden(true)
            TextField("Alben filtern", text: $filterText).textInputAutocapitalization(.never).autocorrectionDisabled()
                .focused($filterFocused).submitLabel(.search)
                .accessibilityIdentifier("library-filter")
            if filtering { ProgressView().controlSize(.small) }
            if !filterText.isEmpty {
                Button { filterText = "" } label: { Image(systemName: "xmark.circle.fill").frame(width: 44, height: 44) }
                    .buttonStyle(.plain).foregroundStyle(.secondary).accessibilityLabel("Filter leeren")
            }
        }
        .padding(.leading, 14).padding(.trailing, filterText.isEmpty ? 14 : 0).frame(minHeight: 44)
        .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }

    private var visibleGenres: [String] { model.genres }
    private func genreName(_ genre: String) -> String { genre == "__none__" ? "Ohne Genre" : genre }
    private var genreChips: some View {
        ScrollView(.horizontal) {
            HStack(spacing: ListenMetrics.tileGap) {
                chip("Alle Genres", value: "")
                ForEach(visibleGenres, id: \.self) { chip(genreName($0), value: $0) }
            }
        }
        .contentMargins(.horizontal, ListenMetrics.pageMargin, for: .scrollContent)
        .scrollIndicators(.hidden)
    }
    private func chip(_ title: String, value: String) -> some View {
        let selected = model.genre == value
        return Button {
            guard model.genre != value else { return }
            model.genre = value
            Task { await model.filterGenre(value) }
        } label: {
            Text(title).font(.subheadline.weight(.semibold)).lineLimit(1)
                .padding(.horizontal, 14).frame(minHeight: 36)
                .foregroundStyle(selected ? Color.white : Color.primary)
                .background(selected ? AnyShapeStyle(.tint) : AnyShapeStyle(ListenMetrics.surface), in: Capsule())
                .contentShape(Capsule())
        }
        .buttonStyle(ListenPressStyle())
        .frame(minHeight: 44)
        .accessibilityAddTraits(selected ? .isSelected : [])
        .accessibilityHint("Zeigt nur Alben dieses Genres")
        .accessibilityIdentifier("library-genre-" + (value.isEmpty ? "all" : value))
    }

    // MARK: Album content

    private var shown: [Release] { filtered ?? model.releases }
    @ViewBuilder private var albums: some View {
        if !query.isEmpty, let filterFailure {
            ListenSectionFailure(message: filterFailure) { Task { await runFilter() } }.padding(.horizontal, ListenMetrics.pageMargin)
        } else if !query.isEmpty, let filtered, filtered.isEmpty {
            NoMatchesView(query: query).padding(.top, 24)
        } else if query.isEmpty, let message = model.libraryError, !model.connecting {
            // A failed load is never shown as an empty library: "Noch keine
            // Alben" needs an answer from the server with an empty list.
            if model.releases.isEmpty {
                ContentUnavailableView {
                    Label("Alben konnten nicht geladen werden", systemImage: "exclamationmark.triangle")
                } description: { Text(message) } actions: {
                    Button("Erneut laden", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }
                        .buttonStyle(.borderedProminent).accessibilityIdentifier("library-retry")
                }.accessibilityIdentifier("library-error")
            } else {
                ListenSectionFailure(message: message) { Task { await model.loadLibrary() } }.padding(.horizontal, ListenMetrics.pageMargin)
                albumList(model.releases)
            }
        } else if shown.isEmpty && (model.connecting || !model.releasesLoaded || filtering) {
            skeleton
        } else if shown.isEmpty {
            ContentUnavailableView {
                Label("Noch keine Alben", systemImage: "square.stack")
            } description: {
                Text(model.genre.isEmpty ? "Füge im Web Musik hinzu. Sie erscheint dann hier." : "In diesem Genre gibt es noch keine Alben.")
            } actions: {
                if !model.genre.isEmpty {
                    Button("Alle Genres zeigen") { model.genre = ""; Task { await model.filterGenre("") } }.buttonStyle(.bordered)
                }
            }.padding(.top, 24)
        } else {
            albumList(shown)
            if filtered == nil && model.moreReleases {
                Button("Weitere Alben laden") { Task { await model.loadMore() } }
                    .disabled(model.connecting).frame(maxWidth: .infinity, minHeight: 44).padding(.top, 16)
            }
        }
    }

    @ViewBuilder private func albumList(_ releases: [Release]) -> some View {
        if layout == .grid {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: gridMinimum, maximum: 260), spacing: ListenMetrics.shelfGap, alignment: .top)],
                      alignment: .leading, spacing: 20) {
                ForEach(releases) { release in
                    LibraryAlbumCard(model: model, release: release).onAppear { loadMoreIfNeeded(release, in: releases) }
                }
            }.padding(.horizontal, ListenMetrics.pageMargin)
        } else {
            LazyVStack(spacing: 0) {
                ForEach(releases) { release in
                    LibraryAlbumRow(model: model, release: release).onAppear { loadMoreIfNeeded(release, in: releases) }
                    if release.id != releases.last?.id { Divider().padding(.leading, 16 + 56 + 12) }
                }
            }
            .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            .padding(.horizontal, ListenMetrics.pageMargin)
        }
    }
    private var gridMinimum: CGFloat { textSize.isAccessibilitySize ? 220 : sizeClass == .regular ? 160 : 150 }
    private var skeleton: some View {
        Group {
            if layout == .grid {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: gridMinimum, maximum: 260), spacing: ListenMetrics.shelfGap, alignment: .top)], spacing: 20) {
                    ForEach(0..<6, id: \.self) { _ in AlbumCardSkeleton() }
                }
            } else {
                VStack(spacing: 0) { ForEach(0..<6, id: \.self) { _ in TrackRowSkeleton().padding(.horizontal, 16).padding(.vertical, 4) } }
                    .background(ListenMetrics.surface.opacity(0.5), in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            }
        }
        .padding(.horizontal, ListenMetrics.pageMargin)
        .accessibilityElement(children: .ignore).accessibilityLabel("Alben werden geladen")
        .accessibilityIdentifier("library-loading")
    }
    /// The next page when the last loaded album comes on screen.
    private func loadMoreIfNeeded(_ release: Release, in releases: [Release]) {
        guard filtered == nil, release.id == releases.last?.id, model.moreReleases, !model.connecting else { return }
        Task { await model.loadMore() }
    }
    /// The filter asks the server (title and artist), debounced, so it finds
    /// albums that are not loaded yet.
    private func runFilter() async {
        let text = query
        guard !text.isEmpty else { filtered = nil; filterFailure = nil; filtering = false; return }
        filtering = true
        do {
            try await Task.sleep(for: .milliseconds(300))
            guard let client = model.client else { return }
            let result: [Release] = try await client.get("/library/releases", query: [
                .init(name: "q", value: text), .init(name: "genre", value: model.genre), .init(name: "limit", value: "120")] + model.albumSort.queryItems)
            try Task.checkCancellation()
            filtered = result; filterFailure = nil; filtering = false
        } catch is CancellationError {
        } catch {
            if !Task.isCancelled { filterFailure = "Der Filter konnte nicht geladen werden. Bitte Verbindung prüfen."; filtering = false }
        }
    }
}

/// The views of the collection besides the albums, as calm tiles: two
/// columns on iPhone, three on iPad, one at the largest text sizes.
private struct LibraryShortcuts: View {
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var textSize
    private var columns: Int { textSize.isAccessibilitySize ? 1 : sizeClass == .regular ? 3 : 2 }
    var body: some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: ListenMetrics.tileGap), count: columns), spacing: ListenMetrics.tileGap) {
            tile("Favoriten", symbol: "heart.fill", id: "favorites", hint: "Öffnet deine Lieblingstitel", value: CollectionKind.favorites)
            tile("Künstler", symbol: "music.microphone", id: "artists", hint: "Alle Künstler von A bis Z", value: Destination.artists)
            tile("Titel", symbol: "music.note", id: "tracks", hint: "Alle Titel der Bibliothek", value: LibraryRoute.tracks)
            tile("Genres", symbol: "guitars.fill", id: "genres", hint: "Alben nach Genre", value: LibraryRoute.genres)
            tile("Offline", symbol: "arrow.down.circle.fill", id: "offline", hint: "Auf diesem Gerät gespeicherte Musik", value: Destination.downloads)
            tile("Playlists", symbol: "music.note.list", id: "playlists", hint: "Deine Playlists", value: Destination.playlists)
        }
    }
    private func tile<Value: Hashable>(_ title: String, symbol: String, id: String, hint: String, value: Value) -> some View {
        NavigationLink(value: value) {
            HStack(spacing: 12) {
                // A fixed box for every symbol: labels share one baseline
                // whatever the symbol's own height.
                Image(systemName: symbol).font(.system(size: 19, weight: .semibold)).foregroundStyle(.tint)
                    .frame(width: 28, height: 28)
                Text(title).font(.body.weight(.semibold)).foregroundStyle(.primary)
                    .lineLimit(textSize.isAccessibilitySize ? 2 : 1).minimumScaleFactor(0.85)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity, minHeight: ListenMetrics.tileHeight, alignment: .leading)
            .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityLabel(title).accessibilityHint(hint)
        .accessibilityIdentifier("library-shortcut-" + id)
    }
}

/// An album in the grid: cover, title, artist.
struct LibraryAlbumCard: View {
    var model: AppModel
    var release: Release
    var body: some View {
        NavigationLink(value: CollectionKind.release(release)) {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: ListenMetrics.cardRadius)
                VStack(alignment: .leading, spacing: 2) {
                    Text(release.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(2)
                    Text(release.artists.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityHint("Öffnet das Album")
    }
}

/// An album in the list layout: small cover, title, artist and year.
struct LibraryAlbumRow: View {
    var model: AppModel
    var release: Release
    var body: some View {
        NavigationLink(value: CollectionKind.release(release)) {
            HStack(spacing: 12) {
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: 6).frame(width: 56, height: 56)
                VStack(alignment: .leading, spacing: 2) {
                    Text(release.title).font(.body.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                    Text(release.year > 0 ? "\(release.artists.joined(separator: " · ")) · \(String(release.year))" : release.artists.joined(separator: " · "))
                        .font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                }.frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary).accessibilityHidden(true)
            }
            .padding(.horizontal, 16).padding(.vertical, 8).contentShape(Rectangle())
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityHint("Öffnet das Album")
    }
}

/// The final shape of an album card while the first page loads.
private struct AlbumCardSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous).fill(ListenMetrics.surface).aspectRatio(1, contentMode: .fit)
            Capsule().fill(ListenMetrics.surface).frame(width: 110, height: 11)
            Capsule().fill(ListenMetrics.surface).frame(width: 70, height: 9)
        }.accessibilityHidden(true)
    }
}

// MARK: - Künstler

/// "Künstler": every artist A–Z in sections with the system's index on the
/// side, round pictures, and a filter that asks the server.
struct MobileArtistListView: View {
    var model: AppModel
    @State private var filterText = ""
    @State private var filtered: [Artist]?
    @State private var filterFailure: String?
    private var directory: ArtistDirectory { model.artistDirectory }
    private var query: String { filterText.trimmingCharacters(in: .whitespacesAndNewlines) }
    var body: some View {
        List {
            Section {
                filterField.listRowInsets(EdgeInsets(top: 4, leading: ListenMetrics.pageMargin, bottom: 8, trailing: ListenMetrics.pageMargin))
                    .listRowSeparator(.hidden).listRowBackground(Color.clear)
            }
            if !query.isEmpty {
                filterResults
            } else if directory.artists.isEmpty {
                if let failure = directory.failure {
                    ContentUnavailableView {
                        Label("Künstler konnten nicht geladen werden", systemImage: "exclamationmark.triangle")
                    } description: { Text(failure) } actions: {
                        Button("Erneut laden", systemImage: "arrow.clockwise") { Task { await directory.retry(model) } }.buttonStyle(.borderedProminent)
                    }.listRowSeparator(.hidden)
                } else if directory.complete {
                    ContentUnavailableView("Noch keine Künstler", systemImage: "music.microphone",
                                           description: Text("Künstler erscheinen, sobald ihre Musik in der Bibliothek ist.")).listRowSeparator(.hidden)
                } else {
                    ForEach(0..<10, id: \.self) { _ in skeletonRow }
                }
            } else {
                ForEach(directory.sections) { section in
                    Section {
                        // A row, not a pinned header: nothing shows through
                        // below the tab bar's hard edge.
                        Text(section.letter).font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
                            .accessibilityAddTraits(.isHeader)
                            .listRowInsets(EdgeInsets(top: 12, leading: ListenMetrics.pageMargin, bottom: 2, trailing: ListenMetrics.pageMargin))
                            .listRowSeparator(.hidden).listRowBackground(Color.clear)
                        ForEach(section.items) { artist in row(artist) }
                    }
                    .sectionIndexLabel(section.letter)
                }
                if directory.loading {
                    HStack(spacing: 10) { ProgressView(); Text("Weitere Künstler werden geladen …").font(.footnote).foregroundStyle(.secondary) }
                        .listRowSeparator(.hidden)
                } else if let failure = directory.failure {
                    ListenSectionFailure(message: failure) { Task { await directory.retry(model) } }.listRowSeparator(.hidden)
                }
            }
        }
        .listStyle(.plain)
        .listSectionIndexVisibility(.visible)
        .modifier(ReadableContentWidth(maxWidth: 760))
        .scrollDismissesKeyboard(.interactively)
        .navigationTitle("Künstler")
        .task { await directory.load(model) }
        .task(id: query) { await runFilter() }
        .refreshable { directory.reset(); await directory.load(model) }
    }
    private var filterField: some View {
        HStack(spacing: 10) {
            Image(systemName: "line.3.horizontal.decrease").foregroundStyle(.secondary).accessibilityHidden(true)
            TextField("Künstler filtern", text: $filterText).textInputAutocapitalization(.never).autocorrectionDisabled()
                .accessibilityIdentifier("artist-filter")
            if !filterText.isEmpty {
                Button { filterText = "" } label: { Image(systemName: "xmark.circle.fill").frame(width: 44, height: 44) }
                    .buttonStyle(.plain).foregroundStyle(.secondary).accessibilityLabel("Filter leeren")
            }
        }
        .padding(.leading, 14).padding(.trailing, filterText.isEmpty ? 14 : 0).frame(minHeight: 44)
        .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
    @ViewBuilder private var filterResults: some View {
        if let filterFailure {
            ListenSectionFailure(message: filterFailure) { Task { await runFilter() } }.listRowSeparator(.hidden)
        } else if let filtered {
            if filtered.isEmpty { NoMatchesView(query: query).listRowSeparator(.hidden) }
            else { ForEach(filtered) { row($0) } }
        } else {
            // The rows' final shape while the server filters.
            ForEach(0..<4, id: \.self) { _ in skeletonRow }
        }
    }
    private func row(_ artist: Artist) -> some View {
        NavigationLink(value: CollectionKind.artist(artist)) {
            HStack(spacing: 12) {
                ArtistAvatar(model: model, artist: artist).frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
                VStack(alignment: .leading, spacing: 2) {
                    Text(artist.name).font(.body.weight(.semibold)).lineLimit(1)
                    if let detail = detail(artist) { Text(detail).font(.subheadline).foregroundStyle(.secondary).lineLimit(1) }
                }
            }
        }
        .listRowInsets(EdgeInsets(top: 6, leading: ListenMetrics.pageMargin, bottom: 6, trailing: ListenMetrics.pageMargin))
        .accessibilityIdentifier("artist-row-" + artist.id)
    }
    private func detail(_ artist: Artist) -> String? {
        var parts: [String] = []
        if let albums = artist.releaseCount, albums > 0 { parts.append(albums == 1 ? "1 Album" : "\(albums) Alben") }
        if let titles = artist.trackCount, titles > 0 { parts.append(CollectionSummary.tracks(titles)) }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
    private var skeletonRow: some View {
        HStack(spacing: 12) {
            Circle().fill(ListenMetrics.surface).frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
            Capsule().fill(ListenMetrics.surface).frame(width: 140, height: 12)
        }.padding(.vertical, 2).listRowSeparator(.hidden).accessibilityHidden(true)
    }
    private func runFilter() async {
        let text = query
        guard !text.isEmpty else { filtered = nil; filterFailure = nil; return }
        filtered = nil; filterFailure = nil
        do {
            try await Task.sleep(for: .milliseconds(300))
            guard let client = model.client else { return }
            let result: [Artist] = try await client.get("/library/artists", query: [
                .init(name: "q", value: text), .init(name: "limit", value: "120"), .init(name: "sort", value: "name"), .init(name: "order", value: "asc")])
            try Task.checkCancellation()
            filtered = result
        } catch is CancellationError {
        } catch {
            if !Task.isCancelled { filterFailure = "Der Filter konnte nicht geladen werden. Bitte Verbindung prüfen." }
        }
    }
}

// MARK: - Titel

/// "Titel": every title of the library, paged by the server in the chosen order.
struct LibraryTracksView: View {
    var model: AppModel
    private enum Order: String, CaseIterable, Identifiable {
        case recent, title, artist, album
        var id: String { rawValue }
        var label: String { switch self { case .recent: "Zuletzt hinzugefügt"; case .title: "Titel"; case .artist: "Künstler"; case .album: "Album" } }
        var direction: String { self == .recent ? "desc" : "asc" }
    }
    @AppStorage("libraryTrackSort") private var orderName = Order.recent.rawValue
    @State private var tracks: [Track] = []
    @State private var more = true
    @State private var busy = false
    @State private var failure: String?
    @State private var generation = 0
    private var order: Order { Order(rawValue: orderName) ?? .recent }
    var body: some View {
        List {
            Section {
                HStack(spacing: ListenMetrics.shelfGap) {
                    Button { play(tracks) } label: { Label("Abspielen", systemImage: "play.fill") }
                        .buttonStyle(MobileActionStyle(prominent: true)).accessibilityIdentifier("tracks-play")
                    Button { play(tracks.shuffled()) } label: { Label("Zufall", systemImage: "shuffle") }
                        .buttonStyle(MobileActionStyle()).accessibilityIdentifier("tracks-shuffle")
                }.disabled(tracks.isEmpty)
                    .listRowInsets(EdgeInsets(top: 8, leading: ListenMetrics.pageMargin, bottom: 16, trailing: ListenMetrics.pageMargin))
                    .listRowSeparator(.hidden).listRowBackground(Color.clear)
            }
            Section {
                ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in
                    TrackRow(model: model, track: track) { play(tracks, index: index) }
                        .listRowInsets(CollectionMetrics.rowInsets)
                        .onAppear { if index == tracks.count - 1 && more && !busy && failure == nil { Task { await load() } } }
                }
                if busy && tracks.isEmpty {
                    ForEach(0..<8, id: \.self) { _ in TrackRowSkeleton().listRowInsets(CollectionMetrics.rowInsets).listRowSeparator(.hidden) }
                } else if busy {
                    ProgressView().frame(maxWidth: .infinity).listRowSeparator(.hidden)
                } else if let failure {
                    VStack(alignment: .leading, spacing: 12) {
                        Label(failure, systemImage: "exclamationmark.triangle").font(.subheadline).foregroundStyle(.secondary)
                        Button("Erneut laden", systemImage: "arrow.clockwise") { Task { await load() } }.buttonStyle(.bordered)
                    }.listRowSeparator(.hidden)
                } else if tracks.isEmpty {
                    ContentUnavailableView("Noch keine Titel", systemImage: "music.note", description: Text("Füge im Web Musik hinzu. Sie erscheint dann hier."))
                        .listRowSeparator(.hidden)
                }
            }
        }
        .listStyle(.plain)
        .modifier(ReadableContentWidth())
        .navigationTitle("Titel")
        .toolbar {
            ToolbarItem(placement: .primaryAction) {
                Menu {
                    Picker("Sortieren nach", selection: $orderName) {
                        ForEach(Order.allCases) { Text($0.label).tag($0.rawValue) }
                    }.pickerStyle(.inline)
                } label: { Label("Sortieren", systemImage: "arrow.up.arrow.down") }
                    .accessibilityLabel("Titel sortieren").accessibilityValue(order.label)
                    .accessibilityIdentifier("tracks-sort")
            }
        }
        .task(id: orderName) { await reload() }
        .refreshable { await reload() }
    }
    /// Starts over in the current order. A newer generation supersedes a load
    /// still in flight, so changing the sort mid-load never leaves the list empty.
    private func reload() async {
        generation += 1
        tracks = []; more = true; failure = nil; busy = false
        await load()
    }
    private func play(_ list: [Track], index: Int = 0) {
        guard let client = model.client, !list.isEmpty else { return }
        model.player.play(list, start: index, client: client, context: QueueContext(kind: "library", title: "Titel"))
    }
    private func load() async {
        guard let client = model.client, !busy, more else { return }
        let started = generation
        let current = order
        busy = true
        defer { if generation == started { busy = false } }
        do {
            let page: [Track] = try await client.get("/library/tracks", query: [
                .init(name: "limit", value: "100"), .init(name: "offset", value: String(tracks.count)),
                .init(name: "sort", value: current.rawValue), .init(name: "order", value: current.direction)])
            guard generation == started, model.client === client else { return }
            tracks += page; more = page.count == 100; failure = nil
        } catch {
            if generation == started && !Task.isCancelled && !(error is CancellationError) {
                failure = "Titel konnten nicht geladen werden. Bitte Verbindung prüfen und erneut versuchen."
            }
        }
    }
}

// MARK: - Genres

/// "Genres": the genres of the library; each opens its albums.
struct LibraryGenresView: View {
    var model: AppModel
    var body: some View {
        List {
            if model.genres.isEmpty {
                ContentUnavailableView("Keine Genres", systemImage: "guitars", description: Text("Alben mit Genre-Angabe erscheinen hier nach Genre sortiert."))
                    .listRowSeparator(.hidden)
            }
            ForEach(model.genres, id: \.self) { genre in
                NavigationLink(value: LibraryRoute.genre(genre)) {
                    HStack(spacing: 12) {
                        Image(systemName: "guitars").font(.body.weight(.semibold)).foregroundStyle(.tint).frame(width: 28)
                        Text(genre == "__none__" ? "Ohne Genre" : genre).font(.body)
                    }.frame(minHeight: 36)
                }.accessibilityIdentifier("genre-row-" + genre)
            }
        }
        .listStyle(.plain)
        .modifier(ReadableContentWidth(maxWidth: 760))
        .navigationTitle("Genres")
    }
}

/// The albums of one genre, in the library's order.
struct GenreAlbumsView: View {
    var model: AppModel
    var genre: String
    @State private var releases: [Release] = []
    @State private var more = true
    @State private var busy = false
    @State private var failure: String?
    @Environment(\.horizontalSizeClass) private var sizeClass
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if releases.isEmpty && busy {
                    LazyVGrid(columns: columns, spacing: 20) { ForEach(0..<6, id: \.self) { _ in AlbumCardSkeleton() } }
                        .accessibilityElement(children: .ignore).accessibilityLabel("Alben werden geladen")
                } else if let failure, releases.isEmpty {
                    ListenSectionFailure(message: failure) { Task { await load() } }
                } else if releases.isEmpty {
                    ContentUnavailableView("Keine Alben", systemImage: "square.stack", description: Text("In diesem Genre gibt es noch keine Alben."))
                } else {
                    LazyVGrid(columns: columns, alignment: .leading, spacing: 20) {
                        ForEach(releases) { release in
                            LibraryAlbumCard(model: model, release: release).onAppear {
                                if release.id == releases.last?.id && more && !busy { Task { await load() } }
                            }
                        }
                    }
                }
            }
            .padding(.horizontal, ListenMetrics.pageMargin).padding(.top, 8).padding(.bottom, 32)
            .frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .navigationTitle(genre == "__none__" ? "Ohne Genre" : genre)
        .task { if releases.isEmpty { await load() } }
    }
    private var columns: [GridItem] {
        [GridItem(.adaptive(minimum: sizeClass == .regular ? 160 : 150, maximum: 260), spacing: ListenMetrics.shelfGap, alignment: .top)]
    }
    private func load() async {
        guard let client = model.client, !busy, more else { return }
        busy = true; defer { busy = false }
        do {
            let page: [Release] = try await client.get("/library/releases", query: [
                .init(name: "genre", value: genre), .init(name: "limit", value: "60"), .init(name: "offset", value: String(releases.count))] + model.albumSort.queryItems)
            try Task.checkCancellation()
            guard model.client === client else { return }
            let known = Set(releases.map(\.id))
            releases += page.filter { !known.contains($0.id) }; more = page.count == 60; failure = nil
        } catch is CancellationError {
        } catch {
            if !Task.isCancelled { failure = "Alben konnten nicht geladen werden. Bitte Verbindung prüfen." }
        }
    }
}
#endif
