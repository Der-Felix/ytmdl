import SwiftUI
import YTMDLCore
#if os(iOS)
import UIKit
import MediaPlayer

/// Sizes of the player ("Jetzt läuft"), its queue and the mini player on
/// iPhone and iPad. Spacing follows `ListenMetrics` (16 pt margins, 12 pt gaps).
enum PlayerMetrics {
    /// Side margin of the player: wider than a list page, so the cover and the
    /// controls breathe; the queue uses the list margin.
    static let margin: CGFloat = 24
    /// The column every part of the player shares. On iPad the cover stops
    /// growing here instead of filling a wide sheet.
    static let maxColumn: CGFloat = 480
    static let maxCover: CGFloat = 420
    static let minCover: CGFloat = 160
    static let coverRadius: CGFloat = 16
    /// The cover steps back while paused and springs forward on play.
    static let pausedCoverScale: CGFloat = 0.86
    static let target: CGFloat = 44
    static let skipTarget: CGFloat = 64
    static let playTarget: CGFloat = 80
    static let scrubberHeight: CGFloat = 6
    static let scrubberActiveHeight: CGFloat = 12
    static let scrubberTouchHeight: CGFloat = 32
    /// Height everything below the cover needs at default text size; the
    /// cover takes the rest, so the player fits without scrolling.
    static func reserved(for size: DynamicTypeSize) -> CGFloat {
        size.isAccessibilitySize ? 600 : size > .large ? 400 : 350
    }
    static let queueArtwork: CGFloat = 44
}

