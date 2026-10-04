import AVFoundation
import MediaToolbox
import Synchronization
import Observation
import YTMDLCore

enum EqualizerPreset: String, CaseIterable, Identifiable {
    case flat, bass, vocal, acoustic, electronic
    var id: String { rawValue }
    var name: String { switch self { case .flat: "Neutral"; case .bass: "Mehr Bass"; case .vocal: "Stimmen"; case .acoustic: "Akustisch"; case .electronic: "Elektronisch" } }
    var gains: [Double] {
        switch self {
        case .flat: Array(repeating: 0, count: 10)
        case .bass: [5, 4, 3, 1, 0, 0, 0, 0, 0, 0]
        case .vocal: [-2, -2, -1, 0, 2, 3, 3, 2, 0, -1]
        case .acoustic: [2, 2, 1, 0, 1, 1, 2, 3, 2, 1]
        case .electronic: [4, 3, 1, 0, -1, 0, 1, 2, 3, 2]
        }
    }
}

/// The render callback reads packed atomic values. No locks, UI objects or
/// allocations are used while processing samples.
final class EqualizerParameters: @unchecked Sendable {
    let low = Atomic<UInt64>(0)
    let high = Atomic<UInt64>(0)
    let frames = Atomic<UInt64>(0)
    let format = Atomic<Int>(0) // 0: awaiting audio, 1: supported, 2: bypassed format
    let meterRMS = Atomic<UInt64>(0)
    let inputEnergy = Atomic<UInt64>(0)
    let outputEnergy = Atomic<UInt64>(0)
    init() { publish(gains: Array(repeating: 0, count: 10), enabled: false, preamp: 0, headroom: true) }
    func publish(gains: [Double], enabled: Bool, preamp: Double, headroom: Bool) {
        var a: UInt64 = 0, b: UInt64 = 0
        for index in 0..<10 {
            let gain = gains.indices.contains(index) && gains[index].isFinite ? gains[index] : 0
            let encoded = UInt64((min(12, max(-12, gain)) * 2 + 24).rounded())
            if index < 8 { a |= encoded << (index * 8) } else { b |= encoded << ((index - 8) * 8) }
        }
        let safePreamp = preamp.isFinite ? min(6, max(-12, preamp)) : 0
        b |= UInt64((safePreamp * 2 + 24).rounded()) << 16
        if enabled { b |= 1 << 24 }
        if headroom { b |= 1 << 25 }
        low.store(a, ordering: .releasing); high.store(b, ordering: .releasing)
    }
}

@MainActor @Observable final class EqualizerModel {
    static let frequencies: [Double] = [31.5, 63, 125, 250, 500, 1000, 2000, 4000, 8000, 16000]
    private(set) var enabled: Bool
    private(set) var gains: [Double]
    private(set) var preamp: Double
    private(set) var headroom: Bool
    private(set) var preset: String
    @ObservationIgnored let parameters = EqualizerParameters()
    @ObservationIgnored var onEnabledChange: ((Bool) -> Void)?
    @ObservationIgnored private let preferences: UserDefaults
    init(preferences: UserDefaults = .standard) {
        self.preferences = preferences
        enabled = preferences.bool(forKey: "eqEnabled")
        let saved = preferences.array(forKey: "eqGains") as? [Double]
        gains = saved?.count == 10 ? saved!.map { $0.isFinite ? min(12, max(-12, $0)) : 0 } : Array(repeating: 0, count: 10)
        let level = preferences.double(forKey: "eqPreamp")
        preamp = level.isFinite ? min(6, max(-12, level)) : 0
        headroom = preferences.object(forKey: "eqHeadroom") as? Bool ?? true
        preset = preferences.string(forKey: "eqPreset") ?? "flat"
        publish()
    }
    func setEnabled(_ value: Bool) {
        let changed = enabled != value
        enabled = value; publish()
        if changed { onEnabledChange?(value) }
    }
    func setGain(_ value: Double, band: Int) {
        guard gains.indices.contains(band), value.isFinite else { return }
        gains[band] = min(12, max(-12, value)); preset = "custom"; publish()
    }
    func setPreamp(_ value: Double) { guard value.isFinite else { return }; preamp = min(6, max(-12, value)); publish() }
    func setHeadroom(_ value: Bool) { headroom = value; publish() }
    func select(_ preset: EqualizerPreset) { gains = preset.gains; self.preset = preset.rawValue; publish() }
    private func publish() {
        parameters.publish(gains: gains, enabled: enabled, preamp: preamp, headroom: headroom)
        preferences.set(enabled, forKey: "eqEnabled"); preferences.set(gains, forKey: "eqGains")
        preferences.set(preamp, forKey: "eqPreamp"); preferences.set(headroom, forKey: "eqHeadroom")
        preferences.set(preset, forKey: "eqPreset")
    }
}

