import Foundation
import Observation
import SwiftUI
import YTMDLCore

/// The server data of the listening start screen, as on the web's "Hören"
/// page: the day's personal mixes, the account's listening history and what
/// is new in the library. Each part loads and fails on its own.
@MainActor @Observable final class ListenFeed {
    enum Phase: Equatable {
        case idle, loading, loaded
        /// The server does not offer this part (an older server version).
        case unsupported
        case failed(String)
    }
    private(set) var mixes: [PersonalMix] = []
    private(set) var mixesPhase = Phase.idle
    private(set) var history: [HistoryTrack] = []
    private(set) var historyPhase = Phase.idle
    private(set) var newReleases: [Release] = []
    private(set) var releasesPhase = Phase.idle
    /// The account's most played titles ("Am meisten gehört"); optional, a
    /// failure only hides that shelf.
    private(set) var frequent: [HistoryTrack] = []
    private(set) var frequentPhase = Phase.idle
    /// The last load could not reach the server at all.
    private(set) var unreachable = false
    /// The mix whose tracks are being loaded to start playback.
    var playingMixID: String?
    var mixError: String?
    @ObservationIgnored private var revision = UUID()
    func clear() {
        revision = UUID(); mixes = []; history = []; newReleases = []; frequent = []; unreachable = false
        mixesPhase = .idle; historyPhase = .idle; releasesPhase = .idle; frequentPhase = .idle; playingMixID = nil; mixError = nil
    }
    fileprivate func begin() -> UUID {
        revision = UUID()
        if mixesPhase != .loaded { mixesPhase = .loading }
        if historyPhase != .loaded { historyPhase = .loading }
        if releasesPhase != .loaded { releasesPhase = .loading }
        if frequentPhase != .loaded { frequentPhase = .loading }
        return revision
    }
    fileprivate func apply(_ token: UUID, mixes: Result<[PersonalMix], any Error>, history: Result<[HistoryTrack], any Error>, releases: Result<[Release], any Error>,
                           frequent: Result<[HistoryTrack], any Error> = .success([])) {
        guard token == revision else { return }
        let failures = [mixes.failure, history.failure, releases.failure].compactMap { $0 }
        unreachable = !failures.isEmpty && failures.allSatisfy(Self.isUnreachable)
        switch mixes {
        case .success(let value): self.mixes = PersonalMix.ordered(value); mixesPhase = .loaded
        case .failure(let error): mixesPhase = Self.phase(for: error); if mixesPhase == .unsupported { self.mixes = [] }
        }
        switch history {
        case .success(let value): self.history = value; historyPhase = .loaded
        case .failure(let error): historyPhase = Self.phase(for: error); if historyPhase == .unsupported { self.history = [] }
        }
        switch releases {
        case .success(let value): newReleases = value; releasesPhase = .loaded
        case .failure(let error): releasesPhase = Self.phase(for: error)
        }
        switch frequent {
        case .success(let value): self.frequent = value; frequentPhase = .loaded
        case .failure(let error): frequentPhase = Self.phase(for: error); if frequentPhase == .unsupported { self.frequent = [] }
        }
    }
    static func isUnreachable(_ error: any Error) -> Bool { error is URLError }
    static func phase(for error: any Error) -> Phase {
        if case PlayerError.server(let status, _, _) = error, status == 404 || status == 405 { return .unsupported }
        if isUnreachable(error) { return .failed("Der Server ist gerade nicht erreichbar.") }
        if let error = error as? PlayerError { return .failed(error.localizedDescription) }
        return .failed("Konnte nicht geladen werden.")
    }
}

private extension Result {
    var failure: Failure? {
        if case .failure(let error) = self { return error }
        return nil
    }
}

/// "1 Std. 5 Min." / "42 Min.", like the mobile collection headers.
func listenDuration(_ milliseconds: Int) -> String {
    let minutes = max(0, milliseconds / 60_000)
    return minutes >= 60 ? "\(minutes / 60) Std. \(minutes % 60) Min." : "\(minutes) Min."
}

