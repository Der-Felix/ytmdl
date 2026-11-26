import SwiftUI
import YTMDLCore

#if os(iOS)

// "Für dich", the Start editor and the building blocks of radio, pins and
// "Wieder einsteigen" on iPhone and iPad. Siblings of Start (`HomeViews`):
// same metrics, shelves, cards and press feedback.

// MARK: - Model helpers

extension HomePin {
    static func playlist(_ playlist: Playlist) -> HomePin { HomePin(kind: .playlist, itemID: playlist.id, title: playlist.name) }
    static func album(_ release: Release) -> HomePin {
        HomePin(kind: .album, itemID: release.id, title: release.title, subtitle: release.artists.joined(separator: " · "))
    }
    static func artist(_ artist: Artist) -> HomePin { HomePin(kind: .artist, itemID: artist.id, title: artist.name) }
    static func mix(_ mix: PersonalMix) -> HomePin { HomePin(kind: .mix, itemID: mix.id, title: mix.title, subtitle: mix.label) }
    static func genre(_ genre: String) -> HomePin { HomePin(kind: .genre, itemID: genre, title: genre) }
    static var favorites: HomePin { HomePin(kind: .favorites, itemID: "favorites", title: "Lieblingstitel") }
}

extension AppModel {
    /// The library artist with this name: from the loaded list, else from a search.
    func resolveArtist(named name: String) async -> Artist? {
        if let index = LibraryMatch.index(of: name, in: artists.map(\.name)) { return artists[index] }
        guard let results = try? await search(name) else { return nil }
        return LibraryMatch.index(of: name, in: results.artists.map(\.name)).map { results.artists[$0] }
    }
    /// The first favorites for Start's "Favoriten" shelf; empty on failure.
    func favoriteShelfTracks() async -> [Track] {
        guard !offlineMode, let client else { return [] }
        let tracks = (try? await client.get("/library/tracks", query: [.init(name: "favorite", value: "true"), .init(name: "limit", value: "20")]) as [Track]) ?? []
        return self.client === client ? tracks : []
    }
    /// Starts a remembered radio again ("Wieder einsteigen").
    /// The saved context keeps the seed's ID only, so the new queue starts
    /// with the radio's titles rather than the seed itself.
    func restartRadio(_ station: RadioStation) async {
        switch station.seed {
        case .artist(let id, let name) where station.needsSeedTrack: await startArtistRadio(Artist(id: id, name: name))
        case .album(let id, let title) where station.needsSeedTrack: await startAlbumRadio(Release(id: id, title: title, artists: []))
        default: await startRadio(station, seed: nil)
        }
    }
    /// Genres for "Deine Genres": those of the genre mixes first, then the library's.
    var preferredGenres: [String] {
        var seen = Set<String>()
        let fromMixes = feed.mixes.compactMap { $0.isGenreMix ? $0.genre : nil }
        return (fromMixes + genres).filter { !$0.isEmpty && $0 != "__none__" && seen.insert(LibraryMatch.normalized($0)).inserted }
    }
}

// MARK: - Menu items

/// "An Start anheften" / "Von Start lösen" for menus and context menus.
struct PinToStartButton: View {
    var model: AppModel
    var pin: HomePin
    var body: some View {
        let pinned = model.homeLayout.isPinned(pin.kind, pin.itemID)
        Button(pinned ? "Von Start lösen" : "An Start anheften", systemImage: pinned ? "pin.slash" : "pin") { model.togglePin(pin) }
    }
}

// MARK: - Shared cards

