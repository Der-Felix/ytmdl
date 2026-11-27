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
    #else
    @State private var destination: Destination? = .home
    #endif
    @State private var expandedPlayer = false
    @State private var pendingWidgetRoute: MusicWidgetRoute?
    #if os(iOS)
    @State private var mobilePaths: [Destination: NavigationPath] = [:]
    /// Grows when the Search tab is chosen while it is already shown.
    @State private var searchFocusRequest = 0
    #endif
    #if os(tvOS)
    /// Apple TV: "Jetzt läuft" over the tabs.
    @State private var tvNowPlaying = false
    @State private var tvPaths: [Destination: NavigationPath] = [:]
    /// Four short tabs and settings as an icon, so nothing is clipped at
    /// 1920 pt: Favoriten are in Bibliothek, playback in the bar and in
    /// "Jetzt läuft", which opens by itself when something is started.
    private static let tvTabs: [Destination] = [.home, .library, .search, .playlists, .settings]
    #endif
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
    @Environment(\.scenePhase) private var scenePhase
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    var body: some View {
        ZStack {
            if model.user == nil { ConnectView(model: model) }
            else {
                #if os(tvOS)
                TabView(selection: $destination) {
                    ForEach(Self.tvTabs) { item in
                        NavigationStack(path: Binding(get: { tvPaths[item] ?? NavigationPath() }, set: { tvPaths[item] = $0 })) {
                            routedContent(item)
                        }
                        // What plays, on every page of every tab.
                        .safeAreaInset(edge: .bottom, spacing: 0) {
                            TVNowPlayingBar(model: model) { tvNowPlaying = true }
                        }
                        .tabItem { tvTabLabel(item) }.tag(Optional(item))
                    }
                }
                .fullScreenCover(isPresented: $tvNowPlaying) {
                    TVNowPlayingView(model: model) { kind in
                        // Back from the player into the shown tab, then the page.
                        tvNowPlaying = false
                        let tab = destination ?? .home
                        var path = tvPaths[tab] ?? NavigationPath()
                        path.append(kind)
                        tvPaths[tab] = path
                    }
                }
                // Something the listener started opens "Jetzt läuft"; the
                // next title, a continued radio or play/pause do not.
                .onChange(of: model.player.userStarts) { tvNowPlaying = true }
                #elseif os(iOS)
                if model.offlineMode {
                    NavigationStack { OfflineLibraryView(model: model) }
                        // Inside the inset: the confirmation sits above the mini player.
                        .overlay(alignment: .bottom) { NoticeBanner(model: model, edge: .bottom) }
                        .safeAreaInset(edge: .bottom, spacing: 0) {
                            if model.player.current != nil {
                                MobileMiniPlayer(model: model) { expandedPlayer = true }
                                    .background(.regularMaterial)
                            }
                        }
                }
                else if sizeClass == .compact {
                    // No player button in the bars: the mini player opens it
                    // whenever something is loaded, and Start offers to resume.
                    // Four short tabs; the settings open from the gear on Start
                    // (the iPad sidebar keeps its own entry).
                    TabView(selection: mobileSelection) {
                        ForEach([Destination.home, .library, .playlists]) { item in
                            Tab(value: Optional(item)) { mobileTab(item) } label: { Label(item.rawValue, systemImage: item.icon) }
                        }
                        // The system's search tab: its field sits in the tab bar.
                        Tab("Suche", systemImage: Destination.search.icon, value: Optional(Destination.search), role: .search) {
                            mobileTab(.search)
                        }
                    }.tabBarMinimizeBehavior(.never)
                        .tabViewBottomAccessory(isEnabled: model.player.current != nil) {
                            MobileMiniPlayer(model: model) { expandedPlayer = true }
                        }
                        // Light confirmations: one per queue or playlist notice and
                        // per confirmed favorite (the open player taps for its own).
                        .sensoryFeedback(.success, trigger: model.notice?.id) { _, new in new != nil }
                        .sensoryFeedback(trigger: model.favoriteConfirmations) { _, _ in expandedPlayer ? nil : SensoryFeedback.impact(weight: .light) }
                } else {
                    // At the bottom of the detail, clear of its navigation bar
                    // and above the floating mini player.
                    splitView.overlay(alignment: .bottom) {
                        NoticeBanner(model: model, edge: .bottom)
                            .padding(.bottom, model.player.current != nil ? 84 : 8)
                    }
                }
                #else
                if model.offlineMode {
                    NavigationStack { OfflineLibraryView(model: model) }
                        .safeAreaInset(edge: .bottom, spacing: 0) { miniPlayer }
                } else { splitView }
                #endif
            }
        }
        .sheet(isPresented: $expandedPlayer, onDismiss: applyWidgetRoute) {
            NavigationStack {
                NowPlayingView(model: model, openCollection: playerLinks)
                    .toolbar { ToolbarItem(placement: .cancellationAction) { Button("Schließen", systemImage: "chevron.down") { expandedPlayer = false } } }
            }
            #if os(macOS)
            .frame(minWidth: 760, idealWidth: 900, minHeight: 540, idealHeight: 640)
            #endif
            #if os(iOS)
            // Drag down to close; the grabber says so.
            .presentationDragIndicator(.visible)
            // iPad: a tall page sheet, so the cover gets room; iPhone ignores it.
            .presentationSizing(.page)
            #endif
        }
        .alert("YTMDL", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
            Button("OK") { model.error = nil }
        } message: { Text(model.error ?? "") }
        #if os(tvOS)
        .onPlayPauseCommand { model.player.remoteToggle() }
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
        // The server may have been updated while the app was away: ask again
        // what it offers (statistics, lyrics search, stations, exclusions),
        // reload the daily mixes once they are old and send waiting plays.
        .onChange(of: scenePhase) { _, phase in if phase == .active { Task { await model.refreshAfterForeground() } } }
        .onChange(of: model.user?.id) { old, _ in
            detailPath = NavigationPath(); detailVisit = UUID()
            #if os(iOS)
            mobilePaths = [:]
            #endif
            #if os(tvOS)
            tvPaths = [:]
            if old != nil { tvNowPlaying = false }
            #endif
            if old != nil { expandedPlayer = false }
            applyWidgetRoute()
        }
        .onOpenURL { url in
            guard let route = MusicWidgetRoute(url: url) else { return }
            pendingWidgetRoute = route
            applyWidgetRoute()
        }
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
                #elseif os(tvOS)
                tvNowPlaying = true
                #else
                selectDestination(.player)
                #endif
            }
        }
        #endif
    }
    private func applyWidgetRoute() {
        guard model.user != nil, let route = pendingWidgetRoute else { return }
        if expandedPlayer && route != .player { expandedPlayer = false; return }
        pendingWidgetRoute = nil
        // Offline browsing stays local. Collection shortcuts return to the
        // saved-music overview instead of attempting authenticated API calls.
        if model.offlineMode && route != .player { return }
        switch route {
        case .player:
            #if os(tvOS)
            tvNowPlaying = true
            #else
            expandedPlayer = true
            #endif
        case .favorites:
            #if os(iOS)
            if sizeClass == .compact {
                mobilePaths[.library] = NavigationPath([CollectionKind.favorites])
                selectDestination(.library)
            } else { selectDestination(.favorites) }
            #elseif os(tvOS)
            // Favoriten live in Bibliothek on Apple TV.
            tvPaths[.library] = NavigationPath([CollectionKind.favorites])
            selectDestination(.library)
            #else
            selectDestination(.favorites)
            #endif
        case .playlists:
            #if os(iOS)
            mobilePaths[.playlists] = NavigationPath()
            #elseif os(tvOS)
            tvPaths[.playlists] = NavigationPath()
            #endif
            selectDestination(.playlists)
        }
    }
    /// The player's artist and album links. They close the player and open the
    /// page in the shown tab (iPhone) or the detail column (iPad).
    private var playerLinks: ((CollectionKind) -> Void)? {
        #if os(iOS)
        return { kind in
            expandedPlayer = false
            if sizeClass == .compact, let tab = destination {
                var path = mobilePaths[tab] ?? NavigationPath()
                path.append(kind)
                mobilePaths[tab] = path
            } else { detailPath.append(kind) }
        }
        #else
        return nil
        #endif
    }
    #if os(iOS)
    private func mobileTab(_ item: Destination) -> some View {
        NavigationStack(path: Binding(get: { mobilePaths[item] ?? NavigationPath() }, set: { mobilePaths[item] = $0 })) {
            routedContent(item, tabRoot: true)
        }
            // Every page of the tab, pushed ones included: nothing
            // readable shows between the mini player and the tab bar.
            .scrollEdgeEffectStyle(.hard, for: .bottom)
            // Confirmations sit at the bottom of the tab's safe area, right
            // above the mini player and the tab bar, on root and pushed pages
            // alike, so they never cover the back button or a toolbar.
            .overlay(alignment: .bottom) { NoticeBanner(model: model, edge: .bottom) }
    }
    /// Choosing the shown tab again returns to its first page, like the
    /// system apps; on Search it also puts the cursor in the field.
    private var mobileSelection: Binding<Destination?> {
        Binding(get: { destination }, set: { selected in
            if selected == destination, let item = selected {
                if mobilePaths[item]?.isEmpty == false { mobilePaths[item] = NavigationPath() }
                if item == .search { searchFocusRequest &+= 1 }
            }
            destination = selected
        })
    }
    #endif
    #if os(tvOS)
    /// Text for the four main tabs; the settings as a gear, like the system apps.
    @ViewBuilder private func tvTabLabel(_ item: Destination) -> some View {
        if item == .settings {
            Image(systemName: "gearshape.fill").accessibilityLabel("Einstellungen")
        } else {
            Text(item.rawValue)
        }
    }
    #endif
    /// "Stöbern" in Search: the library, filtered by the genre.
    private func openGenre(_ genre: String) {
        model.genre = genre
        Task { await model.filterGenre(genre) }
        #if os(iOS)
        mobilePaths[.library] = NavigationPath()
        #endif
        selectDestination(.library)
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
    /// `tabRoot`: the first page of an iPhone tab, which shows its own large title.
    private func routedContent(_ destination: Destination, tabRoot: Bool = false) -> some View {
        content(destination, tabRoot: tabRoot)
            .navigationDestination(for: CollectionKind.self) { kind in CollectionView(model: model, kind: kind) }
            .navigationDestination(for: Destination.self) { item in content(item) }
            .navigationDestination(for: PersonalMix.self) { mix in MixDetailView(model: model, mix: mix) }
            #if os(iOS)
            .navigationDestination(for: LibraryRoute.self) { route in
                switch route {
                case .tracks: LibraryTracksView(model: model)
                case .genres: LibraryGenresView(model: model)
                case .genre(let genre): GenreAlbumsView(model: model, genre: genre)
                }
            }
            .navigationDestination(for: ListeningRoute.self) { route in
                switch route {
                case .stats: StatsView(model: model)
                case .preferences: PreferencesView(model: model)
                }
            }
            #endif
            .navigationDestination(for: ForYouRoute.self) { _ in
                #if os(tvOS)
                TVForYouView(model: model) { tvNowPlaying = true }
                #elseif os(iOS)
                ForYouHubView(model: model)
                #else
                ForYouView(model: model)
                #endif
            }
    }
    @ViewBuilder private func content(_ destination: Destination, tabRoot: Bool = false) -> some View {
        switch destination {
        case .home:
            #if os(macOS)
            DesktopHomeView(model: model, navigate: selectDestination)
            #else
            #if os(iOS)
            MobileHomeView(model: model, tabRoot: tabRoot)
            #else
            TVHomeView(model: model) { tvNowPlaying = true }
            #endif
            #endif
        #if os(iOS)
        case .library: MobileLibraryView(model: model, tabRoot: tabRoot)
        case .artists: MobileArtistListView(model: model)
        #else
        case .library: LibraryView(model: model)
        case .artists: ArtistListView(model: model)
        #endif
        case .search:
            #if os(iOS)
            MobileSearchView(model: model, focusRequest: searchFocusRequest, openGenre: openGenre)
            #else
            SearchView(model: model)
            #endif
        case .favorites: CollectionView(model: model, kind: .favorites)
        case .playlists: PlaylistListView(model: model, tabRoot: tabRoot)
        case .player: NowPlayingView(model: model)
        case .downloads:
            #if !os(tvOS)
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
            #elseif os(iOS)
            // iPad: the iPhone's mini player on a floating bar of limited width.
            MobileMiniPlayer(model: model) { expandedPlayer = true }
                .padding(.vertical, 4)
                .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 20, style: .continuous))
                .frame(maxWidth: 640)
                .padding(.horizontal, 16).padding(.bottom, 8)
                .frame(maxWidth: .infinity)
                .id(track.id)
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
                    sidebarGroup("DEINE SAMMLUNG", items: [.library, .artists, .downloads])
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
                            do {
                                try model.connect(origin, localHTTP: localHTTP)
                                let restored = await model.restore()
                                connected = restored && address == origin && allowHTTP == localHTTP
                            }
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
                #if !os(tvOS)
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
                let origin = address
                do {
                    try model.connect(origin, localHTTP: false)
                    let restored = await model.restore()
                    connected = restored && address == origin && !allowHTTP
                }
                catch { model.report(error) }
            }
        }
    }
}

