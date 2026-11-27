#if os(tvOS)
import SwiftUI
import YTMDLCore

// "Jetzt läuft" on Apple TV: a full-screen, artwork-led player for the
// ten-foot view, and the bar every screen shows while something is loaded.
// The cover is the one large element; title, scrubber and transport sit
// beside it on a blurred, darkened copy of the cover. Queue and lyrics are
// panels of the same screen, the Menu button steps back out of them.

/// Colours and timing of the Apple TV screens.
enum TVTheme {
    /// The app's rose accent, lifted for dark backgrounds like on iPhone.
    static let accent = DesktopTheme.rose.accent(.dark)
    /// Behind the blurred cover; the app's dark rose background.
    static let base = DesktopTheme.rose.background(.dark)
    /// Seconds without input before the controls fade (while playing).
    static let restDelay: Double = 10
}

// MARK: - Button styles

/// A round control: translucent at rest, white with a dark glyph and lifted
/// when focused. `active` (shuffle, repeat, a running timer) tints the glyph
/// and adds a dot, so the state never rests on colour alone.
struct TVRoundControlStyle: ButtonStyle {
    var diameter: CGFloat
    var glyph: CGFloat
    var prominent = false
    var active = false
    func makeBody(configuration: Configuration) -> some View {
        TVRoundControlBody(configuration: configuration, diameter: diameter, glyph: glyph, prominent: prominent, active: active)
    }
}
private struct TVRoundControlBody: View {
    let configuration: ButtonStyleConfiguration
    let diameter: CGFloat
    let glyph: CGFloat
    let prominent: Bool
    let active: Bool
    @Environment(\.isFocused) private var focused
    @Environment(\.isEnabled) private var enabled
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        configuration.label
            .font(.system(size: glyph, weight: .semibold))
            .foregroundStyle(focused ? Color.black : active ? TVTheme.accent : Color.white)
            .frame(width: diameter, height: diameter)
            .background(Circle().fill(focused ? Color.white : Color.white.opacity(prominent ? 0.2 : 0.1)))
            .overlay(alignment: .bottom) {
                if active {
                    Circle().fill(focused ? Color.black : TVTheme.accent).frame(width: 7, height: 7)
                        .padding(.bottom, diameter * 0.12)
                }
            }
            .scaleEffect(configuration.isPressed ? 0.94 : focused ? 1.12 : 1)
            .shadow(color: .black.opacity(focused ? 0.4 : 0), radius: 20, y: 12)
            .opacity(enabled ? 1 : 0.35)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.16), value: focused)
            .animation(.easeOut(duration: 0.1), value: configuration.isPressed)
    }
}

/// A text button in a capsule ("Warteschlange", "Lyrics", "Rückgängig").
struct TVPillStyle: ButtonStyle {
    var selected = false
    func makeBody(configuration: Configuration) -> some View {
        TVPillBody(configuration: configuration, selected: selected)
    }
}
private struct TVPillBody: View {
    let configuration: ButtonStyleConfiguration
    let selected: Bool
    @Environment(\.isFocused) private var focused
    @Environment(\.isEnabled) private var enabled
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        configuration.label
            .font(.system(size: 26, weight: .semibold))
            .lineLimit(1)
            .foregroundStyle(focused ? Color.black : Color.white.opacity(0.9))
            .padding(.horizontal, 30)
            .frame(height: 66)
            .background(Capsule().fill(focused ? Color.white : Color.white.opacity(selected ? 0.22 : 0.1)))
            .scaleEffect(configuration.isPressed ? 0.96 : focused ? 1.08 : 1)
            .shadow(color: .black.opacity(focused ? 0.35 : 0), radius: 16, y: 10)
            .opacity(enabled ? 1 : 0.35)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.16), value: focused)
    }
}

/// The artist and album lines: plain text until focused, then a white
/// capsule. The padding never changes, so focusing does not move the text.
struct TVLinkStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View { TVLinkBody(configuration: configuration) }
}
private struct TVLinkBody: View {
    let configuration: ButtonStyleConfiguration
    @Environment(\.isFocused) private var focused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        configuration.label
            .foregroundStyle(focused ? Color.black : Color.white.opacity(0.72))
            .padding(.horizontal, 18).padding(.vertical, 6)
            .background(Capsule().fill(focused ? Color.white : Color.clear))
            .scaleEffect(focused ? 1.04 : 1, anchor: .leading)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.16), value: focused)
    }
}

