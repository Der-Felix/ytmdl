import SwiftUI
import AVKit
import CoreImage.CIFilterBuiltins
import YTMDLCore

enum Destination: String, CaseIterable, Identifiable, Hashable {
    case home = "Start", library = "Bibliothek", artists = "Künstler", search = "Suche", favorites = "Favoriten", playlists = "Playlists", player = "Player", downloads = "Offline-Musik", settings = "Einstellungen"
    var id: String { rawValue }
    var icon: String {
        switch self { case .downloads: "arrow.down.circle"; case .home: "house.fill"; case .library: "square.stack"; case .artists: "person.2"; case .search: "magnifyingglass"; case .favorites: "heart"; case .playlists: "music.note.list"; case .player: "play.circle"; case .settings: "gearshape" }
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
    #elseif os(iOS)
    @State private var destination: Destination? = .home
    #else
    @State private var destination: Destination? = .library
    #endif
    @State private var expandedPlayer = false
    @State private var detailVisit = UUID()
    @State private var detailPath = NavigationPath()
    private struct DetailIdentity: Hashable {
        let selection: Destination?
        let visit: UUID
    }
    // Value-based sidebar selection must also replace the view-based navigation
    // stack. Otherwise a pushed album/playlist can conceal the newly selected root.
    private func selectDestination(_ item: Destination) {
        detailPath = NavigationPath()
        if destination == item { detailVisit = UUID() }
        destination = item
        #if os(macOS)
        searchFocused = item == .search
        playerOverlayOpen = false
        #endif
    }
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
                        NavigationStack { routedContent(item) }.tabItem { Label(item == .settings ? "Optionen" : item.rawValue, systemImage: item.icon) }.tag(Optional(item))
                    }
                }
                #elseif os(iOS)
                if model.offlineMode { NavigationStack { OfflineLibraryView(model: model) } }
                else if sizeClass == .compact {
                    TabView(selection: $destination) {
                        ForEach([Destination.home, .search, .library, .playlists, .settings]) { item in
                            NavigationStack {
                                routedContent(item).toolbar {
                                    ToolbarItem(placement: .topBarTrailing) {
                                        Button("Player", systemImage: "play.circle") { expandedPlayer = true }
                                            .accessibilityIdentifier("open-mobile-player")
                                    }
                                }
                            }
                                .tabItem { Label(item.rawValue, systemImage: item.icon) }.tag(Optional(item))
                        }
                    }.tabBarMinimizeBehavior(.never)
                        .tabViewBottomAccessory(isEnabled: model.player.current != nil) {
                            MobileMiniPlayer(model: model) { expandedPlayer = true }
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
        .onChange(of: model.query) { if !model.query.isEmpty && destination != .search { selectDestination(.search) } }
        .onChange(of: searchFocused) { if searchFocused && destination != .search { selectDestination(.search) } }
        #endif
        .onChange(of: destination) { if !detailPath.isEmpty { detailPath = NavigationPath() } }
        .onChange(of: model.user?.id) { detailPath = NavigationPath(); detailVisit = UUID() }
        #if DEBUG
        .task {
            await model.loadFixtureIfRequested()
            #if os(macOS)
            let args = ProcessInfo.processInfo.arguments
            if let index = args.firstIndex(of: "--fixture-view"), args.indices.contains(index + 1),
               ["127.0.0.1", "::1"].contains(model.client?.server.url.host ?? ""),
               let requested = Destination(rawValue: args[index + 1]) { selectDestination(requested) }
            #endif
            if ProcessInfo.processInfo.arguments.contains("--fixture-player") {
                #if os(iOS)
                expandedPlayer = true
                #else
                selectDestination(.player)
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
            List(Destination.allCases, selection: $destination) { item in
                Label(item.rawValue, systemImage: item.icon).tag(item)
            }.navigationTitle("YTMDL").navigationSplitViewColumnWidth(min: 180, ideal: 200, max: 240)
            #endif
        } detail: {
            NavigationStack(path: $detailPath) {
                routedContent(destination ?? .library)
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
                        Button("Suche öffnen", systemImage: "magnifyingglass") { selectDestination(.search) }
                            .keyboardShortcut("f", modifiers: .command)
                    }
                    if destination == .player {
                        ToolbarItem(placement: .navigation) {
                            Button("Zurück zur Bibliothek", systemImage: "chevron.left") { selectDestination(.library) }
                                .keyboardShortcut(playerOverlayOpen ? nil : .cancelAction)
                        }
                    }
                }
                .task {
                    // The old search field is removed with the stack. Request focus
                    // only after the replacement field has joined the view tree.
                    guard destination == .search else { return }
                    searchFocused = false
                    try? await Task.sleep(for: .milliseconds(120))
                    guard !Task.isCancelled, destination == .search else { return }
                    searchFocused = true
                }
                #endif
            }.id(DetailIdentity(selection: destination, visit: detailVisit))
        }
    }
    private func routedContent(_ destination: Destination) -> some View {
        content(destination)
            .navigationDestination(for: CollectionKind.self) { kind in CollectionView(model: model, kind: kind) }
            .navigationDestination(for: Destination.self) { item in content(item) }
    }
    @ViewBuilder private func content(_ destination: Destination) -> some View {
        switch destination {
        case .home:
            #if os(macOS)
            DesktopHomeView(model: model, navigate: selectDestination)
            #else
            #if os(iOS)
            MobileHomeView(model: model)
            #else
            LibraryView(model: model)
            #endif
            #endif
        case .library: LibraryView(model: model)
        case .artists: ArtistListView(model: model)
        case .search: SearchView(model: model)
        case .favorites: CollectionView(model: model, kind: .favorites)
        case .playlists: PlaylistListView(model: model)
        case .player: NowPlayingView(model: model)
        case .downloads:
            #if os(iOS)
            OfflineLibraryView(model: model)
            #else
            ContentUnavailableView("Offline-Musik", systemImage: "arrow.down.circle", description: Text("Offline-Verwaltung ist derzeit auf iPhone und iPad verfügbar."))
            #endif
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
                    selectDestination(item)
                } label: {
                    HStack(spacing: 18) {
                        Image(systemName: item.icon).desktopScaledFont(23).foregroundStyle(accent).frame(width: 32)
                        Text(item.rawValue).desktopScaledFont(22, weight: destination == item ? .semibold : .medium).lineLimit(1)
                        Spacer(minLength: 0)
                    }.padding(.horizontal, 14).padding(.vertical, 16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(destination == item ? accent.opacity(0.16) : .clear, in: RoundedRectangle(cornerRadius: 14))
                        .contentShape(RoundedRectangle(cornerRadius: 14))
                }.buttonStyle(DesktopHoverStyle()).accessibilityIdentifier("sidebar-" + item.id).accessibilityAddTraits(destination == item ? .isSelected : [])
            }
        }
    }
    private func desktopMiniPlayer(_ track: Track) -> some View {
        DesktopTransportBar(model: model, track: track) { selectDestination(.player) }
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
    @State private var offlinePicker = false
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
                #if os(iOS)
                if !model.offline.profiles.isEmpty {
                    Button("Offline-Musik öffnen", systemImage: "arrow.down.circle") { offlinePicker = true }.buttonStyle(.bordered)
                        .sheet(isPresented: $offlinePicker) {
                            NavigationStack {
                                List(model.offline.profiles) { profile in
                                    Button { model.openOffline(profile); offlinePicker = false } label: {
                                        VStack(alignment: .leading) { Text(profile.user.displayName).font(.headline); Text(profile.origin).font(.caption).foregroundStyle(.secondary) }
                                    }
                                }.navigationTitle("Offline-Sammlungen").toolbar { ToolbarItem(placement: .cancellationAction) { Button("Abbrechen") { offlinePicker = false } } }
                            }
                        }
                }
                #endif
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
            } else if kind == "tracks", let url = model.offline.artworkURL(id), let data = try? Data(contentsOf: url), let image = ArtworkPalette.thumbnail(data) {
                Image(decorative: image, scale: 1).resizable().scaledToFill()
            } else if !model.offlineMode, let client = model.client, let request = try? client.artworkRequest(kind: kind, id: id) {
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
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { collectionShortcuts }.fixedSize(horizontal: true, vertical: false)
                    VStack(alignment: .leading, spacing: 8) { collectionShortcuts }
                }.buttonStyle(.bordered)
                genrePicker
                #endif
                if model.connecting && model.releases.isEmpty { ProgressView("Bibliothek wird geladen …") }
                else if model.releases.isEmpty { ContentUnavailableView("Noch keine Alben", systemImage: "square.stack", description: Text("Musik im Web hinzufügen oder einen anderen Genre-Filter wählen.")) }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: cardMinimum, maximum: max(280, cardMinimum + 40)), spacing: 20)], spacing: 24) {
                    ForEach(model.releases) { release in
                        NavigationLink(value: CollectionKind.release(release)) {
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
    private var collectionShortcuts: some View {
        Group {
            NavigationLink(value: Destination.artists) { Label("Künstler", systemImage: "person.2") }
            NavigationLink(value: CollectionKind.favorites) { Label("Favoriten", systemImage: "heart.fill") }
            #if os(iOS)
            NavigationLink(value: Destination.downloads) { Label("Offline", systemImage: "arrow.down.circle") }
            #endif
        }.lineLimit(1)
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
                        NavigationLink(value: CollectionKind.artist(artist)) {
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
                    NavigationLink(value: CollectionKind.artist(artist)) {
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
            guard model.client === client else { return }
            artists += result; offset += result.count; more = result.count == 60
        } catch { if model.client === client { model.report(error) } }
    }
}

enum CollectionKind: Hashable {
    // Routes keep their display metadata while navigation identity uses only kind/id.
    private var routeID: String {
        switch self {
        case .favorites: "favorites"
        case .release(let value): "release:" + value.id
        case .artist(let value): "artist:" + value.id
        case .playlist(let value): "playlist:" + value.id
        }
    }
    static func == (lhs: Self, rhs: Self) -> Bool { lhs.routeID == rhs.routeID }
    func hash(into hasher: inout Hasher) { hasher.combine(routeID) }
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
    @State private var editor = false
    @State private var adding = false
    @State private var deleting = false
    @State private var failure: String?
    @State private var loadFailure: String?
    @Environment(\.dismiss) private var dismiss
    private var playlist: Playlist? {
        guard case .playlist(let value) = kind else { return nil }
        return model.playlists.first { $0.id == value.id } ?? value
    }
    private var collectionTitle: String { playlist?.name ?? kind.title }
    #if os(macOS)
    @State private var collectionQuery = ""
    @State private var collectionSort = "original"
    #endif
    var body: some View {
        Group {
            #if os(macOS)
            if isListeningCollection { desktopCollection }
            else { standardCollection }
            #else
            standardCollection
            #endif
        }.navigationTitle(collectionTitle).task { if tracks.isEmpty { await load() } }
        .toolbar {
            #if os(iOS)
            ToolbarItem {
                Button("Offline speichern", systemImage: "arrow.down.circle") { Task { await model.downloadCollection(kind) } }
                    .disabled(busy || model.offlineMode)
            }
            #endif
            if let playlist {
                ToolbarItem {
                    Menu("Playlist", systemImage: "music.note.list") {
                        Button("Bearbeiten", systemImage: "pencil") { editor = true }
                        if playlist.smartRules == nil {
                            Button("Titel hinzufügen", systemImage: "plus") { adding = true }
                        }
                        Button("Neu laden", systemImage: "arrow.clockwise") { Task { await reloadPlaylist() } }
                        Divider()
                        Button("Playlist löschen", systemImage: "trash", role: .destructive) { deleting = true }
                    }.disabled(busy || model.playlistBusy)
                }
            }
        }
        .sheet(isPresented: $editor) {
            if let playlist { PlaylistEditor(model: model, playlist: playlist) { _ in Task { await reloadPlaylist() } } }
        }
        .sheet(isPresented: $adding) {
            if let playlist { PlaylistTrackPicker(model: model, playlist: playlist, existing: Set(tracks.map(\.id))) { tracks = $0.tracks } }
        }
        .confirmationDialog("Playlist löschen?", isPresented: $deleting, titleVisibility: .visible) {
            Button("Playlist löschen", role: .destructive) {
                Task {
                    guard let playlist, let client = model.client else { return }
                    do { try await model.deletePlaylist(playlist); if model.client === client { dismiss() } }
                    catch is CancellationError { }
                    catch { if model.client === client { failure = playlistFailure(error) } }
                }
            }
        } message: { Text("Die Playlist wird entfernt. Ihre Titel und Audiodateien bleiben in der Bibliothek.") }
        .alert("Playlist konnte nicht geändert werden", isPresented: Binding(get: { failure != nil }, set: { if !$0 { failure = nil } })) {
            Button("OK") { failure = nil }
        } message: { Text(failure ?? "") }
        .onChange(of: model.favoriteIDs) { old, new in
            guard isFavorites else { return }
            let removed = old.subtracting(new)
            let count = tracks.filter { removed.contains($0.id) }.count
            tracks.removeAll { removed.contains($0.id) }
            offset = max(0, offset - count)
        }
        .refreshable {
            if playlist != nil { await reloadPlaylist() }
            else { tracks = []; offset = 0; more = true; await load() }
        }
    }
    private var standardCollection: some View {
        List {
            Section {
                #if os(iOS)
                MobileCollectionArtwork(model: model, tracks: tracks).frame(height: 180)
                    .frame(maxWidth: .infinity).listRowBackground(Color.clear)
                Text("\(tracks.count)\(more ? " geladene" : "") Titel · \(formatTime(tracks.reduce(0) { $0 + $1.duration }))")
                    .font(.subheadline).foregroundStyle(.secondary)
                if let description = playlist?.description, !description.isEmpty { Text(description).font(.subheadline).foregroundStyle(.secondary) }
                #endif
                HStack {
                    Button("Abspielen", systemImage: "play.fill") { play(tracks) }.buttonStyle(.borderedProminent).disabled(tracks.isEmpty)
                    Button("Zufall", systemImage: "shuffle") { play(tracks.shuffled()) }.buttonStyle(.bordered).disabled(tracks.isEmpty)
                }.padding(.vertical, 12)
            }
            Section {
                ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in
                    TrackRow(model: model, track: track, playlistEdit: playlistEdit(track, index: index)) { play(tracks, index: index) }
                }
                if busy { ProgressView("Titel werden geladen …") }
                else if let loadFailure {
                    Text(loadFailure).foregroundStyle(.secondary)
                    Button("Titel erneut laden") { Task { await load() } }
                }
                else if tracks.isEmpty { ContentUnavailableView("Noch keine Titel", systemImage: "music.note") }
                if more && !tracks.isEmpty { Button("Weitere Titel laden") { Task { await load() } } }
            }
        }
        #if os(macOS)
        .desktopScaledFont(17).listStyle(.plain).scrollContentBackground(.hidden).padding(.horizontal, 24)
            .frame(maxWidth: 1400).frame(maxWidth: .infinity)
        #endif
    }
    private var isFavorites: Bool { if case .favorites = kind { true } else { false } }
    #if os(macOS)
    private var isListeningCollection: Bool {
        switch kind { case .favorites, .playlist: true; default: false }
    }
    private var availableTracks: [Track] {
        isFavorites ? tracks.filter { model.favoriteIDs.contains($0.id) } : tracks
    }
    private var visibleTracks: [Track] {
        let query = collectionQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        let result = availableTracks.filter { query.isEmpty || $0.title.localizedCaseInsensitiveContains(query) || $0.artistText.localizedCaseInsensitiveContains(query) || $0.album.localizedCaseInsensitiveContains(query) }
        switch collectionSort {
        case "title": return result.sorted { $0.title.localizedStandardCompare($1.title) == .orderedAscending }
        case "artist": return result.sorted { $0.artistText.localizedStandardCompare($1.artistText) == .orderedAscending }
        default: return result
        }
    }
    private var desktopCollection: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                DesktopCollectionHeader(model: model, tracks: availableTracks, title: collectionTitle,
                    subtitle: isFavorites ? "Deine Musik, die bleibt." : playlist?.smartRules != nil ? "Intelligente Playlist · bei jedem Öffnen aktualisiert" : playlist?.description?.isEmpty == false ? playlist!.description! : "Deine Sammlung für diesen Moment.",
                    symbol: isFavorites ? "heart.fill" : "music.note.list", canPlay: !visibleTracks.isEmpty,
                    detail: "\(availableTracks.count)\(more ? " geladene" : "") Titel · \(formatTime(availableTracks.reduce(0) { $0 + $1.duration }))") {
                    play(visibleTracks)
                } shuffle: { play(visibleTracks.shuffled()) }
                if let playlist {
                    HStack {
                        collectionTool("Playlist bearbeiten", icon: "pencil") { editor = true }
                        if playlist.smartRules == nil { collectionTool("Titel hinzufügen", icon: "plus") { adding = true } }
                        Spacer()
                        if playlist.smartRules != nil { Label("Automatisch zusammengestellt", systemImage: "sparkles").foregroundStyle(.secondary) }
                    }.disabled(busy || model.playlistBusy)
                }
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 20) { collectionFilter; collectionSortMenu }
                    VStack(alignment: .leading, spacing: 14) { collectionFilter; collectionSortMenu }
                }
                LazyVStack(spacing: 0) {
                    ForEach(Array(visibleTracks.enumerated()), id: \.offset) { index, track in
                        TrackRow(model: model, track: track, playlistEdit: playlistEdit(track, index: index)) { play(visibleTracks, index: index) }
                            .id(track.id).disabled(busy || model.playlistBusy).padding(.horizontal, 16).padding(.vertical, 6)
                        if index < visibleTracks.count - 1 { Divider().padding(.horizontal, 20).opacity(0.35) }
                    }
                    if busy { ProgressView("Titel werden geladen …").padding(28) }
                    else if let loadFailure {
                        Text(loadFailure).foregroundStyle(.secondary).padding(24)
                        Button("Titel erneut laden") { Task { await load() } }.padding(.bottom, 24)
                    }
                    else if visibleTracks.isEmpty {
                        ContentUnavailableView(collectionQuery.isEmpty ? "Noch keine Titel" : "Keine passenden Titel",
                            systemImage: collectionQuery.isEmpty ? (isFavorites ? "heart" : "music.note.list") : "magnifyingglass",
                            description: Text(collectionQuery.isEmpty ? (isFavorites ? "Markiere Titel mit dem Herz. Deine Lieblingstitel erscheinen hier." : "Füge Titel hinzu oder passe die Regeln deiner intelligenten Playlist an.") : "Suche nach Titel, Künstler oder Album."))
                            .padding(24)
                    }
                }.background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 20))
                if more && !tracks.isEmpty {
                    Button("Weitere Titel laden") { Task { await load() } }.buttonStyle(.bordered).disabled(busy)
                }
            }.padding(32).frame(maxWidth: 1500).frame(maxWidth: .infinity, alignment: .top)
        }
    }
    private func collectionTool(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Label(title, systemImage: icon).desktopScaledFont(16).padding(.horizontal, 16).frame(minHeight: 44)
                .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 12))
        }.buttonStyle(DesktopHoverStyle(radius: 12))
    }
    private var collectionFilter: some View {
        HStack(spacing: 12) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField("Titel, Künstler oder Album filtern", text: $collectionQuery).textFieldStyle(.plain)
            if !collectionQuery.isEmpty {
                Button { collectionQuery = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain).accessibilityLabel("Sammlungsfilter leeren")
            }
        }.desktopScaledFont(17).padding(14).frame(minWidth: 200, maxWidth: .infinity)
            .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 12))
            .accessibilityLabel("Geladene Titel filtern")
    }
    private var collectionSortMenu: some View {
        Menu {
            Picker("Sortierung", selection: $collectionSort) {
                Text(isFavorites ? "Reihenfolge der Sammlung" : "Playlist-Reihenfolge").tag("original")
                Text("Titel A–Z").tag("title")
                Text("Künstler A–Z").tag("artist")
            }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: "arrow.up.arrow.down")
                Text(collectionSort == "title" ? "Titel A–Z" : collectionSort == "artist" ? "Künstler A–Z" : "Reihenfolge")
                Image(systemName: "chevron.down").font(.caption)
            }.desktopScaledFont(15).padding(.horizontal, 14).frame(minHeight: 44)
                .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 12))
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 12)).fixedSize()
            .accessibilityLabel("Titel sortieren")
    }
    #endif
    private var canReorder: Bool {
        #if os(macOS)
        collectionQuery.isEmpty && collectionSort == "original" && !model.playlistBusy && !busy
        #else
        !model.playlistBusy && !busy
        #endif
    }
    private func playlistEdit(_ track: Track, index: Int) -> PlaylistRowEdit? {
        guard let playlist, playlist.smartRules == nil else { return nil }
        return PlaylistRowEdit(canMoveUp: canReorder && index > 0,
            canMoveDown: canReorder && index < tracks.count - 1,
            moveUp: { Task { await moveTrack(index, by: -1) } },
            moveDown: { Task { await moveTrack(index, by: 1) } },
            remove: {
                Task {
                    guard let client = model.client else { return }
                    do { let result = try await model.changePlaylistTracks(playlist, ids: [], removing: track.id); tracks = result.tracks }
                    catch is CancellationError { }
                    catch { if model.client === client { failure = playlistFailure(error) } }
                }
            })
    }
    private func moveTrack(_ index: Int, by distance: Int) async {
        guard canReorder, let playlist, let client = model.client, tracks.indices.contains(index), tracks.indices.contains(index + distance) else { return }
        var reordered = tracks; reordered.swapAt(index, index + distance)
        do { let result = try await model.changePlaylistTracks(playlist, ids: reordered.map(\.id), reorder: true); tracks = result.tracks }
        catch is CancellationError { }
        catch { if model.client === client { failure = playlistFailure(error) } }
    }
    private func reloadPlaylist() async {
        guard let playlist, let client = model.client, !busy else { return }
        busy = true; defer { busy = false }
        do {
            let detail: PlaylistDetail = try await client.get(try client.playlistPath(playlist.id))
            guard model.client === client else { return }
            tracks = detail.tracks; model.rememberPlaylist(detail.playlist(fallback: playlist)); more = false
        } catch is CancellationError { }
        catch { if model.client === client { failure = playlistFailure(error) } }
    }
    private func play(_ tracks: [Track], index: Int = 0) { if let client = model.client { model.player.play(tracks, start: index, client: client) } }
    private func load() async {
        guard let client = model.client, !busy else { return }
        loadFailure = nil
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
                let detail: PlaylistDetail = try await client.get(try client.playlistPath(playlist.id))
                guard model.client === client else { return }
                model.rememberPlaylist(detail.playlist(fallback: playlist)); result = detail.tracks
            }
            try Task.checkCancellation()
            guard model.client === client else { return }
            tracks += result; offset += result.count
            if case .playlist = kind { more = false } else { more = result.count == 100 }
        } catch is CancellationError { }
        catch {
            if model.client === client, !Task.isCancelled {
                loadFailure = "Titel konnten nicht geladen werden. Bitte Verbindung prüfen und erneut versuchen."
            }
        }
    }
}

