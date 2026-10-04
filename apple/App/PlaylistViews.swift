import SwiftUI
import YTMDLCore

// Music collections use the existing authenticated APIs. No download or server
// administration actions belong in these editors.
struct PlaylistEditor: View {
    var model: AppModel
    var playlist: Playlist?
    var saved: (Playlist) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var name: String
    @State private var notes: String
    @State private var intelligent: Bool
    @State private var rules: SmartPlaylistRules
    @State private var failure: String?
    @State private var working = false
    @State private var artistPicker = false
    @State private var selectedArtistName: String?
    init(model: AppModel, playlist: Playlist? = nil, saved: @escaping (Playlist) -> Void = { _ in }) {
        self.model = model; self.playlist = playlist; self.saved = saved
        _name = State(initialValue: playlist?.name ?? "")
        _notes = State(initialValue: playlist?.description ?? "")
        _intelligent = State(initialValue: playlist?.smartRules != nil)
        _rules = State(initialValue: playlist?.smartRules ?? SmartPlaylistRules())
    }
    private var validName: Bool {
        let value = name.trimmingCharacters(in: .whitespacesAndNewlines)
        return !value.isEmpty && value.utf8.count <= 128
    }
    var body: some View {
        NavigationStack {
            Form {
                Section("Deine Playlist") {
                    TextField("Name", text: $name).accessibilityIdentifier("playlistName")
                    TextField("Beschreibung (optional)", text: $notes)
                    if !name.isEmpty && !validName { Text("Bitte einen kürzeren Namen wählen.").foregroundStyle(.secondary) }
                }
                Section {
                    Toggle("Intelligente Playlist", isOn: $intelligent)
                    Text(intelligent ? "Der Server stellt die Titel bei jedem Öffnen anhand deiner Regeln zusammen. Alle Filter gelten gemeinsam." : "Du bestimmst die Titel und ihre Reihenfolge selbst.")
                        .font(.callout).foregroundStyle(.secondary)
                    if playlist?.smartRules != nil && !intelligent {
                        Text("Beim Ausschalten gelten wieder die früher manuell gespeicherten Titel. Die aktuelle automatische Auswahl wird dabei nicht kopiert.")
                            .font(.callout).foregroundStyle(.secondary)
                    }
                }
                if intelligent { ruleFields }
                if let failure { Section { Text(failure).foregroundStyle(.red).accessibilityIdentifier("playlistSaveError") } }
            }
            .formStyle(.grouped).disabled(working)
            .navigationTitle(playlist == nil ? "Neue Playlist" : "Playlist bearbeiten")
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Abbrechen") { dismiss() }.disabled(working) }
                ToolbarItem(placement: .confirmationAction) {
                    Button(working ? "Speichern …" : "Speichern") { Task { await save() } }
                        .disabled(!validName || working || model.playlistBusy).accessibilityIdentifier("playlistSave")
                }
            }
            .sheet(isPresented: $artistPicker) {
                PlaylistArtistPicker(model: model, selected: rules.artistId) { rules.artistId = $0?.id; selectedArtistName = $0?.name }
            }
        }.playlistSheetFrame().interactiveDismissDisabled(working)
    }
    private var ruleFields: some View {
        Group {
            Section("Schnellstart") {
                ViewThatFits(in: .horizontal) {
                    HStack { presets }
                    VStack(alignment: .leading) { presets }
                }
            }
            Section("Regeln") {
                Picker("Genre", selection: Binding(get: { rules.genre ?? "" }, set: { rules.genre = $0.isEmpty ? nil : $0 })) {
                    Text("Alle Genres").tag("")
                    ForEach(Array(Set(model.genres + [rules.genre ?? ""]).subtracting([""])).sorted(), id: \.self) { Text($0).tag($0) }
                }
                Button { artistPicker = true } label: {
                    HStack { Text("Künstler"); Spacer(); Text(artistName).foregroundStyle(.secondary); Image(systemName: "chevron.right") }
                }.buttonStyle(.plain)
                Toggle("Nur Lieblingstitel", isOn: $rules.favorites)
                Picker("Hinzugefügt", selection: $rules.addedDays) {
                    Text("Jederzeit").tag(0); Text("Letzte 7 Tage").tag(7); Text("Letzte 30 Tage").tag(30); Text("Letzte 90 Tage").tag(90)
                    if ![0, 7, 30, 90].contains(rules.addedDays) { Text("Letzte \(rules.addedDays) Tage").tag(rules.addedDays) }
                }
                Picker("Zusammenstellung", selection: $rules.sort) {
                    Text("Neueste Titel").tag("recent"); Text("Titel A–Z").tag("title")
                    Text("Am häufigsten gehört").tag("frequent"); Text("Zuletzt gehört").tag("last_played")
                }
                #if os(tvOS)
                HStack {
                    Text("Maximal \(rules.limit) Titel")
                    Spacer()
                    Button("Weniger", systemImage: "minus") { rules.limit = max(1, rules.limit - 1) }.disabled(rules.limit <= 1)
                    Button("Mehr", systemImage: "plus") { rules.limit = min(500, rules.limit + 1) }.disabled(rules.limit >= 500)
                }
                #else
                Stepper("Maximal \(rules.limit) Titel", value: $rules.limit, in: 1...500)
                #endif
                Text("Hörsortierungen verwenden den Verlauf auf dem Server. Neue Favoriten und passende Bibliothekstitel werden beim nächsten Öffnen berücksichtigt.")
                    .font(.callout).foregroundStyle(.secondary)
            }
        }
    }
    private var artistName: String {
        guard let id = rules.artistId else { return "Alle Künstler" }
        return selectedArtistName ?? model.artists.first { $0.id == id }?.name ?? "Ausgewählter Künstler"
    }
    private var presets: some View {
        Group {
            Button("Lieblingstitel", systemImage: "heart") { selectedArtistName = nil; rules = SmartPlaylistRules(favorites: true); if name.isEmpty { name = "Meine Lieblingstitel" } }
            Button("Neu entdeckt", systemImage: "sparkles") { selectedArtistName = nil; rules = SmartPlaylistRules(addedDays: 30); if name.isEmpty { name = "Neu entdeckt" } }
            Button("Oft gehört", systemImage: "repeat") { selectedArtistName = nil; rules = SmartPlaylistRules(sort: "frequent"); if name.isEmpty { name = "Oft gehört" } }
        }.buttonStyle(.bordered)
    }
    private func save() async {
        guard let client = model.client, !working else { return }
        working = true; failure = nil; defer { working = false }
        do {
            let result: Playlist
            if let playlist { result = try await model.editPlaylist(playlist, name: name.trimmingCharacters(in: .whitespacesAndNewlines), description: notes, rules: intelligent ? rules : nil) }
            else { result = try await model.createPlaylist(name: name.trimmingCharacters(in: .whitespacesAndNewlines), description: notes, rules: intelligent ? rules : nil) }
            guard model.client === client else { return }
            saved(result); dismiss()
        } catch is CancellationError { }
        catch { if model.client === client { failure = playlistFailure(error) } }
    }
}

