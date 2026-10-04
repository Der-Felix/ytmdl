import SwiftUI
import AVKit
import YTMDLCore

#if os(macOS)
/// An open player overlay owns Escape before the underlying navigation toolbar.
struct PlayerOverlayPreferenceKey: PreferenceKey {
    static var defaultValue: Bool { false }
    static func reduce(value: inout Bool, nextValue: () -> Bool) { value = value || nextValue() }
}

enum PlayerVisualizerStyle: String, CaseIterable, Identifiable {
    case bars, columns, orbit = "curve", rings, dots, ribbon
    var id: String { rawValue }
    var name: String {
        switch self {
        case .bars: "Spiegel-Spektrum"
        case .columns: "Säulen"
        case .orbit: "Orbit"
        case .rings: "Ringe"
        case .dots: "Lichtpunkte"
        case .ribbon: "Frequenzband"
        }
    }
    var radial: Bool { self == .orbit || self == .rings }
}

enum PlayerVisualizerPlacement: String, CaseIterable, Identifiable {
    case below, overlay, background
    var id: String { rawValue }
    var name: String {
        switch self {
        case .below: "Unter dem Cover"
        case .overlay: "Auf dem Cover"
        case .background: "Ohne Cover"
        }
    }
}

enum VisualizerColorMode: String, CaseIterable, Identifiable {
    case cover, theme, aurora, sunset, ocean, neon, custom
    var id: String { rawValue }
    var name: String {
        switch self {
        case .cover: "Coverfarben"
        case .theme: "Theme-Farbe"
        case .aurora: "Aurora"
        case .sunset: "Sonnenuntergang"
        case .ocean: "Ozean"
        case .neon: "Neon"
        case .custom: "Eigene Farben"
        }
    }
    static func resolved(_ raw: String, legacyCover: Bool) -> Self {
        Self(rawValue: raw) ?? (legacyCover ? .cover : .theme)
    }
}

/// RGB strings keep native color-picker choices portable and device-local.
private enum VisualizerColors {
    static func color(_ hex: String, fallback: String) -> Color {
        let valid = hex.count == 6 && hex.allSatisfy { $0.isASCII && $0.isHexDigit }
        let value = UInt32(valid ? hex : fallback, radix: 16) ?? 0xEF548E
        return Color(red: Double((value >> 16) & 255) / 255,
                     green: Double((value >> 8) & 255) / 255, blue: Double(value & 255) / 255)
    }
    static func hex(_ color: Color) -> String? {
        guard let rgb = NSColor(color).usingColorSpace(.sRGB) else { return nil }
        let channels = [rgb.redComponent, rgb.greenComponent, rgb.blueComponent]
        guard channels.allSatisfy({ $0.isFinite }) else { return nil }
        return channels.map { String(format: "%02X", Int((min(1, max(0, $0)) * 255).rounded())) }.joined()
    }
    static func palette(_ mode: VisualizerColorMode, cover: Color, theme: Color,
                        custom: Color, end: Color, gradient: Bool) -> (Color, Color?) {
        switch mode {
        case .cover: (cover, nil)
        case .theme: (theme, nil)
        case .aurora: (color("53E9BB", fallback: "53E9BB"), color("9C71FF", fallback: "9C71FF"))
        case .sunset: (color("FFB65B", fallback: "FFB65B"), color("EF548E", fallback: "EF548E"))
        case .ocean: (color("46DAD2", fallback: "46DAD2"), color("5285FF", fallback: "5285FF"))
        case .neon: (color("EE5ADA", fallback: "EE5ADA"), color("55CCFF", fallback: "55CCFF"))
        case .custom: (custom, gradient ? end : nil)
        }
    }
}

