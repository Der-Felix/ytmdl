import SwiftUI
import YTMDLCore
#if os(iOS)
import Charts

// "Deine Statistik" on iPhone and iPad (server 1.11, `history.stats`): the
// window's plays as the page's one large figure, the activity as bars, then
// the ranked top lists. A sibling of "Für dich": same margins, surfaces,
// section titles and press feedback.

/// The statistics page; reachable from "Für dich", Einstellungen and Start.
struct StatsView: View {
    var model: AppModel
    @State private var period = StatsPeriod.week
    /// Answers per window, so switching back shows the last figures at once.
    @State private var answers: [StatsPeriod: ListeningStats] = [:]
    @State private var failures: [StatsPeriod: String] = [:]
    @State private var loading: Set<StatsPeriod> = []
    @State private var savingPlaylist = false
    @State private var savedPlaylist: Playlist?
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var textSize
    private var stats: ListeningStats? { answers[period] }
    /// iPad (and not at accessibility sizes): the top lists side by side.
    private var twoColumns: Bool { sizeClass == .regular && !textSize.isAccessibilitySize }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
                Picker("Zeitraum", selection: $period) {
                    ForEach(StatsPeriod.allCases) { Text($0.title).tag($0) }
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, ListenMetrics.pageMargin)
                .accessibilityIdentifier("stats-period")
                content
            }
            .padding(.top, 4).padding(.bottom, 32)
            .frame(maxWidth: twoColumns ? 940 : CollectionMetrics.readableWidth, alignment: .leading).frame(maxWidth: .infinity)
        }
        .background(Color(uiColor: .systemGroupedBackground))
        .scrollEdgeEffectStyle(.hard, for: .bottom)
        .navigationTitle("Deine Statistik")
        .navigationBarTitleDisplayMode(.large)
        .refreshable { await load(period, force: true) }
        .task(id: period) { await load(period) }
        .toolbar {
            ToolbarItem(placement: .topBarTrailing) {
                Menu { PinToStartButton(model: model, pin: .stats) } label: {
                    Image(systemName: "ellipsis").font(.system(size: 20, weight: .semibold))
                        .frame(minWidth: 44, minHeight: 44).contentShape(Rectangle())
                }.accessibilityLabel("Weitere Aktionen").accessibilityIdentifier("stats-more")
            }
        }
        .sheet(isPresented: $savingPlaylist) {
            AddToPlaylistSheet(model: model, tracks: stats?.topTracks.map(\.track) ?? [], title: "Top-Titel als Playlist speichern",
                               suggestedName: PersonalMix.playlistName("Top-Titel · \(period.title)", date: .now),
                               createLabel: "Als neue Playlist speichern") { savedPlaylist = $0 }
        }
    }

    @ViewBuilder private var content: some View {
        if !model.serverFeatures.statistics {
            ContentUnavailableView("Statistik nicht verfügbar", systemImage: "chart.bar.xaxis",
                                   description: Text("Deine Statistik braucht Server 1.11 oder neuer."))
        } else if let stats {
            if stats.isEmpty {
                ContentUnavailableView {
                    Label("Noch keine Wiedergaben in diesem Zeitraum", systemImage: "chart.bar")
                } description: {
                    Text("Was du auf deinen Geräten hörst, erscheint hier. Wähle einen längeren Zeitraum oder spiele etwas ab.")
                }
                .accessibilityIdentifier("stats-empty")
            } else {
                figures(stats)
            }
        } else if let failure = failures[period] {
            ListenSectionFailure(message: failure) { Task { await load(period, force: true) } }
                .padding(.horizontal, ListenMetrics.pageMargin)
                .accessibilityIdentifier("stats-failure")
        } else {
            StatsSkeleton()
        }
    }

    @ViewBuilder private func figures(_ stats: ListeningStats) -> some View {
        StatsTotalsCard(stats: stats, period: period).padding(.horizontal, ListenMetrics.pageMargin)
        if !stats.activity.points.isEmpty {
            ListenSection {
                ListenSectionHeader(title: "Verlauf", subtitle: stats.activity.isMonthly ? "Wiedergaben pro Monat" : "Wiedergaben pro Tag")
                StatsActivityChart(stats: stats).padding(.horizontal, ListenMetrics.pageMargin)
            }
        }
        if !stats.topTracks.isEmpty { trackActions(stats) }
        let columns = Array(repeating: GridItem(.flexible(), spacing: ListenMetrics.shelfGap, alignment: .top), count: twoColumns ? 2 : 1)
        LazyVGrid(columns: columns, alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
            if !stats.topTracks.isEmpty { topTracks(stats) }
            if !stats.topArtists.isEmpty { topArtists(stats) }
            if !stats.topReleases.isEmpty { topReleases(stats) }
            if !stats.topGenres.isEmpty { topGenres(stats) }
        }
        .padding(.horizontal, ListenMetrics.pageMargin)
    }

    private func trackActions(_ stats: ListeningStats) -> some View {
        let tracks = stats.topTracks.map(\.track)
        let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: ListenMetrics.shelfGap)) : AnyLayout(HStackLayout(spacing: ListenMetrics.shelfGap))
        return VStack(alignment: .leading, spacing: ListenMetrics.shelfGap) {
            layout {
                Button { play(tracks, start: 0) } label: { Label("Top-Titel abspielen", systemImage: "play.fill") }
                    .buttonStyle(MobileActionStyle(prominent: true)).accessibilityIdentifier("stats-play-top")
                Button { savedPlaylist = nil; savingPlaylist = true } label: { Label("Als Playlist speichern", systemImage: "music.note.list") }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier("stats-save-playlist")
                    .accessibilityHint("Legt eine Playlist mit den Top-Titeln dieses Zeitraums an")
            }
            if let savedPlaylist {
                NavigationLink(value: CollectionKind.playlist(savedPlaylist)) {
                    Label("Als Playlist „\(savedPlaylist.name)“ gespeichert", systemImage: "checkmark.circle.fill").font(.subheadline)
                }.accessibilityHint("Öffnet die Playlist").accessibilityIdentifier("stats-saved-playlist")
            }
        }
        .frame(maxWidth: twoColumns ? 560 : .infinity, alignment: .leading)
        .padding(.horizontal, ListenMetrics.pageMargin)
    }

    // MARK: Top lists

    private func topTracks(_ stats: ListeningStats) -> some View {
        let largest = stats.topTracks.map(\.plays).max() ?? 0
        let tracks = stats.topTracks.map(\.track)
        return StatsListCard(title: "Top-Titel") {
            ForEach(Array(stats.topTracks.enumerated()), id: \.offset) { index, entry in
                let track = entry.track
                let row = StatsRankRow(rank: index + 1, title: track.title.isEmpty ? "Unbekannter Titel" : track.title, subtitle: track.artistText,
                                       plays: entry.plays, largest: largest) {
                    ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 6)
                }
                Group {
                    if let release = track.releaseId {
                        NavigationLink(value: CollectionKind.release(Release(id: release, title: track.album, artists: track.artists))) { row }
                            .accessibilityHint("Öffnet das Album")
                    } else {
                        Button { play(tracks, start: index) } label: { row }.accessibilityHint("Spielt den Titel")
                    }
                }
                .buttonStyle(ListenPressStyle())
                .accessibilityIdentifier("stats-track-\(index)")
                .contextMenu {
                    Button("Ab hier abspielen", systemImage: "play.fill") { play(tracks, start: index) }
                    TrackQuickActions(model: model, track: track)
                }
                .accessibilityAction(named: "Abspielen") { play(tracks, start: index) }
            }
        }
    }
    private func topArtists(_ stats: ListeningStats) -> some View {
        let largest = stats.topArtists.map(\.plays).max() ?? 0
        return StatsListCard(title: "Top-Künstler") {
            ForEach(Array(stats.topArtists.enumerated()), id: \.offset) { index, entry in
                let artist = Artist(id: entry.id, name: entry.name)
                NavigationLink(value: CollectionKind.artist(artist)) {
                    StatsRankRow(rank: index + 1, title: entry.name, subtitle: entry.distinctTracks == 1 ? "1 Titel" : "\(entry.distinctTracks) Titel",
                                 plays: entry.plays, largest: largest, round: true) {
                        ArtworkView(model: model, kind: "artists", id: entry.id, cornerRadius: 0, monogram: entry.name)
                    }
                }
                .buttonStyle(ListenPressStyle())
                .accessibilityHint("Öffnet den Künstler")
                .accessibilityIdentifier("stats-artist-\(index)")
                .contextMenu {
                    Button("Künstler-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startArtistRadio(artist) } }
                        .disabled(model.listeningBusy)
                    ExclusionToggleButton(model: model, kind: .artist, target: entry.id, name: entry.name)
                }
                .accessibilityAction(named: "Künstler-Radio starten") { Task { await model.startArtistRadio(artist) } }
            }
        }
    }
    private func topReleases(_ stats: ListeningStats) -> some View {
        let largest = stats.topReleases.map(\.plays).max() ?? 0
        return StatsListCard(title: "Top-Alben") {
            ForEach(Array(stats.topReleases.enumerated()), id: \.offset) { index, entry in
                let release = Release(id: entry.id, title: entry.title, artists: entry.artistName.map { [$0] } ?? [])
                NavigationLink(value: CollectionKind.release(release)) {
                    StatsRankRow(rank: index + 1, title: entry.title, subtitle: entry.artistName, plays: entry.plays, largest: largest) {
                        ArtworkView(model: model, kind: "releases", id: entry.id, cornerRadius: 6)
                    }
                }
                .buttonStyle(ListenPressStyle())
                .accessibilityHint("Öffnet das Album")
                .accessibilityIdentifier("stats-release-\(index)")
                .contextMenu {
                    Button("Album abspielen", systemImage: "play.fill") { Task { await model.playRelease(id: entry.id, title: entry.title) } }
                    Button("Album-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startAlbumRadio(release) } }
                        .disabled(model.listeningBusy)
                }
                .accessibilityAction(named: "Album abspielen") { Task { await model.playRelease(id: entry.id, title: entry.title) } }
            }
        }
    }
    private func topGenres(_ stats: ListeningStats) -> some View {
        let largest = stats.topGenres.map(\.plays).max() ?? 0
        return StatsListCard(title: "Top-Genres", note: "Eine Wiedergabe zählt für jedes Genre ihres Künstlers.") {
            ForEach(Array(stats.topGenres.enumerated()), id: \.offset) { index, entry in
                NavigationLink(value: LibraryRoute.genre(entry.name)) {
                    StatsRankRow(rank: index + 1, title: entry.name, subtitle: nil, plays: entry.plays, largest: largest) {
                        RoundedRectangle(cornerRadius: 6, style: .continuous).fill(GenreTile.color(for: entry.name).gradient)
                            .overlay { Image(systemName: "guitars").font(.subheadline.weight(.semibold)).foregroundStyle(.white) }
                    }
                }
                .buttonStyle(ListenPressStyle())
                .accessibilityHint("Öffnet das Genre")
                .accessibilityIdentifier("stats-genre-\(index)")
                .contextMenu {
                    Button("Genre-Radio starten", systemImage: "dot.radiowaves.left.and.right") { Task { await model.startGenreRadio(entry.name) } }
                        .disabled(model.listeningBusy)
                    ExclusionToggleButton(model: model, kind: .genre, target: entry.name, name: entry.name)
                }
                .accessibilityAction(named: "Genre-Radio starten") { Task { await model.startGenreRadio(entry.name) } }
            }
        }
    }

    // MARK: Actions

    private func play(_ tracks: [Track], start: Int) {
        guard let client = model.client, !tracks.isEmpty else { return }
        model.player.play(tracks, start: start, client: client,
                          context: QueueContext(kind: "history", id: "stats-" + period.rawValue, title: "Top-Titel · \(period.title)"))
    }
    private func load(_ period: StatsPeriod, force: Bool = false) async {
        guard model.serverFeatures.statistics, force || answers[period] == nil, !loading.contains(period) else { return }
        loading.insert(period); defer { loading.remove(period) }
        do {
            let answer = try await model.listeningStats(period)
            answers[period] = answer; failures[period] = nil
        } catch is CancellationError {
        } catch {
            guard !Task.isCancelled else { return }
            failures[period] = ListenFeed.phase(for: error).message ?? "Die Statistik konnte nicht geladen werden."
        }
    }
}

private extension ListenFeed.Phase {
    var message: String? { if case .failed(let text) = self { text } else { nil } }
}

// MARK: - Building blocks

/// The window's figures: plays as the page's one large number, then titles,
/// artists and the estimated listening time; a note where the window is not
/// covered completely.
private struct StatsTotalsCard: View {
    var stats: ListeningStats
    var period: StatsPeriod
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text(period.caption).font(.caption.weight(.semibold)).foregroundStyle(.tint).textCase(.uppercase)
                Text(stats.totals.plays.formatted(.number.locale(GermanDates.locale)))
                    .font(.system(size: 52, weight: .bold, design: .rounded)).monospacedDigit()
                    .minimumScaleFactor(0.6).lineLimit(1)
                    .accessibilityIdentifier("stats-plays")
                Text(stats.totals.plays == 1 ? "Wiedergabe" : "Wiedergaben").font(.headline).foregroundStyle(.secondary)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("\(StatsFormat.plays(stats.totals.plays)), \(period.caption)")
            let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(alignment: .top, spacing: 12))
            layout {
                figure(value: "\(stats.totals.distinctTracks)", label: "Titel")
                figure(value: "\(stats.totals.distinctArtists)", label: "Künstler")
                figure(value: StatsFormat.listeningTime(stats.totals.listeningMs), label: "Hörzeit, geschätzt")
            }
            if let note = StatsFormat.note(stats) {
                Label(note, systemImage: "info.circle").font(.footnote).foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("stats-note")
            }
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
    }
    private func figure(value: String, label: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(value).font(.title3.bold()).monospacedDigit().lineLimit(1).minimumScaleFactor(0.7)
            Text(label).font(.footnote).foregroundStyle(.secondary).lineLimit(2)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }
}

