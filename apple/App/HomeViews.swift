import SwiftUI
import YTMDLCore
#if os(iOS)

/// Spacing, sizes and radii of the iPhone and iPad listening pages (Start and
/// Suche). Views use these instead of their own numbers.
enum ListenMetrics {
    /// Leading and trailing page margin; shelves scroll edge to edge but their
    /// first card starts here, aligned with the section titles.
    static let pageMargin: CGFloat = 16
    /// Between cards in a shelf.
    static let shelfGap: CGFloat = 12
    /// Between quick-access tiles and genre tiles.
    static let tileGap: CGFloat = 8
    /// Between the sections of a page.
    static let sectionSpacing: CGFloat = 30
    /// Between a section title and its content.
    static let headerGap: CGFloat = 12
    static let cardWidth: CGFloat = 148
    /// Card width at accessibility text sizes, so two lines of text still fit.
    static let largeCardWidth: CGFloat = 200
    static let tileHeight: CGFloat = 56
    /// One radius for every tile, card, action and surface of the pages.
    static let tileRadius: CGFloat = 12
    static let cardRadius: CGFloat = 12
    /// The cover of a row (titles, albums, artists, the queue).
    static let rowArtwork: CGFloat = 48
    /// Insets of every row in a list: 6 pt above and below, the page margin
    /// in front, room for the "…" button behind.
    static let rowInsets = EdgeInsets(top: 6, leading: pageMargin, bottom: 6, trailing: 4)
    static let pressDuration = 0.14
    static var surface: Color { Color(uiColor: .secondarySystemGroupedBackground) }
}

/// The large title of a tab's first page on iPhone. It sits where the system
/// large title would, without the empty navigation bar row above it, and
/// wraps or shrinks instead of truncating at the largest text sizes.
struct MobilePageHeader<Trailing: View>: View {
    var title: String
    @ViewBuilder var trailing: Trailing
    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Text(title).font(.largeTitle.bold())
                .lineLimit(2).minimumScaleFactor(0.7)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityAddTraits(.isHeader)
            trailing
        }
        .padding(.leading, ListenMetrics.pageMargin).padding(.trailing, ListenMetrics.pageMargin - 6)
        .padding(.top, 8)
    }
}
extension MobilePageHeader where Trailing == EmptyView {
    init(title: String) { self.init(title: title) { EmptyView() } }
}

/// A tab's first page on iPhone shows its own title (`MobilePageHeader`), so
/// the navigation bar is hidden there; the page colour covers the status bar
/// so scrolled content never reads behind the clock. Pushed copies of the
/// same view (Start's "Alle Alben") keep the bar and its back button.
struct TabRootChrome: ViewModifier {
    var active: Bool
    @ViewBuilder func body(content: Content) -> some View {
        if active {
            content
                .toolbar(.hidden, for: .navigationBar)
                .overlay(alignment: .top) {
                    Color.clear.frame(height: 0)
                        .background(Color(uiColor: .systemGroupedBackground).ignoresSafeArea(edges: .top))
                        .allowsHitTesting(false).accessibilityHidden(true)
                }
        } else {
            content
        }
    }
}

/// The empty result of a filter, in German whatever the system's language.
struct NoMatchesView: View {
    var query: String
    var body: some View {
        ContentUnavailableView("Keine Treffer für „\(query)“", systemImage: "magnifyingglass",
                               description: Text("Prüfe die Schreibweise oder versuche einen anderen Begriff."))
    }
}

/// A short confirmation after "Als Nächstes spielen", "Zur Warteschlange" or
/// "Zur Playlist hinzufügen", in the style of the queue's undo bar. It fades
/// in from the top edge (only fades with Reduce Motion) and leaves by itself.
struct NoticeBanner: View {
    var model: AppModel
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        VStack {
            if let notice = model.notice {
                HStack(spacing: 10) {
                    Image(systemName: notice.symbol).foregroundStyle(.tint).accessibilityHidden(true)
                    Text(notice.text).font(.subheadline.weight(.semibold)).lineLimit(2)
                }
                .padding(.horizontal, 16).padding(.vertical, 10)
                .background(.regularMaterial, in: Capsule())
                .shadow(color: .black.opacity(0.12), radius: 8, y: 2)
                .padding(.horizontal, ListenMetrics.pageMargin).padding(.top, 4)
                .accessibilityElement(children: .combine)
                .accessibilityIdentifier("app-notice")
                .transition(reduceMotion ? .opacity : .move(edge: .top).combined(with: .opacity))
                .onTapGesture { model.notice = nil }
            }
        }
        .frame(maxWidth: .infinity)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: model.notice?.id)
    }
}

