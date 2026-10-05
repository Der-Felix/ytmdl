import Foundation
import CryptoKit
import Observation
import YTMDLCore

struct OfflineProfile: Codable, Identifiable, Sendable {
    let id: String
    let origin: String
    let user: User
}
enum DownloadState: String, Codable, Sendable { case queued, downloading, paused, ready, failed }
struct OfflineTrack: Codable, Identifiable, Sendable {
    let id: String
    let scope: String
    var track: Track
    var state: DownloadState
    var bytes: Int64 = 0
    var expected: Int64 = 0
    var fileName: String?
    var transferID: Int?
    var failure: String?
    var automatic = false
    var date = Date()
}
struct OfflineCollection: Codable, Identifiable, Sendable {
    let id: String
    let scope: String
    let kind: String
    let sourceID: String
    var name: String
    var trackIDs: [String]
    var keepUpdated: Bool
}
private struct OfflineManifest: Codable {
    var profiles: [OfflineProfile] = []
    var tracks: [OfflineTrack] = []
    var collections: [OfflineCollection]?
}
enum OfflineError: LocalizedError {
    case storage, unavailable, limit, invalidMedia, session
    var errorDescription: String? {
        switch self {
        case .storage: "Offline-Speicher konnte nicht geschrieben werden. Bitte freien Speicher prüfen."
        case .unavailable: "Dieser Titel ist noch nicht vollständig offline gespeichert."
        case .limit: "Das Speicherlimit oder die maximale Dateigröße von 256 MiB ist erreicht."
        case .invalidMedia: "Der Server hat keine gültige Audiodatei geliefert."
        case .session: "Zum Herunterladen bitte erneut mit dem Server anmelden."
        }
    }
}

// The delegate moves temporary files before returning. The system may remove
// the URL as soon as didFinishDownloadingTo returns, even during a cold launch.
private final class OfflineTransferDelegate: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    let staging: URL
    let progress: @Sendable (String, Int, Int64, Int64) -> Void
    let finished: @Sendable (String, Int, URL, HTTPURLResponse?) -> Void
    let failed: @Sendable (String, Int, Int?) -> Void
    let events: @Sendable () -> Void
    private var lastProgress: [Int: Date] = [:]
    init(staging: URL, progress: @escaping @Sendable (String, Int, Int64, Int64) -> Void,
         finished: @escaping @Sendable (String, Int, URL, HTTPURLResponse?) -> Void,
         failed: @escaping @Sendable (String, Int, Int?) -> Void, events: @escaping @Sendable () -> Void) {
        self.staging = staging; self.progress = progress; self.finished = finished; self.failed = failed; self.events = events
    }
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) { completionHandler(nil) }
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64,
                    totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        guard let key = downloadTask.taskDescription else { downloadTask.cancel(); return }
        if totalBytesWritten > OfflineLibrary.maximumFileBytes || totalBytesExpectedToWrite > OfflineLibrary.maximumFileBytes { downloadTask.cancel() }
        let now = Date()
        if now.timeIntervalSince(lastProgress[downloadTask.taskIdentifier] ?? .distantPast) >= 0.2 || totalBytesWritten > OfflineLibrary.maximumFileBytes || totalBytesExpectedToWrite == totalBytesWritten {
            lastProgress[downloadTask.taskIdentifier] = now
            progress(key, downloadTask.taskIdentifier, totalBytesWritten, totalBytesExpectedToWrite)
        }
    }
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {
        guard let key = downloadTask.taskDescription else { return }
        let target = staging.appendingPathComponent(UUID().uuidString)
        do {
            try FileManager.default.moveItem(at: location, to: target)
            finished(key, downloadTask.taskIdentifier, target, downloadTask.response as? HTTPURLResponse)
        } catch { failed(key, downloadTask.taskIdentifier, nil) }
    }
    func urlSession(_ session: URLSession, task: URLSessionTask, didCompleteWithError error: Error?) {
        lastProgress.removeValue(forKey: task.taskIdentifier)
        if let error, let key = task.taskDescription { failed(key, task.taskIdentifier, (error as? URLError)?.errorCode) }
    }
    func urlSessionDidFinishEvents(forBackgroundURLSession session: URLSession) { events() }
}

