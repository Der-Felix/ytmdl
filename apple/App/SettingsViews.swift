import SwiftUI
import CoreImage.CIFilterBuiltins
import YTMDLCore

struct SettingsView: View {
    var model: AppModel
    @State private var signingOut = false
    @AppStorage("appearance") private var appearance = "dark"
    var body: some View {
        Form {
            Section("Darstellung") {
                Picker("Erscheinungsbild", selection: $appearance) { Text("System").tag("system"); Text("Dunkel").tag("dark"); Text("Hell").tag("light") }
            }
            Section("Verbindung") {
                LabeledContent("Server", value: model.client?.server.url.absoluteString ?? "")
                LabeledContent("Konto", value: model.user?.displayName ?? "")
                if model.client?.server.isSecure == false { Text("Entwicklungsmodus: lokale HTTP-Verbindung ohne Verschlüsselung.").foregroundStyle(.orange) }
                Button("Bibliothek aktualisieren", systemImage: "arrow.clockwise") { Task { await model.loadLibrary() } }
            }
            #if !os(tvOS)
            Section("Anderes Gerät anmelden") { ConfirmDeviceView(model: model) }
            if let url = model.client?.server.url.appendingPathComponent("profile") {
                Section("Verwaltung") { Link("Profil & Sicherheit im Web öffnen", destination: url) }
            }
            #endif
            Section("Datenschutz") {
                Text("Die App verbindet sich nur mit deinem YTMDL-Server. Keine Werbung, keine Analyse-SDKs. Anmeldesitzungen werden gerätegebunden im Schlüsselbund gespeichert. Cover und Musik werden vom Server geladen.")
                Text("App-Vorschau 0.1 · Bibliothek, Suche und Wiedergabe. Die Verwaltung und dauerhafte Offline-Kopien erfolgen weiterhin im Web.").font(.footnote).foregroundStyle(.secondary)
            }
            Section {
                Button("Abmelden", systemImage: "rectangle.portrait.and.arrow.right", role: .destructive) { signingOut = true }
            }
        }.navigationTitle("Einstellungen")
        .confirmationDialog("Auf diesem Gerät abmelden?", isPresented: $signingOut) {
            Button("Abmelden", role: .destructive) { Task { await model.logout() } }
        } message: { Text("Die Wiedergabe stoppt. Die lokal gespeicherte Sitzung wird entfernt.") }
    }
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
                .onChange(of: code) { preview = nil; message = nil }
            Button("Gerät prüfen") {
                Task {
                    guard let client = model.client else { return }
                    busy = true; defer { busy = false }
                    let submittedCode = code
                    do {
                        let result: DevicePreview = try await client.send("/auth/device/preview", body: ["user_code": submittedCode])
                        guard code == submittedCode else { return }
                        preview = result
                    }
                    catch { model.report(error) }
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
            let deadline = Date().addingTimeInterval(Double(result.expiresIn))
            var interval = max(5, result.interval)
            while Date() < deadline {
                try await Task.sleep(for: .seconds(interval)); try Task.checkCancellation()
                let state: DevicePoll = try await client.send("/auth/device/poll", body: ["device_code": result.deviceCode])
                try Task.checkCancellation()
                if state.status == "authorized" { request = nil; await model.restore(); return }
                if state.status == "slow_down" { interval = min(30, interval+5) }
            }
            request = nil; message = "Der Code ist abgelaufen. Bitte einen neuen anfordern."
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
