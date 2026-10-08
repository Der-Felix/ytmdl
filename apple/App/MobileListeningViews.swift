import SwiftUI
import YTMDLCore
#if os(iOS)
import UIKit
import MediaPlayer

struct MobileMiniPlayer: View {
    var model: AppModel
    var open: () -> Void
    var body: some View {
        if let track = model.player.current {
            HStack(spacing: 8) {
                Button(action: open) {
                    HStack(spacing: 10) {
                        ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 40, height: 40)
                        VStack(alignment: .leading, spacing: 2) {
                            Text(track.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                            Text(model.player.loading ? "Wird geladen …" : track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        // The system bottom accessory has a compact height;
                        // expanded playback exposes fully scaled metadata.
                        }.dynamicTypeSize(...DynamicTypeSize.xxxLarge).frame(maxWidth: .infinity, alignment: .leading)
                    }.contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel("Player öffnen: \(track.title), \(track.artistText)").accessibilityIdentifier("mobile-mini-player-open")
                Button { model.player.toggle() } label: {
                    Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen")
                Button { model.player.next() } label: { Image(systemName: "forward.end.fill").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }
                    .buttonStyle(.plain).accessibilityLabel("Nächster Titel")
            }.padding(.horizontal, 12).padding(.vertical, 6)
        }
    }
}

#endif

struct MobileCollectionArtwork: View {
    var model: AppModel
    var tracks: [Track]
    var size: CGFloat = 180
    private var preview: [Track] {
        var seen = Set<String>()
        return Array(tracks.filter { seen.insert($0.album.isEmpty ? $0.id : $0.artistText + "\n" + $0.album).inserted }.prefix(4))
    }
    var body: some View {
        Group {
            if preview.isEmpty { Image(systemName: "music.note.list").font(.system(size: size * 0.35)).foregroundStyle(.tint).frame(width: size, height: size).background(.tint.opacity(0.1)) }
            else if preview.count == 1 { ArtworkView(model: model, kind: "tracks", id: preview[0].id).frame(width: size, height: size) }
            else {
                LazyVGrid(columns: [GridItem(.fixed(size / 2), spacing: 0), GridItem(.fixed(size / 2), spacing: 0)], spacing: 0) {
                    ForEach(0..<4, id: \.self) { index in ArtworkView(model: model, kind: "tracks", id: preview[index % preview.count].id, cornerRadius: 0).frame(width: size / 2, height: size / 2) }
                }.frame(width: size, height: size)
            }
        }.clipShape(RoundedRectangle(cornerRadius: size > 100 ? 20 : 12)).accessibilityLabel("Cover der Sammlung")
    }
}

// Consistent touch surfaces: accent is reserved for the main action and icons.
struct MobileActionStyle: ButtonStyle {
    var prominent = false
    private var collectionSurface: Color {
        #if os(iOS)
        Color(uiColor: .secondarySystemGroupedBackground)
        #else
        Color.primary.opacity(0.06)
        #endif
    }
    @Environment(\.isEnabled) private var enabled
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.font(.headline).fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, 14).padding(.vertical, 12).frame(maxWidth: .infinity, minHeight: 52)
            .foregroundStyle(prominent ? Color.white : Color.primary)
            .background {
                RoundedRectangle(cornerRadius: 16)
                    .fill(prominent ? AnyShapeStyle(.tint) : AnyShapeStyle(collectionSurface))
            }
            .opacity(!enabled ? 0.45 : configuration.isPressed ? 0.75 : 1)
            .contentShape(RoundedRectangle(cornerRadius: 16))
    }
}

struct MobileCollectionHeader: View {
    var model: AppModel
    var tracks: [Track]
    var title: String
    var detail: String
    var description: String?
    var play: () -> Void
    var shuffle: () -> Void
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        VStack(alignment: .leading, spacing: 24) {
            let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 16)) : AnyLayout(HStackLayout(alignment: .center, spacing: 20))
            layout {
                MobileCollectionArtwork(model: model, tracks: tracks, size: 128)
                VStack(alignment: .leading, spacing: 8) {
                    Text(title).font(.title2.bold()).foregroundStyle(.primary)
                    Text(detail).font(.subheadline).foregroundStyle(.secondary)
                    if let description, !description.isEmpty {
                        Text(description).font(.subheadline).foregroundStyle(.secondary).lineLimit(3)
                    }
                }.frame(maxWidth: .infinity, alignment: .leading)
            }
            let actions = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 12)) : AnyLayout(HStackLayout(spacing: 12))
            actions {
                Button(action: play) { Label("Abspielen", systemImage: "play.fill") }
                    .buttonStyle(MobileActionStyle(prominent: true)).accessibilityIdentifier("collection-play")
                Button(action: shuffle) { Label("Zufall", systemImage: "shuffle") }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier("collection-shuffle")
            }.disabled(tracks.isEmpty)
        }.padding(.vertical, 8)
    }
}

#if os(iOS)
struct MobileLibraryShortcuts: View {
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 10)) : AnyLayout(HStackLayout(alignment: .top, spacing: 10))
        layout {
            NavigationLink(value: CollectionKind.favorites) { tile("Favoriten", symbol: "heart.fill") }.accessibilityLabel("Favoriten")
            NavigationLink(value: Destination.artists) { tile("Künstler", symbol: "person.2.fill") }.accessibilityLabel("Künstler")
            NavigationLink(value: Destination.downloads) { tile("Offline", symbol: "arrow.down.circle.fill") }.accessibilityLabel("Offline")
        }.buttonStyle(.plain)
    }
    private func tile(_ title: String, symbol: String) -> some View {
        VStack(spacing: 10) {
            Image(systemName: symbol).font(.system(size: 23, weight: .medium)).foregroundStyle(.tint)
            Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(2)
        }.padding(.horizontal, 4).padding(.vertical, 16).frame(maxWidth: .infinity, minHeight: 86)
            .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16))
            .contentShape(RoundedRectangle(cornerRadius: 16))
    }
}

