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
    static let tileRadius: CGFloat = 8
    static let cardRadius: CGFloat = 12
    static let rowArtwork: CGFloat = 52
    static let pressDuration = 0.14
    static var surface: Color { Color(uiColor: .secondarySystemGroupedBackground) }
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

/// The start screen ("Start"): a greeting as the title, a grid of what you most
/// likely want right now, then shelves of mixes, recent titles, new albums and
/// most played titles. Server parts load and fail on their own and never raise
/// an alert; without a connection a hint points to the saved music.
struct MobileHomeView: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.horizontalSizeClass) private var sizeClass
    private var feed: ListenFeed { model.feed }
    private var cardWidth: CGFloat { textSize.isAccessibilitySize ? ListenMetrics.largeCardWidth : ListenMetrics.cardWidth }
    private var localGenres: [String] { model.genres.filter { $0 != "__none__" } }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
                if feed.unreachable { offlineHint.padding(.horizontal, ListenMetrics.pageMargin) }
                quickAccess.padding(.horizontal, ListenMetrics.pageMargin)
                mixShelf
                historyShelf
                releaseShelf
                frequentShelf
            }
            .padding(.top, 4).padding(.bottom, 32)
            .frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
        }
        .background(Color(uiColor: .systemGroupedBackground))
        // Content scrolling under the floating tab bar and mini player gets a
        // solid edge, so no caption reads half-covered behind them.
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .navigationTitle(HomeGreeting.text(hour: Calendar.current.component(.hour, from: .now)))
        .refreshable { await model.loadLibrary(); await model.loadListenFeed() }
        .task(id: model.playlistRevision) { await model.loadPlaylistPreviews(Array(model.playlists.prefix(6))) }
        .task(id: model.user?.id) { await model.loadListenFeed() }
    }

    // MARK: Quick access

    private enum QuickEntry: Identifiable {
        case resume(Track), favorites, mix(PersonalMix), genre(String), playlist(Playlist)
        var id: String {
            switch self {
            case .resume: "resume"
            case .favorites: "favorites"
            case .mix(let mix): "mix-" + mix.id
            case .genre(let genre): "genre-" + genre
            case .playlist(let playlist): "playlist-" + playlist.id
            }
        }
    }
    private var quickColumns: Int { textSize.isAccessibilitySize ? 1 : sizeClass == .regular ? 3 : 2 }
    /// Up to six entries: resume the saved queue (only when nothing is loaded,
    /// the mini player covers the rest), favorites, the first mixes, then
    /// playlists. Full rows only, so the grid never ends in a gap.
    private var quickEntries: [QuickEntry] {
        var entries: [QuickEntry] = []
        if model.player.current == nil, let snapshot = model.listeningHistory.snapshot, snapshot.tracks.indices.contains(snapshot.index) {
            entries.append(.resume(snapshot.tracks[snapshot.index]))
        }
        entries.append(.favorites)
        switch feed.mixesPhase {
        case .loaded: entries += feed.mixes.prefix(2).map(QuickEntry.mix)
        case .unsupported: entries += localGenres.prefix(2).map(QuickEntry.genre)
        default: break
        }
        entries += model.playlists.prefix(6).map(QuickEntry.playlist)
        entries = Array(entries.prefix(6))
        if entries.count > quickColumns { entries.removeLast(entries.count % quickColumns) }
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
        case .resume(let track):
            Button { model.resumeLastSession() } label: {
                QuickTile(title: "Weiterhören", subtitle: track.title) {
                    ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 0)
                }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Weiterhören: \(track.title), \(track.artistText)")
                .accessibilityHint("Setzt die letzte Wiedergabe fort")
                .accessibilityIdentifier("home-quick-resume")
        case .favorites:
            NavigationLink(value: CollectionKind.favorites) {
                QuickTile(title: "Favoriten") { QuickSymbol(name: "heart.fill") }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Favoriten").accessibilityHint("Öffnet deine Lieblingstitel")
                .accessibilityIdentifier("home-quick-favorites")
        case .mix(let mix):
            NavigationLink(value: mix) {
                QuickTile(title: mix.title) {
                    if let cover = mix.coverTrackIds.first { ArtworkView(model: model, kind: "tracks", id: cover, cornerRadius: 0) }
                    else { QuickSymbol(name: "sparkles") }
                }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Mix: \(mix.title)").accessibilityHint("Öffnet den Mix")
                .accessibilityIdentifier("home-quick-mix-" + mix.id)
        case .genre(let genre):
            Button { Task { await model.playMix(genre, genre: genre) } } label: {
                QuickTile(title: genre) { QuickSymbol(name: "waveform") }
            }.buttonStyle(ListenPressStyle()).disabled(model.listeningBusy)
                .accessibilityLabel("\(genre)-Mix abspielen")
                .accessibilityIdentifier("home-quick-genre-" + genre)
        case .playlist(let playlist):
            NavigationLink(value: CollectionKind.playlist(playlist)) {
                QuickTile(title: playlist.name) {
                    if let cover = model.playlistPreviews[playlist.id]?.first { ArtworkView(model: model, kind: "tracks", id: cover.id, cornerRadius: 0) }
                    else { QuickSymbol(name: "music.note.list") }
                }
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel("Playlist: \(playlist.name)").accessibilityHint("Öffnet die Playlist")
                .accessibilityIdentifier("home-quick-playlist-" + playlist.id)
        }
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
                                      caption: entry.lastPlayedAt.map { $0.formatted(.relative(presentation: .named)) }) {
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
                Button { Task { await model.loadListenFeed() } } label: { Label("Erneut versuchen", systemImage: "arrow.clockwise") }
                    .buttonStyle(MobileActionStyle())
            }
        }.padding(16).background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: 18))
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
        Button("Als Nächstes abspielen", systemImage: "text.insert") { if let client = model.client { model.player.insertNext(track, client: client) } }
            .disabled(model.player.queue.tracks.count >= PlaybackQueue.limit)
        Button("Zur Warteschlange", systemImage: "text.badge.plus") { if let client = model.client { model.player.append(track, client: client) } }
            .disabled(model.player.queue.tracks.count >= PlaybackQueue.limit)
        Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
            .disabled(model.offlineMode || model.listeningBusy)
        Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten", systemImage: "heart") { Task { await model.toggleFavorite(track) } }
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
