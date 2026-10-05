import SwiftUI
import YTMDLCore
#if os(iOS)
import UIKit
import MediaPlayer

struct MobileHomeView: View {
    var model: AppModel
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                VStack(alignment: .leading, spacing: 10) {
                    Text("Hallo, \(model.user?.displayName ?? "")").font(.subheadline).foregroundStyle(.secondary)
                    Text("Deine Musik.\nDein Moment.").font(.largeTitle.bold())
                    HStack {
                        Button("Favoriten-Mix", systemImage: "heart.fill") { Task { await model.playMix("Favoriten") } }.buttonStyle(.borderedProminent)
                        NavigationLink(value: Destination.downloads) { Label("Offline", systemImage: "arrow.down.circle") }.buttonStyle(.bordered)
                    }.disabled(model.listeningBusy)
                }.padding(24).frame(maxWidth: .infinity, alignment: .leading).background(.tint.opacity(0.10), in: RoundedRectangle(cornerRadius: 24))
                if model.listeningHistory.snapshot != nil {
                    Button("Letzte Wiedergabe fortsetzen", systemImage: "play.circle.fill") { model.resumeLastSession() }.buttonStyle(.bordered)
                }
                HStack {
                    NavigationLink(value: CollectionKind.favorites) { Label("Lieblingstitel", systemImage: "heart") }
                    Spacer()
                    NavigationLink(value: Destination.artists) { Label("Künstler", systemImage: "person.2") }
                }.buttonStyle(.bordered)
                if !model.listeningHistory.tracks.isEmpty {
                    Text("Zuletzt gehört").font(.title2.bold())
                    ForEach(Array(model.listeningHistory.tracks.prefix(6))) { track in
                        TrackRow(model: model, track: track) { if let client = model.client { model.player.play(model.listeningHistory.tracks, start: model.listeningHistory.tracks.firstIndex(of: track) ?? 0, client: client) } }
                    }
                }
                Text("Für deinen Moment").font(.title2.bold())
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 14) {
                    mix("Favoriten", subtitle: "Deine Lieblingstitel", symbol: "heart.fill")
                    mix("Neu", subtitle: "Frisch in der Bibliothek", symbol: "sparkles")
                    ForEach(Array(model.genres.filter { $0 != "__none__" }.prefix(6)), id: \.self) { genre in mix(genre, subtitle: "Genre-Mix", symbol: "waveform", genre: genre) }
                }
                Text("Neu in deiner Bibliothek").font(.title2.bold())
                ScrollView(.horizontal) {
                    LazyHStack(alignment: .top, spacing: 16) {
                        ForEach(Array(model.releases.prefix(16))) { release in
                            NavigationLink(value: CollectionKind.release(release)) {
                                VStack(alignment: .leading, spacing: 8) {
                                    ArtworkView(model: model, kind: "releases", id: release.id).frame(width: 160, height: 160)
                                    Text(release.title).font(.headline).lineLimit(2)
                                    Text(release.artists.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                                }.frame(width: 160, alignment: .leading)
                            }.buttonStyle(.plain)
                        }
                    }
                }.scrollIndicators(.hidden)
                if !model.playlists.isEmpty {
                    Text("Deine Playlists").font(.title2.bold())
                    ForEach(Array(model.playlists.prefix(6))) { playlist in
                        NavigationLink(value: CollectionKind.playlist(playlist)) {
                            HStack(spacing: 14) {
                                Image(systemName: playlist.smartRules == nil ? "music.note.list" : "sparkles").font(.title2).frame(width: 48, height: 48).background(.tint.opacity(0.1), in: RoundedRectangle(cornerRadius: 12))
                                VStack(alignment: .leading) { Text(playlist.name).font(.headline); Text("\(playlist.trackCount) Titel").font(.caption).foregroundStyle(.secondary) }
                                Spacer(); Image(systemName: "chevron.right").foregroundStyle(.secondary)
                            }.padding(12).background(.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 16))
                        }.buttonStyle(.plain)
                    }
                }
                NavigationLink(value: Destination.settings) { Label("Klang, Downloads & Einstellungen", systemImage: "slider.horizontal.3") }
            }.padding(20).frame(maxWidth: 1000).frame(maxWidth: .infinity)
        }.navigationTitle("Start").refreshable { await model.loadLibrary() }
    }
    private func mix(_ name: String, subtitle: String, symbol: String, genre: String = "") -> some View {
        Button { Task { await model.playMix(name, genre: genre) } } label: {
            VStack(alignment: .leading, spacing: 12) {
                Image(systemName: symbol).font(.title2)
                Text(name).font(.headline).lineLimit(1)
                Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(2)
            }.padding(18).frame(maxWidth: .infinity, minHeight: 130, alignment: .leading)
                .background(.tint.opacity(0.08), in: RoundedRectangle(cornerRadius: 18))
        }.buttonStyle(.plain).disabled(model.listeningBusy)
    }
}