extension AppModel {
    /// Loads the start screen's server sections side by side. Errors stay in
    /// their section: no alert, and a failed part never hides the others.
    func loadListenFeed() async {
        guard !offlineMode, let client, user != nil else { return }
        let token = feed.begin()
        async let mixes: Result<[PersonalMix], any Error> = Self.fetch(client, "/library/mixes")
        async let history: Result<[HistoryTrack], any Error> = Self.fetch(client, "/history", [.init(name: "sort", value: "recent")])
        async let releases: Result<[Release], any Error> = Self.fetch(client, "/library/releases", [
            .init(name: "limit", value: "12"), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc"), .init(name: "in_library", value: "true")])
        async let frequent: Result<[HistoryTrack], any Error> = Self.fetch(client, "/history", [.init(name: "sort", value: "frequent")])
        let loaded = await (mixes, history, releases, frequent)
        guard self.client === client else { return }
        feed.apply(token, mixes: loaded.0, history: loaded.1, releases: loaded.2, frequent: loaded.3)
    }
    private static func fetch<T: Decodable & Sendable>(_ client: APIClient, _ path: String, _ query: [URLQueryItem] = []) async -> Result<T, any Error> {
        do { return .success(try await client.get(path, query: query)) } catch { return .failure(error) }
    }
    func loadPersonalMix(_ id: String) async throws -> PersonalMixDetail {
        guard let client, !offlineMode else { throw CancellationError() }
        let detail: PersonalMixDetail = try await client.get(try client.server.itemPath("mixes", id: id))
        guard self.client === client else { throw CancellationError() }
        return detail
    }
    /// Plays a mix from its card: loads its tracks, then hands them to the player.
    /// A late answer never replaces something the user started in the meantime.
    func playPersonalMix(_ mix: PersonalMix, shuffle: Bool = false) async {
        guard feed.playingMixID == nil, let client else { return }
        let playbackRevision = player.playbackRevision
        feed.playingMixID = mix.id; feed.mixError = nil
        defer { if self.client === client { feed.playingMixID = nil } }
        do {
            let detail = try await loadPersonalMix(mix.id)
            guard self.client === client, player.playbackRevision == playbackRevision else { return }
            guard !detail.tracks.isEmpty else { feed.mixError = "Die Titel dieses Mixes sind nicht mehr in der Mediathek."; return }
            player.play(shuffle ? detail.tracks.shuffled() : detail.tracks, client: client, context: detail.mix.queueContext)
        } catch is CancellationError {
        } catch PlayerError.server(404, _, _) {
            if self.client === client { feed.mixError = "Diesen Mix gibt es heute nicht mehr. Die Übersicht wird aktualisiert."; await loadListenFeed() }
        } catch {
            if self.client === client { feed.mixError = (error as? PlayerError)?.localizedDescription ?? "Der Mix konnte nicht geladen werden." }
        }
    }
    /// "Am meisten gehört": the account's most played tracks of one artist.
    func artistTopTracks(_ artistID: String, limit: Int = 10) async throws -> [HistoryTrack] {
        guard let client, !offlineMode else { throw CancellationError() }
        let result: [HistoryTrack] = try await client.get(try client.server.itemPath("artists", id: artistID, suffix: "/top-tracks"),
                                                         query: [.init(name: "limit", value: String(limit))])
        guard self.client === client else { throw CancellationError() }
        return result
    }
    /// "Als Nächstes" / "Zur Warteschlange" for a whole album, artist, playlist or mix.
    func enqueue(_ tracks: [Track], next: Bool) {
        guard let client, !tracks.isEmpty else { return }
        let added = next ? player.insertNext(tracks, client: client) : player.append(tracks, client: client)
        if added < tracks.count { error = added == 0 ? "Die Warteschlange ist voll (höchstens 500 Titel)." : "Nur \(added) von \(tracks.count) Titeln passten noch in die Warteschlange (höchstens 500)." }
    }
}

// MARK: - Shared views

/// Navigation value for the "Für dich" overview of all mixes.
struct ForYouRoute: Hashable {}

private var listenSurface: Color {
    #if os(iOS)
    Color(uiColor: .secondarySystemGroupedBackground)
    #else
    Color.primary.opacity(0.06)
    #endif
}

/// A collage of up to four track covers, as on the web; a single cover fills it.
struct MixArtwork: View {
    var model: AppModel
    var mix: PersonalMix
    var size: CGFloat
    var body: some View {
        let ids = mix.coverTrackIds
        Group {
            if ids.isEmpty {
                Image(systemName: "sparkles").font(.system(size: size * 0.3)).foregroundStyle(.tint)
                    .frame(width: size, height: size).background(.tint.opacity(0.12))
            } else if ids.count == 1 {
                ArtworkView(model: model, kind: "tracks", id: ids[0], cornerRadius: 0).frame(width: size, height: size)
            } else {
                Grid(horizontalSpacing: 0, verticalSpacing: 0) {
                    ForEach(0..<2, id: \.self) { row in
                        GridRow {
                            ForEach(0..<2, id: \.self) { column in
                                ArtworkView(model: model, kind: "tracks", id: ids[(row * 2 + column) % ids.count], cornerRadius: 0)
                                    .frame(width: size / 2, height: size / 2)
                            }
                        }
                    }
                }
            }
        }.frame(width: size, height: size).clipShape(RoundedRectangle(cornerRadius: size > 100 ? 18 : 12)).accessibilityHidden(true)
    }
}

/// One mix: the card opens the mix, the round button plays it right away.
struct MixCard: View {
    var model: AppModel
    var mix: PersonalMix
    var width: CGFloat = 164
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            NavigationLink(value: mix) {
                VStack(alignment: .leading, spacing: 8) {
                    MixArtwork(model: model, mix: mix, size: width)
                    Text(mix.label.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(.tint).lineLimit(1)
                    Text(mix.title).font(.headline).foregroundStyle(.primary).lineLimit(2)
                    if !mix.description.isEmpty {
                        Text(mix.description).font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    }
                }.frame(width: width, alignment: .leading).contentShape(Rectangle())
            }.buttonStyle(.plain)
                .accessibilityElement(children: .combine)
                .accessibilityLabel("\(mix.title), \(mix.label), \(mix.trackCount) Titel")
                .accessibilityHint("Öffnet den Mix")
                .accessibilityIdentifier("mix-card-" + mix.id)
            HStack {
                Text("\(mix.trackCount) Titel").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                Spacer(minLength: 4)
                Button { Task { await model.playPersonalMix(mix) } } label: {
                    Group {
                        if model.feed.playingMixID == mix.id { ProgressView() }
                        else { Image(systemName: "play.fill").font(.system(size: 15, weight: .semibold)) }
                    }.frame(width: 44, height: 44).background(.tint.opacity(0.15), in: Circle()).contentShape(Circle())
                }.buttonStyle(.plain).foregroundStyle(.tint)
                    .disabled(model.feed.playingMixID != nil)
                    .accessibilityLabel("\(mix.title) abspielen")
            }
        }.frame(width: width, alignment: .leading)
    }
}

/// "Für dich": every mix of the day, genre mixes first.
struct ForYouView: View {
    var model: AppModel
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                Text("Täglich neu gemischt aus deiner Mediathek und deinem Hörverlauf · \(GermanDates.day(.now))")
                    .font(.subheadline).foregroundStyle(.secondary)
                if let message = model.feed.mixError { Text(message).font(.callout).foregroundStyle(.red) }
                switch model.feed.mixesPhase {
                case .loading, .idle: ProgressView("Mixe werden zusammengestellt …").frame(maxWidth: .infinity)
                case .failed(let message):
                    ListenSectionFailure(message: message) { Task { await model.loadListenFeed() } }
                case .unsupported:
                    ContentUnavailableView("Mixe nicht verfügbar", systemImage: "sparkles", description: Text("Dein Server bietet noch keine persönlichen Mixe an. Sie gibt es ab YTMDL 1.10."))
                case .loaded:
                    if model.feed.mixes.isEmpty {
                        ContentUnavailableView("Noch keine Mixe", systemImage: "sparkles", description: Text("Mixe entstehen aus der Musik in deiner Mediathek und aus dem, was du hörst. Jeder Mix braucht mindestens zehn passende Titel – mit mehr Musik und Hörverlauf erscheinen sie hier und wechseln täglich."))
                    }
                    grid("Deine Mixe", model.feed.mixes.filter(\.isGenreMix))
                    grid("Aus deinem Hören", model.feed.mixes.filter { !$0.isGenreMix })
                }
            }.padding(20).frame(maxWidth: 1200, alignment: .leading).frame(maxWidth: .infinity)
        }
        #if os(iOS)
        .background(Color(uiColor: .systemGroupedBackground))
        .refreshable { await model.loadListenFeed() }
        #endif
        .navigationTitle("Für dich")
        .task { if model.feed.mixesPhase == .idle { await model.loadListenFeed() } }
    }
    @ViewBuilder private func grid(_ title: String, _ mixes: [PersonalMix]) -> some View {
        if !mixes.isEmpty {
            VStack(alignment: .leading, spacing: 14) {
                Text(title).font(.title2.bold()).accessibilityAddTraits(.isHeader)
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 150, maximum: 220), spacing: 16, alignment: .top)], alignment: .leading, spacing: 24) {
                    ForEach(mixes) { mix in MixCard(model: model, mix: mix, width: 150) }
                }
            }
        }
    }
}

