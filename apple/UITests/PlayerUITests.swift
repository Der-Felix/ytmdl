import XCTest
#if os(iOS)
import UIKit
#endif

final class PlayerUITests: XCTestCase {
    @MainActor private func app(player: Bool = false) throws -> XCUIApplication {
        let server = ProcessInfo.processInfo.environment["YTMDL_FIXTURE_URL"] ?? "http://127.0.0.1:59583"
        guard let host = URL(string: server)?.host, ["127.0.0.1", "localhost"].contains(host) else { throw NSError(domain: "Loopback fixture required", code: 1) }
        let app = XCUIApplication()
        app.launchArguments = ["--fixture-server", server]
        if player { app.launchArguments.append("--fixture-player") }
        app.launch()
        return app
    }
    @MainActor private func attach(_ app: XCUIApplication, name: String) {
        let image = XCTAttachment(screenshot: app.screenshot())
        image.name = name; image.lifetime = .keepAlways; add(image)
    }
    @MainActor func testLibraryFitsNativeScreen() throws {
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Native library")
    }
    #if os(iOS)
    @MainActor func testMobileHomeDownloadsAndSoundSettingsWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Mobile flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Deine Musik.\nDein Moment."].waitForExistence(timeout: 20))
        attach(app, name: "Mobile Start")
        app.tabBars.buttons["Bibliothek"].tap()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Offline speichern"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["Offline speichern"].firstMatch.tap()
        app.tabBars.buttons["Start"].tap()
        app.buttons["Offline"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Offline-Musik"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Offline verfügbar"].firstMatch.waitForExistence(timeout: 30))
        XCTAssertFalse(app.buttons["Pause"].exists, "Downloading must not start playback.")
        attach(app, name: "Mobile Offline downloads")
        app.tabBars.buttons["Einstellungen"].tap()
        app.buttons["Equalizer, Überblendung & Visualizer"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Klang & Wiedergabe"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.switches["Equalizer aktivieren"].firstMatch.exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Mobile EQ settings without playback")
    }
    @MainActor func testSidebarSwitchDiscardsOpenedCollections() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "Sidebar regression requires iPad.")
        XCUIDevice.shared.orientation = .landscapeLeft
        defer { XCUIDevice.shared.orientation = .portrait }
        let app = try app() // Library fixture only: never starts playback.
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Playlists"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Abends unterwegs"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Abspielen"].firstMatch.exists, "Album drilldown must not conceal playlist overview.")
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Favoriten"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Lieblingstitel"].waitForExistence(timeout: 10))
        app.staticTexts["Bibliothek"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Abspielen"].firstMatch.exists, "Collection drilldown must be discarded on section change.")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Sidebar returns to library without playback")
    }
    #endif
    @MainActor func testAuthenticatedPlayerAndQueue() throws {
        let app = try app(player: true)
        #if os(tvOS)
        // Navigate with the remote like a user. Initial TV focus may select
        // Library while the asynchronous fixture session is restored.
        let playerTab = app.buttons["Player"].firstMatch
        XCTAssertTrue(playerTab.waitForExistence(timeout: 20))
        for _ in 0..<8 {
            if playerTab.hasFocus { break }
            XCUIRemote.shared.press(.right)
        }
        XCTAssertTrue(playerTab.hasFocus)
        XCUIRemote.shared.press(.select)
        #endif
        XCTAssertTrue(app.buttons["Pause"].firstMatch.waitForExistence(timeout: 20), "Fixture stream must actually begin playing.")
        XCTAssertTrue(app.buttons["Pause"].firstMatch.isHittable, "Primary playback control must fit the visible screen.")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.exists)
        let elapsed = app.staticTexts["playbackElapsed"]
        expectation(for: NSPredicate(format: "label != %@", "0:00"), evaluatedWith: elapsed)
        waitForExpectations(timeout: 5)
        #if os(tvOS)
        XCUIRemote.shared.press(.playPause)
        #else
        app.buttons["Pause"].firstMatch.tap()
        #endif
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Native player paused")
        #if !os(tvOS)
        app.buttons["Schließen"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Deine Musik"].firstMatch.waitForExistence(timeout: 5))
        #endif
    }
}