struct ArtworkView: View {
    var model: AppModel
    var kind: String
    var id: String
    var cornerRadius: CGFloat = 12
    /// A name for a generated monogram in place of the generic symbol (artists).
    var monogram: String? = nil
    var body: some View {
        Group {
            if kind == "tracks", model.player.current?.id == id, let image = model.player.artwork {
                Image(decorative: image, scale: 1).resizable().scaledToFill()
            } else if kind == "tracks", let url = model.offline.artworkURL(id) {
                OfflineArtworkImage(url: url) { online }
            } else { online }
        }.aspectRatio(1, contentMode: .fit).clipped().clipShape(RoundedRectangle(cornerRadius: cornerRadius)).accessibilityHidden(true)
    }
    @ViewBuilder private var online: some View {
        if !model.offlineMode, model.client != nil {
            CachedArtworkImage(model: model, kind: kind, id: id) { placeholder }
        } else { placeholder }
    }
    @ViewBuilder private var placeholder: some View {
        if let monogram { MonogramView(name: monogram) }
        else {
            RoundedRectangle(cornerRadius: 12).fill(.quaternary)
                .overlay { Image(systemName: kind == "artists" ? "person.crop.circle" : "music.note").font(.largeTitle).foregroundStyle(.secondary) }
        }
    }
}

