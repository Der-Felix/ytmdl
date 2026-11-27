#if os(tvOS)
import SwiftUI
import YTMDLCore

/// The Apple TV start screen: the listening home of the iPhone app (the web's
/// "Hören") as shelves for the remote. Covers are focusable cards that lift;
/// titles sit beneath them. Uses the shared `ListenFeed`, so each server part
/// loads and fails on its own and nothing starts playing by itself.
struct TVHomeView: View {
    var model: AppModel
    /// Opens "Jetzt läuft" (starting playback opens it by itself).
    var openPlayer: () -> Void
    private var feed: ListenFeed { model.feed }
    private var genreMixes: [PersonalMix] { feed.mixes.filter(\.isGenreMix) }
    private var listeningMixes: [PersonalMix] { feed.mixes.filter { !$0.isGenreMix } }
    private var releases: [Release] { Array((feed.releasesPhase == .loaded ? feed.newReleases : model.releases).prefix(12)) }
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 56) {
                if feed.unreachable { offlineHint }
                continueListening
                mixShelves
                historyShelf
                if !releases.isEmpty {
                    shelf("Neu in der Mediathek", subtitle: "Deine zuletzt hinzugefügten Alben") {
                        ForEach(releases) { release in releaseCard(release) }
                    }
                }
            }.padding(.horizontal, 80).padding(.vertical, 40)
        }
        .task(id: model.user?.id) { await model.loadListenFeed() }
    }

    // MARK: Sections

    @ViewBuilder private var continueListening: some View {
        if let track = model.player.current {
            VStack(alignment: .leading, spacing: 20) {
                heading("Weiterhören", subtitle: "Deine aktuelle Wiedergabe")
                HStack(spacing: 32) {
                    Button(action: openPlayer) {
                        HStack(spacing: 28) {
                            ArtworkView(model: model, kind: "tracks", id: track.id).frame(width: 150, height: 150)
                            VStack(alignment: .leading, spacing: 8) {
                                Text(track.title).font(.headline).lineLimit(1)
                                Text([track.artistText, track.album].filter { !$0.isEmpty }.joined(separator: " · "))
                                    .font(.callout).foregroundStyle(.secondary).lineLimit(1)
                                if model.player.queue.tracks.count > 1 {
                                    Text("Titel \(model.player.queue.index + 1) von \(model.player.queue.tracks.count) in der Warteschlange")
                                        .font(.caption).monospacedDigit().foregroundStyle(.secondary)
                                }
                                if model.player.duration > 0 {
                                    ProgressView(value: min(model.player.position, model.player.duration), total: model.player.duration)
                                        .accessibilityLabel("Position im Titel")
                                }
                            }.frame(width: 640, alignment: .leading)
                        }.padding(24)
                    }.buttonStyle(.card)
                        .accessibilityLabel("Weiterhören: \(track.title), \(track.artistText)")
                        .accessibilityHint("Öffnet den Player")
                        .accessibilityIdentifier("tv-continue-listening")
                    Button { model.player.toggle() } label: {
                        Label(model.player.isPlaybackRequested ? "Pausieren" : "Fortsetzen",
                              systemImage: model.player.isPlaybackRequested ? "pause.fill" : "play.fill")
                    }.buttonStyle(.bordered)
                        .accessibilityLabel(model.player.isPlaybackRequested ? "Wiedergabe pausieren" : "Wiedergabe fortsetzen")
                }
            }
        } else if model.listeningHistory.snapshot != nil {
            VStack(alignment: .leading, spacing: 20) {
                heading("Weiterhören", subtitle: "Da weitermachen, wo du aufgehört hast")
                Button { model.resumeLastSession() } label: {
                    Label("Letzte Wiedergabe fortsetzen", systemImage: "play.circle.fill")
                }.buttonStyle(.bordered)
            }
        }
    }

    @ViewBuilder private var mixShelves: some View {
        switch feed.mixesPhase {
        case .loaded where !feed.mixes.isEmpty:
            VStack(alignment: .leading, spacing: 20) {
                if let message = feed.mixError { Text(message).font(.callout).foregroundStyle(.red) }
                // "Alle Mixe" closes the first mix row, like "Alle Mixe" on the iPhone.
                if !genreMixes.isEmpty {
                    shelf("Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek") {
                        ForEach(genreMixes) { mix in TVMixCard(model: model, mix: mix, openPlayer: openPlayer) }
                        allMixesCard
                    }
                }
                if !listeningMixes.isEmpty {
                    shelf("Aus deinem Hören", subtitle: "Aus dem, was du zuletzt gehört hast") {
                        ForEach(listeningMixes) { mix in TVMixCard(model: model, mix: mix, openPlayer: openPlayer) }
                        if genreMixes.isEmpty { allMixesCard }
                    }
                }
            }
        case .loaded, .unsupported:
            // Without server mixes: the library mix the iPhone app also offers.
            VStack(alignment: .leading, spacing: 20) {
                heading("Für deinen Moment", subtitle: "Ein Mix aus deiner Bibliothek")
                Button { Task { await model.playMix("Favoriten") } } label: {
                    Label("Favoriten-Mix", systemImage: "heart.fill")
                }.buttonStyle(.bordered).disabled(model.listeningBusy)
            }
        case .failed(let message) where !feed.unreachable:
            VStack(alignment: .leading, spacing: 20) {
                heading("Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek")
                failure(message)
            }
        case .loading, .idle:
            ProgressView("Mixe werden zusammengestellt …").frame(maxWidth: .infinity, alignment: .leading)
        default: EmptyView()
        }
    }

    /// The account's history from the server (all devices); without it the
    /// local list of this device, as on the iPhone.
    @ViewBuilder private var historyShelf: some View {
        let server = feed.historyPhase == .loaded ? feed.history.map(\.track) : []
        let tracks = server.isEmpty ? model.listeningHistory.tracks : server
        if !tracks.isEmpty {
            shelf("Zuletzt gehört", subtitle: server.isEmpty ? "Auf diesem Gerät" : "Auf allen deinen Geräten") {
                ForEach(Array(tracks.prefix(12).enumerated()), id: \.offset) { index, track in
                    trackCard(track) {
                        guard let client = model.client else { return }
                        model.player.play(tracks, start: index, client: client, context: QueueContext(kind: "history", title: "Zuletzt gehört"))
                    }
                }
            }
        } else if case .failed(let message) = feed.historyPhase, !feed.unreachable {
            VStack(alignment: .leading, spacing: 20) {
                heading("Zuletzt gehört", subtitle: "Auf allen deinen Geräten")
                failure(message)
            }
        }
    }

    private var offlineHint: some View {
        VStack(alignment: .leading, spacing: 16) {
            Label("Keine Verbindung zum Server", systemImage: "wifi.slash").font(.headline)
            Text("Mixe und dein Hörverlauf erscheinen, sobald der Server wieder erreichbar ist.")
                .font(.callout).foregroundStyle(.secondary)
            Button { Task { await model.loadListenFeed() } } label: { Label("Erneut versuchen", systemImage: "arrow.clockwise") }
                .buttonStyle(.bordered)
        }
    }

    // MARK: Building blocks

    private static let cardSize = TVMixCard.size

    private func heading(_ title: String, subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(.title3.bold()).accessibilityAddTraits(.isHeader)
            Text(subtitle).font(.callout).foregroundStyle(.secondary)
        }
    }

    /// A titled horizontal row. Clipping is off so a focused card can lift.
    private func shelf<Content: View>(_ title: String, subtitle: String, @ViewBuilder content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 20) {
            heading(title, subtitle: subtitle)
            ScrollView(.horizontal) {
                LazyHStack(alignment: .top, spacing: 48) { content() }.padding(.vertical, 24)
            }.scrollClipDisabled().scrollIndicators(.hidden)
        }
    }

    private func failure(_ message: String) -> some View {
        HStack(spacing: 24) {
            Image(systemName: "exclamationmark.triangle").foregroundStyle(.secondary).accessibilityHidden(true)
            Text(message).font(.callout).foregroundStyle(.secondary)
            Button("Erneut laden") { Task { await model.loadListenFeed() } }
        }
    }

    /// Opens "Für dich" with every mix of the day.
    private var allMixesCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink(value: ForYouRoute()) {
                Image(systemName: "square.grid.2x2").font(.system(size: 72, weight: .medium)).foregroundStyle(.tint)
                    .frame(width: Self.cardSize, height: Self.cardSize).background(.tint.opacity(0.12))
            }.buttonStyle(.card)
                .accessibilityLabel("Alle Mixe")
                .accessibilityHint("Öffnet „Für dich“ mit allen Mixen des Tages")
                .accessibilityIdentifier("tv-all-mixes")
            Text("Alle Mixe").font(.callout.weight(.semibold)).lineLimit(1).accessibilityHidden(true)
            Text(feed.mixes.count == 1 ? "1 Mix in „Für dich“" : "\(feed.mixes.count) Mixe in „Für dich“").font(.caption).monospacedDigit().foregroundStyle(.secondary).lineLimit(1)
                .accessibilityHidden(true)
        }.frame(width: Self.cardSize, alignment: .leading)
    }

    private func releaseCard(_ release: Release) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink(value: CollectionKind.release(release)) {
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: 0)
                    .frame(width: Self.cardSize, height: Self.cardSize)
            }.buttonStyle(.card)
                .accessibilityLabel("\(release.title), \(release.artists.joined(separator: ", "))")
            Text(release.title).font(.callout.weight(.semibold)).lineLimit(2)
            Text(release.artists.joined(separator: " · ")).font(.caption).foregroundStyle(.secondary).lineLimit(1)
        }.frame(width: Self.cardSize, alignment: .leading)
    }

    private func trackCard(_ track: Track, play: @escaping () -> Void) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Button(action: play) {
                ArtworkView(model: model, kind: "tracks", id: track.id, cornerRadius: 0)
                    .frame(width: Self.cardSize * 0.8, height: Self.cardSize * 0.8)
                    .overlay {
                        if model.player.current?.id == track.id {
                            ZStack {
                                Color.black.opacity(0.45)
                                PlayingIndicator(playing: model.player.isPlaying).scaleEffect(2.6)
                            }
                        }
                    }
            }.buttonStyle(.card)
                .accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
            Text(track.title).font(.callout.weight(.semibold)).lineLimit(1).accessibilityHidden(true)
            Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1).accessibilityHidden(true)
        }.frame(width: Self.cardSize * 0.8, alignment: .leading)
    }
}