/// The bar of the other screens: glass at rest, white when focused.
private struct TVBarStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View { TVBarBody(configuration: configuration) }
}
private struct TVBarBody: View {
    let configuration: ButtonStyleConfiguration
    @Environment(\.isFocused) private var focused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        configuration.label
            .environment(\.tvBarFocused, focused)
            .foregroundStyle(focused ? Color.black : Color.white)
            .background {
                RoundedRectangle(cornerRadius: 28, style: .continuous)
                    .fill(focused ? AnyShapeStyle(Color.white) : AnyShapeStyle(.regularMaterial))
                    .overlay(RoundedRectangle(cornerRadius: 28, style: .continuous).strokeBorder(.white.opacity(focused ? 0 : 0.12), lineWidth: 1))
            }
            .scaleEffect(configuration.isPressed ? 0.97 : focused ? 1.05 : 1)
            .shadow(color: .black.opacity(focused ? 0.45 : 0.25), radius: focused ? 28 : 14, y: focused ? 16 : 8)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: focused)
    }
}
private struct TVBarFocusedKey: EnvironmentKey { static let defaultValue = false }
extension EnvironmentValues {
    /// Whether the now-playing bar that contains this view has the focus.
    var tvBarFocused: Bool {
        get { self[TVBarFocusedKey.self] }
        set { self[TVBarFocusedKey.self] = newValue }
    }
}

/// One line of lyrics: dimmed, white when it is the sung line or focused.
private struct TVLyricStyle: ButtonStyle {
    var active: Bool
    var dimmed: Bool
    func makeBody(configuration: Configuration) -> some View { TVLyricBody(configuration: configuration, active: active, dimmed: dimmed) }
}
private struct TVLyricBody: View {
    let configuration: ButtonStyleConfiguration
    let active: Bool
    let dimmed: Bool
    @Environment(\.isFocused) private var focused
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        configuration.label
            .foregroundStyle(active || focused || !dimmed ? Color.white : Color.white.opacity(0.42))
            .padding(.horizontal, 24).padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Color.white.opacity(focused ? 0.12 : 0)))
            .scaleEffect(focused ? 1.02 : 1, anchor: .leading)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.2), value: focused)
            .animation(reduceMotion ? nil : .easeOut(duration: 0.3), value: active)
    }
}

// MARK: - Backdrop

/// The cover, blurred and darkened over the app's dark background; the
/// cover's main colour while the image is not there yet.
private struct TVNowPlayingBackdrop: View {
    var player: PlayerModel
    var body: some View {
        ZStack {
            TVTheme.base
            if let image = player.artwork {
                Color.clear.overlay {
                    Image(decorative: image, scale: 1).resizable().scaledToFill()
                        .blur(radius: 90, opaque: true)
                        .scaleEffect(1.25)
                        .saturation(1.2)
                }.clipped().opacity(0.7)
            } else if let palette = player.artworkPalette {
                Color(red: palette.dominant.red, green: palette.dominant.green, blue: palette.dominant.blue).opacity(0.35)
            }
            LinearGradient(colors: [.black.opacity(0.2), .black.opacity(0.62)], startPoint: .topTrailing, endPoint: .bottomLeading)
        }
        .ignoresSafeArea()
        .animation(.easeOut(duration: 0.4), value: player.current?.id)
        .accessibilityHidden(true)
    }
}

// MARK: - Scrubber

/// The playback position as a bar: thin while resting, thicker with a knob
/// and in the accent while it has the focus (left and right then seek).
private struct TVScrubberBar: View {
    var fraction: Double
    var focused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        GeometryReader { proxy in
            let width = proxy.size.width
            let height: CGFloat = focused ? 14 : 8
            let filled = max(height, width * fraction)
            ZStack(alignment: .leading) {
                Capsule().fill(Color.white.opacity(0.22)).frame(height: height)
                Capsule().fill(focused ? TVTheme.accent : Color.white).frame(width: filled, height: height)
                if focused {
                    Circle().fill(Color.white).frame(width: 32, height: 32)
                        .shadow(color: .black.opacity(0.35), radius: 8, y: 4)
                        .offset(x: min(max(0, width * fraction - 16), width - 32))
                }
            }
            .frame(width: width, height: proxy.size.height, alignment: .leading)
        }
        .frame(height: 36)
        .animation(reduceMotion ? nil : .easeOut(duration: 0.18), value: focused)
    }
}

// MARK: - Now Playing

private enum TVNowPlayingFocus: Hashable {
    case artist, album, scrubber
    case shuffle, previous, playPause, next, repeatMode, favorite
    case queue, lyrics, sleep, speed, radio, less, more, notice
    case wake
}
private enum TVNowPlayingPanel: Hashable { case queue, lyrics }
private enum TVNowPlayingSheet: String, Identifiable {
    case sleep, speed, playlist
    var id: String { rawValue }
}