/// Press feedback for cards and tiles: a slight shrink (not with Reduce
/// Motion) and dimming, ease-out, under 150 ms.
struct ListenPressStyle: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.isEnabled) private var enabled
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed && !reduceMotion ? 0.97 : 1)
            .opacity(!enabled ? 0.45 : configuration.isPressed ? 0.75 : 1)
            .animation(.easeOut(duration: ListenMetrics.pressDuration), value: configuration.isPressed)
    }
}

/// The start screen ("Start"): a greeting as the title, "Weiterhören" while
/// something waits to go on, then the shelves in the order the listener chose
/// ("Start anpassen"): quick access with pinned tiles, mixes, "Wieder
/// einsteigen", recent titles, new albums, most played titles and the
/// optional shelves. Server parts load and fail on their own and never raise
/// an alert; without a connection a hint points to the saved music.
struct MobileHomeView: View {
    var model: AppModel
    /// The first page of the iPhone's Start tab: own title and the settings button.
    var tabRoot = false
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// An artist opened from "Deine Top-Künstler".
    @State private var pushed: CollectionKind?
    @State private var missingArtist: String?
    /// The "Favoriten" shelf, loaded only while it is shown.
    @State private var favoriteTracks: [Track] = []
    private var feed: ListenFeed { model.feed }
    private var layout: HomeLayout { model.homeLayout }
    private var cardWidth: CGFloat { textSize.isAccessibilitySize ? ListenMetrics.largeCardWidth : ListenMetrics.cardWidth }
    private var localGenres: [String] { model.genres.filter { $0 != "__none__" } }
    private var greeting: String { HomeGreeting.text(hour: Calendar.current.component(.hour, from: .now)) }
    private struct FavoritesKey: Equatable { let shown: Bool; let favorites: Int; let user: String? }

    var body: some View {
        content
            .navigationDestination(item: $pushed) { CollectionView(model: model, kind: $0) }
            .task(id: FavoritesKey(shown: layout.isVisible(.favorites), favorites: model.favoriteIDs.count, user: model.user?.id)) {
                guard layout.isVisible(.favorites) else { return }
                favoriteTracks = await model.favoriteShelfTracks()
            }
            .alert("Nicht gefunden", isPresented: Binding(get: { missingArtist != nil }, set: { if !$0 { missingArtist = nil } })) {
                Button("OK") { missingArtist = nil }
            } message: { Text("„\(missingArtist ?? "")“ ist in deiner Bibliothek nicht als Künstler zu finden.") }
    }

    @ViewBuilder private func shelf(_ shelf: HomeShelf) -> some View {
        switch shelf {
        case .quickAccess: quickAccess.padding(.horizontal, ListenMetrics.pageMargin)
        case .mixes: mixShelf
        case .jumpBackIn: jumpBackInShelf
        case .recent: historyShelf
        case .newReleases: releaseShelf
        case .frequent: frequentShelf
        case .discover: discoverShelf
        case .topArtists: topArtistShelf
        case .genres: genreShelf
        case .favorites: favoritesShelf
        case .playlists: playlistShelf
        }
    }

