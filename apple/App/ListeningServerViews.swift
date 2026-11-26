import SwiftUI
import YTMDLCore
#if os(iOS)

// The parts of server 1.11's listening features on iPhone and iPad that are
// not the statistics page: "Weniger davon" and the exclusions of artists and
// genres in menus, the "Empfehlungen" page, lyrics hits in Suche and the way
// from "Für dich" to both pages. Everything is hidden where the server lacks
// the feature (`ServerFeatures`).

/// Navigation values of the listening pages.
enum ListeningRoute: Hashable {
    /// "Deine Statistik".
    case stats
    /// "Empfehlungen": the listener's exclusions.
    case preferences
}

// MARK: - Menu items

/// "Weniger davon" and "Künstler nicht mehr vorschlagen" for a title's menus;
/// when already excluded, the way back ("Wieder vorschlagen").
struct TrackSteeringItems: View {
    var model: AppModel
    var track: Track
    /// From the player: a mix or radio also moves on to the next title.
    var fromPlayer = false
    var body: some View {
        if model.serverFeatures.preferences && !model.offlineMode {
            Section {
                if model.preferences.contains(.track, track.id) {
                    Button("Wieder vorschlagen", systemImage: "hand.thumbsup") {
                        Task { await model.allow(.track, target: track.id, name: track.title) }
                    }
                } else {
                    Button("Weniger davon", systemImage: "hand.thumbsdown") {
                        Task { await model.lessOfTrack(track, fromPlayer: fromPlayer) }
                    }
                    .accessibilityHint("Mixe und Radios schlagen diesen Titel nicht mehr vor")
                }
                if let artist = model.artistReference(for: track) {
                    ExclusionToggleButton(model: model, kind: .artist, target: artist.id, name: artist.name)
                }
            }
        }
    }
}

/// "Künstler/Genre nicht mehr vorschlagen", or "… wieder vorschlagen" once excluded.
struct ExclusionToggleButton: View {
    var model: AppModel
    var kind: PreferenceKind
    var target: String
    var name: String
    var body: some View {
        if model.serverFeatures.preferences && !model.offlineMode && !target.isEmpty && kind != .track {
            let excluded = model.preferences.contains(kind, target)
            let noun = kind == .artist ? "Künstler" : "Genre"
            Button(excluded ? "\(noun) wieder vorschlagen" : "\(noun) nicht mehr vorschlagen",
                   systemImage: excluded ? "hand.thumbsup" : (kind == .artist ? "person.crop.circle.badge.xmark" : "nosign")) {
                Task {
                    if excluded { await model.allow(kind, target: target, name: name) }
                    else if kind == .artist { await model.excludeArtist(id: target, name: name) }
                    else { await model.excludeGenre(target) }
                }
            }
            .accessibilityHint(excluded ? "Mixe und Radios dürfen \(name) wieder vorschlagen" : "Mixe und Radios lassen \(name) aus; abspielen kannst du weiterhin alles")
        }
    }
}

// MARK: - "Für dich"

/// The end of "Für dich": the ways to "Deine Statistik" and "Empfehlungen".
struct ListeningLinksSection: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    @Environment(\.horizontalSizeClass) private var sizeClass
    var body: some View {
        let features = model.serverFeatures
        if features.statistics || features.preferences {
            ListenSection {
                ListenSectionHeader(title: "Dein Hören", subtitle: "Was du hörst und was dir vorgeschlagen wird")
                let columns = textSize.isAccessibilitySize ? 1 : 2
                LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: ListenMetrics.tileGap, alignment: .top), count: columns),
                          spacing: ListenMetrics.tileGap) {
                    if features.statistics {
                        NavigationLink(value: ListeningRoute.stats) {
                            ListeningLinkTile(title: "Deine Statistik", detail: statsDetail, symbol: "chart.bar.xaxis")
                        }
                        .buttonStyle(ListenPressStyle())
                        .accessibilityHint("Wiedergaben, Top-Titel und Hörzeit")
                        .accessibilityIdentifier("foryou-stats")
                    }
                    if features.preferences {
                        NavigationLink(value: ListeningRoute.preferences) {
                            ListeningLinkTile(title: "Empfehlungen", detail: preferencesDetail, symbol: "hand.thumbsdown")
                        }
                        .buttonStyle(ListenPressStyle())
                        .accessibilityHint("Was Mixe und Radios auslassen")
                        .accessibilityIdentifier("foryou-preferences")
                    }
                }
                .padding(.horizontal, ListenMetrics.pageMargin)
            }
            .task(id: features.statistics) { if features.statistics && model.weekStats == nil { await model.loadWeekStats() } }
        }
    }
    private var statsDetail: String {
        model.weekStats.map { "\(StatsFormat.plays($0.totals.plays)) diese Woche" } ?? "Wiedergaben, Top-Titel, Hörzeit"
    }
    private var preferencesDetail: String {
        let count = model.preferences.entries.count
        return count == 0 ? "Mixe und Radios steuern" : count == 1 ? "1 Ausschluss" : "\(count) Ausschlüsse"
    }
}