/// The mini player in the tab bar's accessory (iPhone) or above the content
/// (iPad, offline mode): cover, title and artist on one line each, play/pause,
/// next and a thin progress line. Tapping or swiping up opens the player.
struct MobileMiniPlayer: View {
    var model: AppModel
    var open: () -> Void
    @State private var playTaps = 0
    @State private var skipTaps = 0
    var body: some View {
        if let track = model.player.current {
            let requested = model.player.isPlaybackRequested
            HStack(spacing: 8) {
                Button(action: open) {
                    HStack(spacing: 10) {
                        ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 8).frame(width: 40, height: 40)
                        VStack(alignment: .leading, spacing: 1) {
                            Text(track.title).font(.subheadline.weight(.semibold)).lineLimit(1)
                            Text(model.player.loading ? "Wird geladen …" : track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        // The system bottom accessory has a compact height;
                        // the expanded player shows fully scaled metadata.
                        }.dynamicTypeSize(...DynamicTypeSize.xxLarge).frame(maxWidth: .infinity, alignment: .leading)
                    }.contentShape(Rectangle())
                }.buttonStyle(.plain)
                    .accessibilityLabel("Player öffnen: \(track.title), \(track.artistText)")
                    .accessibilityHint("Öffnet „Jetzt läuft“")
                    .accessibilityIdentifier("mobile-mini-player-open")
                Button { playTaps += 1; model.player.toggle() } label: {
                    Image(systemName: requested ? "pause.fill" : "play.fill").font(.system(size: 22, weight: .semibold))
                        .frame(width: PlayerMetrics.target, height: PlayerMetrics.target).contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel(requested ? "Pause" : "Abspielen")
                    .accessibilityIdentifier("mobile-mini-player-toggle")
                Button { skipTaps += 1; model.player.next() } label: {
                    Image(systemName: "forward.fill").font(.system(size: 20, weight: .semibold))
                        .frame(width: PlayerMetrics.target, height: PlayerMetrics.target).contentShape(Rectangle())
                }.buttonStyle(.plain).accessibilityLabel("Nächster Titel").accessibilityIdentifier("mobile-mini-player-next")
            }.padding(.horizontal, 12).padding(.vertical, 6)
                .overlay(alignment: .bottom) {
                    // Inset far enough to stay inside the accessory's capsule.
                    MiniProgressLine(player: model.player).padding(.horizontal, 24).padding(.bottom, 3)
                }
                // Swiping up opens the player, like tapping the title.
                .contentShape(Rectangle())
                .simultaneousGesture(DragGesture(minimumDistance: 16).onEnded { drag in
                    if drag.translation.height < -24 && abs(drag.translation.width) < abs(drag.translation.height) { open() }
                })
                .sensoryFeedback(.impact(weight: .medium), trigger: playTaps)
                .sensoryFeedback(.selection, trigger: skipTaps)
        }
    }
}

/// Two points of progress under the mini player, decoration only: the player
/// itself carries the position for VoiceOver.
private struct MiniProgressLine: View {
    var player: PlayerModel
    var body: some View {
        GeometryReader { proxy in
            let fraction = PlaybackClock.fraction(player.position, of: player.duration)
            ZStack(alignment: .leading) {
                Capsule().fill(Color.primary.opacity(0.12))
                Capsule().fill(Color.primary.opacity(0.6)).frame(width: proxy.size.width * fraction)
            }
        }.frame(height: 2).accessibilityHidden(true).allowsHitTesting(false)
    }
}

/// "Jetzt läuft" on iPhone and iPad: the cover is the one large element; a
/// calm column below holds title and artist, the scrubber, the transport,
/// volume and AirPlay, and a row for lyrics, sleep timer, sound tools and the
/// queue. At default text size everything fits without scrolling; larger text
/// shrinks the cover first and then scrolls.
struct MobilePlayerView: View {
    var model: AppModel
    /// Opens an artist or album page; the player closes for it. Nil hides the links.
    var openCollection: ((CollectionKind) -> Void)? = nil
    @State private var showsLyrics = false
    @State private var queueShown = false
    @State private var tools = false
    @State private var adding = false
    @State private var playlistSelection: [Track] = []
    @State private var linkMessage: String?
    @State private var resolvingLink = false
    @State private var playTaps = 0
    @State private var skipTaps = 0
    @State private var favoriteTaps = 0
    @AppStorage("mobileCoverColors") private var coverColors = true
    @AppStorage("mobileVisualizerPlacement") private var placement = "overlay"
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    // Active states use the app tint, not a cover colour: a dark cover made
    // "on" look dimmer than "off" (seen in the CI screenshots).
    var body: some View {
        GeometryReader { geometry in
            ScrollView {
                Group {
                    if let track = model.player.current { player(track, in: geometry.size) }
                    else {
                        ContentUnavailableView("Musik auswählen", systemImage: "play.circle", description: Text("Öffne ein Album oder eine Playlist."))
                    }
                }
                .frame(maxWidth: .infinity)
                .frame(minHeight: geometry.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollIndicators(.hidden)
        }
        .background { PlayerBackdrop(model: model, enabled: coverColors) }
        .navigationTitle("Jetzt läuft").navigationBarTitleDisplayMode(.inline)
        .toolbarBackground(.hidden, for: .navigationBar)
        .toolbar {
            if let track = model.player.current {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu { moreMenu(track) } label: { Label("Titel-Aktionen", systemImage: "ellipsis") }
                        .accessibilityIdentifier("mobile-player-more")
                }
            }
        }
        // While the queue is open it shows "Rückgängig" itself.
        .safeAreaInset(edge: .bottom, spacing: 0) { if !queueShown { QueueUndoBar(player: model.player) } }
        .sheet(isPresented: $queueShown) { MobileQueueSheet(model: model) }
        .sheet(isPresented: $tools) {
            NavigationStack { MobileAudioSettings(model: model).toolbar { ToolbarItem(placement: .confirmationAction) { Button("Fertig") { tools = false } } } }
        }
        .sheet(isPresented: $adding) { AddToPlaylistSheet(model: model, tracks: playlistSelection) }
        .alert("Nicht gefunden", isPresented: Binding(get: { linkMessage != nil }, set: { if !$0 { linkMessage = nil } })) {
            Button("OK") { linkMessage = nil }
        } message: { Text(linkMessage ?? "") }
        .sensoryFeedback(.impact(weight: .medium), trigger: playTaps)
        .sensoryFeedback(.selection, trigger: skipTaps)
        .sensoryFeedback(.impact(weight: .light), trigger: favoriteTaps)
    }

    // MARK: Layout

    private func player(_ track: Track, in size: CGSize) -> some View {
        let column = max(200, min(size.width - 2 * PlayerMetrics.margin, PlayerMetrics.maxColumn))
        let side = min(column, PlayerMetrics.maxCover, max(PlayerMetrics.minCover, size.height - PlayerMetrics.reserved(for: textSize)))
        return VStack(spacing: 0) {
            stage(track, side: side, column: column).padding(.top, 8)
            titleRow(track).padding(.top, 28)
            PlayerScrubber(player: model.player).padding(.top, 16)
            transport.padding(.top, 8)
            if let error = model.player.error {
                HStack(spacing: 12) {
                    Text(error).font(.footnote).foregroundStyle(.secondary).frame(maxWidth: .infinity, alignment: .leading)
                    Button("Erneut versuchen") { model.player.resume() }.font(.footnote.weight(.semibold)).frame(minHeight: PlayerMetrics.target)
                }.padding(.top, 4)
            }
            volumeRow.padding(.top, 12)
            toolRow.padding(.top, 12).padding(.bottom, 8)
        }
        .frame(width: column)
        .padding(.horizontal, PlayerMetrics.margin)
    }

    /// The cover, the visualizer without cover, or the lyrics in its place.
    @ViewBuilder private func stage(_ track: Track, side: CGFloat, column: CGFloat) -> some View {
        let playing = model.player.isPlaybackRequested
        if showsLyrics {
            MobileLyricsView(player: model.player, viewportHeight: side, compact: true)
                .frame(width: column, height: side)
                .transition(.opacity)
        } else if model.player.visualizationEnabled && placement == "background" {
            MobileSpectrum(player: model.player).frame(width: side, height: side)
                .allowsHitTesting(false).accessibilityHidden(true)
        } else {
            ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 0).frame(width: side, height: side)
                .overlay(alignment: .bottom) {
                    // On the cover the bars stay inside its rounded square.
                    if model.player.visualizationEnabled {
                        MobileSpectrum(player: model.player).frame(height: min(100, side * 0.32))
                            .padding(.horizontal, 12).padding(.bottom, 10)
                            .allowsHitTesting(false).accessibilityHidden(true)
                    }
                }
                .clipShape(RoundedRectangle(cornerRadius: PlayerMetrics.coverRadius, style: .continuous))
                .shadow(color: .black.opacity(scheme == .dark ? 0.45 : 0.2), radius: playing ? 24 : 12, y: playing ? 14 : 6)
                // The standout: the cover steps back while paused and springs
                // forward on play. Reduce Motion keeps it still.
                .scaleEffect(playing || reduceMotion ? 1 : PlayerMetrics.pausedCoverScale)
                .animation(reduceMotion ? nil : .spring(duration: 0.5, bounce: 0.25), value: playing)
                .frame(width: side, height: side)
                .accessibilityElement()
                .accessibilityLabel("Cover von \(track.title)")
                .accessibilityAddTraits(.isImage)
                .accessibilityIdentifier("mobile-player-cover")
        }
    }

