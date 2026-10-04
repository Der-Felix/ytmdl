import SwiftUI
import AVKit
import CoreImage.CIFilterBuiltins
import YTMDLCore

enum Destination: String, CaseIterable, Identifiable {
    case library = "Bibliothek", artists = "Künstler", search = "Suche", favorites = "Favoriten", playlists = "Playlists", player = "Player", settings = "Einstellungen"
    var id: String { rawValue }
    var icon: String {
        switch self { case .library: "square.stack"; case .artists: "person.2"; case .search: "magnifyingglass"; case .favorites: "heart"; case .playlists: "music.note.list"; case .player: "play.circle"; case .settings: "gearshape" }
    }
}

struct RootView: View {
    @Bindable var model: AppModel
    @State private var destination: Destination? = .library
    @State private var expandedPlayer = false
    @AppStorage("appearance") private var appearance = "dark"
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    var body: some View {
        ZStack {
            if model.user == nil { ConnectView(model: model) }
            else {
                #if os(tvOS)
                TabView(selection: $destination) {
                    ForEach([Destination.library, .search, .favorites, .playlists, .player, .settings]) { item in
                        NavigationStack { content(item) }.tabItem { Label(item == .settings ? "Optionen" : item.rawValue, systemImage: item.icon) }.tag(Optional(item))
                    }
                }
                #elseif os(iOS)
                if sizeClass == .compact {
                    TabView(selection: $destination) {
                        ForEach([Destination.library, .search, .favorites, .playlists, .settings]) { item in
                            NavigationStack { content(item).safeAreaInset(edge: .bottom) { miniPlayer } }
                                .tabItem { Label(item.rawValue, systemImage: item.icon) }.tag(Optional(item))
                        }
                    }
                } else { splitView }
                #else
                splitView
                #endif
            }
        }
        .sheet(isPresented: $expandedPlayer) {
            NavigationStack {
                NowPlayingView(model: model)
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Schließen", systemImage: "chevron.down") { expandedPlayer = false } } }
            }
            #if os(macOS)
            .frame(minWidth: 760, idealWidth: 900, minHeight: 540, idealHeight: 640)
            #endif
        }
        .alert("YTMDL", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        #if os(tvOS)
        .onPlayPauseCommand { model.player.toggle() }
        #endif
        .preferredColorScheme(appearance == "system" ? nil : appearance == "light" ? .light : .dark)
        #if DEBUG
        .task {
            await model.loadFixtureIfRequested()
            if ProcessInfo.processInfo.arguments.contains("--fixture-player") {
                #if os(iOS)
                expandedPlayer = true
                #else
                destination = .player
                #endif
            }
        }
        #endif
    }
    private var splitView: some View {
        NavigationSplitView {
            List(Destination.allCases, selection: $destination) { item in Label(item.rawValue, systemImage: item.icon).tag(item) }
                .navigationTitle("YTMDL")
                .navigationSplitViewColumnWidth(min: 180, ideal: 200, max: 240)
        } detail: {
            NavigationStack {
                content(destination ?? .library).safeAreaInset(edge: .bottom) {
                    #if os(macOS)
                    if destination != .player { miniPlayer }
                    #else
                    miniPlayer
                    #endif
                }
                #if os(macOS)
                .toolbar {
                    if destination == .player {
                        ToolbarItem(placement: .navigation) {
                            Button("Zurück zur Bibliothek", systemImage: "chevron.left") { destination = .library }
                                .keyboardShortcut(.cancelAction)
                        }
                    }
                }
                #endif
            }
        }
    }
    @ViewBuilder private func content(_ destination: Destination) -> some View {
        switch destination {
        case .library: LibraryView(model: model)
        case .artists: ArtistListView(model: model)
        case .search: SearchView(model: model)
        case .favorites: CollectionView(model: model, kind: .favorites)
        case .playlists: PlaylistListView(model: model)
        case .player: NowPlayingView(model: model)
        case .settings: SettingsView(model: model)
        }
    }
    @ViewBuilder private var miniPlayer: some View {
        if let track = model.player.current {
            #if os(macOS)
            desktopMiniPlayer(track)
            #else
            HStack(spacing: 14) {
                Button { expandedPlayer = true } label: {
                    HStack(spacing: 12) {
                        ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 46, height: 46)
                        VStack(alignment: .leading, spacing: 3) {
                            Text(track.title).font(.headline).lineLimit(1)
                            Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel("Player öffnen: \(track.title)")
                Button { model.player.toggle() } label: { Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").frame(width: 44, height: 44) }.accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen")
                Button { model.player.next() } label: { Image(systemName: "forward.end.fill").frame(width: 44, height: 44) }.accessibilityLabel("Nächster Titel")
            }
            .padding(12).background(.regularMaterial, in: RoundedRectangle(cornerRadius: 20)).padding(.horizontal, 12).padding(.bottom, 8)
            #endif
        }
    }
    #if os(macOS)
    private func desktopMiniPlayer(_ track: Track) -> some View {
        HStack(spacing: 20) {
            Button { destination = .player } label: {
                HStack(spacing: 10) {
                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 44, height: 44)
                    VStack(alignment: .leading, spacing: 3) {
                        Text(track.title).font(.headline).lineLimit(1)
                        Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                    }
                }.frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
            }.buttonStyle(.plain).frame(maxWidth: 260).accessibilityLabel("Player öffnen: \(track.title)")
            HStack(spacing: 4) {
                Button { model.player.previous() } label: { Image(systemName: "backward.end.fill").frame(width: 32, height: 36) }.accessibilityLabel("Vorheriger Titel")
                Button { model.player.toggle() } label: { Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").font(.title3).frame(width: 36, height: 36) }.accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen")
                Button { model.player.next() } label: { Image(systemName: "forward.end.fill").frame(width: 32, height: 36) }.accessibilityLabel("Nächster Titel")
            }.buttonStyle(.borderless).tint(.primary)
            VStack(spacing: 2) {
                Slider(value: Binding(get: { min(model.player.position, model.player.duration) }, set: { model.player.seek($0) }), in: 0...max(1, model.player.duration)).accessibilityLabel("Wiedergabeposition")
                HStack { Text(formatTime(model.player.position)); Spacer(); Text(formatTime(model.player.duration)) }
                    .font(.caption2.monospacedDigit()).foregroundStyle(.secondary)
            }.frame(maxWidth: .infinity)
            AirPlayPicker().frame(width: 30, height: 30).accessibilityLabel("Audioausgabe wählen")
        }.padding(.horizontal, 20).padding(.vertical, 10).background(.bar)
    }
    #endif
}

struct ConnectView: View {
    @Bindable var model: AppModel
    @State private var address = UserDefaults.standard.string(forKey: "serverAddress") ?? ""
    @State private var username = ""
    @State private var password = ""
    @State private var allowHTTP = false
    @State private var connected = false
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                Image(systemName: "waveform.circle.fill").font(.system(size: 72)).foregroundStyle(.pink).accessibilityHidden(true)
                Text("Deine Musik.\nDein Server.").font(.largeTitle.bold())
                Text("Verbinde YTMDL mit deiner bestehenden Musikbibliothek.").foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Server-Adresse · https://…", text: $address)
                        .textContentType(.URL)
                        #if os(iOS) || os(tvOS)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        #endif
                    #if DEBUG
                    Toggle("Lokalen HTTP-Test erlauben", isOn: $allowHTTP)
                    if allowHTTP { Text("Nur im vertrauenswürdigen lokalen Netz: Anmeldung und Musik werden unverschlüsselt übertragen. Für den regulären Betrieb HTTPS verwenden.").font(.caption).foregroundStyle(.secondary) }
                    #endif
                    Button("Server verbinden", systemImage: "network") {
                        Task {
                            do { try model.connect(address, localHTTP: allowHTTP); await model.restore(); connected = true }
                            catch { model.report(error) }
                        }
                    }.buttonStyle(.borderedProminent).disabled(model.busy || address.isEmpty)
                }
                #if !os(tvOS)
                .textFieldStyle(.roundedBorder)
                #endif
                if connected && model.client != nil {
                    #if os(tvOS)
                    DeviceSignInView(model: model)
                    #else
                    VStack(alignment: .leading, spacing: 12) {
                        TextField("Benutzername", text: $username).textContentType(.username)
                            #if os(iOS)
                            .textInputAutocapitalization(.never).autocorrectionDisabled()
                            #endif
                        SecureField("Passwort", text: $password).textContentType(.password)
                        Button("Anmelden", systemImage: "person.crop.circle") {
                            let secret = password; password = ""
                            Task { await model.login(username: username, password: secret) }
                        }.buttonStyle(.borderedProminent).disabled(model.busy || username.isEmpty || password.isEmpty)
                        if model.busy { ProgressView("Anmeldung läuft …") }
                    }.textFieldStyle(.roundedBorder)
                    #endif
                }
                Text("Keine Analyse- oder Werbe-SDKs. Sitzungen bleiben im Schlüsselbund dieses Geräts.").font(.footnote).foregroundStyle(.secondary)
            }.padding(32).frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
        .task {
            if address.hasPrefix("https://") {
                do { try model.connect(address, localHTTP: false); await model.restore(); connected = true }
                catch { model.report(error) }
            }
        }
    }
}