struct MobilePlaylistRow: View {
    var model: AppModel
    var playlist: Playlist
    var body: some View {
        HStack(spacing: 16) {
            MobileCollectionArtwork(model: model, tracks: model.playlistPreviews[playlist.id] ?? [], size: 72)
            VStack(alignment: .leading, spacing: 6) {
                Text(playlist.name).font(.headline).foregroundStyle(.primary).lineLimit(2)
                Text("\(playlist.trackCount) Titel · \(mobileCollectionDuration(playlist.durationMs))")
                    .font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                if playlist.smartRules != nil { Label("Intelligent", systemImage: "sparkles").font(.caption).foregroundStyle(.secondary) }
            }.frame(maxWidth: .infinity, alignment: .leading)
            Image(systemName: "chevron.right").font(.caption.weight(.semibold)).foregroundStyle(.tertiary)
        }.padding(16).contentShape(Rectangle())
    }
}

func mobileCollectionDuration(_ milliseconds: Int) -> String {
    let minutes = max(0, milliseconds / 60_000)
    return minutes >= 60 ? "\(minutes / 60) Std. \(minutes % 60) Min." : "\(minutes) Min."
}

struct MobileHomeView: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                hero
                MobileLibraryShortcuts()
                if model.listeningHistory.snapshot != nil {
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
                if !model.listeningHistory.tracks.isEmpty {
                    VStack(alignment: .leading, spacing: 12) {
                        heading("Zuletzt gehört", subtitle: "Schnell zurück zu deiner Musik")
                        VStack(spacing: 0) {
                            ForEach(Array(model.listeningHistory.tracks.prefix(4))) { track in
                                TrackRow(model: model, track: track) { if let client = model.client { model.player.play(model.listeningHistory.tracks, start: model.listeningHistory.tracks.firstIndex(of: track) ?? 0, client: client) } }
                                    .padding(.horizontal, 12).padding(.vertical, 6)
                            }
                        }.background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18))
                    }
                }
                if !model.releases.isEmpty { albumShelf }
                VStack(alignment: .leading, spacing: 14) {
                    heading("Für deinen Moment", subtitle: "Ein Mix aus deiner Bibliothek")
                    LazyVGrid(columns: Array(repeating: GridItem(.flexible()), count: textSize.isAccessibilitySize ? 1 : 2), spacing: 12) {
                        mix("Favoriten", subtitle: "Deine Lieblingstitel", symbol: "heart.fill")
                        mix("Neu", subtitle: "Frisch hinzugefügt", symbol: "sparkles")
                        ForEach(Array(model.genres.filter { $0 != "__none__" }.prefix(4)), id: \.self) { genre in mix(genre, subtitle: "Genre-Mix", symbol: "waveform", genre: genre) }
                    }
                }
                if !model.playlists.isEmpty {
                    VStack(alignment: .leading, spacing: 14) {
                        heading("Deine Playlists", subtitle: "Deine Musik, zusammengestellt von dir")
                        VStack(spacing: 0) {
                            ForEach(Array(model.playlists.prefix(3))) { playlist in
                                NavigationLink(value: CollectionKind.playlist(playlist)) { MobilePlaylistRow(model: model, playlist: playlist) }.buttonStyle(.plain)
                                if playlist.id != model.playlists.prefix(3).last?.id { Divider().padding(.leading, 104) }
                            }
                        }.background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18))
                    }
                }
            }.padding(.horizontal, 20).padding(.top, 12).padding(.bottom, 32)
                .frame(maxWidth: 1000).frame(maxWidth: .infinity)
        }.background(Color(uiColor: .systemGroupedBackground))
            .navigationTitle("Start").refreshable { await model.loadLibrary() }
            .task(id: model.playlistRevision) { await model.loadPlaylistPreviews(Array(model.playlists.prefix(3))) }
    }
    private var hero: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack(alignment: .center, spacing: 16) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Hallo, \(model.user?.displayName ?? "")").font(.subheadline).foregroundStyle(.secondary)
                    Text("Deine Musik.\nDein Moment.").font(.title.bold()).fixedSize(horizontal: false, vertical: true)
                }.frame(maxWidth: .infinity, alignment: .leading)
                if !textSize.isAccessibilitySize, let release = model.releases.first {
                    ArtworkView(model: model, kind: "releases", id: release.id).frame(width: 80, height: 80).rotationEffect(.degrees(5)).accessibilityHidden(true)
                }
            }
            Button { Task { await model.playMix("Favoriten") } } label: { Label("Favoriten-Mix", systemImage: "play.fill") }
                .buttonStyle(MobileActionStyle(prominent: true)).disabled(model.listeningBusy)
        }.padding(20).background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 22))
    }
    private var albumShelf: some View {
        VStack(alignment: .leading, spacing: 14) {
            heading("Neu in deiner Bibliothek", subtitle: "Deine zuletzt hinzugefügten Alben")
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 16) {
                    ForEach(Array(model.releases.prefix(12))) { release in
                        NavigationLink(value: CollectionKind.release(release)) {
                            VStack(alignment: .leading, spacing: 8) {
                                ArtworkView(model: model, kind: "releases", id: release.id).frame(width: 148, height: 148)
                                Text(release.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(2)
                                Text(release.artists.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                            }.frame(width: 148, alignment: .leading)
                        }.buttonStyle(.plain)
                    }
                }
            }.scrollIndicators(.hidden)
        }
    }
    private func heading(_ title: String, subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.title2.bold())
            Text(subtitle).font(.subheadline).foregroundStyle(.secondary)
        }
    }
    private func mix(_ name: String, subtitle: String, symbol: String, genre: String = "") -> some View {
        Button { Task { await model.playMix(name, genre: genre) } } label: {
            VStack(alignment: .leading, spacing: 10) {
                Image(systemName: symbol).font(.title2).foregroundStyle(.tint)
                Text(name).font(.headline).foregroundStyle(.primary).lineLimit(2)
                Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }.padding(16).frame(maxWidth: .infinity, minHeight: 124, alignment: .leading)
                .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 18))
        }.buttonStyle(.plain).disabled(model.listeningBusy)
    }
}