    private func titleRow(_ track: Track) -> some View {
        HStack(alignment: .center, spacing: 12) {
            VStack(alignment: .leading, spacing: 4) {
                // Two lines at most, shrinking a little before the tail is cut.
                Text(track.title).font(.title2.bold()).lineLimit(2).minimumScaleFactor(0.8)
                    .accessibilityAddTraits(.isHeader)
                artistLink(track)
            }.frame(maxWidth: .infinity, alignment: .leading)
            if !model.offlineMode {
                let favorite = model.favoriteIDs.contains(track.id)
                Button { favoriteTaps += 1; Task { await model.toggleFavorite(track) } } label: {
                    Image(systemName: favorite ? "heart.fill" : "heart").font(.system(size: 22, weight: .semibold))
                        .foregroundStyle(favorite ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.primary))
                        .contentTransition(.symbolEffect(.replace))
                        .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: favorite)
                        .frame(width: PlayerMetrics.target, height: PlayerMetrics.target).contentShape(Rectangle())
                }.buttonStyle(ListenPressStyle())
                    .accessibilityLabel(favorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen")
                    .accessibilityIdentifier("mobile-player-favorite")
                    .disabled(model.pendingFavorites.contains(track.id))
            }
        }
    }

    /// The artist line opens the artist page; several artists offer a choice.
    @ViewBuilder private func artistLink(_ track: Track) -> some View {
        let text = Text(track.artistText.isEmpty ? "Unbekannter Künstler" : track.artistText)
            .font(.title3).foregroundStyle(.secondary).lineLimit(1)
        if openCollection != nil && !model.offlineMode && !track.artists.isEmpty {
            if track.artists.count == 1 {
                Button { openArtist(track.artists[0]) } label: { text.frame(minHeight: 28, alignment: .leading).contentShape(Rectangle()) }
                    .buttonStyle(.plain).disabled(resolvingLink)
                    .accessibilityHint("Öffnet die Künstlerseite")
                    .accessibilityIdentifier("mobile-player-artist")
            } else {
                Menu {
                    ForEach(track.artists, id: \.self) { name in Button(name, systemImage: "person") { openArtist(name) } }
                } label: { text.frame(minHeight: 28, alignment: .leading).contentShape(Rectangle()) }
                    .buttonStyle(.plain).disabled(resolvingLink)
                    .accessibilityHint("Wählt eine Künstlerseite")
                    .accessibilityIdentifier("mobile-player-artist")
            }
        } else { text }
    }