/// Cascaded peaking filters (W3C Audio EQ Cookbook), with separate delay lines
/// per channel. Storage is allocated in prepare, never in the render callback.
final class EqualizerKernel {
    private let coefficients = UnsafeMutablePointer<Double>.allocate(capacity: 50)
    private var delays: UnsafeMutablePointer<Double>?
    private var channels = 0
    private var sampleRate: Double = 48000
    private var lastLow: UInt64 = .max, lastHigh: UInt64 = .max
    private var targetLevel: Double = 1, level: Double = 1
    private(set) var enabled = false
    init() { coefficients.initialize(repeating: 0, count: 50) }
    deinit { coefficients.deallocate(); delays?.deallocate() }
    func prepare(rate: Double, channels: Int) {
        _ = EqualizerFrequencies.values[0]
        delays?.deallocate(); delays = nil
        self.channels = max(1, min(32, channels)); sampleRate = rate.isFinite && rate > 0 ? rate : 48000
        delays = .allocate(capacity: self.channels * 20); delays!.initialize(repeating: 0, count: self.channels * 20)
        lastLow = .max; lastHigh = .max; level = 1
    }
    func reset() { delays?.update(repeating: 0, count: channels * 20) }
    func refresh(_ parameters: EqualizerParameters) {
        let low = parameters.low.load(ordering: .acquiring), high = parameters.high.load(ordering: .acquiring)
        guard low != lastLow || high != lastHigh else { return }
        lastLow = low; lastHigh = high
        enabled = high & (1 << 24) != 0
        for band in 0..<10 {
            let encoded = band < 8 ? (low >> (band * 8)) & 255 : (high >> ((band - 8) * 8)) & 255
            let gain = Double(encoded) / 2 - 12
            let frequency = EqualizerFrequencies.values[band]
            let c = coefficients.advanced(by: band * 5)
            if frequency >= sampleRate * 0.49 {
                c[0] = 1; c[1] = 0; c[2] = 0; c[3] = 0; c[4] = 0
                continue // A band above Nyquist must not shift into the audible range.
            }
            let omega = 2 * Double.pi * frequency / sampleRate, a = pow(10, gain / 40)
            let alpha = sin(omega) / (2 * 1.41421356237), cosine = cos(omega)
            let a0 = 1 + alpha / a
            c[0] = (1 + alpha * a) / a0; c[1] = -2 * cosine / a0
            c[2] = (1 - alpha * a) / a0; c[3] = -2 * cosine / a0; c[4] = (1 - alpha / a) / a0
        }
        let preamp = Double((high >> 16) & 255) / 2 - 12
        var peak = 1.0
        if high & (1 << 25) != 0 {
            // Evaluate the complete response, including overlaps between bands.
            for step in 0..<128 {
                let f = min(sampleRate * 0.49, 20 * pow(1000, Double(step) / 127))
                let w = 2 * Double.pi * f / sampleRate
                let cr = cos(w), ci = -sin(w), dr = cos(2 * w), di = -sin(2 * w)
                var response = 1.0
                for band in 0..<10 {
                    let c = coefficients.advanced(by: band * 5)
                    let nr = c[0] + c[1] * cr + c[2] * dr, ni = c[1] * ci + c[2] * di
                    let ar = 1 + c[3] * cr + c[4] * dr, ai = c[3] * ci + c[4] * di
                    response *= sqrt((nr * nr + ni * ni) / max(1e-20, ar * ar + ai * ai))
                }
                peak = max(peak, response * pow(10, preamp / 20))
            }
        }
        targetLevel = pow(10, preamp / 20) / peak
        level = min(level, targetLevel)
    }
    func sample(_ input: Double, channel: Int) -> Double {
        guard enabled, let delays, channel < channels else { return input }
        var value = input
        for band in 0..<10 {
            let c = coefficients.advanced(by: band * 5), d = delays.advanced(by: channel * 20 + band * 2)
            let output = c[0] * value + d[0]
            d[0] = c[1] * value - c[3] * output + d[1]; d[1] = c[2] * value - c[4] * output
            value = output
        }
        level += 0.002 * (targetLevel - level)
        let result = value * level
        return result.isFinite ? min(1, max(-1, result)) : 0
    }
}
private enum EqualizerFrequencies { static let values: [Double] = [31.5, 63, 125, 250, 500, 1000, 2000, 4000, 8000, 16000] }

