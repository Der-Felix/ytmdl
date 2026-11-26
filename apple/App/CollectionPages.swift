import SwiftUI
import YTMDLCore

/// Navigation values of the iPhone and iPad library that are not a collection.
enum LibraryRoute: Hashable {
    /// Every title of the library.
    case tracks
    /// The genres, each opening its albums.
    case genres
    case genre(String)
}

/// What the album page asks the server besides the tracks: the album with its
/// type and the linked artist.
struct ReleaseDetailResponse: Decodable, Sendable {
    let release: Release
    let artist: Artist?
}

#if os(iOS)
/// Sizes of the album, artist, playlist and mix pages. Margins and gaps come
/// from `ListenMetrics`, so these pages line up with Start and Suche.
enum CollectionMetrics {
    /// The cover on iPhone; it is the one large element of the page.
    static let compactCover: CGFloat = 240
    /// The cover beside the text on iPad.
    static let regularCover: CGFloat = 248
    /// The round picture of an artist.
    static let artistCover: CGFloat = 196
    /// Pages stop growing here on iPad and center their content.
    static let readableWidth: CGFloat = 860
    /// Track numbers and the playing indicator.
    static let numberColumn: CGFloat = 28
    /// The row cover and insets of every list (`ListenMetrics`).
    static let rowArtwork: CGFloat = ListenMetrics.rowArtwork
    static let shelfCard: CGFloat = 148
    static let rowInsets = ListenMetrics.rowInsets
    /// Insets of a section title written as a row: room above, none below
    /// the gap to its first row.
    static let titleInsets = EdgeInsets(top: 20, leading: ListenMetrics.pageMargin, bottom: 4, trailing: ListenMetrics.pageMargin)
}

extension View {
    /// A section title (`CollectionSectionTitle`) as an ordinary list row
    /// instead of a section header: it scrolls away like everything else,
    /// never pins above the tab bar's hard edge and keeps the full colour.
    func sectionTitleRow() -> some View {
        listRowInsets(CollectionMetrics.titleInsets).listRowSeparator(.hidden).listRowBackground(Color.clear)
    }
}

/// Centers a scrolling page in `CollectionMetrics.readableWidth` on wide
/// screens; on iPhone the content keeps its normal margins.
struct ReadableContentWidth: ViewModifier {
    var maxWidth: CGFloat = CollectionMetrics.readableWidth
    @State private var width: CGFloat = 0
    func body(content: Content) -> some View {
        content
            .contentMargins(.horizontal, max(0, (width - maxWidth) / 2), for: .scrollContent)
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
    }
}

/// A secondary action of a collection: an icon over a short label in the
/// row under "Abspielen" and "Zufall", or an entry of one menu when the row
/// does not fit (large text).
struct CollectionAction: Identifiable {
    struct Item: Identifiable {
        let title: String
        let symbol: String
        var enabled = true
        let action: () -> Void
        var id: String { title }
    }
    enum Kind { case button(() -> Void), menu([Item]) }
    /// The accessibility identifier.
    let id: String
    /// The short label under the icon.
    let title: String
    /// The full name VoiceOver reads and the menu shows.
    let label: String
    let symbol: String
    var hint: String? = nil
    var enabled = true
    var selected = false
    let kind: Kind
}

/// The header of every collection page (album, artist, playlist, favorites,
/// mix): the cover as the page's one standout, title, an optional link
/// (the artist), one summary line, "Abspielen" and "Zufall", then the
/// secondary actions. iPhone stacks it centered; iPad puts the cover beside
/// the text.
struct CollectionHeader<Artwork: View>: View {
    var eyebrow: String? = nil
    var title: String
    var link: String? = nil
    var linkHint = "Öffnet den Künstler"
    var openLink: (() -> Void)? = nil
    var detail: String
    var description: String? = nil
    var canPlay: Bool
    /// Identifier prefix of the buttons: "collection" or "mix".
    var idPrefix = "collection"
    var round = false
    var actions: [CollectionAction] = []
    var play: () -> Void
    var shuffle: () -> Void
    /// Whether the large title is on screen; the page shows the title in the
    /// navigation bar once it scrolled away.
    var titleVisibility: (Bool) -> Void = { _ in }
    /// The cover at the size the layout gives it.
    @ViewBuilder var artwork: (CGFloat) -> Artwork
    @Environment(\.horizontalSizeClass) private var sizeClass
    @Environment(\.dynamicTypeSize) private var textSize
    private var wide: Bool { sizeClass == .regular && !textSize.isAccessibilitySize }

