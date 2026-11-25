import Foundation

/// Values for sound settings chosen in steps, as on Apple TV, where pickers
/// replace the sliders of the other platforms.
public enum SoundStep {
    /// The speeds every platform offers ("Tempo").
    public static let playbackRates: [Double] = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
    /// Overlap between titles in seconds ("Überblendung"), like the slider elsewhere.
    public static let crossfade: ClosedRange<Double> = 0...12
    /// One equalizer band in dB.
    public static let equalizerGain: ClosedRange<Double> = -12...12
    /// The equalizer's preamplification in dB.
    public static let preamp: ClosedRange<Double> = -12...6

    /// The choices of a picker for a value: every multiple of `step` in
    /// `range`, plus the current value (clamped) when it lies between two
    /// steps, so a finer value set elsewhere stays selected and visible.
    public static func options(around value: Double, step: Double, in range: ClosedRange<Double>) -> [Double] {
        let current = clamped(value, in: range)
        guard step.isFinite, step > 0 else { return [current] }
        let first = (range.lowerBound / step).rounded(.up) * step
        let count = max(0, Int(((range.upperBound - first) / step + 1e-9).rounded(.down)))
        var values = (0...count).map { first + Double($0) * step }.filter { $0 <= range.upperBound }
        if !values.contains(where: { abs($0 - current) < 1e-9 }) { values.append(current); values.sort() }
        return values
    }

    static func clamped(_ value: Double, in range: ClosedRange<Double>) -> Double {
        guard value.isFinite else { return min(range.upperBound, max(range.lowerBound, 0)) }
        return min(range.upperBound, max(range.lowerBound, value))
    }
}

extension Track {
    /// The queue filter of every platform: title, artists and album contain
    /// the text, ignoring case. An empty or blank filter matches everything.
    public func matchesQueueFilter(_ query: String) -> Bool {
        let text = query.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return true }
        return [title, artistText, album].joined(separator: " ").localizedCaseInsensitiveContains(text)
    }
}

extension PlaybackQueue {
    /// The rows a queue filter leaves visible. Positions stay those of the whole
    /// queue, so editing a visible row always edits that entry.
    public func rows(filter query: String) -> [QueueRow] {
        rows { $0.matchesQueueFilter(query) }
    }

    /// The visible row to focus after `position` was removed while only
    /// `visible` rows were shown (positions from before the removal): the next
    /// visible entry, else the one before it. Returns positions after the removal.
    public static func focusAfterRemoving(_ position: Int, visible: [Int]) -> Int? {
        let others = visible.filter { $0 != position }
        if let next = others.filter({ $0 > position }).min() { return next - 1 }
        return others.filter { $0 < position }.max()
    }
}