private final class EqualizerTapState {
    let parameters: EqualizerParameters
    let kernel = EqualizerKernel()
    var format = AudioStreamBasicDescription()
    var supported = false
    init(_ parameters: EqualizerParameters) { self.parameters = parameters }
    func prepare(_ format: AudioStreamBasicDescription) {
        self.format = format
        supported = format.mFormatID == kAudioFormatLinearPCM && format.mChannelsPerFrame > 0 && format.mChannelsPerFrame <= 32
            && format.mFormatFlags & kAudioFormatFlagIsBigEndian == 0
            && ((format.mFormatFlags & kAudioFormatFlagIsFloat != 0 && [32, 64].contains(format.mBitsPerChannel))
                || (format.mFormatFlags & kAudioFormatFlagIsSignedInteger != 0 && [16, 32].contains(format.mBitsPerChannel)))
        let packedChannels = format.mFormatFlags & kAudioFormatFlagIsNonInterleaved != 0 ? 1 : format.mChannelsPerFrame
        supported = supported && format.mBytesPerFrame == packedChannels * format.mBitsPerChannel / 8
        kernel.prepare(rate: format.mSampleRate, channels: Int(format.mChannelsPerFrame))
        parameters.format.store(supported ? 1 : 2, ordering: .releasing)
    }
    func process(_ buffers: UnsafeMutablePointer<AudioBufferList>, frames: Int, discontinuity: Bool) {
        guard supported else { return }
        kernel.refresh(parameters)
        if discontinuity { kernel.reset() }
        let list = UnsafeMutableAudioBufferListPointer(buffers)
        var channelBase = 0, sampleCount = 0, inputEnergy = 0.0, outputEnergy = 0.0
        for buffer in list {
            guard let data = buffer.mData else { continue }
            let channels = Int(buffer.mNumberChannels)
            let count = min(frames * channels, Int(buffer.mDataByteSize) / Int(format.mBitsPerChannel / 8))
            for index in 0..<count {
                let input: Double
                let floating = format.mFormatFlags & kAudioFormatFlagIsFloat != 0
                if floating && format.mBitsPerChannel == 32 { input = Double(data.assumingMemoryBound(to: Float.self)[index]) }
                else if floating { input = data.assumingMemoryBound(to: Double.self)[index] }
                else if format.mBitsPerChannel == 16 { input = Double(data.assumingMemoryBound(to: Int16.self)[index]) / 32768 }
                else { input = Double(data.assumingMemoryBound(to: Int32.self)[index]) / 2147483648 }
                let output = kernel.sample(input, channel: channelBase + index % max(1, channels))
                if kernel.enabled {
                    if floating && format.mBitsPerChannel == 32 { data.assumingMemoryBound(to: Float.self)[index] = Float(output) }
                    else if floating { data.assumingMemoryBound(to: Double.self)[index] = output }
                    else if format.mBitsPerChannel == 16 { data.assumingMemoryBound(to: Int16.self)[index] = Int16(min(32767, max(-32768, output * 32768))) }
                    else { data.assumingMemoryBound(to: Int32.self)[index] = Int32(min(2147483647, max(-2147483648, output * 2147483648))) }
                }
                inputEnergy += input * input; outputEnergy += output * output; sampleCount += 1
            }
            channelBase += channels
        }
        parameters.inputEnergy.store(inputEnergy.bitPattern, ordering: .relaxed)
        parameters.outputEnergy.store(outputEnergy.bitPattern, ordering: .relaxed)
        parameters.meterRMS.store((sqrt(outputEnergy / Double(max(1, sampleCount)))).bitPattern, ordering: .relaxed)
        parameters.frames.wrappingAdd(UInt64(frames), ordering: .releasing)
    }
}

@MainActor func attachEqualizer(to item: AVPlayerItem, parameters: EqualizerParameters) throws {
    let retained = Unmanaged.passRetained(EqualizerTapState(parameters))
    var callbacks = MTAudioProcessingTapCallbacks(version: kMTAudioProcessingTapCallbacksVersion_0, clientInfo: retained.toOpaque(),
        init: { _, info, storage in storage.pointee = info },
        finalize: { tap in Unmanaged<EqualizerTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).release() },
        prepare: { tap, _, format in Unmanaged<EqualizerTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue().prepare(format.pointee) },
        unprepare: { _ in },
        process: { tap, count, _, buffers, outputCount, outputFlags in
            var range = CMTimeRange.zero
            let status = MTAudioProcessingTapGetSourceAudio(tap, count, buffers, outputFlags, &range, outputCount)
            guard status == noErr else { outputCount.pointee = 0; return }
            let state = Unmanaged<EqualizerTapState>.fromOpaque(MTAudioProcessingTapGetStorage(tap)).takeUnretainedValue()
            state.process(buffers, frames: outputCount.pointee, discontinuity: outputFlags.pointee & kMTAudioProcessingTapFlag_StartOfStream != 0)
        })
    var tap: MTAudioProcessingTap?
    let status = MTAudioProcessingTapCreateWithPreferredFormat(kCFAllocatorDefault, &callbacks, kMTAudioProcessingTapCreationFlag_PostEffects, nil, &tap)
    guard status == noErr, let tap else { retained.release(); throw PlayerError.badResponse }
    let input = AVMutableAudioMixInputParameters()
    input.audioTapProcessor = tap
    let mix = AVMutableAudioMix(); mix.inputParameters = [input]; item.audioMix = mix
}
