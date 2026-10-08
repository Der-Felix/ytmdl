#if os(iOS)
import SwiftUI
import YTMDLCore

// The saved music, shown with the same tabs, cards and rows as the online app. Only the data
// differs: everything here comes from the device, so nothing waits for the server. Managing the
// downloads (states, sorting, removing copies) stays in `OfflineLibraryView` under Settings.

/// Where a row in the offline screens leads.
enum OfflineTarget: Hashable {
    case collection(String)
    case album(String)
}

/// One album's worth of stored songs, grouped by the album and artist names the songs carry.
struct OfflineAlbum: Identifiable, Hashable {
    let id: String
    let title: String
    let artist: String
    var tracks: [Track]
    static func == (left: OfflineAlbum, right: OfflineAlbum) -> Bool { left.id == right.id }
    func hash(into hasher: inout Hasher) { hasher.combine(id) }
}

enum OfflineBrowse {
    /// Songs without an album are collected under one entry instead of one album each.
    static func albums(_ tracks: [Track]) -> [OfflineAlbum] {
        var order: [String] = []
        var groups: [String: OfflineAlbum] = [:]
        for track in tracks {
            let key = track.album.isEmpty ? "" : track.artistText + "\n" + track.album
            if groups[key] == nil {
                order.append(key)
                groups[key] = OfflineAlbum(id: key, title: track.album.isEmpty ? "Einzelne Titel" : track.album,
                                           artist: track.album.isEmpty ? "" : track.artistText, tracks: [])
            }
            groups[key]?.tracks.append(track)
        }
        return order.compactMap { groups[$0] }
    }
}

/// Playable songs and albums on this device, read once per change instead of on every render
/// (every playable song costs a file check).
struct OfflineShelf {
    var tracks: [Track] = []
    var albums: [OfflineAlbum] = []
    @MainActor init(_ library: OfflineLibrary) {
        tracks = library.availableTracks(in: OfflineCatalog.records(library.currentRecords, sort: .artist))
        albums = OfflineBrowse.albums(tracks)
    }
}

extension AppModel {
    /// Changes whenever saved songs or collections do.
    var offlineSignature: String {
        "\(offline.currentRecords.count)-\(offline.usedBytes)-\(offline.currentCollections.count)"
    }
}

/// Says why only saved music is shown and how to leave: reconnect after an unreachable server,
/// or sign in when the saved music was opened by hand.
struct OfflineStatusCard: View {
    var model: AppModel
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Label(model.canReconnect ? "Server nicht erreichbar" : "Offline-Modus", systemImage: "wifi.slash").font(.headline)
            Text(model.canReconnect ? "Deine gespeicherte Musik ist trotzdem verfügbar." : "Hier erscheint nur Musik, die auf diesem Gerät gespeichert ist.")
                .font(.subheadline).foregroundStyle(.secondary)
            if model.canReconnect {
                Button(model.reconnecting ? "Verbinde …" : "Erneut verbinden", systemImage: "arrow.clockwise") { Task { await model.reconnect() } }
                    .buttonStyle(MobileActionStyle()).disabled(model.reconnecting).accessibilityIdentifier("offline-reconnect")
            } else {
                Button("Zur Anmeldung", systemImage: "network") { Task { await model.logout() } }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier("offline-sign-in")
            }
        }.padding(16).frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18))
    }
}

private func offlineHeading(_ title: String, subtitle: String) -> some View {
    VStack(alignment: .leading, spacing: 4) {
        Text(title).font(.title2.bold())
        Text(subtitle).font(.subheadline).foregroundStyle(.secondary)
    }
}

private func offlineCard<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
    VStack(spacing: 0) { content() }.background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18))
}

@MainActor private func offlinePlay(_ model: AppModel, _ tracks: [Track], selected: String? = nil) {
    guard let client = model.client else { return }
    model.player.play(tracks, start: selected.flatMap { id in tracks.firstIndex { $0.id == id } } ?? 0, client: client)
}

