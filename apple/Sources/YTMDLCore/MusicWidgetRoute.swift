import Foundation

public enum MusicWidgetRoute: String, Sendable {
    case player, favorites, playlists
    public init?(url: URL) {
        guard url.scheme?.lowercased() == "ytmdl-player", url.user == nil, url.password == nil,
              url.port == nil, url.query == nil, url.fragment == nil,
              url.path.isEmpty || url.path == "/", let host = url.host,
              let route = Self(rawValue: host.lowercased()) else { return nil }
        self = route
    }
}