struct PlaylistArtistPicker: View {
    var model: AppModel
    var selected: String?
    var picked: (Artist?) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""
    @State private var results: [Artist] = []
    @State private var failure: String?
    @State private var loading = false
    var body: some View {
        NavigationStack {
            List {
                Button("Alle Künstler") { picked(nil); dismiss() }
                if loading { ProgressView("Künstler suchen …") }
                if let failure { Text(failure).foregroundStyle(.red) }
                ForEach(results) { artist in
                    Button { picked(artist); dismiss() } label: {
                        HStack { Text(artist.name); Spacer(); if artist.id == selected { Image(systemName: "checkmark") } }
                    }
                }
                if results.isEmpty && !loading { Text("Künstler über mindestens zwei Zeichen suchen.").foregroundStyle(.secondary) }
            }.searchable(text: $query, prompt: "Künstler suchen").navigationTitle("Künstler auswählen")
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Abbrechen") { dismiss() } } }
                .task(id: query) {
                    loading = true; failure = nil; defer { if !Task.isCancelled { loading = false } }
                    guard let client = model.client else { return }
                    do {
                        if query.trimmingCharacters(in: .whitespacesAndNewlines).count < 2 { results = model.artists; return }
                        try await Task.sleep(for: .milliseconds(300))
                        let response = try await model.search(query)
                        try Task.checkCancellation(); guard model.client === client else { return }
                        results = response?.artists ?? []
                    } catch is CancellationError { }
                    catch { if !Task.isCancelled && model.client === client { failure = playlistFailure(error) } }
                }
        }.playlistSheetFrame()
    }
}

