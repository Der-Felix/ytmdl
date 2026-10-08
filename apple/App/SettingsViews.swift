import SwiftUI
import CoreImage.CIFilterBuiltins
import YTMDLCore

struct SettingsView: View {
    var model: AppModel
    @State private var signingOut = false
    @State private var removeOfflineOnLogout = false
    @AppStorage("mobileAccent") private var mobileAccent = "rose"
    @AppStorage("appearance") private var appearance = "dark"
    #if os(macOS)
    @AppStorage("desktopTheme") private var themeName = "rose"
    @AppStorage("desktopTextSize") private var textSize = "large"
    @AppStorage("desktopCoverSize") private var coverSize = 260.0
    @AppStorage("desktopStartView") private var startView = "Start"
    @AppStorage("homeShowFavorites") private var showFavorites = true
    @AppStorage("homeShowArtists") private var showArtists = true
    @AppStorage("homeShowPlaylists") private var showPlaylists = true
    @AppStorage("homeShowRecent") private var showRecent = true
    @AppStorage("playerCoverColors") private var coverColors = true
    @Environment(\.desktopTheme) private var theme
    @Environment(\.desktopAccent) private var accent
    @Environment(\.colorScheme) private var scheme
    @State private var settingsTab = "Darstellung"
    #endif
    var body: some View {
        Group {
            #if os(macOS)
            desktopSettings
            #else
            Form {
                Section("Darstellung") {
                    Picker("Erscheinungsbild", selection: $appearance) { Text("System").tag("system"); Text("Dunkel").tag("dark"); Text("Hell").tag("light") }
                }
                #if os(iOS)
                Section("Musik & Wiedergabe") {
                    NavigationLink("Equalizer, Überblendung & Visualizer", destination: MobileAudioSettings(model: model))
                    NavigationLink("Offline-Musik", destination: OfflineLibraryView(model: model))
                    Toggle("Lokalen Hörverlauf merken", isOn: Binding(get: { model.listeningHistory.enabled }, set: { model.listeningHistory.setEnabled($0) }))
                    Toggle("Hörverlauf mit meinem Server synchronisieren", isOn: Binding(get: { model.syncHistory }, set: { model.setSyncHistory($0) }))
                    Text("Ermöglicht häufig gehörte und zuletzt gehörte intelligente Playlists. Offline-Einträge werden bei erneuter Anmeldung an dasselbe Konto übertragen.").font(.footnote).foregroundStyle(.secondary)
                    Button("Lokalen Hörverlauf leeren", role: .destructive) { model.listeningHistory.clear() }
                    Button("Wiedergabe von anderem Gerät prüfen", systemImage: "arrow.down.forward.and.arrow.up.backward") { Task { await model.checkHandoff() } }
                    if let handoff = model.handoff {
                        Text("\(handoff.sourceName) · \(handoff.queue?.count ?? 0) Titel").font(.caption).foregroundStyle(.secondary)
                        Button("Wiedergabe hier fortsetzen") { model.acceptHandoff() }
                    }
                }
                DownloadPreferences(model: model)
                Section("Theme") { Picker("Akzentfarbe", selection: $mobileAccent) { ForEach(DesktopTheme.allCases) { Text($0.name).tag($0.rawValue) } } }
                #endif
                Section("Verbindung") {
                    LabeledContent("Server", value: model.client?.server.url.absoluteString ?? "")
                    LabeledContent("Konto", value: model.user?.displayName ?? "")
                    if model.client?.server.isSecure == false { Text("Entwicklungsmodus: lokale HTTP-Verbindung ohne Verschlüsselung.").foregroundStyle(.orange) }
                    if !model.offlineMode { Button("Bibliothek aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } } }
                }
                #if !os(tvOS)
                if !model.offlineMode { Section("Anderes Gerät anmelden") { ConfirmDeviceView(model: model) } }
                if let url = model.client?.server.url.appendingPathComponent("profile") {
                    Section("Verwaltung") { Link("Profil & Sicherheit im Web öffnen", destination: url) }
                }
                #endif
                Section("Über YTMDL") {
                    LabeledContent("App-Version", value: (Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "–") + " (" + (Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "–") + ")")
                }
                Section("Datenschutz") {
                    Text("Die App verbindet sich nur mit deinem YTMDL-Server. Keine Werbung, keine Analyse-SDKs. Anmeldesitzungen werden gerätegebunden im Schlüsselbund gespeichert. Cover und Musik werden vom Server geladen.")
                    Text("App-Vorschau · Bibliothek, Playlists, Offline-Musik und Wiedergabe. Offline-Kopien werden auf diesem Gerät gespeichert. Die Server-Verwaltung bleibt im Web.").font(.footnote).foregroundStyle(.secondary)
                }
                Section {
                    #if os(iOS)
                    Toggle("Offline-Musik beim Abmelden entfernen", isOn: $removeOfflineOnLogout)
                    Text("Ohne diese Option bleiben Musik und Metadaten lokal verfügbar, auch nach dem Abmelden.").font(.footnote).foregroundStyle(.secondary)
                    #endif
                    Button("Abmelden", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { signingOut = true }
                }
            }
            #endif
        }.navigationTitle("Einstellungen")
        .confirmationDialog("Auf diesem Gerät abmelden?", isPresented: $signingOut) {
            Button("Abmelden", role: .destructive) { if removeOfflineOnLogout { model.player.stop(); model.offline.clearCurrent() }; Task { await model.logout() } }
        } message: { Text("Die Wiedergabe stoppt. Die lokal gespeicherte Sitzung wird entfernt.") }
    }
    #if os(macOS)
    private var desktopSettings: some View {
        VStack(alignment: .leading, spacing: 24) {
            VStack(alignment: .leading, spacing: 10) {
                Text("Deine App").desktopScaledFont(30, weight: .bold)
                Text("Themes, Lesbarkeit, Startseite und Wiedergabe.").desktopScaledFont(18).foregroundStyle(.secondary)
            }
            ScrollView(.horizontal) {
                HStack(spacing: 12) {
                    ForEach(["Darstellung", "Startseite", "Wiedergabe", "Klang", "Konto"], id: \.self) { tab in
                        Button { settingsTab = tab } label: {
                            Text(tab).desktopScaledFont(18, weight: .semibold).padding(.horizontal, 18).padding(.vertical, 14)
                                .background(settingsTab == tab ? accent.opacity(0.18) : theme.surface(scheme), in: RoundedRectangle(cornerRadius: 14))
                        }.buttonStyle(DesktopHoverStyle()).accessibilityAddTraits(settingsTab == tab ? .isSelected : [])
                    }
                }
            }
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    switch settingsTab {
                    case "Darstellung": appearanceSettings
                    case "Startseite": homeSettings
                    case "Wiedergabe": playbackSettings
                    case "Klang": settingsCard("Equalizer & Klang", icon: "slider.vertical.3") { EqualizerControls(player: model.player) }
                    default: accountSettings
                    }
                    Text(appVersion).desktopScaledFont(14).foregroundStyle(.secondary)
                }.padding(.bottom, 24)
            }
        }.desktopScaledFont(18).padding(32).frame(maxWidth: 1120, alignment: .leading).frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
        #if DEBUG
        .task {
            let args = ProcessInfo.processInfo.arguments
            if args.contains("--fixture-server"), ["127.0.0.1", "localhost"].contains(model.client?.server.url.host ?? ""),
               let index = args.firstIndex(of: "--fixture-settings-tab"), args.indices.contains(index + 1),
               ["Darstellung", "Startseite", "Wiedergabe", "Klang", "Konto"].contains(args[index + 1]) { settingsTab = args[index + 1] }
        }
        #endif
    }
    private var appearanceSettings: some View {
        VStack(spacing: 24) {
            settingsCard("Dein Theme", icon: "paintpalette") {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 200, maximum: 350), spacing: 18)], spacing: 18) {
                    ForEach(DesktopTheme.allCases) { option in
                        Button { themeName = option.rawValue } label: {
                            VStack(alignment: .leading, spacing: 12) {
                                HStack(spacing: 8) {
                                    RoundedRectangle(cornerRadius: 6).fill(option.accent(scheme)).frame(width: 24)
                                    VStack(alignment: .leading, spacing: 9) {
                                        Capsule().fill(option.accent(scheme)).frame(width: 65, height: 9)
                                        HStack(spacing: 8) { ForEach(0..<3) { _ in RoundedRectangle(cornerRadius: 5).fill(option.accent(scheme).opacity(0.35)).frame(height: 33) } }
                                    }
                                }.padding(14).frame(height: 100).background(option.background(scheme), in: RoundedRectangle(cornerRadius: 12))
                                HStack {
                                    Text(option.name).desktopScaledFont(18, weight: .semibold).foregroundStyle(.primary)
                                    Spacer()
                                    if themeName == option.rawValue { Image(systemName: "checkmark.circle.fill").foregroundStyle(accent) }
                                }
                            }.padding(12).background(themeName == option.rawValue ? accent.opacity(0.1) : .clear, in: RoundedRectangle(cornerRadius: 16))
                                .overlay(RoundedRectangle(cornerRadius: 16).stroke(themeName == option.rawValue ? accent : Color.primary.opacity(0.12), lineWidth: themeName == option.rawValue ? 2 : 1))
                        }.buttonStyle(DesktopHoverStyle(radius: 16)).accessibilityLabel("Theme \(option.name)").accessibilityAddTraits(themeName == option.rawValue ? .isSelected : [])
                    }
                }
                Picker("Helligkeit", selection: $appearance) {
                    Text("System").tag("system"); Text("Dunkel").tag("dark"); Text("Hell").tag("light")
                }.pickerStyle(.segmented).controlSize(.large).frame(maxWidth: 480)
            }
            settingsCard("Schrift & Cover", icon: "textformat.size") {
                Toggle("Player-Farben aus dem aktuellen Cover", isOn: $coverColors)
                Picker("Schriftgröße", selection: $textSize) {
                    ForEach(DesktopTextSize.allCases) { Text($0.name).tag($0.rawValue) }
                }.pickerStyle(.segmented).controlSize(.large).frame(maxWidth: 580)
                Text("Die Schriftgröße gilt auch für die Seitenleiste. Änderungen sind sofort sichtbar.").desktopScaledFont(16).foregroundStyle(.secondary)
                HStack {
                    Text("Cover in der Bibliothek")
                    Spacer()
                    Text("\(Int(coverSize.isFinite ? coverSize : 260)) pt").monospacedDigit().foregroundStyle(.secondary)
                }
                Slider(value: $coverSize, in: 220...340, step: 20).accessibilityLabel("Covergröße in der Bibliothek")
                Text("Größere Cover zeigen weniger Alben pro Reihe.").desktopScaledFont(16).foregroundStyle(.secondary)
            }
        }
    }
    private var homeSettings: some View {
        VStack(spacing: 24) {
            settingsCard("Beim Öffnen", icon: "house") {
                Picker("Startansicht", selection: $startView) {
                    ForEach([Destination.home, .library, .favorites, .player]) { Text($0.rawValue).tag($0.rawValue) }
                }.pickerStyle(.menu).controlSize(.large).frame(maxWidth: 440, alignment: .leading)
                Text("Diese Ansicht öffnet sich beim nächsten App-Start nach der Anmeldung.").desktopScaledFont(16).foregroundStyle(.secondary)
            }
            settingsCard("Dein Musik-Feed", icon: "rectangle.grid.1x2") {
                Toggle("Zuletzt gehört", isOn: $showRecent)
                Toggle("Lieblingstitel", isOn: $showFavorites)
                Toggle("Playlists", isOn: $showPlaylists)
                Toggle("Künstler aus deiner Sammlung", isOn: $showArtists)
                Text("Neue Alben und die schnellen Einstiege bleiben auf der Startseite sichtbar.").desktopScaledFont(16).foregroundStyle(.secondary)
            }
        }
    }
    private var playbackSettings: some View {
        VStack(spacing: 24) {
            settingsCard("Wiedergabe", icon: "speaker.wave.2") {
                ViewThatFits(in: .horizontal) {
                    HStack { Text("App-Lautstärke").fixedSize(); Spacer(); DesktopVolumeControl(player: model.player).frame(width: 260) }
                    VStack(alignment: .leading, spacing: 12) { Text("App-Lautstärke"); DesktopVolumeControl(player: model.player) }
                }
                Toggle("Warteschlange wiederholen", isOn: Binding(get: { model.player.repeatAll }, set: { model.player.repeatAll = $0 }))
                Text("Die Systemlautstärke stellst du am Mac ein. AirPlay findest du im Player und in der Wiedergabeleiste.").desktopScaledFont(16).foregroundStyle(.secondary)
            }
            settingsCard("Lokaler Hörverlauf", icon: "clock.arrow.circlepath") {
                Toggle("Gehörte Titel auf diesem Mac merken", isOn: Binding(get: { model.listeningHistory.enabled }, set: { model.listeningHistory.setEnabled($0) }))
                Text("Bis zu 40 Titel, getrennt nach Server und Konto. Ausgeschaltet werden keine neuen Titel gespeichert; dein bisheriger Verlauf bleibt erhalten und kann gelöscht werden.").desktopScaledFont(16).foregroundStyle(.secondary)
                Button("Hörverlauf löschen", systemImage: "trash", role: .destructive) { model.listeningHistory.clear() }
                    .buttonStyle(.bordered).controlSize(.large).disabled(model.listeningHistory.tracks.isEmpty)
            }
            settingsCard("Übergänge & Timer", icon: "waveform") { PlaybackOptions(player: model.player) }
            settingsCard("Tastenkürzel", icon: "keyboard") {
                shortcut("Suche", keys: "⌘F")
                shortcut("Wiedergabe / Pause", keys: "Leertaste")
                shortcut("Vorheriger / nächster Titel", keys: "⌘← / ⌘→")
                shortcut("Lautstärke", keys: "⌘↑ / ⌘↓")
                shortcut("Stummschalten", keys: "⇧⌘M")
                shortcut("Player verlassen", keys: "Esc")
            }
        }
    }
    private var accountSettings: some View {
        VStack(spacing: 24) {
            settingsCard("Verbindung", icon: "network") {
                Text(model.user?.displayName ?? "").desktopScaledFont(22, weight: .semibold)
                Text(model.client?.server.url.absoluteString ?? "").foregroundStyle(.secondary).textSelection(.enabled)
                if model.client?.server.isSecure == false { Label("Lokaler HTTP-Test: Verbindung ohne Verschlüsselung", systemImage: "exclamationmark.shield").desktopScaledFont(16).foregroundStyle(.orange) }
                Button("Bibliothek aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }
                    .buttonStyle(.bordered).controlSize(.large).disabled(model.connecting)
            }
            settingsCard("Anderes Gerät anmelden", icon: "appletv") {
                ConfirmDeviceView(model: model).textFieldStyle(.roundedBorder).controlSize(.large).frame(maxWidth: 650, alignment: .leading)
            }
            settingsCard("Konto & Datenschutz", icon: "lock.shield") {
                if let url = model.client?.server.url.appendingPathComponent("profile") { Link("Profil & Sicherheit im Web öffnen", destination: url) }
                Toggle("Hörverlauf mit meinem Server synchronisieren", isOn: Binding(get: { model.syncHistory }, set: { model.setSyncHistory($0) }))
                Text("Nur dein YTMDL-Server. Keine Werbung, keine Analyse-SDKs. Deine Sitzung bleibt im Schlüsselbund dieses Geräts. Darstellung und lokale Wiedergabelisten bleiben auf dem Gerät; aktivierter Hörverlauf wird mit deinem eigenen Server synchronisiert.").foregroundStyle(.secondary)
                Button("Abmelden", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { signingOut = true }.buttonStyle(.bordered).controlSize(.large)
            }
        }
    }
    private func shortcut(_ title: String, keys: String) -> some View {
        HStack { Text(title); Spacer(); Text(keys).monospaced().foregroundStyle(.secondary) }
    }
    private var appVersion: String {
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0.1.0"
        let build = Bundle.main.object(forInfoDictionaryKey: "CFBundleVersion") as? String ?? "–"
        return "YTMDL für Mac · Vorschau \(version) (\(build))"
    }
    private func settingsCard<Content: View>(_ title: String, icon: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            Label(title, systemImage: icon).desktopScaledFont(21, weight: .semibold)
            content()
        }.padding(24).frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.surface(scheme), in: RoundedRectangle(cornerRadius: 20))
    }
    #endif

}

