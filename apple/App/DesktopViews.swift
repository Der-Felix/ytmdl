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
    @Environment(\.desktopButtonAccent) private var buttonAccent
    let configuration: ButtonStyleConfiguration
    let prominent: Bool
    @State private var hovered = false
    var body: some View {
        configuration.label
            .foregroundStyle(prominent ? Color.white : Color.primary)
            .background(prominent ? buttonAccent : Color.primary.opacity(hovered ? 0.1 : 0), in: Circle())
            .opacity(configuration.isPressed ? 0.7 : 1)
            .contentShape(Circle()).onHover { hovered = $0 }
    }
}
struct DesktopVolumeControl: View {
    var player: PlayerModel
    var expanded = false
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
        }.frame(minWidth: 160, maxWidth: expanded ? 420 : 260)
    }
}
struct DesktopPlaybackControls: View {
    @Environment(\.desktopAccent) private var accent
    var player: PlayerModel
    var large = false
    private var target: CGFloat { large ? 56 : 38 }
    var body: some View {
        HStack(spacing: large ? 16 : 8) {
            Button { player.shuffle() } label: { Image(systemName: "shuffle").frame(width: target, height: target) }
                .accessibilityLabel("Nächste Titel mischen").help("Nächste Titel mischen")
            Button { player.previous() } label: { Image(systemName: "backward.end.fill").frame(width: target, height: target) }
                .accessibilityLabel("Vorheriger Titel").help("Vorheriger Titel")
            Button { player.toggle() } label: {
                Image(systemName: player.isPlaybackRequested ? "pause.fill" : "play.fill")
                    .desktopScaledFont(large ? 34 : 19, weight: .semibold)
                    .frame(width: large ? 88 : 46, height: large ? 88 : 46)
            }.buttonStyle(DesktopControlStyle(prominent: true))
                .accessibilityLabel(player.isPlaying ? "Pause" : "Abspielen")
                .help(player.isPlaying ? "Pause" : "Abspielen")
            Button { player.next() } label: { Image(systemName: "forward.end.fill").frame(width: target, height: target) }
                .accessibilityLabel("Nächster Titel").help("Nächster Titel")
            Button {
                if player.repeatOne { player.setRepeatOne(false); player.repeatAll = false }
                else if player.repeatAll { player.repeatAll = false; player.setRepeatOne(true) }
                else { player.repeatAll = true }
            } label: {
                Image(systemName: player.repeatOne ? "repeat.1" : "repeat").foregroundStyle(player.repeatAll || player.repeatOne ? accent : .primary).frame(width: target, height: target)
            }.accessibilityLabel("Wiederholmodus ändern")
                .accessibilityValue(player.repeatOne ? "Ein Titel" : player.repeatAll ? "Warteschlange" : "Aus").help("Wiederholung: Aus, Warteschlange, ein Titel")
        }.desktopScaledFont(large ? 24 : 16).buttonStyle(DesktopControlStyle())
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
    @Environment(\.desktopTheme) private var theme
    @Environment(\.desktopAccent) private var accent
    @Environment(\.colorScheme) private var scheme
    @AppStorage("playerCoverColors") private var coverColors = true
    var model: AppModel
    var size: CGSize
    @State private var tab = 0
    @State private var queueFilter = ""
    var body: some View {
        Group {
            if let track = model.player.current {
                if size.width >= 1600 {
                    let available = size.width - 112
                    let artworkWidth = min(480, available * 0.27)
                    let contextWidth = min(600, available * 0.28)
                    let controlWidth = available - artworkWidth - contextWidth
                    HStack(alignment: .top, spacing: 24) {
                        ScrollView {
                            artworkCard(track, cover: max(240, min(420, artworkWidth - 8, size.height - 210)))
                        }.scrollIndicators(.hidden).frame(width: artworkWidth)
                        ScrollView {
                            playbackCard(track, expanded: true)
                        }.scrollIndicators(.hidden).frame(width: controlWidth)
                        queuePanel.frame(width: contextWidth)
                    }.padding(32).frame(maxWidth: .infinity, maxHeight: .infinity)
                } else if size.width >= 850 {
                    let available = size.width - 80
                    let contextWidth = max(340, min(560, available * 0.44))
                    let artworkWidth = available - contextWidth
                    VStack(spacing: 0) {
                        HStack(alignment: .top, spacing: 24) {
                            ScrollView {
                                artworkCard(track, cover: max(200, min(420, artworkWidth - 8, size.height - 460)))
                            }.scrollIndicators(.hidden).frame(width: artworkWidth)
                            queuePanel
                        }.padding(28).frame(maxWidth: .infinity, maxHeight: .infinity)
                        playbackDock(track)
                    }
                } else {
                    VStack(spacing: 0) {
                        ScrollView {
                            VStack(spacing: 24) {
                                artworkCard(track, cover: max(180, min(320, size.width - 56, size.height - 440)))
                                queuePanel.frame(height: 540)
                            }.padding(24).frame(maxWidth: 720).frame(maxWidth: .infinity)
                        }
                        playbackDock(track, compact: true)
                    }
                }
            } else {
                ContentUnavailableView("Deine Musik wartet", systemImage: "play.circle", description: Text("Wähle ein Album, einen Lieblingstitel oder eine Playlist in der Seitenleiste."))
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .background(playerBackground)
        .environment(\.desktopAccent, playerAccent)
        .environment(\.desktopButtonAccent, playerButtonAccent)
        .tint(playerAccent)
        #if DEBUG
        .task {
            let args = ProcessInfo.processInfo.arguments
            if args.contains("--fixture-server"), model.client?.server.url.host == "127.0.0.1",
               let index = args.firstIndex(of: "--fixture-player-tab"), args.indices.contains(index + 1) {
                tab = args[index + 1] == "Klang" ? 2 : args[index + 1] == "Lyrics" ? 1 : 0
            }
        }
        #endif
    }
    private var playerButtonAccent: Color {
        guard coverColors, let palette = model.player.artworkPalette else { return theme.accent }
        let value = palette.accent.button
        return Color(red: value.red, green: value.green, blue: value.blue)
    }
    private var playerAccent: Color { scheme == .dark ? playerButtonAccent.mix(with: .white, by: 0.38) : playerButtonAccent }
    private var playerBackground: some View {
        ZStack {
            if coverColors, let palette = model.player.artworkPalette {
                let primary = Color(red: palette.dominant.red, green: palette.dominant.green, blue: palette.dominant.blue)
                let secondary = Color(red: palette.secondary.red, green: palette.secondary.green, blue: palette.secondary.blue)
                (scheme == .dark ? Color.black.mix(with: primary, by: 0.16) : Color.white.mix(with: primary, by: 0.08))
                RadialGradient(colors: [primary.opacity(scheme == .dark ? 0.32 : 0.14), .clear], center: .topLeading, startRadius: 0, endRadius: max(size.width, size.height) * 0.75)
                RadialGradient(colors: [secondary.opacity(scheme == .dark ? 0.24 : 0.09), .clear], center: .bottomTrailing, startRadius: 0, endRadius: max(size.width, size.height) * 0.65)
            } else { theme.background(scheme) }
        }
    }
    private func artworkCard(_ track: Track, cover: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                Text("JETZT LÄUFT").desktopScaledFont(13, weight: .semibold).tracking(2).foregroundStyle(.secondary)
                Spacer()
                if model.player.equalizer.enabled { Label("EQ", systemImage: "slider.vertical.3").desktopScaledFont(14).foregroundStyle(playerAccent) }
                if model.player.sleepMode != .off { Image(systemName: "moon.zzz.fill").foregroundStyle(playerAccent) }
            }
            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: cover, height: cover)
                .shadow(color: .black.opacity(0.22), radius: 20, y: 12)
                .frame(maxWidth: .infinity)
            HStack(alignment: .top, spacing: 16) {
                VStack(alignment: .leading, spacing: 10) {
                    Text(track.title).desktopScaledFont(32, weight: .bold).lineLimit(3)
                    Text(track.artistText).desktopScaledFont(23).foregroundStyle(.secondary).lineLimit(2)
                    if !track.album.isEmpty && track.album != track.title { Text(track.album).desktopScaledFont(18).foregroundStyle(.secondary).lineLimit(1) }
                }.frame(maxWidth: .infinity, alignment: .leading)
                Button { Task { await model.toggleFavorite(track) } } label: {
                    Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart")
                        .desktopScaledFont(27).foregroundStyle(playerAccent).frame(width: 56, height: 56)
                }.buttonStyle(DesktopControlStyle()).accessibilityLabel("Favorit umschalten")
            }
        }.padding(.horizontal, 4).padding(.bottom, 8)
    }
    private func playbackCard(_ track: Track, expanded: Bool = false) -> some View {
        VStack(alignment: .leading, spacing: expanded ? 18 : 28) {
            Text("Wiedergabe").desktopScaledFont(26, weight: .bold)
            ViewThatFits(in: .horizontal) {
                DesktopPlaybackControls(player: model.player, large: true)
                DesktopPlaybackControls(player: model.player)
            }.frame(maxWidth: .infinity)
            DesktopSeekControl(player: model.player)
            HStack(spacing: 16) {
                DesktopVolumeControl(player: model.player, expanded: expanded)
                Spacer(minLength: 0)
                AirPlayPicker().frame(width: 44, height: 44).accessibilityLabel("Audioausgabe wählen")
            }
            playbackStatus(track)
            Divider()
            if expanded {
                Text("Equalizer").desktopScaledFont(24, weight: .semibold)
                EqualizerControls(player: model.player, inline: true)
                Divider()
                HStack {
                    Text("Überblendung").desktopScaledFont(22, weight: .semibold)
                    Spacer()
                    Text(model.player.crossfadeSeconds == 0 ? "Aus" : "\(Int(model.player.crossfadeSeconds)) s").desktopScaledFont(19).monospacedDigit().foregroundStyle(.secondary)
                }
                Slider(value: Binding(get: { model.player.crossfadeSeconds }, set: { model.player.setCrossfade($0) }), in: 0...12, step: 1)
                    .accessibilityLabel("Überblendung zwischen Titeln")
                Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { model.player.smartAlbumTransition }, set: { model.player.setSmartAlbumTransition($0) })).desktopScaledFont(17)
                HStack(spacing: 14) { timerMenu; speedMenu }
                Button { tab = 1 } label: { playerActionLabel("Lyrics anzeigen", icon: "text.quote") }.buttonStyle(DesktopHoverStyle(radius: 12))
            } else {
                Text("Dein Klang").desktopScaledFont(22, weight: .semibold)
                quickTools
                HStack(spacing: 12) {
                    Button { tab = 2 } label: { playerActionLabel("Equalizer", icon: "slider.vertical.3") }
                    Button { tab = 1 } label: { playerActionLabel("Lyrics", icon: "text.quote") }
                }.buttonStyle(DesktopHoverStyle(radius: 12))
            }
            Divider()
            HStack {
                Button { model.player.seek(model.player.position - 10) } label: { playerActionLabel("−10 s", icon: "gobackward.10") }
                Spacer(minLength: 8)
                Button { model.player.seek(model.player.position + 10) } label: { playerActionLabel("+10 s", icon: "goforward.10") }
            }.buttonStyle(DesktopHoverStyle(radius: 12))
            if let deadline = model.player.sleepDeadline {
                Label("Stoppt um \(deadline.formatted(date: .omitted, time: .shortened))", systemImage: "moon.zzz").desktopScaledFont(16).foregroundStyle(.secondary)
            }
            playbackError
        }.padding(24).frame(maxWidth: .infinity, alignment: .leading)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 26))
            .overlay(RoundedRectangle(cornerRadius: 26).strokeBorder(Color.primary.opacity(0.08)))
    }
    private func playerActionLabel(_ title: String, icon: String) -> some View {
        Label(title, systemImage: icon).desktopScaledFont(17, weight: .medium)
            .padding(.horizontal, 14).frame(minHeight: 46)
            .background(Color.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 12))
    }
    private func playbackDock(_ track: Track, compact: Bool = false) -> some View {
        VStack(spacing: 14) {
            if compact {
                VStack(spacing: 8) {
                    DesktopPlaybackControls(player: model.player)
                    DesktopSeekControl(player: model.player)
                    dockOutput
                }
                ScrollView(.horizontal) { compactTools.frame(minWidth: 660) }.scrollIndicators(.hidden)
            } else {
                HStack(spacing: 24) {
                    VStack(spacing: 8) {
                        DesktopPlaybackControls(player: model.player, large: true)
                        DesktopSeekControl(player: model.player)
                    }.frame(maxWidth: .infinity)
                    dockOutput.frame(maxWidth: 300)
                }
                HStack(spacing: 20) {
                    compactTools
                    Spacer(minLength: 0)
                    playbackStatus(track)
                }
            }
            playbackError
        }.padding(.horizontal, compact ? 20 : 28).padding(.vertical, compact ? 14 : 20)
            .background(.ultraThinMaterial).overlay(alignment: .top) { Divider() }
    }
    private var dockOutput: some View {
        HStack(spacing: 12) {
            DesktopVolumeControl(player: model.player)
            AirPlayPicker().frame(width: 40, height: 40).accessibilityLabel("Audioausgabe wählen")
        }
    }
    private var quickTools: some View {
        LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: 14) {
            eqMenu
            fadeMenu
            timerMenu
            speedMenu
        }
    }
    private var compactTools: some View {
        HStack(spacing: 10) {
            eqMenu
            fadeMenu
            timerMenu
            speedMenu
        }
    }
    private var eqMenu: some View {
        Menu {
            Toggle("Equalizer aktivieren", isOn: Binding(get: { model.player.equalizer.enabled }, set: { model.player.equalizer.setEnabled($0) }))
            Divider()
            ForEach(EqualizerPreset.allCases) { preset in
                Button(preset.name) { model.player.equalizer.select(preset); model.player.equalizer.setEnabled(true) }
            }
            Divider()
            Button("Alle Frequenzbänder anzeigen") { tab = 2 }
        } label: {
            toolLabel("Equalizer", value: model.player.equalizer.enabled ? (EqualizerPreset(rawValue: model.player.equalizer.preset)?.name ?? "Eigener Klang") : "Aus", icon: "slider.vertical.3")
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 14))
    }
    private var fadeMenu: some View {
        Menu {
            ForEach([0, 2, 4, 6, 8, 12], id: \.self) { seconds in
                Button(seconds == 0 ? "Aus" : "\(seconds) Sekunden") { model.player.setCrossfade(Double(seconds)) }
            }
            Divider()
            Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { model.player.smartAlbumTransition }, set: { model.player.setSmartAlbumTransition($0) }))
            Button("Übergänge einstellen") { tab = 2 }
        } label: {
            toolLabel("Übergang", value: model.player.crossfadeSeconds == 0 ? "Aus" : "\(Int(model.player.crossfadeSeconds)) s", icon: "waveform")
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 14))
    }
    private var timerMenu: some View {
        Menu {
            ForEach(SleepMode.allCases) { mode in Button(mode.name) { model.player.setSleepMode(mode) } }
        } label: {
            toolLabel("Sleep-Timer", value: model.player.sleepMode.name, icon: "moon.zzz")
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 14))
    }
    private var speedMenu: some View {
        Menu {
            ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0], id: \.self) { speed in
                Button(String(format: "%g×", speed)) { model.player.setPlaybackRate(speed) }
            }
        } label: {
            toolLabel("Tempo", value: String(format: "%g×", model.player.playbackRate), icon: "speedometer")
        }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 14))
    }
    private func toolLabel(_ title: String, value: String, icon: String) -> some View {
        HStack(spacing: 10) {
            Image(systemName: icon).foregroundStyle(playerAccent).desktopScaledFont(19)
            VStack(alignment: .leading, spacing: 5) {
                Text(title).desktopScaledFont(16, weight: .semibold).lineLimit(1).minimumScaleFactor(0.8)
                Text(value).desktopScaledFont(14).foregroundStyle(.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }.padding(12).frame(maxWidth: .infinity, minHeight: 64, alignment: .leading)
            .background(Color.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 14))
            .contentShape(RoundedRectangle(cornerRadius: 14))
    }
    private func playbackStatus(_ track: Track) -> some View {
        HStack(spacing: 10) {
            if model.player.loading { ProgressView().controlSize(.small); Text("Titel wird vorbereitet …") }
            else if model.player.isCrossfading { Label("Weicher Übergang", systemImage: "waveform") }
            else { Text(track.codec?.uppercased() ?? "AUDIO"); Text("·"); Text(formatTime(model.player.duration)) }
        }.desktopScaledFont(14).foregroundStyle(.secondary).frame(minHeight: 24)
    }
    @ViewBuilder private var playbackError: some View {
        if let error = model.player.error {
            Text(error).desktopScaledFont(16).foregroundStyle(.secondary)
            Button("Erneut versuchen") { model.player.resume() }.buttonStyle(.bordered)
        }
    }
    private var queuePanel: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack(spacing: 8) {
                panelTab("Warteschlange", value: 0)
                panelTab("Lyrics", value: 1)
                panelTab("Klang", value: 2)
            }
            if tab == 0 {
                HStack {
                    Text("\(model.player.queue.tracks.count) Titel · \(max(0, model.player.queue.tracks.count - model.player.queue.index - 1)) als Nächstes").desktopScaledFont(15).foregroundStyle(.secondary)
                    Spacer()
                    Button { model.player.shuffle() } label: {
                        Image(systemName: "shuffle").desktopScaledFont(20).frame(width: 44, height: 44)
                    }.buttonStyle(DesktopControlStyle()).accessibilityLabel("Nächste Titel mischen").help("Nur die nächsten Titel mischen")
                }
                HStack(spacing: 10) {
                    Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                    TextField("Warteschlange filtern", text: $queueFilter).textFieldStyle(.plain).accessibilityLabel("Warteschlange filtern")
                    if !queueFilter.isEmpty {
                        Button { queueFilter = "" } label: { Image(systemName: "xmark.circle.fill") }.buttonStyle(.plain).accessibilityLabel("Filter leeren")
                    }
                }.desktopScaledFont(17).padding(12)
                    .background(Color.primary.opacity(0.045), in: RoundedRectangle(cornerRadius: 12))
            }
            ScrollView {
                if tab == 0 {
                    LazyVStack(spacing: 10) {
                        ForEach(Array(model.player.queue.tracks.enumerated()).filter { queueFilter.isEmpty || $0.element.title.localizedCaseInsensitiveContains(queueFilter) || $0.element.artistText.localizedCaseInsensitiveContains(queueFilter) }, id: \.offset) { index, track in
                            Button { model.player.select(index) } label: {
                                HStack(spacing: 14) {
                                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 56, height: 56)
                                    VStack(alignment: .leading, spacing: 5) {
                                        Text(track.title).desktopScaledFont(18, weight: .semibold).lineLimit(1)
                                        Text(track.artistText).desktopScaledFont(15).foregroundStyle(.secondary).lineLimit(1)
                                    }.frame(maxWidth: .infinity, alignment: .leading)
                                    if model.player.queue.index == index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(playerAccent) }
                                }.padding(12).background(model.player.queue.index == index ? playerAccent.opacity(0.12) : Color.primary.opacity(0.035), in: RoundedRectangle(cornerRadius: 16))
                                    .contentShape(Rectangle())
                            }.buttonStyle(DesktopHoverStyle(radius: 16)).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
                        }
                    }
                } else if tab == 2 {
                    VStack(alignment: .leading, spacing: 32) {
                        EqualizerControls(player: model.player)
                        Divider()
                        PlaybackOptions(player: model.player)
                    }
                } else {
                    Text(model.player.lyrics.isEmpty ? "Lyrics werden geladen …" : cleanLyrics(model.player.lyrics))
                        .desktopScaledFont(24).lineSpacing(14).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }.padding(24).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 26))
            .overlay(RoundedRectangle(cornerRadius: 26).strokeBorder(Color.primary.opacity(0.08)))
    }
    private func panelTab(_ title: String, value: Int) -> some View {
        Button { tab = value } label: {
            Text(title).desktopScaledFont(18, weight: .semibold).lineLimit(1).minimumScaleFactor(0.75)
                .padding(.horizontal, 10).padding(.vertical, 12)
                .background(tab == value ? playerAccent.opacity(0.18) : .clear, in: RoundedRectangle(cornerRadius: 12))
                .foregroundStyle(tab == value ? playerAccent : .primary)
        }.buttonStyle(DesktopHoverStyle(radius: 12)).accessibilityAddTraits(tab == value ? .isSelected : [])
    }
}
#endif