#endif

#if !os(tvOS)
struct OfflineLibraryView: View {
    var model: AppModel
    var collectionID: String? = nil
    @State private var query = ""
    @State private var onlyReady = false
    @State private var browsing = "collections"
    @State private var sorting: OfflineSort = .collection
    @AppStorage("offlineTrackSort") private var trackSort = OfflineSort.artist.rawValue
    @State private var deleting: String?
    @State private var confirmDelete = false
    @State private var options = false
    @State private var player = false
    private var collection: OfflineCollection? { model.offline.currentCollections.first { $0.id == collectionID } }
    private var showingTracks: Bool { collectionID != nil || browsing == "tracks" }
    private var selectedSort: OfflineSort { collectionID != nil ? sorting : OfflineSort(rawValue: trackSort) ?? .artist }
    private var records: [OfflineTrack] {
        guard collectionID == nil || collection != nil else { return [] }
        return OfflineCatalog.records(model.offline.currentRecords, collection: collection,
                                      sort: selectedSort, query: query, onlyReady: onlyReady)
    }
    private var playable: [Track] { model.offline.availableTracks(in: records) }
    private var visibleCollections: [OfflineCollection] {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return model.offline.currentCollections.filter { item in
            query.isEmpty || item.name.localizedCaseInsensitiveContains(query) ||
                !OfflineCatalog.records(model.offline.currentRecords, collection: item, sort: .collection, query: query).isEmpty
        }.sorted {
            let result = $0.name.localizedStandardCompare($1.name)
            return result == .orderedSame ? $0.id < $1.id : result == .orderedAscending
        }
    }
    var body: some View {
        List {
            if collectionID == nil {
                Section {
                    Label(model.canReconnect ? "Keine Verbindung zum Server" : model.offlineMode ? "Offline-Modus" : "Auf diesem Gerät", systemImage: "wifi.slash")
                    Text("\(model.offline.readyTracks.count) Titel · \(ByteCountFormatter.string(fromByteCount: model.offline.usedBytes, countStyle: .file)) gespeichert")
                        .font(.subheadline).foregroundStyle(.secondary)
                    Picker("Offline-Ansicht", selection: $browsing) {
                        Text("Sammlungen").tag("collections")
                        Text("Alle Titel").tag("tracks")
                    }.pickerStyle(.segmented).accessibilityIdentifier("offline-view")
                }
            }
            if showingTracks {
                trackHeader
                if records.isEmpty {
                    ContentUnavailableView(query.isEmpty ? "Keine Titel in dieser Ansicht" : "Keine passenden Titel",
                        systemImage: query.isEmpty ? "arrow.down.circle" : "magnifyingglass",
                        description: Text(query.isEmpty ? "Gespeicherte Titel erscheinen hier. Prüfe auch den Filter für fertige Downloads." : "Suche nach Titel, Künstler oder Album oder lösche den Suchtext."))
                }
                Section("\(records.count) Titel") { ForEach(records) { record in trackRow(record) } }
            } else {
                collectionSections
            }
            if let error = model.offline.error { Section { Text(error).foregroundStyle(.red); Button("Meldung schließen") { model.offline.error = nil } } }
            if model.offlineMode && collectionID == nil {
                Section {
                    if model.canReconnect {
                        Button(model.reconnecting ? "Verbinde …" : "Erneut verbinden", systemImage: "arrow.clockwise") { Task { await model.reconnect() } }
                            .disabled(model.reconnecting).accessibilityIdentifier("offline-reconnect")
                    } else { Button("Zur Anmeldung", systemImage: "network") { Task { await model.logout() } } }
                }
            }
        }.navigationTitle(collection?.name ?? "Offline-Musik")
        .searchable(text: $query, prompt: showingTracks ? "Titel, Künstler oder Album" : "Playlist, Album oder Titel")
        .toolbar {
            ToolbarItem { Button("Download-Einstellungen", systemImage: "gearshape") { options = true } }
            if model.offlineMode && model.player.current != nil { ToolbarItem { Button("Player", systemImage: "play.circle") { player = true } } }
        }
        .sheet(isPresented: $options) { NavigationStack { Form { DownloadPreferences(model: model) }.navigationTitle("Downloads").toolbar { ToolbarItem(placement: .confirmationAction) { Button("Fertig") { options = false } } } } }
        .sheet(isPresented: $player) { NavigationStack { NowPlayingView(model: model).toolbar { ToolbarItem(placement: .cancellationAction) { Button("Schließen") { player = false } } } } }
        .confirmationDialog("Lokale Kopie entfernen?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Vom Gerät entfernen", role: .destructive) { if let deleting { model.offline.remove(deleting) } }
        } message: { Text("Der Titel bleibt auf deinem Musikserver erhalten. Die lokale Kopie wird auch in anderen Offline-Sammlungen nicht mehr verfügbar sein.") }
    }
    @ViewBuilder private var collectionSections: some View {
        if visibleCollections.isEmpty {
            ContentUnavailableView(query.isEmpty ? "Noch keine Offline-Sammlungen" : "Keine passende Sammlung",
                systemImage: query.isEmpty ? "music.note.list" : "magnifyingglass",
                description: Text(query.isEmpty ? "Speichere eine Playlist, ein Album oder deine Favoriten über „Offline speichern“. Einzeln gespeicherte Titel findest du unter „Alle Titel“." : "Suche nach einem Namen oder einem enthaltenen Titel."))
            if query.isEmpty { Button("Alle gespeicherten Titel anzeigen") { browsing = "tracks" } }
        }
        ForEach(["playlist", "favorites", "release", "artist"], id: \.self) { kind in
            let items = visibleCollections.filter { $0.kind == kind }
            if !items.isEmpty {
                Section(kind == "playlist" ? "Playlists" : kind == "favorites" ? "Favoriten" : kind == "release" ? "Alben" : "Künstler") {
                    ForEach(items) { item in
                        let saved = OfflineCatalog.records(model.offline.currentRecords, collection: item, sort: .collection)
                        let ready = model.offline.availableTracks(in: saved)
                        NavigationLink {
                            OfflineLibraryView(model: model, collectionID: item.id)
                        } label: {
                            HStack(spacing: 14) {
                                MobileCollectionArtwork(model: model, tracks: saved.map(\.track), size: 64)
                                VStack(alignment: .leading, spacing: 5) {
                                    Text(item.name).font(.headline).lineLimit(2)
                                    Text("\(ready.count) von \(Set(item.trackIDs).count) Titeln offline")
                                        .font(.subheadline).foregroundStyle(.secondary)
                                }.frame(maxWidth: .infinity, alignment: .leading)
                            }.padding(.vertical, 6)
                        }.accessibilityIdentifier("offline-collection-" + item.sourceID)
                    }
                }
            }
        }
    }
    private var trackHeader: some View {
        Section {
            if let collection {
                MobileCollectionHeader(model: model, tracks: records.map(\.track), title: collection.name,
                    detail: "\(playable.count) offline verfügbar · \(records.count) gespeichert",
                    play: { play(playable) }, shuffle: { play(playable.shuffled()) })
                    .disabled(playable.isEmpty)
                Toggle("Beim Aktualisieren synchronisieren", isOn: Binding(get: { collection.keepUpdated }, set: { model.offline.setKeepUpdated(collection.id, enabled: $0) }))
                    .disabled(model.offlineMode)
            } else {
                HStack {
                    Button("Abspielen", systemImage: "play.fill") { play(playable) }.buttonStyle(MobileActionStyle(prominent: true))
                    Button("Zufall", systemImage: "shuffle") { play(playable.shuffled()) }.buttonStyle(MobileActionStyle())
                }.disabled(playable.isEmpty)
            }
            Toggle("Nur fertige Downloads", isOn: $onlyReady)
            Picker("Sortierung", selection: Binding(get: { selectedSort }, set: { value in
                if collectionID != nil { sorting = value } else { trackSort = value.rawValue }
            })) {
                ForEach(OfflineSort.allCases.filter { collectionID != nil || $0 != .collection }, id: \.rawValue) { Text($0.title).tag($0) }
            }.accessibilityIdentifier("offline-sort")
        }
    }
    private func trackRow(_ record: OfflineTrack) -> some View {
        // One file check per row and render; each lookup stats the stored file.
        let available = model.offline.audioURL(record.track.id) != nil
        return HStack(spacing: 12) {
            Button { play(playable, selected: record.track.id) } label: {
                HStack(spacing: 12) {
                    ArtworkView(model: model, kind: "tracks", id: record.track.id).frame(width: 52, height: 52)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(record.track.title).font(.headline).lineLimit(1)
                        Text(record.track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        if !record.track.album.isEmpty { Text(record.track.album).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                        status(record, available: available)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain).disabled(!available)
                .accessibilityIdentifier("offline-track-" + record.track.id)
            Menu {
                Button("Als Nächstes abspielen", systemImage: "text.insert") {
                    if let client = model.client { model.player.insertNext(record.track, client: client) }
                }.disabled(!available || model.player.queue.tracks.count >= 500)
                Button("Zur Warteschlange hinzufügen", systemImage: "text.badge.plus") {
                    if let client = model.client { model.player.append(record.track, client: client) }
                }.disabled(!available || model.player.queue.tracks.count >= 500)
                if record.state != .ready {
                    if record.state == .downloading || record.state == .queued { Button("Pausieren", systemImage: "pause") { model.offline.pause(record.track.id) } }
                    else { Button("Erneut herunterladen", systemImage: "arrow.clockwise") { model.offline.enqueue([record.track]) }.disabled(model.offlineMode) }
                }
                Button("Lokale Kopie entfernen", systemImage: "trash", role: .destructive) { deleting = record.track.id; confirmDelete = true }
            } label: { Image(systemName: "ellipsis").frame(width: 44, height: 44) }.accessibilityLabel("Download-Aktionen für \(record.track.title)")
        }
    }
    private func play(_ tracks: [Track], selected: String? = nil) { if let client = model.client { model.player.play(tracks, start: selected.flatMap { id in tracks.firstIndex { $0.id == id } } ?? 0, client: client) } }
    @ViewBuilder private func status(_ record: OfflineTrack, available: Bool) -> some View {
        switch record.state {
        case .ready:
            Label(!available ? "Lokale Datei fehlt" : record.automatic ? "Automatisch gespeichert" : "Offline verfügbar",
                  systemImage: !available ? "exclamationmark.circle" : "checkmark.circle.fill")
                .font(.caption).foregroundStyle(!available ? .orange : .green)
        case .queued: Text("Wartet auf Download").font(.caption).foregroundStyle(.secondary)
        case .paused: Text("Pausiert · Fortsetzen über Aktionen").font(.caption).foregroundStyle(.secondary)
        case .failed: Text(record.failure ?? "Download fehlgeschlagen").font(.caption).foregroundStyle(.red)
        case .downloading:
            if record.expected > 0 { ProgressView(value: Double(record.bytes), total: Double(record.expected)) }
            Text("\(ByteCountFormatter.string(fromByteCount: record.bytes, countStyle: .file)) · wird geladen").font(.caption).foregroundStyle(.secondary)
        }
    }
}

struct DownloadPreferences: View {
    var model: AppModel
    @State private var clear = false
    var body: some View {
        Section("Downloads") {
            Toggle("Nur WLAN", isOn: Binding(get: { model.offline.wifiOnly }, set: { model.offline.wifiOnly = $0 }))
            Picker("Speicherlimit für Musik", selection: Binding(get: { model.offline.limitGB }, set: { model.offline.limitGB = $0 })) { ForEach([1, 2, 4, 8, 16, 32, 64, 128], id: \.self) { Text("\($0) GB").tag($0) } }
            Toggle("Gehörte Titel automatisch speichern", isOn: Binding(get: { model.offline.cachePlayed }, set: { model.offline.cachePlayed = $0 }))
            Toggle("Favoriten beim Aktualisieren speichern", isOn: Binding(get: { model.offline.syncFavorites }, set: { model.offline.syncFavorites = $0; if $0 { Task { await model.downloadCollection(.favorites) } } }))
                .disabled(model.offlineMode)
            Text("Manuell gespeicherte Musik bleibt erhalten. Automatisch gespeicherte Titel können bei vollem Speicher ersetzt werden. Downloads laufen nach Möglichkeit im Hintergrund; nach erzwungenem Beenden kann ein Neustart nötig sein. Änderungen an „Nur WLAN“ gelten für neue Transfers.").font(.footnote).foregroundStyle(.secondary)
            Text("\(ByteCountFormatter.string(fromByteCount: model.offline.totalBytes, countStyle: .file)) Musik auf diesem Gerät. Cover und Metadaten benötigen zusätzlich Speicher.").font(.footnote).foregroundStyle(.secondary)
            Button("Lokale Musik dieses Kontos entfernen", role: .destructive) { clear = true }
                .confirmationDialog("Offline-Musik dieses Kontos entfernen?", isPresented: $clear, titleVisibility: .visible) { Button("Lokale Kopien entfernen", role: .destructive) { model.player.stop(); model.offline.clearCurrent() } } message: { Text("Die Server-Bibliothek bleibt unverändert.") }
        }
    }
}

#endif

#if os(iOS)
struct MobilePlayerView: View {
    var model: AppModel
    @State private var tab = "Warteschlange"
    @State private var adding = false
    @State private var playlistSelection: [Track] = []
    @State private var queueQuery = ""
    @State private var tools = false
    @AppStorage("mobileCoverColors") private var coverColors = true
    @AppStorage("mobileVisualizerPlacement") private var placement = "overlay"
    @Environment(\.colorScheme) private var scheme
    private var accent: Color {
        guard coverColors, let color = model.player.artworkPalette?.accent.button else { return .pink }
        return Color(red: color.red, green: color.green, blue: color.blue)
    }
    private func coverSize(_ size: CGSize) -> CGFloat { min(360, max(180, min(size.width - 48, size.height * 0.38))) }
    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                if let track = model.player.current {
                    VStack(spacing: 22) {
                        ZStack(alignment: .bottom) {
                            if !model.player.visualizationEnabled || placement != "background" {
                                ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: coverSize(geometry.size), height: coverSize(geometry.size))
                                    .shadow(color: .black.opacity(0.18), radius: 20, y: 12)
                            }
                            if model.player.visualizationEnabled {
                                MobileSpectrum(player: model.player).frame(height: placement == "background" ? 300 : 100)
                                    .padding(18).allowsHitTesting(false).accessibilityHidden(true)
                            }
                        }.frame(maxWidth: .infinity)
                        HStack(alignment: .center, spacing: 12) {
                            VStack(alignment: .leading, spacing: 6) {
                                Text(track.title).font(.title2.bold()).lineLimit(3)
                                Text(track.artistText).font(.title3).foregroundStyle(.secondary)
                                if !track.album.isEmpty && track.album != track.title { Text(track.album).font(.caption).foregroundStyle(.secondary) }
                            }.frame(maxWidth: .infinity, alignment: .leading)
                            if !model.offlineMode {
                                Button { Task { await model.toggleFavorite(track) } } label: { Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel("Favorit umschalten").disabled(model.pendingFavorites.contains(track.id))
                            }
                            Menu {
                                if !model.offlineMode {
                                    Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; adding = true }
                                    Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
                                    Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }
                                    Button("Wiedergabe übergeben", systemImage: "arrow.up.forward.app") { Task { await model.saveHandoff() } }
                                }
                                Button("Klang & Wiedergabe", systemImage: "slider.horizontal.3") { tools = true }
                            } label: { Image(systemName: "ellipsis").font(.system(size: 20, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel("Titel-Aktionen")
                        }
                        VStack(spacing: 6) {
                            Slider(value: Binding(get: { min(model.player.position, model.player.duration) }, set: { model.player.seek($0) }), in: 0...max(1, model.player.duration)).accessibilityLabel("Wiedergabeposition")
                            HStack { Text(formatTime(model.player.position)).accessibilityIdentifier("playbackElapsed"); Spacer(); Text(formatTime(model.player.duration)) }.font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                        }
                        HStack(spacing: min(20, max(0, (geometry.size.width - 48 - 246) / 4))) {
                            Button { model.player.shuffle() } label: { Image(systemName: "shuffle").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel("Nächste Titel mischen").accessibilityIdentifier("mobile-player-shuffle")
                            Button { model.player.previous() } label: { Image(systemName: "backward.end.fill").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel("Vorheriger Titel").accessibilityIdentifier("mobile-player-previous")
                            Button { model.player.toggle() } label: { Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 30, weight: .semibold)).frame(width: 70, height: 70).background(accent, in: Circle()).foregroundStyle(.white).contentShape(Circle()) }.accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen").accessibilityIdentifier("mobile-player-toggle")
                            Button { model.player.next() } label: { Image(systemName: "forward.end.fill").font(.system(size: 22, weight: .semibold)).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel("Nächster Titel").accessibilityIdentifier("mobile-player-next")
                            Button {
                                if model.player.repeatOne { model.player.setRepeatOne(false); model.player.repeatAll = false }
                                else if model.player.repeatAll { model.player.setRepeatOne(true) }
                                else { model.player.repeatAll = true }
                            } label: { Image(systemName: model.player.repeatOne ? "repeat.1" : "repeat").font(.system(size: 22, weight: .semibold)).foregroundStyle(model.player.repeatAll || model.player.repeatOne ? Color.pink : Color.primary).frame(width: 44, height: 44).contentShape(Rectangle()) }.accessibilityLabel(model.player.repeatOne ? "Einzeltitel wiederholen" : model.player.repeatAll ? "Warteschlange wiederholen" : "Wiederholung aus").accessibilityIdentifier("mobile-player-repeat")
                        }.buttonStyle(.plain)
                        HStack { SystemVolumeSlider().frame(height: 36); AirPlayPicker().frame(width: 44, height: 44) }.accessibilityLabel("Lautstärke und Audioausgabe")
                        HStack {
                            Text(track.codec?.uppercased() ?? "Audio").font(.caption).foregroundStyle(.secondary)
                            if model.offline.audioURL(track.id) != nil { Label("Offline", systemImage: "checkmark.circle").font(.caption).foregroundStyle(.secondary) }
                            Spacer()
                            Button("Werkzeuge", systemImage: "slider.horizontal.3") { tools = true }.font(.subheadline)
                        }
                        if model.player.loading { ProgressView("Titel wird geladen …") }
                        if let error = model.player.error { Text(error).font(.callout).foregroundStyle(.secondary); Button("Erneut versuchen") { model.player.resume() } }
                        Picker("Player-Bereich", selection: $tab) { ForEach(["Warteschlange", "Lyrics"], id: \.self) { Text($0).tag($0) } }.pickerStyle(.segmented)
                        if tab == "Lyrics" { MobileLyricsView(player: model.player) }
                        else { mobileQueue }
                    }.padding(24).frame(maxWidth: 760).frame(maxWidth: .infinity)
                } else { ContentUnavailableView("Musik auswählen", systemImage: "play.circle", description: Text("Öffne ein Album oder eine Playlist.")) }
            }.background {
                if coverColors, let palette = model.player.artworkPalette {
                    LinearGradient(colors: [Color(red: palette.dominant.red, green: palette.dominant.green, blue: palette.dominant.blue).opacity(scheme == .dark ? 0.32 : 0.13), Color.primary.opacity(0.02)], startPoint: .topLeading, endPoint: .bottomTrailing).ignoresSafeArea()
                }
            }
        }.navigationTitle("Jetzt läuft").navigationBarTitleDisplayMode(.inline)
        .sheet(isPresented: $tools) { NavigationStack { MobileAudioSettings(model: model).toolbar { ToolbarItem(placement: .confirmationAction) { Button("Fertig") { tools = false } } } } }
        .sheet(isPresented: $adding) { AddToPlaylistSheet(model: model, tracks: playlistSelection) }
    }
    private var mobileQueue: some View {
        LazyVStack(spacing: 8) {
            HStack {
                Text("\(model.player.queue.tracks.count) Titel").font(.subheadline).foregroundStyle(.secondary)
                Spacer()
                Menu("Warteschlange", systemImage: "ellipsis.circle") {
                    if !model.offlineMode {
                        Button("Als Playlist speichern", systemImage: "music.note.list") { playlistSelection = model.player.queue.tracks; adding = true }
                    }
                    Button("Nächste leeren") { model.player.clearUpcoming() }.disabled(model.player.queue.index + 1 >= model.player.queue.tracks.count)
                    Button("Leeren und stoppen", role: .destructive) { model.player.clearQueue() }
                }.accessibilityIdentifier("mobile-queue-tools")
            }
            TextField("Warteschlange filtern", text: $queueQuery).textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("mobile-queue-filter")
            ForEach(Array(model.player.queue.tracks.enumerated()).filter {
                queueQuery.isEmpty || [$0.element.title, $0.element.artistText, $0.element.album].joined(separator: " ").localizedCaseInsensitiveContains(queueQuery)
            }, id: \.offset) { index, track in
                HStack(spacing: 12) {
                    Button { model.player.select(index) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 48, height: 48)
                            VStack(alignment: .leading) { Text(track.title).font(.headline).lineLimit(1); Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                            Spacer()
                            if index == model.player.queue.index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(.pink) }
                        }.contentShape(Rectangle())
                    }.buttonStyle(.plain).accessibilityIdentifier("mobile-queue-track-\(index)")
                    Menu {
                        Button("Nach oben", systemImage: "arrow.up") { model.player.moveInQueue(from: index, to: index - 1) }.disabled(index == 0)
                        Button("Nach unten", systemImage: "arrow.down") { model.player.moveInQueue(from: index, to: index + 1) }.disabled(index + 1 >= model.player.queue.tracks.count)
                        if !model.offlineMode {
                            Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; adding = true }
                            Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen", systemImage: "heart") { Task { await model.toggleFavorite(track) } }.disabled(model.pendingFavorites.contains(track.id))
                        }
                        Button("Als Nächstes", systemImage: "text.insert") { model.player.playNextInQueue(index) }.disabled(index <= model.player.queue.index + 1)
                        Button("Aus Warteschlange entfernen", systemImage: "minus.circle", role: .destructive) { model.player.removeFromQueue(index) }.disabled(index == model.player.queue.index)
                        if !model.offlineMode { Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) } }
                    } label: { Image(systemName: "ellipsis").frame(width: 44, height: 44) }.accessibilityLabel("Warteschlangen-Aktionen für \(track.title)").accessibilityIdentifier("mobile-queue-actions-\(index)")
                }.padding(10).background(index == model.player.queue.index ? Color.pink.opacity(0.08) : .clear, in: RoundedRectangle(cornerRadius: 14))
            }
        }
    }
}

