import SwiftUI
import AVKit
import CoreImage.CIFilterBuiltins
import YTMDLCore

enum Destination: String, CaseIterable, Identifiable {
    case home = "Start", library = "Bibliothek", artists = "Künstler", search = "Suche", favorites = "Favoriten", playlists = "Playlists", player = "Player", settings = "Einstellungen"
    var id: String { rawValue }
    var icon: String {
        switch self { case .home: "house.fill"; case .library: "square.stack"; case .artists: "person.2"; case .search: "magnifyingglass"; case .favorites: "heart"; case .playlists: "music.note.list"; case .player: "play.circle"; case .settings: "gearshape" }
    }
}

struct RootView: View {
    @Bindable var model: AppModel
    #if os(macOS)
    @State private var destination: Destination? = Destination(rawValue: UserDefaults.standard.string(forKey: "desktopStartView") ?? "Start") ?? .home
    @AppStorage("desktopTheme") private var themeName = "rose"
    @AppStorage("desktopTextSize") private var textSize = "large"
    @Environment(\.colorScheme) private var colorScheme
    private var selectedTheme: DesktopTheme { DesktopTheme(rawValue: themeName) ?? .rose }
    private var accent: Color { selectedTheme.accent(colorScheme) }
    #else
    @State private var destination: Destination? = .library
    #endif
    @State private var expandedPlayer = false
    #if os(macOS)
    @FocusState private var searchFocused: Bool
    @State private var playerOverlayOpen = false
    #endif
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
        #if os(macOS)
        .environment(\.desktopTheme, selectedTheme)
        .environment(\.desktopAccent, accent)
        .environment(\.desktopButtonAccent, selectedTheme.accent)
        .environment(\.desktopTextScale, (DesktopTextSize(rawValue: textSize) ?? .large).scale)
        .tint(accent)
        .onPreferenceChange(PlayerOverlayPreferenceKey.self) { playerOverlayOpen = $0 }
        .onChange(of: model.query) { if !model.query.isEmpty { destination = .search } }
        .onChange(of: searchFocused) { if searchFocused { destination = .search } }
        #endif
        #if DEBUG
        .task {
            await model.loadFixtureIfRequested()
            #if os(macOS)
            let args = ProcessInfo.processInfo.arguments
            if let index = args.firstIndex(of: "--fixture-view"), args.indices.contains(index + 1),
               ["127.0.0.1", "::1"].contains(model.client?.server.url.host ?? ""),
               let requested = Destination(rawValue: args[index + 1]) { destination = requested }
            #endif
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
            #if os(macOS)
            desktopSidebar.navigationTitle("YTMDL")
                .navigationSplitViewColumnWidth(min: 300, ideal: 340, max: 440)
            #else
            List(Destination.allCases.filter { $0 != .home }, selection: $destination) { item in
                Label(item.rawValue, systemImage: item.icon).tag(item)
            }.navigationTitle("YTMDL").navigationSplitViewColumnWidth(min: 180, ideal: 200, max: 240)
            #endif
        } detail: {
            NavigationStack {
                content(destination ?? .library)
                    #if os(macOS)
                    .background(selectedTheme.background(colorScheme))
                    #endif
                    .safeAreaInset(edge: .bottom) {
                    #if os(macOS)
                    if destination != .player { miniPlayer }
                    #else
                    miniPlayer
                    #endif
                }
                #if os(macOS)
                .toolbar(removing: .title)
                .searchable(text: $model.query, placement: .toolbarPrincipal, prompt: "Musik suchen")
                .searchFocused($searchFocused)
                .toolbar {
                    ToolbarItem {
                        Button("Suche öffnen", systemImage: "magnifyingglass") { destination = .search; searchFocused = true }
                            .keyboardShortcut("f", modifiers: .command)
                    }
                    if destination == .player {
                        ToolbarItem(placement: .navigation) {
                            Button("Zurück zur Bibliothek", systemImage: "chevron.left") { destination = .library }
                                .keyboardShortcut(playerOverlayOpen ? nil : .cancelAction)
                        }
                    }
                }
                #endif
            }
        }
    }
    @ViewBuilder private func content(_ destination: Destination) -> some View {
        switch destination {
        case .home:
            #if os(macOS)
            DesktopHomeView(model: model) { self.destination = $0; if $0 == .search { searchFocused = true } }
            #else
            LibraryView(model: model)
            #endif
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
                            Text(track.title).desktopScaledFont(18, weight: .semibold, fallback: .headline).lineLimit(1)
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
    private var desktopSidebar: some View {
        VStack(spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 28) {
                    HStack(spacing: 16) {
                        Image("BrandMark").resizable().scaledToFit().frame(width: 52, height: 60).accessibilityHidden(true)
                        VStack(alignment: .leading, spacing: 5) {
                            Text("YTMDL").desktopScaledFont(28, weight: .bold)
                            Text("Deine Musik").desktopScaledFont(16).foregroundStyle(.secondary)
                        }
                    }.padding(.top, 16).padding(.bottom, 8)
                    sidebarGroup("FÜR DICH", items: [.home, .search, .favorites, .playlists])
                    sidebarGroup("DEINE SAMMLUNG", items: [.library, .artists])
                    sidebarGroup("WIEDERGABE", items: [.player])
                }.padding(18)
            }
            Divider()
            sidebarGroup("", items: [.settings]).padding(.horizontal, 18).padding(.top, 8)
            VStack(alignment: .leading, spacing: 6) {
                Text("Angemeldet als").desktopScaledFont(14).foregroundStyle(.secondary)
                Text(model.user?.displayName ?? "").desktopScaledFont(18, weight: .semibold).lineLimit(1)
            }.padding(.horizontal, 30).padding(.vertical, 14).frame(maxWidth: .infinity, alignment: .leading)
        }.background(selectedTheme.surface(colorScheme))
    }
    private func sidebarGroup(_ title: String, items: [Destination]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            if !title.isEmpty { Text(title).desktopScaledFont(12, weight: .semibold).tracking(1.5).foregroundStyle(.secondary).padding(.horizontal, 12) }
            ForEach(items) { item in
                Button {
                    destination = item
                    if item == .search { searchFocused = true }
                } label: {
                    HStack(spacing: 18) {
                        Image(systemName: item.icon).desktopScaledFont(23).foregroundStyle(accent).frame(width: 32)
                        Text(item.rawValue).desktopScaledFont(22, weight: destination == item ? .semibold : .medium).lineLimit(1)
                        Spacer(minLength: 0)
                    }.padding(.horizontal, 14).padding(.vertical, 16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(destination == item ? accent.opacity(0.16) : .clear, in: RoundedRectangle(cornerRadius: 14))
                        .contentShape(RoundedRectangle(cornerRadius: 14))
                }.buttonStyle(DesktopHoverStyle()).accessibilityAddTraits(destination == item ? .isSelected : [])
            }
        }
    }
    private func desktopMiniPlayer(_ track: Track) -> some View {
        DesktopTransportBar(model: model, track: track) { destination = .player }
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
    @State private var checkingServer = false
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                Image("BrandMark").resizable().scaledToFit().frame(width: 84, height: 96).accessibilityHidden(true)
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
                        checkingServer = true; connected = false
                        let origin = address; let localHTTP = allowHTTP
                        Task {
                            defer { checkingServer = false }
                            do { try model.connect(origin, localHTTP: localHTTP); connected = await model.restore() }
                            catch { model.report(error) }
                        }
                    }.buttonStyle(.borderedProminent).disabled(model.busy || checkingServer || address.isEmpty)
                    if checkingServer { ProgressView("Server wird geprüft …") }
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
                            let loginName = username; let secret = password; password = ""
                            Task { await model.login(username: loginName, password: secret) }
                        }.buttonStyle(.borderedProminent).disabled(model.busy || checkingServer || username.isEmpty || password.isEmpty)
                        if model.busy { ProgressView("Anmeldung läuft …") }
                    }.textFieldStyle(.roundedBorder)
                    #endif
                }
                Text("Keine Analyse- oder Werbe-SDKs. Sitzungen bleiben im Schlüsselbund dieses Geräts.").font(.footnote).foregroundStyle(.secondary)
            }.padding(32).frame(maxWidth: 560).frame(maxWidth: .infinity)
        }
        .onChange(of: address) { _, _ in connected = false; password = "" }
        .onChange(of: allowHTTP) { _, _ in connected = false; password = "" }
        .task {
            if address.hasPrefix("https://") {
                checkingServer = true
                defer { checkingServer = false }
                do { try model.connect(address, localHTTP: false); connected = await model.restore() }
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
            if kind == "tracks", model.player.current?.id == id, let image = model.player.artwork {
                Image(decorative: image, scale: 1).resizable().scaledToFill()
            } else if let client = model.client, let request = try? client.artworkRequest(kind: kind, id: id) {
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
    @AppStorage("desktopCoverSize") private var coverSize = 260.0
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 24) {
                #if os(macOS)
                HStack(alignment: .center, spacing: 16) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Deine Alben").desktopScaledFont(30, weight: .bold)
                        Text("Musik aus deiner Bibliothek").desktopScaledFont(17).foregroundStyle(.secondary)
                    }
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
                LazyVGrid(columns: [GridItem(.adaptive(minimum: cardMinimum, maximum: max(280, cardMinimum + 40)), spacing: 20)], spacing: 24) {
                    ForEach(model.releases) { release in
                        NavigationLink { CollectionView(model: model, kind: .release(release)) } label: {
                            VStack(alignment: .leading, spacing: 8) {
                                ArtworkView(model: model, kind: "releases", id: release.id)
                                Text(release.title).desktopScaledFont(18, weight: .semibold, fallback: .headline).foregroundStyle(.primary).lineLimit(2)
                                Text(release.artists.joined(separator: " · ")).desktopScaledFont(16, fallback: .subheadline).foregroundStyle(.secondary).lineLimit(1)
                            }.frame(maxWidth: .infinity, alignment: .leading)
                        }.buttonStyle(.plain)
                    }
                }
                if model.moreReleases { Button("Weitere Alben laden") { Task { await model.loadMore() } }.disabled(model.connecting) }
            }.padding(contentPadding)
                #if os(macOS)
                .frame(maxWidth: 1600).frame(maxWidth: .infinity)
                #endif
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
        coverSize.isFinite ? min(340, max(220, coverSize)) : 260
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
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 270), spacing: 24)], spacing: 24) {
                    ForEach(artists) { artist in
                        NavigationLink { CollectionView(model: model, kind: .artist(artist)) } label: {
                            VStack(spacing: 10) {
                                ArtworkView(model: model, kind: "artists", id: artist.id).clipShape(Circle())
                                Text(artist.name).desktopScaledFont(18, weight: .semibold).foregroundStyle(.primary).lineLimit(1)
                                Text("\(artist.trackCount ?? 0) Titel").desktopScaledFont(16).foregroundStyle(.secondary)
                            }.padding(8).frame(maxWidth: .infinity)
                        }.buttonStyle(.plain)
                    }
                }.padding(28).frame(maxWidth: 1600).frame(maxWidth: .infinity)
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
        }
        #if os(macOS)
        .desktopScaledFont(17).listStyle(.plain).scrollContentBackground(.hidden).padding(.horizontal, 24)
            .frame(maxWidth: 1400).frame(maxWidth: .infinity)
        #endif
        .navigationTitle(kind.title).task { if tracks.isEmpty { await load() } }
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
                        Text(track.title).desktopScaledFont(18, weight: .semibold, fallback: .headline).lineLimit(1)
                        Text(track.artistText).desktopScaledFont(16, fallback: .subheadline).foregroundStyle(.secondary).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    Text(formatTime(track.duration)).desktopScaledFont(15, fallback: .caption).monospacedDigit().foregroundStyle(.secondary)
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
    @Bindable var model: AppModel
    @State private var localQuery = ""
    private var query: String {
        #if os(macOS)
        model.query
        #else
        localQuery
        #endif
    }
    @State private var results: SearchResults?
    @State private var busy = false
    @State private var failure: String?
    var body: some View {
        Group {
            #if os(macOS)
            desktopResults
            #else
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
            .searchable(text: $localQuery, prompt: "Titel, Alben, Künstler")
            #endif
        }
        .navigationTitle("Suche")
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
    #if os(macOS)
    private var desktopResults: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                if busy { ProgressView("Suche …").desktopScaledFont(17) }
                if let failure { Text(failure).desktopScaledFont(17).foregroundStyle(.secondary) }
                if let results {
                    if !results.artists.isEmpty {
                        Text("Künstler").desktopScaledFont(25, weight: .bold)
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190, maximum: 240), spacing: 24)], spacing: 24) {
                            ForEach(results.artists) { artist in
                                NavigationLink { CollectionView(model: model, kind: .artist(artist)) } label: {
                                    VStack(spacing: 12) {
                                        ArtworkView(model: model, kind: "artists", id: artist.id).clipShape(Circle())
                                        Text(artist.name).desktopScaledFont(19, weight: .semibold).foregroundStyle(.primary)
                                    }.padding(12)
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                    if !results.releases.isEmpty {
                        Text("Alben").desktopScaledFont(25, weight: .bold)
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 190, maximum: 260), spacing: 24)], spacing: 24) {
                            ForEach(results.releases) { release in
                                NavigationLink { CollectionView(model: model, kind: .release(release)) } label: {
                                    VStack(alignment: .leading, spacing: 10) {
                                        ArtworkView(model: model, kind: "releases", id: release.id)
                                        Text(release.title).desktopScaledFont(18, weight: .semibold).foregroundStyle(.primary)
                                        Text(release.artists.joined(separator: " · ")).desktopScaledFont(16).foregroundStyle(.secondary)
                                    }
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                    if !results.tracks.isEmpty {
                        Text("Titel").desktopScaledFont(25, weight: .bold)
                        LazyVStack(spacing: 12) {
                            ForEach(results.tracks) { track in
                                TrackRow(model: model, track: track) {
                                    if let client = model.client { model.player.play(results.tracks, start: results.tracks.firstIndex(of: track) ?? 0, client: client) }
                                }.padding(12).background(.quaternary.opacity(0.3), in: RoundedRectangle(cornerRadius: 14))
                            }
                        }
                    }
                    if results.artists.isEmpty && results.releases.isEmpty && results.tracks.isEmpty { ContentUnavailableView.search(text: query) }
                } else if !busy && failure == nil {
                    VStack(alignment: .leading, spacing: 12) {
                        Text("Was möchtest du hören?").desktopScaledFont(34, weight: .bold)
                        Text("Suche oben nach einem Titel, Album oder Künstler. ⌘F bringt dich direkt ins Suchfeld.")
                            .desktopScaledFont(18).foregroundStyle(.secondary)
                    }.padding(.vertical, 20)
                    if !model.releases.isEmpty {
                        Text("Aus deiner Bibliothek").desktopScaledFont(24, weight: .semibold)
                        LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 280), spacing: 24)], spacing: 24) {
                            ForEach(Array(model.releases.prefix(8))) { release in
                                NavigationLink { CollectionView(model: model, kind: .release(release)) } label: {
                                    VStack(alignment: .leading, spacing: 10) {
                                        ArtworkView(model: model, kind: "releases", id: release.id)
                                        Text(release.title).desktopScaledFont(18, weight: .semibold).foregroundStyle(.primary)
                                        Text(release.artists.joined(separator: " · ")).desktopScaledFont(16).foregroundStyle(.secondary)
                                    }
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                }
            }.padding(32).frame(maxWidth: 1450, alignment: .leading).frame(maxWidth: .infinity, alignment: .top)
        }
    }
    #endif

}