    private var transport: some View {
        let player = model.player, requested = player.isPlaybackRequested
        return HStack(spacing: 0) {
            PlayerModeButton(label: "Zufällige Wiedergabe", symbol: "shuffle", active: player.queue.isShuffled,
                             value: player.queue.isShuffled ? "Ein" : "Aus") { player.setShuffle(!player.queue.isShuffled) }
                .accessibilityIdentifier("mobile-player-shuffle")
            Spacer(minLength: 4)
            transportButton("backward.fill", label: "Vorheriger Titel", id: "mobile-player-previous") { skipTaps += 1; player.previous() }
            Spacer(minLength: 4)
            Button { playTaps += 1; player.toggle() } label: {
                ZStack {
                    if player.loading && requested { ProgressView().controlSize(.large) }
                    else {
                        Image(systemName: requested ? "pause.fill" : "play.fill").font(.system(size: 46, weight: .semibold))
                            .contentTransition(.symbolEffect(.replace))
                            .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: requested)
                    }
                }
                .frame(width: PlayerMetrics.playTarget, height: PlayerMetrics.playTarget).contentShape(Circle())
            }.buttonStyle(ListenPressStyle())
                .accessibilityLabel(requested ? "Pause" : "Abspielen")
                .accessibilityValue(player.loading ? "Wird geladen" : "")
                .accessibilityIdentifier("mobile-player-toggle")
            Spacer(minLength: 4)
            transportButton("forward.fill", label: "Nächster Titel", id: "mobile-player-next") { skipTaps += 1; player.next() }
            Spacer(minLength: 4)
            PlayerModeButton(label: "Wiederholen", symbol: player.repeatMode.symbol, active: player.repeatMode.isActive,
                             value: player.repeatMode.label) { player.setRepeat(player.repeatMode.next) }
                .accessibilityIdentifier("mobile-player-repeat")
        }.foregroundStyle(.primary)
    }
    private func transportButton(_ symbol: String, label: String, id: String, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 30, weight: .semibold))
                .frame(width: PlayerMetrics.skipTarget, height: PlayerMetrics.skipTarget).contentShape(Rectangle())
        }.buttonStyle(ListenPressStyle()).accessibilityLabel(label).accessibilityIdentifier(id)
    }

    private var volumeRow: some View {
        HStack(spacing: 10) {
            Image(systemName: "speaker.fill").font(.caption).foregroundStyle(.secondary).accessibilityHidden(true)
            PlayerVolumeSlider(player: model.player).frame(height: PlayerMetrics.target)
            Image(systemName: "speaker.wave.3.fill").font(.caption).foregroundStyle(.secondary).accessibilityHidden(true)
            AirPlayPicker().frame(width: PlayerMetrics.target, height: PlayerMetrics.target)
                .accessibilityLabel("Audioausgabe wählen").accessibilityIdentifier("mobile-player-airplay")
        }
    }

    /// Lyrics, sleep timer, sound tools and the queue: equal places, icons
    /// with a dot under the ones that are on.
    private var toolRow: some View {
        let sleeping = model.player.sleepMode != .off
        return HStack(spacing: 0) {
            PlayerModeButton(label: "Liedtext", symbol: showsLyrics ? "quote.bubble.fill" : "quote.bubble", active: showsLyrics,
                             value: showsLyrics ? "Eingeblendet" : "Ausgeblendet") {
                if reduceMotion { showsLyrics.toggle() } else { withAnimation(.easeOut(duration: 0.2)) { showsLyrics.toggle() } }
            }.accessibilityIdentifier("mobile-player-lyrics")
            Spacer(minLength: 0)
            Menu {
                Picker("Schlaf-Timer", selection: Binding(get: { model.player.sleepMode }, set: { model.player.setSleepMode($0) })) {
                    ForEach(SleepMode.allCases) { Text($0.name).tag($0) }
                }
            } label: {
                PlayerModeLabel(symbol: sleeping ? "moon.zzz.fill" : "moon.zzz", active: sleeping)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Schlaf-Timer")
            .accessibilityValue(sleepValue)
            .accessibilityIdentifier("mobile-player-sleep")
            Spacer(minLength: 0)
            PlayerModeButton(label: "Werkzeuge", symbol: "slider.horizontal.3", active: false, value: "") { tools = true }
                .accessibilityHint("Klang, Equalizer, Überblendung und Visualizer")
                .accessibilityIdentifier("mobile-player-tools")
            Spacer(minLength: 0)
            PlayerModeButton(label: "Warteschlange", symbol: "list.bullet", active: false,
                             value: "\(model.player.queue.remaining) Titel danach") { queueShown = true }
                .accessibilityIdentifier("mobile-player-queue")
        }
    }
    private var sleepValue: String {
        guard let deadline = model.player.sleepDeadline else { return model.player.sleepMode.name }
        return "Endet um " + deadline.formatted(date: .omitted, time: .shortened)
    }

    @ViewBuilder private func moreMenu(_ track: Track) -> some View {
        if openCollection != nil && !model.offlineMode {
            Section {
                ForEach(track.artists, id: \.self) { name in
                    Button(track.artists.count == 1 ? "Zum Künstler" : "Zu \(name)", systemImage: "person") { openArtist(name) }
                }
                if !track.album.isEmpty { Button("Zum Album", systemImage: "square.stack") { openAlbum(track) } }
            }
        }
        if !model.offlineMode {
            Section {
                Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; adding = true }
                Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
                Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }
                Button("Wiedergabe übergeben", systemImage: "arrow.up.forward.app") { Task { await model.saveHandoff() } }
            }
        }
        Section(audioDetails(track)) {
            Button("Klang & Wiedergabe", systemImage: "slider.horizontal.3") { tools = true }
        }
    }
    private func audioDetails(_ track: Track) -> String {
        var parts = [track.codec?.uppercased() ?? "Audio"]
        if !track.album.isEmpty && track.album != track.title { parts.insert(track.album, at: 0) }
        if model.offline.audioURL(track.id) != nil { parts.append("Offline") }
        return parts.joined(separator: " · ")
    }

    // MARK: Links

    private func openArtist(_ name: String) {
        guard let openCollection else { return }
        if let index = LibraryMatch.index(of: name, in: model.artists.map(\.name)) { openCollection(.artist(model.artists[index])); return }
        resolvingLink = true
        Task {
            defer { resolvingLink = false }
            let results = try? await model.search(name)
            if let results, let index = LibraryMatch.index(of: name, in: results.artists.map(\.name)) {
                openCollection(.artist(results.artists[index]))
            } else { linkMessage = "„\(name)“ ist in deiner Bibliothek nicht als Künstler zu finden." }
        }
    }
    private func openAlbum(_ track: Track) {
        guard let openCollection else { return }
        let names = track.artists + (track.albumArtist.map { [$0] } ?? [])
        func pick(_ list: [Release]) -> Release? {
            LibraryMatch.releaseIndex(title: track.album, artists: names, in: list.map { (title: $0.title, artists: $0.artists) }).map { list[$0] }
        }
        if let release = pick(model.releases) { openCollection(.release(release)); return }
        resolvingLink = true
        Task {
            defer { resolvingLink = false }
            if let results = try? await model.search(track.album), let release = pick(results.releases) { openCollection(.release(release)) }
            else { linkMessage = "Das Album „\(track.album)“ ist in deiner Bibliothek nicht zu finden." }
        }
    }
}

