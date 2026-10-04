import SwiftUI
import AVKit
import YTMDLCore

#if os(macOS)
struct DesktopHoverStyle: ButtonStyle {
    var radius: CGFloat = 14
    func makeBody(configuration: Configuration) -> some View {
        DesktopHoverSurface(configuration: configuration, radius: radius)
    }
}
private struct DesktopHoverSurface: View {
    let configuration: ButtonStyleConfiguration
    let radius: CGFloat
    @State private var hovered = false
    var body: some View {
        configuration.label
            .background(Color.primary.opacity(configuration.isPressed ? 0.10 : hovered ? 0.05 : 0), in: RoundedRectangle(cornerRadius: radius))
            .overlay(RoundedRectangle(cornerRadius: radius).strokeBorder(Color.primary.opacity(hovered ? 0.12 : 0)))
            .contentShape(RoundedRectangle(cornerRadius: radius))
            .onHover { hovered = $0 }
            .animation(.easeOut(duration: 0.15), value: hovered)
    }
}
/// Shared controls keep the listening page and persistent transport consistent.
struct DesktopControlStyle: ButtonStyle {
    var prominent = false
    func makeBody(configuration: Configuration) -> some View {
        DesktopControlSurface(configuration: configuration, prominent: prominent)
    }
}
private struct DesktopControlSurface: View {
    @Environment(\.desktopTheme) private var theme
    let configuration: ButtonStyleConfiguration
    let prominent: Bool
    @State private var hovered = false
    var body: some View {
        configuration.label
            .foregroundStyle(prominent ? Color.white : Color.primary)
            .background(prominent ? theme.accent : Color.primary.opacity(hovered ? 0.1 : 0), in: Circle())
            .opacity(configuration.isPressed ? 0.7 : 1)
            .contentShape(Circle()).onHover { hovered = $0 }
    }
}
struct DesktopVolumeControl: View {
    var player: PlayerModel
    var body: some View {
        HStack(spacing: 10) {
            Button { player.toggleMute() } label: {
                Image(systemName: player.isMuted || player.volume == 0 ? "speaker.slash.fill" : "speaker.wave.2.fill")
                    .desktopScaledFont(18).frame(width: 36, height: 36)
            }.buttonStyle(DesktopControlStyle())
                .accessibilityLabel(player.isMuted ? "Ton einschalten" : "Stummschalten")
                .help(player.isMuted ? "Ton einschalten" : "Stummschalten")
            Slider(value: Binding(get: { player.volume }, set: { player.setVolume($0) }), in: 0...1)
                .accessibilityLabel("App-Lautstärke").accessibilityValue("\(Int(player.volume * 100)) Prozent")
                .help("Lautstärke von YTMDL")
            Text(player.isMuted ? "Aus" : "\(Int(player.volume * 100)) %")
                .desktopScaledFont(13).monospacedDigit().foregroundStyle(.secondary).frame(width: 52)
        }.frame(minWidth: 160, maxWidth: 260)
    }
}
struct DesktopPlaybackControls: View {
    @Environment(\.desktopAccent) private var accent
    var player: PlayerModel
    var large = false
    private var target: CGFloat { large ? 52 : 38 }
    var body: some View {
        HStack(spacing: large ? 16 : 8) {
            Button { player.shuffle() } label: { Image(systemName: "shuffle").frame(width: target, height: target) }
                .accessibilityLabel("Nächste Titel mischen").help("Nächste Titel mischen")
            Button { player.previous() } label: { Image(systemName: "backward.end.fill").frame(width: target, height: target) }
                .accessibilityLabel("Vorheriger Titel").help("Vorheriger Titel")
            Button { player.toggle() } label: {
                Image(systemName: player.isPlaying ? "pause.fill" : "play.fill")
                    .desktopScaledFont(large ? 30 : 19, weight: .semibold)
                    .frame(width: large ? 76 : 46, height: large ? 76 : 46)
            }.buttonStyle(DesktopControlStyle(prominent: true))
                .accessibilityLabel(player.isPlaying ? "Pause" : "Abspielen")
                .help(player.isPlaying ? "Pause" : "Abspielen")
            Button { player.next() } label: { Image(systemName: "forward.end.fill").frame(width: target, height: target) }
                .accessibilityLabel("Nächster Titel").help("Nächster Titel")
            Button { player.repeatAll.toggle() } label: {
                Image(systemName: "repeat").foregroundStyle(player.repeatAll ? accent : .primary).frame(width: target, height: target)
            }.accessibilityLabel(player.repeatAll ? "Wiederholen ausschalten" : "Warteschlange wiederholen")
                .accessibilityValue(player.repeatAll ? "Ein" : "Aus").help("Warteschlange wiederholen")
        }.desktopScaledFont(large ? 21 : 16).buttonStyle(DesktopControlStyle())
    }
}
struct DesktopSeekControl: View {
    var player: PlayerModel
    var body: some View {
        HStack(spacing: 12) {
            Text(formatTime(player.position)).accessibilityIdentifier("playbackElapsed").frame(width: 60)
            Slider(value: Binding(get: { min(player.position, player.duration) }, set: { player.seek($0) }), in: 0...max(1, player.duration))
                .accessibilityLabel("Wiedergabeposition").disabled(player.current == nil)
            Text(formatTime(player.duration)).frame(width: 60)
        }.desktopScaledFont(14).monospacedDigit().foregroundStyle(.secondary)
    }
}
struct DesktopTransportBar: View {
    var model: AppModel
    var track: Track
    var openPlayer: () -> Void
    @Environment(\.desktopTheme) private var theme
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        GeometryReader { geometry in
            if geometry.size.width >= 800 {
                HStack(spacing: 24) {
                    metadata.frame(maxWidth: .infinity, alignment: .leading)
                    transport.frame(width: min(580, geometry.size.width * 0.43))
                    output.frame(maxWidth: .infinity, alignment: .trailing)
                }.padding(.horizontal, 24).frame(maxHeight: .infinity)
            } else {
                VStack(spacing: 8) {
                    HStack(spacing: 16) { metadata; Spacer(minLength: 4); output }
                    transport
                }.padding(16)
            }
        }.onGeometryChange(for: CGFloat.self) { $0.size.width } action: { barWidth = $0 }
            .frame(height: barHeight).background(theme.surface(scheme)).overlay(alignment: .top) { Divider() }
    }
    // The container switches to two rows before either side can crowd transport.
    @State private var barWidth: CGFloat = 1000
    private var barHeight: CGFloat { barWidth < 800 ? 180 : 110 }
    private var metadata: some View {
        Button(action: openPlayer) {
            HStack(spacing: 14) {
                ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 60, height: 60)
                VStack(alignment: .leading, spacing: 5) {
                    Text(track.title).desktopScaledFont(17, weight: .semibold).lineLimit(1)
                    Text(track.artistText).desktopScaledFont(15).foregroundStyle(.secondary).lineLimit(1)
                }
            }.contentShape(Rectangle())
        }.buttonStyle(.plain).accessibilityLabel("Player öffnen: \(track.title)")
    }
    private var transport: some View {
        VStack(spacing: 4) { DesktopPlaybackControls(player: model.player); DesktopSeekControl(player: model.player) }
    }
    private var output: some View {
        HStack(spacing: 10) {
            DesktopVolumeControl(player: model.player)
            AirPlayPicker().frame(width: 36, height: 36).accessibilityLabel("Audioausgabe wählen")
        }.frame(maxWidth: 310)
    }
}