/// A genre to browse: a deep color derived from its name (stable across
/// launches), white text with at least 4.5:1 contrast.
struct GenreTile: View {
    var name: String
    var showsRadio = false
    var body: some View {
        HStack(alignment: .bottom) {
            Text(name).font(.headline).foregroundStyle(.white).lineLimit(2).multilineTextAlignment(.leading)
            Spacer(minLength: 4)
            if showsRadio {
                Image(systemName: "dot.radiowaves.left.and.right").font(.subheadline.weight(.semibold)).foregroundStyle(.white.opacity(0.85))
                    .accessibilityHidden(true)
            }
        }
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

/// "Weiterhören": Start's one large element while something waits to go on,
/// a queue that is loaded but paused, or the last one of this account.
struct ResumeCard: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    private struct Resume {
        let track: Track
        let context: QueueContext?
        let progress: Double
        let loaded: Bool
    }
    private var state: Resume? {
        let player = model.player
        if let track = player.current {
            guard !player.isPlaybackRequested else { return nil }
            let duration = player.duration
            return Resume(track: track, context: player.queue.context, progress: duration > 0 ? min(1, player.position / duration) : 0, loaded: true)
        }
        guard let snapshot = model.listeningHistory.snapshot, snapshot.tracks.indices.contains(snapshot.index) else { return nil }
        let track = snapshot.tracks[snapshot.index]
        return Resume(track: track, context: snapshot.context, progress: track.duration > 0 ? min(1, snapshot.position / track.duration) : 0, loaded: false)
    }
    private func source(_ context: QueueContext?) -> String? {
        guard let context else { return nil }
        return context.kind == "radio" ? context.title : "Aus: \(context.title)"
    }
    var body: some View {
        if let state {
            Button {
                if state.loaded { model.player.toggle() } else { model.resumeLastSession() }
            } label: {
                let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(spacing: 14))
                layout {
                    ArtworkView(model: model, kind: "tracks", id: state.track.id, cornerRadius: 10).frame(width: 72, height: 72)
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Weiterhören").font(.caption.weight(.semibold)).foregroundStyle(.tint).textCase(.uppercase)
                        Text(state.track.title).font(.headline).foregroundStyle(.primary).lineLimit(textSize.isAccessibilitySize ? 3 : 1)
                        Text(state.track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        if let from = source(state.context) {
                            Label(from, systemImage: state.context?.kind == "radio" ? "dot.radiowaves.left.and.right" : "play.square.stack")
                                .font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "play.fill").font(.system(size: 20, weight: .semibold)).foregroundStyle(.white)
                        .frame(width: 52, height: 52).background(Color.accentColor, in: Circle())
                        .accessibilityHidden(true)
                }
                .padding(14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(ListenMetrics.surface)
                .overlay(alignment: .bottom) {
                    // How far the title got; decoration, the label says what it is.
                    GeometryReader { proxy in
                        Capsule().fill(Color.accentColor).frame(width: max(0, proxy.size.width * state.progress), height: 3)
                    }.frame(height: 3).accessibilityHidden(true)
                }
                .clipShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
                .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            }
            .buttonStyle(ListenPressStyle())
            .accessibilityLabel("Weiterhören: \(state.track.title), \(state.track.artistText)" + (source(state.context).map { ", \($0)" } ?? ""))
            .accessibilityHint(state.loaded ? "Setzt die Wiedergabe fort" : "Setzt die letzte Wiedergabe fort")
            .accessibilityIdentifier("home-resume")
        }
    }
}

/// One entry of "Wieder einsteigen": an album, playlist, mix, artist, the
/// favorites or a radio the listener started; it opens again (a radio starts again).
struct RecentSourceCard: View {
    var model: AppModel
    var source: RecentSource
    var width: CGFloat
    var body: some View {
        Group {
            if source.kind == "radio", let station = RadioStation(context: source.context) {
                Button { Task { await model.restartRadio(station) } } label: { card }
                    .disabled(model.listeningBusy || model.offlineMode)
                    .accessibilityHint("Startet das Radio neu")
            } else if let destination {
                NavigationLink(value: destination) { card }
                    .accessibilityHint("Öffnet „\(source.title)“")
            } else if source.kind == "mix" {
                NavigationLink(value: mix) { card }.accessibilityHint("Öffnet den Mix")
            }
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityIdentifier("home-recent-" + source.id)
        .contextMenu {
            pinButton
            Button("Aus „Wieder einsteigen“ entfernen", systemImage: "minus.circle") { model.forgetSource(source.id) }
        }
    }
    private var mix: PersonalMix {
        model.feed.mixes.first { $0.id == source.itemID } ?? PersonalMix(id: source.itemID, kind: "", title: source.title, trackCount: 0)
    }
    private var destination: CollectionKind? {
        switch source.kind {
        case "album": .release(Release(id: source.itemID, title: source.title, artists: []))
        case "artist": .artist(Artist(id: source.itemID, name: source.title))
        case "playlist": .playlist(model.playlists.first { $0.id == source.itemID } ?? Playlist(id: source.itemID, name: source.title, trackCount: 0, durationMs: 0))
        case "favorites": .favorites
        default: nil
        }
    }
    @ViewBuilder private var pinButton: some View {
        switch source.kind {
        case "album": PinToStartButton(model: model, pin: HomePin(kind: .album, itemID: source.itemID, title: source.title))
        case "artist": PinToStartButton(model: model, pin: HomePin(kind: .artist, itemID: source.itemID, title: source.title))
        case "playlist": PinToStartButton(model: model, pin: HomePin(kind: .playlist, itemID: source.itemID, title: source.title))
        case "mix": PinToStartButton(model: model, pin: .mix(mix))
        case "favorites": PinToStartButton(model: model, pin: .favorites)
        default: EmptyView()
        }
    }
    private var card: some View {
        VStack(alignment: .leading, spacing: 6) {
            artwork.frame(width: width, height: width)
                .clipShape(source.kind == "artist" ? AnyShape(Circle()) : AnyShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous)))
            VStack(alignment: .leading, spacing: 2) {
                Text(source.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                Text(source.label).font(.caption).foregroundStyle(.secondary)
            }.lineLimit(1)
        }.frame(width: width, alignment: .leading).contentShape(Rectangle())
    }
    @ViewBuilder private var artwork: some View {
        switch source.kind {
        case "album": ArtworkView(model: model, kind: "releases", id: source.itemID, cornerRadius: 0)
        case "artist": ArtworkView(model: model, kind: "artists", id: source.itemID, cornerRadius: 0, monogram: source.title)
        case "playlist": MobileCollectionArtwork(model: model, tracks: model.playlistPreviews[source.itemID] ?? [], size: width)
        case "mix":
            if mix.coverTrackIds.isEmpty { QuickSymbol(name: "sparkles") } else { MixArtwork(model: model, mix: mix, size: width) }
        case "favorites": QuickSymbol(name: "heart.fill")
        default:
            ZStack {
                GenreTile.color(for: source.title).gradient
                Image(systemName: "dot.radiowaves.left.and.right").font(.system(size: width * 0.24, weight: .semibold)).foregroundStyle(.white)
            }.accessibilityHidden(true)
        }
    }
}

/// An artist of "Deine Top-Künstler": the round picture opens the artist,
/// the button under it starts the artist's radio.
struct TopArtistCard: View {
    var model: AppModel
    var artist: TopArtist
    var width: CGFloat
    var open: (Artist) -> Void
    var notFound: (String) -> Void
    @State private var resolving = false
    private var known: Artist? { LibraryMatch.index(of: artist.name, in: model.artists.map(\.name)).map { model.artists[$0] } }
    var body: some View {
        VStack(spacing: 8) {
            Button { resolve(open) } label: {
                VStack(spacing: 8) {
                    Group {
                        if let known { ArtistAvatar(model: model, artist: known) }
                        else { MonogramView(name: artist.name).clipShape(Circle()) }
                    }
                    .frame(width: width, height: width)
                    .overlay { if resolving { ProgressView().controlSize(.large) } }
                    VStack(spacing: 2) {
                        Text(artist.name).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                        Text("\(artist.plays)× gehört").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                    }
                }.frame(width: width).contentShape(Rectangle())
            }
            .buttonStyle(ListenPressStyle())
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(artist.name), \(artist.plays)-mal gehört")
            .accessibilityHint("Öffnet den Künstler")
            .accessibilityIdentifier("foryou-artist-" + artist.name)
            Button {
                resolve { found in Task { await model.startArtistRadio(found) } }
            } label: {
                Label("Radio", systemImage: "dot.radiowaves.left.and.right").font(.footnote.weight(.semibold))
                    .padding(.horizontal, 14).frame(minHeight: 36)
                    .background(.tint.opacity(0.14), in: Capsule())
            }
            .buttonStyle(ListenPressStyle()).foregroundStyle(.tint)
            .frame(minHeight: 44)
            .disabled(model.listeningBusy || model.offlineMode || resolving)
            .accessibilityLabel("Künstler-Radio: \(artist.name)")
            .accessibilityIdentifier("foryou-artist-radio-" + artist.name)
        }
        .frame(width: width)
    }
    private func resolve(_ then: @escaping (Artist) -> Void) {
        if let known { then(known); return }
        resolving = true
        Task {
            let found = await model.resolveArtist(named: artist.name)
            resolving = false
            if let found { then(found) } else { notFound(artist.name) }
        }
    }
}

/// The first mix of "Für dich": the hub's one large element.
private struct FeaturedMixCard: View {
    var model: AppModel
    var mix: PersonalMix
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.horizontalSizeClass) private var sizeClass
    var body: some View {
        let stacked = textSize.isAccessibilitySize
        let cover: CGFloat = sizeClass == .regular ? 180 : 120
        let layout = stacked ? AnyLayout(VStackLayout(alignment: .leading, spacing: 14)) : AnyLayout(HStackLayout(alignment: .center, spacing: 16))
        layout {
            NavigationLink(value: mix) {
                let inner = stacked ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(alignment: .center, spacing: 16))
                inner {
                    MixArtwork(model: model, mix: mix, size: cover)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(mix.label).font(.caption.weight(.semibold)).foregroundStyle(.tint).textCase(.uppercase).lineLimit(1)
                        Text(mix.title).font(.title2.bold()).foregroundStyle(.primary).lineLimit(2)
                        if !mix.description.isEmpty {
                            Text(mix.description).font(.subheadline).foregroundStyle(.secondary).lineLimit(stacked ? 4 : 3)
                        }
                        Text("\(mix.trackCount) Titel").font(.caption).monospacedDigit().foregroundStyle(.secondary).padding(.top, 2)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.contentShape(Rectangle())
            }
            .buttonStyle(ListenPressStyle())
            .accessibilityElement(children: .combine)
            .accessibilityLabel("\(mix.title), \(mix.label), \(mix.trackCount) Titel")
            .accessibilityHint("Öffnet den Mix")
            .accessibilityIdentifier("foryou-featured-" + mix.id)
            .contextMenu { PinToStartButton(model: model, pin: .mix(mix)) }
            Button { Task { await model.playPersonalMix(mix) } } label: {
                Group {
                    if model.feed.playingMixID == mix.id { ProgressView().tint(.white) }
                    else { Image(systemName: "play.fill").font(.system(size: 22, weight: .semibold)) }
                }
                .foregroundStyle(.white).frame(width: 56, height: 56).background(Color.accentColor, in: Circle()).contentShape(Circle())
            }
            .buttonStyle(ListenPressStyle())
            .disabled(model.feed.playingMixID != nil)
            .accessibilityLabel("\(mix.title) abspielen")
            .accessibilityIdentifier("foryou-featured-play")
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
    }
}

// MARK: - "Für dich"

/// "Für dich" on iPhone and iPad: the day's mixes by section ("Für dich
/// gemacht", "Entdecken" and what newer servers add), then "Deine Genres"
/// and "Deine Top-Künstler". The first mix is the page's one large element.
struct ForYouHubView: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var pushed: CollectionKind?
    @State private var missingArtist: String?
    @State private var allGenres = false
    private var feed: ListenFeed { model.feed }
    private var cardWidth: CGFloat { textSize.isAccessibilitySize ? ListenMetrics.largeCardWidth : ListenMetrics.cardWidth }
    private var artistWidth: CGFloat { textSize.isAccessibilitySize ? 150 : 112 }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
                Text("Täglich neu gemischt aus deiner Mediathek und dem, was du hörst · \(GermanDates.day(.now))")
                    .font(.subheadline).foregroundStyle(.secondary)
                    .padding(.horizontal, ListenMetrics.pageMargin)
                    .padding(.bottom, ListenMetrics.headerGap - ListenMetrics.sectionSpacing)
                if let message = feed.mixError {
                    Text(message).font(.callout).foregroundStyle(.red).padding(.horizontal, ListenMetrics.pageMargin)
                }
                mixSections
                genreSection
                artistSection
            }
            .padding(.top, 4).padding(.bottom, 32)
            .frame(maxWidth: 1000, alignment: .leading).frame(maxWidth: .infinity)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: [feed.mixesPhase, feed.frequentPhase])
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .navigationTitle("Für dich")
        .navigationBarTitleDisplayMode(.large)
        .refreshable { await model.loadListenFeed() }
        .task { if feed.mixesPhase == .idle || feed.frequentPhase == .idle { await model.loadListenFeed() } }
        .navigationDestination(item: $pushed) { CollectionView(model: model, kind: $0) }
        .alert("Nicht gefunden", isPresented: Binding(get: { missingArtist != nil }, set: { if !$0 { missingArtist = nil } })) {
            Button("OK") { missingArtist = nil }
        } message: { Text("„\(missingArtist ?? "")“ ist in deiner Bibliothek nicht als Künstler zu finden.") }
    }

    @ViewBuilder private var mixSections: some View {
        switch feed.mixesPhase {
        case .idle, .loading:
            ListenSection {
                ListenSectionHeader(title: MixGroup.madeForYou.title, subtitle: MixGroup.madeForYou.subtitle)
                ListenShelf { ForEach(0..<4, id: \.self) { _ in CardSkeleton(width: cardWidth) } }
                    .allowsHitTesting(false).accessibilityElement(children: .ignore).accessibilityLabel("Wird geladen")
            }
        case .failed(let message):
            ListenSectionFailure(message: message) { Task { await model.loadListenFeed() } }
                .padding(.horizontal, ListenMetrics.pageMargin)
        case .unsupported:
            ContentUnavailableView("Mixe nicht verfügbar", systemImage: "sparkles",
                                   description: Text("Dein Server bietet noch keine persönlichen Mixe an. Sie gibt es ab YTMDL 1.10. Genres und Radios funktionieren trotzdem."))
        case .loaded:
            let sections = MixGroup.sections(feed.mixes)
            if sections.isEmpty {
                ContentUnavailableView("Noch keine Mixe", systemImage: "sparkles",
                                       description: Text("Mixe entstehen aus der Musik in deiner Mediathek und aus dem, was du hörst. Mit mehr Musik und Hörverlauf erscheinen sie hier und wechseln täglich."))
            }
            let featured = sections.first?.mixes.first
            if let featured {
                FeaturedMixCard(model: model, mix: featured).padding(.horizontal, ListenMetrics.pageMargin)
            }
            ForEach(sections) { section in
                let mixes = section.mixes.filter { $0.id != featured?.id }
                if !mixes.isEmpty {
                    ListenSection {
                        ListenSectionHeader(title: section.group.title, subtitle: section.group.subtitle)
                        ListenShelf {
                            ForEach(mixes) { mix in
                                MixCard(model: model, mix: mix, width: cardWidth)
                                    .contextMenu { PinToStartButton(model: model, pin: .mix(mix)) }
                            }
                        }
                    }
                }
            }
        }
    }

    @ViewBuilder private var genreSection: some View {
        let genres = model.preferredGenres
        if !genres.isEmpty {
            let limit = sizeClass == .regular ? 8 : 6
            ListenSection {
                ListenSectionHeader(title: "Deine Genres", subtitle: "Alben und Radio nach Genre") {
                    if genres.count > limit {
                        Button(allGenres ? "Weniger" : "Alle") { allGenres.toggle() }
                            .font(.body.weight(.semibold)).frame(minWidth: 44, minHeight: 44)
                            .accessibilityLabel(allGenres ? "Weniger Genres" : "Alle \(genres.count) Genres")
                    }
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: textSize.isAccessibilitySize ? 280 : 150), spacing: ListenMetrics.tileGap)],
                          spacing: ListenMetrics.tileGap) {
                    ForEach(allGenres ? genres : Array(genres.prefix(limit)), id: \.self) { genre in
                        NavigationLink(value: LibraryRoute.genre(genre)) { GenreTile(name: genre, showsRadio: true) }
                            .buttonStyle(ListenPressStyle())
                            .accessibilityLabel(genre).accessibilityHint("Öffnet das Genre mit seinem Radio")
                            .accessibilityIdentifier("foryou-genre-" + genre)
                            .contextMenu {
                                Button("Genre-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startGenreRadio(genre) } }
                                    .disabled(model.offlineMode || model.listeningBusy)
                                PinToStartButton(model: model, pin: .genre(genre))
                            }
                    }
                }.padding(.horizontal, ListenMetrics.pageMargin)
            }
        }
    }

    @ViewBuilder private var artistSection: some View {
        let top = feed.frequentPhase == .loaded ? TopArtist.rank(feed.frequent) : []
        if !top.isEmpty {
            ListenSection {
                ListenSectionHeader(title: "Deine Top-Künstler", subtitle: "Aus deinen meistgespielten Titeln")
                ListenShelf {
                    ForEach(top) { artist in
                        TopArtistCard(model: model, artist: artist, width: artistWidth, open: { pushed = .artist($0) }, notFound: { missingArtist = $0 })
                    }
                }
            }
        }
    }
}