/// Behind the player: the cover, heavily blurred, under a veil of the
/// system background so text stays legible in light and dark mode. Without
/// cover colours, or before the cover loaded, the plain background.
private struct PlayerBackdrop: View {
    var model: AppModel
    var enabled: Bool
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        ZStack {
            Color(uiColor: .systemBackground)
            if enabled {
                if let image = model.player.artwork {
                    GeometryReader { proxy in
                        Image(decorative: image, scale: 1).resizable().scaledToFill()
                            .frame(width: proxy.size.width, height: proxy.size.height)
                            .blur(radius: 60, opaque: true)
                            .scaleEffect(1.3)
                            .clipped()
                    }.opacity(scheme == .dark ? 0.6 : 0.5)
                } else if let palette = model.player.artworkPalette {
                    Color(red: palette.dominant.red, green: palette.dominant.green, blue: palette.dominant.blue)
                        .opacity(scheme == .dark ? 0.35 : 0.18)
                }
                LinearGradient(colors: [Color(uiColor: .systemBackground).opacity(scheme == .dark ? 0.25 : 0.2),
                                        Color(uiColor: .systemBackground).opacity(scheme == .dark ? 0.6 : 0.55)],
                               startPoint: .top, endPoint: .bottom)
            }
        }
        .ignoresSafeArea()
        .animation(.easeOut(duration: 0.3), value: model.player.current?.id)
        .accessibilityHidden(true)
    }
}

/// An icon button for a mode or a tool: 44 pt, the icon in the accent and a
/// dot under it while on, so the state never rests on colour alone.
struct PlayerModeButton: View {
    var label: String
    var symbol: String
    var active: Bool
    var value: String
    var action: () -> Void
    var body: some View {
        Button(action: action) { PlayerModeLabel(symbol: symbol, active: active) }
            .buttonStyle(ListenPressStyle())
            .accessibilityLabel(label)
            .accessibilityValue(value)
            .accessibilityAddTraits(active ? .isSelected : [])
    }
}

struct PlayerModeLabel: View {
    var symbol: String
    var active: Bool
    var body: some View {
        VStack(spacing: 3) {
            Image(systemName: symbol).font(.system(size: 20, weight: .semibold))
            Circle().frame(width: 4, height: 4).opacity(active ? 1 : 0)
        }
        .foregroundStyle(active ? AnyShapeStyle(.tint) : AnyShapeStyle(Color.primary.opacity(0.65)))
        .frame(width: PlayerMetrics.target, height: PlayerMetrics.target)
        .contentShape(Rectangle())
    }
}

/// The position: a thin bar that grows while dragged. The time labels follow
/// the finger; the player seeks once, on release, so the bar never jumps back
/// to the old position while the seek runs.
struct PlayerScrubber: View {
    var player: PlayerModel
    @State private var dragFraction: Double?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        let duration = player.duration
        let dragging = dragFraction != nil
        let shown = dragFraction.map { PlaybackClock.position(atFraction: $0, of: duration) } ?? player.position
        let fraction = dragFraction ?? PlaybackClock.fraction(player.position, of: duration)
        VStack(spacing: 6) {
            GeometryReader { proxy in
                let height = dragging ? PlayerMetrics.scrubberActiveHeight : PlayerMetrics.scrubberHeight
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.primary.opacity(0.16))
                    Capsule().fill(Color.primary.opacity(dragging ? 0.95 : 0.7))
                        .frame(width: max(height, proxy.size.width * fraction))
                }
                .frame(height: height)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 0)
                    .onChanged { value in dragFraction = min(1, max(0, value.location.x / max(1, proxy.size.width))) }
                    .onEnded { value in
                        let end = min(1, max(0, value.location.x / max(1, proxy.size.width)))
                        player.seek(PlaybackClock.position(atFraction: end, of: duration))
                        dragFraction = nil
                    })
                .animation(reduceMotion ? nil : .easeOut(duration: 0.15), value: dragging)
            }
            .frame(height: PlayerMetrics.scrubberTouchHeight)
            .allowsHitTesting(duration > 0)
            .accessibilityElement()
            .accessibilityLabel("Wiedergabeposition")
            .accessibilityValue("\(formatTime(shown)) von \(formatTime(duration))")
            .accessibilityAdjustableAction { direction in
                switch direction {
                case .increment: player.seek(PlaybackClock.stepped(player.position, by: 10, of: duration))
                case .decrement: player.seek(PlaybackClock.stepped(player.position, by: -10, of: duration))
                @unknown default: break
                }
            }
            .accessibilityIdentifier("mobile-player-scrubber")
            HStack {
                Text(formatTime(shown)).accessibilityIdentifier("playbackElapsed")
                Spacer()
                Text("−" + formatTime(PlaybackClock.remaining(shown, of: duration)))
                    .accessibilityLabel("noch \(formatTime(PlaybackClock.remaining(shown, of: duration)))")
                    .accessibilityIdentifier("playbackRemaining")
            }
            .font(.caption.monospacedDigit().weight(dragging ? .semibold : .regular))
            .foregroundStyle(dragging ? .primary : .secondary)
        }
    }
}