/// "Jetzt läuft" on Apple TV, shown full screen over the tabs.
///
/// - Left and right on the focused scrubber seek; quick repeated presses
///   (or holding the edge of the touch surface) move further each time.
/// - "Warteschlange" and "Lyrics" open panels beside a smaller cover; the
///   Menu button closes a panel, then Now Playing. Playback continues.
/// - After `TVTheme.restDelay` seconds without input while playing, the
///   controls fade and cover, title and progress remain; any input, the
///   Menu button included, brings them back.
struct TVNowPlayingView: View {
    var model: AppModel
    /// Opens an artist or album page in the shown tab (and closes this).
    var openCollection: (CollectionKind) -> Void
    @Environment(\.dismiss) private var dismiss
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @FocusState private var focus: TVNowPlayingFocus?
    @State private var restoreFocus: TVNowPlayingFocus = .playPause
    @State private var panel: TVNowPlayingPanel?
    @State private var resting = false
    @State private var inputRevision = 0
    @State private var sheet: TVNowPlayingSheet?
    @State private var moreActions = false
    @State private var scrubStreak = 0
    @State private var lastScrub: (direction: MoveCommandDirection, at: Date)?
    private var player: PlayerModel { model.player }
    private struct RestKey: Hashable { let input: Int; let playing: Bool; let panel: Bool; let resting: Bool; let sheet: Bool }
    private var restKey: RestKey {
        RestKey(input: inputRevision, playing: player.isPlaybackRequested, panel: panel != nil, resting: resting, sheet: sheet != nil || moreActions)
    }
    private var motion: Animation? { reduceMotion ? nil : .easeInOut(duration: 0.35) }