/// One mix with its tracks.
struct MixDetailView: View {
    var model: AppModel
    var mix: PersonalMix
    @State private var detail: PersonalMixDetail?
    @State private var failure: String?
    @State private var gone = false
    @State private var savingPlaylist = false
    @State private var savedPlaylist: Playlist?
    @State private var sort = MixTrackSort.mix
    @State private var descending = false
    @Environment(\.dynamicTypeSize) private var textSize
    /// In the chosen order, like the web; playing and saving use this order too.
    private var tracks: [Track] { sort.apply(detail?.tracks ?? [], descending: descending) }
    private var shown: PersonalMix { detail?.mix ?? mix }
    /// Saving creates a playlist on the server: not without a connection.
    private var saveUnavailable: Bool { model.offlineMode || model.feed.unreachable || model.client == nil }
    var body: some View {
        List {
            Section {
                header.listRowBackground(Color.clear)
                    #if !os(tvOS)
                    .listRowSeparator(.hidden)
                    #endif
            }
            if let savedPlaylist {
                Section {
                    NavigationLink(value: CollectionKind.playlist(savedPlaylist)) {
                        Label("Als Playlist „\(savedPlaylist.name)“ gespeichert", systemImage: "checkmark.circle.fill")
                    }.accessibilityHint("Öffnet die Playlist").accessibilityIdentifier("mix-saved-playlist")
                }
            }
            Section {
                if gone {
                    ContentUnavailableView("Diesen Mix gibt es heute nicht", systemImage: "sparkles",
                        description: Text("Mixe werden täglich neu zusammengestellt. Öffne „Für dich“, um die aktuellen Mixe zu sehen."))
                } else if let failure {
                    Text(failure).foregroundStyle(.secondary)
                    Button("Mix erneut laden") { Task { await load() } }
                } else if detail == nil {
                    ProgressView("Mix wird geladen …")
                } else if tracks.isEmpty {
                    ContentUnavailableView("Keine Titel mehr verfügbar", systemImage: "sparkles", description: Text("Die Titel dieses Mixes sind nicht mehr in der Mediathek."))
                }
                #if os(tvOS)
                if tracks.count > 1 { sortMenu }
                #endif
                ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in
                    TrackRow(model: model, track: track) { play(start: index) }
                }
            }
        }
        .navigationTitle(shown.title)
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await load() }
        #endif
        .task { if detail == nil { await load() } }
        #if !os(tvOS)
        .toolbar { if tracks.count > 1 { ToolbarItem(placement: .primaryAction) { sortMenu } } }
        #endif
        .sheet(isPresented: $savingPlaylist) {
            AddToPlaylistSheet(model: model, tracks: tracks, title: "Mix als Playlist speichern",
                               suggestedName: PersonalMix.playlistName(shown.title, date: .now),
                               createLabel: "Als neue Playlist speichern") { savedPlaylist = $0 }
        }
    }
    /// Sorting as on the web's mix page: the mix order or one column, either direction.
    private var sortMenu: some View {
        Menu {
            Picker("Sortieren nach", selection: $sort) {
                ForEach(MixTrackSort.allCases) { option in Text(option.label).tag(option) }
            }.pickerStyle(.inline)
            if sort != .mix {
                Picker("Richtung", selection: $descending) {
                    Text("Aufsteigend").tag(false)
                    Text("Absteigend").tag(true)
                }.pickerStyle(.inline)
            }
        } label: {
            Label(sort == .mix ? "Sortieren" : "Sortiert nach \(sort.label)", systemImage: "arrow.up.arrow.down")
        }
        .accessibilityLabel(sort == .mix ? "Sortieren, Mix-Reihenfolge" : "Sortieren, \(sort.label), \(descending ? "absteigend" : "aufsteigend")")
        .accessibilityIdentifier("mix-sort")
    }
    private func savePlaylistButton() -> some View {
        Button { savedPlaylist = nil; savingPlaylist = true } label: { Label("Als Playlist speichern", systemImage: "music.note.list") }
            .disabled(tracks.isEmpty || saveUnavailable)
            .accessibilityHint(saveUnavailable ? "Braucht eine Verbindung zum Server" : "Legt eine Playlist mit den Titeln dieses Mixes an")
            .accessibilityIdentifier("mix-save-playlist")
    }
    @ViewBuilder private var offlineSaveNote: some View {
        if saveUnavailable && !tracks.isEmpty {
            Label("Zum Speichern als Playlist wird eine Verbindung zum Server benötigt.", systemImage: "wifi.slash")
                .font(.footnote).foregroundStyle(.secondary)
        }
    }
    #if os(tvOS)
    /// Apple TV: a large cover beside the title and one row of focusable actions.
    private var header: some View {
        HStack(alignment: .top, spacing: 48) {
            MixArtwork(model: model, mix: shown, size: 340)
            VStack(alignment: .leading, spacing: 14) {
                Text(shown.label.uppercased()).font(.caption.weight(.semibold)).foregroundStyle(.tint)
                Text(shown.title).font(.title2.bold()).accessibilityAddTraits(.isHeader)
                if !shown.description.isEmpty { Text(shown.description).font(.callout).foregroundStyle(.secondary).lineLimit(3) }
                Text("\(detail.map { $0.tracks.count } ?? shown.trackCount) Titel · \(listenDuration(tracks.reduce(0) { $0 + $1.durationMs }))")
                    .font(.callout).monospacedDigit().foregroundStyle(.secondary)
                HStack(spacing: 24) {
                    Button { play(start: 0) } label: { Label("Abspielen", systemImage: "play.fill") }
                        .buttonStyle(.borderedProminent).accessibilityIdentifier("mix-play")
                    Button { play(start: 0, shuffle: true) } label: { Label("Zufällig", systemImage: "shuffle") }
                        .buttonStyle(.bordered).accessibilityIdentifier("mix-shuffle")
                    Menu {
                        Button("Als Nächstes abspielen", systemImage: "text.insert") { model.enqueue(tracks, next: true) }
                        Button("Zur Warteschlange hinzufügen", systemImage: "text.badge.plus") { model.enqueue(tracks, next: false) }
                    } label: { Label("Warteschlange", systemImage: "text.badge.plus") }
                    savePlaylistButton().buttonStyle(.bordered)
                }.disabled(tracks.isEmpty).padding(.top, 12)
                offlineSaveNote
            }.frame(maxWidth: .infinity, alignment: .leading)
        }.padding(.vertical, 24)
    }
    #elseif os(iOS)
    /// The header of all collection pages, with the mix's own actions.
    private var header: some View {
        VStack(spacing: ListenMetrics.shelfGap) {
            CollectionHeader(eyebrow: shown.label, title: shown.title,
                             detail: "\(detail.map { $0.tracks.count } ?? shown.trackCount) Titel · \(listenDuration(tracks.reduce(0) { $0 + $1.durationMs }))",
                             description: shown.description, canPlay: !tracks.isEmpty, idPrefix: "mix",
                             actions: [
                                queueCollectionAction(model, tracks: tracks, id: "mix-enqueue"),
                                CollectionAction(id: "mix-save-playlist", title: "Als Playlist", label: "Als Playlist speichern", symbol: "music.note.list",
                                                 hint: saveUnavailable ? "Braucht eine Verbindung zum Server" : "Legt eine Playlist mit den Titeln dieses Mixes an",
                                                 enabled: !tracks.isEmpty && !saveUnavailable,
                                                 kind: .button({ savedPlaylist = nil; savingPlaylist = true }))
                             ],
                             play: { play(start: 0) }, shuffle: { play(start: 0, shuffle: true) }) { size in
                MixArtwork(model: model, mix: shown, size: size)
            }
            offlineSaveNote
        }
    }
    #else
    private var header: some View {
        let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 16)) : AnyLayout(HStackLayout(alignment: .center, spacing: 20))
        return VStack(alignment: .leading, spacing: 18) {
            layout {
                MixArtwork(model: model, mix: shown, size: 128)
                VStack(alignment: .leading, spacing: 6) {
                    Text(shown.label.uppercased()).font(.caption.weight(.semibold)).foregroundStyle(.tint)
                    Text(shown.title).font(.title2.bold()).accessibilityAddTraits(.isHeader)
                    if !shown.description.isEmpty { Text(shown.description).font(.subheadline).foregroundStyle(.secondary).lineLimit(3) }
                    Text("\(detail.map { $0.tracks.count } ?? shown.trackCount) Titel · \(listenDuration(tracks.reduce(0) { $0 + $1.durationMs }))")
                        .font(.footnote).monospacedDigit().foregroundStyle(.secondary)
                }.frame(maxWidth: .infinity, alignment: .leading)
            }
            let actions = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 12)) : AnyLayout(HStackLayout(spacing: 12))
            actions {
                Button { play(start: 0) } label: { Label("Abspielen", systemImage: "play.fill") }
                    .buttonStyle(MobileActionStyle(prominent: true)).accessibilityIdentifier("mix-play")
                Button { play(start: 0, shuffle: true) } label: { Label("Zufällig", systemImage: "shuffle") }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier("mix-shuffle")
            }.disabled(tracks.isEmpty)
            // Three equal icon-over-label tiles with short labels; when they do
            // not fit (large text), one "Weitere Aktionen" menu instead, so no
            // label ever wraps inside its button.
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 10) {
                    Button { model.enqueue(tracks, next: true) } label: { Label("Als Nächstes", systemImage: "text.insert") }
                        .buttonStyle(MixTileStyle()).disabled(tracks.isEmpty)
                        .accessibilityLabel("Als Nächstes abspielen").accessibilityIdentifier("mix-play-next")
                    Button { model.enqueue(tracks, next: false) } label: { Label("Warteschlange", systemImage: "text.badge.plus") }
                        .buttonStyle(MixTileStyle()).disabled(tracks.isEmpty)
                        .accessibilityLabel("Zur Warteschlange hinzufügen").accessibilityIdentifier("mix-enqueue")
                    Button { savedPlaylist = nil; savingPlaylist = true } label: { Label("Als Playlist", systemImage: "music.note.list") }
                        .buttonStyle(MixTileStyle()).disabled(tracks.isEmpty || saveUnavailable)
                        .accessibilityLabel("Als Playlist speichern")
                        .accessibilityHint(saveUnavailable ? "Braucht eine Verbindung zum Server" : "Legt eine Playlist mit den Titeln dieses Mixes an")
                        .accessibilityIdentifier("mix-save-playlist")
                }
                Menu {
                    Button("Als Nächstes abspielen", systemImage: "text.insert") { model.enqueue(tracks, next: true) }.disabled(tracks.isEmpty)
                    Button("Zur Warteschlange hinzufügen", systemImage: "text.badge.plus") { model.enqueue(tracks, next: false) }.disabled(tracks.isEmpty)
                    savePlaylistButton()
                } label: { Label("Weitere Aktionen", systemImage: "ellipsis.circle") }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier("mix-more")
            }
            offlineSaveNote
        }.padding(.vertical, 8)
    }
    #endif
    private func play(start: Int, shuffle: Bool = false) {
        guard let client = model.client, !tracks.isEmpty else { return }
        model.player.play(shuffle ? tracks.shuffled() : tracks, start: shuffle ? 0 : start, client: client, context: shown.queueContext)
    }
    private func load() async {
        failure = nil
        do { detail = try await model.loadPersonalMix(mix.id); gone = false }
        catch is CancellationError { }
        catch PlayerError.server(404, _, _) { gone = true }
        catch { if !Task.isCancelled { failure = "Der Mix konnte nicht geladen werden. Bitte Verbindung prüfen und erneut versuchen." } }
    }
}