/// The volume: the system slider on a device. The simulator draws no system
/// slider, so there the player's own volume stands in, which keeps the layout
/// the same in screenshots.
private struct PlayerVolumeSlider: View {
    var player: PlayerModel
    var body: some View {
        #if targetEnvironment(simulator)
        Slider(value: Binding(get: { player.volume }, set: { player.setVolume($0) }), in: 0...1)
            .tint(.primary)
            .accessibilityLabel("Lautstärke")
            .accessibilityIdentifier("mobile-player-volume")
        #else
        SystemVolumeSlider()
            .accessibilityLabel("Lautstärke")
            .accessibilityIdentifier("mobile-player-volume")
        #endif
    }
}

struct SystemVolumeSlider: UIViewRepresentable {
    func makeUIView(context: Context) -> MPVolumeView {
        let view = MPVolumeView()
        view.tintColor = .label
        return view
    }
    func updateUIView(_ uiView: MPVolumeView, context: Context) { }
}

// MARK: - Queue

/// The queue as a sheet over the player, half or full height.
struct MobileQueueSheet: View {
    var model: AppModel
    @Environment(\.dismiss) private var dismiss
    @State private var detent: PresentationDetent = .large
    var body: some View {
        NavigationStack {
            MobileQueueView(model: model)
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("Fertig") { dismiss() }.accessibilityIdentifier("mobile-queue-done")
                    }
                }
        }
        .presentationDetents([.medium, .large], selection: $detent)
        .presentationDragIndicator(.visible)
    }
}

/// "Warteschlange": shuffle, repeat and autoplay on top, then the sections
/// "Jetzt läuft", "Als Nächstes" (queued by hand), "Weiter aus: …" and
/// "Ähnliche Titel" (autoplay). Tap a row to jump there, drag to reorder
/// within a section, swipe to remove or to play next; the same actions sit in
/// each row's context menu and its "…" button.
struct MobileQueueView: View {
    var model: AppModel
    @State private var query = ""
    @State private var showsHistory = false
    @State private var editMode: EditMode = .inactive
    @State private var adding = false
    @State private var playlistSelection: [Track] = []
    @Environment(\.dynamicTypeSize) private var textSize

    /// A run of visible rows of one section, drawn as one list section.
    private struct Run: Identifiable {
        let section: QueueSection
        var rows: [QueueRow]
        var id: String { "\(section.rawValue)-\(rows.first?.position ?? -1)" }
    }
    private var runs: [Run] {
        let filtering = !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        var result: [Run] = []
        for row in model.player.queue.rows(filter: query) where showsHistory || filtering || row.section != .played {
            if let last = result.last, last.section == row.section, !row.startsSection { result[result.count - 1].rows.append(row) }
            else { result.append(Run(section: row.section, rows: [row])) }
        }
        return result
    }
    var body: some View {
        let queue = model.player.queue
        Group {
            if queue.tracks.isEmpty {
                ContentUnavailableView {
                    Label("Warteschlange ist leer", systemImage: "list.bullet")
                } description: {
                    Text("Spiele ein Album, eine Playlist oder einen Mix ab. Mit „Als Nächstes“ reihst du einzelne Titel hier ein.")
                }.accessibilityIdentifier("mobile-queue-empty")
            } else {
                list(queue)
            }
        }
        .navigationTitle("Warteschlange").navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if !queue.tracks.isEmpty {
                ToolbarItem(placement: .topBarLeading) { toolsMenu(queue) }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) { QueueUndoBar(player: model.player) }
        .sheet(isPresented: $adding) { AddToPlaylistSheet(model: model, tracks: playlistSelection) }
    }