struct ArtworkView: View {
    var model: AppModel
    var kind: String
    var id: String
    var body: some View {
        Group {
            if let client = model.client, let request = try? client.artworkRequest(kind: kind, id: id) {
                AsyncImage(request: request) { image in image.resizable().scaledToFill() } placeholder: { placeholder }
                    .asyncImageURLSession(client.session)
            } else { placeholder }
        }.aspectRatio(1, contentMode: .fit).clipped().clipShape(RoundedRectangle(cornerRadius: 12)).accessibilityHidden(true)
    }
    private var placeholder: some View {
        RoundedRectangle(cornerRadius: 12).fill(.quaternary)
            .overlay { Image(systemName: kind == "artists" ? "person.crop.circle" : "music.note").font(.largeTitle).foregroundStyle(.secondary) }
    }
}

struct LibraryView: View {
    @Bindable var model: AppModel
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                #if os(macOS)
                HStack(alignment: .center, spacing: 16) {
                    Text("Alben").font(.title2.bold())
                    Spacer()
                    genrePicker.frame(maxWidth: 200)
                }
                #else
                Text("Deine Musik").font(.title2.bold())
                HStack {
                    NavigationLink { ArtistListView(model: model) } label: { Label("Künstler", systemImage: "person.2") }
                    NavigationLink { CollectionView(model: model, kind: .favorites) } label: { Label("Favoriten", systemImage: "heart.fill") }
                }.buttonStyle(.bordered)
                genrePicker
                #endif
                if model.connecting && model.releases.isEmpty { ProgressView("Bibliothek wird geladen …") }
                else if model.releases.isEmpty { ContentUnavailableView("Noch keine Alben", systemImage: "square.stack", description: Text("Musik im Web hinzufügen oder einen anderen Genre-Filter wählen.")) }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: cardMinimum, maximum: 280), spacing: 20)], spacing: 24) {
                    ForEach(model.releases) { release in
                        NavigationLink { CollectionView(model: model, kind: .release(release)) } label: {
                            VStack(alignment: .leading, spacing: 8) {
                                ArtworkView(model: model, kind: "releases", id: release.id)
                                Text(release.title).font(.headline).foregroundStyle(.primary).lineLimit(2)
                                Text(release.artists.joined(separator: " · ")).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                            }.frame(maxWidth: .infinity, alignment: .leading)
                        }.buttonStyle(.plain)
                    }
                }
                if model.moreReleases { Button("Weitere Alben laden") { Task { await model.loadMore() } }.disabled(model.connecting) }
            }.padding(contentPadding)
        }.navigationTitle("Bibliothek")
        .toolbar { ToolbarItem { Button { Task { await model.loadLibrary() } } label: { Image(systemName: "arrow.clockwise") }.accessibilityLabel("Bibliothek aktualisieren").disabled(model.connecting) } }
    }
    @ViewBuilder private var genrePicker: some View {
        if !model.genres.isEmpty {
            Picker("Genre", selection: $model.genre) {
                Text("Alle Genres").tag("")
                ForEach(model.genres, id: \.self) { Text($0).tag($0) }
            }.pickerStyle(.menu).onChange(of: model.genre) { _, genre in Task { await model.filterGenre(genre) } }
        }
    }
    private var cardMinimum: CGFloat {
        #if os(tvOS)
        240
        #elseif os(macOS)
        175
        #else
        145
        #endif
    }
    private var contentPadding: CGFloat {
        #if os(tvOS)
        60
        #elseif os(macOS)
        24
        #else
        20
        #endif
    }
}