/// Shared device-local preferences for the player and its detailed sound settings.
struct VisualizerPreferences: View {
    @AppStorage("playerVisualizerStyle") private var style = PlayerVisualizerStyle.bars.rawValue
    @AppStorage("playerVisualizerPlacement") private var placement = PlayerVisualizerPlacement.below.rawValue
    @AppStorage("playerVisualizerIntensity") private var intensity = 1.0
    @AppStorage("playerVisualizerOpacity") private var opacity = 0.85
    @AppStorage("playerVisualizerPeaks") private var peaks = true
    @AppStorage("playerVisualizerCoverColors") private var coverColors = true
    @AppStorage("playerVisualizerColorMode") private var colorMode = "automatic"
    @AppStorage("playerVisualizerCustomColor") private var customColor = "EF548E"
    @AppStorage("playerVisualizerEndColor") private var endColor = "55CCFF"
    @AppStorage("playerVisualizerCustomGradient") private var customGradient = false
    private var selectedColorMode: VisualizerColorMode { .resolved(colorMode, legacyCover: coverColors) }
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 16) {
                fieldLabel("Stil")
                Menu {
                    Picker("Visualizer-Stil", selection: $style) {
                        ForEach(PlayerVisualizerStyle.allCases) { Text($0.name).tag($0.rawValue) }
                    }
                } label: { selectionLabel((PlayerVisualizerStyle(rawValue: style) ?? .bars).name) }
                    .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer-Stil")
            }
            HStack(spacing: 16) {
                fieldLabel("Darstellung")
                Menu {
                    Picker("Visualizer-Darstellung", selection: $placement) {
                        ForEach(PlayerVisualizerPlacement.allCases) { Text($0.name).tag($0.rawValue) }
                    }
                } label: { selectionLabel((PlayerVisualizerPlacement(rawValue: placement) ?? .below).name) }
                    .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer-Darstellung")
            }
            Divider()
            VStack(spacing: 8) {
                HStack { Text("Intensität"); Spacer(); Text(String(format: "%g×", intensity)).monospacedDigit().foregroundStyle(.secondary) }
                Slider(value: $intensity, in: 0.5...2, step: 0.1).accessibilityLabel("Visualizer-Intensität")
            }
            if placement == PlayerVisualizerPlacement.overlay.rawValue {
                VStack(spacing: 8) {
                    HStack { Text("Deckkraft"); Spacer(); Text("\(Int(opacity * 100)) %").monospacedDigit().foregroundStyle(.secondary) }
                    Slider(value: $opacity, in: 0.35...1, step: 0.05).accessibilityLabel("Deckkraft auf dem Cover")
                }
            }
            if [.bars, .columns, .orbit].contains(PlayerVisualizerStyle(rawValue: style) ?? .bars) {
                Toggle(isOn: $peaks) { Text("Spitzen anzeigen").frame(maxWidth: .infinity, alignment: .leading) }.toggleStyle(.switch)
            }
            Divider()
            HStack(spacing: 16) {
                fieldLabel("Farben")
                Menu {
                    ForEach(VisualizerColorMode.allCases) { mode in
                        Button { colorMode = mode.rawValue } label: {
                            if selectedColorMode == mode { Label(mode.name, systemImage: "checkmark") }
                            else { Text(mode.name) }
                        }
                    }
                } label: { selectionLabel(selectedColorMode.name) }
                    .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer-Farbpalette")
            }
            if selectedColorMode == .custom {
                customColorRow("Farbe", stored: $customColor, fallback: "EF548E")
                Toggle(isOn: $customGradient) { Text("Farbverlauf").frame(maxWidth: .infinity, alignment: .leading) }.toggleStyle(.switch)
                if customGradient {
                    customColorRow("Zweite Farbe", stored: $endColor, fallback: "55CCFF")
                }
            }
            Text("Nur der Visualizer verwendet diese Farben. Cover und Bedienelemente behalten ihr Design.")
                .desktopScaledFont(13).foregroundStyle(.secondary)
        }.desktopScaledFont(15).frame(maxWidth: .infinity, alignment: .leading)
    }
    private func customColorRow(_ title: String, stored: Binding<String>, fallback: String) -> some View {
        HStack {
            Text(title)
            Spacer()
            ColorPicker(title, selection: colorBinding(stored, fallback: fallback), supportsOpacity: false)
                .labelsHidden().accessibilityLabel(title)
        }
    }
    private func colorBinding(_ stored: Binding<String>, fallback: String) -> Binding<Color> {
        Binding(get: { VisualizerColors.color(stored.wrappedValue, fallback: fallback) },
                set: { if let hex = VisualizerColors.hex($0) { stored.wrappedValue = hex } })
    }
    private func fieldLabel(_ title: String) -> some View {
        Text(title).foregroundStyle(.secondary).frame(width: 100, alignment: .leading)
    }
    private func selectionLabel(_ value: String) -> some View {
        HStack(spacing: 12) {
            Text(value).lineLimit(1)
            Spacer(minLength: 8)
            Image(systemName: "chevron.down").font(.caption).foregroundStyle(.secondary)
        }.padding(.horizontal, 10).frame(maxWidth: .infinity, minHeight: 36)
            .contentShape(RoundedRectangle(cornerRadius: 8))
    }

}

/// Instantaneous frequency data; there is no time history or simulated waveform.
struct AudioSpectrumVisualizer: View {
    var player: PlayerModel
    var style: PlayerVisualizerStyle
    var accent: Color
    @Environment(\.desktopTheme) private var theme
    @AppStorage("playerVisualizerCoverColors") private var legacyCover = true
    @AppStorage("playerVisualizerColorMode") private var colorMode = "automatic"
    @AppStorage("playerVisualizerCustomColor") private var customColor = "EF548E"
    @AppStorage("playerVisualizerEndColor") private var endColor = "55CCFF"
    @AppStorage("playerVisualizerCustomGradient") private var customGradient = false
    @AppStorage("playerVisualizerIntensity") private var intensity = 1.0
    @AppStorage("playerVisualizerPeaks") private var peaks = true
    var body: some View {
        let palette = VisualizerColors.palette(.resolved(colorMode, legacyCover: legacyCover), cover: accent,
                                              theme: theme.accent, custom: VisualizerColors.color(customColor, fallback: "EF548E"),
                                              end: VisualizerColors.color(endColor, fallback: "55CCFF"), gradient: customGradient)
        SpectrumCanvas(levels: player.spectrumLevels, peaks: player.spectrumPeaks, style: style,
                       accent: palette.0, endAccent: palette.1, intensity: intensity, showPeaks: peaks)
            .accessibilityLabel("Live-Musikspektrum: \(style.name)")
            .accessibilityValue(player.isPlaying ? "Wiedergabe läuft" : "Pausiert")
            .help("32 gemessene Frequenzbänder: Bass bis Höhen; ohne Mikrofonaufnahme")
    }
}