struct ConfirmDeviceView: View {
    var model: AppModel
    @State private var code = ""
    @State private var preview: DevicePreview?
    @State private var busy = false
    @State private var message: String?
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Gib den Code ein, der auf deinem Apple TV angezeigt wird.").foregroundStyle(.secondary)
            TextField("Gerätecode · ABCD-EFGH", text: $code)
                .disabled(busy)
                #if os(iOS)
                .textInputAutocapitalization(.characters).autocorrectionDisabled()
                #endif
                // Clearing the field after an approval is not an edit: keep its confirmation.
                .onChange(of: code) { preview = nil; if !code.isEmpty { message = nil } }
            Button("Gerät prüfen") {
                Task {
                    guard let client = model.client else { return }
                    busy = true; defer { busy = false }
                    let submittedCode = code
                    do {
                        let result: DevicePreview = try await client.send("/auth/device/preview", body: ["user_code": submittedCode])
                        guard model.client === client, code == submittedCode else { return }
                        preview = result
                    }
                    catch {
                        guard model.client === client else { return }
                        if case PlayerError.server(let status, _, _) = error, status == 404 {
                            message = "Dieser Server unterstützt Gerätecodes noch nicht. Die Anmeldung mit Benutzername und Passwort funktioniert weiterhin."
                        } else { model.report(error) }
                    }
                }
            }.disabled(busy || code.trimmingCharacters(in: .whitespacesAndNewlines).count < 8)
            if let preview {
                Text("\(preview.deviceName) anmelden?").font(.headline)
                Text("Bestätige nur einen Code, den du selbst auf deinem Gerät angefordert hast. Das Gerät erhält Zugriff auf dieses Konto und erscheint unter den aktiven Sitzungen.").font(.callout).foregroundStyle(.secondary)
                if model.user?.role == "admin" { Text("Dein Konto hat Verwaltungsrechte. Diese gelten auch für die neue Gerätesitzung.").font(.callout).foregroundStyle(.secondary) }
                Button("Dieses Gerät ausdrücklich freigeben") {
                    Task {
                        guard let client = model.client else { return }
                        busy = true; defer { busy = false }
                        do {
                            try await client.mutate("/auth/device/confirm", method: "POST", body: ["user_code": code])
                            guard model.client === client else { return }
                            self.preview = nil; code = ""; message = "Gerät freigegeben. Die Anmeldung wird auf dem TV abgeschlossen."
                        } catch { model.report(error) }
                    }
                }.buttonStyle(.borderedProminent).disabled(busy)
            }
            if let message { Text(message).foregroundStyle(.secondary) }
        }
    }
}