/// A saved playlist, favorites, album or artist as a row, like the playlist rows of the Start tab.
private struct OfflineCollectionRow: View {
    var model: AppModel
    var collection: OfflineCollection
    var body: some View {
        let saved = OfflineCatalog.records(model.offline.currentRecords, collection: collection, sort: .collection)
        let ready = model.offline.availableTracks(in: saved)
        NavigationLink(value: OfflineTarget.collection(collection.id)) {
            HStack(spacing: 16) {
                MobileCollectionArtwork(model: model, tracks: saved.map(\.track), size: 72)
                VStack(alignment: .leading, spacing: 6) {
                    Text(collection.name).font(.headline).foregroundStyle(.primary).lineLimit(2)
                    Text("\(ready.count) von \(Set(collection.trackIDs).count) Titeln offline")
                        .font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                }.frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
            }.padding(16).contentShape(Rectangle())
        }.buttonStyle(.plain).accessibilityIdentifier("offline-collection-" + collection.sourceID)
    }
}

@MainActor private func offlineCollections(_ model: AppModel, kind: String) -> [OfflineCollection] {
    model.offline.currentCollections.filter { $0.kind == kind }
        .sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
}

// MARK: Start

struct OfflineHomeView: View {
    var model: AppModel
    @State private var shelf: OfflineShelf?
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                OfflineStatusCard(model: model)
                if let shelf = self.shelf {
                    hero(shelf)
                    if shelf.tracks.isEmpty {
                        ContentUnavailableView("Noch keine gespeicherte Musik", systemImage: "arrow.down.circle",
                            description: Text("Speichere Playlists, Alben oder Favoriten mit „Offline speichern“, solange du verbunden bist."))
                    } else {
                        resume(shelf)
                        recent(shelf)
                        if !shelf.albums.isEmpty { albumShelf(shelf) }
                        playlists
                    }
                }
            }.padding(.horizontal, 20).padding(.top, 12).padding(.bottom, 32)
                .frame(maxWidth: 1000).frame(maxWidth: .infinity)
        }.background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle("Start")
            .task(id: model.offlineSignature) { shelf = OfflineShelf(model.offline) }
    }
    private func hero(_ shelf: OfflineShelf) -> some View {
        VStack(alignment: .leading, spacing: 20) {
            VStack(alignment: .leading, spacing: 8) {
                Text("Hallo, \(model.user?.displayName ?? "")").font(.subheadline).foregroundStyle(.secondary)
                Text("Deine Musik.\nDein Moment.").font(.title.bold()).fixedSize(horizontal: false, vertical: true)
            }.frame(maxWidth: .infinity, alignment: .leading)
            Button { offlinePlay(model, shelf.tracks.shuffled()) } label: { Label("Alles mischen", systemImage: "shuffle") }
                .buttonStyle(MobileActionStyle(prominent: true)).disabled(shelf.tracks.isEmpty)
        }.padding(20).background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 22))
    }
    @ViewBuilder private func resume(_ shelf: OfflineShelf) -> some View {
        if let snapshot = model.listeningHistory.snapshot, snapshot.tracks.contains(where: { model.offline.audioURL($0.id) != nil }) {
            Button { model.resumeLastSession() } label: {
                HStack(spacing: 14) {
                    Image(systemName: "play.circle.fill").font(.title2).foregroundStyle(.tint)
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Weiterhören").font(.headline).foregroundStyle(.primary)
                        Text("Letzte Wiedergabe fortsetzen").font(.subheadline).foregroundStyle(.secondary)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    Image(systemName: "chevron.right").font(.caption).foregroundStyle(.secondary)
                }.padding(16).background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16))
            }.buttonStyle(.plain).accessibilityLabel("Letzte Wiedergabe fortsetzen")
        }
    }
    @ViewBuilder private func recent(_ shelf: OfflineShelf) -> some View {
        let known = Set(shelf.tracks.map(\.id))
        let tracks = model.listeningHistory.tracks.filter { known.contains($0.id) }
        if !tracks.isEmpty {
            VStack(alignment: .leading, spacing: 12) {
                offlineHeading("Zuletzt gehört", subtitle: "Schnell zurück zu deiner Musik")
                offlineCard {
                    ForEach(Array(tracks.prefix(4))) { track in
                        TrackRow(model: model, track: track) { offlinePlay(model, tracks, selected: track.id) }
                            .padding(.horizontal, 12).padding(.vertical, 6)
                    }
                }
            }
        }
    }
    private func albumShelf(_ shelf: OfflineShelf) -> some View {
        VStack(alignment: .leading, spacing: 14) {
            offlineHeading("Auf diesem Gerät", subtitle: "Deine gespeicherten Alben")
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 16) {
                    ForEach(Array(shelf.albums.prefix(12))) { album in
                        NavigationLink(value: OfflineTarget.album(album.id)) {
                            VStack(alignment: .leading, spacing: 8) {
                                ArtworkView(model: model, kind: "tracks", id: album.tracks[0].id).frame(width: 148, height: 148)
                                Text(album.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(2)
                                Text(album.artist).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }.frame(width: 148, alignment: .leading)
                        }.buttonStyle(.plain)
                    }
                }
            }.scrollIndicators(.hidden)
        }
    }
    @ViewBuilder private var playlists: some View {
        let items = offlineCollections(model, kind: "playlist")
        if !items.isEmpty {
            VStack(alignment: .leading, spacing: 14) {
                offlineHeading("Deine Playlists", subtitle: "Offline gespeichert")
                offlineCard {
                    ForEach(Array(items.prefix(3))) { item in
                        OfflineCollectionRow(model: model, collection: item)
                        if item.id != items.prefix(3).last?.id { Divider().padding(.leading, 104) }
                    }
                }
            }
        }
    }
}