/// One mix as a focusable cover card with its label, title and size. Select
/// opens the mix; a long press offers to play it right away. Used by the
/// Start shelves and the "Für dich" grid, so both behave the same.
struct TVMixCard: View {
    var model: AppModel
    var mix: PersonalMix
    var openPlayer: () -> Void
    /// Keeps accessibility identifiers unique while Start stays beneath "Für dich".
    var identifierPrefix = "tv-mix-"
    static let size: CGFloat = 280
    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink(value: mix) {
                MixArtwork(model: model, mix: mix, size: Self.size)
                    .overlay {
                        if model.feed.playingMixID == mix.id { ProgressView().padding(16).background(.regularMaterial, in: Circle()) }
                    }
            }.buttonStyle(.card)
                .accessibilityLabel("\(mix.title), \(mix.label), \(mix.trackCount) Titel")
                .accessibilityHint("Öffnet den Mix. Gedrückt halten zum direkten Abspielen.")
                .accessibilityIdentifier(identifierPrefix + mix.id)
                .contextMenu {
                    Button("Abspielen", systemImage: "play.fill") { Task { await model.playPersonalMix(mix) } }
                    Button("Zufällig abspielen", systemImage: "shuffle") { Task { await model.playPersonalMix(mix, shuffle: true) } }
                }
            Text(mix.label.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(.tint).lineLimit(1)
                .accessibilityHidden(true)
            Text(mix.title).font(.callout.weight(.semibold)).lineLimit(2).accessibilityHidden(true)
            Text("\(mix.trackCount) Titel").font(.caption).monospacedDigit().foregroundStyle(.secondary).accessibilityHidden(true)
        }.frame(width: Self.size, alignment: .leading)
    }
}