    var body: some View {
        Group {
            if wide {
                HStack(alignment: .center, spacing: 28) {
                    cover(CollectionMetrics.regularCover)
                    VStack(alignment: .leading, spacing: 16) {
                        texts(alignment: .leading)
                        buttons
                    }.frame(maxWidth: 460, alignment: .leading)
                    Spacer(minLength: 0)
                }
            } else {
                VStack(spacing: 16) {
                    cover(round ? CollectionMetrics.artistCover : CollectionMetrics.compactCover)
                    texts(alignment: textSize.isAccessibilitySize ? .leading : .center)
                    buttons
                }.frame(maxWidth: .infinity)
            }
        }
        .padding(.top, 8)
        // These pages are plain lists on the system background: the
        // secondary buttons and tiles take a fill that shows in light mode.
        .environment(\.listenPlainBackground, true)
    }

    private func cover(_ size: CGFloat) -> some View {
        artwork(size).frame(width: size, height: size)
            .clipShape(round ? AnyShape(Circle()) : AnyShape(RoundedRectangle(cornerRadius: 16, style: .continuous)))
            .shadow(color: .black.opacity(0.18), radius: 16, y: 8)
            .accessibilityHidden(true)
    }

    private func texts(alignment: HorizontalAlignment) -> some View {
        let textAlignment: TextAlignment = alignment == .center ? .center : .leading
        return VStack(alignment: alignment, spacing: 4) {
            if let eyebrow {
                Text(eyebrow).font(.caption.weight(.semibold)).foregroundStyle(.tint).textCase(.uppercase)
            }
            Text(title).font(.title2.bold()).multilineTextAlignment(textAlignment)
                .accessibilityAddTraits(.isHeader)
                .onScrollVisibilityChange(threshold: 0.2) { titleVisibility($0) }
            if let link, !link.isEmpty {
                if let openLink {
                    Button(action: openLink) { Text(link).font(.title3).multilineTextAlignment(textAlignment) }
                        .buttonStyle(.plain).foregroundStyle(.tint)
                        .frame(minHeight: 44)
                        .accessibilityLabel("Künstler: \(link)").accessibilityHint(linkHint)
                        .accessibilityIdentifier(idPrefix + "-artist")
                } else {
                    Text(link).font(.title3).foregroundStyle(.secondary).multilineTextAlignment(textAlignment)
                }
            }
            Text(detail).font(.subheadline).monospacedDigit().foregroundStyle(.secondary).multilineTextAlignment(textAlignment)
                .padding(.top, link == nil ? 4 : 0)
            if let description, !description.isEmpty {
                Text(description).font(.subheadline).foregroundStyle(.secondary).lineLimit(3)
                    .multilineTextAlignment(textAlignment).padding(.top, 4)
            }
        }.frame(maxWidth: .infinity, alignment: Alignment(horizontal: alignment, vertical: .center))
    }

    private var buttons: some View {
        VStack(spacing: ListenMetrics.shelfGap) {
            let layout = textSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: ListenMetrics.shelfGap)) : AnyLayout(HStackLayout(spacing: ListenMetrics.shelfGap))
            layout {
                Button(action: play) { Label("Abspielen", systemImage: "play.fill") }
                    .buttonStyle(MobileActionStyle(prominent: true)).accessibilityIdentifier(idPrefix + "-play")
                Button(action: shuffle) { Label("Zufall", systemImage: "shuffle") }
                    .buttonStyle(MobileActionStyle()).accessibilityIdentifier(idPrefix + "-shuffle")
            }.disabled(!canPlay)
            if !actions.isEmpty { CollectionActionRow(actions: actions, idPrefix: idPrefix) }
        }
    }
}

/// The secondary actions in equal tiles; one "Weitere Aktionen" menu when
/// they do not fit, so no label ever wraps inside its tile.
struct CollectionActionRow: View {
    var actions: [CollectionAction]
    var idPrefix = "collection"
    var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: 10) { ForEach(actions) { tile($0) } }
            Menu {
                ForEach(actions) { entry($0) }
            } label: { Label("Weitere Aktionen", systemImage: "ellipsis.circle") }
                .buttonStyle(MobileActionStyle()).accessibilityIdentifier(idPrefix + "-more")
        }
    }
    @ViewBuilder private func tile(_ action: CollectionAction) -> some View {
        let label = Label(action.title, systemImage: action.symbol)
        Group {
            switch action.kind {
            case .button(let run):
                Button(action: run) { label }.buttonStyle(MixTileStyle())
            case .menu(let items):
                Menu {
                    ForEach(items) { item in Button(item.title, systemImage: item.symbol, action: item.action).disabled(!item.enabled) }
                } label: { label }
                    .menuStyle(.button).buttonStyle(MixTileStyle())
            }
        }
        .disabled(!action.enabled)
        .accessibilityLabel(action.label)
        .accessibilityHint(action.hint ?? "")
        .accessibilityAddTraits(action.selected ? .isSelected : [])
        .accessibilityIdentifier(action.id)
    }
    @ViewBuilder private func entry(_ action: CollectionAction) -> some View {
        switch action.kind {
        case .button(let run): Button(action.label, systemImage: action.symbol, action: run).disabled(!action.enabled)
        case .menu(let items):
            Section(action.label) {
                ForEach(items) { item in Button(item.title, systemImage: item.symbol, action: item.action).disabled(!item.enabled || !action.enabled) }
            }
        }
    }
}