    var body: some View {
        ZStack {
            TVNowPlayingBackdrop(player: player)
            wakeCatcher
            if let track = player.current {
                HStack(alignment: .center, spacing: panel == nil ? 96 : 80) {
                    coverColumn(track)
                    Group {
                        switch panel {
                        case .none: controlsColumn(track)
                        case .queue?: queuePanel
                        case .lyrics?: lyricsPanel
                        }
                    }
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: panel == nil ? .leading : .topLeading)
                }
                .padding(.horizontal, 40)
            } else {
                ContentUnavailableView("Nichts in der Wiedergabe", systemImage: "play.circle",
                                       description: Text("Wähle einen Titel, ein Album oder einen Mix, um loszuhören."))
            }
        }
        .defaultFocus($focus, .playPause)
        .onPlayPauseCommand { player.remoteToggle(); touch() }
        .onExitCommand { back() }
        .onChange(of: focus) { _, new in
            guard let new, new != .wake else { return }
            restoreFocus = new
            touch()
        }
        .onChange(of: player.current == nil) { _, empty in if empty { dismiss() } }
        .task(id: restKey) {
            guard !restKey.resting, !restKey.panel, !restKey.sheet, restKey.playing else { return }
            try? await Task.sleep(for: .seconds(TVTheme.restDelay))
            guard !Task.isCancelled else { return }
            rest()
        }
        .sheet(item: $sheet) { item in sheetContent(item) }
        // The page around is tinted white; the dialog buttons need the accent
        // or their labels vanish on the white focus platter.
        .background {
            Color.clear
                .confirmationDialog("Weitere Aktionen", isPresented: $moreActions, titleVisibility: .visible) { moreDialog }
                .alert("YTMDL", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) {
                    Button("OK") { model.error = nil }
                } message: { Text(model.error ?? "") }
                .tint(TVTheme.accent)
        }
    }

    // MARK: Input and resting

    private func touch() { inputRevision &+= 1 }
    private func act(_ action: () -> Void) { action(); touch() }
    private func rest() {
        withAnimation(reduceMotion ? .easeOut(duration: 0.2) : .easeInOut(duration: 0.8)) { resting = true }
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(120))
            if resting { focus = .wake }
        }
    }
    private func wake() {
        guard resting else { touch(); return }
        withAnimation(reduceMotion ? .easeOut(duration: 0.15) : .easeOut(duration: 0.3)) { resting = false }
        let target = restoreFocus
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(80))
            focus = target
        }
        touch()
    }
    /// Menu: wake from rest, close a panel, or leave Now Playing.
    private func back() {
        if resting { wake() }
        else if panel != nil { closePanel() }
        else { dismiss() }
    }
    private func open(_ next: TVNowPlayingPanel) {
        withAnimation(motion) { panel = next }
        touch()
    }
    private func closePanel() {
        let was = panel
        withAnimation(motion) { panel = nil }
        let target: TVNowPlayingFocus = was == .lyrics ? .lyrics : .queue
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(80))
            focus = target
        }
        touch()
    }

    /// Covers the screen without being seen. Only focusable while resting:
    /// the controls are disabled then, so the focus lands here and the first
    /// press only wakes them instead of pressing a hidden button.
    private var wakeCatcher: some View {
        Button { wake() } label: { Color.clear.frame(maxWidth: .infinity, maxHeight: .infinity) }
            .buttonStyle(TVInvisibleStyle())
            .focused($focus, equals: .wake)
            .disabled(!resting)
            .onMoveCommand { _ in wake() }
            .accessibilityLabel("Steuerung einblenden")
            .accessibilityHidden(!resting)
            .accessibilityIdentifier("tv-np-wake")
    }

    // MARK: Cover column

    @ViewBuilder private func coverColumn(_ track: Track) -> some View {
        let side: CGFloat = panel == nil ? 620 : 420
        VStack(alignment: .leading, spacing: 0) {
            ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: panel == nil ? 30 : 24)
                .frame(width: side, height: side)
                .shadow(color: .black.opacity(0.5), radius: 44, y: 26)
            if panel != nil {
                // Beside a panel: what plays, and where it stands.
                VStack(alignment: .leading, spacing: 10) {
                    Text(track.title).font(.system(size: 38, weight: .bold)).lineLimit(2).foregroundStyle(.white)
                    Text(track.artistText).font(.system(size: 28, weight: .medium)).lineLimit(1).foregroundStyle(.white.opacity(0.7))
                    progress(compact: true)
                }
                .frame(width: side, alignment: .leading)
                .padding(.top, 36)
            }
        }
        .frame(width: side)
    }

    // MARK: Controls column

    @ViewBuilder private func controlsColumn(_ track: Track) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            source
            Text(track.title)
                .font(.system(size: 66, weight: .bold))
                .tracking(-0.5)
                .lineLimit(2)
                .minimumScaleFactor(0.75)
                .foregroundStyle(.white)
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
                .accessibilityIdentifier("tv-np-title")
            links(track).padding(.top, 10)
            progress(compact: false).padding(.top, 44)
            controls(track).padding(.top, 36)
                .opacity(resting ? 0 : 1)
                .disabled(resting)
                .accessibilityHidden(resting)
        }
        .frame(maxWidth: 1040, alignment: .leading)
    }

    /// Where the queue plays from, like "Aus: …" on iPhone.
    @ViewBuilder private var source: some View {
        if let context = player.queue.context {
            Label(context.kind == "radio" ? context.title : "Aus: \(context.title)",
                  systemImage: context.kind == "radio" ? "dot.radiowaves.left.and.right" : "play.square.stack")
                .font(.system(size: 24, weight: .semibold))
                .foregroundStyle(TVTheme.accent)
                .lineLimit(1)
                .padding(.bottom, 14)
                .accessibilityIdentifier("tv-np-source")
        }
    }

    /// Artist and album; links to their pages when the library knows them.
    @ViewBuilder private func links(_ track: Track) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            if let artist = model.artistReference(for: track), !model.offlineMode {
                Button { openCollection(.artist(Artist(id: artist.id, name: artist.name))) } label: {
                    HStack(spacing: 12) {
                        Text(track.artistText).lineLimit(1)
                        Image(systemName: "chevron.forward").font(.system(size: 22, weight: .bold)).opacity(focus == .artist ? 1 : 0)
                    }.font(.system(size: 36, weight: .semibold))
                }
                .buttonStyle(TVLinkStyle())
                .focused($focus, equals: .artist)
                .disabled(resting)
                .accessibilityLabel("Künstler: \(track.artistText)")
                .accessibilityHint("Öffnet die Künstlerseite")
                .accessibilityIdentifier("tv-np-artist")
            } else {
                Text(track.artistText).font(.system(size: 36, weight: .semibold)).foregroundStyle(.white.opacity(0.72))
                    .lineLimit(1).padding(.horizontal, 18).padding(.vertical, 6)
            }
            if !track.album.isEmpty && track.album != track.title {
                if let release = track.releaseId, !model.offlineMode {
                    Button { openCollection(.release(Release(id: release, title: track.album, artists: track.artists))) } label: {
                        HStack(spacing: 10) {
                            Text(track.album).lineLimit(1)
                            Image(systemName: "chevron.forward").font(.system(size: 18, weight: .bold)).opacity(focus == .album ? 1 : 0)
                        }.font(.system(size: 28, weight: .medium))
                    }
                    .buttonStyle(TVLinkStyle())
                    .focused($focus, equals: .album)
                    .disabled(resting)
                    .accessibilityLabel("Album: \(track.album)")
                    .accessibilityHint("Öffnet die Albumseite")
                    .accessibilityIdentifier("tv-np-album")
                } else {
                    Text(track.album).font(.system(size: 28, weight: .medium)).foregroundStyle(.white.opacity(0.55))
                        .lineLimit(1).padding(.horizontal, 18).padding(.vertical, 6)
                }
            }
        }
        .padding(.leading, -18)
    }

    /// The scrubber with elapsed and remaining time. Focusable on the full
    /// player only; beside a panel it is a plain progress line.
    @ViewBuilder private func progress(compact: Bool) -> some View {
        let duration = player.duration
        let position = min(player.position, duration > 0 ? duration : player.position)
        let fraction = PlaybackClock.fraction(position, of: duration)
        VStack(spacing: compact ? 8 : 12) {
            if compact {
                TVScrubberBar(fraction: fraction, focused: false).accessibilityHidden(true)
            } else {
                TVScrubberBar(fraction: fraction, focused: focus == .scrubber)
                    .focusable(!resting)
                    .focused($focus, equals: .scrubber)
                    .focusEffectDisabled()
                    .onMoveCommand { scrub($0) }
                    .accessibilityElement()
                    .accessibilityLabel("Wiedergabeposition")
                    .accessibilityValue("\(formatTime(position)) von \(formatTime(duration))")
                    .accessibilityHint("Nach links oder rechts zum Spulen")
                    .accessibilityAdjustableAction { direction in
                        switch direction {
                        case .increment: scrub(.right)
                        case .decrement: scrub(.left)
                        @unknown default: break
                        }
                    }
                    .accessibilityIdentifier("tv-np-scrubber")
            }
            HStack {
                Text(formatTime(position)).accessibilityIdentifier("playbackElapsed")
                Spacer()
                Text("−" + formatTime(PlaybackClock.remaining(position, of: duration))).accessibilityLabel("Noch \(formatTime(PlaybackClock.remaining(position, of: duration)))")
            }
            .font(.system(size: compact ? 22 : 26, weight: .medium).monospacedDigit())
            .foregroundStyle(.white.opacity(0.62))
        }
    }

    /// One press of left or right: 10 s, further on quick repeated presses.
    private func scrub(_ direction: MoveCommandDirection) {
        guard direction == .left || direction == .right else { return }
        let now = Date()
        let interval = lastScrub.map { now.timeIntervalSince($0.at) } ?? .infinity
        scrubStreak = ScrubStep.streak(after: scrubStreak, sameDirection: lastScrub?.direction == direction, interval: interval)
        lastScrub = (direction, now)
        let step = ScrubStep.seconds(streak: scrubStreak) * (direction == .right ? 1 : -1)
        player.seek(PlaybackClock.stepped(player.position, by: step, of: player.duration))
        touch()
    }

    // MARK: Transport and secondary actions

    @ViewBuilder private func controls(_ track: Track) -> some View {
        VStack(alignment: .leading, spacing: 34) {
            HStack(spacing: 34) {
                Button { act { player.setShuffle(!player.queue.isShuffled) } } label: { Image(systemName: "shuffle") }
                    .buttonStyle(TVRoundControlStyle(diameter: 86, glyph: 30, active: player.queue.isShuffled))
                    .focused($focus, equals: .shuffle)
                    .accessibilityLabel(player.queue.isShuffled ? "Zufall aus" : "Zufall an")
                    .accessibilityIdentifier("tv-np-shuffle")
                Button { act { player.previous() } } label: { Image(systemName: "backward.fill") }
                    .buttonStyle(TVRoundControlStyle(diameter: 104, glyph: 38))
                    .focused($focus, equals: .previous)
                    .accessibilityLabel("Vorheriger Titel")
                    .accessibilityIdentifier("tv-np-previous")
                Button { act { player.toggle() } } label: {
                    Image(systemName: player.isPlaybackRequested ? "pause.fill" : "play.fill")
                        .contentTransition(.symbolEffect(.replace))
                }
                .buttonStyle(TVRoundControlStyle(diameter: 136, glyph: 54, prominent: true))
                .focused($focus, equals: .playPause)
                .accessibilityLabel(player.isPlaybackRequested ? "Pause" : "Abspielen")
                .accessibilityIdentifier("tv-np-toggle")
                Button { act { player.next() } } label: { Image(systemName: "forward.fill") }
                    .buttonStyle(TVRoundControlStyle(diameter: 104, glyph: 38))
                    .focused($focus, equals: .next)
                    .accessibilityLabel("Nächster Titel")
                    .accessibilityIdentifier("tv-np-next")
                Button { act { player.setRepeat(player.repeatMode.next) } } label: { Image(systemName: player.repeatMode.symbol) }
                    .buttonStyle(TVRoundControlStyle(diameter: 86, glyph: 30, active: player.repeatMode.isActive))
                    .focused($focus, equals: .repeatMode)
                    .accessibilityLabel(player.repeatMode.label)
                    .accessibilityIdentifier("tv-player-repeat")
                Spacer(minLength: 24)
                let favorite = model.favoriteIDs.contains(track.id)
                Button { Task { await model.toggleFavorite(track) }; touch() } label: { Image(systemName: favorite ? "heart.fill" : "heart") }
                    .buttonStyle(TVRoundControlStyle(diameter: 86, glyph: 30, active: favorite))
                    .focused($focus, equals: .favorite)
                    .disabled(model.offlineMode || model.pendingFavorites.contains(track.id))
                    .accessibilityLabel(favorite ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen")
                    .accessibilityIdentifier("tv-np-favorite")
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            secondary(track)
            footnote(track)
        }
        .padding(.leading, 6)
    }

    @ViewBuilder private func secondary(_ track: Track) -> some View {
        HStack(spacing: 18) {
            Button { open(.queue) } label: { Label("Warteschlange", systemImage: "list.bullet") }
                .buttonStyle(TVPillStyle())
                .focused($focus, equals: .queue)
                .accessibilityHint("Zeigt, was als Nächstes läuft")
                .accessibilityIdentifier("tv-np-queue")
            Button { open(.lyrics) } label: { Label("Lyrics", systemImage: "quote.bubble") }
                .buttonStyle(TVPillStyle())
                .focused($focus, equals: .lyrics)
                .accessibilityIdentifier("tv-np-lyrics")
            Rectangle().fill(Color.white.opacity(0.18)).frame(width: 2, height: 40).padding(.horizontal, 8).accessibilityHidden(true)
            Button { sheet = .sleep; touch() } label: { Image(systemName: "moon.zzz") }
                .buttonStyle(TVRoundControlStyle(diameter: 66, glyph: 24, active: player.sleepMode != .off))
                .focused($focus, equals: .sleep)
                .accessibilityLabel("Sleep-Timer")
                .accessibilityValue(player.sleepMode.name)
                .accessibilityIdentifier("tv-player-sleep")
            Button { sheet = .speed; touch() } label: { Image(systemName: "gauge.with.dots.needle.50percent") }
                .buttonStyle(TVRoundControlStyle(diameter: 66, glyph: 24, active: player.playbackRate != 1))
                .focused($focus, equals: .speed)
                .accessibilityLabel("Tempo")
                .accessibilityValue(TVSoundSettings.rate(player.playbackRate))
                .accessibilityIdentifier("tv-player-speed")
            if !model.offlineMode {
                Button { Task { await model.startRadio(track) }; touch() } label: { Image(systemName: "dot.radiowaves.left.and.right") }
                    .buttonStyle(TVRoundControlStyle(diameter: 66, glyph: 24))
                    .focused($focus, equals: .radio)
                    .disabled(model.listeningBusy)
                    .accessibilityLabel("Song-Radio starten")
                    .accessibilityIdentifier("tv-np-radio")
                if model.serverFeatures.preferences {
                    let excluded = model.preferences.contains(.track, track.id)
                    Button {
                        Task {
                            if excluded { await model.allow(.track, target: track.id, name: track.title) }
                            else { await model.lessOfTrack(track, fromPlayer: true) }
                        }
                        touch()
                    } label: { Image(systemName: excluded ? "hand.thumbsup" : "hand.thumbsdown") }
                        .buttonStyle(TVRoundControlStyle(diameter: 66, glyph: 24, active: excluded))
                        .focused($focus, equals: .less)
                        .accessibilityLabel(excluded ? "Wieder vorschlagen" : "Weniger davon")
                        .accessibilityIdentifier("tv-np-less")
                }
                Button { moreActions = true; touch() } label: { Image(systemName: "ellipsis") }
                    .buttonStyle(TVRoundControlStyle(diameter: 66, glyph: 24))
                    .focused($focus, equals: .more)
                    .accessibilityLabel("Weitere Aktionen")
                    .accessibilityIdentifier("tv-player-actions")
            }
        }
    }

    /// Under the actions: the name of the focused small button (they show
    /// only an icon), a confirmation ("… kommt seltener"), or what is next.
    @ViewBuilder private func footnote(_ track: Track) -> some View {
        HStack(spacing: 20) {
            if let hint = focusHint {
                Text(hint).foregroundStyle(.white)
            } else if let notice = model.notice {
                Label(notice.text, systemImage: notice.symbol).foregroundStyle(.white)
                if let action = notice.action {
                    Button(action.title) { action.run(); model.notice = nil; touch() }
                        .buttonStyle(TVPillStyle())
                        .focused($focus, equals: .notice)
                        .accessibilityHint(action.hint)
                }
            } else if let next = upcoming {
                HStack(spacing: 10) {
                    Text("Als Nächstes").foregroundStyle(.white.opacity(0.5))
                    Text("\(next.title) · \(next.artistText)").foregroundStyle(.white.opacity(0.85))
                }
                .accessibilityElement(children: .combine)
            } else if let error = player.error {
                Text(error).foregroundStyle(.white.opacity(0.8))
            }
        }
        .font(.system(size: 24, weight: .medium))
        .lineLimit(1)
        .frame(minHeight: 66, alignment: .leading)
    }
    private var upcoming: Track? {
        let queue = player.queue
        return queue.tracks.indices.contains(queue.index + 1) ? queue.tracks[queue.index + 1] : nil
    }
    private var focusHint: String? {
        switch focus {
        case .sleep?:
            if let deadline = player.sleepDeadline { return "Sleep-Timer · endet um \(deadline.formatted(date: .omitted, time: .shortened))" }
            return "Sleep-Timer · \(player.sleepMode.name)"
        case .speed?: return "Tempo · \(TVSoundSettings.rate(player.playbackRate))"
        case .radio?: return "Song-Radio starten"
        case .less?:
            guard let track = player.current else { return nil }
            return model.preferences.contains(.track, track.id) ? "Wieder vorschlagen" : "Weniger davon · Mixe und Radios lassen den Titel aus"
        case .more?: return "Weitere Aktionen"
        case .scrubber?: return "Nach links oder rechts zum Spulen"
        default: return nil
        }
    }

    @ViewBuilder private var moreDialog: some View {
        if let track = player.current {
            Button("Zur Playlist hinzufügen") {
                // After the dialog has gone, so the sheet can present.
                Task { @MainActor in try? await Task.sleep(for: .milliseconds(400)); sheet = .playlist }
            }
            Button("Wiedergabe übergeben") { Task { await model.saveHandoff() } }
            if let artist = model.artistReference(for: track), model.serverFeatures.preferences {
                let excluded = model.preferences.contains(.artist, artist.id)
                Button(excluded ? "Künstler wieder vorschlagen" : "Künstler nicht mehr vorschlagen") {
                    Task {
                        if excluded { await model.allow(.artist, target: artist.id, name: artist.name) }
                        else { await model.excludeArtist(id: artist.id, name: artist.name) }
                    }
                }
            }
            Button("Abbrechen", role: .cancel) {}
        }
    }

    @ViewBuilder private func sheetContent(_ item: TVNowPlayingSheet) -> some View {
        switch item {
        case .sleep:
            NavigationStack {
                TVChoiceList(title: "Sleep-Timer", identifier: "tv-np-sleep", selection: player.sleepMode,
                             choices: SleepMode.allCases.map { ($0, $0.name) }) { player.setSleepMode($0) }
            }
        case .speed:
            NavigationStack {
                TVChoiceList(title: "Tempo", identifier: "tv-np-speed", selection: player.playbackRate,
                             choices: SoundStep.playbackRates.map { ($0, TVSoundSettings.rate($0)) }) { player.setPlaybackRate($0) }
            }
        case .playlist:
            if let track = player.current { AddToPlaylistSheet(model: model, tracks: [track]) }
        }
    }

    // MARK: Panels

    private func panelHeader(_ title: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.system(size: 44, weight: .bold)).foregroundStyle(.white).accessibilityAddTraits(.isHeader)
            Label("Zurück zum Player mit der Menütaste", systemImage: "chevron.backward")
                .font(.system(size: 22, weight: .medium)).foregroundStyle(.white.opacity(0.5))
        }
    }

    private var queuePanel: some View {
        VStack(alignment: .leading, spacing: 20) {
            panelHeader("Warteschlange")
            ScrollView {
                TVQueueView(model: model).padding(.horizontal, 12).padding(.bottom, 40)
            }
            .scrollClipDisabled()
        }
    }

    private var lyricsPanel: some View {
        VStack(alignment: .leading, spacing: 20) {
            panelHeader("Lyrics")
            TVLyricsPanel(player: player)
        }
    }
}

