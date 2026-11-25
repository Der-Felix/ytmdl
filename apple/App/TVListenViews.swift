#if os(tvOS)
import SwiftUI
import YTMDLCore

/// The Apple TV start screen: the listening home of the iPhone app (the web's
/// "Hören") as shelves for the remote. Covers are focusable cards that lift;
/// titles sit beneath them. Uses the shared `ListenFeed`, so each server part
/// loads and fails on its own and nothing starts playing by itself.
struct TVHomeView: View {
    var model: AppModel
    /// Switches to the Player tab.
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
                Button { model.resumeLastSession(); openPlayer() } label: {
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
                if !genreMixes.isEmpty {
                    shelf("Deine Mixe", subtitle: "Täglich neu aus deiner Mediathek") { ForEach(genreMixes) { mix in mixCard(mix) } }
                }
                if !listeningMixes.isEmpty {
                    shelf("Aus deinem Hören", subtitle: "Aus dem, was du zuletzt gehört hast") { ForEach(listeningMixes) { mix in mixCard(mix) } }
                }
            }
        case .loaded, .unsupported:
            // Without server mixes: the library mix the iPhone app also offers.
            VStack(alignment: .leading, spacing: 20) {
                heading("Für deinen Moment", subtitle: "Ein Mix aus deiner Bibliothek")
                Button { Task { await model.playMix("Favoriten"); openPlayer() } } label: {
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
                        openPlayer()
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

    private static let cardSize: CGFloat = 280

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

    /// Select opens the mix; a long press offers to play it right away.
    private func mixCard(_ mix: PersonalMix) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            NavigationLink(value: mix) {
                MixArtwork(model: model, mix: mix, size: Self.cardSize)
                    .overlay {
                        if feed.playingMixID == mix.id { ProgressView().padding(16).background(.regularMaterial, in: Circle()) }
                    }
            }.buttonStyle(.card)
                .accessibilityLabel("\(mix.title), \(mix.label), \(mix.trackCount) Titel")
                .accessibilityHint("Öffnet den Mix. Gedrückt halten zum direkten Abspielen.")
                .accessibilityIdentifier("tv-mix-" + mix.id)
                .contextMenu {
                    Button("Abspielen", systemImage: "play.fill") { Task { await model.playPersonalMix(mix); openPlayer() } }
                    Button("Zufällig abspielen", systemImage: "shuffle") { Task { await model.playPersonalMix(mix, shuffle: true); openPlayer() } }
                }
            Text(mix.label.uppercased()).font(.caption2.weight(.semibold)).foregroundStyle(.tint).lineLimit(1)
                .accessibilityHidden(true)
            Text(mix.title).font(.callout.weight(.semibold)).lineLimit(2).accessibilityHidden(true)
            Text("\(mix.trackCount) Titel").font(.caption).monospacedDigit().foregroundStyle(.secondary).accessibilityHidden(true)
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
            }.buttonStyle(.card)
                .accessibilityLabel("\(track.title) abspielen, \(track.artistText)")
            Text(track.title).font(.callout.weight(.semibold)).lineLimit(1).accessibilityHidden(true)
            Text(track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1).accessibilityHidden(true)
        }.frame(width: Self.cardSize * 0.8, alignment: .leading)
    }
}

/// The Apple TV queue: the same sections as on iPhone and the web ("Als
/// Nächstes", "Weiter aus: …", "Ähnliche Titel") with the autoplay switch.
/// Each row is one focusable button that plays that entry.
struct TVQueueView: View {
    var model: AppModel
    var body: some View {
        let queue = model.player.queue
        LazyVStack(alignment: .leading, spacing: 6) {
            Toggle(isOn: Binding(get: { model.player.autoplay }, set: { model.player.setAutoplay($0) })) {
                VStack(alignment: .leading, spacing: 4) {
                    Text("Automatisch weiterspielen").font(.callout.weight(.medium))
                    Text("Spielt am Ende der Warteschlange ähnliche Titel aus deiner Bibliothek.").font(.caption).foregroundStyle(.secondary)
                }
            }.accessibilityIdentifier("tv-queue-autoplay")
            ForEach(queue.rows()) { row in
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
                Button { model.player.select(row.position) } label: {
                    HStack(spacing: 20) {
                        ArtworkView(model: model, kind: "tracks", id: row.track.id).frame(width: 72, height: 72)
                        VStack(alignment: .leading, spacing: 4) {
                            Text(row.track.title).font(.callout.weight(.semibold)).lineLimit(1)
                            Text(row.track.artistText).font(.caption).foregroundStyle(.secondary).lineLimit(1)
                        }
                        Spacer(minLength: 12)
                        if row.section == .current { Image(systemName: "speaker.wave.2.fill").foregroundStyle(.pink).accessibilityHidden(true) }
                    }.padding(12)
                }.buttonStyle(.plain)
                    .accessibilityLabel(row.section == .current ? "Läuft: \(row.track.title), \(row.track.artistText)" : "\(row.track.title) abspielen, \(row.track.artistText)")
                    .accessibilityIdentifier("tv-queue-track-\(row.position)")
            }
        }
    }
}
#endif
