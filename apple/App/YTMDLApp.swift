import SwiftUI

@main struct YTMDLApp: App {
    @State private var model = AppModel()
    var body: some Scene {
        WindowGroup {
            RootView(model: model)
                #if os(tvOS)
                .tint(.white)
                #else
                .tint(.pink)
                #endif
                #if os(macOS)
                .frame(minWidth: 720, minHeight: 560)
                #endif
        }
        #if os(macOS)
        .defaultSize(width: 1180, height: 800)
        .commands {
            CommandMenu("Wiedergabe") {
                Button("Wiedergabe / Pause") { model.player.toggle() }.keyboardShortcut(.space, modifiers: [])
                Button("Nächster Titel") { model.player.next() }.keyboardShortcut(.rightArrow, modifiers: .command)
                Button("Vorheriger Titel") { model.player.previous() }.keyboardShortcut(.leftArrow, modifiers: .command)
            }
        }
        #endif
    }
}