struct OfflineLibraryView: View {
    var model: AppModel
    @State private var query = ""
    @State private var onlyReady = false
    @State private var sorting = "date"
    @State private var collectionID = ""
    @State private var deleting: String?
    @State private var confirmDelete = false
    @State private var options = false
    @State private var player = false
    private var records: [OfflineTrack] {
        let collection = model.offline.currentCollections.first { $0.id == collectionID }
        let ids = collection.map { Set($0.trackIDs) }
        let filtered = model.offline.currentRecords.filter {
            (ids == nil || ids!.contains($0.track.id)) &&
            (!onlyReady || $0.state == .ready) && (query.isEmpty || ($0.track.title + " " + $0.track.artistText + " " + $0.track.album).localizedCaseInsensitiveContains(query))
        }
        if sorting == "collection", let collection {
            var positions: [String: Int] = [:]
            for (index, id) in collection.trackIDs.enumerated() where positions[id] == nil { positions[id] = index }
            return filtered.sorted { (positions[$0.track.id] ?? Int.max) < (positions[$1.track.id] ?? Int.max) }
        }
        return sorting == "title" ? filtered.sorted { $0.track.title.localizedStandardCompare($1.track.title) == .orderedAscending } : filtered.sorted { $0.date > $1.date }
    }
    private var playable: [Track] { model.offline.availableTracks(in: records) }
    var body: some View {
        List {
            Section {
                Label(model.offlineMode ? "Offline-Modus · keine Server-Anfragen" : "Auf diesem Gerät", systemImage: "iphone")
                Text("\(ByteCountFormatter.string(fromByteCount: model.offline.usedBytes, countStyle: .file)) gespeichert · \(model.offline.readyTracks.count) Titel").font(.subheadline).foregroundStyle(.secondary)
                HStack {
                    Button("Abspielen", systemImage: "play.fill") { play(playable) }.buttonStyle(.borderedProminent)
                    Button("Zufall", systemImage: "shuffle") { play(playable.shuffled()) }.buttonStyle(.bordered)
                }.disabled(playable.isEmpty)
                if !model.offline.currentCollections.isEmpty {
                    Picker("Sammlung", selection: $collectionID) {
                        Text("Alle Offline-Titel").tag("")
                        ForEach(model.offline.currentCollections) { Text($0.name).tag($0.id) }
                    }
                    if let collection = model.offline.currentCollections.first(where: { $0.id == collectionID }) {
                        Toggle("Beim Bibliothek-Aktualisieren synchronisieren", isOn: Binding(get: { collection.keepUpdated }, set: { model.offline.setKeepUpdated(collection.id, enabled: $0) }))
                            .disabled(model.offlineMode)
                    }
                }
                Toggle("Nur fertige Downloads", isOn: $onlyReady)
                Picker("Sortierung", selection: $sorting) {
                    Text("Zuletzt hinzugefügt").tag("date"); Text("Titel A–Z").tag("title")
                    if !collectionID.isEmpty { Text("Reihenfolge der Sammlung").tag("collection") }
                }
            }
            if records.isEmpty { ContentUnavailableView("Noch keine Offline-Musik", systemImage: "arrow.down.circle", description: Text("Öffne einen Titel, ein Album oder eine Playlist und wähle „Offline speichern“.")) }
            ForEach(records) { record in
                HStack(spacing: 12) {
                    Button { play(playable, selected: record.track.id) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(model: model, kind: "tracks", id: record.track.id).frame(width: 52, height: 52)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(record.track.title).font(.headline).lineLimit(1)
                                Text(record.track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                                status(record)
                            }.frame(maxWidth: .infinity, alignment: .leading)
                        }.contentShape(Rectangle())
                    }.buttonStyle(.plain).disabled(record.state != .ready)
                    Menu {
                        if record.state != .ready {
                            if record.state == .downloading || record.state == .queued { Button("Pausieren", systemImage: "pause") { model.offline.pause(record.track.id) } }
                            else { Button("Erneut herunterladen", systemImage: "arrow.clockwise") { model.offline.enqueue([record.track]) }.disabled(model.offlineMode) }
                        }
                        Button("Lokale Kopie entfernen", systemImage: "trash", role: .destructive) { deleting = record.track.id; confirmDelete = true }
                    } label: { Image(systemName: "ellipsis").frame(width: 44, height: 44) }.accessibilityLabel("Download-Aktionen für \(record.track.title)")
                }
            }
            if let error = model.offline.error { Section { Text(error).foregroundStyle(.red); Button("Meldung schließen") { model.offline.error = nil } } }
            if model.offlineMode { Section { Button("Zur Anmeldung", systemImage: "network") { Task { await model.logout() } } } }
        }.navigationTitle("Offline-Musik")
        .onChange(of: collectionID) { _, value in sorting = value.isEmpty ? "date" : "collection" }
        .searchable(text: $query, prompt: "Offline-Titel, Künstler, Alben")
        .toolbar {
            ToolbarItem { Button("Download-Einstellungen", systemImage: "gearshape") { options = true } }
            if model.offlineMode && model.player.current != nil { ToolbarItem { Button("Player", systemImage: "play.circle") { player = true } } }
        }
        .sheet(isPresented: $options) { NavigationStack { Form { DownloadPreferences(model: model) }.navigationTitle("Downloads").toolbar { ToolbarItem(placement: .confirmationAction) { Button("Fertig") { options = false } } } } }
        .sheet(isPresented: $player) { NavigationStack { MobilePlayerView(model: model).toolbar { ToolbarItem(placement: .cancellationAction) { Button("Schließen") { player = false } } } } }
        .confirmationDialog("Lokale Kopie entfernen?", isPresented: $confirmDelete, titleVisibility: .visible) {
            Button("Vom iPhone entfernen", role: .destructive) { if let deleting { model.offline.remove(deleting) } }
        } message: { Text("Der Titel bleibt auf deinem Musikserver erhalten.") }
    }
    private func play(_ tracks: [Track], selected: String? = nil) { if let client = model.client { model.player.play(tracks, start: selected.flatMap { id in tracks.firstIndex { $0.id == id } } ?? 0, client: client) } }
    @ViewBuilder private func status(_ record: OfflineTrack) -> some View {
        switch record.state {
        case .ready: Label(record.automatic ? "Automatisch gespeichert" : "Offline verfügbar", systemImage: "checkmark.circle.fill").font(.caption).foregroundStyle(.green)
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

struct MobilePlayerView: View {
    var model: AppModel
    @State private var tab = "Warteschlange"
    @State private var adding = false
    @State private var tools = false
    @AppStorage("mobileCoverColors") private var coverColors = true
    @AppStorage("mobileVisualizerPlacement") private var placement = "overlay"
    @Environment(\.colorScheme) private var scheme
    private var accent: Color {
        guard coverColors, let color = model.player.artworkPalette?.accent.button else { return .pink }
        return Color(red: color.red, green: color.green, blue: color.blue)
    }
    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                if let track = model.player.current {
                    VStack(spacing: 22) {
                        ZStack(alignment: .bottom) {
                            if !model.player.visualizationEnabled || placement != "background" {
                                ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: min(400, max(160, geometry.size.width - 64)), height: min(400, max(160, geometry.size.width - 64)))
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
                                Button { Task { await model.toggleFavorite(track) } } label: { Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart").font(.title2).frame(width: 44, height: 44) }.accessibilityLabel("Favorit umschalten")
                            }
                            Menu {
                                if !model.offlineMode {
                                    Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { adding = true }
                                    Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
                                    Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }
                                    Button("Wiedergabe übergeben", systemImage: "arrow.up.forward.app") { Task { await model.saveHandoff() } }
                                }
                                Button("Klang & Wiedergabe", systemImage: "slider.horizontal.3") { tools = true }
                            } label: { Image(systemName: "ellipsis").frame(width: 44, height: 44) }.accessibilityLabel("Titel-Aktionen")
                        }
                        VStack(spacing: 6) {
                            Slider(value: Binding(get: { min(model.player.position, model.player.duration) }, set: { model.player.seek($0) }), in: 0...max(1, model.player.duration)).accessibilityLabel("Wiedergabeposition")
                            HStack { Text(formatTime(model.player.position)).accessibilityIdentifier("playbackElapsed"); Spacer(); Text(formatTime(model.player.duration)) }.font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                        }
                        HStack(spacing: 20) {
                            Button { model.player.shuffle() } label: { Image(systemName: "shuffle").frame(width: 44, height: 44) }.accessibilityLabel("Nächste Titel mischen")
                            Button { model.player.previous() } label: { Image(systemName: "backward.end.fill").font(.title2).frame(width: 44, height: 44) }.accessibilityLabel("Vorheriger Titel")
                            Button { model.player.toggle() } label: { Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").font(.title).frame(width: 70, height: 70).background(accent, in: Circle()).foregroundStyle(.white) }.accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen")
                            Button { model.player.next() } label: { Image(systemName: "forward.end.fill").font(.title2).frame(width: 44, height: 44) }.accessibilityLabel("Nächster Titel")
                            Button {
                                if model.player.repeatOne { model.player.setRepeatOne(false); model.player.repeatAll = false }
                                else if model.player.repeatAll { model.player.setRepeatOne(true) }
                                else { model.player.repeatAll = true }
                            } label: { Image(systemName: model.player.repeatOne ? "repeat.1" : "repeat").foregroundStyle(model.player.repeatAll || model.player.repeatOne ? Color.pink : Color.primary).frame(width: 44, height: 44) }.accessibilityLabel(model.player.repeatOne ? "Einzeltitel wiederholen" : model.player.repeatAll ? "Warteschlange wiederholen" : "Wiederholung aus")
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
        .sheet(isPresented: $adding) { AddToPlaylistSheet(model: model, tracks: model.player.current.map { [$0] } ?? []) }
    }
    private var mobileQueue: some View {
        VStack(spacing: 8) {
            HStack { Text("\(model.player.queue.tracks.count) Titel").font(.subheadline).foregroundStyle(.secondary); Spacer(); Button("Nächste leeren") { model.player.clearUpcoming() }.font(.caption) }
            ForEach(Array(model.player.queue.tracks.enumerated()), id: \.offset) { index, track in
                HStack(spacing: 12) {
                    Button { model.player.select(index) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 48, height: 48)
                            VStack(alignment: .leading) { Text(track.title).font(.headline).lineLimit(1); Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                            Spacer()
                            if index == model.player.queue.index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(.pink) }
                        }.contentShape(Rectangle())
                    }.buttonStyle(.plain)
                    Menu {
                        Button("Als Nächstes", systemImage: "text.insert") { model.player.playNextInQueue(index) }.disabled(index <= model.player.queue.index + 1)
                        Button("Aus Warteschlange entfernen", systemImage: "minus.circle", role: .destructive) { model.player.removeFromQueue(index) }.disabled(index == model.player.queue.index)
                        if !model.offlineMode { Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) } }
                    } label: { Image(systemName: "ellipsis").frame(width: 44, height: 44) }.accessibilityLabel("Warteschlangen-Aktionen für \(track.title)")
                }.padding(10).background(index == model.player.queue.index ? Color.pink.opacity(0.08) : .clear, in: RoundedRectangle(cornerRadius: 14))
            }
        }
    }
}

