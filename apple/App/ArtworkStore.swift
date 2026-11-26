import Foundation
import CoreGraphics
import Observation
import YTMDLCore

/// Covers fetched once per session and decoded off the main thread at the
/// size they are shown. The API session has no URL cache, so without this a
/// cover was fetched and decoded again every time its row scrolled back in.
@MainActor final class ArtworkImageCache {
    private final class Entry {
        let image: CGImage?
        init(_ image: CGImage?) { self.image = image }
    }
    /// What a fetch learned: a picture, none at all (404), or nothing (a failure, asked again later).
    private enum Outcome: Sendable { case image(CGImage), missing, failed }
    private let images: NSCache<NSString, Entry> = {
        let cache = NSCache<NSString, Entry>()
        cache.countLimit = 400
        cache.totalCostLimit = 96 * 1024 * 1024
        return cache
    }()
    private var running: [String: Task<Outcome, Never>] = [:]
    private var generation = UUID()

    /// The decoding sizes, in pixels; a cover asks for the smallest that covers its frame.
    static let buckets = [160, 400, 800, 1400]
    static func bucket(forPixels pixels: CGFloat) -> Int {
        buckets.first { CGFloat($0) >= pixels } ?? buckets[buckets.count - 1]
    }
    private static func key(_ kind: String, _ id: String, _ size: Int) -> String { kind + "/" + id + "@" + String(size) }

    /// A decoded cover from this session, `.some(nil)` when the server has none.
    func cached(kind: String, id: String, size: Int) -> CGImage?? {
        guard let entry = images.object(forKey: Self.key(kind, id, size) as NSString) else { return nil }
        return .some(entry.image)
    }

    /// The cover, fetched once even when several views ask at the same time.
    /// A caller that goes away does not cancel the fetch: the next one finds it ready.
    func image(kind: String, id: String, size: Int, client: APIClient) async -> CGImage? {
        let key = Self.key(kind, id, size)
        if let entry = images.object(forKey: key as NSString) { return entry.image }
        let task: Task<Outcome, Never>
        if let existing = running[key] { task = existing }
        else {
            guard let request = try? client.artworkRequest(kind: kind, id: id) else { return nil }
            let session = client.session
            task = Task.detached(priority: .utility) {
                guard let answer = try? await session.data(for: request),
                      let status = (answer.1 as? HTTPURLResponse)?.statusCode else { return .failed }
                if status == 404 { return .missing }
                guard status == 200, let image = ArtworkPalette.thumbnail(answer.0, maxPixelSize: size) else { return .failed }
                return .image(image)
            }
            running[key] = task
            let generation = generation
            Task { [weak self] in
                let outcome = await task.value
                guard let self, self.generation == generation else { return }
                self.running[key] = nil
                switch outcome {
                case .image(let image): self.images.setObject(Entry(image), forKey: key as NSString, cost: image.width * image.height * 4)
                case .missing: self.images.setObject(Entry(nil), forKey: key as NSString, cost: 1)
                case .failed: break
                }
            }
        }
        if case .image(let image) = await task.value { return image }
        return nil
    }

    /// Another server or account: nothing of the old one is shown or stored.
    func reset() {
        generation = UUID()
        for task in running.values { task.cancel() }
        running = [:]
        images.removeAllObjects()
    }
}

/// The covers of genre tiles, owned by the model rather than by a view: a
/// load started from a view's `.task` was cancelled whenever SwiftUI rebuilt
/// the view. Tiles ask as they appear; every genre is fetched at most once
/// per session, four at a time, and the result is remembered per account
/// and server so tiles show their covers right away on the next launch.
@MainActor @Observable final class GenreArtworkStore {
    /// Genre key → release IDs (up to four). Empty: the genre has no albums.
    private(set) var index = GenreCoverIndex()
    @ObservationIgnored private var queue = GenreCoverQueue()
    @ObservationIgnored private var names: [String: String] = [:]
    @ObservationIgnored private var storageKey: String?
    @ObservationIgnored private var generation = UUID()
    @ObservationIgnored private var saveTask: Task<Void, Never>?
    /// Fetches a few releases of a genre; set by the model.
    @ObservationIgnored var fetch: (@MainActor (String) async throws -> [Release])?
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) { self.defaults = defaults }

    /// The release IDs for a genre's tile; nil while unknown.
    func covers(for genre: String) -> [String]? { index[GenreCovers.key(genre)] }

    /// Binds the store to an account on a server (nil: signed out). The
    /// remembered covers of that account come back, nothing is fetched yet.
    func configure(storageKey: String?) {
        guard storageKey != self.storageKey else { return }
        flush()
        self.storageKey = storageKey
        generation = UUID(); queue.reset(); names = [:]
        index = storageKey.map { GenreCoverIndex.decode(defaults.data(forKey: $0)) } ?? GenreCoverIndex()
    }
    /// The library was loaded anew (pull to refresh, a new session): genres
    /// may have new albums, so each is asked once more when it shows.
    func refresh() { queue.forgetLoaded() }

    /// A tile appeared: its covers load unless known from this session.
    func request(_ genre: String) {
        guard fetch != nil, GenreCovers.isBrowsable(genre) else { return }
        let key = GenreCovers.key(genre)
        if queue.enqueue(key) { names[key] = genre }
        pump()
    }
    /// A tile left the screen before its covers were asked for.
    func withdraw(_ genre: String) { queue.withdraw(GenreCovers.key(genre)) }

    /// The genre page loaded its first albums: their covers serve the tile too.
    func remember(_ genre: String, releases: [Release]) {
        guard GenreCovers.isBrowsable(genre), !releases.isEmpty else { return }
        let key = GenreCovers.key(genre)
        queue.markLoaded(key)
        store(key, GenreCovers.pick(releases))
    }

    private func pump() {
        while let key = queue.next() {
            let genre = names[key] ?? key, generation = generation
            Task { [weak self] in
                guard let self, let fetch = self.fetch else { return }
                var succeeded = false
                do {
                    let releases = try await fetch(genre)
                    if self.generation == generation { store(key, GenreCovers.pick(releases)); succeeded = true }
                } catch { }
                guard self.generation == generation else { return }
                queue.finish(key, succeeded: succeeded)
                pump()
            }
        }
    }
    private func store(_ key: String, _ ids: [String]) {
        var updated = index
        // Unchanged covers leave the observed index alone: no tile redraws.
        guard updated.set(key, ids) else { return }
        index = updated
        scheduleSave()
    }
    private func scheduleSave() {
        guard saveTask == nil, storageKey != nil else { return }
        saveTask = Task { [weak self] in
            try? await Task.sleep(for: .seconds(1))
            self?.flush()
        }
    }
    private func flush() {
        saveTask?.cancel(); saveTask = nil
        guard let storageKey, let data = index.encoded() else { return }
        defaults.set(data, forKey: storageKey)
    }
}
