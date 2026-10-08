import SwiftUI
import YTMDLCore

#if os(macOS)
struct DesktopHomeView: View {
    var model: AppModel
    var navigate: (Destination) -> Void
    @Environment(\.desktopTheme) private var theme
    @Environment(\.colorScheme) private var scheme
    @Environment(\.desktopAccent) private var accent
    @AppStorage("homeShowFavorites") private var showFavorites = true
    @AppStorage("homeShowArtists") private var showArtists = true
    @AppStorage("homeShowPlaylists") private var showPlaylists = true
    @AppStorage("homeShowRecent") private var showRecent = true
    @State private var favorites: [Track] = []
    @State private var favoritesError: String?
    @State private var retry = 0
    @State private var availableWidth: CGFloat = 1000
    private var compact: Bool { availableWidth < 700 }
    private struct FavoritesRequest: Hashable {
        let ids: Set<String>
        let visible: Bool
        let retry: Int
    }
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 36) {
                hero
                quickActions
                if showRecent && model.listeningHistory.enabled {
                    if !model.listeningHistory.tracks.isEmpty {
                        sectionHeading("Zuletzt gehört", subtitle: "Dein Hörverlauf auf diesem Mac")
                        trackShelf(Array(model.listeningHistory.tracks.prefix(10)), compact: true)
                    } else {
                        Label("Dein Hörverlauf beginnt, sobald du hier Musik abspielst.", systemImage: "clock")
                            .desktopScaledFont(17).foregroundStyle(.secondary)
                    }
                }
                if !model.releases.isEmpty {
                    sectionHeading("Neu in deiner Bibliothek", subtitle: "Deine zuletzt hinzugefügten Alben")
                    ScrollView(.horizontal) {
                        HStack(alignment: .top, spacing: 24) {
                            ForEach(Array(model.releases.prefix(12))) { release in
                                NavigationLink(value: CollectionKind.release(release)) {
                                    VStack(alignment: .leading, spacing: 12) {
                                        ArtworkView(model: model, kind: "releases", id: release.id).frame(width: 220, height: 220)
                                        Text(release.title).desktopScaledFont(19, weight: .semibold).foregroundStyle(.primary).lineLimit(2)
                                        Text(release.artists.joined(separator: " · ")).desktopScaledFont(16).foregroundStyle(.secondary).lineLimit(1)
                                    }.frame(width: 220, alignment: .leading)
                                }.buttonStyle(DesktopHoverStyle(radius: 20))
                            }
                        }.padding(.bottom, 8)
                    }
                } else if model.connecting { ProgressView("Deine Bibliothek wird geladen …") }
                else { ContentUnavailableView("Deine Startseite wartet auf Musik", systemImage: "music.note", description: Text("Füge Musik im Web hinzu oder aktualisiere deine Bibliothek.")) }
                if showFavorites {
                    sectionHeading("Deine Lieblingstitel", subtitle: "Schnell wieder bei deiner Musik")
                    if !favorites.isEmpty { trackShelf(favorites) }
                    else if let favoritesError {
                        Text(favoritesError).foregroundStyle(.secondary)
                        Button("Erneut laden") { retry += 1 }.buttonStyle(.bordered)
                    } else {
                        Text("Markiere Titel mit dem Herz. Deine Favoriten erscheinen dann hier.").desktopScaledFont(17).foregroundStyle(.secondary)
                    }
                }
                if showPlaylists && !model.playlists.isEmpty {
                    sectionHeading("Deine Playlists", subtitle: "Für jeden Moment die passende Sammlung")
                    LazyVGrid(columns: [GridItem(.adaptive(minimum: 240, maximum: 420), spacing: 20)], spacing: 20) {
                        ForEach(Array(model.playlists.prefix(6))) { playlist in
                            NavigationLink(value: CollectionKind.playlist(playlist)) {
                                HStack(spacing: 16) {
                                    Image(systemName: "music.note.list").desktopScaledFont(30).foregroundStyle(accent)
                                    VStack(alignment: .leading, spacing: 6) {
                                        Text(playlist.name).desktopScaledFont(20, weight: .semibold).foregroundStyle(.primary).lineLimit(2)
                                        Text("\(playlist.trackCount) Titel").desktopScaledFont(16).foregroundStyle(.secondary)
                                    }
                                    Spacer(minLength: 0)
                                }.padding(24).frame(maxWidth: .infinity, minHeight: 92, alignment: .leading)
                                    .background(theme.surface(scheme), in: RoundedRectangle(cornerRadius: 20))
                            }.buttonStyle(DesktopHoverStyle(radius: 20))
                        }
                    }
                }
                if showArtists && !model.artists.isEmpty {
                    sectionHeading("Künstler in deiner Sammlung", subtitle: "Stöbere weiter in deiner Bibliothek")
                    ScrollView(.horizontal) {
                        HStack(alignment: .top, spacing: 28) {
                            ForEach(Array(model.artists.prefix(10))) { artist in
                                NavigationLink(value: CollectionKind.artist(artist)) {
                                    VStack(spacing: 14) {
                                        ArtworkView(model: model, kind: "artists", id: artist.id).frame(width: 160, height: 160).clipShape(Circle())
                                        Text(artist.name).desktopScaledFont(18, weight: .semibold).foregroundStyle(.primary).lineLimit(2)
                                    }.frame(width: 175)
                                }.buttonStyle(DesktopHoverStyle(radius: 20))
                            }
                        }.padding(.bottom, 8)
                    }
                }
            }.padding(compact ? 20 : 36).frame(maxWidth: 1700, alignment: .leading).frame(maxWidth: .infinity)
        }.navigationTitle("Start")
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { availableWidth = $0 }
            .task(id: FavoritesRequest(ids: model.favoriteIDs, visible: showFavorites, retry: retry)) { await loadFavorites() }
            .toolbar { ToolbarItem { Button("Bibliothek aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary(); retry += 1 } }.disabled(model.connecting) } }
    }
    private var hero: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 36) { introduction; heroArtwork.frame(width: 220, height: 220) }
            introduction
        }.padding(compact ? 24 : 32).frame(maxWidth: .infinity, minHeight: compact ? 0 : 250, alignment: .leading)
            .background(LinearGradient(colors: [accent.opacity(scheme == .dark ? 0.28 : 0.15), theme.surface(scheme)], startPoint: .topLeading, endPoint: .bottomTrailing), in: RoundedRectangle(cornerRadius: 28))
    }
    private var introduction: some View {
        VStack(alignment: .leading, spacing: compact ? 14 : 18) {
            Text("Hallo, \(model.user?.displayName ?? "")").desktopScaledFont(18, weight: .medium).foregroundStyle(.secondary)
            Text("Deine Musik.\nDein Moment.").desktopScaledFont(compact ? 28 : 38, weight: .bold).fixedSize(horizontal: false, vertical: true)
            Text(model.player.current == nil ? "Alben entdecken, Lieblingstitel hören oder eine Playlist starten." : "Deine Wiedergabe läuft hier weiter. Alles andere bleibt in Reichweite.")
                .desktopScaledFont(compact ? 16 : 18).foregroundStyle(.secondary)
            ViewThatFits(in: .horizontal) {
                HStack(spacing: 14) { heroButtons }
                VStack(alignment: .leading, spacing: 12) { heroButtons }
            }
        }.frame(maxWidth: .infinity, alignment: .leading)
    }
    @ViewBuilder private var heroButtons: some View {
        Button {
            navigate(model.player.current == nil ? .library : .player)
        } label: {
            Label(model.player.current == nil ? "Bibliothek öffnen" : "Zum Player", systemImage: model.player.current == nil ? "square.stack" : "play.circle.fill")
                .desktopScaledFont(17, weight: .semibold).padding(.horizontal, 18).padding(.vertical, 13)
                .foregroundStyle(.white).background(theme.accent, in: RoundedRectangle(cornerRadius: 14))
        }.buttonStyle(DesktopHoverStyle()).fixedSize()
        Button { navigate(.favorites) } label: {
            Label("Lieblingstitel", systemImage: "heart.fill").desktopScaledFont(17, weight: .semibold)
                .padding(.horizontal, 18).padding(.vertical, 13)
                .background(Color.primary.opacity(0.07), in: RoundedRectangle(cornerRadius: 14))
        }.buttonStyle(DesktopHoverStyle()).fixedSize()
    }
    @ViewBuilder private var heroArtwork: some View {
        if let track = model.player.current {
            Button { navigate(.player) } label: { ArtworkView(model: model, kind: "tracks", id: track.id) }
                .buttonStyle(DesktopHoverStyle(radius: 20)).accessibilityLabel("Player öffnen: \(track.title)")
        } else if let release = model.releases.first {
            NavigationLink(value: CollectionKind.release(release)) { ArtworkView(model: model, kind: "releases", id: release.id) }.buttonStyle(DesktopHoverStyle(radius: 20))
        }
    }
    private var quickActions: some View {
        let contentWidth = min(availableWidth, 1700) - (compact ? 40 : 72)
        let count = contentWidth >= 1020 ? 3 : contentWidth >= 680 ? 2 : 1
        return LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 18), count: count), spacing: 18) {
            quickAction("Deine Favoriten", detail: "\(model.favoriteIDs.count) Lieblingstitel", icon: "heart.fill", destination: .favorites)
            quickAction("Deine Playlists", detail: "Deine Sammlungen öffnen", icon: "music.note.list", destination: .playlists)
            quickAction("Etwas finden", detail: "Titel, Alben und Künstler", icon: "magnifyingglass", destination: .search)
        }
    }
    private func quickAction(_ title: String, detail: String, icon: String, destination: Destination) -> some View {
        Button { navigate(destination) } label: {
            HStack(spacing: 18) {
                Image(systemName: icon).desktopScaledFont(27).foregroundStyle(accent).frame(width: 44)
                VStack(alignment: .leading, spacing: 6) {
                    Text(title).desktopScaledFont(20, weight: .semibold).foregroundStyle(.primary)
                    Text(detail).desktopScaledFont(16).foregroundStyle(.secondary)
                }
                Spacer(minLength: 0)
            }.padding(22).frame(maxWidth: .infinity, minHeight: 104, alignment: .leading)
                .background(theme.surface(scheme), in: RoundedRectangle(cornerRadius: 20))
        }.buttonStyle(DesktopHoverStyle(radius: 20))
    }
    private func sectionHeading(_ title: String, subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 8) { Text(title).desktopScaledFont(27, weight: .bold); Text(subtitle).desktopScaledFont(17).foregroundStyle(.secondary) }
    }
    private func trackShelf(_ tracks: [Track], compact: Bool = false) -> some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 24) {
                ForEach(Array(tracks.prefix(10))) { track in
                    Button {
                        if let client = model.client { model.player.play(tracks, start: tracks.firstIndex(of: track) ?? 0, client: client) }
                    } label: {
                        if compact {
                            HStack(spacing: 16) {
                                ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 80, height: 80)
                                VStack(alignment: .leading, spacing: 8) {
                                    Text(track.title).desktopScaledFont(19, weight: .semibold).foregroundStyle(.primary).lineLimit(2)
                                    Text(track.artistText).desktopScaledFont(16).foregroundStyle(.secondary).lineLimit(1)
                                }
                                Spacer(minLength: 0)
                                Image(systemName: "play.circle.fill").desktopScaledFont(28).foregroundStyle(accent)
                            }.padding(16).frame(width: 360, alignment: .leading).background(theme.surface(scheme), in: RoundedRectangle(cornerRadius: 20))
                        } else {
                            VStack(alignment: .leading, spacing: 12) {
                                ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 180, height: 180)
                                Text(track.title).desktopScaledFont(19, weight: .semibold).foregroundStyle(.primary).lineLimit(2)
                                Text(track.artistText).desktopScaledFont(16).foregroundStyle(.secondary).lineLimit(1)
                            }.frame(width: 180, alignment: .leading)
                        }
                    }.buttonStyle(DesktopHoverStyle(radius: 20)).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
                }
            }.padding(.bottom, 8)
        }
    }
    private func loadFavorites() async {
        guard showFavorites, let client = model.client else { return }
        let account = model.user?.id
        do {
            let result: [Track] = try await client.get("/library/tracks", query: [.init(name: "favorite", value: "true"), .init(name: "limit", value: "25")])
            try Task.checkCancellation()
            guard model.client === client, model.user?.id == account else { return }
            favorites = result; favoritesError = nil
        } catch is CancellationError { }
        catch { if !Task.isCancelled { favoritesError = "Deine Favoriten konnten nicht geladen werden. Bitte erneut versuchen." } }
    }
}
#endif