/// "Einreihen": the queue actions for a whole collection.
@MainActor func queueCollectionAction(_ model: AppModel, tracks: [Track], id: String) -> CollectionAction {
    let full = model.player.queue.tracks.count >= PlaybackQueue.limit
    return CollectionAction(id: id, title: "Einreihen", label: "Warteschlange", symbol: "text.badge.plus",
        hint: "Als Nächstes spielen oder an die Warteschlange anhängen", enabled: !tracks.isEmpty && !full,
        kind: .menu([
            .init(title: "Als Nächstes spielen", symbol: "text.insert") { model.enqueue(tracks, next: true) },
            .init(title: "Zur Warteschlange", symbol: "text.badge.plus") { model.enqueue(tracks, next: false) },
        ]))
}

/// The section title of every list page (collection pages, search): the
/// style of Start's section titles, with an optional trailing control.
struct CollectionSectionTitle<Trailing: View>: View {
    var title: String
    @ViewBuilder var trailing: Trailing
    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            // `Color.primary`, not `.primary`: inside a list header the
            // hierarchical style resolves against the header's dimmed colour.
            Text(title).font(.title2.bold()).foregroundStyle(Color.primary).accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            trailing
        }.textCase(nil)
    }
}
extension CollectionSectionTitle where Trailing == EmptyView {
    init(title: String) { self.init(title: title) { EmptyView() } }
}

/// An album in a shelf of a collection page. A button, not a link: inside a
/// list row a link would turn the whole row into one.
struct ShelfReleaseCard: View {
    var model: AppModel
    var release: Release
    var caption: String
    var width: CGFloat = CollectionMetrics.shelfCard
    var open: () -> Void
    var body: some View {
        Button(action: open) {
            VStack(alignment: .leading, spacing: 6) {
                ArtworkView(model: model, kind: "releases", id: release.id, cornerRadius: ListenMetrics.cardRadius).frame(width: width, height: width)
                VStack(alignment: .leading, spacing: 2) {
                    Text(release.title).font(.subheadline.weight(.semibold)).foregroundStyle(.primary)
                    Text(caption).font(.caption).monospacedDigit().foregroundStyle(.secondary)
                }.lineLimit(1)
            }.frame(width: width, alignment: .leading).contentShape(Rectangle())
        }.buttonStyle(ListenPressStyle())
            .accessibilityHint("Öffnet das Album")
            .accessibilityIdentifier("release-card-" + release.id)
    }
}

/// A row while the tracks of a page load: the final height, no text.
struct TrackRowSkeleton: View {
    var numbered = false
    var body: some View {
        HStack(spacing: 12) {
            RoundedRectangle(cornerRadius: numbered ? 4 : 6, style: .continuous).fill(ListenMetrics.surface)
                .frame(width: numbered ? 20 : CollectionMetrics.rowArtwork, height: numbered ? 14 : CollectionMetrics.rowArtwork)
                .frame(width: numbered ? CollectionMetrics.numberColumn : nil)
            VStack(alignment: .leading, spacing: 6) {
                Capsule().fill(ListenMetrics.surface).frame(width: 160, height: 12)
                Capsule().fill(ListenMetrics.surface).frame(width: 96, height: 10)
            }
            Spacer()
        }.frame(minHeight: numbered ? 44 : CollectionMetrics.rowArtwork).accessibilityHidden(true)
    }
}
#endif

/// The picture of an artist with a generated monogram while there is none.
struct ArtistAvatar: View {
    var model: AppModel
    var artist: Artist
    var body: some View {
        ArtworkView(model: model, kind: "artists", id: artist.id, cornerRadius: 0, monogram: artist.name)
            .clipShape(Circle())
    }
}

/// Initials on a calm color that belongs to the name.
struct MonogramView: View {
    var name: String
    var body: some View {
        GeometryReader { proxy in
            let hue = ArtistMonogram.hue(name)
            ZStack {
                LinearGradient(colors: [Color(hue: hue, saturation: 0.45, brightness: 0.72), Color(hue: hue, saturation: 0.55, brightness: 0.48)],
                               startPoint: .topLeading, endPoint: .bottomTrailing)
                Text(ArtistMonogram.initials(name))
                    .font(.system(size: max(12, proxy.size.width * 0.36), weight: .semibold, design: .rounded))
                    .foregroundStyle(.white.opacity(0.92))
                    .minimumScaleFactor(0.5).lineLimit(1)
            }
        }.accessibilityHidden(true)
    }
}