/// Stateless drawing also permits silent rendering checks without starting an audio device.
struct SpectrumCanvas: View {
    var levels: [Double]
    var peaks: [Double]
    var style: PlayerVisualizerStyle
    var accent: Color
    var endAccent: Color? = nil
    var intensity = 1.0
    var showPeaks = true
    var body: some View {
        Canvas { context, size in
            guard !levels.isEmpty else { return }
            let gain = intensity.isFinite ? min(2, max(0.5, intensity)) : 1
            let amplitudes = levels.map { $0.isFinite ? min(1, max(0, $0 * gain)) : 0 }
            func bandColor(_ index: Int, count: Int) -> Color {
                accent.mix(with: endAccent ?? accent, by: Double(index) / Double(max(1, count - 1)))
            }
            let shading = GraphicsContext.Shading.linearGradient(Gradient(colors: [accent, endAccent ?? accent]), startPoint: .zero, endPoint: CGPoint(x: size.width, y: 0))
            let step = size.width / CGFloat(amplitudes.count)
            switch style {
            case .bars, .columns:
                let middle = style == .bars ? size.height * 0.5 : size.height * 0.95
                let maximum = size.height * (style == .bars ? 0.46 : 0.89)
                for (index, level) in amplitudes.enumerated() {
                    let x = (CGFloat(index) + 0.5) * step
                    let height = max(2, level * maximum), width = max(1, step * 0.62)
                    let rect = CGRect(x: x - width / 2, y: middle - height, width: width, height: height * (style == .bars ? 2 : 1))
                    let color = bandColor(index, count: amplitudes.count)
                    let column = GraphicsContext.Shading.linearGradient(Gradient(colors: [color, color.opacity(0.35)]), startPoint: CGPoint(x: 0, y: middle - height), endPoint: CGPoint(x: 0, y: middle + (style == .bars ? height : 0)))
                    context.fill(Path(roundedRect: rect, cornerRadius: min(4, width / 2)), with: column)
                    if showPeaks, peaks.indices.contains(index), peaks[index].isFinite {
                        let peak = min(1, max(0, peaks[index] * gain)) * maximum
                        if peak > 3 {
                            context.fill(Path(roundedRect: CGRect(x: x - width / 2, y: middle - peak - 4, width: width, height: 2), cornerRadius: 1), with: .color(color.mix(with: .white, by: 0.18).opacity(0.8)))
                        }
                    }
                }
            case .orbit, .rings:
                let center = CGPoint(x: size.width / 2, y: size.height / 2)
                let limit = min(size.width, size.height) * 0.46
                let radius = limit * 0.42
                let energy = amplitudes.prefix(8).reduce(0, +) / Double(min(8, amplitudes.count))
                let halo = CGRect(x: center.x - radius, y: center.y - radius, width: radius * 2, height: radius * 2)
                context.fill(Path(ellipseIn: halo), with: .radialGradient(Gradient(colors: [accent.opacity(0.1 + energy * 0.25), .clear]), center: center, startRadius: 0, endRadius: radius))
                if style == .rings {
                    // Four nested rings represent bass, low-mid, high-mid and treble energy.
                    for group in 0..<4 {
                        let lower = group * amplitudes.count / 4, upper = (group + 1) * amplitudes.count / 4
                        let band = amplitudes[lower..<upper]
                        let value = band.reduce(0, +) / Double(max(1, band.count))
                        let ringRadius = limit * (0.22 + Double(group) * 0.19) + value * limit * 0.13
                        let rect = CGRect(x: center.x - ringRadius, y: center.y - ringRadius, width: ringRadius * 2, height: ringRadius * 2)
                        context.stroke(Path(ellipseIn: rect), with: .color(bandColor(group, count: 4).opacity(0.3 + value * 0.7)), lineWidth: 2 + value * 7)
                    }
                } else {
                    context.stroke(Path(ellipseIn: halo), with: .color(accent.opacity(0.3)), lineWidth: 1)
                    for (index, level) in amplitudes.enumerated() {
                        let angle = Double(index) / Double(amplitudes.count) * 2 * Double.pi - Double.pi / 2
                        let outer = radius + 3 + level * (limit - radius)
                        var ray = Path()
                        ray.move(to: CGPoint(x: center.x + cos(angle) * radius, y: center.y + sin(angle) * radius))
                        ray.addLine(to: CGPoint(x: center.x + cos(angle) * outer, y: center.y + sin(angle) * outer))
                        context.stroke(ray, with: .color(bandColor(index, count: amplitudes.count)), style: StrokeStyle(lineWidth: max(2, min(8, radius * 0.045)), lineCap: .round))
                        if showPeaks, peaks.indices.contains(index), peaks[index].isFinite {
                            let peakRadius = radius + 3 + min(1, max(0, peaks[index] * gain)) * (limit - radius)
                            let point = CGPoint(x: center.x + cos(angle) * peakRadius, y: center.y + sin(angle) * peakRadius)
                            if peaks[index] > 0.03 { context.fill(Path(ellipseIn: CGRect(x: point.x - 2, y: point.y - 2, width: 4, height: 4)), with: .color(bandColor(index, count: amplitudes.count).opacity(0.6))) }
                        }
                    }
                }
            case .dots:
                let rows = 12, dot = max(1, min(step * 0.65, size.height / CGFloat(rows) * 0.62))
                for (index, level) in amplitudes.enumerated() {
                    for row in 0..<rows {
                        let active = Double(row) / Double(rows) < level && level > 0.005
                        let point = CGPoint(x: (CGFloat(index) + 0.5) * step, y: size.height - (CGFloat(row) + 0.5) * size.height / CGFloat(rows))
                        context.fill(Path(ellipseIn: CGRect(x: point.x - dot / 2, y: point.y - dot / 2, width: dot, height: dot)), with: .color(active ? bandColor(index, count: amplitudes.count).opacity(0.45 + Double(row) / Double(rows) * 0.55) : bandColor(index, count: amplitudes.count).opacity(0.08)))
                    }
                }
            case .ribbon:
                // Mirrored frequency envelope, deliberately not labelled a PCM waveform.
                let middle = size.height / 2, maximum = size.height * 0.45
                var path = Path()
                path.move(to: CGPoint(x: 0, y: middle))
                let upper = amplitudes.enumerated().map { CGPoint(x: (CGFloat($0.offset) + 0.5) * step, y: middle - max(1, $0.element * maximum)) }
                let lower = amplitudes.enumerated().reversed().map { CGPoint(x: (CGFloat($0.offset) + 0.5) * step, y: middle + max(1, $0.element * maximum)) }
                for (index, point) in upper.enumerated() {
                    let next = index + 1 < upper.count ? upper[index + 1] : CGPoint(x: size.width, y: middle)
                    path.addQuadCurve(to: CGPoint(x: (point.x + next.x) / 2, y: (point.y + next.y) / 2), control: point)
                }
                path.addLine(to: CGPoint(x: size.width, y: middle))
                for (index, point) in lower.enumerated() {
                    let next = index + 1 < lower.count ? lower[index + 1] : CGPoint(x: 0, y: middle)
                    path.addQuadCurve(to: CGPoint(x: (point.x + next.x) / 2, y: (point.y + next.y) / 2), control: point)
                }
                path.closeSubpath()
                context.fill(path, with: shading)
                context.stroke(path, with: shading, style: StrokeStyle(lineWidth: 2, lineJoin: .round))
            }
        }
    }
}

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
    @AppStorage("playerVisualizerStyle") private var visualizerStyle = PlayerVisualizerStyle.bars.rawValue
    @AppStorage("playerVisualizerPlacement") private var visualizerPlacement = PlayerVisualizerPlacement.below.rawValue
    @AppStorage("playerVisualizerOpacity") private var visualizerOpacity = 0.85
    var model: AppModel
    var size: CGSize
    @State private var tab = 0
    @State private var queueFilter = ""
    @State private var playlistSelection: [Track] = []
    @State private var addingToPlaylist = false
    @State private var expandedArtwork = false
    @State private var expandedVisualizer = false
    @State private var visualizerSettingsOpen = false
    @State private var transitionSettingsOpen = false
    var body: some View {
        Group {
            if let track = model.player.current {
                if size.width >= 1000 {
                    let available = size.width - 72
                    let contextWidth = max(480, min(640, available * 0.36))
                    HStack(alignment: .top, spacing: 24) {
                        ScrollView {
                            listeningCard(track, cover: max(220, min(520, (available - contextWidth) * 0.7, size.height - (showsInlineVisualizer ? selectedVisualizerStyle.radial ? 780 : 706 : 560))))
                        }.scrollIndicators(.hidden).frame(maxWidth: .infinity)
                        ScrollView {
                            VStack(spacing: 0) {
                                contextPanel(queueHeight: max(500, size.height - 48))
                            }
                        }.scrollIndicators(.hidden).frame(width: contextWidth)
                    }.padding(24).frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    VStack(spacing: 0) {
                        ScrollView {
                            VStack(spacing: 24) {
                                artworkCard(track, cover: max(180, min(320, size.width - 56, size.height - 440)))
                                contextPanel(queueHeight: 500)
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
        .preference(key: PlayerOverlayPreferenceKey.self, value: visualizerSettingsOpen || transitionSettingsOpen || expandedArtwork || expandedVisualizer || addingToPlaylist)
        .environment(\.desktopAccent, playerAccent)
        .environment(\.desktopButtonAccent, playerButtonAccent)
        .tint(playerAccent)
        .sheet(isPresented: $addingToPlaylist) { AddToPlaylistSheet(model: model, tracks: playlistSelection) }
        .sheet(isPresented: $expandedArtwork) {
            VStack(spacing: 20) {
                if let track = model.player.current {
                    ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 520, height: 520)
                    Text(track.title).font(.title2).multilineTextAlignment(.center).lineLimit(2)
                }
                Button("Schließen") { expandedArtwork = false }.keyboardShortcut(.cancelAction)
            }.padding(28)
        }
        .sheet(isPresented: $expandedVisualizer) { visualizerSheet }
        #if DEBUG
        .task {
            let args = ProcessInfo.processInfo.arguments
            if args.contains("--fixture-server"), model.client?.server.url.host == "127.0.0.1",
               let index = args.firstIndex(of: "--fixture-player-tab"), args.indices.contains(index + 1) {
                tab = args[index + 1] == "Wiedergabe" ? 3 : args[index + 1] == "Klang" ? 2 : args[index + 1] == "Lyrics" ? 1 : 0
            }
            if args.contains("--fixture-server"), model.client?.server.url.host == "127.0.0.1", args.contains("--fixture-visualizer") {
                model.player.setVisualization(true); expandedVisualizer = true
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
            artworkPresentation(track, cover: cover).frame(maxWidth: .infinity)
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
            if showsInlineVisualizer { visualizerPanel }
        }.padding(.horizontal, 4).padding(.bottom, 8)
    }
    private func listeningCard(_ track: Track, cover: CGFloat) -> some View {
        VStack(spacing: 22) {
            Spacer(minLength: 16)
            artworkPresentation(track, cover: cover)
            HStack(alignment: .top, spacing: 12) {
                Spacer().frame(width: 88)
                VStack(spacing: 10) {
                    Text(track.title).desktopScaledFont(32, weight: .bold).multilineTextAlignment(.center).lineLimit(3)
                    Text(track.artistText).desktopScaledFont(23).foregroundStyle(.secondary).multilineTextAlignment(.center).lineLimit(2)
                }.frame(maxWidth: .infinity)
                HStack(spacing: 4) {
                    Button { Task { await model.toggleFavorite(track) } } label: {
                        Image(systemName: model.favoriteIDs.contains(track.id) ? "heart.fill" : "heart")
                            .desktopScaledFont(26).foregroundStyle(playerAccent).frame(width: 44, height: 44)
                    }.buttonStyle(DesktopControlStyle()).accessibilityLabel("Favorit umschalten")
                    Menu {
                        Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; addingToPlaylist = true }
                        Button("Warteschlange als Playlist speichern", systemImage: "square.and.arrow.down") { playlistSelection = model.player.queue.tracks; addingToPlaylist = true }
                        Button("Titel und Künstler kopieren") { copyTrack(track) }
                        Button("Cover vergrößern") { expandedArtwork = true }
                        Button("Lyrics anzeigen") { tab = 1 }
                        Divider()
                        Button("Kommende Titel aus Warteschlange entfernen") { model.player.clearUpcoming() }
                            .disabled(model.player.queue.index + 1 >= model.player.queue.tracks.count)
                    } label: { Image(systemName: "ellipsis").desktopScaledFont(23).frame(width: 44, height: 44) }
                        .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle())
                        .accessibilityLabel("Aktionen für den aktuellen Titel")
                }
            }
            if showsInlineVisualizer { visualizerPanel }
            Spacer(minLength: 36)
            DesktopSeekControl(player: model.player)
            ViewThatFits(in: .horizontal) {
                DesktopPlaybackControls(player: model.player, large: true)
                DesktopPlaybackControls(player: model.player)
            }.frame(maxWidth: .infinity)
            HStack(spacing: 20) {
                DesktopVolumeControl(player: model.player, expanded: true)
                AirPlayPicker().frame(width: 44, height: 44).accessibilityLabel("Audioausgabe wählen")
            }
            playbackStatus(track)
            playbackError
        }.frame(maxWidth: .infinity, minHeight: max(0, size.height - 104)).padding(28)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 26))
            .overlay(RoundedRectangle(cornerRadius: 26).strokeBorder(Color.primary.opacity(0.08)))
    }
    private var selectedVisualizerPlacement: PlayerVisualizerPlacement {
        model.player.visualizationEnabled ? PlayerVisualizerPlacement(rawValue: visualizerPlacement) ?? .below : .below
    }
    private var showsInlineVisualizer: Bool { model.player.visualizationEnabled && selectedVisualizerPlacement == .below }
    private var visualizerAccent: Color { playerAccent }
    @ViewBuilder private func artworkPresentation(_ track: Track, cover: CGFloat) -> some View {
        if selectedVisualizerPlacement == .background {
            VStack(spacing: 16) {
                HStack {
                    Label(selectedVisualizerStyle.name, systemImage: "waveform").foregroundStyle(.secondary)
                    Spacer()
                    expandVisualizerButton
                }
                AudioSpectrumVisualizer(player: model.player, style: selectedVisualizerStyle, accent: visualizerAccent)
                    .frame(height: max(240, min(560, size.height - 490)))
            }.frame(maxWidth: .infinity)
        } else {
            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: cover, height: cover)
                .overlay {
                    if selectedVisualizerPlacement == .overlay {
                        ZStack(alignment: .bottom) {
                            LinearGradient(colors: [.clear, .black.opacity(0.72)], startPoint: .center, endPoint: .bottom)
                            AudioSpectrumVisualizer(player: model.player, style: selectedVisualizerStyle, accent: visualizerAccent)
                                .frame(height: selectedVisualizerStyle.radial ? cover * 0.75 : cover * 0.36)
                                .padding(16).opacity(visualizerOpacity.isFinite ? min(1, max(0.35, visualizerOpacity)) : 0.85)
                        }.allowsHitTesting(false).accessibilityHidden(true)
                    }
                }
                .overlay(alignment: .topTrailing) {
                    Button { expandedArtwork = true } label: {
                        Image(systemName: "arrow.up.left.and.arrow.down.right").font(.title3)
                            .padding(10).background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 10))
                    }.buttonStyle(DesktopHoverStyle(radius: 10)).padding(12).accessibilityLabel("Cover vergrößern")
                }
                .clipShape(RoundedRectangle(cornerRadius: 18))
                .shadow(color: .black.opacity(0.24), radius: 20, y: 12)
        }
    }
    private var expandVisualizerButton: some View {
        Button { expandedVisualizer = true } label: {
            Image(systemName: "arrow.up.left.and.arrow.down.right").frame(width: 36, height: 32)
        }.buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer vergrößern")
    }
    private var selectedVisualizerStyle: PlayerVisualizerStyle { PlayerVisualizerStyle(rawValue: visualizerStyle) ?? .bars }
    private var visualizerStylePicker: some View {
        Picker("Visualizer-Stil", selection: $visualizerStyle) {
            ForEach(PlayerVisualizerStyle.allCases) { Text($0.name).tag($0.rawValue) }
        }.pickerStyle(.menu).labelsHidden().frame(maxWidth: 240)
    }
    private var visualizerPanel: some View {
        VStack(spacing: 10) {
            HStack {
                Label("Visualizer", systemImage: "waveform").desktopScaledFont(15, weight: .semibold)
                Spacer(minLength: 8)
                Button { expandedVisualizer = true } label: {
                    Image(systemName: "arrow.up.left.and.arrow.down.right").frame(width: 36, height: 32)
                }.buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer vergrößern")
            }.foregroundStyle(.secondary)
            AudioSpectrumVisualizer(player: model.player, style: selectedVisualizerStyle, accent: visualizerAccent).frame(height: selectedVisualizerStyle.radial ? 170 : 100)
        }.frame(maxWidth: 680)
    }
    private var visualizerSheet: some View {
        VStack(spacing: 24) {
            HStack {
                Label("Visualizer", systemImage: "waveform").font(.title2.bold())
                Spacer()
                visualizerStylePicker
                Button("Schließen") { expandedVisualizer = false }.keyboardShortcut(.cancelAction)
            }
            VStack(spacing: 8) {
                Text(model.player.current?.title ?? "Deine Musik").font(.title.bold()).multilineTextAlignment(.center).lineLimit(2)
                Text(model.player.current?.artistText ?? "").font(.title3).foregroundStyle(.secondary)
            }
            AudioSpectrumVisualizer(player: model.player, style: selectedVisualizerStyle, accent: visualizerAccent)
                .frame(maxWidth: .infinity, maxHeight: .infinity).padding(.vertical, 16)
            HStack(spacing: 24) {
                Button { model.player.toggle() } label: {
                    Label(model.player.isPlaybackRequested ? "Pause" : "Abspielen", systemImage: model.player.isPlaybackRequested ? "pause.fill" : "play.fill")
                }.controlSize(.large)
                Text(model.player.equalizerFormat == 2 ? "Dieses Ausgabeformat kann nicht visualisiert werden." : "Live-Frequenzanalyse · 32 Bänder").foregroundStyle(.secondary)
            }
        }.padding(32).frame(minWidth: 680, idealWidth: 960, minHeight: 480, idealHeight: 620)
            .background(playerBackground).tint(playerAccent)
    }
    private func contextPanel(queueHeight: CGFloat) -> some View {
        queuePanel.frame(height: queueHeight)
            .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 26))
            .overlay(RoundedRectangle(cornerRadius: 26).strokeBorder(Color.primary.opacity(0.08)))
    }
    private var listeningOptions: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 12) {
                Image(systemName: "waveform").foregroundStyle(.secondary).frame(width: 22)
                VStack(alignment: .leading, spacing: 4) {
                    Text("Visualizer")
                    Text(model.player.visualizationEnabled ? "\(selectedVisualizerStyle.name) · \(selectedVisualizerPlacement.name)" : "Aus")
                        .desktopScaledFont(13).foregroundStyle(.secondary).lineLimit(2)
                }.frame(maxWidth: .infinity, alignment: .leading)
                Button { visualizerSettingsOpen.toggle() } label: {
                    Image(systemName: "slider.horizontal.3").frame(width: 36, height: 36)
                }.buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer anpassen").help("Visualizer anpassen")
                    .popover(isPresented: $visualizerSettingsOpen, arrowEdge: .leading) { visualizerSettingsPopover }
                Toggle("Visualizer", isOn: Binding(get: { model.player.visualizationEnabled }, set: { model.player.setVisualization($0) }))
                    .labelsHidden().toggleStyle(.switch).fixedSize()
            }
            Menu { equalizerMenuItems } label: {
                optionValueRow("Equalizer", value: model.player.equalizer.enabled ? (EqualizerPreset(rawValue: model.player.equalizer.preset)?.name ?? "Eigener Klang") : "Aus", icon: "slider.vertical.3")
            }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8))
                .accessibilityLabel("Equalizer-Profil wählen")
            Divider()
            VStack(spacing: 8) {
                HStack(spacing: 12) {
                    optionTitle("Überblendung", icon: "shuffle")
                    Spacer(minLength: 8)
                    crossfadeValue.foregroundStyle(.secondary)
                    Button { transitionSettingsOpen.toggle() } label: {
                        Image(systemName: "slider.horizontal.3").frame(width: 36, height: 36)
                    }.buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Übergänge anpassen").help("Übergänge anpassen")
                        .popover(isPresented: $transitionSettingsOpen, arrowEdge: .leading) { transitionSettingsPopover }
                }
                crossfadeSlider
            }
            Divider()
            Menu {
                ForEach(SleepMode.allCases) { mode in Button(mode.name) { model.player.setSleepMode(mode) } }
            } label: { optionValueRow("Sleep-Timer", value: model.player.sleepMode.name, icon: "moon.zzz") }
                .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8))
            Menu {
                ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0], id: \.self) { speed in
                    Button(String(format: "%g×", speed)) { model.player.setPlaybackRate(speed) }
                }
            } label: { optionValueRow("Tempo", value: String(format: "%g×", model.player.playbackRate), icon: "speedometer") }
                .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 8))
            HStack {
                Button { model.player.seek(model.player.position - 10) } label: { Label("−10 s", systemImage: "gobackward.10").padding(8) }
                Spacer()
                if model.player.visualizationEnabled {
                    Button { expandedVisualizer = true } label: { Image(systemName: "arrow.up.left.and.arrow.down.right").frame(width: 36, height: 36) }
                        .accessibilityLabel("Visualizer vergrößern").help("Visualizer vergrößern")
                }
                Spacer()
                Button { model.player.seek(model.player.position + 10) } label: { Label("+10 s", systemImage: "goforward.10").padding(8) }
            }.buttonStyle(DesktopHoverStyle(radius: 8)).foregroundStyle(.secondary)
            if let soundError = model.player.soundError { Text(soundError).desktopScaledFont(13).foregroundStyle(.secondary) }
            if model.player.visualizationEnabled && model.player.equalizerFormat == 2 {
                Text("Der Visualizer unterstützt dieses Ausgabeformat nicht. Die Musik läuft weiter.").desktopScaledFont(13).foregroundStyle(.secondary)
            }
            if let deadline = model.player.sleepDeadline {
                Text("Stoppt um \(deadline.formatted(date: .omitted, time: .shortened))").desktopScaledFont(13).foregroundStyle(.secondary)
            }
        }.desktopScaledFont(15).padding(.vertical, 10).frame(maxWidth: .infinity, alignment: .leading)
    }
    private func optionTitle(_ title: String, icon: String) -> some View {
        HStack(spacing: 12) {
            Image(systemName: icon).foregroundStyle(.secondary).frame(width: 22)
            Text(title).lineLimit(1)
        }
    }
    private func optionValueRow(_ title: String, value: String, icon: String) -> some View {
        HStack(spacing: 12) {
            optionTitle(title, icon: icon)
            Spacer(minLength: 8)
            Text(value).foregroundStyle(.secondary).lineLimit(1)
            Image(systemName: "chevron.down").font(.caption).foregroundStyle(.tertiary)
        }.frame(maxWidth: .infinity, minHeight: 36).contentShape(RoundedRectangle(cornerRadius: 8))
    }
    private var visualizerSettingsPopover: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack {
                Text("Visualizer anpassen").desktopScaledFont(19, weight: .semibold)
                Spacer()
                Button { visualizerSettingsOpen = false } label: { Image(systemName: "xmark").frame(width: 32, height: 32) }
                    .buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Visualizer-Einstellungen schließen").keyboardShortcut(.cancelAction)
            }
            ScrollView { VisualizerPreferences().padding(.vertical, 2) }.scrollIndicators(.automatic)
                .frame(height: min(500, max(260, size.height - 200)))
        }.padding(24).frame(width: 420).tint(playerAccent)
    }
    private var transitionSettingsPopover: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack {
                Text("Übergänge").desktopScaledFont(19, weight: .semibold)
                Spacer()
                Button { transitionSettingsOpen = false } label: { Image(systemName: "xmark").frame(width: 32, height: 32) }
                    .buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityLabel("Übergangs-Einstellungen schließen").keyboardShortcut(.cancelAction)
            }
            Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { model.player.smartAlbumTransition }, set: { model.player.setSmartAlbumTransition($0) })).toggleStyle(.switch)
            Text("Benachbarte Titel desselben Albums bleiben ohne Überlappung. Die eingestellte Überblendung gilt für andere Titel.")
                .desktopScaledFont(13).foregroundStyle(.secondary)
        }.desktopScaledFont(15).padding(24).frame(width: 390).tint(playerAccent)
    }
    private var crossfadeSlider: some View {
        Slider(value: Binding(get: { model.player.crossfadeSeconds }, set: { model.player.setCrossfade($0) }), in: 0...12, step: 1)
            .accessibilityLabel("Überblendung zwischen Titeln")
    }
    private var crossfadeValue: some View {
        Text("\(Int(model.player.crossfadeSeconds)) s").monospacedDigit().frame(width: 36)
    }
    private func copyTrack(_ track: Track) {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString("\(track.artistText) – \(track.title)", forType: .string)
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
    private var compactTools: some View {
        HStack(spacing: 10) {
            eqMenu
            fadeMenu
            timerMenu
            speedMenu
        }
    }
    @ViewBuilder private var equalizerMenuItems: some View {
            Toggle("Equalizer aktivieren", isOn: Binding(get: { model.player.equalizer.enabled }, set: { model.player.equalizer.setEnabled($0) }))
            Divider()
            ForEach(EqualizerPreset.allCases) { preset in
                Button(preset.name) { model.player.equalizer.select(preset); model.player.equalizer.setEnabled(true) }
            }
            Divider()
            Button("Alle Frequenzbänder anzeigen") { tab = 2 }
            Divider()
            Toggle("Automatischer EQ-Pegelschutz", isOn: Binding(get: { model.player.equalizer.headroom }, set: { model.player.equalizer.setHeadroom($0) }))
                .disabled(!model.player.equalizer.enabled)
    }
    private var eqMenu: some View {
        Menu { equalizerMenuItems } label: {
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
            Button("Übergänge einstellen") { tab = 3 }
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
                Text(title).desktopScaledFont(16, weight: .semibold).lineLimit(1)
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
        VStack(alignment: .leading, spacing: 14) {
            ScrollViewReader { reader in
                ScrollView(.horizontal) {
                    HStack(spacing: 8) {
                        panelTab("Warteschlange", value: 0).id(0)
                        panelTab("Lyrics", value: 1).id(1)
                        panelTab("Wiedergabe", value: 3).id(3)
                        panelTab("Klang", value: 2).id(2)
                    }.fixedSize(horizontal: true, vertical: false)
                }.scrollIndicators(.hidden)
                    .onChange(of: tab) { _, value in reader.scrollTo(value, anchor: .center) }
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
                    LazyVStack(spacing: 2) {
                        ForEach(Array(model.player.queue.tracks.enumerated()).filter { queueFilter.isEmpty || $0.element.title.localizedCaseInsensitiveContains(queueFilter) || $0.element.artistText.localizedCaseInsensitiveContains(queueFilter) }, id: \.offset) { index, track in
                            HStack(spacing: 4) {
                                Button { model.player.select(index) } label: {
                                    HStack(spacing: 12) {
                                        ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 44, height: 44)
                                        VStack(alignment: .leading, spacing: 5) {
                                            Text(track.title).desktopScaledFont(15, weight: .medium).lineLimit(1)
                                                .foregroundStyle(model.player.queue.index == index ? playerAccent : Color.primary)
                                            Text(track.artistText).desktopScaledFont(13).foregroundStyle(.secondary).lineLimit(1)
                                        }.frame(maxWidth: .infinity, alignment: .leading)
                                        if model.player.queue.index == index { Image(systemName: "speaker.wave.2.fill").foregroundStyle(playerAccent) }
                                    }.padding(8).contentShape(Rectangle())
                                }.buttonStyle(DesktopHoverStyle(radius: 10)).accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
                                Menu {
                                    Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [track]; addingToPlaylist = true }
                                    Button("Jetzt abspielen") { model.player.select(index) }
                                    Button("Als Nächstes abspielen") { model.player.playNextInQueue(index) }.disabled(index <= model.player.queue.index + 1)
                                    Button(model.favoriteIDs.contains(track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen") { Task { await model.toggleFavorite(track) } }
                                    Button("Titel und Künstler kopieren") { copyTrack(track) }
                                    Divider()
                                    Button("Aus Warteschlange entfernen") { model.player.removeFromQueue(index) }.disabled(index == model.player.queue.index)
                                } label: { Image(systemName: "ellipsis").desktopScaledFont(20).frame(width: 40, height: 44) }
                                    .menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle())
                                    .accessibilityLabel("Aktionen für \(track.title)")
                            }.padding(.trailing, 6)
                                .background(model.player.queue.index == index ? playerAccent.opacity(0.09) : .clear, in: RoundedRectangle(cornerRadius: 10))
                        }
                    }
                } else if tab == 3 {
                    listeningOptions
                } else if tab == 2 {
                    VStack(alignment: .leading, spacing: 32) {
                        EqualizerControls(player: model.player)
                        Divider()
                        PlaybackOptions(player: model.player)
                        Text("Titel-Normalisierung benötigt verlässliche Lautheitswerte pro Titel und ist noch nicht verfügbar. Der EQ-Pegelschutz gleicht nur Verstärkungen durch den Equalizer aus.").desktopScaledFont(15).foregroundStyle(.secondary)
                    }
                } else {
                    Text(model.player.lyrics.isEmpty ? "Lyrics werden geladen …" : cleanLyrics(model.player.lyrics))
                        .desktopScaledFont(24).lineSpacing(14).textSelection(.enabled).frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }.padding(24).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)

    }
    private func panelTab(_ title: String, value: Int) -> some View {
        Button { tab = value } label: {
            Text(title).desktopScaledFont(16, weight: .semibold).lineLimit(1).minimumScaleFactor(0.8)
                .padding(.horizontal, 8).padding(.vertical, 12)
                .foregroundStyle(tab == value ? playerAccent : .secondary)
                .overlay(alignment: .bottom) { Capsule().fill(tab == value ? playerAccent : .clear).frame(height: 2).padding(.horizontal, 8) }
        }.buttonStyle(DesktopHoverStyle(radius: 8)).accessibilityAddTraits(tab == value ? .isSelected : [])
    }
}

