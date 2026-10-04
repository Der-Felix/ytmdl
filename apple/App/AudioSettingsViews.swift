import SwiftUI

#if os(macOS)
struct EqualizerControls: View {
    var player: PlayerModel
    var inline = false
    @Environment(\.desktopAccent) private var accent
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            Toggle("Equalizer aktivieren", isOn: Binding(get: { player.equalizer.enabled }, set: { player.equalizer.setEnabled($0) }))
            if inline {
                HStack(spacing: 16) {
                    Text("Klangprofil").desktopScaledFont(19)
                    Menu {
                        ForEach(EqualizerPreset.allCases) { preset in
                            Button(preset.name) { player.equalizer.select(preset) }
                        }
                    } label: {
                        HStack(spacing: 12) {
                            Text(EqualizerPreset(rawValue: player.equalizer.preset)?.name ?? "Eigener Klang")
                            Image(systemName: "chevron.down")
                        }.desktopScaledFont(18, weight: .medium).padding(.horizontal, 16).frame(height: 44)
                            .background(Color.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 12))
                    }.menuStyle(.button).menuIndicator(.hidden).buttonStyle(DesktopHoverStyle(radius: 12)).accessibilityLabel("Klangprofil wählen")
                    Spacer(minLength: 0)
                    Button { player.equalizer.select(.flat); player.equalizer.setPreamp(0) } label: {
                        Image(systemName: "arrow.counterclockwise").desktopScaledFont(20).frame(width: 44, height: 44)
                    }.buttonStyle(DesktopControlStyle()).accessibilityLabel("Equalizer zurücksetzen").help("Alle Frequenzbänder und Vorverstärkung auf 0 dB")
                }
            } else {
                HStack {
                    Picker("Klangprofil", selection: Binding(get: { player.equalizer.preset }, set: { if let preset = EqualizerPreset(rawValue: $0) { player.equalizer.select(preset) } })) {
                        ForEach(EqualizerPreset.allCases) { Text($0.name).tag($0.rawValue) }
                        if player.equalizer.preset == "custom" { Text("Eigener Klang").tag("custom") }
                    }.controlSize(.large)
                    Button("Zurücksetzen", systemImage: "arrow.counterclockwise") { player.equalizer.select(.flat); player.equalizer.setPreamp(0) }
                        .labelStyle(.iconOnly).buttonStyle(.bordered).help("Alle Frequenzbänder und Vorverstärkung auf 0 dB")
                }
            }
            ScrollView(.horizontal) {
                HStack(spacing: 12) {
                    ForEach(0..<10) { band in
                        VStack(spacing: 12) {
                            Text(String(format: "%+.1f", player.equalizer.gains[band])).desktopScaledFont(inline ? 16 : 14).monospacedDigit().foregroundStyle(.secondary)
                            Slider(value: Binding(get: { player.equalizer.gains[band] }, set: { player.equalizer.setGain($0, band: band) }), in: -12...12, step: 0.5)
                                .frame(width: inline ? 180 : 150).rotationEffect(.degrees(-90)).frame(width: 30, height: inline ? 180 : 150)
                                .accessibilityLabel("\(EqualizerModel.frequencies[band]) Hertz").accessibilityValue("\(player.equalizer.gains[band]) Dezibel")
                            Text(frequency(band)).desktopScaledFont(inline ? 16 : 14, weight: .medium)
                        }.frame(width: inline ? 64 : 54)
                    }
                }.padding(.horizontal, 4).padding(.vertical, 12)
            }
            HStack { Text("Vorverstärkung"); Spacer(); Text(String(format: "%+.1f dB", player.equalizer.preamp)).monospacedDigit().foregroundStyle(.secondary) }
            Slider(value: Binding(get: { player.equalizer.preamp }, set: { player.equalizer.setPreamp($0) }), in: -12...6, step: 0.5).accessibilityLabel("Vorverstärkung")
            Toggle("Automatischer Pegelschutz", isOn: Binding(get: { player.equalizer.headroom }, set: { player.equalizer.setHeadroom($0) }))
            if !inline {
                Text("Gleicht angehobene Frequenzen mit zusätzlichem Headroom aus. Ist der Equalizer ausgeschaltet, läuft die Wiedergabe im Bypass; deine Werte bleiben gespeichert.")
                    .desktopScaledFont(15).foregroundStyle(.secondary)
            }
            if let soundError = player.soundError { Text(soundError).desktopScaledFont(15).foregroundStyle(.secondary) }
            if player.equalizer.enabled && player.equalizerFormat == 2 {
                Text("Das Ausgabeformat unterstützt diesen Equalizer nicht. Die Wiedergabe läuft im Bypass.").desktopScaledFont(15).foregroundStyle(.secondary)
            }
        }.desktopScaledFont(17).tint(accent)
    }
    private func frequency(_ band: Int) -> String {
        let value = EqualizerModel.frequencies[band]
        return value >= 1000 ? "\(Int(value / 1000))k" : String(format: "%g", value)
    }
}