/// Lyrics for the sofa: large lines; with timed lyrics the sung line is
/// white, the view follows it and selecting a line jumps there. Every line
/// takes focus, so long plain lyrics scroll with the remote too.
private struct TVLyricsPanel: View {
    var player: PlayerModel
    @State private var timeline = LyricsTimeline("")
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        Group {
            let text = cleanLyrics(player.lyrics).trimmingCharacters(in: .whitespacesAndNewlines)
            if player.lyrics.isEmpty {
                ProgressView("Lyrics werden geladen …").frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if timeline.lines.isEmpty && text.isEmpty {
                ContentUnavailableView("Kein Liedtext", systemImage: "quote.bubble", description: Text("Für diesen Titel ist kein Liedtext gespeichert."))
            } else if timeline.lines.isEmpty {
                let lines = text.components(separatedBy: .newlines)
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 4) {
                        ForEach(Array(lines.enumerated()), id: \.offset) { index, line in
                            Button {} label: { Text(line.isEmpty ? " " : line).font(.system(size: 40, weight: .bold)).lineSpacing(6) }
                                .buttonStyle(TVLyricStyle(active: false, dimmed: false))
                                .accessibilityIdentifier("tv-lyrics-line-\(index)")
                        }
                    }.padding(.vertical, 20)
                }
                .scrollClipDisabled()
            } else {
                let active = timeline.activeIndex(at: player.position)
                ScrollViewReader { reader in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 4) {
                            ForEach(timeline.lines) { line in
                                Button { player.seek(line.seconds) } label: {
                                    Text(line.text.isEmpty ? "♪" : line.text).font(.system(size: 44, weight: .bold))
                                }
                                .buttonStyle(TVLyricStyle(active: active == line.id, dimmed: true))
                                .id(line.id)
                                .accessibilityLabel("\(line.text), ab \(formatTime(line.seconds))")
                                .accessibilityIdentifier("tv-lyrics-line-\(line.id)")
                            }
                        }.padding(.vertical, 120)
                    }
                    .scrollClipDisabled()
                    .onChange(of: active, initial: true) { _, active in
                        guard let active else { return }
                        withAnimation(reduceMotion ? nil : .easeInOut(duration: 0.4)) { reader.scrollTo(active, anchor: .center) }
                    }
                }
            }
        }
        .onChange(of: player.lyrics, initial: true) { timeline = LyricsTimeline(player.lyrics) }
    }
}

