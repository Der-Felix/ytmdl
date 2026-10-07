import SwiftUI

@main struct YTMDLApp: App {
    @State private var model = AppModel()
    #if os(iOS)
    @UIApplicationDelegateAdaptor(OfflineAppDelegate.self) private var delegate
    #endif
    @AppStorage("mobileAccent") private var mobileAccent = "rose"
    var body: some Scene {
        WindowGroup {
            RootView(model: model)
                #if os(tvOS)
                .tint(.white)
                #elseif os(iOS)
                .tint((DesktopTheme(rawValue: mobileAccent) ?? .rose).accent)
                #else
                .tint(.pink)
                #endif
                #if os(macOS)
                .frame(minWidth: 720, minHeight: 560)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1280, height: 860)
        .commands {
            CommandMenu("Wiedergabe") {
                Button("Wiedergabe / Pause") { model.player.toggle() }.keyboardShortcut(.space, modifiers: [])
                Button("Nächster Titel") { model.player.next() }.keyboardShortcut(.rightArrow, modifiers: .command)
                Button("Vorheriger Titel") { model.player.previous() }.keyboardShortcut(.leftArrow, modifiers: .command)
                Divider()
                Button("Lauter") { model.player.setVolume(model.player.volume + 0.05) }.keyboardShortcut(.upArrow, modifiers: .command)
                Button("Leiser") { model.player.setVolume(model.player.volume - 0.05) }.keyboardShortcut(.downArrow, modifiers: .command)
                Button(model.player.isMuted ? "Ton einschalten" : "Stummschalten") { model.player.toggleMute() }.keyboardShortcut("m", modifiers: [.command, .shift])
            }
        }
        #endif
    }
}

#if os(iOS)
final class OfflineAppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, handleEventsForBackgroundURLSession identifier: String,
                     completionHandler: @escaping () -> Void) {
        guard identifier == OfflineLibrary.backgroundIdentifier else { completionHandler(); return }
        OfflineLibrary.shared.backgroundCompletion = completionHandler
    }
}
#endif