struct PlaylistRowEdit {
    let canMoveUp: Bool
    let canMoveDown: Bool
    let moveUp: () -> Void
    let moveDown: () -> Void
    let remove: () -> Void
}
struct TrackRow: View {
    var model: AppModel
    var track: Track
    var playlistEdit: PlaylistRowEdit? = nil
    var action: () -> Void
    @State private var addingToPlaylist = false
    var body: some View {
        Group {
            #if os(macOS)
            desktopRow
            #else
            standardRow
            #endif
        }
        .sheet(isPresented: $addingToPlaylist) { AddToPlaylistSheet(model: model, tracks: [track]) }
    }
    @ViewBuilder private var playlistMenuItems: some View {
        if let edit = playlistEdit {
            Divider()
            Button("In Playlist nach oben", systemImage: "arrow.up", action: edit.moveUp).disabled(!edit.canMoveUp)
            Button("In Playlist nach unten", systemImage: "arrow.down", action: edit.moveDown).disabled(!edit.canMoveDown)
            Button("Aus Playlist entfernen", systemImage: "minus.circle", role: .destructive, action: edit.remove)
            Divider()
        }
    }
    private var standardRow: some View {
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
                Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { addingToPlaylist = true }
                playlistMenuItems
                Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten", systemImage: "heart") { Task { await model.toggleFavorite(track) } }
                #if os(iOS)
                Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }
                Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }.disabled(model.listeningBusy)
                #endif
                Button("Zur Warteschlange", systemImage: "text.badge.plus") { if let client = model.client { model.player.append(track, client: client) } }
            } label: { Image(systemName: "ellipsis").frame(minWidth: 44, minHeight: 44) }.accessibilityLabel("Aktionen für \(track.title)")
        }.padding(.vertical, 4)
    }
    #if os(macOS)
    @Environment(\.desktopAccent) private var accent
    @State private var favoriteBusy = false
    private var desktopRow: some View {
        HStack(spacing: 12) {
            Button(action: action) {
                HStack(spacing: 16) {
                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 56, height: 56)
                    VStack(alignment: .leading, spacing: 5) {
                        Text(track.title).desktopScaledFont(18, weight: .semibold).foregroundStyle(model.player.current?.id == track.id ? accent : .primary).lineLimit(1)
                        Text(track.album.isEmpty ? track.artistText : "\(track.artistText) · \(track.album)")
                            .desktopScaledFont(15).foregroundStyle(.secondary).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    Text(formatTime(track.duration)).desktopScaledFont(14).monospacedDigit().foregroundStyle(.secondary)
                }.padding(8).contentShape(RoundedRectangle(cornerRadius: 10))
            }.buttonStyle(DesktopHoverStyle(radius: 10)).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
            Button {
                favoriteBusy = true
                Task { await model.toggleFavorite(track); favoriteBusy = false }
            } label: {
                Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart")
                    .foregroundStyle(model.favoriteIDs.contains(track.id) ? accent : .secondary).frame(width: 40, height: 44)
            }.buttonStyle(DesktopHoverStyle(radius: 8)).disabled(favoriteBusy)
                .accessibilityLabel(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen")
                .help(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen")
            Button {
                if let client = model.client { model.player.append(track, client: client) }
            } label: { Image(systemName: "text.badge.plus").frame(width: 40, height: 44) }
                .buttonStyle(DesktopHoverStyle(radius: 8)).foregroundStyle(.secondary)
                .disabled(model.player.queue.tracks.count >= 500)
                .accessibilityLabel("Zur Warteschlange hinzufügen").help("Zur Warteschlange hinzufügen")
            Menu {
                Button("Jetzt abspielen", systemImage: "play.fill", action: action)
                Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { addingToPlaylist = true }
                playlistMenuItems
                Divider()
                Button("Titel und Künstler kopieren", systemImage: "doc.on.doc") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString("\(track.artistText) – \(track.title)", forType: .string)
                }
            } label: {
                HStack(spacing: 6) { Text("Aktionen"); Image(systemName: "chevron.down").font(.caption) }
                    .desktopScaledFont(14).foregroundStyle(.secondary).padding(.horizontal, 10).frame(minHeight: 44)
            }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8))
                .accessibilityLabel("Aktionen für \(track.title)")
        }.padding(.vertical, 4)
    }
    #endif

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
                            NavigationLink(value: CollectionKind.artist(artist)) {
                                HStack { ArtworkView(model: model, kind: "artists", id: artist.id).frame(width: 56, height: 56); Text(artist.name) }
                            }
                        }
                    }
                    Section("Alben") {
                        ForEach(results.releases) { release in
                            NavigationLink(value: CollectionKind.release(release)) {
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
                                NavigationLink(value: CollectionKind.artist(artist)) {
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
                                NavigationLink(value: CollectionKind.release(release)) {
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
                                NavigationLink(value: CollectionKind.release(release)) {
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
    @State private var creating = false
    #if os(macOS)
    @State private var playlistQuery = ""
    @State private var alphabetical = false
    private var visiblePlaylists: [Playlist] {
        let query = playlistQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        let result = model.playlists.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }
        return alphabetical ? result.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending } : result
    }
    #endif
    var body: some View {
        Group {
            #if os(macOS)
            desktopPlaylists
            #else
            List {
                if model.playlists.isEmpty { emptyPlaylists }
                ForEach(model.playlists) { playlist in
                    NavigationLink(value: CollectionKind.playlist(playlist)) {
                        VStack(alignment: .leading, spacing: 8) { Text(playlist.name).desktopScaledFont(20, weight: .semibold, fallback: .headline); Text("\(playlist.trackCount) Titel · \(formatTime(Double(playlist.durationMs)/1000))").desktopScaledFont(16, fallback: .body).foregroundStyle(.secondary) }.padding(.vertical, 14)
                    }
                }
            }
            #endif
        }.navigationTitle("Playlists")
            .toolbar {
                ToolbarItem { Button("Neue Playlist", systemImage: "plus") { creating = true }.disabled(model.playlistBusy) }
                ToolbarItem { Button("Aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }.disabled(model.connecting || model.playlistBusy) }
            }
            .sheet(isPresented: $creating) { PlaylistEditor(model: model) }
    }
    private var emptyPlaylists: some View {
        ContentUnavailableView("Noch keine Playlists", systemImage: "music.note.list", description: Text("Erstelle deine erste Playlist direkt hier – mit eigenen Titeln oder intelligenten Regeln."))
    }
    #if os(macOS)
    private var desktopPlaylists: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 28) {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Deine Playlists").desktopScaledFont(32, weight: .bold)
                    Text("Für jeden Moment die passende Musik. \(model.playlists.count) Sammlungen.").desktopScaledFont(17).foregroundStyle(.secondary)
                    Button("Neue Playlist", systemImage: "plus") { creating = true }.buttonStyle(.borderedProminent).controlSize(.large).disabled(model.playlistBusy)
                }
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 20) { playlistFilter; playlistSort }
                    VStack(alignment: .leading, spacing: 14) { playlistFilter; playlistSort }
                }
                if model.playlists.isEmpty { emptyPlaylists.frame(maxWidth: .infinity).padding(32) }
                else if visiblePlaylists.isEmpty { ContentUnavailableView("Keine passende Playlist", systemImage: "magnifyingglass", description: Text("Versuche einen anderen Namen.")) }
                else {
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 280, maximum: 450), spacing: 24)], spacing: 24) {
                        ForEach(visiblePlaylists) { playlist in
                            NavigationLink(value: CollectionKind.playlist(playlist)) {
                                DesktopPlaylistCard(playlist: playlist)
                            }.buttonStyle(DesktopHoverStyle(radius: 20)).accessibilityLabel("Playlist öffnen: \(playlist.name), \(playlist.trackCount) Titel")
                        }
                    }
                }
            }.padding(32).frame(maxWidth: 1500).frame(maxWidth: .infinity, alignment: .top)
        }
    }
    private var playlistFilter: some View {
        HStack(spacing: 12) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
            TextField("Playlists filtern", text: $playlistQuery).textFieldStyle(.plain)
            if !playlistQuery.isEmpty {
                Button { playlistQuery = "" } label: { Image(systemName: "xmark.circle.fill") }
                    .buttonStyle(.plain).accessibilityLabel("Playlist-Filter leeren")
            }
        }.desktopScaledFont(17).padding(14).frame(minWidth: 200, maxWidth: .infinity)
            .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 12))
    }
    private var playlistSort: some View {
        Menu {
            Picker("Sortierung", selection: $alphabetical) {
                Text("Reihenfolge der Sammlung").tag(false)
                Text("Name A–Z").tag(true)
            }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: "arrow.up.arrow.down")
                Text(alphabetical ? "Name A–Z" : "Reihenfolge")
                Image(systemName: "chevron.down").font(.caption)
            }.desktopScaledFont(15).padding(.horizontal, 14).frame(minHeight: 44)
                .background(.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 12))
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 12)).fixedSize()
            .accessibilityLabel("Playlists sortieren")
    }
    #endif
}

struct NowPlayingView: View {
    var model: AppModel
    @State private var tab = 0
    var body: some View {
        GeometryReader { geometry in
            #if os(macOS)
            desktopPlayer(geometry.size)
            #elseif os(iOS)
            MobilePlayerView(model: model)
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