/// A button that draws nothing: the full-screen catcher of the resting state.
private struct TVInvisibleStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View { configuration.label.contentShape(Rectangle()) }
}

// MARK: - Now-playing bar

/// What plays, on every screen while something is loaded: cover, title,
/// artist, a moving indicator and the progress. Select opens Now Playing.
/// It sits at the bottom, clear of the tab bar; content scrolls above it.
struct TVNowPlayingBar: View {
    var model: AppModel
    var open: () -> Void
    var body: some View {
        if let track = model.player.current {
            Button(action: open) { TVNowPlayingBarLabel(model: model, track: track) }
                .buttonStyle(TVBarStyle())
                .accessibilityLabel("Jetzt läuft: \(track.title), \(track.artistText)")
                .accessibilityValue(model.player.isPlaybackRequested ? "Wird abgespielt" : "Pausiert")
                .accessibilityHint("Öffnet Jetzt läuft")
                .accessibilityIdentifier("tv-now-playing-bar")
                .padding(.top, 12).padding(.bottom, 4)
                .frame(maxWidth: .infinity)
                .focusSection()
        }
    }
}
private struct TVNowPlayingBarLabel: View {
    var model: AppModel
    var track: Track
    @Environment(\.tvBarFocused) private var focused
    var body: some View {
        let player = model.player
        HStack(spacing: 24) {
            ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 12).frame(width: 76, height: 76)
            VStack(alignment: .leading, spacing: 6) {
                Text(track.title).font(.system(size: 28, weight: .semibold)).lineLimit(1)
                Text(track.artistText).font(.system(size: 22, weight: .medium)).foregroundStyle(.secondary).lineLimit(1)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            PlayingIndicator(playing: player.isPlaying, color: focused ? TVTheme.accent : .white)
                .scaleEffect(1.7).frame(width: 30, height: 30)
            Text(player.isPlaybackRequested ? "Läuft" : "Pausiert")
                .font(.system(size: 22, weight: .semibold)).foregroundStyle(.secondary)
                .frame(minWidth: 100, alignment: .trailing)
        }
        .padding(.leading, 18).padding(.trailing, 30).padding(.vertical, 16)
        .frame(width: 880)
        .overlay(alignment: .bottom) {
            GeometryReader { proxy in
                Capsule().fill(focused ? TVTheme.accent : Color.white.opacity(0.85))
                    .frame(width: max(0, proxy.size.width * PlaybackClock.fraction(player.position, of: player.duration)), height: 4)
            }
            .frame(height: 4)
            .padding(.horizontal, 28).padding(.bottom, 8)
            .accessibilityHidden(true)
        }
    }
}
#endif
