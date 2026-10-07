import XCTest
import AVFoundation
#if os(iOS)
import UIKit
#endif

final class PlayerUITests: XCTestCase {
    @MainActor private func app(player: Bool = false, pausedPlayer: Bool = false, largeText: Bool = false) throws -> XCUIApplication {
        let server = ProcessInfo.processInfo.environment["YTMDL_FIXTURE_URL"] ?? "http://127.0.0.1:59583"
        guard let host = URL(string: server)?.host, ["127.0.0.1", "localhost"].contains(host) else { throw NSError(domain: "Loopback fixture required", code: 1) }
        let app = XCUIApplication()
        app.launchArguments = ["--fixture-server", server]
        if player { app.launchArguments.append("--fixture-player") }
        if pausedPlayer { app.launchArguments.append("--fixture-paused-player") }
        if largeText { app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] }
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
    @MainActor func testQueueManagementAndNormalizationSettingsWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact player test requires iPhone.")
        let app = try app(pausedPlayer: true)
        XCTAssertTrue(app.buttons["mobile-mini-player-open"].waitForExistence(timeout: 20))
        app.buttons["mobile-mini-player-open"].tap()
        let row = app.buttons["mobile-queue-track-0"]
        for _ in 0..<5 { if row.isHittable { break }; app.swipeUp() }
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        let title = row.label
        app.buttons["mobile-queue-actions-0"].tap()
        XCTAssertTrue(app.buttons["Nach unten"].waitForExistence(timeout: 5))
        app.buttons["Nach unten"].tap()
        XCTAssertEqual(app.buttons["mobile-queue-track-1"].label, title)
        XCTAssertTrue(app.buttons["mobile-player-toggle"].label == "Abspielen")
        app.buttons["mobile-queue-tools"].tap()
        XCTAssertTrue(app.buttons["Als Playlist speichern"].waitForExistence(timeout: 5))
        app.buttons["Als Playlist speichern"].tap()
        XCTAssertTrue(app.buttons["Erstellen und Titel hinzufügen"].waitForExistence(timeout: 5))
        app.navigationBars["Zur Playlist hinzufügen"].buttons["Schließen"].tap()
        for _ in 0..<5 { if app.buttons["Werkzeuge"].isHittable { break }; app.swipeDown() }
        app.buttons["Werkzeuge"].tap()
        let normalization = app.switches["playback-normalization"]
        XCTAssertTrue(normalization.waitForExistence(timeout: 5))
        XCTAssertTrue(normalization.isHittable)
        // Settings persist across fixture launches. Test the rendered enabled
        // state instead of assuming the OS encodes a switch value as "1".
        let recheck = app.buttons["Lautheit erneut prüfen"]
        if recheck.exists {
            normalization.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
            expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: recheck)
            waitForExpectations(timeout: 5)
        }
        normalization.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.5)).tap()
        XCTAssertTrue(recheck.waitForExistence(timeout: 5), "The switch must actually enable normalization.")
        let measured = app.staticTexts.containing(NSPredicate(format: "label BEGINSWITH %@ AND label CONTAINS %@", "Aktueller Titel:", "-3")).firstMatch
        XCTAssertTrue(measured.waitForExistence(timeout: 10))
        attach(app, name: "Native playback normalization")
        app.buttons["Fertig"].tap(); app.navigationBars["Jetzt läuft"].buttons["Schließen"].tap()
        XCTAssertTrue(app.buttons["mobile-mini-player-open"].isHittable)
    }

    @MainActor func testWidgetLinksOpenDestinationsWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Widget routes require compact navigation.")
        let app = try app(pausedPlayer: true)
        XCTAssertTrue(app.buttons["mobile-mini-player-open"].waitForExistence(timeout: 20))
        func openRoute(_ route: String) {
            app.open(URL(string: "ytmdl-player://" + route)!)
            // A simulator URL opened externally can leave a system confirmation
            // across app launches. Confirm only this fixture app's open prompt.
            let prompt = XCUIApplication(bundleIdentifier: "com.apple.springboard").alerts.firstMatch
            if prompt.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "YTMDL")).firstMatch.exists {
                let confirm = prompt.buttons.matching(NSPredicate(format: "label IN %@", ["Open", "Öffnen"])).firstMatch
                if confirm.exists { confirm.tap() }
            }
        }
        openRoute("player")
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Pause"].exists)
        openRoute("favorites")
        XCTAssertTrue(app.navigationBars["Lieblingstitel"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["collection-play"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["collection-play"].isHittable)
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Widget favorites destination")
        openRoute("playlists")
        XCTAssertTrue(app.staticTexts["Abends unterwegs"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        XCTAssertTrue(app.buttons["collection-play"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Redesigned playlist header")
    }
    @MainActor func testLargestTextKeepsMiniPlayerAndNavigationUsableWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact accessibility requires iPhone.")
        let app = try app(pausedPlayer: true, largeText: true)
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        XCTAssertTrue(mini.isHittable)
        XCTAssertLessThanOrEqual(mini.frame.height, 70)
        XCTAssertGreaterThan(app.staticTexts["Deine Musik.\nDein Moment."].frame.height, 120, "The fixture must actually use accessibility text size.")
        let mix = app.buttons["Favoriten-Mix"].firstMatch
        for _ in 0..<6 where !mix.isHittable { app.swipeUp() }
        XCTAssertTrue(mix.isHittable, "The large-text primary action remains reachable by scrolling.")
        let next = app.buttons["Nächster Titel"].firstMatch
        XCTAssertTrue(next.isHittable)
        XCTAssertGreaterThanOrEqual(next.frame.width, 44)
        XCTAssertLessThanOrEqual(next.frame.height, 60)
        for title in ["Playlists", "Bibliothek", "Einstellungen", "Start"] {
            app.tabBars.buttons[title].tap()
            XCTAssertTrue(mini.isHittable)
        }
        mini.tap()
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.isHittable)
        let previous = app.buttons["mobile-player-previous"]
        for _ in 0..<6 where !previous.isHittable { app.swipeUp() }
        for id in ["mobile-player-shuffle", "mobile-player-previous", "mobile-player-next", "mobile-player-repeat"] {
            let control = app.buttons[id]
            XCTAssertTrue(control.isHittable, id)
            XCTAssertGreaterThanOrEqual(control.frame.width, 44, id)
            XCTAssertGreaterThanOrEqual(control.frame.height, 44, id)
            XCTAssertLessThanOrEqual(control.frame.height, 50, id)
        }
        attach(app, name: "Accessible large player")
        app.buttons["Schließen"].firstMatch.tap()
        XCTAssertTrue(mini.isHittable)
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Largest text with compact transport")
    }
    @MainActor func testMobileCollectionDesignAndPlaylistFilterWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact design requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Deine Musik.\nDein Moment."].waitForExistence(timeout: 20))
        let mix = app.buttons["Favoriten-Mix"].firstMatch
        XCTAssertTrue(mix.isHittable)
        XCTAssertGreaterThanOrEqual(mix.frame.height, 44)
        XCTAssertTrue(app.buttons["Offline"].firstMatch.isHittable)
        attach(app, name: "Build 28 Start")
        app.tabBars.buttons["Bibliothek"].tap()
        for title in ["Favoriten", "Künstler", "Offline"] {
            let button = app.buttons[title].firstMatch
            XCTAssertTrue(button.isHittable)
            XCTAssertGreaterThanOrEqual(button.frame.height, 44)
        }
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.exists)
        attach(app, name: "Build 28 Library")
        app.tabBars.buttons["Playlists"].tap()
        let create = app.buttons["Neue Playlist"].firstMatch
        XCTAssertTrue(create.isHittable)
        XCTAssertGreaterThanOrEqual(create.frame.height, 44)
        XCTAssertTrue(app.staticTexts["Abends unterwegs"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Build 28 Playlists")
        let filter = app.textFields["mobile-playlist-filter"]
        XCTAssertTrue(filter.isHittable)
        filter.tap(); filter.typeText("Missing")
        XCTAssertTrue(app.staticTexts["Keine passende Playlist"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Playlist-Filter leeren"].tap()
        XCTAssertTrue(app.staticTexts["Abends unterwegs"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Playlists sortieren und aktualisieren"].tap()
        XCTAssertTrue(app.buttons["Aktualisieren"].firstMatch.waitForExistence(timeout: 5))
        app.tap()
        if app.keyboards.firstMatch.exists { app.swipeUp() }
        XCTAssertFalse(app.buttons["Pause"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
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
    @MainActor func testMobileManualPlaylistCreateEditOrderRemoveDeleteWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact editor requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        app.tabBars.buttons["Playlists"].tap()
        app.buttons["Neue Playlist"].tap()
        let name = app.textFields["playlistName"]
        XCTAssertTrue(name.waitForExistence(timeout: 10))
        name.tap(); name.typeText("Release check")
        app.buttons["playlistSave"].tap()
        XCTAssertTrue(app.staticTexts["Release check"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Release check"].firstMatch.tap()
        let options = app.buttons["playlist-collection-options"]
        XCTAssertTrue(options.waitForExistence(timeout: 10))
        options.tap(); app.buttons["Titel hinzufügen"].tap()
        guard app.buttons["select-playlist-track-t0"].waitForExistence(timeout: 10) else {
            attach(app, name: "Playlist picker missing library tracks")
            XCTFail("The playlist picker must load selectable library tracks.")
            return
        }
        app.buttons["select-playlist-track-t0"].tap()
        XCTAssertEqual(app.buttons["playlist-add-selected"].label, "1 hinzufügen")
        app.buttons["select-playlist-track-t1"].tap()
        XCTAssertEqual(app.buttons["playlist-add-selected"].label, "2 hinzufügen")
        attach(app, name: "Selected two playlist tracks")
        app.buttons["playlist-add-selected"].tap()
        let first = app.buttons["Aktionen für Nachtfahrt"], second = app.buttons["Aktionen für Zeitlos"]
        XCTAssertTrue(first.waitForExistence(timeout: 10))
        XCTAssertTrue(second.waitForExistence(timeout: 10))
        XCTAssertLessThan(first.frame.minY, second.frame.minY)
        second.tap(); app.buttons["In Playlist nach oben"].tap()
        expectation(for: NSPredicate { _, _ in second.frame.minY < first.frame.minY }, evaluatedWith: second)
        waitForExpectations(timeout: 10)
        first.tap(); app.buttons["Aus Playlist entfernen"].tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: first)
        waitForExpectations(timeout: 10)
        options.tap(); app.buttons["Bearbeiten"].tap()
        XCTAssertTrue(name.waitForExistence(timeout: 10))
        name.tap(); name.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 13) + "Ready playlist")
        app.buttons["playlistSave"].tap()
        XCTAssertTrue(app.navigationBars["Ready playlist"].waitForExistence(timeout: 10))
        options.tap(); app.buttons["Playlist löschen"].tap()
        app.buttons["Playlist löschen"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Neue Playlist"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.staticTexts["Ready playlist"].exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Manual playlist CRUD without playback")
    }
    @MainActor func testRepeatedMobileTabSwitchesKeepPausedMiniPlayerReachable() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact navigation requires iPhone.")
        let app = try app(pausedPlayer: true)
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        for _ in 0..<3 {
            for tab in ["Suche", "Bibliothek", "Playlists", "Einstellungen", "Start"] {
                app.tabBars.buttons[tab].tap()
                XCTAssertTrue(mini.isHittable, "Mini-player remains reachable after " + tab)
                XCTAssertFalse(app.alerts.firstMatch.exists)
            }
        }
        mini.tap()
        XCTAssertTrue(app.buttons["mobile-player-toggle"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["mobile-player-toggle"].isHittable)
        app.buttons["Schließen"].firstMatch.tap()
        XCTAssertTrue(mini.isHittable)
        XCTAssertFalse(app.buttons["Pause"].exists)
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
        let play = app.buttons["collection-play"], shuffle = app.buttons["collection-shuffle"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        for control in [play, shuffle] {
            XCTAssertTrue(control.isHittable)
            XCTAssertGreaterThanOrEqual(control.frame.height, 52)
            XCTAssertGreaterThanOrEqual(control.frame.width, 120)
        }
        app.buttons["playlist-collection-options"].tap()
        XCTAssertTrue(app.buttons["Offline speichern"].firstMatch.waitForExistence(timeout: 10))
        app.navigationBars.firstMatch.tap()
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
        app.buttons["offline-collection-r0"].firstMatch.tap()
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
    @MainActor func testOfflinePlaylistsBrowseSearchAndSortWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Offline flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Deine Musik.\nDein Moment."].waitForExistence(timeout: 20))
        app.tabBars.buttons["Playlists"].tap()
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        app.buttons["playlist-collection-options"].tap()
        app.buttons["Offline speichern"].firstMatch.tap()
        app.tabBars.buttons["Start"].tap()
        app.buttons["Offline"].firstMatch.tap()
        XCTAssertTrue(app.buttons["offline-collection-p0"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["6 von 6 Titeln offline"].waitForExistence(timeout: 30))
        // Enter the same local-only mode available without a server session.
        app.tabBars.buttons["Einstellungen"].tap()
        for _ in 0..<8 where !app.buttons["Abmelden"].firstMatch.isHittable { app.swipeUp() }
        app.buttons["Abmelden"].firstMatch.tap()
        app.buttons["Abmelden"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Offline-Musik öffnen"].waitForExistence(timeout: 10))
        app.buttons["Offline-Musik öffnen"].tap()
        app.buttons.containing(.staticText, identifier: "Design-Vorschau").firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Offline-Modus"].waitForExistence(timeout: 10))
        attach(app, name: "Offline playlists with cached covers")
        app.buttons["offline-collection-p0"].tap()
        XCTAssertTrue(app.navigationBars["Abends unterwegs"].waitForExistence(timeout: 10))
        let first = app.buttons["offline-track-t0"].firstMatch
        let second = app.buttons["offline-track-t1"].firstMatch
        // The saved playlist order is t0, t1; alphabetical order differs.
        for _ in 0..<4 where !first.isHittable { app.swipeUp() }
        XCTAssertTrue(first.exists); XCTAssertTrue(second.exists)
        XCTAssertLessThan(first.frame.minY, second.frame.minY)
        XCTAssertFalse(app.buttons["Pause"].exists)
        app.navigationBars.buttons.firstMatch.tap()
        app.buttons["Alle Titel"].tap()
        XCTAssertTrue(app.buttons["offline-sort"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["offline-sort"].firstMatch.tap()
        app.buttons["Titel A–Z"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Blaue Stunde"].firstMatch.exists)
        attach(app, name: "Offline tracks sorted by title")
        app.swipeDown()
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        search.tap(); search.typeText("no such song")
        XCTAssertTrue(app.staticTexts["Keine passenden Titel"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
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
        #if os(iOS)
        let manualStart = UIDevice.current.userInterfaceIdiom == .phone
        let app = try app(player: !manualStart, pausedPlayer: manualStart)
        if manualStart {
            // Exercise the user's foreground Play action instead of starting
            // AVPlayer while the cold simulator is still launching the app.
            let mini = app.buttons["mobile-mini-player-open"]
            XCTAssertTrue(mini.waitForExistence(timeout: 20))
            mini.tap()
            let play = app.buttons["mobile-player-toggle"]
            XCTAssertTrue(play.waitForExistence(timeout: 10))
            XCTAssertEqual(play.label, "Abspielen")
            XCTAssertTrue(play.isHittable)
            play.tap()
        }
        #else
        let app = try app(pausedPlayer: true)
        #endif
        #if os(tvOS)
        // Wait for the restored catalogue before moving focus into the tab bar.
        // Foreground Play follows navigation; it must not race cold launch.
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        let playerTab = app.buttons["Player"].firstMatch
        XCTAssertTrue(playerTab.waitForExistence(timeout: 20))
        XCUIRemote.shared.press(.up)
        for _ in 0..<8 {
            if app.buttons["Bibliothek"].firstMatch.hasFocus { break }
            XCUIRemote.shared.press(.left)
        }
        for _ in 0..<8 {
            if playerTab.hasFocus { break }
            XCUIRemote.shared.press(.right)
        }
        XCTAssertTrue(playerTab.hasFocus)
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(app.buttons["Abspielen"].firstMatch.waitForExistence(timeout: 10))
        XCUIRemote.shared.press(.playPause)
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
        let progressed = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == true AND label != %@", "0:00"), object: elapsed)
        guard XCTWaiter.wait(for: [progressed], timeout: 5) == .completed else {
            attach(app, name: "Playback clock did not advance")
            XCTFail("The real AVPlayer playback clock must advance within five seconds.")
            return
        }
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