struct ArtistListView: View {
    var model: AppModel
    @State private var artists: [Artist] = []
    @State private var offset = 0
    @State private var more = true
    @State private var busy = false
    var body: some View {
        Group {
            #if os(macOS)
            ScrollView {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 160, maximum: 240), spacing: 24)], spacing: 24) {
                    ForEach(artists) { artist in
                        NavigationLink { CollectionView(model: model, kind: .artist(artist)) } label: {
                            VStack(spacing: 10) {
                                ArtworkView(model: model, kind: "artists", id: artist.id).clipShape(Circle())
                                Text(artist.name).font(.headline).foregroundStyle(.primary).lineLimit(1)
                                Text("\(artist.trackCount ?? 0) Titel").font(.subheadline).foregroundStyle(.secondary)
                            }.padding(8).frame(maxWidth: .infinity)
                        }.buttonStyle(.plain)
                    }
                }.padding(24)
                if more { loadMoreButton.padding(.bottom, 24) }
                if busy && artists.isEmpty { ProgressView("Künstler werden geladen …") }
                else if artists.isEmpty { ContentUnavailableView("Noch keine Künstler", systemImage: "person.2") }
            }
            #else
            List {
                ForEach(artists) { artist in
                    NavigationLink { CollectionView(model: model, kind: .artist(artist)) } label: {
                        HStack(spacing: 14) {
                            ArtworkView(model: model, kind: "artists", id: artist.id).frame(width: 60, height: 60)
                            VStack(alignment: .leading) { Text(artist.name).font(.headline); Text("\(artist.trackCount ?? 0) Titel").foregroundStyle(.secondary) }
                        }.padding(.vertical, 4)
                    }
                }
                if more { loadMoreButton }
            }
            #endif
        }.navigationTitle("Künstler").task { if artists.isEmpty { await load() } }
    }
    private var loadMoreButton: some View {
        Button("Weitere Künstler laden") { Task { await load() } }.disabled(busy)
    }
    private func load() async {
        guard let client = model.client, !busy else { return }
        busy = true; defer { busy = false }
        do {
            let result: [Artist] = try await client.get("/library/artists", query: [.init(name: "limit", value: "60"), .init(name: "offset", value: String(offset))])
            try Task.checkCancellation()
            artists += result; offset += result.count; more = result.count == 60
        } catch { model.report(error) }
    }
}

