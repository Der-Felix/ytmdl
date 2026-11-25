import Foundation

/// Values for sound settings adjusted step by step, as on Apple TV, where a
/// remote's +/- buttons replace the sliders of the other platforms.
public enum SoundStep {
    /// The speeds every platform offers ("Tempo").
    public static let playbackRates: [Double] = [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2]
    /// Overlap between titles in seconds ("Überblendung"), like the slider elsewhere.
    public static let crossfade: ClosedRange<Double> = 0...12
    /// One equalizer band in dB.
    public static let equalizerGain: ClosedRange<Double> = -12...12
    /// The equalizer's preamplification in dB.
    public static let preamp: ClosedRange<Double> = -12...6

    /// `value` moved by `delta`, snapped to multiples of the step and kept in
    /// `range`. A value between two steps moves to the next one in that direction.
    public static func adjusted(_ value: Double, by delta: Double, in range: ClosedRange<Double>) -> Double {
        let start = clamped(value, in: range)
        guard delta.isFinite, delta != 0 else { return start }
        let step = abs(delta)
        let units = start / step
        let next = delta > 0 ? (units + 1e-9).rounded(.down) + 1 : (units - 1e-9).rounded(.up) - 1
        return clamped(next * step, in: range)
    }

    /// Whether a press would change the value; a control at its bound is disabled.
    public static func canAdjust(_ value: Double, by delta: Double, in range: ClosedRange<Double>) -> Bool {
        adjusted(value, by: delta, in: range) != clamped(value, in: range)
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