/// "Für dich" on Apple TV: every mix of the day as a grid of cover cards,
/// genre mixes ("Dein Mix 1…n") first, then the mixes from listening.
struct TVForYouView: View {
    var model: AppModel
    /// Opens "Jetzt läuft" (starting playback opens it by itself).
    var openPlayer: () -> Void
    private var feed: ListenFeed { model.feed }
    private var mixes: [PersonalMix] { PersonalMix.ordered(feed.mixes) }
    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 48) {
                Text("Täglich neu gemischt aus deiner Mediathek und deinem Hörverlauf · \(GermanDates.day(.now))")
                    .font(.callout).foregroundStyle(.secondary)
                if let message = feed.mixError { Text(message).font(.callout).foregroundStyle(.red) }
                content
            }.padding(.horizontal, 80).padding(.vertical, 40)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .navigationTitle("Für dich")
        .task { if feed.mixesPhase == .idle { await model.loadListenFeed() } }
    }

    @ViewBuilder private var content: some View {
        switch feed.mixesPhase {
        case .idle, .loading:
            ProgressView("Mixe werden zusammengestellt …").frame(maxWidth: .infinity).padding(.top, 80)
        case .failed(let message):
            state("Mixe konnten nicht geladen werden", symbol: "exclamationmark.triangle", message: message, retry: true)
        case .unsupported:
            state("Mixe nicht verfügbar", symbol: "sparkles",
                  message: "Dein Server bietet noch keine persönlichen Mixe an. Sie gibt es ab YTMDL 1.10.", retry: false)
        case .loaded:
            if mixes.isEmpty {
                state("Noch keine Mixe", symbol: "sparkles",
                      message: "Mixe entstehen aus der Musik in deiner Mediathek und aus dem, was du hörst. Jeder Mix braucht mindestens zehn passende Titel – mit mehr Musik und Hörverlauf erscheinen sie hier und wechseln täglich.",
                      retry: true)
            } else {
                grid("Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek", mixes.filter(\.isGenreMix))
                grid("Aus deinem Hören", subtitle: "Aus dem, was du zuletzt gehört hast", mixes.filter { !$0.isGenreMix })
            }
        }
    }

    private func state(_ title: String, symbol: String, message: String, retry: Bool) -> some View {
        ContentUnavailableView {
            Label(title, systemImage: symbol)
        } description: {
            Text(message)
        } actions: {
            if retry {
                Button { Task { await model.loadListenFeed() } } label: { Label("Erneut laden", systemImage: "arrow.clockwise") }
                    .accessibilityIdentifier("tv-foryou-retry")
            }
        }.frame(maxWidth: .infinity)
    }

    @ViewBuilder private func grid(_ title: String, subtitle: String, _ mixes: [PersonalMix]) -> some View {
        if !mixes.isEmpty {
            VStack(alignment: .leading, spacing: 20) {
                VStack(alignment: .leading, spacing: 4) {
                    Text(title).font(.title3.bold()).accessibilityAddTraits(.isHeader)
                    Text(subtitle).font(.callout).foregroundStyle(.secondary)
                }
                LazyVGrid(columns: [GridItem(.adaptive(minimum: TVMixCard.size, maximum: TVMixCard.size), spacing: 48, alignment: .top)],
                          alignment: .leading, spacing: 56) {
                    ForEach(mixes) { mix in
                        TVMixCard(model: model, mix: mix, openPlayer: openPlayer, identifierPrefix: "tv-foryou-mix-")
                    }
                }.padding(.vertical, 12)
            }
        }
    }
}