// MARK: Bibliothek

struct OfflineAlbumsView: View {
    var model: AppModel
    @State private var shelf: OfflineShelf?
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Deine Alben").font(.title2.bold())
                    Text("Musik, die auf diesem Gerät gespeichert ist").font(.subheadline).foregroundStyle(.secondary)
                }
                if let shelf = self.shelf {
                    if shelf.albums.isEmpty {
                        ContentUnavailableView("Noch keine gespeicherte Musik", systemImage: "square.stack",
                            description: Text("Speichere Musik mit „Offline speichern“, solange du verbunden bist."))
                    }
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 145, maximum: 280), spacing: 20)], spacing: 24) {
                        ForEach(shelf.albums) { album in
                            NavigationLink(value: OfflineTarget.album(album.id)) {
                                VStack(alignment: .leading, spacing: 8) {
                                    ArtworkView(model: model, kind: "tracks", id: album.tracks[0].id)
                                    Text(album.title).font(.headline).foregroundStyle(.primary).lineLimit(2)
                                    Text(album.artist).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                                }.frame(maxWidth: .infinity, alignment: .leading)
                            }.buttonStyle(.plain)
                        }
                    }
                }
            }.padding(20)
        }.background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle("Bibliothek")
            .task(id: model.offlineSignature) { shelf = OfflineShelf(model.offline) }
    }
}

// MARK: Playlists

struct OfflinePlaylistsView: View {
    var model: AppModel
    @State private var query = ""
    private var kinds: [(kind: String, title: String)] {
        [("playlist", "Playlists"), ("favorites", "Favoriten"), ("release", "Alben"), ("artist", "Künstler")]
    }
    private func items(_ kind: String) -> [OfflineCollection] {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return offlineCollections(model, kind: kind).filter { text.isEmpty || $0.name.localizedCaseInsensitiveContains(text) }
    }
    var body: some View {
        let empty = kinds.allSatisfy { items($0.kind).isEmpty }
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                Text("Offline gespeicherte Sammlungen.").font(.subheadline).foregroundStyle(.secondary)
                if empty {
                    ContentUnavailableView(query.isEmpty ? "Noch keine Sammlungen" : "Keine passende Sammlung",
                        systemImage: query.isEmpty ? "music.note.list" : "magnifyingglass",
                        description: Text(query.isEmpty ? "Speichere eine Playlist, ein Album oder deine Favoriten mit „Offline speichern“." : "Suche nach einem Namen."))
                }
                ForEach(kinds, id: \.kind) { entry in
                    let list = items(entry.kind)
                    if !list.isEmpty {
                        VStack(alignment: .leading, spacing: 12) {
                            Text(entry.title).font(.title3.bold())
                            offlineCard {
                                ForEach(list) { item in
                                    OfflineCollectionRow(model: model, collection: item)
                                    if item.id != list.last?.id { Divider().padding(.leading, 104) }
                                }
                            }
                        }
                    }
                }
            }.padding(20)
        }.background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle("Playlists")
            .searchable(text: $query, prompt: "Sammlungen filtern")
    }
}