@MainActor @Observable final class OfflineLibrary {
    static let shared = OfflineLibrary()
    nonisolated static let maximumFileBytes: Int64 = 256 * 1024 * 1024
    nonisolated static let backgroundIdentifier = "org.ytmdl.player.offline.v1"
    private(set) var profiles: [OfflineProfile] = []
    private(set) var records: [OfflineTrack] = []
    private(set) var collections: [OfflineCollection] = []
    private(set) var scope: String?
    var protectedTrackIDs: Set<String> = []
    var error: String?
    var wifiOnly: Bool { didSet { preferences.set(wifiOnly, forKey: "offlineWiFiOnly") } }
    var limitGB: Int { didSet { preferences.set(limitGB, forKey: "offlineLimitGB") } }
    var cachePlayed: Bool { didSet { preferences.set(cachePlayed, forKey: "offlineCachePlayed") } }
    var syncFavorites: Bool { didSet { preferences.set(syncFavorites, forKey: "offlineSyncFavorites") } }
    @ObservationIgnored private let root: URL
    @ObservationIgnored private let preferences: UserDefaults
    @ObservationIgnored private let backgroundTransfers: Bool
    @ObservationIgnored private var clients: [String: APIClient] = [:]
    @ObservationIgnored private var tasks: [String: URLSessionDownloadTask] = [:]
    @ObservationIgnored private var transfers: URLSession?
    @ObservationIgnored private var delegate: OfflineTransferDelegate?
    @ObservationIgnored var backgroundCompletion: (() -> Void)?
    @ObservationIgnored private var auxiliary: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var reconciling = true
    var currentRecords: [OfflineTrack] { records.filter { $0.scope == scope } }
    var currentCollections: [OfflineCollection] { collections.filter { $0.scope == scope } }
    var readyTracks: [Track] { availableTracks(in: currentRecords) }
    func availableTracks(in entries: [OfflineTrack]) -> [Track] {
        entries.filter { $0.scope == scope && storedAudioURL($0) != nil }.map(\.track)
    }
    var usedBytes: Int64 { currentRecords.filter { $0.state == .ready }.reduce(0) { $0 + $1.bytes } }
    var totalBytes: Int64 { records.filter { $0.state == .ready }.reduce(0) { $0 + $1.bytes } }
    var limitBytes: Int64 { Int64(min(128, max(1, limitGB))) * 1024 * 1024 * 1024 }
    nonisolated static func digest(_ text: String) -> String { SHA256.hash(data: Data(text.utf8)).map { String(format: "%02x", $0) }.joined() }
    static func profileID(server: ServerAddress, userID: String) -> String {
        var origin = URLComponents(url: server.url, resolvingAgainstBaseURL: false)!
        origin.path = ""; origin.host = origin.host?.lowercased()
        return digest((origin.string ?? "") + "\n" + userID)
    }
    init(root: URL? = nil, preferences: UserDefaults = .standard, startTransfers: Bool = true) {
        self.preferences = preferences; backgroundTransfers = root == nil
        self.root = root ?? FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("YTMDL/Offline", isDirectory: true)
        wifiOnly = preferences.object(forKey: "offlineWiFiOnly") as? Bool ?? true
        limitGB = preferences.object(forKey: "offlineLimitGB") as? Int ?? 8
        cachePlayed = preferences.bool(forKey: "offlineCachePlayed")
        syncFavorites = preferences.bool(forKey: "offlineSyncFavorites")
        do {
            try FileManager.default.createDirectory(at: self.root, withIntermediateDirectories: true)
            try FileManager.default.createDirectory(at: self.root.appendingPathComponent("staging"), withIntermediateDirectories: true)
            var directory = self.root; var resources = URLResourceValues(); resources.isExcludedFromBackup = true
            try directory.setResourceValues(resources)
            let manifest = self.root.appendingPathComponent("manifest.json")
            if FileManager.default.fileExists(atPath: manifest.path) {
                let size = try manifest.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0
                guard size < 16 * 1024 * 1024 else { throw OfflineError.storage }
                let saved = try JSONDecoder().decode(OfflineManifest.self, from: Data(contentsOf: manifest))
                profiles = saved.profiles.filter { $0.id == Self.digestCanonicalProfile($0) }
                let scopes = Set(profiles.map(\.id))
                collections = (saved.collections ?? []).filter { scopes.contains($0.scope) }
                records = saved.tracks.filter { scopes.contains($0.scope) && $0.id == Self.digest($0.scope + "\n" + $0.track.id) }
            }
            let staging = self.root.appendingPathComponent("staging")
            for url in (try? FileManager.default.contentsOfDirectory(at: staging, includingPropertiesForKeys: [.contentModificationDateKey])) ?? [] {
                if let date = try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate, date < Date().addingTimeInterval(-86400) { try? FileManager.default.removeItem(at: url) }
            }
            if startTransfers { createSession() } else { reconciling = false }
        } catch { self.error = OfflineError.storage.localizedDescription; reconciling = false }
    }
    private static func digestCanonicalProfile(_ profile: OfflineProfile) -> String {
        guard let server = try? ServerAddress(profile.origin, allowLocalHTTP: true) else { return "" }
        return profileID(server: server, userID: profile.user.id)
    }
    func configure(client: APIClient, user: User) {
        let id = Self.profileID(server: client.server, userID: user.id)
        scope = id; clients[id] = client
        let profile = OfflineProfile(id: id, origin: client.server.url.absoluteString, user: user)
        if let index = profiles.firstIndex(where: { $0.id == id }) { profiles[index] = profile }
        else { profiles.append(profile) }
        save()
        pump()
    }
    func selectProfile(_ profile: OfflineProfile) { scope = profile.id }
    func detach() {
        if let scope {
            clients.removeValue(forKey: scope)
            for record in currentRecords where record.state != .ready {
                pause(record.track.id); tasks.removeValue(forKey: record.id)?.cancel()
            }
            clients.removeValue(forKey: scope)
            for record in currentRecords { auxiliary.removeValue(forKey: record.id)?.cancel() }
        }
        scope = nil
    }
    func entry(_ trackID: String) -> OfflineTrack? { currentRecords.first { $0.track.id == trackID } }
    func audioURL(_ trackID: String) -> URL? {
        guard let entry = entry(trackID) else { return nil }
        return storedAudioURL(entry)
    }
    private func storedAudioURL(_ entry: OfflineTrack) -> URL? {
        guard entry.state == .ready, let name = entry.fileName,
              name.hasPrefix(entry.id + "."), !name.contains("/"), !name.contains("\\"), !name.contains("..") else { return nil }
        let url = root.appendingPathComponent(entry.scope).appendingPathComponent(name)
        guard let values = try? url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey, .fileSizeKey]),
              values.isRegularFile == true, values.isSymbolicLink != true, Int64(values.fileSize ?? 0) == entry.bytes else { return nil }
        return url
    }
    func artworkURL(_ trackID: String) -> URL? { sidecar(trackID, suffix: ".artwork") }
    func lyrics(_ trackID: String) -> String? {
        guard let url = sidecar(trackID, suffix: ".lyrics"), let data = try? Data(contentsOf: url), data.count <= 256 * 1024 else { return nil }
        return String(data: data, encoding: .utf8)
    }
    private func sidecar(_ id: String, suffix: String) -> URL? {
        guard let entry = entry(id) else { return nil }
        let url = root.appendingPathComponent(entry.scope).appendingPathComponent(entry.id + suffix)
        guard let values = try? url.resourceValues(forKeys: [.isRegularFileKey, .isSymbolicLinkKey]), values.isRegularFile == true, values.isSymbolicLink != true else { return nil }
        return url
    }
    func enqueue(_ tracks: [Track], automatic: Bool = false) {
        guard let scope, clients[scope] != nil else { error = OfflineError.session.localizedDescription; return }
        for track in tracks.prefix(500) {
            let id = Self.digest(scope + "\n" + track.id)
            if let index = records.firstIndex(where: { $0.id == id }) {
                if !automatic { records[index].automatic = false }
                if records[index].state == .ready && audioURL(track.id) != nil { continue }
                if records[index].state == .downloading { continue }
                records[index].state = .queued; records[index].failure = nil
            } else {
                guard records.count < 10000 else { error = "Maximal 10.000 Offline-Titel pro Gerät."; break }
                records.append(OfflineTrack(id: id, scope: scope, track: track, state: .queued, automatic: automatic))
            }
        }
        save(); pump()
    }
    func pause(_ id: String) {
        guard let index = records.firstIndex(where: { $0.scope == scope && $0.track.id == id }), records[index].state != .ready else { return }
        records[index].state = .paused
        if let task = tasks[records[index].id], task.state == .running { task.suspend() }
        save(); pump()
    }
    func remove(_ id: String) {
        guard let record = entry(id) else { return }
        tasks.removeValue(forKey: record.id)?.cancel(); auxiliary.removeValue(forKey: record.id)?.cancel()
        let directory = root.appendingPathComponent(record.scope)
        do {
            for url in try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil) where url.lastPathComponent.hasPrefix(record.id + ".") {
                try FileManager.default.removeItem(at: url)
            }
        } catch { if FileManager.default.fileExists(atPath: directory.path) { self.error = OfflineError.storage.localizedDescription; return } }
        records.removeAll { $0.id == record.id }; save(); pump()
    }
    func clearCurrent() {
        for record in currentRecords { remove(record.track.id) }
        collections.removeAll { $0.scope == scope }; save()
    }
    func rememberCollection(_ kind: CollectionKind, tracks: [Track]) {
        guard let scope else { return }
        let type: String, source: String
        switch kind {
        case .favorites: type = "favorites"; source = ""
        case .playlist(let value): type = "playlist"; source = value.id
        case .artist(let value): type = "artist"; source = value.id
        case .release(let value): type = "release"; source = value.id
        }
        let id = Self.digest(scope + "\n" + type + "\n" + source)
        let previous = collections.first { $0.id == id }
        collections.removeAll { $0.id == id }
        collections.append(OfflineCollection(id: id, scope: scope, kind: type, sourceID: source, name: kind.title, trackIDs: Array(tracks.prefix(500).map(\.id)), keepUpdated: previous?.keepUpdated ?? false)); save()
    }
    func setKeepUpdated(_ id: String, enabled: Bool) {
        guard let index = collections.firstIndex(where: { $0.scope == scope && $0.id == id }) else { return }
        collections[index].keepUpdated = enabled; save()
    }
    func updateCollection(_ id: String, tracks: [Track]) {
        guard let index = collections.firstIndex(where: { $0.scope == scope && $0.id == id }) else { return }
        collections[index].trackIDs = Array(tracks.prefix(500).map(\.id)); save(); enqueue(tracks)
    }
    private func save() {
        do {
            let data = try JSONEncoder().encode(OfflineManifest(profiles: profiles, tracks: records, collections: collections))
            try data.write(to: root.appendingPathComponent("manifest.json"), options: .atomic)
            #if os(iOS)
            try FileManager.default.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: root.path)
            #endif
        } catch { self.error = OfflineError.storage.localizedDescription }
    }
    func closeTransfers() { transfers?.invalidateAndCancel(); transfers = nil }
    private func createSession() {
        let delegate = OfflineTransferDelegate(staging: root.appendingPathComponent("staging"), progress: { [weak self] id, taskID, bytes, expected in
            Task { @MainActor in self?.progress(id, taskID: taskID, bytes: bytes, expected: expected) }
        }, finished: { [weak self] id, taskID, location, response in
            Task { @MainActor in self?.finish(id, location: location, response: response, taskID: taskID) }
        }, failed: { [weak self] id, taskID, code in
            Task { @MainActor in self?.fail(id, taskID: taskID, code: code) }
        }, events: { [weak self] in
            Task { @MainActor in self?.backgroundCompletion?(); self?.backgroundCompletion = nil }
        })
        self.delegate = delegate
        #if os(iOS)
        let configuration = backgroundTransfers ? URLSessionConfiguration.background(withIdentifier: Self.backgroundIdentifier) : URLSessionConfiguration.ephemeral
        if backgroundTransfers { configuration.sessionSendsLaunchEvents = true; configuration.isDiscretionary = false }
        #else
        let configuration = URLSessionConfiguration.ephemeral
        #endif
        configuration.httpCookieStorage = nil; configuration.httpShouldSetCookies = false; configuration.urlCache = nil
        configuration.timeoutIntervalForResource = 60 * 60
        configuration.httpMaximumConnectionsPerHost = 2
        let session = URLSession(configuration: configuration, delegate: delegate, delegateQueue: nil)
        transfers = session
        Task { [weak self] in
            let all = await session.allTasks
            guard let self else { return }
            for task in all {
                guard let download = task as? URLSessionDownloadTask, let id = task.taskDescription,
                      let record = records.first(where: { $0.id == id }), record.state == .downloading || record.state == .paused else { task.cancel(); continue }
                tasks[id] = download
                if record.state == .paused && task.state == .running { task.suspend() }
            }
            // A process terminated by the user may lose a system transfer.
            // Do not retry with stale credentials; require an authenticated session.
            for index in records.indices where records[index].state == .downloading && tasks[records[index].id] == nil { records[index].state = .paused }
            reconciling = false; save(); pump()
        }
    }
    private func pump() {
        guard !reconciling, let transfers, !records.contains(where: { $0.state == .downloading }) else { return }
        guard let index = records.firstIndex(where: { $0.state == .queued && clients[$0.scope] != nil }), let client = clients[records[index].scope] else { return }
        let record = records[index]
        if totalBytes >= limitBytes {
            // Only automatically cached music may be evicted, never pinned downloads.
            if let oldest = currentRecords.filter({ $0.automatic && $0.state == .ready && !protectedTrackIDs.contains($0.track.id) }).min(by: { $0.date < $1.date }) { remove(oldest.track.id); return }
            records[index].state = .failed; records[index].failure = OfflineError.limit.localizedDescription; save(); Task { pump() }; return
        }
        do {
            let path = try client.server.itemPath("tracks", id: record.track.id, suffix: "/stream")
            var request = try client.request(path)
            request.setValue("audio/*, application/ogg, application/octet-stream", forHTTPHeaderField: "Accept")
            let cookies = client.authenticationCookies.filter { client.server.isSecure || !$0.isSecure }
            guard cookies.contains(where: { $0.name == "ytmdl_session" }) else { throw OfflineError.session }
            request.setValue(HTTPCookie.requestHeaderFields(with: cookies)["Cookie"], forHTTPHeaderField: "Cookie")
            request.allowsCellularAccess = !wifiOnly
            request.allowsExpensiveNetworkAccess = !wifiOnly
            records[index].state = .downloading; records[index].failure = nil
            let task = tasks[record.id] ?? transfers.downloadTask(with: request)
            task.taskDescription = record.id; tasks[record.id] = task
            records[index].transferID = task.taskIdentifier; save(); task.resume()
        } catch { records[index].state = .failed; records[index].failure = (error as? OfflineError)?.localizedDescription ?? "Download konnte nicht begonnen werden."; save(); Task { pump() } }
    }
    private func progress(_ id: String, taskID: Int, bytes: Int64, expected: Int64) {
        guard tasks[id]?.taskIdentifier == taskID,
              let index = records.firstIndex(where: { $0.id == id }), records[index].state == .downloading else { return }
        records[index].bytes = bytes; records[index].expected = max(0, expected)
        if bytes > Self.maximumFileBytes || expected > Self.maximumFileBytes || totalBytes + bytes > limitBytes {
            tasks.removeValue(forKey: id)?.cancel(); records[index].state = .failed; records[index].failure = OfflineError.limit.localizedDescription; save(); pump()
        }
    }
    func finish(_ id: String, location: URL, response: HTTPURLResponse?, taskID: Int? = nil) {
        defer { try? FileManager.default.removeItem(at: location) }
        guard let index = records.firstIndex(where: { $0.id == id }), records[index].state == .downloading || records[index].state == .paused else { return }
        // Persist the attempt identity before resume. A background completion can
        // arrive on cold launch before allTasks has repopulated the live map.
        if let taskID, records[index].transferID != taskID { return }
        tasks.removeValue(forKey: id)
        do {
            if response?.statusCode == 401 || response?.statusCode == 403 { throw OfflineError.session }
            guard let response, response.statusCode == 200, let profile = profiles.first(where: { $0.id == records[index].scope }),
                  response.url?.host?.lowercased() == URL(string: profile.origin)?.host?.lowercased(),
                  response.url?.scheme == URL(string: profile.origin)?.scheme,
                  response.url?.port == URL(string: profile.origin)?.port else { throw OfflineError.invalidMedia }
            let size = Int64(try location.resourceValues(forKeys: [.fileSizeKey]).fileSize ?? 0)
            guard size > 0, size <= Self.maximumFileBytes, totalBytes + size <= limitBytes else { throw OfflineError.limit }
            let ext = try Self.mediaExtension(location, mime: response.mimeType ?? "")
            let directory = root.appendingPathComponent(records[index].scope)
            try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
            let name = id + "." + ext
            let target = directory.appendingPathComponent(name)
            if FileManager.default.fileExists(atPath: target.path) { try FileManager.default.removeItem(at: target) }
            try FileManager.default.moveItem(at: location, to: target)
            #if os(iOS)
            try FileManager.default.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: target.path)
            #endif
            records[index].state = .ready; records[index].bytes = size; records[index].expected = size; records[index].fileName = name; records[index].failure = nil
            save(); cacheDetails(records[index])
        } catch { records[index].state = .failed; records[index].failure = (error as? OfflineError)?.localizedDescription ?? OfflineError.storage.localizedDescription; save() }
        pump()
    }
    nonisolated static func mediaExtension(_ url: URL, mime: String) throws -> String {
        guard mime.hasPrefix("audio/") || ["application/ogg", "application/octet-stream"].contains(mime) else { throw OfflineError.invalidMedia }
        let handle = try FileHandle(forReadingFrom: url); defer { try? handle.close() }
        let header = try handle.read(upToCount: 32) ?? Data()
        if header.starts(with: Data("OggS".utf8)) { return "ogg" }
        if header.starts(with: Data("fLaC".utf8)) { return "flac" }
        if header.count >= 12, header[4..<8] == Data("ftyp".utf8) { return "m4a" }
        if header.starts(with: Data("ID3".utf8)) || (header.count >= 2 && header[0] == 255 && header[1] & 224 == 224) { return mime == "audio/aac" ? "aac" : "mp3" }
        if header.starts(with: Data("RIFF".utf8)), header.count >= 12, header[8..<12] == Data("WAVE".utf8) { return "wav" }
        throw OfflineError.invalidMedia
    }
    private func fail(_ id: String, taskID: Int, code: Int?) {
        guard tasks[id]?.taskIdentifier == taskID else { return }
        tasks.removeValue(forKey: id)
        guard let index = records.firstIndex(where: { $0.id == id }), records[index].state == .downloading else { return }
        records[index].state = .failed
        records[index].failure = code == URLError.userAuthenticationRequired.rawValue ? OfflineError.session.localizedDescription : "Download unterbrochen. Bitte Verbindung prüfen und erneut versuchen."
        save(); pump()
    }
    private func cacheDetails(_ record: OfflineTrack) {
        guard let client = clients[record.scope] else { return }
        auxiliary[record.id]?.cancel()
        auxiliary[record.id] = Task { [weak self] in
            guard let self else { return }
            let directory = root.appendingPathComponent(record.scope)
            do {
                let (data, response) = try await client.session.data(for: client.artworkRequest(kind: "tracks", id: record.track.id))
                try Task.checkCancellation()
                if (response as? HTTPURLResponse)?.statusCode == 200, data.count <= 8 * 1024 * 1024, ArtworkPalette.thumbnail(data) != nil, records.contains(where: { $0.id == record.id }) {
                    try data.write(to: directory.appendingPathComponent(record.id + ".artwork"), options: .atomic)
                }
            } catch { }
            do {
                let lyrics: Lyrics = try await client.get(client.server.itemPath("tracks", id: record.track.id, suffix: "/lyrics"))
                try Task.checkCancellation()
                if let content = lyrics.content, content.utf8.count <= 256 * 1024, records.contains(where: { $0.id == record.id }) {
                    try Data(content.utf8).write(to: directory.appendingPathComponent(record.id + ".lyrics"), options: .atomic)
                }
            } catch { }
            auxiliary.removeValue(forKey: record.id)
        }
    }
}