struct PlaylistListView: View {
    var model: AppModel
    var body: some View {
        List {
            if model.playlists.isEmpty { ContentUnavailableView("Noch keine Playlists", systemImage: "music.note.list", description: Text("Playlists im Web anlegen. Sie erscheinen hier nach dem Aktualisieren.")) }
            ForEach(model.playlists) { playlist in
                NavigationLink { CollectionView(model: model, kind: .playlist(playlist)) } label: {
                    VStack(alignment: .leading, spacing: 8) { Text(playlist.name).desktopScaledFont(20, weight: .semibold, fallback: .headline); Text("\(playlist.trackCount) Titel · \(formatTime(Double(playlist.durationMs)/1000))").desktopScaledFont(16, fallback: .body).foregroundStyle(.secondary) }.padding(.vertical, 14)
                }
            }
        }
        #if os(macOS)
        .listStyle(.plain).scrollContentBackground(.hidden).padding(24).frame(maxWidth: 1400).frame(maxWidth: .infinity)
        #endif
        .navigationTitle("Playlists")
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
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
    #if os(macOS)
    private func desktopPlayer(_ size: CGSize) -> some View {
        DesktopListeningView(model: model, size: size)
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
                                Text(track.title).desktopScaledFont(18, weight: .semibold, fallback: .headline).lineLimit(1)
                                Text(track.artistText).desktopScaledFont(16, fallback: .subheadline).foregroundStyle(.secondary).lineLimit(1)
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