enum CollectionKind {
    case favorites, release(Release), artist(Artist), playlist(Playlist)
    var title: String {
        switch self { case .favorites: "Lieblingstitel"; case .release(let x): x.title; case .artist(let x): x.name; case .playlist(let x): x.name }
    }
}
struct CollectionView: View {
    var model: AppModel
    var kind: CollectionKind
    @State private var tracks: [Track] = []
    @State private var busy = false
    @State private var more = true
    @State private var offset = 0
    var body: some View {
        List {
            Section {
                HStack {
                    Button("Abspielen", systemImage: "play.fill") { play(tracks) }.buttonStyle(.borderedProminent).disabled(tracks.isEmpty)
                    Button("Zufall", systemImage: "shuffle") { play(tracks.shuffled()) }.buttonStyle(.bordered).disabled(tracks.isEmpty)
                }.padding(.vertical, 12)
            }
            Section {
                ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in TrackRow(model: model, track: track) { play(tracks, index: index) } }
                if busy { ProgressView("Titel werden geladen …") }
                else if tracks.isEmpty { ContentUnavailableView("Noch keine Titel", systemImage: "music.note") }
                if more && !tracks.isEmpty { Button("Weitere Titel laden") { Task { await load() } } }
            }
        }.navigationTitle(kind.title).task { if tracks.isEmpty { await load() } }
    }
    private func play(_ tracks: [Track], index: Int = 0) { if let client = model.client { model.player.play(tracks, start: index, client: client) } }
    private func load() async {
        guard let client = model.client, !busy else { return }
        busy = true; defer { busy = false }
        do {
            let result: [Track]
            switch kind {
            case .favorites:
                result = try await client.get("/library/tracks", query: [.init(name: "favorite", value: "true"), .init(name: "limit", value: "100"), .init(name: "offset", value: String(offset))])
            case .release(let release):
                result = try await client.get("/library/tracks", query: [.init(name: "release_id", value: release.id), .init(name: "sort", value: "track_number"), .init(name: "order", value: "asc"), .init(name: "limit", value: "100"), .init(name: "offset", value: String(offset))])
            case .artist(let artist):
                result = try await client.get("/library/tracks", query: [.init(name: "artist_id", value: artist.id), .init(name: "limit", value: "100"), .init(name: "offset", value: String(offset))])
            case .playlist(let playlist):
                let detail: PlaylistDetail = try await client.get("/playlists/\(playlist.id)"); result = detail.tracks
            }
            try Task.checkCancellation()
            tracks += result; offset += result.count
            if case .playlist = kind { more = false } else { more = result.count == 100 }
        } catch { model.report(error); more = false }
    }
}