    private func list(_ queue: PlaybackQueue) -> some View {
        let visible = runs
        let filtering = !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        return List {
            if !filtering && queue.count(of: .played) > 0 {
                Section {
                    Button(showsHistory ? "Zuvor gespielte ausblenden" : "Zuvor gespielt anzeigen (\(queue.count(of: .played)))",
                           systemImage: showsHistory ? "chevron.up" : "clock.arrow.circlepath") { showsHistory.toggle() }
                        .font(.subheadline).accessibilityIdentifier("mobile-queue-history")
                }
            }
            ForEach(visible) { run in
                Section {
                    ForEach(run.rows) { row in rowView(row, queue: queue) }
                        .onMove(perform: mover(for: run, filtering: filtering))
                } header: { header(run, queue: queue) }
            }
            if filtering && visible.isEmpty {
                ContentUnavailableView.search(text: query)
            } else if !filtering && queue.remaining == 0 {
                Section {
                    Text(model.player.autoplay ? "Danach sucht Autoplay ähnliche Titel aus deiner Bibliothek."
                         : "Danach ist Schluss. Schalte Autoplay ein, damit die Musik weiterläuft.")
                        .font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
        .listStyle(.insetGrouped)
        .environment(\.editMode, $editMode)
        .safeAreaInset(edge: .top, spacing: 0) { controls(queue) }
    }

    /// Shuffle, repeat and autoplay as labelled switches, and the filter.
    private func controls(_ queue: PlaybackQueue) -> some View {
        let player = model.player
        let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 8)) : AnyLayout(HStackLayout(spacing: 8))
        return VStack(spacing: 12) {
            layout {
                QueueModeChip(title: "Zufall", symbol: "shuffle", active: queue.isShuffled) { player.setShuffle(!queue.isShuffled) }
                    .accessibilityIdentifier("mobile-queue-shuffle")
                QueueModeChip(title: player.repeatMode == .one ? "Titel" : "Wiederholen", symbol: player.repeatMode.symbol,
                              active: player.repeatMode.isActive, value: player.repeatMode.label) { player.setRepeat(player.repeatMode.next) }
                    .accessibilityIdentifier("mobile-queue-repeat")
                QueueModeChip(title: "Autoplay", symbol: "infinity", active: player.autoplay) { player.setAutoplay(!player.autoplay) }
                    .accessibilityHint("Spielt am Ende der Warteschlange ähnliche Titel aus deiner Bibliothek.")
                    .accessibilityIdentifier("mobile-queue-autoplay")
            }
            HStack(spacing: 10) {
                Image(systemName: "magnifyingglass").foregroundStyle(.secondary).accessibilityHidden(true)
                TextField("Warteschlange filtern", text: $query).textInputAutocapitalization(.never).autocorrectionDisabled()
                    .accessibilityIdentifier("mobile-queue-filter")
                if !query.isEmpty {
                    Button { query = "" } label: { Image(systemName: "xmark.circle.fill").frame(width: PlayerMetrics.target, height: PlayerMetrics.target) }
                        .buttonStyle(.plain).foregroundStyle(.secondary).accessibilityLabel("Filter leeren")
                }
            }.padding(.leading, 14).padding(.trailing, query.isEmpty ? 14 : 0).frame(minHeight: PlayerMetrics.target)
                .background(Color(uiColor: .tertiarySystemFill), in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        }
        .padding(.horizontal, ListenMetrics.pageMargin).padding(.top, 8).padding(.bottom, 12)
        .background(.bar)
    }

    private func header(_ run: Run, queue: PlaybackQueue) -> some View {
        HStack(spacing: 6) {
            if run.section == .autoplay { Image(systemName: "infinity").accessibilityHidden(true) }
            Text(run.section == .current ? "Jetzt läuft" : run.section.heading(queue.context)).lineLimit(2)
            Spacer(minLength: 8)
            if run.section != .current { Text("\(queue.count(of: run.section))").monospacedDigit().foregroundStyle(.secondary) }
        }
        .font(.subheadline.weight(.semibold)).foregroundStyle(.primary).textCase(nil)
        .accessibilityElement(children: .combine).accessibilityAddTraits(.isHeader)
    }

    private func mover(for run: Run, filtering: Bool) -> ((IndexSet, Int) -> Void)? {
        guard !filtering, [QueueSection.user, .context, .autoplay].contains(run.section), run.rows.count > 1 else { return nil }
        let positions = run.rows.map(\.position)
        return { offsets, insertion in
            guard offsets.count == 1, let offset = offsets.first,
                  let move = PlaybackQueue.listMove(from: offset, to: insertion, positions: positions) else { return }
            model.player.moveInQueue(from: move.source, to: move.destination)
        }
    }

    private func rowView(_ row: QueueRow, queue: PlaybackQueue) -> some View {
        let position = row.position, track = row.track
        let isCurrent = position == queue.index
        return HStack(spacing: 12) {
            Button { if !isCurrent { model.player.select(position) } } label: {
                HStack(spacing: 12) {
                    ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 6)
                        .frame(width: PlayerMetrics.queueArtwork, height: PlayerMetrics.queueArtwork)
                        .overlay {
                            if isCurrent {
                                ZStack {
                                    RoundedRectangle(cornerRadius: 6, style: .continuous).fill(.black.opacity(0.4))
                                    PlayingIndicator(playing: model.player.isPlaying)
                                }
                            }
                        }
                    VStack(alignment: .leading, spacing: 2) {
                        Text(track.title).font(.body.weight(isCurrent ? .semibold : .regular))
                            .foregroundStyle(isCurrent ? AnyShapeStyle(.tint) : AnyShapeStyle(.primary)).lineLimit(1)
                        Text(track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading).layoutPriority(1)
                    if !textSize.isAccessibilitySize {
                        Text(formatTime(track.duration)).font(.caption.monospacedDigit()).foregroundStyle(.secondary)
                    }
                }.contentShape(Rectangle())
            }.buttonStyle(.plain)
                .accessibilityValue(isCurrent ? "Läuft gerade" : "")
                .accessibilityHint(isCurrent ? "" : "Spielt diesen Titel")
                .accessibilityIdentifier("mobile-queue-track-\(position)")
            Menu { actions(row, queue: queue) } label: {
                Image(systemName: "ellipsis").frame(width: PlayerMetrics.target, height: PlayerMetrics.target).contentShape(Rectangle())
            }.fixedSize()
                .accessibilityLabel("Warteschlangen-Aktionen für \(track.title)")
                .accessibilityIdentifier("mobile-queue-actions-\(position)")
        }
        .opacity(row.section == .played ? 0.6 : 1)
        .listRowBackground(isCurrent ? Color.accentColor.opacity(0.12) : nil)
        .moveDisabled(!queue.isUpcoming(position))
        .contextMenu { actions(row, queue: queue) }
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            if !isCurrent {
                Button(role: .destructive) { model.player.removeFromQueue(position) } label: { Label("Entfernen", systemImage: "minus.circle") }
            }
        }
        .swipeActions(edge: .leading, allowsFullSwipe: true) {
            if queue.canPlayNext(position) {
                Button { model.player.playNextInQueue(position) } label: { Label("Als Nächstes", systemImage: "text.insert") }.tint(.indigo)
            }
        }
    }

    @ViewBuilder private func actions(_ row: QueueRow, queue: PlaybackQueue) -> some View {
        let position = row.position, track = row.track
        Button("Als Nächstes spielen", systemImage: "text.insert") { model.player.playNextInQueue(position) }
            .disabled(!queue.canPlayNext(position))
        Button("Nach oben", systemImage: "arrow.up") { model.player.moveInQueue(from: position, to: position - 1) }
            .disabled(!queue.canMoveUp(position))
        Button("Nach unten", systemImage: "arrow.down") { model.player.moveInQueue(from: position, to: position + 1) }
            .disabled(!queue.canMoveDown(position))
        if !model.offlineMode {
            Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; adding = true }
            Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen", systemImage: "heart") {
                Task { await model.toggleFavorite(track) }
            }.disabled(model.pendingFavorites.contains(track.id))
            Button("Offline speichern", systemImage: "arrow.down.circle") { model.offline.enqueue([track]) }
        }
        Button("Aus Warteschlange entfernen", systemImage: "minus.circle", role: .destructive) { model.player.removeFromQueue(position) }
            .disabled(position == queue.index)
    }

    @ViewBuilder private func toolsMenu(_ queue: PlaybackQueue) -> some View {
        Menu {
            Button(editMode.isEditing ? "Sortieren beenden" : "Reihenfolge ändern", systemImage: "arrow.up.arrow.down") {
                editMode = editMode.isEditing ? .inactive : .active
            }.disabled(queue.remaining < 2)
            if !model.offlineMode {
                Button("Als Playlist speichern", systemImage: "music.note.list") { playlistSelection = queue.tracks; adding = true }
            }
            Button("Nächste leeren", systemImage: "text.badge.minus") { model.player.clearUpcoming() }
                .disabled(queue.remaining == 0)
            Button("Leeren und stoppen", systemImage: "trash", role: .destructive) { model.player.clearQueue() }
        } label: { Label("Warteschlangen-Aktionen", systemImage: "ellipsis.circle") }
            .accessibilityIdentifier("mobile-queue-tools")
    }
}