struct SystemVolumeSlider: UIViewRepresentable {
    func makeUIView(context: Context) -> MPVolumeView { let view = MPVolumeView(); return view }
    func updateUIView(_ uiView: MPVolumeView, context: Context) { }
}

struct MobileLyricsView: View {
    var player: PlayerModel
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
                }.frame(height: 360)
                .onChange(of: active) { if following, let active { withAnimation(.easeInOut(duration: 0.3)) { reader.scrollTo(active, anchor: .center) } } }
            }
        }
        }.onChange(of: player.lyrics, initial: true) { timeline = LyricsTimeline(player.lyrics) }
    }
}

struct MobileAudioSettings: View {
    var model: AppModel
    @AppStorage("mobileVisualizerStyle") private var style = "bars"
    @AppStorage("mobileVisualizerPlacement") private var placement = "overlay"
    @AppStorage("mobileVisualizerColor") private var color = "cover"
    @AppStorage("mobileVisualizerCustom") private var custom = "FF3476"
    @AppStorage("mobileCoverColors") private var coverColors = true
    var body: some View {
        Form {
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
                Picker("Tempo", selection: Binding(get: { model.player.playbackRate }, set: { model.player.setPlaybackRate($0) })) { ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0], id: \.self) { Text("\($0, specifier: "%g")×").tag($0) } }
            }
            Section("Visualizer") {
                Toggle("Visualizer aktivieren", isOn: Binding(get: { model.player.visualizationEnabled }, set: { model.player.setVisualization($0) }))
                Picker("Stil", selection: $style) { Text("Spektrum").tag("bars"); Text("Spiegel-Spektrum").tag("mirror"); Text("Ringe").tag("rings"); Text("Lichtpunkte").tag("dots") }
                Picker("Darstellung", selection: $placement) { Text("Auf dem Cover").tag("overlay"); Text("Ohne Cover").tag("background") }
                Picker("Farben", selection: $color) { Text("Cover").tag("cover"); Text("Rose").tag("rose"); Text("Ozean").tag("ocean"); Text("Wald").tag("forest"); Text("Lavendel").tag("violet"); Text("Eigene Farbe").tag("custom") }
                if color == "custom" { ColorPicker("Eigene Farbe", selection: Binding(get: { Self.customColor(custom) }, set: { custom = Self.hex($0) }), supportsOpacity: false) }
                Toggle("Player-Farben aus dem Cover", isOn: $coverColors)
                Text("Die Anzeige reagiert auf die echte Frequenzanalyse der Musik. Ohne EQ und Visualizer entfällt die Klangverarbeitung.").font(.footnote).foregroundStyle(.secondary)
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