/// Plays per day or month as bars; VoiceOver reads each bar and offers the
/// chart's audio graph.
private struct StatsActivityChart: View {
    var stats: ListeningStats
    private struct Bar: Identifiable { let id: String; let date: Date; let plays: Int }
    private var bars: [Bar] {
        let zone = stats.zone
        return stats.activity.points.compactMap { point in point.date(in: zone).map { Bar(id: point.start, date: $0, plays: point.plays) } }
    }
    var body: some View {
        let monthly = stats.activity.isMonthly
        let bars = self.bars
        let total = bars.reduce(0) { $0 + $1.plays }
        let axisFormat: Date.FormatStyle = monthly
            ? Date.FormatStyle(timeZone: stats.zone).month(.abbreviated).locale(GermanDates.locale)
            : Date.FormatStyle(timeZone: stats.zone).day().month(.defaultDigits).locale(GermanDates.locale)
        Chart(bars) { bar in
            BarMark(x: .value("Zeit", bar.date, unit: monthly ? .month : .day), y: .value("Wiedergaben", bar.plays))
                .foregroundStyle(Color.accentColor.gradient)
                .cornerRadius(3)
                .accessibilityLabel(label(bar.date, monthly: monthly))
                .accessibilityValue(StatsFormat.plays(bar.plays))
        }
        .chartXAxis {
            AxisMarks(values: .automatic(desiredCount: monthly ? 6 : 7)) { _ in
                AxisGridLine()
                AxisValueLabel(format: axisFormat, centered: true)
            }
        }
        .chartYAxis { AxisMarks(position: .leading) }
        .environment(\.locale, GermanDates.locale)
        .frame(height: 180)
        .padding(16)
        .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Verlauf, \(StatsFormat.plays(total)) in \(bars.count) \(monthly ? "Monaten" : "Tagen")")
        .accessibilityIdentifier("stats-chart")
    }
    private func label(_ date: Date, monthly: Bool) -> String {
        let formatter = DateFormatter()
        formatter.locale = GermanDates.locale; formatter.timeZone = stats.zone
        formatter.setLocalizedDateFormatFromTemplate(monthly ? "MMMMyyyy" : "EEEEdMMMM")
        return formatter.string(from: date)
    }
}