struct SystemVolumeSlider: UIViewRepresentable {
    func makeUIView(context: Context) -> MPVolumeView { let view = MPVolumeView(); return view }
    func updateUIView(_ uiView: MPVolumeView, context: Context) { }
}

#endif

struct MobileLyricsView: View {
    var player: PlayerModel
    var viewportHeight: CGFloat = 360
    @State private var following = true
    @State private var timeline = LyricsTimeline("")
    var body: some View {
        Group {
        if timeline.lines.isEmpty { Text(cleanLyrics(player.lyrics)).font(.title3).lineSpacing(12).frame(maxWidth: .infinity, alignment: .leading) }
        else {
            let lines = timeline.lines
            let active = timeline.activeIndex(at: player.position)
            Toggle("Lyrics folgen", isOn: $following).font(.subheadline)
            ScrollViewReader { reader in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 20) {
                        ForEach(lines) { line in
                            Button { player.seek(line.seconds) } label: { Text(line.text.isEmpty ? "♪" : line.text).font(.title2.bold()).foregroundStyle(active == line.id ? Color.primary : Color.secondary).frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, 8) }
                                .buttonStyle(.plain).id(line.id).accessibilityLabel("\(line.text), ab \(formatTime(line.seconds))")
                        }
                    }.padding(.vertical, 24)
                }.frame(height: viewportHeight)
                .onChange(of: active) { if following, let active { withAnimation(.easeInOut(duration: 0.3)) { reader.scrollTo(active, anchor: .center) } } }
            }
        }
        }.onChange(of: player.lyrics, initial: true) { timeline = LyricsTimeline(player.lyrics) }
    }
}