struct PlaybackOptions: View {
    var player: PlayerModel
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack { Text("Überblendung"); Spacer(); Text(player.crossfadeSeconds == 0 ? "Aus" : "\(Int(player.crossfadeSeconds)) s").monospacedDigit().foregroundStyle(.secondary) }
            Slider(value: Binding(get: { player.crossfadeSeconds }, set: { player.setCrossfade($0) }), in: 0...12, step: 1).accessibilityLabel("Überblendung zwischen Titeln")
            Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { player.smartAlbumTransition }, set: { player.setSmartAlbumTransition($0) }))
            Text("Benachbarte Titel desselben Albums und derselben Künstler bleiben ohne Überlappung. Kurze Titel werden höchstens zur Hälfte überblendet.")
                .desktopScaledFont(15).foregroundStyle(.secondary)
            Toggle("Visualizer", isOn: Binding(get: { player.visualizationEnabled }, set: { player.setVisualization($0) }))
            if player.visualizationEnabled {
                VisualizerPreferences()
            }
            Text("Sechs Stile reagieren auf die Frequenzen der laufenden Musik. Wähle ein Cover-Overlay oder eine Ansicht ohne Cover. Im Player lässt sich die Anzeige vergrößern. Ohne EQ und Visualizer entfällt die Audioverarbeitung.").desktopScaledFont(15).foregroundStyle(.secondary)
            Toggle("Nächsten Titel vorladen", isOn: Binding(get: { player.preloadEnabled }, set: { player.setPreload($0) }))
            Text("Puffert nur den nächsten Titel. Eine aktive Überblendung braucht dieses Vorladen ebenfalls. Es entstehen keine dauerhaften Offline-Kopien.").desktopScaledFont(15).foregroundStyle(.secondary)
            Toggle("Schneller Abspielstart", isOn: Binding(get: { player.fastStart }, set: { player.setFastStart($0) }))
            Text("Beginnt mit verfügbaren Audiodaten. Bei schwachen Verbindungen ausschalten, um mehr vor dem Start zu puffern.").desktopScaledFont(15).foregroundStyle(.secondary)
            Picker("Geschwindigkeit", selection: Binding(get: { player.playbackRate }, set: { player.setPlaybackRate($0) })) {
                ForEach([0.5, 0.75, 1.0, 1.25, 1.5, 2.0], id: \.self) { Text(String(format: "%g×", $0)).tag($0) }
            }.controlSize(.large)
            Picker("Sleep-Timer", selection: Binding(get: { player.sleepMode }, set: { player.setSleepMode($0) })) {
                ForEach(SleepMode.allCases) { Text($0.name).tag($0) }
            }.controlSize(.large)
            if let deadline = player.sleepDeadline { Text("Stoppt um \(deadline.formatted(date: .omitted, time: .shortened)).").desktopScaledFont(15).foregroundStyle(.secondary) }
            Toggle("Diesen Titel wiederholen", isOn: Binding(get: { player.repeatOne }, set: { player.setRepeatOne($0) }))
            if player.isCrossfading { Label("Titel werden gerade überblendet", systemImage: "waveform").desktopScaledFont(15).foregroundStyle(.secondary) }
        }.desktopScaledFont(17)
    }
}
#endif