/// One ranked list on a surface, titled like every section of the pages.
private struct StatsListCard<Rows: View>: View {
    var title: String
    var note: String? = nil
    @ViewBuilder var rows: Rows
    var body: some View {
        VStack(alignment: .leading, spacing: ListenMetrics.headerGap) {
            Text(title).font(.title2.bold()).accessibilityAddTraits(.isHeader)
            VStack(alignment: .leading, spacing: 4) { rows }
                .padding(.vertical, 8).padding(.horizontal, 12)
                .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            if let note { Text(note).font(.footnote).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true) }
        }
    }
}

/// Rank, cover, name, plays and a bar proportional to the list's first entry.
private struct StatsRankRow<Artwork: View>: View {
    var rank: Int
    var title: String
    var subtitle: String?
    var plays: Int
    var largest: Int
    var round = false
    @ViewBuilder var artwork: Artwork
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        HStack(spacing: 12) {
            Text("\(rank)").font(.subheadline.weight(.semibold)).monospacedDigit().foregroundStyle(.secondary)
                .frame(minWidth: CollectionMetrics.numberColumn)
            artwork.frame(width: ListenMetrics.rowArtwork, height: ListenMetrics.rowArtwork)
                .clipShape(round ? AnyShape(Circle()) : AnyShape(RoundedRectangle(cornerRadius: 6, style: .continuous)))
            VStack(alignment: .leading, spacing: 4) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(title).font(.body.weight(.semibold)).foregroundStyle(.primary)
                        .lineLimit(textSize.isAccessibilitySize ? 3 : 1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    Text("\(plays)×").font(.subheadline).monospacedDigit().foregroundStyle(.secondary)
                }
                if let subtitle, !subtitle.isEmpty {
                    Text(subtitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(1)
                }
                Capsule().fill(Color.accentColor.opacity(0.15)).frame(height: 4)
                    .overlay(alignment: .leading) {
                        GeometryReader { proxy in
                            Capsule().fill(Color.accentColor).frame(width: proxy.size.width * StatsFormat.share(plays, of: largest))
                        }
                    }
                    .accessibilityHidden(true)
            }
        }
        .padding(.vertical, 6)
        .frame(minHeight: 44)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel("Platz \(rank): \(title)" + (subtitle.map { $0.isEmpty ? "" : ", \($0)" } ?? "") + ", \(StatsFormat.plays(plays))")
    }
}