// Offline cover art is read and decoded once, off the main thread, at row size.
// Doing this in `body` re-read and re-decoded a 1400 px image per row per render.
@MainActor private enum OfflineArtworkCache {
    final class Box { let image: CGImage; init(_ image: CGImage) { self.image = image } }
    static let images: NSCache<NSURL, Box> = { let cache = NSCache<NSURL, Box>(); cache.countLimit = 300; return cache }()
}
private struct OfflineArtworkImage: View {
    let url: URL
    let fallback: AnyView
    @State private var image: CGImage?
    @State private var failed = false
    init(url: URL, @ViewBuilder fallback: () -> some View) { self.url = url; self.fallback = AnyView(fallback()) }
    var body: some View {
        Group {
            if let image { Image(decorative: image, scale: 1).resizable().scaledToFill() }
            else if failed { fallback }
            else { Rectangle().fill(.quaternary) }
        }.task(id: url) {
            if let cached = OfflineArtworkCache.images.object(forKey: url as NSURL) { image = cached.image; failed = false; return }
            image = nil; failed = false
            let decoded = await Task.detached(priority: .utility) { () -> CGImage? in
                guard let data = try? Data(contentsOf: url) else { return nil }
                return ArtworkPalette.thumbnail(data, maxPixelSize: 512)
            }.value
            guard !Task.isCancelled else { return }
            if let decoded { OfflineArtworkCache.images.setObject(.init(decoded), forKey: url as NSURL); image = decoded } else { failed = true }
        }
    }
}

