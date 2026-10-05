import Foundation

public struct LyricLine: Identifiable, Equatable, Sendable {
    public let id: Int
    public let seconds: Double
    public let text: String
}
public struct LyricsTimeline: Sendable {
    public let lines: [LyricLine]
    public init(_ content: String) {
        guard content.utf8.count <= 256 * 1024,
              let regex = try? NSRegularExpression(pattern: #"\[(\d{1,3}):(\d{2})(?:\.(\d{1,3}))?\]"#) else { lines = []; return }
        var timed: [(Double, String)] = []
        for raw in content.components(separatedBy: .newlines).prefix(8000) {
            let range = NSRange(raw.startIndex..., in: raw)
            let matches = regex.matches(in: raw, range: range)
            guard let last = matches.last, let end = Range(last.range, in: raw)?.upperBound else { continue }
            let text = String(raw[end...]).trimmingCharacters(in: .whitespaces)
            for match in matches {
                func component(_ index: Int) -> String { Range(match.range(at: index), in: raw).map { String(raw[$0]) } ?? "" }
                guard let minute = Double(component(1)), let second = Double(component(2)), second < 60 else { continue }
                let fraction = component(3)
                let time = minute * 60 + second + (Double(fraction) ?? 0) / pow(10, Double(fraction.count))
                timed.append((time, text))
            }
        }
        lines = timed.enumerated().sorted { $0.element.0 == $1.element.0 ? $0.offset < $1.offset : $0.element.0 < $1.element.0 }
            .enumerated().map { LyricLine(id: $0.offset, seconds: $0.element.element.0, text: $0.element.element.1) }
    }
    public func activeIndex(at seconds: Double) -> Int? {
        guard seconds.isFinite, !lines.isEmpty, seconds >= lines[0].seconds else { return nil }
        var lower = 0, upper = lines.count
        while lower < upper { let middle = (lower + upper) / 2; if lines[middle].seconds <= seconds { lower = middle + 1 } else { upper = middle } }
        return lower - 1
    }
}
public struct PlaybackHandoff: Decodable, Sendable {
    public let id: String
    public let queue: [Track]?
    public let queueIndex: Int
    public let positionSeconds: Double
    public let repeatMode: String
    public let sourceName: String
}
public struct HandoffPayload: Encodable, Sendable {
    public let queueIds: [String]
    public let queueIndex: Int
    public let positionSeconds: Double
    public let repeatMode: String
    public let sourceName: String
    public init(queue: PlaybackQueue, position: Double, repeatMode: String, sourceName: String) {
        queueIds = queue.tracks.map(\.id); queueIndex = queue.index
        positionSeconds = max(0, min(86400, position)); self.repeatMode = repeatMode; self.sourceName = sourceName
    }
}
