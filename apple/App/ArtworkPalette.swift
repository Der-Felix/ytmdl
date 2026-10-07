import Foundation
import CoreGraphics
import ImageIO

struct ArtworkColor: Equatable, Sendable {
    let red: Double, green: Double, blue: Double
    var luminance: Double {
        func linear(_ value: Double) -> Double { value <= 0.04045 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4) }
        return 0.2126 * linear(red) + 0.7152 * linear(green) + 0.0722 * linear(blue)
    }
    // Keep the white transport glyph readable even for pale or yellow covers.
    var button: ArtworkColor {
        var factor = 0.85
        var color = ArtworkColor(red: red * factor, green: green * factor, blue: blue * factor)
        while color.luminance > 0.18 {
            factor *= 0.9
            color = ArtworkColor(red: red * factor, green: green * factor, blue: blue * factor)
        }
        return color
    }
}
struct ArtworkPalette: Equatable, Sendable {
    let dominant: ArtworkColor
    let secondary: ArtworkColor
    let accent: ArtworkColor
    static func extract(_ image: CGImage) -> ArtworkPalette? {
        let side = 32
        var pixels = [UInt8](repeating: 0, count: side * side * 4)
        guard let context = CGContext(data: &pixels, width: side, height: side, bitsPerComponent: 8, bytesPerRow: side * 4,
            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { return nil }
        context.draw(image, in: CGRect(x: 0, y: 0, width: side, height: side))
        var bins: [Int: (count: Int, r: Double, g: Double, b: Double)] = [:]
        for index in stride(from: 0, to: pixels.count, by: 4) {
            guard pixels[index + 3] > 200 else { continue }
            let r = Double(pixels[index]) / 255, g = Double(pixels[index + 1]) / 255, b = Double(pixels[index + 2]) / 255
            let highest = max(r, g, b), lowest = min(r, g, b)
            guard highest > 0.12, lowest < 0.9, highest - lowest > 0.08 else { continue }
            let key = Int(r * 7) * 64 + Int(g * 7) * 8 + Int(b * 7)
            var bucket = bins[key] ?? (0, 0, 0, 0)
            bucket.count += 1; bucket.r += r; bucket.g += g; bucket.b += b; bins[key] = bucket
        }
        let sorted = bins.sorted { left, right in
            func score(_ value: (count: Int, r: Double, g: Double, b: Double)) -> Double {
                let saturation = (max(value.r, value.g, value.b) - min(value.r, value.g, value.b)) / Double(value.count)
                return Double(value.count) * (0.25 + saturation)
            }
            let a = score(left.value), b = score(right.value)
            return a == b ? left.key < right.key : a > b
        }
        guard let first = sorted.first else { return nil }
        func color(_ value: (count: Int, r: Double, g: Double, b: Double)) -> ArtworkColor {
            ArtworkColor(red: value.r / Double(value.count), green: value.g / Double(value.count), blue: value.b / Double(value.count))
        }
        let dominant = color(first.value)
        let second = sorted.dropFirst().first { entry in
            let c = color(entry.value)
            return abs(c.red - dominant.red) + abs(c.green - dominant.green) + abs(c.blue - dominant.blue) > 0.4
        }
        return ArtworkPalette(dominant: dominant, secondary: second.map { color($0.value) } ?? dominant, accent: dominant)
    }
    static func thumbnail(_ data: Data, maxPixelSize: Int = 1400) -> CGImage? {
        guard data.count <= 8 * 1024 * 1024, let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
        return CGImageSourceCreateThumbnailAtIndex(source, 0, [kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize, kCGImageSourceCreateThumbnailWithTransform: true] as CFDictionary)
    }
}