struct DeviceSignInView: View {
    var model: AppModel
    @State private var request: DeviceStart?
    @State private var message = "Gerätecode wird angefordert …"
    @State private var retry = UUID()
    var body: some View {
        VStack(alignment: .leading, spacing: 20) {
            if let request {
                Text(request.userCode).font(.system(size: 56, weight: .bold, design: .monospaced)).tracking(5)
                    #if !os(tvOS)
                    .textSelection(.enabled)
                    #endif
                Text("Auf einem angemeldeten iPhone, iPad oder Mac: Einstellungen → Anderes Gerät anmelden. Oder den QR-Code scannen und im Web bestätigen.").foregroundStyle(.secondary)
                if let base = model.client?.server.url,
                   var components = URLComponents(url: base.appendingPathComponent("profile"), resolvingAgainstBaseURL: false) {
                    let _ = components.queryItems = [.init(name: "device_code", value: request.userCode)]
                    if let url = components.url { QRCodeView(value: url.absoluteString).frame(width: 160, height: 160) }
                }
            }
            Text(message).font(.callout)
            Button("Neuen Code anfordern") { retry = UUID() }
        }.task(id: retry) { await authorize() }
    }
    private func authorize() async {
        request = nil
        guard let client = model.client else { return }
        do {
            let _: AuthStatus = try await client.get("/auth/status")
            let result: DeviceStart = try await client.send("/auth/device", body: ["device_name": "Apple TV"])
            try Task.checkCancellation(); request = result; message = "Code gilt fünf Minuten. Warte auf deine Freigabe …"
            let authorized = try await model.completeDeviceSignIn(result)
            request = nil
            if !authorized { message = "Der Code ist abgelaufen. Bitte einen neuen anfordern." }
        } catch is CancellationError { }
        catch {
            request = nil
            if case PlayerError.server(let status, _, _) = error, status == 404 {
                message = "Dieser Server unterstützt die Geräte-Anmeldung noch nicht. Bitte zuerst das Backend mit Gerätecodes aktualisieren."
            } else { message = "Geräte-Anmeldung nicht möglich. Bitte Verbindung prüfen und einen neuen Code anfordern." }
        }
    }
}

struct QRCodeView: View {
    let value: String
    private var image: CGImage? {
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(value.utf8); filter.correctionLevel = "M"
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)) else { return nil }
        return CIContext().createCGImage(output, from: output.extent)
    }
    var body: some View {
        if let image {
            Image(decorative: image, scale: 1).interpolation(.none).resizable().scaledToFit().padding(12).background(.white).clipShape(RoundedRectangle(cornerRadius: 12))
                .accessibilityLabel("QR-Code zur Geräte-Anmeldung auf deinem Server")
        }
    }
}
