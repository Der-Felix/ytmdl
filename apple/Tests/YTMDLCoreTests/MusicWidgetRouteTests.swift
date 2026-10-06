import XCTest
@testable import YTMDLCore

final class MusicWidgetRouteTests: XCTestCase {
    func testOnlyExplicitNavigationRoutesAreAccepted() {
        for route in ["player", "favorites", "playlists"] {
            XCTAssertEqual(MusicWidgetRoute(url: URL(string: "ytmdl-player://" + route)!)?.rawValue, route)
        }
        for value in ["https://player", "ytmdl-player://download", "ytmdl-player://player/track",
                      "ytmdl-player://player?autoplay=true", "ytmdl-player://player#play",
                      "ytmdl-player://user@player", "ytmdl-player://player:8080"] {
            XCTAssertNil(MusicWidgetRoute(url: URL(string: value)!))
        }
    }
}