struct PlaylistTrackPicker: View {
    var model: AppModel
    var playlist: Playlist
    var existing: Set<String>
    var saved: (PlaylistDetail) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var query = ""
    @State private var results: [Track] = []
    @State private var selected: [Track] = []
    @State private var loading = false
    @State private var working = false
    @State private var failure: String?
    var body: some View {
        NavigationStack {
            List {
                Section { Text("\(selected.count) von maximal 100 Titeln ausgewählt. Bereits enthaltene Titel werden nicht doppelt hinzugefügt.").font(.callout).foregroundStyle(.secondary) }
                if let failure { Text(failure).foregroundStyle(.red) }
                if loading { ProgressView("Bibliothek durchsuchen …") }
                ForEach(results) { track in
                    Button { toggle(track) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 44, height: 44)
                            VStack(alignment: .leading) { Text(track.title).font(.headline); Text(track.artistText).foregroundStyle(.secondary) }
                            Spacer()
                            Image(systemName: existing.contains(track.id) ? "checkmark.circle.fill" : selected.contains(where: { $0.id == track.id }) ? "checkmark.circle.fill" : "circle")
                        }.padding(.vertical, 4)
                    }.buttonStyle(.plain).disabled(existing.contains(track.id) || working)
                }
                if !loading && results.isEmpty { Text("Keine passenden Titel gefunden.").foregroundStyle(.secondary) }
            }.searchable(text: $query, prompt: "Titel, Künstler oder Album suchen")
                .navigationTitle("Titel hinzufügen")
                .toolbar {
                    ToolbarItem(placement: .cancellationAction) { Button("Abbrechen") { dismiss() }.disabled(working) }
                    ToolbarItem(placement: .confirmationAction) {
                        Button(working ? "Hinzufügen …" : "\(selected.count) hinzufügen") { Task { await add() } }
                            .disabled(selected.isEmpty || working || model.playlistBusy)
                    }
                }
                .task(id: query) { await search() }
        }.playlistSheetFrame().interactiveDismissDisabled(working)
    }
    private func toggle(_ track: Track) {
        if let index = selected.firstIndex(where: { $0.id == track.id }) { selected.remove(at: index) }
        else if selected.count < 100 { selected.append(track) }
    }
    private func search() async {
        guard let client = model.client else { return }
        loading = true; failure = nil; defer { if !Task.isCancelled { loading = false } }
        do {
            let result: [Track]
            let trimmed = query.trimmingCharacters(in: .whitespacesAndNewlines)
            if trimmed.isEmpty { result = try await client.get("/library/tracks", query: [.init(name: "limit", value: "100"), .init(name: "sort", value: "recent"), .init(name: "order", value: "desc")]) }
            else if trimmed.count < 2 { result = [] }
            else { try await Task.sleep(for: .milliseconds(300)); result = try await model.search(trimmed)?.tracks ?? [] }
            try Task.checkCancellation(); guard model.client === client else { return }; results = result
        } catch is CancellationError { }
        catch { if !Task.isCancelled && model.client === client { failure = playlistFailure(error) } }
    }
    private func add() async {
        guard let client = model.client, !working else { return }
        working = true; failure = nil; defer { working = false }
        do {
            let detail = try await model.changePlaylistTracks(playlist, ids: selected.map(\.id))
            guard model.client === client else { return }; saved(detail); dismiss()
        } catch is CancellationError { }
        catch { if model.client === client { failure = playlistFailure(error) } }
    }
}