struct TrackRow: View {
    var model: AppModel
    var track: Track
    var action: () -> Void
    var body: some View {
        HStack(spacing: 14) {
            Button(action: action) {
                HStack(spacing: 14) {
                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 52, height: 52)
                    VStack(alignment: .leading, spacing: 4) {
                        Text(track.title).font(.headline).lineLimit(1)
                        Text(track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    Text(formatTime(track.duration)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                }.contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
            Menu {
                Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten", systemImage: "heart") { Task { await model.toggleFavorite(track) } }
                Button("Zur Warteschlange", systemImage: "text.badge.plus") { if let client = model.client { model.player.append(track, client: client) } }
            } label: { Image(systemName: "ellipsis").frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Aktionen für \(track.title)")
        }.padding(.vertical, 4)
    }
}

struct SearchView: View {
    var model: AppModel
    @State private var query = ""
    @State private var results: SearchResults?
    @State private var busy = false
    @State private var failure: String?
    var body: some View {
        List {
            if busy { ProgressView("Suche …") }
            if let failure { Text(failure).foregroundStyle(.secondary) }
            if let results {
                Section("Künstler") {
                    ForEach(results.artists) { artist in
                        NavigationLink { CollectionView(model: model, kind: .artist(artist)) } label: {
                            HStack { ArtworkView(model: model, kind: "artists", id: artist.id).frame(width: 56, height: 56); Text(artist.name) }
                        }
                    }
                }
                Section("Alben") {
                    ForEach(results.releases) { release in
                        NavigationLink { CollectionView(model: model, kind: .release(release)) } label: {
                            HStack { ArtworkView(model: model, kind: "releases", id: release.id).frame(width: 56, height: 56); VStack(alignment: .leading) { Text(release.title); Text(release.artists.joined(separator: " · ")).foregroundStyle(.secondary) } }
                        }
                    }
                }
                Section("Titel") { ForEach(results.tracks) { track in TrackRow(model: model, track: track) { if let client = model.client { model.player.play(results.tracks, start: results.tracks.firstIndex(of: track) ?? 0, client: client) } } } }
                if results.artists.isEmpty && results.releases.isEmpty && results.tracks.isEmpty { ContentUnavailableView.search(text: query) }
            } else if !busy && failure == nil { ContentUnavailableView("Deine Bibliothek durchsuchen", systemImage: "magnifyingglass", description: Text("Mindestens zwei Zeichen eingeben.")) }
        }
        .navigationTitle("Suche").searchable(text: $query, prompt: "Titel, Alben, Künstler")
        .task(id: query) {
            results = nil; failure = nil; busy = query.count >= 2
            do {
                try await Task.sleep(for: .milliseconds(300))
                let result = try await model.search(query)
                try Task.checkCancellation(); results = result; busy = false
            } catch is CancellationError { }
            catch { if !Task.isCancelled { failure = "Die Suche konnte nicht geladen werden. Bitte Verbindung prüfen und erneut suchen."; busy = false } }
        }
    }
}

struct PlaylistListView: View {
    var model: AppModel
    var body: some View {
        List {
            if model.playlists.isEmpty { ContentUnavailableView("Noch keine Playlists", systemImage: "music.note.list", description: Text("Playlists im Web anlegen. Sie erscheinen hier nach dem Aktualisieren.")) }
            ForEach(model.playlists) { playlist in
                NavigationLink { CollectionView(model: model, kind: .playlist(playlist)) } label: {
                    VStack(alignment: .leading, spacing: 6) { Text(playlist.name).font(.headline); Text("\(playlist.trackCount) Titel · \(formatTime(Double(playlist.durationMs)/1000))").foregroundStyle(.secondary) }.padding(.vertical, 10)
                }
            }
        }.navigationTitle("Playlists")
        .toolbar { ToolbarItem { Button("Aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } } } }
    }
}

struct NowPlayingView: View {
    var model: AppModel
    @State private var tab = 0
    var body: some View {
        GeometryReader { geometry in
            #if os(macOS)
            desktopPlayer(geometry.size)
            #else
            ScrollView {
                if let track = model.player.current {
                    let wide = geometry.size.width > 820
                    VStack(spacing: 28) {
                        if wide {
                            HStack(alignment: .top, spacing: 40) { player(track, coverSize: wideCoverSize(geometry.size.height)).frame(maxWidth: .infinity); details.frame(maxWidth: .infinity) }
                        } else { player(track, coverSize: min(340, max(140, geometry.size.width-48), geometry.size.height*0.38)); details }
                    }.padding(wide ? 40 : 24).frame(maxWidth: 1300).frame(maxWidth: .infinity)
                } else { ContentUnavailableView("Musik auswählen", systemImage: "play.circle", description: Text("Öffne ein Album oder eine Playlist, um loszuhören.")).frame(width: geometry.size.width, height: geometry.size.height) }
            }
            #endif
        }.navigationTitle("Player")
        #if os(macOS)
        .toolbar { ToolbarItem { AirPlayPicker().frame(width: 30, height: 30).accessibilityLabel("Audioausgabe wählen") } }
        #endif
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
    #if os(macOS)
    @ViewBuilder private func desktopPlayer(_ size: CGSize) -> some View {
        if let track = model.player.current {
            if size.width >= 640 {
                let playerWidth = min(360, (size.width - 72) * 0.48)
                HStack(alignment: .top, spacing: 24) {
                    ScrollView {
                        player(track, coverSize: max(150, min(playerWidth, size.height - 290)))
                    }.scrollIndicators(.hidden).frame(width: playerWidth)
                    Divider()
                    desktopDetails
                }.padding(24)
            } else {
                ScrollView {
                    VStack(spacing: 24) {
                        player(track, coverSize: min(280, size.width - 48, max(160, size.height - 290)))
                            .frame(maxWidth: 380)
                        Divider()
                        details
                    }.padding(24).frame(maxWidth: .infinity)
                }
            }
        } else {
            ContentUnavailableView("Musik auswählen", systemImage: "play.circle", description: Text("Öffne ein Album oder eine Playlist, um loszuhören."))
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
    private var desktopDetails: some View {
        VStack(alignment: .leading, spacing: 16) {
            detailsPicker
            ScrollView {
                detailsContent
            }
        }.frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
    }
    #endif
    private func wideCoverSize(_ height: CGFloat) -> CGFloat {
        #if os(tvOS)
        min(250, height * 0.32)
        #else
        min(380, height * 0.48)
        #endif
    }
    private var titleFont: Font {
        #if os(tvOS) || os(macOS)
        .title2
        #else
        .title
        #endif
    }
    private func player(_ track: Track, coverSize: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: playerSpacing) {
            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: coverSize, height: coverSize).frame(maxWidth: .infinity)
            HStack {
                VStack(alignment: .leading, spacing: 6) {
                    Text(track.title).font(titleFont.bold()).lineLimit(2); Text(track.artistText).font(.title3).foregroundStyle(.secondary).lineLimit(2)
                    if !track.album.isEmpty && track.album != track.title { Text(track.album).font(.subheadline).foregroundStyle(.secondary) }
                }
                Spacer()
                Button { Task { await model.toggleFavorite(track) } } label: { Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart").frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Favorit umschalten")
                #if os(macOS)
                .buttonStyle(.borderless)
                #endif
            }
            #if os(tvOS)
            ProgressView(value: min(model.player.position, model.player.duration), total: max(1, model.player.duration))
            HStack { Button("−10 Sekunden") { model.player.seek(model.player.position-10) }; Button("+10 Sekunden") { model.player.seek(model.player.position+10) } }
            #else
            Slider(value: Binding(get: { min(model.player.position, model.player.duration) }, set: { model.player.seek($0) }), in: 0...max(1, model.player.duration)).accessibilityLabel("Wiedergabeposition")
            #endif
            HStack { Text(formatTime(model.player.position)).accessibilityIdentifier("playbackElapsed"); Spacer(); Text(formatTime(model.player.duration)) }.font(.caption.monospacedDigit()).foregroundStyle(.secondary)
            HStack(spacing: transportSpacing) {
                Button { model.player.shuffle() } label: { Image(systemName: "shuffle").frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Nächste Titel mischen")
                Button { model.player.previous() } label: { Image(systemName: "backward.end.fill").font(.title2).frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Vorheriger Titel")
                Button { model.player.toggle() } label: {
                    Image(systemName: model.player.isPlaying ? "pause.fill" : "play.fill").font(.title).frame(width: playButtonSize, height: playButtonSize)
                }.buttonStyle(.borderedProminent).buttonBorderShape(.circle).tint(.pink).accessibilityLabel(model.player.isPlaying ? "Pause" : "Abspielen")
                Button { model.player.next() } label: { Image(systemName: "forward.end.fill").font(.title2).frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Nächster Titel")
                Button { model.player.repeatAll.toggle() } label: { Image(systemName: "repeat").foregroundStyle(model.player.repeatAll ? .pink : .primary).frame(minWidth: 44, minHeight: 44) }.accessibilityLabel(model.player.repeatAll ? "Wiederholen ausschalten" : "Warteschlange wiederholen")
            }.frame(maxWidth: .infinity).tint(.primary)
            #if os(macOS)
            .buttonStyle(.borderless)
            #endif
            if model.player.loading { ProgressView("Titel wird geladen …") }
            if let error = model.player.error { Text(error).font(.callout).foregroundStyle(.secondary); Button("Erneut versuchen") { model.player.resume() } }
            #if os(iOS)
            AirPlayPicker().frame(width: 44, height: 44).frame(maxWidth: .infinity).accessibilityLabel("Audioausgabe wählen")
            #endif
        }
    }
    private var playerSpacing: CGFloat {
        #if os(macOS)
        12
        #else
        16
        #endif
    }
    private var transportSpacing: CGFloat {
        #if os(macOS)
        4
        #else
        20
        #endif
    }
    private var playButtonSize: CGFloat {
        #if os(macOS)
        48
        #else
        64
        #endif
    }
    private var details: some View {
        VStack(alignment: .leading, spacing: 20) {
            detailsPicker
            detailsContent
        }
    }
    private var detailsPicker: some View {
        Picker("Player-Ansicht", selection: $tab) { Text("Warteschlange").tag(0); Text("Lyrics").tag(1) }.pickerStyle(.segmented).labelsHidden()
    }
    @ViewBuilder private var detailsContent: some View {
        if tab == 0 {
            LazyVStack(alignment: .leading, spacing: 4) {
                ForEach(Array(model.player.queue.tracks.enumerated()), id: \.offset) { index, track in
                    Button { model.player.select(index) } label: {
                        HStack(spacing: 12) {
                            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 48, height: 48)
                            VStack(alignment: .leading, spacing: 4) {
                                Text(track.title).font(.headline).lineLimit(1)
                                Text(track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                            }
                            Spacer(minLength: 8)
                            if model.player.queue.index == index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(.pink) }
                        }.padding(8).contentShape(Rectangle())
                        #if os(macOS)
                        .background(model.player.queue.index == index ? Color.primary.opacity(0.06) : .clear, in: RoundedRectangle(cornerRadius: 10))
                        #endif
                    }.buttonStyle(.plain)
                }
            }
        } else {
            Text(model.player.lyrics.isEmpty ? "Lyrics werden geladen …" : cleanLyrics(model.player.lyrics)).font(.title3).lineSpacing(12)
                #if !os(tvOS)
                .textSelection(.enabled)
                #endif
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

#if os(iOS)
struct AirPlayPicker: UIViewRepresentable {
    func makeUIView(context: Context) -> AVRoutePickerView { let view = AVRoutePickerView(); view.activeTintColor = .systemPink; return view }
    func updateUIView(_ uiView: AVRoutePickerView, context: Context) {}
}
#elseif os(macOS)
struct AirPlayPicker: NSViewRepresentable {
    func makeNSView(context: Context) -> AVRoutePickerView { AVRoutePickerView() }
    func updateNSView(_ nsView: AVRoutePickerView, context: Context) {}
}
#endif

func formatTime(_ seconds: Double) -> String {
    guard seconds.isFinite else { return "0:00" }
    let value = Int(max(0, seconds)); return String(format: "%d:%02d", value/60, value%60)
}
func cleanLyrics(_ content: String) -> String {
    content.replacingOccurrences(of: #"\[\d{1,3}:\d{2}(?:\.\d{1,3})?\]"#, with: "", options: .regularExpression)
}
