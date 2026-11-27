import XCTest
import AVFoundation
#if os(iOS)
import UIKit
#endif

final class PlayerUITests: XCTestCase {
    /// `oldServer`: the fixture's second port, which answers like server 1.10 (no `features`).
    /// `unreachable`: a loopback port where nothing listens, so connecting fails.
    @MainActor private func app(player: Bool = false, pausedPlayer: Bool = false, largeText: Bool = false, light: Bool = false, oldServer: Bool = false, unreachable: Bool = false) throws -> XCUIApplication {
        var server = ProcessInfo.processInfo.environment["YTMDL_FIXTURE_URL"] ?? "http://127.0.0.1:59583"
        guard let host = URL(string: server)?.host, ["127.0.0.1", "localhost"].contains(host) else { throw NSError(domain: "Loopback fixture required", code: 1) }
        if oldServer, var parts = URLComponents(string: server), let port = parts.port {
            parts.port = port + 1
            server = parts.string ?? server
        }
        if unreachable { server = "http://127.0.0.1:59599" }
        let app = XCUIApplication()
        app.launchArguments = ["--fixture-server", server]
        if player { app.launchArguments.append("--fixture-player") }
        if pausedPlayer { app.launchArguments.append("--fixture-paused-player") }
        if largeText { app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"] }
        // The app's own appearance setting, for this launch only (argument domain).
        if light { app.launchArguments += ["-appearance", "light"] }
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
    /// The settings open from the gear on Start's first page (the tab bar
    /// keeps Start, Bibliothek, Playlists and Suche).
    @MainActor private func openSettings(_ app: XCUIApplication) {
        selectTab(app, "Start")
        let gear = app.buttons["home-settings"]
        // Choosing the shown tab again returns to its first page.
        if !gear.waitForExistence(timeout: 3) { selectTab(app, "Start") }
        if !gear.waitForExistence(timeout: 10) { print(app.debugDescription) }
        gear.tap()
        XCTAssertTrue(app.navigationBars["Einstellungen"].waitForExistence(timeout: 10))
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
    /// Opens "Jetzt läuft" from the mini player and waits for its transport.
    @MainActor private func openPlayer(_ app: XCUIApplication) {
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        mini.tap()
        XCTAssertTrue(app.buttons["mobile-player-toggle"].waitForExistence(timeout: 10))
    }
    /// Opens the queue sheet from the player and waits for its first upcoming row.
    @MainActor private func openQueue(_ app: XCUIApplication) {
        let button = app.buttons["mobile-player-queue"]
        for _ in 0..<6 where !button.isHittable { app.swipeUp() }
        button.tap()
        if !app.buttons["mobile-queue-track-1"].waitForExistence(timeout: 10) { print(app.debugDescription) }
        XCTAssertTrue(app.buttons["mobile-queue-track-1"].exists)
    }
    @MainActor func testQueueManagementAndNormalizationSettingsWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact player test requires iPhone.")
        let app = try app(pausedPlayer: true)
        openPlayer(app)
        XCTAssertEqual(app.buttons["mobile-player-toggle"].label, "Abspielen")
        // Everything of the player fits at default text size: no scrolling to
        // reach the tools, and nothing sits under the navigation bar.
        let werkzeuge = app.buttons["Werkzeuge"]
        let playerBar = app.navigationBars["Jetzt läuft"]
        XCTAssertTrue(werkzeuge.isHittable)
        XCTAssertGreaterThan(werkzeuge.frame.minY, playerBar.frame.maxY)
        XCTAssertLessThanOrEqual(werkzeuge.frame.maxY, app.frame.maxY)
        attach(app, name: "Player paused")
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

        openQueue(app)
        attach(app, name: "Queue sheet")
        // Moving an upcoming row down from its menu.
        let title = app.buttons["mobile-queue-track-1"].label
        app.buttons["mobile-queue-actions-1"].tap()
        XCTAssertTrue(app.buttons["Nach unten"].waitForExistence(timeout: 5))
        app.buttons["Nach unten"].tap()
        XCTAssertTrue(app.buttons["mobile-queue-track-2"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.buttons["mobile-queue-track-2"].label, title)
        // "Als Nächstes spielen" moves a later row up into its own section,
        // right after the current title. (The row's context menu offers the
        // same items; a long press is left out here because it also lifts the
        // row for dragging.)
        let later = app.buttons["mobile-queue-track-4"]
        XCTAssertTrue(later.waitForExistence(timeout: 5))
        let laterTitle = later.label
        app.buttons["mobile-queue-actions-4"].tap()
        let playNext = app.buttons["Als Nächstes spielen"].firstMatch
        XCTAssertTrue(playNext.waitForExistence(timeout: 5))
        playNext.tap()
        expectation(for: NSPredicate(format: "label == %@", laterTitle), evaluatedWith: app.buttons["mobile-queue-track-1"])
        waitForExpectations(timeout: 5)
        let heading = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Als Nächstes")).firstMatch
        XCTAssertTrue(heading.waitForExistence(timeout: 5), "The queued title gets the section \"Als Nächstes\".")
        attach(app, name: "Queue with queued title")
        // The modes at the top switch and say so.
        let shuffle = app.buttons["mobile-queue-shuffle"]
        XCTAssertEqual(shuffle.value as? String, "Aus")
        shuffle.tap()
        XCTAssertEqual(shuffle.value as? String, "Ein")
        attach(app, name: "Queue shuffled")
        shuffle.tap()
        XCTAssertEqual(shuffle.value as? String, "Aus")
        // Clearing what follows can be undone.
        let tools = app.buttons["mobile-queue-tools"]
        tools.tap()
        XCTAssertTrue(app.buttons["Nächste leeren"].waitForExistence(timeout: 5))
        app.buttons["Nächste leeren"].tap()
        let undo = app.buttons["queue-undo"]
        XCTAssertTrue(undo.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["mobile-queue-track-1"].exists)
        attach(app, name: "Queue cleared with undo")
        undo.tap()
        XCTAssertTrue(app.buttons["mobile-queue-track-1"].waitForExistence(timeout: 5))
        tools.tap()
        XCTAssertTrue(app.buttons["Als Playlist speichern"].waitForExistence(timeout: 5))
        app.buttons["Als Playlist speichern"].tap()
        XCTAssertTrue(app.buttons["Erstellen und Titel hinzufügen"].waitForExistence(timeout: 5))
        app.navigationBars["Zur Playlist hinzufügen"].buttons["Schließen"].tap()
        let done = app.buttons["mobile-queue-done"]
        XCTAssertTrue(done.waitForExistence(timeout: 10))
        done.tap()
        let close = app.navigationBars["Jetzt läuft"].buttons["Schließen"]
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        close.tap()
        XCTAssertTrue(app.buttons["mobile-mini-player-open"].isHittable)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    @MainActor func testPlayerModesLyricsAndArtistLinkWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact player test requires iPhone.")
        let app = try app(pausedPlayer: true)
        openPlayer(app)
        for id in ["mobile-player-scrubber", "mobile-player-volume", "mobile-player-airplay", "mobile-player-favorite",
                   "mobile-player-lyrics", "mobile-player-sleep", "mobile-player-tools", "mobile-player-queue"] {
            XCTAssertTrue(app.descendants(matching: .any)[id].firstMatch.exists, id)
        }
        XCTAssertTrue(app.staticTexts["playbackRemaining"].exists)
        for id in ["mobile-player-shuffle", "mobile-player-previous", "mobile-player-toggle", "mobile-player-next", "mobile-player-repeat"] {
            let control = app.buttons[id]
            XCTAssertTrue(control.isHittable, id)
            XCTAssertGreaterThanOrEqual(control.frame.width, 44, id)
            XCTAssertGreaterThanOrEqual(control.frame.height, 44, id)
        }
        let shuffle = app.buttons["mobile-player-shuffle"], repeatButton = app.buttons["mobile-player-repeat"]
        XCTAssertEqual(shuffle.value as? String, "Aus")
        shuffle.tap()
        XCTAssertEqual(shuffle.value as? String, "Ein")
        XCTAssertEqual(repeatButton.value as? String, "Wiederholung aus")
        repeatButton.tap()
        XCTAssertEqual(repeatButton.value as? String, "Warteschlange wiederholen")
        attach(app, name: "Player with shuffle and repeat on")
        repeatButton.tap()
        XCTAssertEqual(repeatButton.value as? String, "Einzeltitel wiederholen")
        repeatButton.tap()
        XCTAssertEqual(repeatButton.value as? String, "Wiederholung aus")
        shuffle.tap()
        XCTAssertEqual(shuffle.value as? String, "Aus")
        // Lyrics take the cover's place and give it back.
        app.buttons["mobile-player-lyrics"].tap()
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label CONTAINS %@", "Durch die Stadt bei Nacht")).firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.descendants(matching: .any)["mobile-player-cover"].exists)
        attach(app, name: "Player lyrics")
        app.buttons["mobile-player-lyrics"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["mobile-player-cover"].waitForExistence(timeout: 5))
        // The sleep timer is one menu away and shows that it runs.
        app.buttons["mobile-player-sleep"].tap()
        let fifteen = app.buttons["15 Minuten"].firstMatch
        XCTAssertTrue(fifteen.waitForExistence(timeout: 5))
        fifteen.tap()
        expectation(for: NSPredicate(format: "value BEGINSWITH %@", "Endet um"), evaluatedWith: app.buttons["mobile-player-sleep"])
        waitForExpectations(timeout: 5)
        app.buttons["mobile-player-sleep"].tap()
        let off = app.buttons["Aus"].firstMatch
        XCTAssertTrue(off.waitForExistence(timeout: 5))
        off.tap()
        // The artist opens its page in the tab the player was opened from.
        let artist = app.buttons["mobile-player-artist"]
        XCTAssertTrue(artist.exists)
        XCTAssertEqual(artist.label, "Nordlicht")
        artist.tap()
        XCTAssertTrue(app.navigationBars["Nordlicht"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.navigationBars["Jetzt läuft"].exists)
        XCTAssertTrue(app.buttons["mobile-mini-player-open"].isHittable)
        attach(app, name: "Artist opened from the player")
        XCTAssertFalse(app.buttons["Pause"].exists)
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
        for title in ["Playlists", "Bibliothek", "Start"] {
            selectTab(app, title)
            XCTAssertTrue(mini.isHittable)
        }
        attach(app, name: "Start title wraps at largest text")
        openSettings(app)
        XCTAssertTrue(mini.isHittable)
        selectTab(app, "Start")
        mini.tap()
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.isHittable)
        let previous = app.buttons["mobile-player-previous"]
        for _ in 0..<6 where !previous.isHittable { app.swipeUp() }
        // Fixed-size targets: modes 44 pt, skips 64 pt, never growing with text.
        for (id, largest) in [("mobile-player-shuffle", 50.0), ("mobile-player-previous", 72.0), ("mobile-player-next", 72.0), ("mobile-player-repeat", 50.0)] {
            let control = app.buttons[id]
            XCTAssertTrue(control.isHittable, id)
            XCTAssertGreaterThanOrEqual(control.frame.width, 44, id)
            XCTAssertGreaterThanOrEqual(control.frame.height, 44, id)
            XCTAssertLessThanOrEqual(control.frame.height, largest, id)
        }
        attach(app, name: "Accessible large player")
        // The rest of the player scrolls into view, the queue opens from it.
        let queueButton = app.buttons["mobile-player-queue"]
        for _ in 0..<6 where !queueButton.isHittable { app.swipeUp() }
        XCTAssertTrue(queueButton.isHittable)
        attach(app, name: "Accessible large player scrolled")
        queueButton.tap()
        XCTAssertTrue(app.buttons["mobile-queue-done"].waitForExistence(timeout: 10))
        attach(app, name: "Accessible large queue")
        app.buttons["mobile-queue-done"].tap()
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.waitForExistence(timeout: 10))
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
    /// The app is designed dark first: Start, Bibliothek, an album and the
    /// player in the light appearance, for a look at contrast.
    @MainActor func testLightAppearanceScreensWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact screens require iPhone.")
        // No loaded player: its mini player would also read "Nachtfahrt".
        let app = try app(light: true)
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        XCTAssertTrue(app.buttons["home-settings"].isHittable)
        attach(app, name: "Light Start")
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Light library")
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        let play = app.buttons["collection-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: play)
        waitForExpectations(timeout: 10)
        attach(app, name: "Light album page")
        // A confirmation after "Zur Warteschlange", like on every track row.
        // On the empty queue the title becomes the current one, paused.
        app.buttons["Aktionen für Nachtfahrt"].firstMatch.tap()
        let enqueue = app.buttons["Zur Warteschlange"].firstMatch
        XCTAssertTrue(enqueue.waitForExistence(timeout: 5))
        enqueue.tap()
        XCTAssertTrue(app.descendants(matching: .any)["app-notice"].waitForExistence(timeout: 5), "Adding to the queue is confirmed.")
        attach(app, name: "Light queue confirmation")
        openPlayer(app)
        attach(app, name: "Light player")
        app.buttons["Schließen"].firstMatch.tap()
        XCTAssertFalse(app.buttons["Pause"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    @MainActor func testRepeatedMobileTabSwitchesKeepPausedMiniPlayerReachable() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact navigation requires iPhone.")
        let app = try app(pausedPlayer: true)
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        for _ in 0..<3 {
            for tab in ["Suche", "Bibliothek", "Playlists", "Start"] {
                selectTab(app, tab)
                XCTAssertTrue(mini.isHittable, "Mini-player remains reachable after " + tab)
                XCTAssertFalse(app.alerts.firstMatch.exists)
            }
        }
        XCTAssertFalse(app.tabBars.buttons["Einstellungen"].exists, "The settings sit behind the gear on Start, not in the tab bar.")
        openSettings(app)
        XCTAssertTrue(mini.isHittable, "Mini-player remains reachable in the settings.")
        selectTab(app, "Start")
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
        // The sixth album sits below the shortcuts and the first rows.
        let album = app.staticTexts["Lichtblick"].firstMatch
        for _ in 0..<5 where !(album.exists && album.isHittable) { app.swipeUp() }
        album.tap()
        let retry = app.buttons["Erneut laden"]
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
            // Layout lands on fractional points (51.99…); half a point is rounding.
            XCTAssertGreaterThanOrEqual(control.frame.height, 51.5)
            XCTAssertGreaterThanOrEqual(control.frame.width, 120)
        }
        // Queue and offline sit in the header's action row; "…" keeps editing.
        XCTAssertTrue(app.buttons["collection-offline"].isHittable)
        XCTAssertTrue(app.buttons["collection-queue"].isHittable)
        app.buttons["playlist-collection-options"].tap()
        XCTAssertTrue(app.buttons["Bearbeiten"].firstMatch.waitForExistence(timeout: 10))
        // Dismiss the menu with a tap beside it; the menu hangs from the
        // top right, so the left edge never lands on one of its entries.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.06, dy: 0.55)).tap()
        XCTAssertTrue(app.buttons["Bearbeiten"].firstMatch.waitForNonExistence(timeout: 5))
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
        openSettings(app)
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
        let save = app.buttons["collection-offline"]
        XCTAssertTrue(save.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: save)
        waitForExpectations(timeout: 10)
        save.tap()
        selectTab(app, "Bibliothek")
        app.buttons["Offline"].firstMatch.tap()
        XCTAssertTrue(app.buttons["offline-collection-p0"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["6 von 6 Titeln offline"].waitForExistence(timeout: 30))
        // Enter the same local-only mode available without a server session.
        openSettings(app)
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
        let mini = app.buttons["mobile-mini-player-open"].firstMatch
        XCTAssertTrue(mini.waitForExistence(timeout: 20))
        XCTAssertTrue(mini.isHittable)
        XCTAssertTrue(mini.label.hasPrefix("Player öffnen: Nachtfahrt"))
        XCTAssertLessThanOrEqual(mini.frame.width, 640, "The tablet mini player keeps a limited width.")
        attach(app, name: "Tablet mini player")
        mini.tap()
        XCTAssertTrue(app.navigationBars["Jetzt läuft"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["mobile-player-toggle"].isHittable, "Transport must fit the tablet sheet without scrolling.")
        XCTAssertTrue(app.buttons["Werkzeuge"].firstMatch.isHittable)
        let cover = app.descendants(matching: .any)["mobile-player-cover"]
        XCTAssertTrue(cover.exists)
        XCTAssertLessThanOrEqual(cover.frame.width, 421, "The cover stops growing on wide sheets.")
        XCTAssertFalse(app.buttons["Pause"].exists)
        attach(app, name: "Tablet player controls without audio")
        app.buttons["mobile-player-queue"].tap()
        XCTAssertTrue(app.buttons["mobile-queue-track-1"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet queue")
        app.buttons["mobile-queue-done"].tap()
        XCTAssertTrue(app.buttons["Schließen"].firstMatch.waitForExistence(timeout: 10))
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
    /// Bibliothek: shortcuts, sort that survives a relaunch, list layout,
    /// the filter and the genre chips.
    @MainActor func testMobileLibrarySortPersistsAndFiltersWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact library requires iPhone.")
        var app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Bibliothek")
        for id in ["favorites", "artists", "tracks", "genres", "offline", "playlists"] {
            let tile = app.buttons["library-shortcut-" + id]
            XCTAssertTrue(tile.waitForExistence(timeout: 10), id)
            XCTAssertTrue(tile.isHittable, id)
            XCTAssertGreaterThanOrEqual(tile.frame.height, 44, id)
        }
        // The labels of a row of tiles share one baseline ("Offline" sat lower).
        let favorites = app.buttons["library-shortcut-favorites"].frame, artists = app.buttons["library-shortcut-artists"].frame
        XCTAssertEqual(favorites.midY, artists.midY, accuracy: 1)
        XCTAssertEqual(app.buttons["library-shortcut-offline"].frame.height, app.buttons["library-shortcut-genres"].frame.height, accuracy: 1)
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Library top")
        // Sort by year; the choice is remembered across launches.
        let sort = app.buttons["library-sort"]
        XCTAssertEqual(sort.value as? String, "Zuletzt hinzugefügt")
        sort.tap()
        let year = app.buttons["Jahr"].firstMatch
        XCTAssertTrue(year.waitForExistence(timeout: 5))
        year.tap()
        expectation(for: NSPredicate(format: "value == %@", "Jahr"), evaluatedWith: sort)
        waitForExpectations(timeout: 10)
        app.terminate()
        app = try self.app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.buttons["library-sort"].waitForExistence(timeout: 10))
        XCTAssertEqual(app.buttons["library-sort"].value as? String, "Jahr", "The album order survives a relaunch.")
        // The list layout, also remembered.
        let layout = app.buttons["library-layout"]
        XCTAssertEqual(layout.label, "Als Liste anzeigen")
        layout.tap()
        XCTAssertEqual(layout.label, "Als Raster anzeigen")
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Library as list")
        // The filter asks the server and shows only matching albums.
        let filter = app.textFields["library-filter"]
        filter.tap(); filter.typeText("Zeit")
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.staticTexts["Nachtfahrt"].firstMatch)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.exists)
        attach(app, name: "Library filtered")
        app.buttons["Filter leeren"].tap()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        // Genre chips filter in place and say which one is chosen.
        let pop = app.buttons["library-genre-Pop"]
        XCTAssertTrue(pop.waitForExistence(timeout: 10))
        pop.tap()
        expectation(for: NSPredicate(format: "selected == true"), evaluatedWith: pop)
        waitForExpectations(timeout: 10)
        attach(app, name: "Library genre chip")
        app.buttons["library-genre-all"].tap()
        // Back to the defaults for the other tests.
        layout.tap()
        app.buttons["library-sort"].tap()
        app.buttons["Zuletzt hinzugefügt"].firstMatch.tap()
        expectation(for: NSPredicate(format: "value == %@", "Zuletzt hinzugefügt"), evaluatedWith: app.buttons["library-sort"])
        waitForExpectations(timeout: 10)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// The album page: header, artist link, numbered titles, the action row
    /// and "Mehr von …"; then the artist page with its sections.
    @MainActor func testMobileAlbumAndArtistPagesWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact pages require iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        let play = app.buttons["collection-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: play)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["collection-shuffle"].isHittable)
        for id in ["collection-favorite", "collection-offline", "collection-queue", "collection-add-to-playlist"] {
            let action = app.buttons[id]
            XCTAssertTrue(action.exists, id)
            XCTAssertGreaterThanOrEqual(action.frame.height, 44, id)
        }
        XCTAssertEqual(app.buttons["collection-offline"].label, "Offline speichern")
        let artist = app.buttons["collection-artist"]
        XCTAssertTrue(artist.waitForExistence(timeout: 10), "The album links its artist.")
        XCTAssertEqual(artist.label, "Künstler: Nordlicht")
        XCTAssertTrue(app.staticTexts.containing(NSPredicate(format: "label BEGINSWITH %@", "Album · 2020")).firstMatch.exists)
        XCTAssertTrue(app.buttons["Aktionen für Nachtfahrt"].exists)
        attach(app, name: "Album page")
        // Every title of the album is a favorite in the fixture: the heart says so and toggles all.
        let favorite = app.buttons["collection-favorite"]
        XCTAssertEqual(favorite.label, "Aus Favoriten entfernen")
        favorite.tap()
        expectation(for: NSPredicate(format: "label == %@", "Alle zu Favoriten hinzufügen"), evaluatedWith: favorite)
        waitForExpectations(timeout: 10)
        favorite.tap()
        expectation(for: NSPredicate(format: "label == %@", "Aus Favoriten entfernen"), evaluatedWith: favorite)
        waitForExpectations(timeout: 10)
        // The queue menu offers both ways in, with the queue's labels.
        app.buttons["collection-queue"].tap()
        XCTAssertTrue(app.buttons["Als Nächstes spielen"].firstMatch.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Zur Warteschlange"].firstMatch.exists)
        attach(app, name: "Album queue menu")
        app.navigationBars.firstMatch.tap()
        // More by the artist at the end of the page.
        let more = app.staticTexts["Mehr von Nordlicht"]
        for _ in 0..<4 where !more.exists { app.swipeUp() }
        XCTAssertTrue(more.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["release-card-r2"].exists)
        attach(app, name: "Album page end")
        for _ in 0..<4 where !artist.isHittable { app.swipeDown() }
        artist.tap()
        XCTAssertTrue(app.navigationBars["Nordlicht"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Beliebte Titel"].waitForExistence(timeout: 10))
        attach(app, name: "Artist page")
        let albums = app.staticTexts["Alben"].firstMatch
        for _ in 0..<4 where !(albums.exists && albums.isHittable) { app.swipeUp() }
        XCTAssertTrue(albums.waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Singles & EPs"].firstMatch.waitForExistence(timeout: 10), "The EP is listed apart from the albums.")
        XCTAssertTrue(app.buttons["release-card-r4"].exists)
        let order = app.buttons["artist-album-sort"]
        XCTAssertEqual(order.value as? String, "Neueste zuerst")
        attach(app, name: "Artist albums")
        order.tap()
        app.buttons["Älteste zuerst"].firstMatch.tap()
        expectation(for: NSPredicate(format: "value == %@", "Älteste zuerst"), evaluatedWith: order)
        waitForExpectations(timeout: 10)
        XCTAssertLessThan(app.buttons["release-card-r0"].frame.minX, app.buttons["release-card-r2"].frame.minX, "Oldest album first.")
        for _ in 0..<3 { app.swipeUp() }
        attach(app, name: "Artist all titles")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// Künstler A–Z with sections and the filter; Titel and Genres open.
    @MainActor func testMobileArtistListTitlesAndGenresWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact library requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-artists"].tap()
        XCTAssertTrue(app.navigationBars["Künstler"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["artist-row-a0"].waitForExistence(timeout: 10))
        for letter in ["A", "M", "N", "O", "#"] {
            XCTAssertTrue(app.staticTexts[letter].firstMatch.exists, "Section " + letter)
        }
        XCTAssertLessThan(app.buttons["artist-row-a2"].frame.minY, app.buttons["artist-row-a1"].frame.minY, "A before M.")
        attach(app, name: "Artists A to Z")
        let filter = app.textFields["artist-filter"]
        filter.tap(); filter.typeText("Mir")
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.buttons["artist-row-a0"])
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["artist-row-a1"].exists)
        attach(app, name: "Artists filtered")
        app.buttons["Filter leeren"].tap()
        app.buttons["artist-row-a1"].tap()
        XCTAssertTrue(app.navigationBars["Mira"].waitForExistence(timeout: 10))
        attach(app, name: "Artist page from the list")
        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-tracks"].tap()
        XCTAssertTrue(app.navigationBars["Titel"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["Aktionen für Nachtfahrt"].waitForExistence(timeout: 10))
        attach(app, name: "All titles")
        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-genres"].tap()
        XCTAssertTrue(app.buttons["genre-row-Pop"].waitForExistence(timeout: 10))
        app.buttons["genre-row-Pop"].tap()
        XCTAssertTrue(app.navigationBars["Pop"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Genre albums")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// iPad: the library, an album and its artist use the width on purpose.
    @MainActor func testTabletLibraryAlbumAndArtistPagesWithoutPlayback() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "Tablet layout requires iPad.")
        XCUIDevice.shared.orientation = .landscapeLeft
        defer { XCUIDevice.shared.orientation = .portrait }
        let app = try app()
        XCTAssertTrue(app.staticTexts["Bibliothek"].firstMatch.waitForExistence(timeout: 20))
        app.staticTexts["Bibliothek"].firstMatch.tap()
        XCTAssertTrue(app.buttons["library-shortcut-artists"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.staticTexts["Zeitlos"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Tablet library")
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        XCTAssertTrue(app.buttons["collection-play"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["collection-artist"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet album page")
        app.buttons["collection-artist"].tap()
        XCTAssertTrue(app.staticTexts["Beliebte Titel"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet artist page")
        app.staticTexts["Künstler"].firstMatch.tap()
        XCTAssertTrue(app.buttons["artist-row-a0"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet artists")
        app.staticTexts["Playlists"].firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Abends unterwegs"].firstMatch.waitForExistence(timeout: 10))
        attach(app, name: "Tablet playlists")
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        XCTAssertTrue(app.buttons["collection-play"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet playlist page")
        // The genres with their covers, and a confirmation clear of the navigation bar.
        app.staticTexts["Bibliothek"].firstMatch.tap()
        let genres = app.buttons["library-shortcut-genres"]
        XCTAssertTrue(genres.waitForExistence(timeout: 10))
        genres.tap()
        let row = app.buttons["genre-row-Ambient"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Tablet library genres")
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["genre-header"].waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Tablet genre page")
        app.buttons["genre-page-more"].tap()
        let pin = app.buttons["An Start anheften"].firstMatch
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        pin.tap()
        let notice = app.descendants(matching: .any)["app-notice"]
        XCTAssertTrue(notice.waitForExistence(timeout: 5))
        XCTAssertGreaterThan(notice.frame.minY, app.navigationBars.firstMatch.frame.maxY, "The confirmation never covers the navigation bar.")
        attach(app, name: "Tablet confirmation on a pushed page")
        // Unpinned again, so Start keeps its defaults for the other tests.
        app.buttons["genre-page-more"].tap()
        let unpin = app.buttons["Von Start lösen"].firstMatch
        XCTAssertTrue(unpin.waitForExistence(timeout: 5))
        unpin.tap()
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }

    // MARK: Für dich, radio and Start anpassen

    /// Scrolls the page until `element` can be tapped.
    @MainActor private func reveal(_ element: XCUIElement, in app: XCUIApplication, up: Bool = true) {
        for _ in 0..<8 where !(element.exists && element.isHittable) { up ? app.swipeUp() : app.swipeDown() }
    }
    /// Opens "Start anpassen" from the end of Start and puts Start back to its defaults.
    @MainActor private func openHomeEditor(_ app: XCUIApplication, reset: Bool = true) {
        let customize = app.buttons["home-customize"]
        reveal(customize, in: app)
        XCTAssertTrue(customize.waitForExistence(timeout: 10))
        customize.tap()
        XCTAssertTrue(app.navigationBars["Start anpassen"].waitForExistence(timeout: 10))
        let resetButton = app.buttons["home-editor-reset"]
        if reset, resetButton.waitForExistence(timeout: 5) {
            reveal(resetButton, in: app)
            if resetButton.isEnabled {
                resetButton.tap()
                let confirm = app.buttons["Zurücksetzen"].firstMatch
                XCTAssertTrue(confirm.waitForExistence(timeout: 5))
                confirm.tap()
                expectation(for: NSPredicate(format: "enabled == false"), evaluatedWith: resetButton)
                waitForExpectations(timeout: 5)
            }
            reveal(app.buttons["home-editor-toggle-quick_access"], in: app, up: false)
        }
    }
    /// "Für dich": its sections, a genre with its radio and a mix page with all actions.
    @MainActor func testForYouHubShowsSectionsAndOpensAMix() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact hub requires iPhone.")
        let app = try app()
        let entry = app.buttons["home-quick-foryou"]
        XCTAssertTrue(entry.waitForExistence(timeout: 20), "Start has its own way into Für dich.")
        entry.tap()
        XCTAssertTrue(app.navigationBars["Für dich"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["foryou-featured-genre-0a1b2c3d4e5f"].waitForExistence(timeout: 10), "The first mix is the large one.")
        for title in ["Für dich gemacht", "Entdecken", "Rückblick"] {
            XCTAssertTrue(app.staticTexts[title].firstMatch.waitForExistence(timeout: 10), title)
        }
        attach(app, name: "Für dich")
        let unknownKind = app.buttons["mix-card-time_capsule"]
        reveal(unknownKind, in: app)
        XCTAssertTrue(unknownKind.waitForExistence(timeout: 5), "A mix kind the app does not know still appears.")
        let genres = app.staticTexts["Deine Genres"].firstMatch
        reveal(genres, in: app)
        XCTAssertTrue(genres.exists)
        let artists = app.staticTexts["Deine Top-Künstler"].firstMatch
        reveal(artists, in: app)
        XCTAssertTrue(artists.exists, "Top artists come from the most played titles.")
        XCTAssertTrue(app.buttons["foryou-artist-radio-Mira"].exists)
        attach(app, name: "Für dich genres and top artists")
        let pop = app.buttons["foryou-genre-Pop"]
        reveal(pop, in: app, up: false)
        pop.tap()
        XCTAssertTrue(app.navigationBars["Pop"].waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["genre-radio"].waitForExistence(timeout: 10))
        attach(app, name: "Genre page with radio")
        app.navigationBars["Pop"].buttons.firstMatch.tap()
        let card = app.buttons["mix-card-rediscover"]
        reveal(card, in: app, up: false)
        XCTAssertTrue(card.waitForExistence(timeout: 10))
        card.tap()
        let play = app.buttons["mix-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: play)
        waitForExpectations(timeout: 10)
        for id in ["mix-shuffle", "mix-enqueue", "mix-save-playlist", "mix-offline"] {
            XCTAssertTrue(app.buttons[id].exists, id)
        }
        attach(app, name: "Mix page with all actions")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists, "Browsing Für dich never starts playback.")
    }
    /// Covers load in the background; screenshots wait a moment for them.
    @MainActor private func settleCovers() { Thread.sleep(forTimeInterval: 1.5) }
    /// Genre tiles lead with covers of the genre's albums: Suche, the
    /// library's genres, the genre page, a pinned genre, "Für dich" and
    /// Start's optional shelf. Confirmations sit above the tab bar on root
    /// and pushed pages and never cover the back button.
    @MainActor func testGenreArtworkEverywhereAndConfirmationsClearOfNavigation() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact screens require iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        openHomeEditor(app)
        app.navigationBars["Start anpassen"].buttons.firstMatch.tap()

        selectTab(app, "Suche")
        let browse = app.buttons["search-genre-Ambient"]
        XCTAssertTrue(browse.waitForExistence(timeout: 10), "Every genre of the library can be browsed.")
        XCTAssertTrue(app.buttons["search-genre-Jazz"].exists, "A genre without albums keeps its colour tile.")
        settleCovers()
        attach(app, name: "Genre tiles in Suche")

        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-genres"].tap()
        let row = app.buttons["genre-row-Ambient"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Library genres with covers")
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["genre-header"].waitForExistence(timeout: 10), "The genre page leads with its covers.")
        XCTAssertTrue(app.buttons["genre-radio"].exists)
        settleCovers()
        attach(app, name: "Genre page header")

        // A confirmation on a pushed page stays clear of the back button.
        app.buttons["genre-page-more"].tap()
        let pin = app.buttons["An Start anheften"].firstMatch
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        pin.tap()
        let notice = app.descendants(matching: .any)["app-notice"]
        XCTAssertTrue(notice.waitForExistence(timeout: 5), "Pinning confirms briefly.")
        let bar = app.navigationBars.firstMatch
        XCTAssertGreaterThan(notice.frame.minY, bar.frame.maxY, "The confirmation never covers the navigation bar.")
        XCTAssertFalse(notice.frame.intersects(bar.buttons.firstMatch.frame), "The back button stays free.")
        XCTAssertLessThanOrEqual(notice.frame.maxY, app.tabBars.firstMatch.frame.minY + 1, "It sits above the tab bar.")
        attach(app, name: "Confirmation on a pushed page")

        // The pinned genre on Start, then the confirmation on a tab's first page.
        selectTab(app, "Start")
        selectTab(app, "Start")
        let tile = app.buttons["home-pin-genre-Ambient"]
        XCTAssertTrue(tile.waitForExistence(timeout: 10), "The pinned genre leads quick access.")
        settleCovers()
        attach(app, name: "Start with a pinned genre")
        tile.press(forDuration: 1.2)
        let unpin = app.buttons["Von Start lösen"].firstMatch
        XCTAssertTrue(unpin.waitForExistence(timeout: 5))
        unpin.tap()
        XCTAssertTrue(notice.waitForExistence(timeout: 5))
        XCTAssertLessThanOrEqual(notice.frame.maxY, app.tabBars.firstMatch.frame.minY + 1, "Above the tab bar on a root page too.")
        XCTAssertFalse(notice.frame.intersects(app.buttons["home-settings"].frame), "The settings button stays free.")
        attach(app, name: "Confirmation on a root tab")

        // "Deine Genres" in "Für dich".
        let entry = app.buttons["home-quick-foryou"]
        XCTAssertTrue(entry.waitForExistence(timeout: 10))
        entry.tap()
        let forYouGenre = app.buttons["foryou-genre-Ambient"]
        reveal(forYouGenre, in: app)
        XCTAssertTrue(forYouGenre.exists)
        settleCovers()
        attach(app, name: "Für dich genre tiles")

        // Start's optional "Deine Genres" shelf.
        selectTab(app, "Start")
        selectTab(app, "Start")
        openHomeEditor(app)
        let show = app.buttons["home-editor-toggle-genres"]
        reveal(show, in: app)
        XCTAssertTrue(show.waitForExistence(timeout: 5))
        show.tap()
        app.navigationBars["Start anpassen"].buttons.firstMatch.tap()
        let shelfTile = app.buttons["home-genre-Ambient"]
        reveal(shelfTile, in: app)
        XCTAssertTrue(shelfTile.waitForExistence(timeout: 10), "Start shows the genres once chosen.")
        settleCovers()
        attach(app, name: "Start genre shelf")
        // Back to the defaults for the other tests.
        openHomeEditor(app)
        app.navigationBars["Start anpassen"].buttons.firstMatch.tap()
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// The genre tiles and the album page's action tiles in the light appearance.
    @MainActor func testLightGenreArtworkAndAlbumActions() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact screens require iPhone.")
        let app = try app(light: true)
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Suche")
        XCTAssertTrue(app.buttons["search-genre-Ambient"].waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Light genre tiles in Suche")
        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-genres"].tap()
        let row = app.buttons["genre-row-Pop"]
        XCTAssertTrue(row.waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Light library genres")
        row.tap()
        XCTAssertTrue(app.descendants(matching: .any)["genre-header"].waitForExistence(timeout: 10))
        settleCovers()
        attach(app, name: "Light genre page")
        selectTab(app, "Bibliothek")
        selectTab(app, "Bibliothek")
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 10))
        app.staticTexts["Nachtfahrt"].firstMatch.tap()
        let play = app.buttons["collection-play"]
        XCTAssertTrue(play.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: play)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["collection-offline"].exists)
        attach(app, name: "Light album action tiles")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    /// "Radio starten" on a title row and "Künstler-Radio" on the artist page:
    /// each fills the queue behind its seed and shows the radio as the source.
    @MainActor func testRadioFromTrackRowAndArtistPageFillsTheQueue() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact radio flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        selectTab(app, "Bibliothek")
        app.buttons["library-shortcut-tracks"].tap()
        let actions = app.buttons["Aktionen für Zeitlos"]
        XCTAssertTrue(actions.waitForExistence(timeout: 10))
        actions.tap()
        let radio = app.buttons["Radio starten"].firstMatch
        XCTAssertTrue(radio.waitForExistence(timeout: 5))
        radio.tap()
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15), "The radio starts a queue.")
        expectation(for: NSPredicate(format: "label CONTAINS %@", "Radio: Zeitlos"), evaluatedWith: mini)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(mini.label.contains("Zeitlos"), "The seed plays first.")
        attach(app, name: "Song radio confirmed")
        app.buttons["mobile-mini-player-toggle"].tap()
        mini.tap()
        let source = app.descendants(matching: .any)["mobile-player-source"]
        XCTAssertTrue(source.waitForExistence(timeout: 10))
        XCTAssertTrue(source.label.contains("Radio: Zeitlos"))
        attach(app, name: "Player playing from a radio")
        openQueue(app)
        let queueSource = app.descendants(matching: .any)["mobile-queue-source"]
        XCTAssertTrue(queueSource.waitForExistence(timeout: 5))
        XCTAssertTrue(queueSource.label.contains("Radio: Zeitlos"))
        XCTAssertTrue(app.buttons["mobile-queue-track-2"].exists, "The radio's titles follow the seed.")
        attach(app, name: "Radio queue")
        app.buttons["mobile-queue-done"].tap()
        let close = app.navigationBars["Jetzt läuft"].buttons["Schließen"]
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        close.tap()

        // The artist page's radio.
        selectTab(app, "Bibliothek")
        selectTab(app, "Bibliothek")
        let artists = app.buttons["library-shortcut-artists"]
        XCTAssertTrue(artists.waitForExistence(timeout: 10))
        artists.tap()
        XCTAssertTrue(app.buttons["artist-row-a0"].waitForExistence(timeout: 10))
        app.buttons["artist-row-a0"].tap()
        let artistRadio = app.buttons["collection-radio"]
        XCTAssertTrue(artistRadio.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: artistRadio)
        waitForExpectations(timeout: 10)
        attach(app, name: "Artist page with radio")
        artistRadio.tap()
        expectation(for: NSPredicate(format: "label CONTAINS %@", "Radio: Nordlicht"), evaluatedWith: mini)
        waitForExpectations(timeout: 15)
        app.buttons["mobile-mini-player-toggle"].tap()
        mini.tap()
        openQueue(app)
        XCTAssertTrue(app.descendants(matching: .any)["mobile-queue-source"].label.contains("Radio: Nordlicht"))
        attach(app, name: "Artist radio queue")
        app.buttons["mobile-queue-done"].tap()
        XCTAssertTrue(close.waitForExistence(timeout: 10))
        close.tap()

        // Start offers to go on and remembers both radios.
        selectTab(app, "Start")
        let resume = app.buttons["home-resume"]
        XCTAssertTrue(resume.waitForExistence(timeout: 10), "A paused queue offers Weiterhören.")
        let recent = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH %@", "home-recent-radio:artist:a0")).firstMatch
        reveal(recent, in: app)
        XCTAssertTrue(recent.exists, "Wieder einsteigen remembers the radio.")
        attach(app, name: "Start with Weiterhören and Wieder einsteigen")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    /// "Start anpassen": hiding and moving a shelf survives a relaunch; "Zurücksetzen" restores Start.
    @MainActor func testStartEditorHidesAndReordersShelvesAcrossRelaunch() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact Start requires iPhone.")
        var app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        XCTAssertTrue(app.staticTexts["Zuletzt gehört"].waitForExistence(timeout: 10))
        openHomeEditor(app)
        attach(app, name: "Start editor")
        let hide = app.buttons["home-editor-toggle-recent"]
        XCTAssertTrue(hide.waitForExistence(timeout: 5))
        XCTAssertEqual(hide.label, "Zuletzt gehört ausblenden")
        hide.tap()
        expectation(for: NSPredicate(format: "label == %@", "Zuletzt gehört einblenden"), evaluatedWith: app.buttons["home-editor-toggle-recent"])
        waitForExpectations(timeout: 5)
        // Drag "Neu in der Mediathek" by its handle above "Deine Mixe".
        let moving = app.buttons["home-editor-toggle-new_releases"], target = app.buttons["home-editor-toggle-mixes"]
        let origin = app.coordinate(withNormalizedOffset: .zero)
        let handleX = app.frame.width - 40
        var moved = false
        // The simulator sometimes misses the long press that lifts a row; try the drag again then.
        for _ in 0..<3 where !moved {
            origin.withOffset(CGVector(dx: handleX, dy: moving.frame.midY))
                .press(forDuration: 1.2, thenDragTo: origin.withOffset(CGVector(dx: handleX, dy: target.frame.minY)), withVelocity: .slow, thenHoldForDuration: 0.6)
            for _ in 0..<10 where !moved {
                moved = moving.frame.minY < target.frame.minY
                if !moved { Thread.sleep(forTimeInterval: 0.3) }
            }
        }
        if !moved { print(app.debugDescription) }
        XCTAssertTrue(moved, "Dragging the handle moves the shelf.")
        attach(app, name: "Start editor changed")
        app.navigationBars["Start anpassen"].buttons.firstMatch.tap()
        func checkStart(_ app: XCUIApplication) {
            let releases = app.staticTexts["Neu in der Mediathek"].firstMatch, mixes = app.staticTexts["Deine Mixe"].firstMatch
            XCTAssertTrue(releases.waitForExistence(timeout: 15))
            XCTAssertTrue(mixes.waitForExistence(timeout: 15))
            XCTAssertLessThan(releases.frame.minY, mixes.frame.minY, "The moved shelf comes first.")
            XCTAssertFalse(app.staticTexts["Zuletzt gehört"].exists, "The hidden shelf stays hidden.")
        }
        checkStart(app)
        attach(app, name: "Start customized")
        app.terminate()
        app = try self.app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        checkStart(app)
        // Settings lead there too; "Zurücksetzen" brings everything back.
        openSettings(app)
        let entry = app.buttons["settings-customize-home"]
        XCTAssertTrue(entry.waitForExistence(timeout: 10))
        entry.tap()
        XCTAssertTrue(app.navigationBars["Start anpassen"].waitForExistence(timeout: 10))
        let reset = app.buttons["home-editor-reset"]
        reveal(reset, in: app)
        XCTAssertTrue(reset.isEnabled)
        reset.tap()
        XCTAssertTrue(app.buttons["Zurücksetzen"].firstMatch.waitForExistence(timeout: 5))
        app.buttons["Zurücksetzen"].firstMatch.tap()
        selectTab(app, "Start")
        selectTab(app, "Start")
        XCTAssertTrue(app.staticTexts["Zuletzt gehört"].waitForExistence(timeout: 10), "Zurücksetzen shows every default shelf again.")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// "An Start anheften" puts a playlist into quick access; "Von Start lösen" takes it out.
    @MainActor func testPinAndUnpinAQuickAccessTile() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact Start requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        openHomeEditor(app)
        app.navigationBars["Start anpassen"].buttons.firstMatch.tap()
        selectTab(app, "Playlists")
        app.staticTexts["Abends unterwegs"].firstMatch.tap()
        let options = app.buttons["playlist-collection-options"]
        XCTAssertTrue(options.waitForExistence(timeout: 10))
        expectation(for: NSPredicate(format: "enabled == true"), evaluatedWith: options)
        waitForExpectations(timeout: 10)
        options.tap()
        let pin = app.buttons["An Start anheften"].firstMatch
        XCTAssertTrue(pin.waitForExistence(timeout: 5))
        pin.tap()
        XCTAssertTrue(app.descendants(matching: .any)["app-notice"].waitForExistence(timeout: 5), "Pinning confirms briefly.")
        // Start is still scrolled to its end from the editor link; a second tap returns to the top.
        selectTab(app, "Start")
        selectTab(app, "Start")
        let tile = app.buttons["home-pin-playlist-p0"]
        XCTAssertTrue(tile.waitForExistence(timeout: 10), "The pinned playlist leads quick access.")
        XCTAssertFalse(app.buttons["home-quick-playlist-p0"].exists, "A pinned playlist is not shown twice.")
        XCTAssertTrue(app.buttons["home-quick-favorites"].exists, "Defaults fill the free places.")
        attach(app, name: "Start with a pinned playlist")
        tile.press(forDuration: 1.2)
        let unpin = app.buttons["Von Start lösen"].firstMatch
        XCTAssertTrue(unpin.waitForExistence(timeout: 5))
        unpin.tap()
        expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: tile)
        waitForExpectations(timeout: 10)
        XCTAssertTrue(app.buttons["home-quick-favorites"].exists)
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }
    /// iPad: "Für dich" and "Start anpassen" use the width on purpose.
    @MainActor func testTabletForYouAndStartEditor() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "Tablet layout requires iPad.")
        XCUIDevice.shared.orientation = .landscapeLeft
        defer { XCUIDevice.shared.orientation = .portrait }
        let app = try app()
        let entry = app.buttons["home-quick-foryou"]
        XCTAssertTrue(entry.waitForExistence(timeout: 20))
        attach(app, name: "Tablet Start")
        entry.tap()
        XCTAssertTrue(app.buttons["foryou-featured-genre-0a1b2c3d4e5f"].waitForExistence(timeout: 10))
        attach(app, name: "Tablet Für dich")
        let artists = app.staticTexts["Deine Top-Künstler"].firstMatch
        reveal(artists, in: app)
        attach(app, name: "Tablet Für dich genres and artists")
        // "Deine Statistik" puts its top lists side by side.
        let stats = app.buttons["foryou-stats"]
        reveal(stats, in: app)
        XCTAssertTrue(stats.waitForExistence(timeout: 5))
        stats.tap()
        XCTAssertTrue(app.descendants(matching: .any)["stats-plays"].waitForExistence(timeout: 15))
        XCTAssertTrue(app.descendants(matching: .any)["stats-track-0"].waitForExistence(timeout: 5))
        attach(app, name: "Tablet Deine Statistik")
        app.navigationBars["Deine Statistik"].buttons.firstMatch.tap()
        XCTAssertTrue(app.navigationBars["Für dich"].waitForExistence(timeout: 10))
        app.navigationBars["Für dich"].buttons.firstMatch.tap()
        openHomeEditor(app)
        attach(app, name: "Tablet Start editor")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists)
    }

    // MARK: Server 1.11 listening features

    /// "Deine Statistik" from "Für dich": a full week, a month that starts with
    /// the history, an empty year and all time with untimed plays.
    @MainActor func testListeningStatsSwitchPeriods() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact statistics require iPhone.")
        let app = try app()
        let entry = app.buttons["home-quick-foryou"]
        XCTAssertTrue(entry.waitForExistence(timeout: 20))
        entry.tap()
        XCTAssertTrue(app.navigationBars["Für dich"].waitForExistence(timeout: 10))
        let link = app.buttons["foryou-stats"]
        reveal(link, in: app)
        // "Dein Hören" ends the page: one more swipe lifts the tiles clear of the tab bar.
        app.swipeUp()
        XCTAssertTrue(link.waitForExistence(timeout: 5), "Für dich leads to the statistics.")
        attach(app, name: "Für dich with Dein Hören")
        link.tap()
        XCTAssertTrue(app.navigationBars["Deine Statistik"].waitForExistence(timeout: 10))
        let any = app.descendants(matching: .any)
        let plays = any["stats-plays"]
        XCTAssertTrue(plays.waitForExistence(timeout: 15))
        XCTAssertTrue(any["stats-chart"].exists, "The activity chart shows the week.")
        XCTAssertTrue(app.buttons["stats-play-top"].exists)
        attach(app, name: "Statistik Woche")
        let track = any["stats-track-0"]
        reveal(track, in: app)
        XCTAssertTrue(track.exists, "The top titles follow the totals.")
        attach(app, name: "Statistik Top-Listen")
        app.swipeDown(); app.swipeDown()
        func choose(_ period: String) {
            let segment = app.buttons[period].firstMatch
            reveal(segment, in: app, up: false)
            segment.tap()
        }
        choose("Monat")
        let note = any["stats-note"]
        XCTAssertTrue(note.waitForExistence(timeout: 10))
        XCTAssertTrue(note.label.contains("Dein Verlauf beginnt am"), note.label)
        attach(app, name: "Statistik Monat unvollständig")
        choose("Jahr")
        XCTAssertTrue(any["stats-empty"].waitForExistence(timeout: 10), "An empty window says so.")
        XCTAssertFalse(app.buttons["stats-play-top"].exists)
        attach(app, name: "Statistik Jahr leer")
        choose("Gesamt")
        XCTAssertTrue(any["stats-note"].waitForExistence(timeout: 10), "Untimed plays are explained.")
        attach(app, name: "Statistik Gesamt")
        XCTAssertFalse(app.alerts.firstMatch.exists)
        XCTAssertFalse(app.buttons["Pause"].exists, "Statistics never start playback.")
    }
    /// A lyrics hit in Suche plays its title; "Weniger davon" confirms with
    /// "Rückgängig", and "Empfehlungen" lists and lifts the exclusion.
    @MainActor func testLyricsHitAndLessOfThisWithUndoAndPreferences() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact flow requires iPhone.")
        let app = try app()
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))

        // "Weniger davon" on a title row, then "Rückgängig".
        selectTab(app, "Bibliothek")
        let tracks = app.buttons["library-shortcut-tracks"]
        XCTAssertTrue(tracks.waitForExistence(timeout: 10))
        tracks.tap()
        let actions = app.buttons["Aktionen für Zeitlos"]
        XCTAssertTrue(actions.waitForExistence(timeout: 10))
        actions.tap()
        let less = app.buttons["Weniger davon"].firstMatch
        XCTAssertTrue(less.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["Künstler nicht mehr vorschlagen"].firstMatch.exists)
        attach(app, name: "Titelmenü mit Weniger davon")
        less.tap()
        let undo = app.buttons["app-notice-action"]
        XCTAssertTrue(undo.waitForExistence(timeout: 5), "The confirmation offers Rückgängig.")
        attach(app, name: "Weniger davon mit Rückgängig")
        undo.tap()
        // The confirmation without an action is one element, "app-notice".
        let notice = app.descendants(matching: .any)["app-notice"]
        expectation(for: NSPredicate(format: "label CONTAINS %@", "wieder vorgeschlagen"), evaluatedWith: notice)
        waitForExpectations(timeout: 10)
        // Again, and this time it stays.
        actions.tap()
        XCTAssertTrue(less.waitForExistence(timeout: 5), "After Rückgängig the title can be excluded again.")
        less.tap()
        XCTAssertTrue(undo.waitForExistence(timeout: 5))

        // "Empfehlungen" lists it and lifts it.
        openSettings(app)
        let preferences = app.buttons["settings-preferences"]
        reveal(preferences, in: app)
        XCTAssertTrue(preferences.waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons["settings-stats"].exists)
        preferences.tap()
        XCTAssertTrue(app.navigationBars["Empfehlungen"].waitForExistence(timeout: 10))
        let row = app.descendants(matching: .any)["preferences-row-track-t1"]
        XCTAssertTrue(row.waitForExistence(timeout: 10), "The excluded title is listed.")
        attach(app, name: "Empfehlungen mit Ausschluss")
        let allow = app.buttons["preferences-allow-track-t1"]
        XCTAssertTrue(allow.exists)
        allow.tap()
        XCTAssertTrue(app.descendants(matching: .any)["preferences-empty"].waitForExistence(timeout: 10), "Lifting the last exclusion empties the page.")
        attach(app, name: "Empfehlungen leer")

        // Suche finds a title by a line of its lyrics and plays it.
        selectTab(app, "Suche")
        let search = app.searchFields.firstMatch
        XCTAssertTrue(search.waitForExistence(timeout: 10))
        XCTAssertTrue(app.descendants(matching: .any)["search-lyrics-hint"].waitForExistence(timeout: 5), "The server searches lyrics, so Suche says so.")
        search.tap(); search.typeText("kalter")
        let hit = app.buttons["search-lyrics-t2"]
        // The group may sit below the fold of the lazy list.
        if !hit.waitForExistence(timeout: 8) || !hit.isHittable { reveal(hit, in: app) }
        XCTAssertTrue(hit.waitForExistence(timeout: 5), "Lyrics hits have their own group.")
        if !hit.exists { print(app.debugDescription) }
        // A short drag puts the keyboard away so the snippet shows.
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.45))
        from.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35)))
        if !hit.isHittable { reveal(hit, in: app) }
        attach(app, name: "Suche mit Liedtext-Treffern")
        hit.tap()
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15), "A lyrics hit plays its title.")
        expectation(for: NSPredicate(format: "label CONTAINS %@", "Fernweh"), evaluatedWith: mini)
        waitForExpectations(timeout: 10)
        attach(app, name: "Liedtext-Treffer spielt")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    /// Server 1.10: no statistics, lyrics search or exclusions anywhere, one
    /// quiet hint in Einstellungen, and the radio works as before.
    @MainActor func testOlderServerHidesListeningFeaturesAndKeepsRadio() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .phone, "Compact flow requires iPhone.")
        let app = try app(oldServer: true)
        XCTAssertTrue(app.buttons["home-quick-favorites"].waitForExistence(timeout: 20))
        XCTAssertFalse(app.descendants(matching: .any)["home-stats"].exists)
        openSettings(app)
        let hint = app.descendants(matching: .any)["settings-server-hint"]
        reveal(hint, in: app)
        XCTAssertTrue(hint.waitForExistence(timeout: 10), "Einstellungen explains what needs a newer server.")
        XCTAssertTrue(app.descendants(matching: .any)["settings-server-version"].label.contains("1.10.1"))
        XCTAssertFalse(app.buttons["settings-stats"].exists)
        XCTAssertFalse(app.buttons["settings-preferences"].exists)
        attach(app, name: "Einstellungen mit älterem Server")
        selectTab(app, "Suche")
        XCTAssertTrue(app.searchFields.firstMatch.waitForExistence(timeout: 10))
        XCTAssertFalse(app.descendants(matching: .any)["search-lyrics-hint"].exists)
        selectTab(app, "Bibliothek")
        selectTab(app, "Bibliothek")
        let tracks = app.buttons["library-shortcut-tracks"]
        XCTAssertTrue(tracks.waitForExistence(timeout: 10))
        tracks.tap()
        let actions = app.buttons["Aktionen für Zeitlos"]
        XCTAssertTrue(actions.waitForExistence(timeout: 10))
        actions.tap()
        let radio = app.buttons["Radio starten"].firstMatch
        XCTAssertTrue(radio.waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["Weniger davon"].exists, "No exclusions without the server feature.")
        radio.tap()
        let mini = app.buttons["mobile-mini-player-open"]
        XCTAssertTrue(mini.waitForExistence(timeout: 15), "The radio starts as with 0.6.0.")
        expectation(for: NSPredicate(format: "label CONTAINS %@", "Radio: Zeitlos"), evaluatedWith: mini)
        waitForExpectations(timeout: 10)
        attach(app, name: "Radio mit älterem Server")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    #endif
    #if os(tvOS)
    @MainActor func testTVHomeShowsFocusableMixes() throws {
        let app = try app()
        let first = app.buttons["tv-mix-genre-0a1b2c3d4e5f"], second = app.buttons["tv-mix-on_repeat"]
        XCTAssertTrue(first.waitForExistence(timeout: 20), "The start screen shows the server's mixes.")
        XCTAssertTrue(second.exists)
        XCTAssertTrue(app.staticTexts["Zuletzt gehört"].waitForExistence(timeout: 10))
        focusTVMixRow(app)
        for _ in 0..<4 where !first.hasFocus && !second.hasFocus { XCUIRemote.shared.press(.left) }
        XCTAssertTrue(first.hasFocus || second.hasFocus, "A mix card takes the remote's focus.")
        attach(app, name: "TV home with focused mix")
        XCTAssertFalse(app.buttons["Pause"].exists, "The start screen never starts playback.")
    }
    @MainActor func testTVForYouOpensFromStartAndFocusesAMix() throws {
        let app = try app()
        let first = app.buttons["tv-mix-genre-0a1b2c3d4e5f"], all = app.buttons["tv-all-mixes"]
        XCTAssertTrue(first.waitForExistence(timeout: 20), "The start screen shows the server's mixes.")
        XCTAssertTrue(all.exists, "Alle Mixe closes the first mix row.")
        focusTVMixRow(app)
        for _ in 0..<4 where !first.hasFocus && !all.hasFocus { XCUIRemote.shared.press(.left) }
        XCTAssertTrue(first.hasFocus || all.hasFocus, "The first mix row takes the remote's focus.")
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
        openTVQueuePanel(app)
        let target = app.buttons["tv-queue-track-1"]
        XCTAssertTrue(target.waitForExistence(timeout: 10))
        XCTAssertTrue(target.label.hasPrefix("Zeitlos"), "The fixture queue starts with Nachtfahrt, then Zeitlos.")
        // Walk down the panel (switch, actions, filter) to the first upcoming row.
        for _ in 0..<30 where !target.hasFocus {
            let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
            let id = focused.exists ? focused.identifier : ""
            if id.hasPrefix("tv-queue-track-"), let row = Int(id.dropFirst("tv-queue-track-".count)) {
                XCUIRemote.shared.press(row < 1 ? .down : .up)
            } else {
                XCUIRemote.shared.press(.down)
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
    /// Down from the tab bar into the first shelf of mixes. The tab bar is
    /// centred, so the focus lands on whichever card sits below "Start".
    @MainActor private func focusTVMixRow(_ app: XCUIApplication) {
        for _ in 0..<6 {
            let id = focusedIdentifier(app)
            if id.hasPrefix("tv-mix-") || id == "tv-all-mixes" { return }
            XCUIRemote.shared.press(.down)
        }
    }
    /// The four text tabs of the Apple TV tab bar.
    private static let tvTabNames = ["Start", "Bibliothek", "Suche", "Playlists"]
    @MainActor private func tvTabFocused(_ app: XCUIApplication) -> Bool {
        Self.tvTabNames.contains { app.buttons[$0].firstMatch.exists && app.buttons[$0].firstMatch.hasFocus }
    }
    /// From any root page: up into the tab bar, onto `tab`, and open it.
    @MainActor private func selectTVTab(_ app: XCUIApplication, _ tab: String) {
        for _ in 0..<8 where !tvTabFocused(app) { XCUIRemote.shared.press(.up) }
        let item = app.buttons[tab].firstMatch
        for _ in 0..<6 where !app.buttons["Start"].firstMatch.hasFocus { XCUIRemote.shared.press(.left) }
        for _ in 0..<6 where !item.hasFocus { XCUIRemote.shared.press(.right) }
        XCTAssertTrue(item.hasFocus, "\(tab) takes the remote's focus in the tab bar.")
        XCUIRemote.shared.press(.select)
    }
    /// The settings are the gear after Playlists.
    @MainActor private func openTVSettings(_ app: XCUIApplication) {
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        let playlists = app.buttons["Playlists"].firstMatch
        XCTAssertTrue(playlists.waitForExistence(timeout: 20))
        for _ in 0..<8 where !tvTabFocused(app) { XCUIRemote.shared.press(.up) }
        for _ in 0..<6 where !playlists.hasFocus { XCUIRemote.shared.press(.right) }
        XCTAssertTrue(playlists.hasFocus)
        XCUIRemote.shared.press(.right)
        XCTAssertFalse(playlists.hasFocus, "The settings gear follows Playlists.")
        attach(app, name: "TV tab bar with the settings gear focused")
        XCUIRemote.shared.press(.select)
    }
    /// The identifier of the focused element, or "".
    @MainActor private func focusedIdentifier(_ app: XCUIApplication) -> String {
        let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
        return focused.exists ? focused.identifier : ""
    }
    /// The label of the focused element (a List row is a cell), or "".
    @MainActor private func focusedLabel(_ app: XCUIApplication) -> String {
        let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
        return focused.exists ? focused.label : ""
    }
    /// Down from any root page onto the now-playing bar, and open it.
    @MainActor private func openTVNowPlayingFromBar(_ app: XCUIApplication) {
        let bar = app.buttons["tv-now-playing-bar"]
        XCTAssertTrue(bar.waitForExistence(timeout: 20), "The now-playing bar shows what is loaded.")
        for _ in 0..<14 where !bar.hasFocus { XCUIRemote.shared.press(.down) }
        if !bar.hasFocus { print(app.debugDescription) }
        XCTAssertTrue(bar.hasFocus, "The now-playing bar takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(app.buttons["tv-np-toggle"].waitForExistence(timeout: 10), "The bar opens Jetzt läuft.")
    }
    /// In Jetzt läuft: down to the row under the transport, onto `identifier`.
    @MainActor private func focusTVPlayerAction(_ app: XCUIApplication, _ identifier: String) -> Bool {
        let row = ["tv-np-queue", "tv-np-lyrics", "tv-player-sleep", "tv-player-speed", "tv-np-radio", "tv-np-less", "tv-player-actions"]
        for _ in 0..<4 where !row.contains(focusedIdentifier(app)) { XCUIRemote.shared.press(.down) }
        guard let target = row.firstIndex(of: identifier) else { return false }
        for _ in 0..<10 {
            let id = focusedIdentifier(app)
            if id == identifier { return true }
            guard let index = row.firstIndex(of: id) else { return false }
            XCUIRemote.shared.press(index < target ? .right : .left)
        }
        return focusedIdentifier(app) == identifier
    }
    /// Jetzt läuft from the bar, then its Warteschlange panel.
    @MainActor private func openTVQueuePanel(_ app: XCUIApplication) {
        openTVNowPlayingFromBar(app)
        let onQueue = focusTVPlayerAction(app, "tv-np-queue")
        if !onQueue { print(app.debugDescription) }
        XCTAssertTrue(onQueue, "Warteschlange takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(app.descendants(matching: .any).matching(identifier: "tv-queue-autoplay").firstMatch.waitForExistence(timeout: 10),
                      "The queue panel opens.")
    }
    /// "1:05" as seconds.
    private func seconds(_ label: String) -> Int {
        let parts = label.split(separator: ":").compactMap { Int($0) }
        return parts.count == 2 ? parts[0] * 60 + parts[1] : -1
    }
    /// The whole way of a listener: start a title from a list, Jetzt läuft
    /// opens by itself, rests, wakes, seeks, shows queue and lyrics, and the
    /// Menu button leads back to the list, where the bar keeps showing it.
    @MainActor func testTVNowPlayingOpensByItselfAndStaysVisible() throws {
        let app = try app()
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        XCTAssertFalse(app.buttons["tv-now-playing-bar"].exists, "Nothing is loaded, so there is no bar.")
        for _ in 0..<6 where !tvTabFocused(app) { XCUIRemote.shared.press(.up) }
        for tab in Self.tvTabNames { XCTAssertTrue(app.buttons[tab].firstMatch.exists, "\(tab) is a tab.") }
        XCTAssertFalse(app.buttons["Player"].exists, "There is no Player tab any more.")
        attach(app, name: "TV 01 tab bar")
        selectTVTab(app, "Playlists")
        let playlist = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Abends unterwegs")).firstMatch
        XCTAssertTrue(playlist.waitForExistence(timeout: 10), "The fixture playlist is listed.")
        for _ in 0..<6 where !focusedLabel(app).hasPrefix("Abends unterwegs") { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(focusedLabel(app).hasPrefix("Abends unterwegs"), "The playlist takes the remote's focus.")
        XCUIRemote.shared.press(.select)
        let row = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Zeitlos abspielen")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 10), "The playlist shows its titles.")
        for _ in 0..<8 where !focusedLabel(app).hasPrefix("Zeitlos abspielen") { XCUIRemote.shared.press(.down) }
        if !focusedLabel(app).hasPrefix("Zeitlos abspielen") { print(app.debugDescription) }
        XCTAssertTrue(focusedLabel(app).hasPrefix("Zeitlos abspielen"), "The second title takes the remote's focus.")
        attach(app, name: "TV 02 playlist page")
        XCUIRemote.shared.press(.select)

        // Starting a title opens Jetzt läuft by itself.
        let toggle = app.buttons["tv-np-toggle"]
        XCTAssertTrue(toggle.waitForExistence(timeout: 10), "Choosing a title opens Jetzt läuft.")
        let title = app.staticTexts["tv-np-title"]
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        XCTAssertEqual(title.label, "Zeitlos")
        XCTAssertTrue(app.buttons["tv-np-artist"].exists, "The artist is a link.")
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV 03 now playing")

        // Without input the controls fade; cover, title and progress stay.
        let next = app.buttons["tv-np-next"]
        let rested = XCTNSPredicateExpectation(predicate: NSPredicate(format: "exists == false"), object: next)
        XCTAssertEqual(XCTWaiter.wait(for: [rested], timeout: 25), .completed, "The controls fade after a while without input.")
        XCTAssertTrue(title.exists && app.staticTexts["playbackElapsed"].exists, "Title and progress stay while resting.")
        attach(app, name: "TV 04 resting")
        XCUIRemote.shared.press(.down)
        XCTAssertTrue(next.waitForExistence(timeout: 5), "Any press brings the controls back.")

        // Up from the transport onto the scrubber; right seeks, faster when repeated.
        let elapsed = app.staticTexts["playbackElapsed"]
        let transport: Set<String> = ["tv-np-shuffle", "tv-np-previous", "tv-np-toggle", "tv-np-next", "tv-player-repeat", "tv-np-favorite"]
        for _ in 0..<3 where transport.contains(focusedIdentifier(app)) { XCUIRemote.shared.press(.up) }
        let before = seconds(elapsed.label)
        XCUIRemote.shared.press(.right); XCUIRemote.shared.press(.right); XCUIRemote.shared.press(.right)
        let moved = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in self.seconds(elapsed.label) >= before + 25 }, object: nil)
        let seeked = XCTWaiter.wait(for: [moved], timeout: 5) == .completed
        attach(app, name: "TV 05 scrubbing")
        XCTAssertTrue(seeked, "Right on the scrubber seeks forward (\(before) s → \(elapsed.label)).")

        // Warteschlange: a panel beside the cover, the playing title marked.
        XCTAssertTrue(focusTVPlayerAction(app, "tv-np-queue"))
        XCUIRemote.shared.press(.select)
        let current = app.buttons["tv-queue-track-1"]
        XCTAssertTrue(current.waitForExistence(timeout: 10), "The queue panel opens.")
        XCTAssertTrue(current.label.hasPrefix("Läuft: Zeitlos"), "The playing title is marked.")
        Thread.sleep(forTimeInterval: 1)
        attach(app, name: "TV 06 queue panel")
        XCUIRemote.shared.press(.menu)
        XCTAssertTrue(current.waitForNonExistence(timeout: 5), "Menu closes the panel.")
        XCTAssertTrue(toggle.waitForExistence(timeout: 5), "Jetzt läuft stays open.")

        // Lyrics: large lines in their own panel.
        XCTAssertTrue(focusTVPlayerAction(app, "tv-np-lyrics"))
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(app.buttons["tv-lyrics-line-0"].waitForExistence(timeout: 10), "The lyrics panel shows the lines.")
        attach(app, name: "TV 07 lyrics panel")
        XCUIRemote.shared.press(.menu)
        XCTAssertTrue(toggle.waitForExistence(timeout: 5))

        // The more dialog: its buttons carry visible labels.
        XCTAssertTrue(focusTVPlayerAction(app, "tv-player-actions"))
        XCUIRemote.shared.press(.select)
        let handoff = app.buttons["Wiedergabe übergeben"].firstMatch
        XCTAssertTrue(handoff.waitForExistence(timeout: 5), "Weitere Aktionen opens.")
        attach(app, name: "TV 08 more actions dialog")
        XCUIRemote.shared.press(.menu)
        XCTAssertTrue(handoff.waitForNonExistence(timeout: 5))

        // Menu leaves Jetzt läuft; the list and the bar show what plays.
        XCUIRemote.shared.press(.menu)
        XCTAssertTrue(toggle.waitForNonExistence(timeout: 5), "Menu closes Jetzt läuft.")
        let bar = app.buttons["tv-now-playing-bar"]
        XCTAssertTrue(bar.waitForExistence(timeout: 5), "The bar shows what plays.")
        XCTAssertTrue(row.exists, "Back on the playlist.")
        attach(app, name: "TV 09 back on the list with the bar")
        for _ in 0..<12 where !bar.hasFocus { XCUIRemote.shared.press(.down) }
        XCTAssertTrue(bar.hasFocus, "The bar takes the remote's focus.")
        attach(app, name: "TV 10 bar focused")
        // Play/pause works outside the player too.
        XCUIRemote.shared.press(.playPause)
        expectation(for: NSPredicate(format: "value == %@", "Pausiert"), evaluatedWith: bar)
        waitForExpectations(timeout: 5)
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(toggle.waitForExistence(timeout: 10), "The bar opens Jetzt läuft.")
        XCTAssertEqual(toggle.label, "Abspielen")

        // The artist link opens the artist page in the shown tab.
        let artist = app.buttons["tv-np-artist"]
        for _ in 0..<4 where !artist.hasFocus { XCUIRemote.shared.press(.up) }
        XCTAssertTrue(artist.hasFocus, "The artist link takes the remote's focus.")
        attach(app, name: "TV 11 artist link focused")
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(toggle.waitForNonExistence(timeout: 5), "The link closes Jetzt läuft.")
        // Mira's page lists her titles only: Nachtfahrt (Nordlicht) leaves with the playlist.
        let otherArtist = app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "Nachtfahrt abspielen")).firstMatch
        XCTAssertTrue(otherArtist.waitForNonExistence(timeout: 10), "The artist page replaces the playlist.")
        XCTAssertTrue(app.staticTexts["Mira"].firstMatch.waitForExistence(timeout: 10), "The artist page opens.")
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV 12 artist page")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    /// The main screens as they look at 1920 × 1080, for review.
    @MainActor func testTVMainScreensForReview() throws {
        let app = try app(pausedPlayer: true)
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV Start with the bar")
        selectTVTab(app, "Bibliothek")
        XCTAssertTrue(app.staticTexts["Deine Musik"].firstMatch.waitForExistence(timeout: 10))
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV Bibliothek")
        // Into the album grid and open the focused album.
        for _ in 0..<4 { XCUIRemote.shared.press(.down) }
        attach(app, name: "TV Bibliothek album focused")
        XCUIRemote.shared.press(.select)
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV album page")
        XCUIRemote.shared.press(.menu)
        selectTVTab(app, "Suche")
        Thread.sleep(forTimeInterval: 2)
        attach(app, name: "TV Suche")
        selectTVTab(app, "Playlists")
        Thread.sleep(forTimeInterval: 1)
        attach(app, name: "TV Playlists")
        XCTAssertFalse(app.alerts.firstMatch.exists)
    }
    /// A server that does not answer: the alert says so and its button reads.
    @MainActor func testTVConnectErrorNamesTheCauseAndShowsItsButton() throws {
        let app = try app(unreachable: true)
        let alert = app.alerts.firstMatch
        XCTAssertTrue(alert.waitForExistence(timeout: 30), "The failed connection is reported.")
        let message = alert.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "antwortet nicht")).firstMatch
        XCTAssertTrue(message.exists, "The alert names the cause: the server does not answer.")
        XCTAssertTrue(alert.buttons["OK"].exists, "The alert's button has its label.")
        attach(app, name: "TV connect error alert")
        XCUIRemote.shared.press(.select)
        XCTAssertTrue(alert.waitForNonExistence(timeout: 5))
        attach(app, name: "TV connect screen")
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
        openTVSettings(app)
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
        XCTAssertTrue(app.staticTexts["Nachtfahrt"].firstMatch.waitForExistence(timeout: 20))
        openTVQueuePanel(app)
        let field = app.descendants(matching: .any).matching(identifier: "tv-queue-filter").firstMatch
        XCTAssertTrue(field.waitForExistence(timeout: 10), "The queue panel has a filter.")
        for _ in 0..<30 where !field.hasFocus {
            let focused = app.descendants(matching: .any).matching(NSPredicate(format: "hasFocus == true")).firstMatch
            let id = focused.exists ? focused.identifier : ""
            if id.hasPrefix("tv-queue-track-") { XCUIRemote.shared.press(.up) }
            else { XCUIRemote.shared.press(.down) }
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
        openTVNowPlayingFromBar(app)
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