#if os(iOS)
struct MobileAudioSettings: View {
    var model: AppModel
    @AppStorage("mobileVisualizerStyle") private var style = "bars"
    @AppStorage("mobileVisualizerPlacement") private var placement = "overlay"
    @AppStorage("mobileVisualizerColor") private var color = "cover"
    @AppStorage("mobileVisualizerCustom") private var custom = "FF3476"
    @AppStorage("mobileCoverColors") private var coverColors = true
    var body: some View {
        Form {
            Section("Lautstärke") {
                Toggle("Lautstärke-Normalisierung", isOn: Binding(get: { model.player.normalizationEnabled }, set: { model.player.setNormalization($0) }))
                    .accessibilityIdentifier("playback-normalization")
                if model.player.normalizationEnabled {
                    Text(model.player.normalizationMessage).font(.footnote).foregroundStyle(.secondary)
                    Button("Lautheit erneut prüfen") { model.player.setNormalization(true) }.disabled(model.offlineMode)
                }
            }
            Section("Equalizer") {
                Toggle("Equalizer aktivieren", isOn: Binding(get: { model.player.equalizer.enabled }, set: { model.player.equalizer.setEnabled($0) }))
                Picker("Klangprofil", selection: Binding(get: { model.player.equalizer.preset }, set: { if let preset = EqualizerPreset(rawValue: $0) { model.player.equalizer.select(preset) } })) {
                    ForEach(EqualizerPreset.allCases) { Text($0.name).tag($0.rawValue) }
                    if model.player.equalizer.preset == "custom" { Text("Eigener Klang").tag("custom") }
                }
                ForEach(0..<10) { band in
                    VStack(alignment: .leading, spacing: 4) {
                        HStack { Text("\(EqualizerModel.frequencies[band], specifier: "%g") Hz"); Spacer(); Text("\(model.player.equalizer.gains[band], specifier: "%+.1f") dB").foregroundStyle(.secondary).monospacedDigit() }
                        Slider(value: Binding(get: { model.player.equalizer.gains[band] }, set: { model.player.equalizer.setGain($0, band: band) }), in: -12...12, step: 0.5).accessibilityLabel("\(EqualizerModel.frequencies[band]) Hertz")
                    }
                }
                Toggle("Automatischer EQ-Pegelschutz", isOn: Binding(get: { model.player.equalizer.headroom }, set: { model.player.equalizer.setHeadroom($0) }))
                HStack { Text("Vorverstärkung"); Spacer(); Text("\(model.player.equalizer.preamp, specifier: "%+.1f") dB").monospacedDigit() }
                Slider(value: Binding(get: { model.player.equalizer.preamp }, set: { model.player.equalizer.setPreamp($0) }), in: -12...6, step: 0.5)
                Button("Klang zurücksetzen") { model.player.equalizer.select(.flat); model.player.equalizer.setPreamp(0) }
                if let soundError = model.player.soundError { Text(soundError).font(.footnote).foregroundStyle(.secondary) }
            }
            Section("Wiedergabe") {
                Toggle("Nach der Warteschlange ähnliche Musik", isOn: Binding(get: { model.player.autoplay }, set: { model.player.setAutoplay($0) }))
                HStack { Text("Überblendung"); Spacer(); Text(model.player.crossfadeSeconds == 0 ? "Aus" : "\(Int(model.player.crossfadeSeconds)) s") }
                Slider(value: Binding(get: { model.player.crossfadeSeconds }, set: { model.player.setCrossfade($0) }), in: 0...12, step: 1)
                Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { model.player.smartAlbumTransition }, set: { model.player.setSmartAlbumTransition($0) }))
                Toggle("Nächsten Titel vorladen", isOn: Binding(get: { model.player.preloadEnabled }, set: { model.player.setPreload($0) }))
                Toggle("Schneller Abspielstart", isOn: Binding(get: { model.player.fastStart }, set: { model.player.setFastStart($0) }))
                Picker("Sleep-Timer", selection: Binding(get: { model.player.sleepMode }, set: { model.player.setSleepMode($0) })) { ForEach(SleepMode.allCases) { Text($0.name).tag($0) } }
                Picker("Tempo", selection: Binding(get: { model.player.playbackRate }, set: { model.player.setPlaybackRate($0) })) { ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 1.75, 2.0], id: \.self) { Text("\($0, specifier: "%g")×").tag($0) } }
            }
            Section("Visualizer") {
                Toggle("Visualizer aktivieren", isOn: Binding(get: { model.player.visualizationEnabled }, set: { model.player.setVisualization($0) }))
                Picker("Stil", selection: $style) { Text("Spektrum").tag("bars"); Text("Spiegel-Spektrum").tag("mirror"); Text("Ringe").tag("rings"); Text("Lichtpunkte").tag("dots") }
                Picker("Darstellung", selection: $placement) { Text("Auf dem Cover").tag("overlay"); Text("Ohne Cover").tag("background") }
                Picker("Farben", selection: $color) { Text("Cover").tag("cover"); Text("Rose").tag("rose"); Text("Ozean").tag("ocean"); Text("Wald").tag("forest"); Text("Lavendel").tag("violet"); Text("Eigene Farbe").tag("custom") }
                if color == "custom" { ColorPicker("Eigene Farbe", selection: Binding(get: { Self.customColor(custom) }, set: { custom = Self.hex($0) }), supportsOpacity: false) }
                Toggle("Player-Farben aus dem Cover", isOn: $coverColors)
                Text("Die Anzeige reagiert auf die echte Frequenzanalyse der Musik. Ohne EQ, Visualizer und Normalisierung entfällt die Klangverarbeitung.").font(.footnote).foregroundStyle(.secondary)
            }
        }.navigationTitle("Klang & Wiedergabe")
    }
    static func customColor(_ hex: String) -> Color {
        let value = UInt32(hex, radix: 16) ?? 0xFF3476
        return Color(red: Double((value >> 16) & 255) / 255, green: Double((value >> 8) & 255) / 255, blue: Double(value & 255) / 255)
    }
    static func hex(_ color: Color) -> String {
        var r: CGFloat = 1, g: CGFloat = 0, b: CGFloat = 0, a: CGFloat = 1
        UIColor(color).getRed(&r, green: &g, blue: &b, alpha: &a)
        return String(format: "%02X%02X%02X", Int(r * 255), Int(g * 255), Int(b * 255))
    }
}