/// Collection artwork uses only already loaded tracks; the index needs no detail requests.
struct DesktopCollectionHeader: View {
    @Environment(\.desktopAccent) private var accent
    var model: AppModel
    var tracks: [Track]
    var title: String
    var subtitle: String
    var symbol: String
    var canPlay: Bool
    var detail: String
    var play: () -> Void
    var shuffle: () -> Void
    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 28) { artwork; metadata }
            VStack(alignment: .leading, spacing: 22) { artwork; metadata }
        }.padding(28).frame(maxWidth: .infinity, alignment: .leading)
            .background(LinearGradient(colors: [accent.opacity(0.14), accent.opacity(0.025)], startPoint: .topLeading, endPoint: .bottomTrailing), in: RoundedRectangle(cornerRadius: 24))
    }
    private var artwork: some View {
        let previews = Array(tracks.prefix(4))
        return ZStack {
            accent.opacity(0.1)
            if previews.isEmpty { Image(systemName: symbol).font(.system(size: 52)).foregroundStyle(accent) }
            else if previews.count < 4 {
                ArtworkView(model: model, kind: "tracks", id: previews[0].id)
            } else {
                VStack(spacing: 2) {
                    HStack(spacing: 2) { preview(previews[0]); preview(previews[1]) }
                    HStack(spacing: 2) { preview(previews[2]); preview(previews[3]) }
                }
            }
        }.frame(width: 150, height: 150).clipShape(RoundedRectangle(cornerRadius: 18)).accessibilityHidden(true)
    }
    private func preview(_ track: Track) -> some View { ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 74, height: 74) }
    private var metadata: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).desktopScaledFont(32, weight: .bold).lineLimit(3)
            Text(subtitle).desktopScaledFont(17).foregroundStyle(.secondary)
            Text(detail).desktopScaledFont(15).monospacedDigit().foregroundStyle(.secondary)
            HStack(spacing: 12) {
                Button(action: play) {
                    Label("Abspielen", systemImage: "play.fill").desktopScaledFont(16, weight: .semibold)
                        .padding(.horizontal, 18).frame(minHeight: 46).foregroundStyle(.white)
                        .background(accent, in: RoundedRectangle(cornerRadius: 12))
                }.buttonStyle(DesktopHoverStyle(radius: 12))
                Button(action: shuffle) {
                    Label("Zufall", systemImage: "shuffle").desktopScaledFont(16, weight: .medium)
                        .padding(.horizontal, 18).frame(minHeight: 46)
                        .background(.primary.opacity(0.05), in: RoundedRectangle(cornerRadius: 12))
                }.buttonStyle(DesktopHoverStyle(radius: 12))
            }.disabled(!canPlay).opacity(canPlay ? 1 : 0.45)
        }.frame(minWidth: 240, maxWidth: .infinity, alignment: .leading)
    }
}

