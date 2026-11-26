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
    #if os(iOS)
    /// Chooses an iPhone tab. While Search is shown the system may fold the
    /// other tabs into one button; that button unfolds them first.
    @MainActor private func selectTab(_ app: XCUIApplication, _ name: String) {
        let button = app.tabBars.buttons[name]
        if !(button.exists && button.isHittable) {
            let folded = app.tabBars.buttons.firstMatch
            if folded.exists && folded.isHittable { folded.tap() }
        }
        if !(button.waitForExistence(timeout: 5) && button.isHittable) { print(app.debugDescription) }
        button.tap()
    }
    #endif
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
        // After scrolling to the queue, "Werkzeuge" can sit under the player's
        // navigation bar: the test system still calls it hittable, but the tap
        // lands on the bar. Scroll back until it is clear of the bar.
        let werkzeuge = app.buttons["Werkzeuge"]
        let playerBar = app.navigationBars["Jetzt läuft"]
        for _ in 0..<6 {
            if werkzeuge.isHittable && werkzeuge.frame.minY > playerBar.frame.maxY + 8 { break }
            app.swipeDown()
        }
        let normalization = app.switches["playback-normalization"]
        werkzeuge.tap()
        if !normalization.waitForExistence(timeout: 5) {
            attach(app, name: "Sound settings did not open")
            print(app.debugDescription)
        }
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
        app.buttons["Fertig"].tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: normalization)
        waitForExpectations(timeout: 10)
        let tools = app.buttons["mobile-queue-tools"]
        for _ in 0..<5 { if tools.isHittable { break }; app.swipeUp() }
        tools.tap()
        XCTAssertTrue(app.buttons["Als Playlist speichern"].waitForExistence(timeout: 5))
        app.buttons["Als Playlist speichern"].tap()
        XCTAssertTrue(app.buttons["Erstellen und Titel hinzufügen"].waitForExistence(timeout: 5))
        app.navigationBars["Zur Playlist hinzufügen"].buttons["Schließen"].tap()
        let close = app.navigationBars["Jetzt läuft"].buttons["Schließen"]
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        close.tap()
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
        let favorites = app.buttons["home-quick-favorites"]
        XCTAssertTrue(favorites.waitForExistence(timeout: 10))
        for _ in 0..<6 where !favorites.isHittable { app.swipeUp() }
        XCTAssertTrue(favorites.isHittable, "The large-text quick access remains reachable by scrolling.")
        // One column only happens at accessibility sizes: this also proves the
        // fixture really uses them.
        XCTAssertGreaterThan(favorites.frame.width, app.frame.width * 0.75, "Quick access falls back to one column.")
        attach(app, name: "Start at largest text")
        let next = app.buttons["Nächster Titel"].firstMatch
        XCTAssertTrue(next.isHittable)
        XCTAssertGreaterThanOrEqual(next.frame.width, 44)
        XCTAssertLessThanOrEqual(next.frame.height, 60)
        for title in ["Playlists", "Bibliothek", "Einstellungen", "Start"] {
            selectTab(app, title)
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
        let favorites = app.buttons["home-quick-favorites"]
        XCTAssertTrue(favorites.waitForExistence(timeout: 20))
        XCTAssertTrue(favorites.isHittable)
        XCTAssertGreaterThanOrEqual(favorites.frame.height, 44)
        XCTAssertLessThan(favorites.frame.width, app.frame.width * 0.6, "Quick access uses two columns at the default text size.")
        XCTAssertFalse(app.buttons["open-mobile-player"].exists, "No player button in the navigation bar.")
        XCTAssertTrue(app.buttons["mix-card-genre-0a1b2c3d4e5f"].waitForExistence(timeout: 15))
        attach(app, name: "Start top")
        app.swipeUp()
        attach(app, name: "Start shelves")
        app.swipeUp()
        attach(app, name: "Start shelves end")
        selectTab(app, "Bibliothek")
        for title in ["Favoriten", "Künstler", "Offline"] {
            let button = app.buttons[title].firstMatch
            XCTAssertTrue(button.isHittable)
            XCTAssertGreaterThanOrEqual(button.frame.height, 44)
        }
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.exists)
        attach(app, name: "Build 28 Library")
        selectTab(app, "Playlists")
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
        selectTab(app, "Bibliothek")
        app.buttons["Favoriten"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Aktionen für Nachtfahrt"].waitForExistence(timeout: 10))
        app.buttons["Aktionen für Nachtfahrt"].tap()
        app.buttons["Aus Favoriten entfernen"].tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.buttons["Aktionen für Nachtfahrt"])
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["Aktionen für Zeitlos"].exists)
        selectTab(app, "Suche")
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
    @MainActor func testMobileSearchGroupsResultsRemembersSearchesAndBrowsesGenres() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "The search tab requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Suche")
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10), "The search field shows without pulling down.")
        XCTAssertTrue(search.isHittable)
        let genre = app.buttons["search-genre-Elektronisch"]
        XCTAssertTrue(genre.waitForExistence(timeout: 10), "Genres to browse before typing.")
        XCTAssertFalse(app.buttons["open-mobile-player"].exists)
        attach(app, name: "Search before typing")
        search.tap(); search.typeText("Nor")
        attach(app, name: "Search while typing")
        let top = app.buttons["search-top-hit"]
        XCTAssertTrue(top.waitForExistence(timeout: 10), "Results arrive while typing.")
        XCTAssertTrue(app.staticTexts["Top-Treffer"].exists)
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label BEGINSWITH %@", "Titel,")).firstMatch.exists)
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label BEGINSWITH %@", "Künstler,")).firstMatch.exists)
        attach(app, name: "Search results")
        search.typeText("\n")
        // Emptying the field shows the search just made.
        search.tap()
        search.typeText(String(repeating: XCUIKeyboardKey.delete.rawValue, count: 3))
        let recent = app.buttons["Erneut suchen: Nor"]
        XCTAssertTrue(recent.waitForExistence(timeout: 10), "The submitted search is remembered.")
        attach(app, name: "Search with recent searches")
        // Scrolling puts the keyboard away; a genre opens the filtered library.
        app.swipeDown()
        for _ in 0..<3 where !genre.isHittable { app.swipeUp() }
        genre.tap()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.tabBars.buttons["Bibliothek"].isSelected, "The genre opens the Library tab.")
        XCTAssertFalse(app.buttons["Pause"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Genre opens the library")
    }
    @MainActor func testMobileManualPlaylistCreateEditOrderRemoveDeleteWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact editor requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        selectTab(app, "Playlists")
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
                selectTab(app, tab)
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
        selectTab(app, "Bibliothek")
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
        selectTab(app, "Playlists")
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
        selectTab(app, "Playlists")
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
        selectTab(app, "Suche")
        XCTAssertTrue(mini.isHittable)
        XCTAssertFalse(app.buttons["open-mobile-player"].exists, "The mini player is the way into the player.")
        mini.tap()
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
    }
    @MainActor func testMobileHomeDownloadsAndSoundSettingsWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Mobile flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        attach(app, name: "Mobile Start")
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Offline speichern"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["Offline speichern"].firstMatch.tap()
        // Choosing the shown tab again returns to its first page, where
        // Offline lives now that Start has no shortcut tiles.
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.buttons["Offline"].firstMatch.waitForExistence(timeout: 10))
        app.buttons["Offline"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Offline-Musik"].waitForExistence(timeout: 10))
        app.buttons["offline-collection-r0"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Offline verfügbar"].firstMatch.waitForExistence(timeout: 30))
        XCTAssertFalse(app.buttons["Pause"].exists, "Downloading must not start playback.")
        attach(app, name: "Mobile Offline downloads")
        selectTab(app, "Einstellungen")
        app.buttons["Equalizer, Überblendung & Visualizer"].firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Klang & Wiedergabe"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.switches["Equalizer aktivieren"].firstMatch.exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Mobile EQ settings without playback")
    }
    @MainActor func testOfflinePlaylistsBrowseSearchAndSortWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Offline flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Playlists")
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        app.buttons["playlist-collection-options"].tap()
        app.buttons["Offline speichern"].firstMatch.tap()
        selectTab(app, "Bibliothek")
        app.buttons["Offline"].firstMatch.tap()
        XCTAssertTrue(app.buttons["offline-collection-p0"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["6 von 6 Titeln offline"].waitForExistence(timeout: 30))
        // Enter the same local-only mode available without a server session.
        selectTab(app, "Einstellungen")
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
    @MainActor func testListeningHomeOpensMixAndStartsItsQueue() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact home requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        // The server sections of the fixture: mixes, history and new albums.
        let card = app.buttons["mix-card-genre-0a1b2c3d4e5f"]
        XCTAssertTrue(card.waitForExistence(timeout: 15), "Mixes come from the server.")
        XCTAssertTrue(app.staticTexts["Zuletzt gehört"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Neu in der Mediathek"].exists)
        for _ in 0..<4 where !card.isHittable { app.swipeUp() }
        attach(app, name: "Listening home with mixes")
        card.tap()
        let play = app.buttons["mix-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: play)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["mix-save-playlist"].isEnabled)
        play.tap()
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 10), "Play starts the mix queue.")
        XCTAssertTrue(mini.label.contains("Nachtfahrt"), "The mix starts with its first title.")
        let pause = app.buttons["Pause"].firstMatch
        XCTAssertTrue(pause.waitForExistence(timeout: 20), "The mix must actually begin playing.")
        pause.tap()
        XCTAssertFalse(app.alerts.firstMatch.exists)
        attach(app, name: "Mix queue started from its page")
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
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 5))
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
    #if os(tvOS)
    @MainActor func testTVHomeShowsFocusableMixes() throws {
        let app = try app()
        let first = app.buttons["tv-mix-genre-0a1b2c3d4e5f"], second = app.buttons["tv-mix-on_repeat"]
        XCTAssertTrue(first.waitForExistence(timeout: 20), "The start screen shows the server's mixes.")
        XCTAssertTrue(second.exists)
        XCTAssertTrue(app.staticTexts["Zuletzt gehört"].waitForExistence(timeout: 10))
        for _ in 0..<6 where !first.hasFocus && !second.hasFocus { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(first.hasFocus || second.hasFocus, "A mix card takes the remote's focus.")
        attach(app, name: "TV home with focused mix")
        XCTAssertFalse(app.buttons["Pause"].exists, "The start screen never starts playback.")
    }
    @MainActor func testTVForYouOpensFromStartAndFocusesAMix() throws {
        let app = try app()
        let first = app.buttons["tv-mix-genre-0a1b2c3d4e5f"], all = app.buttons["tv-all-mixes"]
        XCTAssertTrue(first.waitForExistence(timeout: 20), "The start screen shows the server's mixes.")
        XCTAssertTrue(all.exists, "Alle Mixe closes the first mix row.")
        for _ in 0..<6 where !first.hasFocus { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(first.hasFocus)
        for _ in 0..<3 where !all.hasFocus { XCUIRemote.shared.press(.right) }
        XCTAssertTrue(all.hasFocus)
        XCUIRemote.shared.press(.select)
        let genre = app.buttons["tv-foryou-mix-genre-0a1b2c3d4e5f"], listening = app.buttons["tv-foryou-mix-on_repeat"]
        XCTAssertTrue(genre.waitForExistence(timeout: 10), "Für dich shows every mix, genre mixes first.")
        for _ in 0..<4 where !genre.hasFocus && !listening.hasFocus { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(genre.hasFocus || listening.hasFocus, "A mix card in the grid takes the remote's focus.")
        attach(app, name: "TV Für dich with focused mix")
        // The grid card has the same long-press menu as the Start cards.
        XCUIRemote.shared.press(.select, forDuration: 1.5)
        let shuffle = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Zufällig abspielen")).firstMatch
        let opened = shuffle.waitForExistence(timeout: 5)
        attach(app, name: "TV Für dich card menu")
        if !opened { print(app.debugDescription) }
        XCTAssertTrue(opened, "A long press opens the mix menu.")
        XCUIRemote.shared.press(.menu)
        XCTAssertFalse(app.buttons["Pause"].exists, "Opening Für dich never starts playback.")
    }
    @MainActor func testTVQueueRemovesUpcomingEntryFromMenu() throws {
        let app = try app(pausedPlayer: true)
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        let playerTab = app.buttons["Player"].firstMatch
        XCTAssertTrue(playerTab.waitForExistence(timeout: 20))
        XCUIRemote.shared.press(.up)
        for _ in 0..<8 where !app.buttons["Bibliothek"].firstMatch.hasFocus { XCUIRemote.shared.press(.left) }
        for _ in 0..<8 where !playerTab.hasFocus { XCUIRemote.shared.press(.right) }
        XCTAssertTrue(playerTab.hasFocus)
        XCUIRemote.shared.press(.select)
        let target = app.buttons["tv-queue-track-1"]
        XCTAssertTrue(target.waitForExistence(timeout: 10))
        XCTAssertTrue(target.label.hasPrefix("Zeitlos"), "The fixture queue starts with Nachtfahrt, then Zeitlos.")
        XCUIRemote.shared.press(.down)
        // Walk into the queue panel on the right without touching the tab bar
        // or the Warteschlange/Lyrics switch, then down to the first upcoming row.
        let tabs: Set<String> = ["Start", "Bibliothek", "Suche", "Favoriten", "Playlists", "Player", "Optionen", "Warteschlange", "Lyrics"]
        for _ in 0..<30 where !target.hasFocus {
            let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
            let id = focused.exists ? focused.identifier : ""
            if id.hasPrefix("tv-queue-track-"), let row = Int(id.dropFirst("tv-queue-track-".count)) {
                XCUIRemote.shared.press(row < 1 ? .down : .up)
            } else if id.hasPrefix("tv-queue-") || (focused.exists && (tabs.contains(focused.label) || focused.elementType == .segmentedControl)) {
                XCUIRemote.shared.press(.down)
            } else {
                XCUIRemote.shared.press(.right)
            }
        }
        XCTAssertTrue(target.hasFocus, "The first upcoming queue entry takes the remote's focus.")
        attach(app, name: "TV queue entry focused")
        XCUIRemote.shared.press(.select, forDuration: 1.5)
        let remove = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Aus Warteschlange entfernen")).firstMatch
        let opened = remove.waitForExistence(timeout: 5)
        attach(app, name: "TV queue after long press")
        if !opened { print(app.debugDescription) }
        XCTAssertTrue(opened, "A long press opens the entry's menu.")
        // Directly after the playing title the menu offers "Nach unten" and then
        // "Aus Warteschlange entfernen". Menu items do not report focus to XCTest;
        // the check below proves the right item was chosen.
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Nach unten")).firstMatch.exists)
        XCTAssertFalse(app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Nach oben")).firstMatch.exists,
                       "Nothing moves above the playing title.")
        XCUIRemote.shared.press(.down)
        attach(app, name: "TV queue entry menu")
        XCUIRemote.shared.press(.select)
        let next = app.buttons["tv-queue-track-1"]
        expectation(for: NSPredicate(format: "label BEGINSWITH %@", "Fernweh"), evaluatedWith: next)
        waitForExpectations(timeout: 5)
        let removed = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@ AND label BEGINSWITH %@", "tv-queue-track-", "Zeitlos"))
        XCTAssertEqual(removed.count, 0, "The removed entry is gone from the queue.")
        XCTAssertTrue(app.buttons["tv-queue-track-0"].label.contains("Nachtfahrt"), "The playing entry stays.")
        expectation(for: NSPredicate(format: "hasFocus == true"), evaluatedWith: next)
        waitForExpectations(timeout: 5)
        attach(app, name: "TV queue after removing an entry")
        XCTAssertFalse(app.buttons["Pause"].exists, "Editing the queue never starts playback.")
    }
    /// Moves focus into the tab bar, onto `tab`, and opens it.
    @MainActor private func openTVTab(_ app: XCUIApplication, _ tab: String) {
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        let item = app.buttons[tab].firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 20))
        XCUIRemote.shared.press(.up)
        for _ in 0..<8 where !app.buttons["Bibliothek"].firstMatch.hasFocus { XCUIRemote.shared.press(.left) }
        for _ in 0..<10 where !item.hasFocus { XCUIRemote.shared.press(.right) }
        XCTAssertTrue(item.hasFocus, "\(tab) takes the remote's focus in the tab bar.")
        XCUIRemote.shared.press(.select)
    }
    /// Whether the remote's focus is on the element with `identifier`. On tvOS a
    /// List or Form row takes focus as a cell: its label starts with `title`, or
    /// the element with the identifier sits inside it.
    @MainActor private func focused(on identifier: String, title: String, in app: XCUIApplication) -> Bool {
        let element = app.descendants(matching: .any).matching(identifier: identifier).firstMatch
        if element.exists && element.hasFocus { return true }
        let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
        guard focused.exists else { return false }
        return focused.identifier == identifier || focused.label.hasPrefix(title)
            || focused.descendants(matching: .any).matching(identifier: identifier).count > 0
    }
    /// Moves focus with `direction` until it is on the element, at most `presses` times.
    @MainActor private func moveFocus(to identifier: String, title: String, in app: XCUIApplication, _ direction: XCUIRemote.Button, presses: Int = 10) -> Bool {
        for _ in 0..<presses where !focused(on: identifier, title: title, in: app) { XCUIRemote.shared.press(direction) }
        return focused(on: identifier, title: title, in: app)
    }
    @MainActor func testTVSoundSettingsShowAChangedCrossfade() throws {
        let app = try app()
        openTVTab(app, "Optionen")
        let link = app.descendants(matching: .any).matching(identifier: "tv-settings-sound").firstMatch
        XCTAssertTrue(link.waitForExistence(timeout: 10), "Optionen has a Klang section.")
        let onLink = moveFocus(to: "tv-settings-sound", title: "Klang & Wiedergabe", in: app, .down)
        attach(app, name: "TV Optionen with Klang")
        XCTAssertTrue(onLink, "Klang & Wiedergabe takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        let crossfade = app.descendants(matching: .any).matching(identifier: "tv-sound-crossfade").firstMatch
        XCTAssertTrue(crossfade.waitForExistence(timeout: 10), "Klang & Wiedergabe opens.")
        // The row reads "Überblendung" with the current value; the link carries it as its value.
        let row = app.cells.matching(NSPredicate(format: "label BEGINSWITH %@", "Überblendung")).firstMatch
        let shows = { (text: String) in
            NSPredicate { _, _ in
                (crossfade.exists && ((crossfade.value as? String) == text || crossfade.label.hasSuffix(text)))
                    || (row.exists && row.label.hasSuffix(text))
            }
        }
        XCTAssertTrue(shows("Aus").evaluate(with: nil), "Crossfade starts off.")
        // Past the sleep timer and tempo rows to the crossfade row.
        let onRow = moveFocus(to: "tv-sound-crossfade", title: "Überblendung", in: app, .down)
        if !onRow { print(app.debugDescription) }
        XCTAssertTrue(onRow, "The crossfade row takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        let one = app.buttons["tv-sound-crossfade-choice-1"]
        let opened = one.waitForExistence(timeout: 10)
        attach(app, name: "TV crossfade choices")
        if !opened { print(app.debugDescription) }
        XCTAssertTrue(opened, "The crossfade choices open.")
        XCTAssertEqual(one.label, "1 s")
        let onOne = moveFocus(to: "tv-sound-crossfade-choice-1", title: "1 s", in: app, .down, presses: 4)
        if !onOne { print(app.debugDescription) }
        XCTAssertTrue(onOne, "The choice 1 s takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        expectation(for: shows("1 s"), evaluatedWith: nil)
        waitForExpectations(timeout: 10)
        attach(app, name: "TV sound settings with crossfade")
        // Put it back, so other tests start with the default.
        XCTAssertTrue(moveFocus(to: "tv-sound-crossfade", title: "Überblendung", in: app, .down, presses: 3))
        XCUIRemote.shared.press(.select)
        let off = app.buttons["tv-sound-crossfade-choice-0"]
        XCTAssertTrue(off.waitForExistence(timeout: 10))
        XCTAssertTrue(moveFocus(to: "tv-sound-crossfade-choice-0", title: "Aus", in: app, .up, presses: 4))
        XCUIRemote.shared.press(.select)
        expectation(for: shows("Aus"), evaluatedWith: nil)
        waitForExpectations(timeout: 10)
    }
    @MainActor func testTVQueueFilterEditsTheShownEntry() throws {
        let app = try app(pausedPlayer: true)
        openTVTab(app, "Player")
        let field = app.descendants(matching: .any).matching(identifier: "tv-queue-filter").firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "The queue panel has a filter.")
        let tabs: Set<String> = ["Start", "Bibliothek", "Suche", "Favoriten", "Playlists", "Player", "Optionen", "Warteschlange", "Lyrics"]
        for _ in 0..<30 where !field.hasFocus {
            let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
            let id = focused.exists ? focused.identifier : ""
            if id.hasPrefix("tv-queue-track-") { XCUIRemote.shared.press(.up) }
            else if id.hasPrefix("tv-queue-") || (focused.exists && (tabs.contains(focused.label) || focused.elementType == .segmentedControl)) { XCUIRemote.shared.press(.down) }
            else { XCUIRemote.shared.press(.right) }
        }
        XCTAssertTrue(field.hasFocus, "The filter field takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        _ = app.keyboards.firstMatch.waitForExistence(timeout: 10)
        attach(app, name: "TV queue filter keyboard")
        app.typeText("Blaue")
        // Let the keyboard take every letter before leaving it; "Bl" alone also matches Lichtblick.
        _ = app.descendants(matching: .any).matching(NSPredicate(format: "value == %@", "Blaue")).firstMatch.waitForExistence(timeout: 5)
        XCUIRemote.shared.press(.menu)
        let count = app.descendants(matching: .any).matching(identifier: "tv-queue-filter-count").firstMatch
        XCTAssertTrue(count.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "label == %@", "1 von 6 Titeln"), evaluatedWith: count)
        waitForExpectations(timeout: 10)
        attach(app, name: "TV queue filtered")
        let shown = app.buttons["tv-queue-track-3"]
        XCTAssertTrue(shown.exists && shown.label.hasPrefix("Blaue Stunde"), "The row keeps its queue position.")
        XCTAssertFalse(app.buttons["tv-queue-track-1"].exists, "Zeitlos is filtered out.")
        for _ in 0..<6 where !shown.hasFocus { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(shown.hasFocus)
        XCUIRemote.shared.press(.select, forDuration: 1.5)
        let playNext = app.descendants(matching: .any).matching(NSPredicate(format: "label == %@", "Als Nächstes abspielen")).firstMatch
        XCTAssertTrue(playNext.waitForExistence(timeout: 5), "A long press opens the entry's menu.")
        attach(app, name: "TV filtered queue entry menu")
        // The first item is focused when the menu opens; the new order proves it was chosen.
        XCUIRemote.shared.press(.select)
        let moved = app.buttons["tv-queue-track-1"]
        expectation(for: NSPredicate(format: "exists == true AND label BEGINSWITH %@", "Blaue Stunde"), evaluatedWith: moved)
        waitForExpectations(timeout: 5)
        XCTAssertFalse(app.buttons["tv-queue-track-3"].exists, "The filtered list shows the moved entry once.")
        attach(app, name: "TV filtered queue after Als Nächstes")
        XCTAssertFalse(app.buttons["Pause"].exists, "Filtering and editing the queue never start playback.")
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
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 5))
        #else
        XCTAssertTrue(app.staticTexts["Deine Musik"].firstMatch.waitForExistence(timeout: 5))
        #endif
        #endif
    }
}