/// A tile of "Dein Hören": symbol, title and one line of detail.
private struct ListeningLinkTile: View {
    var title: String
    var detail: String
    var symbol: String
    var body: some View {
        // Symbol above the text: half the phone's width stays for the words,
        // so "Empfehlungen" is never hyphenated.
        VStack(alignment: .leading, spacing: 10) {
            Image(systemName: symbol).font(.body.weight(.semibold)).foregroundStyle(.tint)
                .frame(width: 36, height: 36).background(.tint.opacity(0.14), in: Circle())
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(2)
                Text(detail).font(.caption).monospacedDigit().foregroundStyle(.secondary).lineLimit(2)
            }
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(14)
        .frame(maxWidth: .infinity, minHeight: 68, alignment: .topLeading)
        .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
    }
}

// MARK: - "Empfehlungen"

/// The listener's exclusions by kind; each can be allowed again.
struct PreferencesView: View {
    var model: AppModel
    private var preferences: ListeningPreferences { model.preferences }
    var body: some View {
        List {
            if !model.serverFeatures.preferences {
                ContentUnavailableView("Nicht verfügbar", systemImage: "hand.thumbsdown",
                                       description: Text("Empfehlungen steuern braucht Server 1.11 oder neuer."))
                    .listRowBackground(Color.clear)
            } else if preferences.isEmpty {
                switch model.preferencesPhase {
                case .failed(let message):
                    ListenSectionFailure(message: message) { Task { await model.loadPreferences() } }
                        .listRowInsets(EdgeInsets()).listRowBackground(Color.clear)
                case .loading, .idle:
                    ForEach(0..<3, id: \.self) { _ in TrackRowSkeleton() }
                        .accessibilityElement(children: .ignore).accessibilityLabel("Wird geladen")
                default:
                    ContentUnavailableView {
                        Label("Keine Ausschlüsse", systemImage: "hand.thumbsup")
                    } description: {
                        Text("Wähle bei einem Titel „Weniger davon“ oder auf einer Künstler- oder Genre-Seite „… nicht mehr vorschlagen“. Mixe und Radios lassen das dann aus – abspielen kannst du alles weiterhin.")
                    }
                    .listRowBackground(Color.clear)
                    .accessibilityIdentifier("preferences-empty")
                }
            } else {
                ForEach(PreferenceKind.allCases) { kind in
                    let entries = preferences.entries(of: kind)
                    if !entries.isEmpty {
                        Section {
                            ForEach(entries) { entry in row(entry) }
                        } header: {
                            Text(kind.title)
                        }
                    }
                }
            }
            if model.serverFeatures.preferences {
                Section {
                } footer: {
                    Text("Ausgeschlossenes wählen Mixe und Radios nicht mehr aus. Alben, Playlists, die Suche und deine Favoriten spielen es weiterhin, und es zählt in deiner Statistik.")
                }
            }
        }
        .listStyle(.insetGrouped)
        .modifier(ReadableContentWidth(maxWidth: 720))
        .navigationTitle("Empfehlungen")
        .navigationBarTitleDisplayMode(.inline)
        .refreshable { await model.loadPreferences() }
        .task { await model.loadPreferences() }
    }
    private func row(_ entry: ListeningPreference) -> some View {
        HStack(spacing: 12) {
            artwork(entry).frame(width: 40, height: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(entry.name).font(.body).lineLimit(2)
                if let detail = detail(entry) { Text(detail).font(.footnote).foregroundStyle(.secondary).lineLimit(1) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            if entry.isPending {
                ProgressView().frame(width: 44, height: 44).accessibilityLabel("Wird gespeichert")
            } else {
                Button("Entfernen") { Task { await model.allow(entry) } }
                    .buttonStyle(.borderless).font(.subheadline.weight(.semibold))
                    .frame(minHeight: 44)
                    .accessibilityLabel("\(entry.name) wieder vorschlagen")
                    .accessibilityIdentifier("preferences-allow-\(entry.kind)-\(entry.target)")
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("preferences-row-\(entry.kind)-\(entry.target)")
        .swipeActions {
            Button("Entfernen", systemImage: "hand.thumbsup") { Task { await model.allow(entry) } }
                .tint(.accentColor)
                .disabled(entry.isPending)
        }
        .accessibilityAction(named: "Wieder vorschlagen") { Task { await model.allow(entry) } }
    }
    @ViewBuilder private func artwork(_ entry: ListeningPreference) -> some View {
        switch entry.preferenceKind {
        case .artist:
            ArtworkView(model: model, kind: "artists", id: entry.target, cornerRadius: 0, monogram: entry.name).clipShape(Circle())
        case .track:
            ArtworkView(model: model, kind: "tracks", id: entry.target, cornerRadius: 6)
        default:
            GenreThumbnail(model: model, genre: entry.target)
        }
    }
    private func detail(_ entry: ListeningPreference) -> String? {
        switch entry.preferenceKind {
        case .track: entry.artistName.map { "Titel · \($0)" } ?? "Titel"
        case .artist: "Künstler"
        case .genre: "Genre"
        case nil: nil
        }
    }
}

// MARK: - Lyrics hits in Suche

/// A title found in its lyrics: the title, its artist and the snippet with
/// the matched words emphasised (bold and in the primary colour, not colour
/// alone). Tapping plays it; the menus are those of every title row.
struct LyricsResultRow: View {
    var model: AppModel
    var hit: LyricsHit
    var query: String
    var play: () -> Void
    @Environment(\.dynamicTypeSize) private var textSize
    private var title: String { hit.track.title.isEmpty ? "Unbekannter Titel" : hit.track.title }
    var body: some View {
        HStack(spacing: 12) {
            Button(action: play) {
                HStack(alignment: .top, spacing: 12) {
                    ArtworkView(model: model, kind: "tracks", id: hit.track.id, cornerRadius: 6)
                        .frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
                    VStack(alignment: .leading, spacing: 2) {
                        Text(title).font(.body.weight(.semibold)).foregroundStyle(.primary).lineLimit(textSize.isAccessibilitySize ? 3 : 1)
                        if !hit.track.artistText.isEmpty {
                            Text(hit.track.artistText).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Text(snippet).font(.subheadline).foregroundStyle(.secondary).lineLimit(2)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(.top, 2)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("\(title) abspielen, \(hit.track.artistText), Liedtext: \(hit.snippet)")
            .accessibilityHint("Spielt den Titel")
            .accessibilityIdentifier("search-lyrics-" + hit.track.id)
            Menu { TrackQuickActions(model: model, track: hit.track) } label: {
                Image(systemName: "ellipsis").foregroundStyle(.secondary).frame(width: 44, height: 44).contentShape(Rectangle())
            }
            .accessibilityLabel("Aktionen für \(title)")
        }
        .contextMenu { TrackQuickActions(model: model, track: hit.track) }
    }
    private var snippet: AttributedString {
        var text = AttributedString(hit.snippet)
        for range in LyricsHighlight.ranges(in: hit.snippet, match: hit.match, query: query) {
            guard let marked = Range(range, in: text) else { continue }
            text[marked].font = .subheadline.weight(.bold)
            text[marked].foregroundColor = .primary
        }
        return text
    }
}
#endif