struct DesktopPlaylistCard: View {
    @Environment(\.desktopAccent) private var accent
    @Environment(\.desktopTheme) private var theme
    @Environment(\.colorScheme) private var scheme
    var playlist: Playlist
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            ZStack(alignment: .bottomLeading) {
                RoundedRectangle(cornerRadius: 16).fill(LinearGradient(colors: [accent.opacity(0.32), accent.opacity(0.08)], startPoint: .topLeading, endPoint: .bottomTrailing))
                Image(systemName: playlist.smartRules == nil ? "music.note.list" : "sparkles").font(.system(size: 48, weight: .light)).foregroundStyle(accent).padding(22)
            }.frame(height: 130).accessibilityHidden(true)
            Text(playlist.name).desktopScaledFont(22, weight: .semibold).foregroundStyle(.primary).lineLimit(3, reservesSpace: true).frame(maxWidth: .infinity, alignment: .leading)
            Text("\(playlist.trackCount) Titel · \(formatTime(Double(playlist.durationMs) / 1000))").desktopScaledFont(15).foregroundStyle(.secondary)
            HStack { Text("Playlist öffnen"); Spacer(); Image(systemName: "arrow.right") }.desktopScaledFont(15).foregroundStyle(accent)
        }.padding(20).frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.surface(scheme), in: RoundedRectangle(cornerRadius: 20))
    }
}

#endif
