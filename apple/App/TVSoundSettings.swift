#if os(tvOS)
import SwiftUI
import YTMDLCore

/// "Klang & Wiedergabe" on Apple TV: the iPhone's sound settings, through the
/// same `PlayerModel` and `EqualizerModel` and so the same stored preferences.
/// tvOS has no slider, so values and choices both use standard pickers in the
/// menu style; the pushed-list style showed an empty list in CI.
/// The timer and tempo come first because a listener on the sofa changes
/// them most.
struct TVSoundSettings: View {
    var model: AppModel
    private var player: PlayerModel { model.player }
    private var equalizer: EqualizerModel { player.equalizer }
    var body: some View {
        Form {
            Section("Wiedergabe") {
                Picker("Sleep-Timer", selection: Binding(get: { player.sleepMode }, set: { player.setSleepMode($0) })) {
                    ForEach(SleepMode.allCases) { Text($0.name).tag($0) }
                }.pickerStyle(.menu).accessibilityIdentifier("tv-sound-sleep")
                if let deadline = player.sleepDeadline {
                    LabeledContent("Verbleibend") { TVCountdown(deadline: deadline) }
                        .accessibilityIdentifier("tv-sound-sleep-remaining")
                }
                Picker("Tempo", selection: Binding(get: { player.playbackRate }, set: { player.setPlaybackRate($0) })) {
                    ForEach(SoundStep.playbackRates, id: \.self) { Text(Self.rate($0)).tag($0) }
                }.pickerStyle(.menu).accessibilityIdentifier("tv-sound-speed")
                TVValuePicker(title: "Überblendung", identifier: "tv-sound-crossfade", current: player.crossfadeSeconds,
                              step: 1, range: SoundStep.crossfade, text: Self.crossfade) { player.setCrossfade($0) }
                Toggle("Albentitel ohne Überblendung", isOn: Binding(get: { player.smartAlbumTransition }, set: { player.setSmartAlbumTransition($0) }))
                Toggle("Nächsten Titel vorladen", isOn: Binding(get: { player.preloadEnabled }, set: { player.setPreload($0) }))
                Toggle("Schneller Abspielstart", isOn: Binding(get: { player.fastStart }, set: { player.setFastStart($0) }))
                Toggle("Automatisch weiterspielen", isOn: Binding(get: { player.autoplay }, set: { player.setAutoplay($0) }))
            }
            Section {
                Toggle("Lautstärke-Normalisierung", isOn: Binding(get: { player.normalizationEnabled }, set: { player.setNormalization($0) }))
                    .accessibilityIdentifier("tv-sound-normalization")
                if player.normalizationEnabled {
                    Text(player.normalizationMessage).font(.callout).foregroundStyle(.secondary)
                    Button("Lautheit erneut prüfen") { player.setNormalization(true) }.disabled(model.offlineMode)
                }
            } header: { Text("Lautstärke") } footer: {
                Text("Gleicht Titel mit den Lautheitswerten deines Servers an. Fehlt ein Wert, läuft der Titel unverändert.")
            }
            Section {
                Toggle("Equalizer aktivieren", isOn: Binding(get: { equalizer.enabled }, set: { equalizer.setEnabled($0) }))
                    .accessibilityIdentifier("tv-sound-eq")
                Picker("Klangprofil", selection: Binding(get: { equalizer.preset }, set: { if let preset = EqualizerPreset(rawValue: $0) { equalizer.select(preset) } })) {
                    ForEach(EqualizerPreset.allCases) { Text($0.name).tag($0.rawValue) }
                    if equalizer.preset == "custom" { Text("Eigener Klang").tag("custom") }
                }.pickerStyle(.menu)
                if equalizer.enabled {
                    ForEach(0..<10, id: \.self) { band in
                        TVValuePicker(title: Self.frequency(band), identifier: "tv-sound-band-\(band)", current: equalizer.gains[band],
                                      step: 1, range: SoundStep.equalizerGain, text: Self.decibels) { equalizer.setGain($0, band: band) }
                    }
                    TVValuePicker(title: "Vorverstärkung", identifier: "tv-sound-preamp", current: equalizer.preamp,
                                  step: 1, range: SoundStep.preamp, text: Self.decibels) { equalizer.setPreamp($0) }
                    Toggle("Automatischer EQ-Pegelschutz", isOn: Binding(get: { equalizer.headroom }, set: { equalizer.setHeadroom($0) }))
                    Button("Klang zurücksetzen") { equalizer.select(.flat); equalizer.setPreamp(0) }
                }
                if let soundError = player.soundError { Text(soundError).font(.callout).foregroundStyle(.secondary) }
                if equalizer.enabled && player.equalizerFormat == 2 {
                    Text("Das Ausgabeformat unterstützt diesen Equalizer nicht. Die Wiedergabe läuft im Bypass.").font(.callout).foregroundStyle(.secondary)
                }
            } header: { Text("Equalizer") } footer: {
                Text("Ist der Equalizer ausgeschaltet, läuft die Wiedergabe im Bypass; deine Werte bleiben gespeichert.")
            }
        }
        .navigationTitle("Klang & Wiedergabe")
    }