// MARK: Suche

struct OfflineSearchView: View {
    var model: AppModel
    @State private var query = ""
    @State private var shelf: OfflineShelf?
    private var text: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }
    var body: some View {
        List {
            if let shelf = self.shelf {
                if text.isEmpty {
                    ContentUnavailableView("Gespeicherte Musik durchsuchen", systemImage: "magnifyingglass",
                        description: Text("Titel, Alben oder Künstler auf diesem Gerät."))
                } else {
                    let tracks = shelf.tracks.filter { [$0.title, $0.artistText, $0.album].joined(separator: " ").localizedCaseInsensitiveContains(text) }
                    let albums = shelf.albums.filter { !$0.artist.isEmpty && [$0.title, $0.artist].joined(separator: " ").localizedCaseInsensitiveContains(text) }
                    if !albums.isEmpty {
                        Section("Alben") {
                            ForEach(albums) { album in
                                NavigationLink(value: OfflineTarget.album(album.id)) {
                                    HStack {
                                        ArtworkView(model: model, kind: "tracks", id: album.tracks[0].id).frame(width: 56, height: 56)
                                        VStack(alignment: .leading) { Text(album.title); Text(album.artist).font(.caption).foregroundStyle(.secondary) }
                                    }
                                }
                            }
                        }
                    }
                    if !tracks.isEmpty {
                        Section("Titel") {
                            ForEach(tracks) { track in TrackRow(model: model, track: track) { offlinePlay(model, tracks, selected: track.id) } }
                        }
                    }
                    if tracks.isEmpty && albums.isEmpty { ContentUnavailableView.search(text: text) }
                }
            }
        }
        .navigationTitle("Suche")
        .searchable(text: $query, prompt: "Titel, Alben, Künstler")
        .task(id: model.offlineSignature) { shelf = OfflineShelf(model.offline) }
    }
}

// MARK: Detail

/// A saved collection or album: the same header and track rows as the online collection screen.
struct OfflineDetailView: View {
    var model: AppModel
    var target: OfflineTarget
    @State private var shelf: OfflineShelf?
    private var collection: OfflineCollection? {
        guard case .collection(let id) = target else { return nil }
        return model.offline.currentCollections.first { $0.id == id }
    }
    private var album: OfflineAlbum? {
        guard case .album(let id) = target else { return nil }
        return shelf?.albums.first { $0.id == id }
    }
    private var title: String { collection?.name ?? album?.title ?? "Offline-Musik" }
    var body: some View {
        let saved = collection.map { OfflineCatalog.records(model.offline.currentRecords, collection: $0, sort: .collection) } ?? []
        let tracks = collection != nil ? model.offline.availableTracks(in: saved) : album?.tracks ?? []
        let total = collection.map { Set($0.trackIDs).count } ?? tracks.count
        List {
            Section {
                MobileCollectionHeader(model: model, tracks: tracks, title: title,
                    detail: "\(tracks.count) von \(total) Titeln offline" + (album.map { $0.artist.isEmpty ? "" : " · " + $0.artist } ?? ""),
                    play: { offlinePlay(model, tracks) }, shuffle: { offlinePlay(model, tracks.shuffled()) })
            }
            Section("\(tracks.count) Titel") {
                ForEach(tracks) { track in TrackRow(model: model, track: track) { offlinePlay(model, tracks, selected: track.id) } }
            }
        }
        .navigationTitle(title).navigationBarTitleDisplayMode(.inline)
        .task(id: model.offlineSignature) { shelf = OfflineShelf(model.offline) }
    }
}
#endif