    /// The way into "Start anpassen", after the last shelf.
    private var customizeButton: some View {
        NavigationLink { HomeEditorView(model: model) } label: {
            Label("Start anpassen", systemImage: "slider.horizontal.3")
        }
        .buttonStyle(MobileActionStyle())
        .frame(maxWidth: 360)
        .frame(maxWidth: .infinity)
        .padding(.horizontal, ListenMetrics.pageMargin)
        .accessibilityHint("Bereiche ein- und ausblenden, sortieren und Kacheln anheften")
        .accessibilityIdentifier("home-customize")
    }

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
                if tabRoot {
                    MobilePageHeader(title: greeting) {
                        // Settings live behind this button, so the tab bar
                        // keeps four short entries.
                        NavigationLink(value: Destination.settings) {
                            // Fixed size: the title next to it already grows, and a
                            // scaled gear pushed past the edge at the largest sizes.
                            Image(systemName: "gearshape").font(.system(size: 22, weight: .semibold))
                                .frame(width: 44, height: 44).contentShape(Rectangle())
                        }
                        .buttonStyle(ListenPressStyle())
                        .accessibilityLabel("Einstellungen").accessibilityHint("Konto, Server, Klang und Darstellung")
                        .accessibilityIdentifier("home-settings")
                    }.padding(.bottom, ListenMetrics.headerGap - ListenMetrics.sectionSpacing)
                }
                if feed.unreachable { offlineHint.padding(.horizontal, ListenMetrics.pageMargin) }
                ResumeCard(model: model).padding(.horizontal, ListenMetrics.pageMargin)
                ForEach(layout.visibleShelves) { shelf($0) }
                customizeButton
            }
            .padding(.top, 4).padding(.bottom, 32)
            .frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
            // A shelf that finishes loading fades from its skeleton (same
            // size, so nothing jumps); with Reduce Motion it just switches.
            .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: [feed.mixesPhase, feed.historyPhase, feed.releasesPhase, feed.frequentPhase])
            .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: layout.visibleShelves)
        }
        .background(Color(uiColor: .systemGroupedBackground))
        // Content scrolling under the floating tab bar and mini player gets a
        // solid edge, so no caption reads half-covered behind them.
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .navigationTitle(greeting)
        .modifier(TabRootChrome(active: tabRoot))
        .refreshable { await model.loadLibrary(); await model.loadListenFeed() }
        .task(id: model.playlistRevision) { await model.loadPlaylistPreviews(Array(model.playlists.prefix(12))) }
        .task(id: model.user?.id) { await model.loadListenFeed() }
    }

    // MARK: Quick access

    private enum QuickEntry: Identifiable {
        case pin(HomePin), favorites, forYou, mix(PersonalMix), genre(String), playlist(Playlist)
        var id: String {
            switch self {
            case .pin(let pin): "pin-" + pin.id
            case .favorites: "favorites"
            case .forYou: "foryou"
            case .mix(let mix): "mix-" + mix.id
            case .genre(let genre): "genre-" + genre
            case .playlist(let playlist): "playlist-" + playlist.id
            }
        }
        var isPin: Bool { if case .pin = self { true } else { false } }
    }
    private var quickColumns: Int { textSize.isAccessibilitySize ? 1 : sizeClass == .regular ? 3 : 2 }
    /// A pin whose item is gone (a deleted playlist, a mix not offered
    /// today) waits hidden instead of opening nothing.
    private func available(_ pin: HomePin) -> Bool {
        switch pin.kind {
        case .playlist: return model.playlists.isEmpty || model.playlists.contains { $0.id == pin.itemID }
        case .mix: return feed.mixesPhase != .loaded || feed.mixes.contains { $0.id == pin.itemID }
        default: return true
        }
    }
    /// The pinned tiles first (up to eight), then defaults fill the grid:
    /// favorites, "Für dich", the first mixes, playlists. Without pins six
    /// tiles. Full rows only, so the grid never ends in a gap; a pin is never cut.
    private var quickEntries: [QuickEntry] {
        let pins = layout.pins.filter(available)
        var entries = pins.map(QuickEntry.pin)
        var defaults: [QuickEntry] = []
        if !layout.isPinned(.favorites, HomePin.favorites.itemID) { defaults.append(.favorites) }
        switch feed.mixesPhase {
        case .loaded:
            defaults.append(.forYou)
            defaults += feed.mixes.filter { !layout.isPinned(.mix, $0.id) }.prefix(2).map(QuickEntry.mix)
        case .unsupported: defaults += localGenres.filter { !layout.isPinned(.genre, $0) }.prefix(2).map(QuickEntry.genre)
        default: break
        }
        defaults += model.playlists.filter { !layout.isPinned(.playlist, $0.id) }.prefix(6).map(QuickEntry.playlist)
        entries = Array((entries + defaults).prefix(pins.isEmpty ? 6 : HomeLayout.pinLimit))
        while entries.count > quickColumns, entries.count % quickColumns != 0, entries.last?.isPin == false { entries.removeLast() }
        return entries
    }
    /// While the first answers are outstanding the grid keeps its final height.
    private var quickLoading: Bool {
        model.playlists.isEmpty && (model.connecting || feed.mixesPhase == .loading || feed.mixesPhase == .idle)
    }
    private var quickAccess: some View {
        let entries = quickEntries
        let placeholders = quickLoading ? max(0, 4 - entries.count) : 0
        return LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: ListenMetrics.tileGap), count: quickColumns),
                         spacing: ListenMetrics.tileGap) {
            ForEach(entries) { quickTile($0) }
            ForEach(0..<placeholders, id: \.self) { _ in
                RoundedRectangle(cornerRadius: ListenMetrics.tileRadius, style: .continuous).fill(ListenMetrics.surface)
                    .frame(height: ListenMetrics.tileHeight).accessibilityHidden(true)
            }
        }
    }
    @ViewBuilder private func quickTile(_ entry: QuickEntry) -> some View {
        switch entry {
        case .pin(let pin): pinTile(pin)
        case .favorites:
            NavigationLink(value: CollectionKind.favorites) {
                QuickTile(title: "Favoriten") { QuickSymbol(name: "heart.fill") }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Favoriten").accessibilityHint("Öffnet deine Lieblingstitel")
                .accessibilityIdentifier("home-quick-favorites")
                .contextMenu { PinToStartButton(model: model, pin: .favorites) }
        case .forYou:
            NavigationLink(value: ForYouRoute()) {
                QuickTile(title: "Für dich") { QuickSymbol(name: "sparkles") }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Für dich").accessibilityHint("Mixe, Genres und deine Top-Künstler")
                .accessibilityIdentifier("home-quick-foryou")
        case .mix(let mix):
            NavigationLink(value: mix) {
                QuickTile(title: mix.title) { mixCover(mix) }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Mix: \(mix.title)").accessibilityHint("Öffnet den Mix")
                .accessibilityIdentifier("home-quick-mix-" + mix.id)
                .contextMenu { PinToStartButton(model: model, pin: .mix(mix)) }
        case .genre(let genre):
            Button { Task { await model.playMix(genre, genre: genre) } } label: {
                QuickTile(title: genre) { QuickSymbol(name: "waveform") }
            }.buttonStyle(ListenPressStyle()).disabled(model.listeningBusy)
                .accessibilityLabel("\(genre)-Mix abspielen")
                .accessibilityIdentifier("home-quick-genre-" + genre)
        case .playlist(let playlist):
            NavigationLink(value: CollectionKind.playlist(playlist)) {
                QuickTile(title: playlist.name) { playlistCover(playlist.id) }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Playlist: \(playlist.name)").accessibilityHint("Öffnet die Playlist")
                .accessibilityIdentifier("home-quick-playlist-" + playlist.id)
                .contextMenu { PinToStartButton(model: model, pin: .playlist(playlist)) }
        }
    }
    @ViewBuilder private func mixCover(_ mix: PersonalMix) -> some View {
        if let cover = mix.coverTrackIds.first { ArtworkView(model: model, kind: "tracks", id: cover, cornerRadius: 0) }
        else { QuickSymbol(name: mix.symbol) }
    }
    @ViewBuilder private func playlistCover(_ id: String) -> some View {
        if let cover = model.playlistPreviews[id]?.first { ArtworkView(model: model, kind: "tracks", id: cover.id, cornerRadius: 0) }
        else { QuickSymbol(name: "music.note.list") }
    }
    /// A pinned tile; it opens its item and is unpinned from its menu.
    @ViewBuilder private func pinTile(_ pin: HomePin) -> some View {
        Group {
            switch pin.kind {
            case .playlist:
                let playlist = model.playlists.first { $0.id == pin.itemID } ?? Playlist(id: pin.itemID, name: pin.title, trackCount: 0, durationMs: 0)
                NavigationLink(value: CollectionKind.playlist(playlist)) {
                    QuickTile(title: playlist.name) { playlistCover(playlist.id) }
                }.accessibilityLabel("Playlist: \(playlist.name)").accessibilityHint("Öffnet die Playlist")
            case .album:
                let artists = pin.subtitle.isEmpty ? [] : pin.subtitle.components(separatedBy: " · ")
                NavigationLink(value: CollectionKind.release(Release(id: pin.itemID, title: pin.title, artists: artists))) {
                    QuickTile(title: pin.title, subtitle: pin.subtitle.isEmpty ? nil : pin.subtitle) {
                        ArtworkView(model: model, kind: "releases", id: pin.itemID, cornerRadius: 0)
                    }
                }.accessibilityLabel("Album: \(pin.title)").accessibilityHint("Öffnet das Album")
            case .artist:
                NavigationLink(value: CollectionKind.artist(Artist(id: pin.itemID, name: pin.title))) {
                    QuickTile(title: pin.title) {
                        ArtworkView(model: model, kind: "artists", id: pin.itemID, cornerRadius: 0, monogram: pin.title)
                    }
                }.accessibilityLabel("Künstler: \(pin.title)").accessibilityHint("Öffnet den Künstler")
            case .mix:
                let mix = feed.mixes.first { $0.id == pin.itemID } ?? PersonalMix(id: pin.itemID, kind: "", title: pin.title, trackCount: 0)
                NavigationLink(value: mix) {
                    QuickTile(title: mix.title) { mixCover(mix) }
                }.accessibilityLabel("Mix: \(mix.title)").accessibilityHint("Öffnet den Mix")
            case .genre:
                NavigationLink(value: LibraryRoute.genre(pin.itemID)) {
                    QuickTile(title: pin.title) {
                        Image(systemName: "guitars.fill").font(.system(size: 20, weight: .semibold)).foregroundStyle(.white)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .background(GenreTile.color(for: pin.itemID).gradient).accessibilityHidden(true)
                    }
                }.accessibilityLabel("Genre: \(pin.title)").accessibilityHint("Öffnet das Genre mit seinem Radio")
            case .favorites:
                NavigationLink(value: CollectionKind.favorites) {
                    QuickTile(title: "Favoriten") { QuickSymbol(name: "heart.fill") }
                }.accessibilityLabel("Favoriten").accessibilityHint("Öffnet deine Lieblingstitel")
            }
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityIdentifier("home-pin-\(pin.kind.rawValue)-\(pin.itemID)")
        .contextMenu { PinToStartButton(model: model, pin: pin) }
    }

    // MARK: Shelves

    @ViewBuilder private var mixShelf: some View {
        switch feed.mixesPhase {
        case .loaded where !feed.mixes.isEmpty:
            ListenSection {
                ListenSectionHeader(title: "Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek") {
                    allLink(ForYouRoute(), label: "Alle Mixe", hint: "Öffnet „Für dich“")
                }
                if let message = feed.mixError {
                    Text(message).font(.callout).foregroundStyle(.red).padding(.horizontal, ListenMetrics.pageMargin)
                }
                ListenShelf {
                    ForEach(feed.mixes.prefix(10)) { mix in MixCard(model: model, mix: mix, width: cardWidth) }
                }
            }
        case .idle, .loading:
            ListenSection {
                ListenSectionHeader(title: "Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek")
                skeletonShelf
            }
        case .failed(let message) where !feed.unreachable:
            ListenSection {
                ListenSectionHeader(title: "Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek")
                ListenSectionFailure(message: message) { Task { await model.loadListenFeed() } }
                    .padding(.horizontal, ListenMetrics.pageMargin)
            }
        case .unsupported:
            // An older server without personal mixes: mixes made on the device.
            ListenSection {
                ListenSectionHeader(title: "Für deinen Moment", subtitle: "Ein Mix aus deiner Bibliothek")
                ListenShelf {
                    localMix("Favoriten", subtitle: "Deine Lieblingstitel", symbol: "heart.fill")
                    localMix("Neu", subtitle: "Frisch hinzugefügt", symbol: "sparkles")
                    ForEach(Array(localGenres.prefix(6)), id: \.self) { genre in
                        localMix(genre, subtitle: "Genre-Mix", symbol: "waveform", genre: genre)
                    }
                }
            }
        default: EmptyView()
        }
    }
    /// The account's history from the server (all devices); without it the
    /// local list of this device.
    @ViewBuilder private var historyShelf: some View {
        let server = feed.historyPhase == .loaded ? feed.history : []
        let local = model.listeningHistory.tracks
        if !server.isEmpty {
            ListenSection {
                ListenSectionHeader(title: "Zuletzt gehört", subtitle: "Auf allen deinen Geräten")
                ListenShelf {
                    ForEach(Array(server.prefix(12).enumerated()), id: \.offset) { index, entry in
                        HomeTrackCard(model: model, track: entry.track, width: cardWidth,
                                      caption: entry.lastPlayedAt.map { GermanDates.relative($0) }) {
                            play(server.map(\.track), start: index, context: QueueContext(kind: "history", title: "Zuletzt gehört"))
                        }
                    }
                }
            }
        } else if !local.isEmpty {
            ListenSection {
                ListenSectionHeader(title: "Zuletzt gehört", subtitle: "Auf diesem Gerät")
                ListenShelf {
                    ForEach(Array(local.prefix(12).enumerated()), id: \.offset) { index, track in
                        HomeTrackCard(model: model, track: track, width: cardWidth) {
                            play(local, start: index, context: QueueContext(kind: "history", title: "Zuletzt gehört"))
                        }
                    }
                }
            }
        } else if feed.historyPhase == .loading {
            ListenSection {
                ListenSectionHeader(title: "Zuletzt gehört", subtitle: "Auf allen deinen Geräten")
                skeletonShelf
            }
        } else if case .failed(let message) = feed.historyPhase, !feed.unreachable {
            ListenSection {
                ListenSectionHeader(title: "Zuletzt gehört", subtitle: "Auf allen deinen Geräten")
                ListenSectionFailure(message: message) { Task { await model.loadListenFeed() } }
                    .padding(.horizontal, ListenMetrics.pageMargin)
            }
        }
    }
    @ViewBuilder private var releaseShelf: some View {
        let releases = feed.releasesPhase == .loaded ? feed.newReleases : model.releases
        if !releases.isEmpty {
            ListenSection {
                ListenSectionHeader(title: "Neu in der Mediathek", subtitle: "Zuletzt hinzugefügte Alben") {
                    allLink(Destination.library, label: "Alle Alben", hint: "Öffnet die Bibliothek")
                }
                ListenShelf {
                    ForEach(Array(releases.prefix(12))) { release in HomeReleaseCard(model: model, release: release, width: cardWidth) }
                }
            }
        } else if feed.releasesPhase == .loading || !model.releasesLoaded {
            ListenSection {
                ListenSectionHeader(title: "Neu in der Mediathek", subtitle: "Zuletzt hinzugefügte Alben")
                skeletonShelf
            }
        } else if case .failed(let message) = feed.releasesPhase, !feed.unreachable {
            ListenSection {
                ListenSectionHeader(title: "Neu in der Mediathek", subtitle: "Zuletzt hinzugefügte Alben")
                ListenSectionFailure(message: message) { Task { await model.loadListenFeed() } }
                    .padding(.horizontal, ListenMetrics.pageMargin)
            }
        }
    }
    /// Optional: shown only when the server knows play counts.
    @ViewBuilder private var frequentShelf: some View {
        if feed.frequentPhase == .loaded && !feed.frequent.isEmpty {
            let entries = Array(feed.frequent.prefix(12))
            ListenSection {
                ListenSectionHeader(title: "Am meisten gehört", subtitle: "Deine meistgespielten Titel")
                ListenShelf {
                    ForEach(Array(entries.enumerated()), id: \.offset) { index, entry in
                        HomeTrackCard(model: model, track: entry.track, width: cardWidth, caption: "\(entry.playCount)× gehört") {
                            play(entries.map(\.track), start: index, context: QueueContext(kind: "history", id: "frequent", title: "Am meisten gehört"))
                        }
                    }
                }
            }
        }
    }
    /// "Wieder einsteigen": albums, playlists, mixes, artists and radios
    /// started on this device, newest first. Hidden until there is one.
    @ViewBuilder private var jumpBackInShelf: some View {
        let sources = model.recentSources.entries
        if !sources.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.jumpBackIn.title, subtitle: "Zuletzt gestartet auf diesem Gerät")
                ListenShelf {
                    ForEach(sources) { source in RecentSourceCard(model: model, source: source, width: cardWidth) }
                }
            }
        }
    }
    /// Optional: the mixes of "Entdecken" (and what newer servers add) as cards.
    @ViewBuilder private var discoverShelf: some View {
        let mixes = feed.mixesPhase == .loaded ? feed.mixes.filter { MixGroup.of($0) != .madeForYou } : []
        if !mixes.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.discover.title, subtitle: HomeShelf.discover.subtitle) {
                    allLink(ForYouRoute(), label: "Für dich", hint: "Öffnet „Für dich“")
                }
                ListenShelf {
                    ForEach(mixes) { mix in
                        MixCard(model: model, mix: mix, width: cardWidth).contextMenu { PinToStartButton(model: model, pin: .mix(mix)) }
                    }
                }
            }
        }
    }
    /// Optional: the artists of the most played titles; each opens the artist or starts its radio.
    @ViewBuilder private var topArtistShelf: some View {
        let top = feed.frequentPhase == .loaded ? TopArtist.rank(feed.frequent) : []
        if !top.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.topArtists.title, subtitle: "Aus deinen meistgespielten Titeln")
                ListenShelf {
                    ForEach(top) { artist in
                        TopArtistCard(model: model, artist: artist, width: textSize.isAccessibilitySize ? 150 : 112,
                                      open: { pushed = .artist($0) }, notFound: { missingArtist = $0 })
                    }
                }
            }
        }
    }
    /// Optional: the genres, each opening its albums with "Genre-Radio".
    @ViewBuilder private var genreShelf: some View {
        let genres = model.preferredGenres
        if !genres.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.genres.title, subtitle: HomeShelf.genres.subtitle) {
                    allLink(LibraryRoute.genres, label: "Alle Genres", hint: "Öffnet die Genres der Bibliothek")
                }
                ListenShelf {
                    ForEach(Array(genres.prefix(12)), id: \.self) { genre in
                        NavigationLink(value: LibraryRoute.genre(genre)) { GenreTile(name: genre, showsRadio: true).frame(width: cardWidth) }
                            .buttonStyle(ListenPressStyle())
                            .accessibilityLabel(genre).accessibilityHint("Öffnet das Genre mit seinem Radio")
                            .accessibilityIdentifier("home-genre-" + genre)
                            .contextMenu {
                                Button("Genre-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startGenreRadio(genre) } }
                                    .disabled(model.offlineMode || model.listeningBusy)
                                PinToStartButton(model: model, pin: .genre(genre))
                            }
                    }
                }
            }
        }
    }
    /// Optional: the first favorites; tapping plays them in this order.
    @ViewBuilder private var favoritesShelf: some View {
        if !favoriteTracks.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.favorites.title, subtitle: HomeShelf.favorites.subtitle) {
                    allLink(CollectionKind.favorites, label: "Alle Lieblingstitel", hint: "Öffnet deine Lieblingstitel")
                }
                ListenShelf {
                    ForEach(Array(favoriteTracks.enumerated()), id: \.offset) { index, track in
                        HomeTrackCard(model: model, track: track, width: cardWidth) {
                            play(favoriteTracks, start: index, context: QueueContext(kind: "favorites", title: "Lieblingstitel"))
                        }
                    }
                }
            }
        }
    }
    /// Optional: the playlists with their cover collages.
    @ViewBuilder private var playlistShelf: some View {
        if !model.playlists.isEmpty {
            ListenSection {
                ListenSectionHeader(title: HomeShelf.playlists.title, subtitle: HomeShelf.playlists.subtitle) {
                    allLink(Destination.playlists, label: "Alle Playlists", hint: "Öffnet die Playlists")
                }
                ListenShelf {
                    ForEach(Array(model.playlists.prefix(12))) { playlist in
                        NavigationLink(value: CollectionKind.playlist(playlist)) {
                            VStack(alignment: .leading, spacing: 6) {
                                MobileCollectionArtwork(model: model, tracks: model.playlistPreviews[playlist.id] ?? [], size: cardWidth)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(playlist.name).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                                    Text(CollectionSummary.tracks(playlist.trackCount)).font(.caption).monospacedDigit().foregroundStyle(.secondary)
                                }.lineLimit(1)
                            }.frame(width: cardWidth, alignment: .leading).contentShape(Rectangle())
                        }
                        .buttonStyle(ListenPressStyle())
                        .accessibilityHint("Öffnet die Playlist")
                        .accessibilityIdentifier("home-playlist-" + playlist.id)
                        .contextMenu { PinToStartButton(model: model, pin: .playlist(playlist)) }
                    }
                }
            }
        }
    }
    private var skeletonShelf: some View {
        ListenShelf {
            ForEach(0..<4, id: \.self) { _ in CardSkeleton(width: cardWidth) }
        }.allowsHitTesting(false)
            .accessibilityElement(children: .ignore).accessibilityLabel("Wird geladen")
    }
    private func allLink<Value: Hashable>(_ value: Value, label: String, hint: String) -> some View {
        NavigationLink(value: value) { Text("Alle").font(.body.weight(.semibold)) }
            .frame(minWidth: 44, minHeight: 44)
            .accessibilityLabel(label).accessibilityHint(hint)
    }
    private func localMix(_ name: String, subtitle: String, symbol: String, genre: String = "") -> some View {
        Button { Task { await model.playMix(name, genre: genre) } } label: {
            VStack(alignment: .leading, spacing: 6) {
                QuickSymbol(name: symbol).frame(width: cardWidth, height: cardWidth)
                    .clipShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
                Text(name).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1)
            }.frame(width: cardWidth, alignment: .leading).contentShape(Rectangle())
        }.buttonStyle(ListenPressStyle()).disabled(model.listeningBusy)
            .accessibilityLabel("\(name) abspielen, \(subtitle)")
    }
    private func play(_ tracks: [Track], start: Int, context: QueueContext) {
        guard let client = model.client else { return }
        model.player.play(tracks, start: start, client: client, context: context)
    }
    private var offlineHint: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label("Keine Verbindung zum Server", systemImage: "wifi.slash").font(.headline)
            Text("Mixe und dein Hörverlauf erscheinen, sobald der Server wieder erreichbar ist. Gespeicherte Musik spielt weiter.")
                .font(.subheadline).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            HStack(spacing: 12) {
                NavigationLink(value: Destination.downloads) { Label("Gespeicherte Musik", systemImage: "arrow.down.circle") }
                    .buttonStyle(MobileActionStyle())
                Button { Task { await model.loadListenFeed() } } label: { Label("Erneut laden", systemImage: "arrow.clockwise") }
                    .buttonStyle(MobileActionStyle())
            }
        }.padding(16).background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            .accessibilityElement(children: .contain)
    }
}