    nonisolated static func rate(_ value: Double) -> String { String(format: "%g×", value) }
    nonisolated static func crossfade(_ seconds: Double) -> String { seconds == 0 ? "Aus" : "\(Int(seconds)) s" }
    nonisolated static func decibels(_ value: Double) -> String { String(format: "%+.1f dB", value) }
    static func frequency(_ band: Int) -> String { String(format: "%g Hz", EqualizerModel.frequencies[band]) }
    /// One line for the settings overview: what is not at its default.
    static func summary(_ player: PlayerModel) -> String {
        var parts: [String] = []
        if player.sleepMode != .off { parts.append("Sleep-Timer") }
        if player.playbackRate != 1 { parts.append(rate(player.playbackRate)) }
        if player.crossfadeSeconds > 0 { parts.append("Überblendung \(crossfade(player.crossfadeSeconds))") }
        if player.normalizationEnabled { parts.append("Normalisierung") }
        if player.equalizer.enabled { parts.append("EQ") }
        return parts.isEmpty ? "Standard" : parts.joined(separator: " · ")
    }
}

/// A value chosen from its steps with a standard tvOS menu picker, since tvOS has
/// no slider. (−/+ buttons inside a Form row cannot take the remote's focus.)
/// A value off the grid, such as an older finer setting, stays selectable.
struct TVValuePicker: View {
    let title: String
    let identifier: String
    let current: Double
    let step: Double
    let range: ClosedRange<Double>
    let text: (Double) -> String
    let set: (Double) -> Void
    var body: some View {
        Picker(title, selection: Binding(get: { current }, set: { set($0) })) {
            ForEach(SoundStep.options(around: current, step: step, in: range), id: \.self) { Text(text($0)).tag($0) }
        }.pickerStyle(.menu).accessibilityIdentifier(identifier)
    }
}

/// The time left on a minute timer, counting down.
struct TVCountdown: View {
    let deadline: Date
    var body: some View {
        let now = Date.now
        Text(timerInterval: now...max(now, deadline), countsDown: true).monospacedDigit()
    }
}

/// Quick access under the player's transport: sleep timer, tempo and the
/// current title's actions, each a standard menu.
struct TVPlayerQuickActions: View {
    var model: AppModel
    var track: Track
    @State private var adding = false
    private var player: PlayerModel { model.player }
    var body: some View {
        HStack(spacing: 24) {
            Menu {
                Picker("Sleep-Timer", selection: Binding(get: { player.sleepMode }, set: { player.setSleepMode($0) })) {
                    ForEach(SleepMode.allCases) { Text($0.name).tag($0) }
                }
            } label: {
                Label { sleepLabel } icon: { Image(systemName: "moon.zzz") }
            }
            .accessibilityLabel("Sleep-Timer")
            .accessibilityValue(player.sleepMode.name)
            .accessibilityIdentifier("tv-player-sleep")
            Menu {
                Picker("Tempo", selection: Binding(get: { player.playbackRate }, set: { player.setPlaybackRate($0) })) {
                    ForEach(SoundStep.playbackRates, id: \.self) { Text(TVSoundSettings.rate($0)).tag($0) }
                }
            } label: {
                Label(TVSoundSettings.rate(player.playbackRate), systemImage: "speedometer")
            }
            .accessibilityLabel("Tempo")
            .accessibilityValue(TVSoundSettings.rate(player.playbackRate))
            .accessibilityIdentifier("tv-player-speed")
            if !model.offlineMode {
                Menu {
                    Button("Song-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startRadio(track) } }
                    Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { adding = true }
                    Button("Wiedergabe übergeben", systemImage: "arrow.up.forward.app") { Task { await model.saveHandoff() } }
                } label: { Image(systemName: "ellipsis") }
                    .accessibilityLabel("Titel-Aktionen")
                    .accessibilityIdentifier("tv-player-actions")
            }
        }
        .frame(maxWidth: .infinity)
        .sheet(isPresented: $adding) { AddToPlaylistSheet(model: model, tracks: [track]) }
    }
    @ViewBuilder private var sleepLabel: some View {
        if let deadline = player.sleepDeadline { TVCountdown(deadline: deadline) }
        else if player.sleepMode == .off { Text("Sleep-Timer") }
        else { Text(player.sleepMode.name) }
    }
}
#endif