/// A server cover from the model's session cache (`ArtworkImageCache`):
/// fetched once, decoded off the main thread at the size it is shown. A
/// cover already decoded in this session shows in the first frame; one that
/// arrives fades in over the placeholder (instantly with Reduce Motion), and
/// the frame never changes size.
struct CachedArtworkImage<Placeholder: View>: View {
    var model: AppModel
    var kind: String
    var id: String
    @ViewBuilder var placeholder: Placeholder
    /// The cover this view fetched, with what it is the cover of: a reused
    /// row never shows the previous title's cover.
    @State private var loaded: (key: String, image: CGImage)?
    @State private var pixels: CGFloat = 0
    @Environment(\.displayScale) private var displayScale
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private var size: Int { ArtworkImageCache.bucket(forPixels: pixels) }
    private var identity: String { kind + "/" + id }
    private struct Key: Equatable { let kind: String; let id: String; let size: Int; let ready: Bool }
    var body: some View {
        let cached: CGImage? = pixels > 0 ? (model.artworkImages.cached(kind: kind, id: id, size: size) ?? nil) : nil
        let fetched: CGImage? = loaded?.key == identity ? loaded?.image : nil
        // The placeholder sets the frame; the cover only fills it.
        placeholder
            .overlay {
                if let image = cached ?? fetched {
                    Image(decorative: image, scale: 1).resizable().scaledToFill()
                        .transition(.opacity)
                }
            }
            .clipped()
            .onGeometryChange(for: CGFloat.self) { max($0.size.width, $0.size.height) } action: { pixels = $0 * displayScale }
            .task(id: Key(kind: kind, id: id, size: size, ready: pixels > 0)) {
                guard pixels > 0, let client = model.client else { return }
                if model.artworkImages.cached(kind: kind, id: id, size: size) != nil { return }
                let wanted = identity
                let image = await model.artworkImages.image(kind: kind, id: id, size: size, client: client)
                guard !Task.isCancelled, let image, wanted == identity else { return }
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.2)) { loaded = (wanted, image) }
            }
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
                #elseif os(iOS)
                MobileLibraryShortcuts()
                VStack(alignment: .leading, spacing: 4) {
                    Text("Deine Alben").font(.title2.bold())
                    Text("Musik aus deiner Bibliothek").font(.subheadline).foregroundStyle(.secondary)
                }
                if !model.genres.isEmpty {
                    HStack {
                        Label("Genre", systemImage: "line.3.horizontal.decrease").font(.subheadline.weight(.medium))
                        Spacer()
                        genrePicker.labelsHidden().tint(.primary)
                    }.padding(.horizontal, 16).frame(minHeight: 52)
                        .background(Color(uiColor: .secondarySystemGroupedBackground), in: RoundedRectangle(cornerRadius: 16))
                }
                #else
                Text("Deine Musik").font(.title2.bold())
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 8) { collectionShortcuts }.fixedSize(horizontal: true, vertical: false)
                    VStack(alignment: .leading, spacing: 8) { collectionShortcuts }
                }.buttonStyle(.bordered)
                genrePicker
                #endif
                // A failed load is never shown as an empty library: "Noch keine
                // Alben" needs an answer from the server with an empty list.
                if let message = model.libraryError, !model.connecting {
                    if model.releases.isEmpty {
                        ContentUnavailableView {
                            Label("Alben konnten nicht geladen werden", systemImage: "exclamationmark.triangle")
                        } description: { Text(message) } actions: {
                            Button("Erneut laden", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }
                                .buttonStyle(.borderedProminent).accessibilityIdentifier("library-retry")
                        }.accessibilityIdentifier("library-error")
                    } else {
                        ListenSectionFailure(message: message) { Task { await model.loadLibrary() } }
                    }
                } else if model.releases.isEmpty && (model.connecting || !model.releasesLoaded) { ProgressView("Bibliothek wird geladen …") }
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
        // Nothing has answered yet and nothing is running: load instead of
        // waiting for a load that was never started.
        .task { if !model.releasesLoaded && model.libraryError == nil && !model.connecting { await model.loadLibrary() } }
        #if os(iOS)
        .background(Color(uiColor: .systemGroupedBackground))
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .refreshable { await model.loadLibrary() }
        #else
        .toolbar { ToolbarItem { Button { Task { await model.loadLibrary() } } label: { Image(systemName: "arrow.clockwise") }.accessibilityLabel("Bibliothek aktualisieren").disabled(model.connecting) } }
        #endif
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
    @State private var topTracks: [HistoryTrack] = []
    #if os(iOS)
    /// The header's large title is on screen; the bar shows the title otherwise.
    @State private var headerTitleVisible = true
    /// The album with its type and linked artist; nil hides the artist link.
    @State private var releaseDetail: ReleaseDetailResponse?
    /// "Mehr von …" on an album, the albums of an artist.
    @State private var related: [Release] = []
    @State private var artistAlbumSort = ArtistAlbumSort.newest
    @State private var popularExpanded = false
    @State private var addingAllToPlaylist = false
    @State private var favoritingAll = false
    /// An album or artist opened from this page (shelf card, artist link).
    @State private var pushed: CollectionKind?
    #endif
    @Environment(\.dismiss) private var dismiss
    /// What a queue started here is shown as: "Weiter aus: …".
    private var queueContext: QueueContext {
        switch kind {
        case .favorites: QueueContext(kind: "favorites", title: "Lieblingstitel")
        case .release(let release): QueueContext(kind: "album", id: release.id, title: release.title)
        case .artist(let artist): QueueContext(kind: "artist", id: artist.id, title: artist.name)
        case .playlist(let value): QueueContext(kind: "playlist", id: value.id, title: playlist?.name ?? value.name)
        }
    }
    @ViewBuilder private var queueMenuItems: some View {
        Button("Als Nächstes spielen", systemImage: "text.insert") { model.enqueue(tracks, next: true) }
            .disabled(tracks.isEmpty || model.player.queue.tracks.count >= PlaybackQueue.limit)
        Button("Zur Warteschlange", systemImage: "text.badge.plus") { model.enqueue(tracks, next: false) }
            .disabled(tracks.isEmpty || model.player.queue.tracks.count >= PlaybackQueue.limit)
    }
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
            #elseif os(iOS)
            mobileCollection
            #else
            standardCollection
            #endif
        }.navigationTitle(collectionTitle).task {
            if tracks.isEmpty { await load() }
            if case .artist(let artist) = kind, topTracks.isEmpty, !model.offlineMode {
                // Optional like on the web: a failure or an older server just hides the section.
                topTracks = (try? await model.artistTopTracks(artist.id)) ?? []
            }
        }
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        .task { await loadRelated() }
        #endif
        .toolbar {
            #if os(iOS)
            // The large title of the header moves into the bar once it scrolled away.
            ToolbarItem(placement: .principal) {
                Text(collectionTitle).font(.headline).lineLimit(1)
                    .opacity(headerTitleVisible ? 0 : 1)
                    .animation(.easeOut(duration: 0.2), value: headerTitleVisible)
                    .accessibilityHidden(headerTitleVisible)
            }
            if playlist == nil {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { pageMenuItems } label: {
                        Image(systemName: "ellipsis").font(.system(size: 20, weight: .semibold))
                            .frame(minWidth: 44, minHeight: 44).contentShape(Rectangle())
                    }.accessibilityLabel("Weitere Aktionen").accessibilityIdentifier("collection-page-more")
                }
            }
            #endif
            #if os(macOS)
            if playlist == nil { ToolbarItem {
                Button("Offline speichern", systemImage: "arrow.down.circle") { Task { await model.downloadCollection(kind) } }
                    .disabled(busy || model.offlineMode)
            } }
            #endif
            #if !os(iOS)
            if playlist == nil {
                ToolbarItem {
                    Menu { queueMenuItems } label: {
                        Image(systemName: "text.badge.plus").frame(minWidth: 44, minHeight: 44).contentShape(Rectangle())
                    }.accessibilityLabel("Zur Warteschlange").accessibilityIdentifier("collection-queue-options")
                }
            }
            #endif
            if let playlist {
                ToolbarItem {
                    Menu {
                        // iPhone and iPad: queue and offline are in the header's action row.
                        #if !os(iOS)
                        queueMenuItems
                        Divider()
                        #endif
                        #if os(macOS)
                        Button("Offline speichern", systemImage: "arrow.down.circle") { Task { await model.downloadCollection(kind) } }
                            .disabled(model.offlineMode)
                        Divider()
                        #endif
                        Button("Bearbeiten", systemImage: "pencil") { editor = true }
                        if playlist.smartRules == nil {
                            Button("Titel hinzufügen", systemImage: "plus") { adding = true }
                        }
                        Button("Neu laden", systemImage: "arrow.clockwise") { Task { await reloadPlaylist() } }
                        #if os(iOS)
                        PinToStartButton(model: model, pin: .playlist(playlist))
                        #endif
                        Divider()
                        Button("Playlist löschen", systemImage: "trash", role: .destructive) { deleting = true }
                    } label: {
                        Image(systemName: "ellipsis").font(.system(size: 20, weight: .semibold))
                            .frame(minWidth: 44, minHeight: 44).contentShape(Rectangle())
                    }.accessibilityLabel("Playlist").disabled(busy || model.playlistBusy).accessibilityIdentifier("playlist-collection-options")
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
            guard !busy else { return }
            if playlist != nil { await reloadPlaylist() }
            else { tracks = []; offset = 0; more = true; await load() }
        }
    }
    #if os(iOS)
    // MARK: iPhone and iPad collection pages

    private var totalDuration: Int { tracks.reduce(0) { $0 + $1.durationMs } }
    private var savedOffline: Bool {
        let type: String, source: String
        switch kind {
        case .favorites: type = "favorites"; source = ""
        case .playlist(let value): type = "playlist"; source = value.id
        case .artist(let value): type = "artist"; source = value.id
        case .release(let value): type = "release"; source = value.id
        }
        return model.offline.currentCollections.contains { $0.kind == type && $0.sourceID == source }
    }
    private var allFavorite: Bool { !tracks.isEmpty && tracks.allSatisfy { model.favoriteIDs.contains($0.id) } }
    private var offlineAction: CollectionAction {
        CollectionAction(id: "collection-offline", title: savedOffline ? "Gespeichert" : "Offline", label: "Offline speichern",
            symbol: savedOffline ? "checkmark.circle.fill" : "arrow.down.circle",
            hint: savedOffline ? "Ist auf diesem Gerät gespeichert; lädt neue Titel nach" : "Speichert die Titel auf diesem Gerät",
            enabled: !busy && !model.offlineMode, selected: savedOffline,
            kind: .button({ Task { await model.downloadCollection(kind) } }))
    }
    private var favoriteAction: CollectionAction {
        CollectionAction(id: "collection-favorite", title: "Favorit", label: allFavorite ? "Aus Favoriten entfernen" : "Alle zu Favoriten hinzufügen",
            symbol: allFavorite ? "heart.fill" : "heart", enabled: !tracks.isEmpty && !model.offlineMode && !favoritingAll, selected: allFavorite,
            kind: .button({ Task { await favoriteAll() } }))
    }
    private var playlistAction: CollectionAction {
        CollectionAction(id: "collection-add-to-playlist", title: "Playlist", label: "Zur Playlist hinzufügen", symbol: "music.note.list",
            enabled: !tracks.isEmpty && !model.offlineMode, kind: .button({ addingAllToPlaylist = true }))
    }
    /// "Künstler-Radio": seeded with the listener's most played title of the artist.
    private func artistRadioAction(_ artist: Artist) -> CollectionAction {
        CollectionAction(id: "collection-radio", title: "Radio", label: "Künstler-Radio starten", symbol: "dot.radiowaves.left.and.right",
            hint: "Spielt Titel dieses Künstlers und ähnliche aus deiner Mediathek", enabled: !model.offlineMode && !model.listeningBusy,
            kind: .button({ Task { await model.startArtistRadio(artist, preferred: topTracks.map(\.track), tracks: tracks) } }))
    }
    /// The page's "…" menu: radio and "An Start anheften" (playlists have their own menu).
    @ViewBuilder private var pageMenuItems: some View {
        switch kind {
        case .release(let release):
            Button("Album-Radio starten", systemImage: "dot.radiowaves.left.and.right") {
                Task { await model.startAlbumRadio(releaseDetail?.release ?? release, tracks: tracks) }
            }.disabled(model.offlineMode || model.listeningBusy)
            PinToStartButton(model: model, pin: .album(releaseDetail?.release ?? release))
        case .artist(let artist):
            Button("Künstler-Radio starten", systemImage: "dot.radiowaves.left.and.right") {
                Task { await model.startArtistRadio(artist, preferred: topTracks.map(\.track), tracks: tracks) }
            }.disabled(model.offlineMode || model.listeningBusy)
            PinToStartButton(model: model, pin: .artist(artist))
            ExclusionToggleButton(model: model, kind: .artist, target: artist.id, name: artist.name)
        case .favorites: PinToStartButton(model: model, pin: .favorites)
        case .playlist(let value): PinToStartButton(model: model, pin: .playlist(playlist ?? value))
        }
    }
    private func releaseCaption(_ release: Release) -> String {
        release.year > 0 ? "\(release.year) · \(release.kindLabel)" : release.kindLabel
    }
    private var trackSummary: String {
        CollectionSummary.line(trackCount: tracks.count, durationMs: totalDuration, partial: more && !tracks.isEmpty && playlist == nil)
    }
    /// "2 Alben · 14 Titel"; the albums count without singles and EPs.
    private func artistSummary(_ artist: Artist) -> String {
        var parts: [String] = []
        let albums = related.filter { !$0.isSingleOrEP }.count
        if albums > 0 { parts.append(albums == 1 ? "1 Album" : "\(albums) Alben") }
        let titles = artist.trackCount ?? tracks.count
        if titles > 0 { parts.append(CollectionSummary.tracks(titles)) }
        return parts.isEmpty ? " " : parts.joined(separator: " · ")
    }

    @ViewBuilder private var mobileHeader: some View {
        switch kind {
        case .release(let release):
            let shown = releaseDetail?.release ?? release
            let artist = releaseDetail?.artist
            let count = tracks.isEmpty ? (shown.trackCountInLibrary ?? 0) : tracks.count
            CollectionHeader(title: shown.title, link: shown.artists.joined(separator: " · "),
                             openLink: artist.map { value in { pushed = .artist(value) } },
                             detail: shown.kindLabel + " · " + CollectionSummary.line(year: shown.year, trackCount: count, durationMs: totalDuration, partial: more && !tracks.isEmpty),
                             canPlay: !tracks.isEmpty,
                             actions: [favoriteAction, offlineAction, queueCollectionAction(model, tracks: tracks, id: "collection-queue"), playlistAction],
                             play: { play(tracks) }, shuffle: { play(tracks.shuffled()) },
                             titleVisibility: { headerTitleVisible = $0 }) { _ in
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: 0)
            }
        case .artist(let artist):
            CollectionHeader(title: artist.name, detail: artistSummary(artist),
                             canPlay: !tracks.isEmpty, round: true,
                             actions: [offlineAction, queueCollectionAction(model, tracks: tracks, id: "collection-queue"), artistRadioAction(artist)],
                             play: { play(tracks) }, shuffle: { play(tracks.shuffled()) },
                             titleVisibility: { headerTitleVisible = $0 }) { _ in
                ArtworkView(model: model, kind: "artists", id: artist.id, cornerRadius: 0, monogram: artist.name)
            }
        case .playlist:
            let smart = playlist?.smartRules != nil
            CollectionHeader(eyebrow: smart ? "Intelligente Playlist" : "Playlist", title: collectionTitle, detail: trackSummary,
                             description: playlist?.description, canPlay: !tracks.isEmpty,
                             actions: [offlineAction, queueCollectionAction(model, tracks: tracks, id: "collection-queue"),
                                       CollectionAction(id: "collection-edit", title: "Bearbeiten", label: smart ? "Regeln bearbeiten" : "Playlist bearbeiten",
                                                        symbol: smart ? "slider.horizontal.3" : "pencil",
                                                        enabled: !busy && !model.playlistBusy && !model.offlineMode, kind: .button({ editor = true }))],
                             play: { play(tracks) }, shuffle: { play(tracks.shuffled()) },
                             titleVisibility: { headerTitleVisible = $0 }) { size in
                MobileCollectionArtwork(model: model, tracks: tracks, size: size)
            }
        case .favorites:
            CollectionHeader(title: collectionTitle, detail: trackSummary, canPlay: !tracks.isEmpty,
                             actions: [offlineAction, queueCollectionAction(model, tracks: tracks, id: "collection-queue")],
                             play: { play(tracks) }, shuffle: { play(tracks.shuffled()) },
                             titleVisibility: { headerTitleVisible = $0 }) { size in
                Image(systemName: "heart.fill").font(.system(size: size * 0.32, weight: .semibold)).foregroundStyle(.white)
                    .frame(width: size, height: size).background(Color.accentColor.gradient)
            }
        }
    }

    private var mobileCollection: some View {
        List {
            Section {
                mobileHeader
                    .listRowInsets(EdgeInsets(top: 8, leading: ListenMetrics.pageMargin, bottom: 24, trailing: ListenMetrics.pageMargin))
                    .listRowSeparator(.hidden).listRowBackground(Color.clear)
            }
            switch kind {
            case .release(let release): albumSections(release)
            case .artist(let artist): artistSections(artist)
            default:
                Section {
                    ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in
                        TrackRow(model: model, track: track, playlistEdit: playlistEdit(track, index: index)) { play(tracks, index: index) }
                            .listRowInsets(CollectionMetrics.rowInsets)
                            .onAppear { loadMoreIfNeeded(index) }
                    }
                    loadState(numbered: false)
                }
            }
        }
        .listStyle(.plain)
        .modifier(ReadableContentWidth())
        .navigationDestination(item: $pushed) { CollectionView(model: model, kind: $0) }
        .sheet(isPresented: $addingAllToPlaylist) { AddToPlaylistSheet(model: model, tracks: tracks) }
    }

    /// Skeleton rows on the first load, the error with "Titel erneut laden",
    /// the empty state and the button for more.
    @ViewBuilder private func loadState(numbered: Bool) -> some View {
        if busy && tracks.isEmpty {
            ForEach(0..<6, id: \.self) { _ in
                TrackRowSkeleton(numbered: numbered).listRowInsets(CollectionMetrics.rowInsets).listRowSeparator(.hidden)
            }.accessibilityElement(children: .ignore).accessibilityLabel("Titel werden geladen")
        } else if busy {
            ProgressView("Titel werden geladen …").frame(maxWidth: .infinity).listRowSeparator(.hidden)
        } else if let loadFailure {
            VStack(alignment: .leading, spacing: 12) {
                Label(loadFailure, systemImage: "exclamationmark.triangle").font(.subheadline).foregroundStyle(.secondary)
                Button("Erneut laden", systemImage: "arrow.clockwise") { Task { await load() } }
                    .buttonStyle(.bordered)
            }.padding(.vertical, 8).listRowInsets(CollectionMetrics.rowInsets).listRowSeparator(.hidden)
        } else if tracks.isEmpty {
            ContentUnavailableView(isFavorites ? "Noch keine Lieblingstitel" : "Noch keine Titel",
                                   systemImage: isFavorites ? "heart" : "music.note",
                                   description: Text(isFavorites ? "Markiere Titel mit dem Herz. Sie erscheinen dann hier."
                                                     : playlist?.smartRules != nil ? "Keine Titel passen zu den Regeln dieser Playlist."
                                                     : playlist != nil ? "Füge über „…“ Titel hinzu." : "Auf dem Server sind hierzu keine Titel gespeichert."))
                .listRowSeparator(.hidden)
        }
        if more && !tracks.isEmpty && !busy {
            Button("Weitere Titel laden") { Task { await load() } }.listRowSeparator(.hidden)
        }
    }
    private func loadMoreIfNeeded(_ index: Int) {
        guard index >= tracks.count - 1, more, !busy, loadFailure == nil, playlist == nil else { return }
        Task { await load() }
    }

    @ViewBuilder private func albumSections(_ release: Release) -> some View {
        let groups = DiscGroup.groups(tracks)
        let albumArtists = Set(release.artists)
        ForEach(groups) { group in
            Section {
                if groups.count > 1 {
                    Text("CD \(group.disc)").font(.subheadline.weight(.semibold)).foregroundStyle(.secondary)
                        .accessibilityAddTraits(.isHeader)
                        .listRowInsets(EdgeInsets(top: 12, leading: ListenMetrics.pageMargin, bottom: 2, trailing: ListenMetrics.pageMargin))
                        .listRowSeparator(.hidden).listRowBackground(Color.clear)
                }
                ForEach(group.positions, id: \.self) { position in
                    let track = tracks[position]
                    TrackRow(model: model, track: track, number: track.trackNumber ?? position + 1,
                             showsArtist: albumArtists.isEmpty || !Set(track.artists).isSubset(of: albumArtists)) { play(tracks, index: position) }
                        .listRowInsets(CollectionMetrics.rowInsets)
                        .onAppear { loadMoreIfNeeded(position) }
                }
            }
        }
        Section { loadState(numbered: true) }
        if !related.isEmpty, let artist = releaseDetail?.artist {
            Section {
                CollectionSectionTitle(title: "Mehr von \(artist.name)").sectionTitleRow()
                shelf(related)
            }
        }
    }

    private func shelf(_ releases: [Release]) -> some View {
        ListenShelf {
            ForEach(releases) { release in
                ShelfReleaseCard(model: model, release: release, caption: releaseCaption(release)) { pushed = .release(release) }
            }
        }
        .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 20, trailing: 0))
        .listRowSeparator(.hidden).listRowBackground(Color.clear)
    }

    /// From the play history when there is one, else the first titles.
    private var popularTracks: [(track: Track, plays: Int?)] {
        if !topTracks.isEmpty { return topTracks.map { ($0.track, Optional($0.playCount)) } }
        return tracks.prefix(10).map { ($0, nil) }
    }

    @ViewBuilder private func artistSections(_ artist: Artist) -> some View {
        let popular = popularTracks
        if !popular.isEmpty {
            Section {
                CollectionSectionTitle(title: "Beliebte Titel").sectionTitleRow()
                ForEach(Array(popular.prefix(popularExpanded ? 10 : 5).enumerated()), id: \.offset) { index, entry in
                    TrackRow(model: model, track: entry.track, detail: entry.plays.map { "\($0)× gehört" }, number: index + 1, showsArtist: false) {
                        if topTracks.isEmpty { play(tracks, index: index) }
                        else if let client = model.client {
                            model.player.play(popular.map { $0.track }, start: index, client: client,
                                              context: QueueContext(kind: "history", id: "artist-top:" + artist.id, title: "Beliebte Titel · \(artist.name)"))
                        }
                    }.listRowInsets(CollectionMetrics.rowInsets)
                }
                if popular.count > 5 {
                    Button(popularExpanded ? "Weniger anzeigen" : "Mehr anzeigen") { popularExpanded.toggle() }
                        .font(.subheadline.weight(.semibold)).listRowInsets(CollectionMetrics.rowInsets).listRowSeparator(.hidden)
                        .accessibilityIdentifier("artist-popular-more")
                }
            }
        }
        let albums = artistAlbumSort.apply(related.filter { !$0.isSingleOrEP })
        if !albums.isEmpty {
            Section {
                CollectionSectionTitle(title: "Alben") {
                    Menu {
                        Picker("Alben sortieren", selection: $artistAlbumSort) {
                            ForEach(ArtistAlbumSort.allCases) { Text($0.label).tag($0) }
                        }
                    } label: {
                        HStack(spacing: 4) { Text(artistAlbumSort.label); Image(systemName: "chevron.up.chevron.down").imageScale(.small) }
                            .font(.subheadline).frame(minHeight: 44).contentShape(Rectangle())
                    }
                    // Borderless: only the control opens the menu, not the whole title row.
                    .menuStyle(.button).buttonStyle(.borderless)
                    .accessibilityLabel("Alben sortieren").accessibilityValue(artistAlbumSort.label)
                    .accessibilityIdentifier("artist-album-sort")
                }.sectionTitleRow()
                shelf(albums)
            }
        }
        let singles = artistAlbumSort.apply(related.filter(\.isSingleOrEP))
        if !singles.isEmpty {
            Section {
                CollectionSectionTitle(title: "Singles & EPs").sectionTitleRow()
                shelf(singles)
            }
        }
        Section {
            CollectionSectionTitle(title: "Alle Titel").sectionTitleRow()
            ForEach(Array(tracks.enumerated()), id: \.offset) { index, track in
                TrackRow(model: model, track: track, detail: track.album.isEmpty ? nil : track.album, showsArtist: false) { play(tracks, index: index) }
                    .listRowInsets(CollectionMetrics.rowInsets)
                    .onAppear { loadMoreIfNeeded(index) }
            }
            loadState(numbered: false)
        }
    }

    /// Adds the missing titles to the favorites; when all are favorites, removes them.
    private func favoriteAll() async {
        favoritingAll = true; defer { favoritingAll = false }
        let removing = allFavorite
        for track in tracks where model.favoriteIDs.contains(track.id) == removing {
            await model.toggleFavorite(track)
        }
    }

    /// The optional parts of album and artist pages; a failure or an older
    /// server only leaves them out.
    private func loadRelated() async {
        guard related.isEmpty, releaseDetail == nil, !model.offlineMode, let client = model.client else { return }
        do {
            switch kind {
            case .release(let release):
                let detail: ReleaseDetailResponse = try await client.get(try client.server.itemPath("releases", id: release.id))
                guard model.client === client else { return }
                releaseDetail = detail
                guard let artist = detail.artist else { return }
                let others: [Release] = try await client.get("/library/releases", query: [
                    .init(name: "artist_id", value: artist.id), .init(name: "in_library", value: "true"),
                    .init(name: "sort", value: "year"), .init(name: "order", value: "desc"), .init(name: "limit", value: "24")])
                guard model.client === client else { return }
                related = others.filter { $0.id != release.id }
            case .artist(let artist):
                let releases: [Release] = try await client.get("/library/releases", query: [
                    .init(name: "artist_id", value: artist.id), .init(name: "in_library", value: "true"),
                    .init(name: "sort", value: "year"), .init(name: "order", value: "desc"), .init(name: "limit", value: "120")])
                guard model.client === client else { return }
                related = releases
            default: return
            }
        } catch {}
    }
    #endif

    private var standardCollection: some View {
        List {
            Section {
                #if os(iOS)
                MobileCollectionHeader(model: model, tracks: tracks, title: collectionTitle,
                    detail: "\(tracks.count)\(more ? " geladene" : "") Titel · \(mobileCollectionDuration(Int(tracks.reduce(0) { $0 + $1.duration }) * 1000))",
                    description: playlist?.description, play: { play(tracks) }, shuffle: { play(tracks.shuffled()) })
                    .listRowBackground(Color.clear).listRowSeparator(.hidden)
                    .listRowInsets(EdgeInsets(top: 8, leading: 0, bottom: 16, trailing: 0))
                #else
                HStack {
                    Button("Abspielen", systemImage: "play.fill") { play(tracks) }.buttonStyle(.borderedProminent).disabled(tracks.isEmpty)
                    Button("Zufall", systemImage: "shuffle") { play(tracks.shuffled()) }.buttonStyle(.bordered).disabled(tracks.isEmpty)
                }.padding(.vertical, 12)
                #endif
            }
            if case .artist(let artist) = kind {
                ArtistTopTracksSection(model: model, artist: artist, tracks: topTracks)
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
    private func play(_ tracks: [Track], index: Int = 0) { if let client = model.client { model.player.play(tracks, start: index, client: client, context: queueContext) } }
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
    /// Optional text after the artist (for example when it was played); the
    /// row then leaves out the duration so the title keeps its room.
    var detail: String? = nil
    /// iPhone and iPad: a track or chart number in place of the cover (album
    /// pages, "Beliebte Titel").
    var number: Int? = nil
    /// iPhone and iPad: the artist line; album pages leave it out when the
    /// title is by the album's artist.
    var showsArtist = true
    var action: () -> Void
    @State private var addingToPlaylist = false
    var body: some View {
        Group {
            #if os(macOS)
            desktopRow
            #elseif os(iOS)
            mobileRow
            #else
            standardRow
            #endif
        }
        .sheet(isPresented: $addingToPlaylist) { AddToPlaylistSheet(model: model, tracks: [track]) }
    }
    #if os(iOS)
    @Environment(\.dynamicTypeSize) private var textSize
    private var isCurrent: Bool { model.player.current?.id == track.id }
    private var queueFull: Bool { model.player.queue.tracks.count >= PlaybackQueue.limit }
    // Through the model, so the iPhone confirms them briefly.
    private func playNext() { model.enqueue([track], next: true) }
    private func enqueue() { model.enqueue([track], next: false) }
    /// The same actions in the "…" menu and the context menu, with the labels
    /// of the queue: "Als Nächstes spielen", "Zur Warteschlange".
    @ViewBuilder private var mobileMenuItems: some View {
        Button("Als Nächstes spielen", systemImage: "text.insert", action: playNext).disabled(queueFull)
        Button("Zur Warteschlange", systemImage: "text.badge.plus", action: enqueue).disabled(queueFull)
        Button("Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
            .disabled(model.offlineMode || model.listeningBusy)
        Divider()
        let favorite = model.favoriteIDs.contains(track.id)
        Button(favorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen", systemImage: favorite ? "heart.slash" : "heart") { Task { await model.toggleFavorite(track) } }
            .disabled(model.offlineMode || model.pendingFavorites.contains(track.id))
        Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { addingToPlaylist = true }.disabled(model.offlineMode)
        Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }.disabled(model.offlineMode)
        TrackSteeringItems(model: model, track: track)
        playlistMenuItems
    }
    @ViewBuilder private var leading: some View {
        if let number {
            Group {
                if isCurrent { PlayingIndicator(playing: model.player.isPlaying, color: .accentColor) }
                else { Text("\(number)").font(.subheadline).monospacedDigit().foregroundStyle(.secondary).lineLimit(1).minimumScaleFactor(0.7) }
            }.frame(width: CollectionMetrics.numberColumn)
        } else {
            ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 6)
                .frame(width: CollectionMetrics.rowArtwork, height: CollectionMetrics.rowArtwork)
                .overlay {
                    if isCurrent {
                        ZStack {
                            RoundedRectangle(cornerRadius: 6, style: .continuous).fill(.black.opacity(0.4))
                            PlayingIndicator(playing: model.player.isPlaying)
                        }
                    }
                }
        }
    }
    private var mobileRow: some View {
        HStack(spacing: 12) {
            Button(action: action) {
                HStack(spacing: 12) {
                    leading
                    VStack(alignment: .leading, spacing: 2) {
                        Text(track.title).font(.body.weight(isCurrent ? .semibold : .regular))
                            .foregroundStyle(isCurrent ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary))
                            .lineLimit(textSize.isAccessibilitySize ? 3 : 1)
                        if showsArtist || detail != nil {
                            HStack(spacing: 4) {
                                if showsArtist {
                                    Text(track.artistText).lineLimit(1)
                                    // A short detail ("7× gehört") keeps its width beside the artist.
                                    if let detail {
                                        Text("·").accessibilityHidden(true)
                                        Text(detail).monospacedDigit().lineLimit(1).fixedSize()
                                    }
                                } else if let detail { Text(detail).lineLimit(1) }
                            }.font(.subheadline).foregroundStyle(.secondary)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    if detail == nil && !textSize.isAccessibilitySize {
                        Text(formatTime(track.duration)).font(.subheadline).monospacedDigit().foregroundStyle(.secondary)
                    }
                }.frame(minHeight: number == nil ? CollectionMetrics.rowArtwork : 44).contentShape(Rectangle())
            }.buttonStyle(.plain)
                .accessibilityLabel(rowLabel)
                .accessibilityValue(isCurrent ? "Läuft gerade" : "")
                .accessibilityHint("Spielt den Titel")
            Menu { mobileMenuItems } label: {
                Image(systemName: "ellipsis").foregroundStyle(.secondary).frame(width: 44, height: 44).contentShape(Rectangle())
            }.accessibilityLabel("Aktionen für \(track.title)")
        }
        .contextMenu { mobileMenuItems }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            Button(action: playNext) { Label("Als Nächstes", systemImage: "text.insert") }.tint(.indigo).disabled(queueFull)
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: playlistEdit == nil) {
            if let edit = playlistEdit {
                Button(role: .destructive, action: edit.remove) { Label("Entfernen", systemImage: "minus.circle") }
            }
            Button(action: enqueue) { Label("Warteschlange", systemImage: "text.badge.plus") }.tint(.orange).disabled(queueFull)
        }
    }
    #endif
    @ViewBuilder private var playlistMenuItems: some View {
        if let edit = playlistEdit {
            Divider()
            Button("In Playlist nach oben", systemImage: "arrow.up", action: edit.moveUp).disabled(!edit.canMoveUp)
            Button("In Playlist nach unten", systemImage: "arrow.down", action: edit.moveDown).disabled(!edit.canMoveDown)
            Button("Aus Playlist entfernen", systemImage: "minus.circle", role: .destructive, action: edit.remove)
            Divider()
        }
    }
    private var rowLabel: String {
        if let detail { return "\(track.title) abspielen, \(track.artistText), \(detail)" }
        return "\(track.title) abspielen, \(track.artistText)"
    }
    private var standardRow: some View {
        HStack(spacing: 14) {
            Button(action: action) {
                HStack(spacing: 14) {
                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 52, height: 52)
                        #if os(tvOS)
                        // The playing title, like on iPhone: moving bars on its cover.
                        .overlay {
                            if model.player.current?.id == track.id {
                                ZStack {
                                    RoundedRectangle(cornerRadius: 12).fill(.black.opacity(0.45))
                                    PlayingIndicator(playing: model.player.isPlaying).scaleEffect(1.5)
                                }
                            }
                        }
                        #endif
                    VStack(alignment: .leading, spacing: 4) {
                        Text(track.title).desktopScaledFont(18, weight: .semibold, fallback: .headline).lineLimit(1)
                        if let detail {
                            // The detail (when it was played) shares the second
                            // line with the artist, so the title keeps the width.
                            HStack(spacing: 4) {
                                Text(track.artistText).lineLimit(1)
                                Text("·").accessibilityHidden(true)
                                Text(detail).lineLimit(1).fixedSize()
                            }.desktopScaledFont(16, fallback: .subheadline).foregroundStyle(.secondary)
                        } else {
                            Text(track.artistText).desktopScaledFont(16, fallback: .subheadline).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }.frame(maxWidth: .infinity, alignment: .leading)
                    if detail == nil {
                        Text(formatTime(track.duration)).desktopScaledFont(15, fallback: .caption).monospacedDigit().foregroundStyle(.secondary)
                    }
                }.contentShape(Rectangle())
            }.buttonStyle(.plain).accessibilityLabel(rowLabel)
            Menu {
                Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { addingToPlaylist = true }.disabled(model.offlineMode)
                playlistMenuItems
                Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten", systemImage: "heart") { Task { await model.toggleFavorite(track) } }
                    .disabled(model.offlineMode || model.pendingFavorites.contains(track.id))
                #if os(iOS)
                Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }.disabled(model.offlineMode)
                Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }.disabled(model.offlineMode || model.listeningBusy)
                #endif
                Button("Als Nächstes abspielen", systemImage: "text.insert") { if let client = model.client { model.player.insertNext(track, client: client) } }.disabled(model.player.queue.tracks.count >= 500)
                Button("Zur Warteschlange", systemImage: "text.badge.plus") { if let client = model.client { model.player.append(track, client: client) } }.disabled(model.player.queue.tracks.count >= 500)
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
                Button("Als Nächstes abspielen", systemImage: "text.insert") { if let client = model.client { model.player.insertNext(track, client: client) } }.disabled(model.player.queue.tracks.count >= 500)
                Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { addingToPlaylist = true }.disabled(model.offlineMode)
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
    /// iPhone: the first page of the Playlists tab, with its own large title.
    var tabRoot = false
    @State private var creating = false
    @State private var playlistQuery = ""
    @State private var alphabetical = false
    private var visiblePlaylists: [Playlist] {
        let query = playlistQuery.trimmingCharacters(in: .whitespacesAndNewlines)
        let result = model.playlists.filter { query.isEmpty || $0.name.localizedCaseInsensitiveContains(query) }
        return alphabetical ? result.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending } : result
    }
    var body: some View {
        Group {
            #if os(macOS)
            desktopPlaylists
            #elseif os(iOS)
            mobilePlaylists
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
            #if !os(iOS)
            .toolbar {
                ToolbarItem { Button("Neue Playlist", systemImage: "plus") { creating = true }.disabled(model.playlistBusy) }
                ToolbarItem { Button("Aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }.disabled(model.connecting || model.playlistBusy) }
            }
            #endif
            .sheet(isPresented: $creating) { PlaylistEditor(model: model) }
    }
    #if os(iOS)
    /// The playlists as one calm list: filter and order on top, "Neue
    /// Playlist" as the first row, then each playlist with its cover mosaic.
    private var mobilePlaylists: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ListenMetrics.headerGap) {
                if tabRoot {
                    MobilePageHeader(title: "Playlists").padding(.horizontal, -ListenMetrics.pageMargin).padding(.top, -8)
                }
                HStack(spacing: 8) {
                    HStack(spacing: 10) {
                        Image(systemName: "line.3.horizontal.decrease").foregroundStyle(.secondary).accessibilityHidden(true)
                        TextField("Playlists filtern", text: $playlistQuery).textInputAutocapitalization(.never).autocorrectionDisabled()
                            .accessibilityIdentifier("mobile-playlist-filter")
                        if !playlistQuery.isEmpty {
                            Button { playlistQuery = "" } label: { Image(systemName: "xmark.circle.fill").frame(width: 44, height: 44) }
                                .buttonStyle(.plain).foregroundStyle(.secondary).accessibilityLabel("Playlist-Filter leeren")
                        }
                    }.padding(.leading, 14).padding(.trailing, playlistQuery.isEmpty ? 14 : 0).frame(minHeight: 44)
                        .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    Menu {
                        Picker("Sortierung", selection: $alphabetical) {
                            Text("Reihenfolge der Sammlung").tag(false)
                            Text("Name A–Z").tag(true)
                        }
                        Button("Aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }
                            .disabled(model.connecting || model.playlistBusy)
                    } label: {
                        Image(systemName: "arrow.up.arrow.down").font(.body.weight(.semibold)).frame(width: 44, height: 44).contentShape(Rectangle())
                    }
                    .foregroundStyle(.tint)
                    .accessibilityLabel("Playlists sortieren und aktualisieren")
                }
                VStack(spacing: 0) {
                    Button { creating = true } label: {
                        HStack(spacing: 12) {
                            Image(systemName: "plus").font(.system(size: 22, weight: .semibold)).foregroundStyle(.tint)
                                .frame(width: 60, height: 60)
                                .background(Color.accentColor.opacity(0.14), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                            Text("Neue Playlist").font(.body.weight(.semibold)).foregroundStyle(.tint)
                                .frame(maxWidth: .infinity, alignment: .leading)
                        }.padding(.horizontal, 16).padding(.vertical, 10).contentShape(Rectangle())
                    }
                    .buttonStyle(ListenPressStyle()).disabled(model.playlistBusy)
                    .accessibilityLabel("Neue Playlist").accessibilityHint("Mit eigenen Titeln oder intelligenten Regeln")
                    if !visiblePlaylists.isEmpty {
                        Divider().padding(.leading, 16 + 60 + 12)
                        LazyVStack(spacing: 0) {
                            ForEach(visiblePlaylists) { playlist in
                                NavigationLink(value: CollectionKind.playlist(playlist)) { MobilePlaylistRow(model: model, playlist: playlist) }
                                    .buttonStyle(ListenPressStyle())
                                    .task(id: model.playlistRevision) { await model.loadPlaylistPreviews([playlist]) }
                                if playlist.id != visiblePlaylists.last?.id { Divider().padding(.leading, 16 + 60 + 12) }
                            }
                        }
                    }
                }.background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
                if model.playlists.isEmpty { emptyPlaylists.padding(.top, 12) }
                else if visiblePlaylists.isEmpty {
                    ContentUnavailableView("Keine passende Playlist", systemImage: "magnifyingglass", description: Text("Versuche einen anderen Namen."))
                }
            }.padding(.horizontal, ListenMetrics.pageMargin).padding(.top, 8).padding(.bottom, 32)
                .frame(maxWidth: 760).frame(maxWidth: .infinity)
        }.background(Color(uiColor: .systemGroupedBackground))
            .scrollEdgeEffectStyle(.hard, for: .bottom)
            .scrollDismissesKeyboard(.interactively)
            .refreshable { await model.loadLibrary() }
            .modifier(TabRootChrome(active: tabRoot))
    }
    #endif
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
    /// Artist and album links of the iPhone and iPad player.
    var openCollection: ((CollectionKind) -> Void)? = nil
    var body: some View {
        GeometryReader { geometry in
            #if os(macOS)
            desktopPlayer(geometry.size)
            #elseif os(iOS)
            MobilePlayerView(model: model, openCollection: openCollection)
            #else
            // Apple TV shows "Jetzt läuft" full screen over the tabs instead.
            TVNowPlayingView(model: model) { openCollection?($0) }
            #endif
        }
        #if os(tvOS)
        .toolbar(.hidden, for: .navigationBar)
        #else
        .navigationTitle("Player")
        #endif
        #if os(iOS)
        .navigationBarTitleDisplayMode(.inline)
        #endif
    }
    #if os(macOS)
    private func desktopPlayer(_ size: CGSize) -> some View {
        DesktopListeningView(model: model, size: size)
    }
    #endif
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