/// "Am meisten gehört" on the artist page: the listener's most played tracks
/// of the artist, numbered by rank. Hidden while loading, on failure and when
/// there is no history yet; every track is also in the full list below.
struct ArtistTopTracksSection: View {
    var model: AppModel
    var artist: Artist
    /// Loaded by the artist page; empty hides the section.
    var tracks: [HistoryTrack]
    @State private var expanded = false
    static let collapsed = 5
    private var context: QueueContext { QueueContext(kind: "history", id: "artist-top:" + artist.id, title: "Am meisten gehört · \(artist.name)") }
    var body: some View {
        if !tracks.isEmpty {
            Section {
                ForEach(Array(tracks.prefix(expanded ? tracks.count : Self.collapsed).enumerated()), id: \.element.id) { index, entry in
                    row(index, entry)
                }
                if tracks.count > Self.collapsed {
                    Button(expanded ? "Weniger anzeigen" : "Mehr anzeigen") { expanded.toggle() }
                }
            } header: { Text("Am meisten gehört").accessibilityAddTraits(.isHeader) }
        }
    }
    private func row(_ index: Int, _ entry: HistoryTrack) -> some View {
        Button {
            if let client = model.client { model.player.play(tracks.map(\.track), start: index, client: client, context: context) }
        } label: {
            HStack(spacing: 12) {
                Text("\(index + 1)").font(.subheadline.weight(.semibold)).monospacedDigit().foregroundStyle(.secondary).frame(minWidth: 22)
                ArtworkView(model: model, kind: "tracks", id: entry.track.id).frame(width: 44, height: 44)
                VStack(alignment: .leading, spacing: 3) {
                    Text(entry.track.title).font(.headline).foregroundStyle(model.player.current?.id == entry.track.id ? Color.accentColor : .primary).lineLimit(1)
                    Text(entry.track.album).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }.frame(maxWidth: .infinity, alignment: .leading)
                Text("\(entry.playCount)× gehört").font(.caption).monospacedDigit().foregroundStyle(.secondary)
            }.contentShape(Rectangle())
        }.buttonStyle(.plain)
            .accessibilityLabel("\(entry.track.title) abspielen, \(entry.playCount)-mal gehört")
    }
}