// MARK: - "Start anpassen"

/// Shows, hides and orders Start's shelves and its pinned tiles, like the
/// system's own edit lists: drag handles, minus to hide, plus to show.
/// Changes apply at once and are saved for this account on this server.
struct HomeEditorView: View {
    var model: AppModel
    @State private var confirmingReset = false
    private var layout: HomeLayout { model.homeLayout }
    private var hidden: [HomeShelf] { layout.shelves.filter { !layout.isVisible($0) } }

    var body: some View {
        List {
            Section {
                ForEach(layout.visibleShelves) { shelf in row(shelf, shown: true) }
                    .onMove { source, destination in model.updateHomeLayout { $0.moveVisible(fromOffsets: source, toOffset: destination) } }
            } header: {
                Text("Auf Start")
            } footer: {
                Text("Halte einen Bereich am Griff und ziehe ihn an seinen Platz.")
            }
            if !hidden.isEmpty {
                Section("Weitere Bereiche") {
                    ForEach(hidden) { shelf in row(shelf, shown: false) }
                }
            }
            Section {
                if layout.pins.isEmpty {
                    Text("Noch nichts angeheftet. Start zeigt dann Favoriten, „Für dich“, deine Mixe und Playlists.")
                        .font(.subheadline).foregroundStyle(.secondary)
                }
                ForEach(layout.pins) { pin in pinRow(pin) }
                    .onMove { source, destination in model.updateHomeLayout { $0.movePins(fromOffsets: source, toOffset: destination) } }
            } header: {
                Text("Schnellzugriff (\(layout.pins.count) von \(HomeLayout.pinLimit))")
            } footer: {
                Text("Hefte Playlists, Alben, Künstler, Mixe und Genres über „An Start anheften“ in ihrem Menü an. Angeheftetes steht vorn, freie Plätze füllt Start selbst.")
            }
            Section {
                Button("Start zurücksetzen", systemImage: "arrow.counterclockwise", role: .destructive) { confirmingReset = true }
                    .disabled(layout.isStandard)
                    .accessibilityIdentifier("home-editor-reset")
            } footer: {
                Text("Stellt die ursprünglichen Bereiche und ihre Reihenfolge wieder her und löst alle angehefteten Kacheln.")
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.editMode, .constant(.active))
        .modifier(ReadableContentWidth(maxWidth: 720))
        .navigationTitle("Start anpassen")
        .navigationBarTitleDisplayMode(.inline)
        .confirmationDialog("Start zurücksetzen?", isPresented: $confirmingReset, titleVisibility: .visible) {
            Button("Zurücksetzen", role: .destructive) {
                model.updateHomeLayout { $0.reset() }
                model.announce("Start zurückgesetzt", symbol: "arrow.counterclockwise")
            }
        } message: { Text("Alle Bereiche erscheinen wieder in ihrer ursprünglichen Reihenfolge, angeheftete Kacheln werden gelöst.") }
    }

    private func row(_ shelf: HomeShelf, shown: Bool) -> some View {
        HStack(spacing: 12) {
            Button {
                model.updateHomeLayout { $0.setVisible(shelf, !shown) }
            } label: {
                Image(systemName: shown ? "minus.circle.fill" : "plus.circle.fill")
                    .font(.title3).symbolRenderingMode(.palette)
                    .foregroundStyle(.white, shown ? Color.red : Color.green)
                    .frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel(shown ? "\(shelf.title) ausblenden" : "\(shelf.title) einblenden")
            .accessibilityIdentifier("home-editor-toggle-" + shelf.rawValue)
            Image(systemName: shelf.symbol).font(.body.weight(.semibold)).foregroundStyle(.tint).frame(width: 28).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(shelf.title).font(.body)
                Text(shelf.subtitle).font(.footnote).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("home-editor-row-" + shelf.rawValue)
        }
        .padding(.vertical, 2)
        .accessibilityActions {
            if shown {
                Button("Nach oben") { model.updateHomeLayout { $0.moveVisible(shelf, by: -1) } }
                Button("Nach unten") { model.updateHomeLayout { $0.moveVisible(shelf, by: 1) } }
            }
        }
    }

    private func pinRow(_ pin: HomePin) -> some View {
        HStack(spacing: 12) {
            Button { model.updateHomeLayout { $0.unpin(pin.kind, pin.itemID) } } label: {
                Image(systemName: "minus.circle.fill").font(.title3).symbolRenderingMode(.palette).foregroundStyle(.white, Color.red)
                    .frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .buttonStyle(.borderless)
            .accessibilityLabel("\(pin.title) von Start lösen")
            .accessibilityIdentifier("home-editor-unpin-" + pin.id)
            Image(systemName: Self.symbol(pin.kind)).font(.body.weight(.semibold)).foregroundStyle(.tint).frame(width: 28).accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(pin.title).font(.body).lineLimit(2)
                Text(pin.subtitle.isEmpty ? Self.kindLabel(pin.kind) : "\(Self.kindLabel(pin.kind)) · \(pin.subtitle)")
                    .font(.footnote).foregroundStyle(.secondary).lineLimit(1)
            }.frame(maxWidth: .infinity, alignment: .leading)
        }
    }
    static func symbol(_ kind: HomePin.Kind) -> String {
        switch kind {
        case .playlist: "music.note.list"
        case .album: "square.stack"
        case .artist: "person"
        case .mix: "sparkles"
        case .genre: "guitars"
        case .favorites: "heart"
        }
    }
    static func kindLabel(_ kind: HomePin.Kind) -> String {
        switch kind {
        case .playlist: "Playlist"
        case .album: "Album"
        case .artist: "Künstler"
        case .mix: "Mix"
        case .genre: "Genre"
        case .favorites: "Lieblingstitel"
        }
    }
}

#endif