struct AddToPlaylistSheet: View {
    var model: AppModel
    var tracks: [Track]
    @Environment(\.dismiss) private var dismiss
    @State private var name = ""
    @State private var working = false
    @State private var failure: String?
    private var uniqueTracks: [Track] {
        var ids = Set<String>(); return tracks.filter { ids.insert($0.id).inserted }
    }
    var body: some View {
        NavigationStack {
            List {
                Section {
                    Text("\(uniqueTracks.count) Titel hinzufügen").font(.headline)
                    Text("Automatische Playlists werden über ihre Regeln gepflegt.").foregroundStyle(.secondary)
                    if uniqueTracks.count > 500 { Text("Bitte höchstens 500 verschiedene Titel gleichzeitig auswählen.").foregroundStyle(.red) }
                }
                if let failure { Section { Text(failure).foregroundStyle(.red) } }
                Section("Neue Playlist") {
                    TextField("Name", text: $name)
                    Button("Erstellen und Titel hinzufügen", systemImage: "plus") { Task { await createAndAdd() } }
                        .disabled(name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || name.utf8.count > 128 || working || model.playlistBusy || uniqueTracks.isEmpty || uniqueTracks.count > 500)
                }
                Section("Deine Playlists") {
                    ForEach(model.playlists) { playlist in
                        Button { Task { await add(playlist) } } label: {
                            HStack { Label(playlist.name, systemImage: playlist.smartRules == nil ? "music.note.list" : "sparkles"); Spacer(); Text("\(playlist.trackCount)").foregroundStyle(.secondary) }
                        }.disabled(working || model.playlistBusy || playlist.smartRules != nil || uniqueTracks.isEmpty || uniqueTracks.count > 500)
                    }
                }
            }.navigationTitle("Zur Playlist hinzufügen")
                .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Schließen") { dismiss() }.disabled(working) } }
        }.playlistSheetFrame().interactiveDismissDisabled(working)
    }
    private func add(_ playlist: Playlist) async {
        guard let client = model.client, !working else { return }
        working = true; failure = nil; defer { working = false }
        do {
            _ = try await model.changePlaylistTracks(playlist, ids: uniqueTracks.map(\.id))
            guard model.client === client else { return }; dismiss()
        } catch is CancellationError { }
        catch { if model.client === client { failure = playlistFailure(error) } }
    }
    private func createAndAdd() async {
        guard let client = model.client, !working else { return }
        working = true; failure = nil; defer { working = false }
        var created = false
        do {
            let playlist = try await model.createPlaylist(name: name.trimmingCharacters(in: .whitespacesAndNewlines), description: "", rules: nil)
            created = true
            _ = try await model.changePlaylistTracks(playlist, ids: uniqueTracks.map(\.id))
            guard model.client === client else { return }; dismiss()
        } catch is CancellationError { }
        catch { if model.client === client { failure = (created ? "Die Playlist wurde erstellt. Die Titel wurden nicht hinzugefügt; wähle die Playlist unten zum erneuten Versuch. " : "") + playlistFailure(error) } }
    }
}

func playlistFailure(_ error: Error) -> String {
    if error is PlayerError { return error.localizedDescription }
    return "Die Änderung konnte nicht bestätigt werden. Bitte Verbindung prüfen und die Playlist neu laden."
}
private struct PlaylistSheetFrame: ViewModifier {
    func body(content: Content) -> some View {
        #if os(macOS)
        content.frame(minWidth: 540, idealWidth: 650, minHeight: 560, idealHeight: 700)
        #else
        content
        #endif
    }
}
private extension View { func playlistSheetFrame() -> some View { modifier(PlaylistSheetFrame()) } }