// MARK: - Building blocks

/// A titled section of a listening page.
struct ListenSection<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        VStack(alignment: .leading, spacing: ListenMetrics.headerGap) { content }
    }
}

/// Section title with an optional subtitle and trailing action ("Alle").
struct ListenSectionHeader<Trailing: View>: View {
    var title: String
    var subtitle: String?
    @ViewBuilder var trailing: Trailing
    var body: some View {
        HStack(alignment: .lastTextBaseline, spacing: 8) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.title2.bold()).accessibilityAddTraits(.isHeader)
                if let subtitle { Text(subtitle).font(.subheadline).foregroundStyle(.secondary) }
            }.frame(maxWidth: .infinity, alignment: .leading)
            trailing
        }.padding(.horizontal, ListenMetrics.pageMargin)
    }
}
extension ListenSectionHeader where Trailing == EmptyView {
    init(title: String, subtitle: String? = nil) { self.init(title: title, subtitle: subtitle) { EmptyView() } }
}

/// A horizontal shelf: scrolls edge to edge, the first card aligns with the
/// page margin, and scrolling settles on a card.
struct ListenShelf<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        ScrollView(.horizontal) {
            LazyHStack(alignment: .top, spacing: ListenMetrics.shelfGap) { content }.scrollTargetLayout()
        }
        .contentMargins(.horizontal, ListenMetrics.pageMargin, for: .scrollContent)
        .scrollTargetBehavior(.viewAligned)
        .scrollIndicators(.hidden)
    }
}

