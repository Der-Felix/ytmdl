import XCTest

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