/// The page's final shapes while the first answer is outstanding.
private struct StatsSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: ListenMetrics.sectionSpacing) {
            RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous).fill(ListenMetrics.surface).frame(height: 190)
            RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous).fill(ListenMetrics.surface).frame(height: 212)
            VStack(alignment: .leading, spacing: 8) {
                ForEach(0..<4, id: \.self) { _ in TrackRowSkeleton(numbered: false) }
            }
        }
        .padding(.horizontal, ListenMetrics.pageMargin)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Statistik wird geladen")
        .accessibilityIdentifier("stats-loading")
    }
}

/// Start's "Statistik": this week's plays and the top artist; opens the page.
struct StatsHomeCard: View {
    var model: AppModel
    @Environment(\.dynamicTypeSize) private var textSize
    var body: some View {
        NavigationLink(value: ListeningRoute.stats) {
            let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(alignment: .leading, spacing: 12)) : AnyLayout(HStackLayout(spacing: 16))
            layout {
                VStack(alignment: .leading, spacing: 2) {
                    Text(model.weekStats.map { $0.totals.plays.formatted(.number.locale(GermanDates.locale)) } ?? "–")
                        .font(.system(size: 34, weight: .bold, design: .rounded)).monospacedDigit().foregroundStyle(.primary)
                    Text("Wiedergaben diese Woche").font(.subheadline).foregroundStyle(.secondary)
                }.frame(maxWidth: .infinity, alignment: .leading)
                if let top = model.weekStats?.topArtists.first {
                    HStack(spacing: 10) {
                        ArtworkView(model: model, kind: "artists", id: top.id, cornerRadius: 0, monogram: top.name)
                            .frame(width: 44, height: 44).clipShape(Circle())
                        VStack(alignment: .leading, spacing: 2) {
                            Text("Top-Künstler").font(.caption.weight(.semibold)).foregroundStyle(.tint).textCase(.uppercase)
                            Text(top.name).font(.subheadline.weight(.semibold)).foregroundStyle(.primary).lineLimit(1)
                        }
                    }.frame(maxWidth: textSize.isAccessibilitySize ? .infinity : 200, alignment: .leading)
                }
                Image(systemName: "chevron.right").font(.subheadline.weight(.semibold)).foregroundStyle(.tertiary).accessibilityHidden(true)
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(ListenMetrics.surface, in: RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: ListenMetrics.cardRadius, style: .continuous))
        }
        .buttonStyle(ListenPressStyle())
        .accessibilityLabel(accessibilityText)
        .accessibilityHint("Öffnet deine Statistik")
        .accessibilityIdentifier("home-stats")
        .contextMenu { PinToStartButton(model: model, pin: .stats) }
    }
    private var accessibilityText: String {
        guard let stats = model.weekStats else { return "Statistik" }
        var text = "Statistik, \(StatsFormat.plays(stats.totals.plays)) diese Woche"
        if let top = stats.topArtists.first { text += ", Top-Künstler \(top.name)" }
        return text
    }
}
#endif