/// A labelled switch in the queue's header: filled with the tint while on.
private struct QueueModeChip: View {
    var title: String
    var symbol: String
    var active: Bool
    var value: String? = nil
    var action: () -> Void
    var body: some View {
        Button(action: action) {
            Label(title, systemImage: symbol)
                .font(.footnote.weight(.semibold)).lineLimit(1).minimumScaleFactor(0.75)
                .padding(.horizontal, 6)
                .frame(maxWidth: .infinity, minHeight: PlayerMetrics.target)
                .foregroundStyle(active ? Color.white : Color.primary)
                .background(active ? AnyShapeStyle(.tint) : AnyShapeStyle(Color(uiColor: .tertiarySystemFill)), in: Capsule())
                .contentShape(Capsule())
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityLabel(title)
        .accessibilityValue(value ?? (active ? "Ein" : "Aus"))
        .accessibilityAddTraits(active ? .isSelected : [])
    }
}

/// Three bars that move while the title plays and rest while paused or with
/// Reduce Motion.
struct PlayingIndicator: View {
    var playing: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private static let resting: [CGFloat] = [0.55, 1, 0.75]
    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 20, paused: !playing || reduceMotion)) { context in
            let time = context.date.timeIntervalSinceReferenceDate
            HStack(alignment: .bottom, spacing: 2) {
                ForEach(0..<3, id: \.self) { bar in
                    let moving = 0.3 + 0.7 * abs(sin(time * (4.2 + Double(bar) * 1.3) + Double(bar)))
                    Capsule().frame(width: 3, height: 14 * (playing && !reduceMotion ? CGFloat(moving) : Self.resting[bar]))
                }
            }.frame(width: 14, height: 14, alignment: .bottom)
        }
        .foregroundStyle(.white)
        .accessibilityHidden(true)
    }
}
#endif