struct DesktopListeningView: View {
    @Environment(\.desktopAccent) private var accent
    var model: AppModel
    var size: CGSize
    @State private var tab = 0
    var body: some View {
        Group {
            if let track = model.player.current {
                if size.width >= 850 {
                    let cover = max(240, min(680, (size.width - 100) * 0.54, size.height - 350))
                    HStack(alignment: .center, spacing: 40) {
                        ScrollView {
                            listeningCard(track, cover: cover)
                        }.scrollIndicators(.hidden).frame(width: max(390, cover), height: min(size.height - 64, cover + 300))
                        queuePanel.frame(width: min(440, max(300, size.width - max(390, cover) - 100)))
                    }.padding(32)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .center)
                } else {
                    ScrollView {
                        VStack(spacing: 32) {
                            listeningCard(track, cover: max(200, min(440, size.width - 64, size.height * 0.48)))
                            queuePanel.frame(minHeight: 320, idealHeight: 440)
                        }.padding(28).frame(maxWidth: 660).frame(maxWidth: .infinity)
                    }
                }
            } else {
                ContentUnavailableView("Deine Musik wartet", systemImage: "play.circle", description: Text("Wähle ein Album, einen Lieblingstitel oder eine Playlist in der Seitenleiste."))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
    }
    private func listeningCard(_ track: Track, cover: CGFloat) -> some View {
        VStack(spacing: 14) {
            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: cover, height: cover)
                .shadow(color: .black.opacity(0.22), radius: 20, y: 12)
            HStack(spacing: 16) {
                VStack(alignment: .leading, spacing: 8) {
                    Text(track.title).desktopScaledFont(30, weight: .bold).lineLimit(2)
                    Text(track.artistText).desktopScaledFont(20).foregroundStyle(.secondary).lineLimit(2)
                    if !track.album.isEmpty && track.album != track.title { Text(track.album).desktopScaledFont(16).foregroundStyle(.secondary).lineLimit(1) }
                }.frame(maxWidth: .infinity, alignment: .leading)
                Button { Task { await model.toggleFavorite(track) } } label: {
                    Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart")
                        .desktopScaledFont(23).foregroundStyle(accent).frame(width: 48, height: 48)
                }.buttonStyle(DesktopControlStyle()).accessibilityLabel("Favorit umschalten")
            }
            DesktopSeekControl(player: model.player)
            DesktopPlaybackControls(player: model.player, large: true)
            HStack(spacing: 20) {
                DesktopVolumeControl(player: model.player)
                AirPlayPicker().frame(width: 40, height: 40).accessibilityLabel("Audioausgabe wählen")
            }
            if model.player.loading { ProgressView("Titel wird geladen …") }
            if let error = model.player.error {
                Text(error).desktopScaledFont(16).foregroundStyle(.secondary)
                Button("Erneut versuchen") { model.player.resume() }.buttonStyle(.bordered)
            }
        }.padding(.horizontal, 4).padding(.bottom, 12)
    }
    private var queuePanel: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack(spacing: 12) {
                panelTab("Warteschlange", value: 0)
                panelTab("Lyrics", value: 1)
                Spacer(minLength: 0)
            }
            if tab == 0 { Text("\(model.player.queue.tracks.count) Titel in dieser Sitzung").desktopScaledFont(15).foregroundStyle(.secondary) }
            ScrollView {
                if tab == 0 {
                    LazyVStack(spacing: 10) {
                        ForEach(Array(model.player.queue.tracks.enumerated()), id: \.offset) { index, track in
                            Button { model.player.select(index) } label: {
                                HStack(spacing: 14) {
                                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 56, height: 56)
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(track.title).desktopScaledFont(18, weight: .semibold).lineLimit(1)
                                        Text(track.artistText).desktopScaledFont(15).foregroundStyle(.secondary).lineLimit(1)
                                    }.frame(maxWidth: .infinity, alignment: .leading)
                                    if model.player.queue.index == index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(accent) }
                                }.padding(12).background(model.player.queue.index == index ? accent.opacity(0.12) : Color.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 16))
                                    .contentShape(Rectangle())
                            }.buttonStyle(.plain).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
                        }
                    }
                } else {
                    Text(model.player.lyrics.isEmpty ? "Lyrics werden geladen …" : cleanLyrics(model.player.lyrics))
                        .desktopScaledFont(24).lineSpacing(14).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }.padding(24).frame(height: max(360, min(850, size.height - 64)))
            .background(.quaternary.opacity(0.35), in: RoundedRectangle(cornerRadius: 24))
    }
    private func panelTab(_ title: String, value: Int) -> some View {
        Button { tab = value } label: {
            Text(title).desktopScaledFont(18, weight: .semibold).padding(.horizontal, 14).padding(.vertical, 12)
                .background(tab == value ? accent.opacity(0.18) : .clear, in: RoundedRectangle(cornerRadius: 12))
                .foregroundStyle(tab == value ? accent : .primary)
        }.buttonStyle(.plain).accessibilityAddTraits(tab == value ? .isSelected : [])
    }
}
#endif