#if !os(tvOS)
/// "Rückgängig" after clearing the queue, like the web's toast: a small bar
/// that never blocks the player and leaves after a few seconds or as soon as
/// the queue changes. Apple TV shows a focusable button in its queue instead.
struct QueueUndoBar: View {
    var player: PlayerModel
    /// The edge the bar slides in from.
    var edge: Edge = .bottom
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        VStack {
            if let offer = player.undoOffer {
                HStack(spacing: 12) {
                    Image(systemName: "trash").foregroundStyle(.secondary).accessibilityHidden(true)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(offer.undo.clearedAll ? "Warteschlange geleert" : "Kommende Titel entfernt").font(.subheadline.weight(.semibold))
                        Text("\(offer.removedCount) Titel entfernt").font(.caption).monospacedDigit().foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Button("Rückgängig") { player.undoClear() }
                        .buttonStyle(.borderless).font(.subheadline.weight(.semibold))
                        .frame(minHeight: 44).contentShape(Rectangle())
                        .accessibilityHint("Stellt die entfernten Titel wieder her")
                        .accessibilityIdentifier("queue-undo")
                }
                .padding(.horizontal, 16).padding(.vertical, 8)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 16))
                .frame(maxWidth: 520)
                .padding(.horizontal, 16).padding(.vertical, 8)
                .transition(reduceMotion ? .opacity : .move(edge: edge).combined(with: .opacity))
            }
        }
        .frame(maxWidth: .infinity)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: player.undoOffer?.id)
    }
}
#endif