/// One quick-access entry: cover left, one-line title (two at large text sizes).
private struct QuickTile<Artwork: View>: View {
    var title: String
    var subtitle: String? = nil
    @ViewBuilder var artwork: Artwork
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        HStack(spacing: 10) {
            artwork.frame(width: ListenMetrics.tileHeight, height: ListenMetrics.tileHeight).clipped()
            // Two lines for a long name rather than an ellipsis mid-word; with
            // a subtitle both keep one line so the tile height stays 56 pt.
            VStack(alignment: .leading, spacing: 1) {
                Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                    .lineLimit(textSize.isAccessibilitySize ? 3 : subtitle == nil ? 2 : 1)
                if let subtitle { Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(textSize.isAccessibilitySize ? 2 : 1) }
            }.frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.trailing, 8)
        .frame(maxWidth: .infinity, minHeight: ListenMetrics.tileHeight, alignment: .leading)
        .background(ListenMetrics.surface)
        .clipShape(RoundedRectangle(cornerRadius: ListenMetrics.tileRadius, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.tileRadius, style: .continuous))
    }
}

/// A symbol on the accent color where an entry has no cover.
struct QuickSymbol: View {
    var name: String
    var body: some View {
        Image(systemName: name).font(.system(size: 20, weight: .semibold)).foregroundStyle(.white)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Color.accentColor.gradient)
            .accessibilityHidden(true)
    }
}