struct MobileSpectrum: View {
    var player: PlayerModel
    @AppStorage("mobileVisualizerStyle") private var style = "bars"
    @AppStorage("mobileVisualizerColor") private var colorName = "cover"
    @AppStorage("mobileVisualizerCustom") private var custom = "FF3476"
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private var color: Color {
        if colorName == "custom" { return MobileAudioSettings.customColor(custom) }
        if colorName == "cover", let color = player.artworkPalette?.accent { return Color(red: color.red, green: color.green, blue: color.blue).mix(with: .white, by: 0.25) }
        return (DesktopTheme(rawValue: colorName) ?? .rose).accent
    }
    var body: some View {
        let levels = reduceMotion ? Array(repeating: 0.05, count: 32) : player.spectrumLevels
        Canvas { context, size in
            let bandWidth = size.width / CGFloat(levels.count)
            for (index, level) in levels.enumerated() {
                let height = max(2, size.height * min(1, max(0, level)))
                let x = CGFloat(index) * bandWidth
                if style == "rings" {
                    if index % 4 == 0 {
                        let radius = min(size.width, size.height) * (0.1 + Double(index) / 64) + level * 12
                        let rect = CGRect(x: size.width / 2 - radius, y: size.height / 2 - radius, width: radius * 2, height: radius * 2)
                        context.stroke(Path(ellipseIn: rect), with: .color(color.opacity(0.3 + level * 0.7)), lineWidth: 2 + level * 4)
                    }
                } else if style == "dots" {
                    for row in 0..<max(1, Int(height / 10)) {
                        let rect = CGRect(x: x + 2, y: size.height - CGFloat(row + 1) * 10, width: max(2, bandWidth - 4), height: 6)
                        context.fill(Path(ellipseIn: rect), with: .color(color.opacity(0.6 + level * 0.4)))
                    }
                } else {
                    let h = style == "mirror" ? height / 2 : height
                    let y = style == "mirror" ? (size.height - h) / 2 : size.height - h
                    let rect = CGRect(x: x + 1.5, y: y, width: max(1, bandWidth - 3), height: h)
                    context.fill(Path(roundedRect: rect, cornerRadius: min(4, bandWidth / 2)), with: .linearGradient(Gradient(colors: [color.mix(with: .white, by: 0.35), color]), startPoint: .zero, endPoint: CGPoint(x: 0, y: size.height)))
                }
            }
        }
    }
}
#endif