/// The Apple TV queue: the same sections as on iPhone and the web ("Als
/// Nächstes", "Weiter aus: …", "Ähnliche Titel") with the autoplay switch.
/// Select plays an entry; a long press on an upcoming entry offers "Als
/// Nächstes abspielen", "Nach oben", "Nach unten" and "Aus Warteschlange
/// entfernen". The playing entry and those already played stay where they
/// are. Focus stays with the edited entry or its neighbour.
struct TVQueueView: View {
    var model: AppModel
    @State private var savingQueue = false
    @State private var playlistSelection: [Track] = []
    /// The iPhone queue's filter: title, artists and album. Rows keep their
    /// queue positions, so the long-press menu edits the entry that is shown.
    @State private var query = ""
    @FocusState private var focus: Focus?
    private enum Focus: Hashable { case row(Int), clear, undo, filter }
    private var player: PlayerModel { model.player }
    var body: some View {
        let queue = player.queue
        let rows = queue.rows(filter: query)
        LazyVStack(alignment: .leading, spacing: 6) {
            Toggle(isOn: Binding(get: { player.autoplay }, set: { player.setAutoplay($0) })) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Automatisch weiterspielen").font(.callout.weight(.medium))
                    Text("Spielt am Ende der Warteschlange ähnliche Titel aus deiner Bibliothek.").font(.caption).foregroundStyle(.secondary)
                }
            }.accessibilityIdentifier("tv-queue-autoplay")
            actions(queue)
            filterField(queue, visible: rows.count)
            ForEach(rows) { row in
                if row.startsSection {
                    HStack(spacing: 10) {
                        if row.section == .autoplay { Image(systemName: "sparkles").accessibilityHidden(true) }
                        Text(row.section.heading(queue.context)).lineLimit(2)
                        Spacer(minLength: 12)
                        if row.section != .current { Text("\(queue.count(of: row.section))").monospacedDigit() }
                    }.font(.callout.weight(.semibold)).foregroundStyle(.secondary)
                        .padding(.top, 24).padding(.horizontal, 8)
                        .accessibilityElement(children: .combine).accessibilityAddTraits(.isHeader)
                }
                Button { player.select(row.position) } label: {
                    HStack(spacing: 20) {
                        ArtworkView(model: model, kind: "tracks", id: row.track.id).frame(width: 72, height: 72)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.track.title).font(.callout.weight(.semibold)).lineLimit(1)
                            Text(row.track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 12)
                        if row.section == .current { PlayingIndicator(playing: player.isPlaying, color: TVTheme.accent).scaleEffect(1.6).frame(width: 28, height: 28) }
                    }.padding(12)
                }.buttonStyle(.plain)
                    .focused($focus, equals: .row(row.position))
                    .contextMenu { rowMenu(row, queue, visible: rows.map(\.position)) }
                    .accessibilityLabel(row.section == .current ? "Läuft: \(row.track.title), \(row.track.artistText)" : "\(row.track.title) abspielen, \(row.track.artistText)")
                    .accessibilityHint(queue.isUpcoming(row.position) ? "Gedrückt halten zum Verschieben oder Entfernen" : "")
                    .accessibilityIdentifier("tv-queue-track-\(row.position)")
            }
        }
        .sheet(isPresented: $savingQueue) { AddToPlaylistSheet(model: model, tracks: playlistSelection) }
    }

    /// "Warteschlange leeren" removes the upcoming entries; for a few seconds
    /// "Rückgängig" takes its place and brings them back.
    private func actions(_ queue: PlaybackQueue) -> some View {
        VStack(alignment: .leading, spacing: 16) {
            if let offer = player.undoOffer {
                HStack(spacing: 24) {
                    Button { undo() } label: { Label("Rückgängig", systemImage: "arrow.uturn.backward") }
                        .focused($focus, equals: .undo)
                        .accessibilityLabel("Rückgängig, \(offer.removedCount) Titel wiederherstellen")
                        .accessibilityIdentifier("tv-queue-undo")
                    Text("\(offer.removedCount) Titel entfernt").font(.callout).monospacedDigit().foregroundStyle(.secondary)
                        .accessibilityHidden(true)
                }
            } else {
                Button { clear() } label: { Label("Warteschlange leeren", systemImage: "trash") }
                    .disabled(queue.remaining == 0)
                    .focused($focus, equals: .clear)
                    .accessibilityHint("Entfernt alle kommenden Titel. Der laufende Titel spielt weiter.")
                    .accessibilityIdentifier("tv-queue-clear")
            }
            Button { playlistSelection = queue.tracks; savingQueue = true } label: { Label("Als Playlist speichern", systemImage: "music.note.list") }
                .disabled(queue.tracks.isEmpty || model.offlineMode)
                .accessibilityHint("Legt eine Playlist mit den Titeln der Warteschlange an")
                .accessibilityIdentifier("tv-queue-save")
        }.padding(.vertical, 16)
    }

    /// The filter field. Selecting it opens the tvOS keyboard; "Filter leeren"
    /// shows the whole queue again without typing.
    @ViewBuilder private func filterField(_ queue: PlaybackQueue, visible: Int) -> some View {
        if queue.tracks.count > 1 || !query.isEmpty {
            HStack(spacing: 24) {
                TextField("Warteschlange filtern", text: $query)
                    .focused($focus, equals: .filter)
                    .accessibilityIdentifier("tv-queue-filter")
                if !query.isEmpty {
                    Button("Filter leeren", systemImage: "xmark.circle") { query = ""; setFocus(.filter) }
                        .accessibilityIdentifier("tv-queue-filter-clear")
                }
            }
            if !query.isEmpty {
                Text(visible == 0 ? "Kein Titel passt zum Filter." : "\(visible) von \(queue.tracks.count) Titeln")
                    .font(.caption).monospacedDigit().foregroundStyle(.secondary).padding(.horizontal, 8)
                    .accessibilityIdentifier("tv-queue-filter-count")
            }
        }
    }

    /// Upcoming entries can be moved and removed; every entry can go to a
    /// playlist or the favorites while online. Only what applies is offered.
    /// `visible` are the rows the filter shows, so focus stays on one of them.
    @ViewBuilder private func rowMenu(_ row: QueueRow, _ queue: PlaybackQueue, visible: [Int]) -> some View {
        let position = row.position
        if queue.isUpcoming(position) {
            if queue.canPlayNext(position) {
                Button("Als Nächstes abspielen", systemImage: "text.insert") { player.playNextInQueue(position); refocus(queue.index + 1) }
            }
            if queue.canMoveUp(position) {
                Button("Nach oben", systemImage: "arrow.up") { player.moveInQueue(from: position, to: position - 1); refocus(position - 1) }
            }
            if queue.canMoveDown(position) {
                Button("Nach unten", systemImage: "arrow.down") { player.moveInQueue(from: position, to: position + 1); refocus(position + 1) }
            }
            Button("Aus Warteschlange entfernen", systemImage: "minus.circle", role: .destructive) {
                player.removeFromQueue(position)
                refocus(PlaybackQueue.focusAfterRemoving(position, visible: visible))
            }
        }
        if !model.offlineMode {
            Button("Zur Playlist hinzufügen", systemImage: "music.note.list") { playlistSelection = [row.track]; savingQueue = true }
            Button(model.favoriteIDs.contains(row.track.id) ? "Aus Favoriten entfernen" : "Zu Favoriten hinzufügen", systemImage: "heart") {
                Task { await model.toggleFavorite(row.track) }
            }.disabled(model.pendingFavorites.contains(row.track.id))
        }
    }

    private func clear() {
        player.clearUpcoming()
        setFocus(.undo)
    }
    private func undo() {
        player.undoClear()
        setFocus(.clear)
    }
    private func refocus(_ position: Int?) {
        guard let position else { return }
        setFocus(.row(position))
    }
    /// After the menu closed and the rows changed, so the system does not
    /// put focus back on the top of the panel.
    private func setFocus(_ target: Focus) {
        Task { @MainActor in
            try? await Task.sleep(for: .milliseconds(100))
            focus = target
        }
    }
}
#endif
