import SwiftUI
import WidgetKit

private struct MusicEntry: TimelineEntry { let date: Date }
private struct MusicProvider: TimelineProvider {
    func placeholder(in context: Context) -> MusicEntry { MusicEntry(date: .now) }
    func getSnapshot(in context: Context, completion: @escaping (MusicEntry) -> Void) { completion(MusicEntry(date: .now)) }
    func getTimeline(in context: Context, completion: @escaping (Timeline<MusicEntry>) -> Void) {
        completion(Timeline(entries: [MusicEntry(date: .now)], policy: .never))
    }
}

// Navigation widgets intentionally have no session, server, network client or
// shared audio state. A tap opens the app; it never claims to start playback.
private struct MusicWidgetView: View {
    @Environment(\.widgetFamily) private var family
    private let accent = Color(red: 0.92, green: 0.12, blue: 0.39)
    var body: some View {
        Group {
            switch family {
            case .accessoryCircular:
                Image(systemName: "play.circle.fill").font(.title).widgetAccentable()
                    .accessibilityLabel("YTMDL Player öffnen")
            case .accessoryInline:
                Label("YTMDL · Player", systemImage: "music.note")
            case .accessoryRectangular:
                VStack(alignment: .leading, spacing: 4) {
                    Label("YTMDL", systemImage: "music.note").font(.headline)
                    Text("Player öffnen").font(.caption)
                }
            default:
                VStack(alignment: .leading, spacing: 12) {
                    HStack(spacing: 8) {
                        Image("BrandMark").resizable().scaledToFit().frame(width: 28, height: 28)
                        Text("YTMDL").font(.headline)
                        Spacer(minLength: 0)
                    }
                    Text("Deine Musik.").font(.title2.bold()).minimumScaleFactor(0.8).lineLimit(1)
                    if family == .systemMedium {
                        HStack(spacing: 8) {
                            shortcut("Player", symbol: "play.fill", route: "player")
                            shortcut("Favoriten", symbol: "heart.fill", route: "favorites")
                            shortcut("Playlists", symbol: "music.note.list", route: "playlists")
                        }
                    } else {
                        Label("Player öffnen", systemImage: "play.fill").font(.subheadline.weight(.semibold))
                            .foregroundStyle(accent)
                    }
                }.foregroundStyle(.white)
            }
        }.widgetURL(URL(string: "ytmdl-player://player"))
            .containerBackground(for: .widget) {
                LinearGradient(colors: [Color(red: 0.16, green: 0.07, blue: 0.12), .black], startPoint: .topLeading, endPoint: .bottomTrailing)
            }
    }
    private func shortcut(_ title: String, symbol: String, route: String) -> some View {
        Link(destination: URL(string: "ytmdl-player://" + route)!) {
            VStack(spacing: 6) {
                Image(systemName: symbol).font(.title3).foregroundStyle(accent).widgetAccentable()
                Text(title).font(.caption.weight(.semibold)).lineLimit(1).minimumScaleFactor(0.8)
            }.frame(maxWidth: .infinity, minHeight: 52)
                .background(.primary.opacity(0.07), in: RoundedRectangle(cornerRadius: 12))
        }.accessibilityLabel(title + " in YTMDL öffnen")
    }
}

private struct MusicWidget: Widget {
    let kind = "YTMDLMusicShortcuts"
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: kind, provider: MusicProvider()) { _ in MusicWidgetView() }
            .configurationDisplayName("Deine Musik")
            .description("Player, Favoriten und Playlists direkt in YTMDL öffnen.")
            .supportedFamilies([.systemSmall, .systemMedium, .accessoryCircular, .accessoryRectangular, .accessoryInline])
    }
}

@main struct MusicWidgets: WidgetBundle { var body: some Widget { MusicWidget() } }