/// A title in a shelf: tapping plays it in the shelf's order; long press
/// offers the queue and favorite actions of a track row.
struct HomeTrackCard: View {
    var model: AppModel
    var track: Track
    var width: CGFloat
    var caption: String? = nil
    var play: () -> Void
    var body: some View {
        Button(action: play) {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: ListenMetrics.cardRadius).frame(width: width, height: width)
                VStack(alignment: .leading, spacing: 2) {
                    Text(track.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                    Text(track.artistText).font(.caption).foregroundStyle(.secondary)
                    if let caption { Text(caption).font(.caption).monospacedDigit().foregroundStyle(.secondary) }
                }.lineLimit(1)
            }.frame(width: width, alignment: .leading).contentShape(Rectangle())
        // The button reads its texts as one label ("Title, Artist, when").
        }.buttonStyle(ListenPressStyle())
            .accessibilityHint("Spielt den Titel")
            .contextMenu { TrackQuickActions(model: model, track: track) }
    }
}

/// The queue, radio and favorite actions of a title, for long-press menus.
struct TrackQuickActions: View {
    var model: AppModel
    var track: Track
    var body: some View {
        // The labels and order of the track row's "…" menu.
        Button("Als Nächstes spielen", systemImage: "text.insert") { model.enqueue([track], next: true) }
            .disabled(model.player.queue.tracks.count >= PlaybackQueue.limit)
        Button("Zur Warteschlange", systemImage: "text.badge.plus") { model.enqueue([track], next: false) }
            .disabled(model.player.queue.tracks.count >= PlaybackQueue.limit)
        Button("Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
            .disabled(model.offlineMode || model.listeningBusy)
        let favorite = model.favoriteIDs.contains(track.id)
        Button(favorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen", systemImage: favorite ? "heart.slash" : "heart") { Task { await model.toggleFavorite(track) } }
            .disabled(model.offlineMode || model.pendingFavorites.contains(track.id))
    }
}

/// An album in a shelf; opens the album.
struct HomeReleaseCard: View {
    var model: AppModel
    var release: Release
    var width: CGFloat
    var body: some View {
        NavigationLink(value: CollectionKind.release(release)) {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: ListenMetrics.cardRadius).frame(width: width, height: width)
                VStack(alignment: .leading, spacing: 2) {
                    Text(release.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                    Text(release.artists.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary)
                }.lineLimit(1)
            }.frame(width: width, alignment: .leading).contentShape(Rectangle())
        // The button reads its texts as one label ("Title, Artist").
        }.buttonStyle(ListenPressStyle())
            .accessibilityHint("Öffnet das Album")
    }
}

/// The final size of a card while its shelf loads.
struct CardSkeleton: View {
    var width: CGFloat
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous).fill(ListenMetrics.surface).frame(width: width, height: width)
            Capsule().fill(ListenMetrics.surface).frame(width: width * 0.8, height: 11)
            Capsule().fill(ListenMetrics.surface).frame(width: width * 0.5, height: 9)
        }.frame(width: width, alignment: .leading).accessibilityHidden(true)
    }
}

#endif