/// An inline error with a retry action, used by the start screen's sections.
/// A secondary action of the mix page: symbol above a one-line label, equal
/// widths in a row, at least 44 pt tall.
struct MixTileStyle: ButtonStyle {
    @Environment(\.isEnabled) private var enabled
    /// The same surface as `MobileActionStyle`.
    private var surface: Color {
        #if os(iOS)
        Color(uiColor: .secondarySystemGroupedBackground)
        #else
        Color.primary.opacity(0.06)
        #endif
    }
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.labelStyle(MixTileLabelStyle())
            .padding(.horizontal, 8).padding(.vertical, 10).frame(maxWidth: .infinity, minHeight: 60)
            .foregroundStyle(Color.primary)
            .background { RoundedRectangle(cornerRadius: 16).fill(surface) }
            .opacity(!enabled ? 0.45 : configuration.isPressed ? 0.75 : 1)
            .contentShape(RoundedRectangle(cornerRadius: 16))
    }
}
private struct MixTileLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        VStack(spacing: 6) {
            configuration.icon.font(.title3).foregroundStyle(.tint)
            configuration.title.font(.footnote.weight(.semibold)).lineLimit(1).fixedSize()
        }
    }
}

struct ListenSectionFailure: View {
    var message: String
    var retry: () -> Void
    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle").foregroundStyle(.secondary).accessibilityHidden(true)
            Text(message).font(.subheadline).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading)
            Button("Erneut laden", action: retry)
        }.padding(14).background(listenSurface, in: RoundedRectangle(cornerRadius: 16))
    }
}
