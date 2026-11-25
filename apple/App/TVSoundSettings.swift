#if os(tvOS)
import SwiftUI
import YTMDLCore

/// "Klang & Wiedergabe" on Apple TV: the iPhone's sound settings, through the
/// same `PlayerModel` and `EqualizerModel` and so the same stored preferences.
/// tvOS has no slider. Every value is a `TVChoiceRow`: title and current value,
/// opening a list of the choices. (In CI, −/+ buttons in a Form row never took
/// focus, a pushed Picker showed an empty list, and a menu Picker hid its title.)
/// The timer and tempo come first because a listener on the sofa changes
/// them most.
struct TVSoundSettings: View {
    var model: AppModel
    private var player: PlayerModel { model.player }
    private var equalizer: EqualizerModel { player.equalizer }
    var body: some View {
        Form {
            Section("Wiedergabe") {
                TVChoiceRow(title: "Sleep-Timer", identifier: "tv-sound-sleep", selection: player.sleepMode,
                            choices: SleepMode.allCases.map { ($0, $0.name) }) { player.setSleepMode($0) }
                if let deadline = player.sleepDeadline {
                    LabeledContent("Verbleibend") { TVCountdown(deadline: deadline) }
                        .accessibilityIdentifier("tv-sound-sleep-remaining")
                }
                TVChoiceRow(title: "Tempo", identifier: "tv-sound-speed", selection: player.playbackRate,
                            choices: SoundStep.playbackRates.map { ($0, Self.rate($0)) }) { player.setPlaybackRate($0) }
                TVChoiceRow(title: "Überblendung", identifier: "tv-sound-crossfade", selection: player.crossfadeSeconds,
                            choices: Self.steps(player.crossfadeSeconds, SoundStep.crossfade, Self.crossfade)) { player.setCrossfade($0) }
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
                TVChoiceRow(title: "Klangprofil", identifier: "tv-sound-preset", selection: equalizer.preset,
                            choices: Self.presets(equalizer.preset)) {
                    if let preset = EqualizerPreset(rawValue: $0) { equalizer.select(preset) }
                }
                if equalizer.enabled {
                    ForEach(0..<10, id: \.self) { band in
                        TVChoiceRow(title: Self.frequency(band), identifier: "tv-sound-band-\(band)", selection: equalizer.gains[band],
                                    choices: Self.steps(equalizer.gains[band], SoundStep.equalizerGain, Self.decibels)) { equalizer.setGain($0, band: band) }
                    }
                    TVChoiceRow(title: "Vorverstärkung", identifier: "tv-sound-preamp", selection: equalizer.preamp,
                                choices: Self.steps(equalizer.preamp, SoundStep.preamp, Self.decibels)) { equalizer.setPreamp($0) }
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

    /// The 1-unit steps of `range` with their labels; an off-grid current value stays a choice.
    static func steps(_ current: Double, _ range: ClosedRange<Double>, _ text: (Double) -> String) -> [(Double, String)] {
        SoundStep.options(around: current, step: 1, in: range).map { ($0, text($0)) }
    }
    /// The presets; "Eigener Klang" only while the bands were changed by hand.
    static func presets(_ current: String) -> [(String, String)] {
        var list = EqualizerPreset.allCases.map { ($0.rawValue, $0.name) }
        if current == "custom" { list.append(("custom", "Eigener Klang")) }
        return list
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

/// One setting as a row: its title on the left, the current value on the
/// right. Selecting it opens `TVChoiceList`; the identifier sits on the link
/// and its accessibility value is the current choice.
struct TVChoiceRow<Value: Hashable>: View {
    let title: String
    let identifier: String
    let selection: Value
    let choices: [(Value, String)]
    let choose: (Value) -> Void
    private var current: String { choices.first { $0.0 == selection }?.1 ?? "" }
    var body: some View {
        NavigationLink {
            TVChoiceList(title: title, identifier: identifier, selection: selection, choices: choices, choose: choose)
        } label: {
            LabeledContent(title, value: current)
        }
        .accessibilityIdentifier(identifier)
        .accessibilityValue(current)
    }
}

/// The choices of a `TVChoiceRow`, one button each with a checkmark on the
/// selected one. Choosing sets the value and goes back to the settings.
struct TVChoiceList<Value: Hashable>: View {
    let title: String
    let identifier: String
    let selection: Value
    let choices: [(Value, String)]
    let choose: (Value) -> Void
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        List {
            ForEach(Array(choices.enumerated()), id: \.offset) { index, choice in
                Button {
                    choose(choice.0)
                    dismiss()
                } label: {
                    HStack {
                        Text(choice.1)
                        Spacer(minLength: 24)
                        if choice.0 == selection { Image(systemName: "checkmark").accessibilityHidden(true) }
                    }
                }
                .accessibilityLabel(choice.1)
                .accessibilityAddTraits(choice.0 == selection ? .isSelected : [])
                .accessibilityIdentifier("\(identifier)-choice-\(index)")
            }
        }
        .navigationTitle(title)
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
