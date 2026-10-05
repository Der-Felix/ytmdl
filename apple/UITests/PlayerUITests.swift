import XCTest
import AVFoundation
#if os(iOS)
import UIKit
#endif

final class PlayerUITests: XCTestCase {
    @MainActor private func app(player: Bool = false, pausedPlayer: Bool = false) throws -> XCUIApplication {
        let server = ProcessInfo.processInfo.environment["YTMDL_FIXTURE_URL"] ?? "http://127.0.0.1:59583"
        guard let host = URL(string: server)?.host, ["127.0.0.1", "localhost"].contains(host) else { throw NSError(domain: "Loopback fixture required", code: 1) }
        let app = XCUIApplication()
        app.launchArguments = ["--fixture-server", server]
        if player { app.launchArguments.append("--fixture-player") }
        if pausedPlayer { app.launchArguments.append("--fixture-paused-player") }
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
    @MainActor func testMobileFavoritesRemovalAndSearchNavigationWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        app.tabBars.buttons["Bibliothek"].tap()
        app.buttons["Favoriten"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Aktionen für Nachtfahrt"].waitForExistence(timeout: 10))
        app.buttons["Aktionen für Nachtfahrt"].tap()
        app.buttons["Aus Favoriten entfernen"].tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.buttons["Aktionen für Nachtfahrt"])
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["Aktionen für Zeitlos"].exists)
        app.tabBars.buttons["Suche"].tap()
        let search = app.searchFields.firstMatch
        if !search.exists { app.swipeDown() }
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        search.tap(); search.typeText("Nordlicht\n")
        XCTAssertTrue(app.staticTexts["Nordlicht"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Favorites update and search resolves without audio")
    }
    @MainActor func testSilentCodecFilesDecodeWithoutStartingPlayer() async throws {
        guard let origin = ProcessInfo.processInfo.environment["YTMDL_CODEC_FIXTURE_URL"] else {
            throw XCTSkip("Requires an explicit loopback server containing synthetic silent codec files.")
        }
        let base = try XCTUnwrap(URL(string: origin))
        guard base.host == "127.0.0.1" else { throw NSError(domain: "Loopback required", code: 1) }
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        for ext in ["opus", "m4a", "flac", "mp3"] {
            let (data, response) = try await session.data(from: base.appendingPathComponent("sample." + ext))
            XCTAssertEqual((response as? HTTPURLResponse)?.statusCode, 200)
            XCTAssertLessThan(data.count, 1024 * 1024)
            let url = folder.appendingPathComponent("sample." + ext); try data.write(to: url)
            let asset = AVURLAsset(url: url)
            let playable = try await asset.load(.isPlayable)
            XCTAssertTrue(playable, ext + " must be playable")
            let tracks = try await asset.loadTracks(withMediaType: .audio)
            let track = try XCTUnwrap(tracks.first)
            let reader = try AVAssetReader(asset: asset)
            let output = AVAssetReaderTrackOutput(track: track, outputSettings: [AVFormatIDKey: kAudioFormatLinearPCM,
                AVLinearPCMIsFloatKey: true, AVLinearPCMBitDepthKey: 32, AVLinearPCMIsNonInterleaved: false])
            reader.add(output); XCTAssertTrue(reader.startReading())
            var samples = 0
            while let buffer = output.copyNextSampleBuffer() { samples += CMSampleBufferGetNumSamples(buffer) }
            XCTAssertEqual(reader.status, .completed, ext + " must fully decode")
            XCTAssertGreaterThan(samples, 0)
        }
    }
    @MainActor func testMobileCollectionFailureCanRetryWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        app.tabBars.buttons["Bibliothek"].tap()
        app.staticTexts["Lichtblick"].firstMatch.tap()
        let retry = app.buttons["Titel erneut laden"]
        XCTAssertTrue(retry.waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Noch keine Titel"].exists, "A network failure must not claim the collection is empty.")
        retry.tap()
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: app.buttons["Abspielen"].firstMatch)
        waitForExpectations(timeout: 10)
        XCTAssertFalse(retry.exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Failed collection recovers without audio")
    }
    @MainActor func testMobilePlaylistEditorReportsFailureAndCanSaveSmartRules() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact editor requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        app.tabBars.buttons["Playlists"].tap()
        app.buttons["Neue Playlist"].tap()
        let name = app.textFields["playlistName"]
        XCTAssertTrue(name.waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["playlistSave"].isEnabled)
        name.tap(); name.typeText("Audit failure")
        app.buttons["playlistSave"].tap()
        XCTAssertTrue(app.staticTexts["playlistSaveError"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["playlistSave"].isEnabled)
        name.tap(); name.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 13) + "Audit favorites")
        let intelligent = app.switches["Intelligente Playlist"].firstMatch
        intelligent.coordinate(withNormalizedOffset: CGVector(dx: 0.92, dy: 0.5)).tap()
        XCTAssertTrue(app.buttons["Lieblingstitel"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Lieblingstitel"].firstMatch.tap()
        app.buttons["playlistSave"].tap()
        XCTAssertTrue(app.staticTexts["Audit favorites"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.navigationBars["Neue Playlist"].exists)
        app.staticTexts["Audit favorites"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.exists)
        XCTAssertFalse(app.staticTexts["Fernweh"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Smart playlist persists favorite rules without audio")
    }
    @MainActor func testMiniPlayerRemainsReachableInPlaylistWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact player requires iPhone.")
        let app = try app(pausedPlayer: true)
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        XCTAssertTrue(mini.isHittable)
        app.tabBars.buttons["Playlists"].tap()
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Offline speichern"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(mini.isHittable, "Player must stay above tabs inside a pushed collection.")
        attach(app, name: "Playlist with anchored paused mini player")
        mini.tap()
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Werkzeuge"].firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Mobile player opened without audio")
        app.buttons["Schließen"].firstMatch.tap()
        app.tabBars.buttons["Suche"].tap()
        XCTAssertTrue(mini.isHittable)
        app.buttons["open-mobile-player"].tap()
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
    }
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
    @MainActor func testTabletPlayerControlsFitWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "Tablet layout requires iPad.")
        let app = try app(pausedPlayer: true)
        let mini = app.buttons["Player öffnen: Nachtfahrt"].firstMatch
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        XCTAssertTrue(mini.isHittable)
        mini.tap()
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["mobile-player-toggle"].isHittable, "Transport must fit the tablet sheet without scrolling.")
        XCTAssertTrue(app.buttons["Werkzeuge"].firstMatch.isHittable)
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Tablet player controls without audio")
        app.buttons["Schließen"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Deine Musik.\nDein Moment."].firstMatch.waitForExistence(timeout: 5))
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
        #if os(iOS)
        let toggle = app.buttons["mobile-player-toggle"]
        XCTAssertTrue(toggle.waitForExistence(timeout: 20))
        expectation(for: NSPredicate(format: "label == %@", "Pause"), evaluatedWith: toggle)
        waitForExpectations(timeout: 20)
        #else
        let toggle = app.buttons["Pause"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 20), "Fixture stream must actually begin playing.")
        #endif
        XCTAssertTrue(toggle.isHittable, "Primary playback control must fit the visible screen.")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.exists)
        let elapsed = app.staticTexts["playbackElapsed"]
        expectation(for: NSPredicate(format: "label != %@", "0:00"), evaluatedWith: elapsed)
        waitForExpectations(timeout: 5)
        #if os(tvOS)
        XCUIRemote.shared.press(.playPause)
        #else
        toggle.tap()
        #endif
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Native player paused")
        #if !os(tvOS)
        app.buttons["Schließen"].firstMatch.tap()
        #if os(iOS)
        XCTAssertTrue(app.staticTexts["Deine Musik.\nDein Moment."].firstMatch.waitForExistence(timeout: 5))
        #else
        XCTAssertTrue(app.staticTexts["Deine Musik"].firstMatch.waitForExistence(timeout: 5))
        #endif
        #endif
    }
}
