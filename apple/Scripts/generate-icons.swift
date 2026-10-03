import CoreGraphics
import ImageIO
import UniformTypeIdentifiers
import Foundation

// Reproduce the repository's vector music-note mark with Core Graphics.
// Keep generated assets deterministic and independent of external artwork.
let root = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "apple/Resources/Assets.xcassets")
let icon = root.appendingPathComponent("AppIcon.appiconset")
try FileManager.default.createDirectory(at: icon, withIntermediateDirectories: true)
let entries: [(String, Int, String, String)] = [
    ("ios", 1024, "1024x1024", "1x"),
    ("mac", 16, "16x16", "1x"), ("mac", 32, "16x16", "2x"),
    ("mac", 32, "32x32", "1x"), ("mac", 64, "32x32", "2x"),
    ("mac", 128, "128x128", "1x"), ("mac", 256, "128x128", "2x"),
    ("mac", 256, "256x256", "1x"), ("mac", 512, "256x256", "2x"),
    ("mac", 512, "512x512", "1x"), ("mac", 1024, "512x512", "2x")
]
var images: [[String: String]] = []
for (idiom, size, points, scale) in entries {
    let filename = "\(idiom)-\(points)-\(scale).png"
    let cg = CGContext(data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: size * 4, space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
    cg.setFillColor(CGColor(red: 11/255, green: 13/255, blue: 20/255, alpha: 1))
    cg.fill(CGRect(x: 0, y: 0, width: size, height: size))
    cg.scaleBy(x: CGFloat(size)/32, y: -CGFloat(size)/32); cg.translateBy(x: 0, y: -32)
    cg.setFillColor(CGColor(red: 206/255, green: 52/255, blue: 99/255, alpha: 1))
    // Two joined stems and tilted bar, with elliptical note heads.
    cg.fillEllipse(in: CGRect(x: 14.7, y: 16.0, width: 8.2, height: 8.2))
    cg.fillEllipse(in: CGRect(x: 5.7, y: 18.7, width: 8.2, height: 8.2))
    let path = CGMutablePath()
    path.move(to: CGPoint(x: 20.5, y: 7.4)); path.addLine(to: CGPoint(x: 20.5, y: 20.1))
    path.addLine(to: CGPoint(x: 18.1, y: 20.1)); path.addLine(to: CGPoint(x: 18.1, y: 10.4))
    path.addLine(to: CGPoint(x: 11.5, y: 11.9)); path.addLine(to: CGPoint(x: 11.5, y: 22.8))
    path.addLine(to: CGPoint(x: 9.1, y: 22.8)); path.addLine(to: CGPoint(x: 9.1, y: 9.9)); path.closeSubpath()
    cg.addPath(path); cg.fillPath()
    let output = CGImageDestinationCreateWithURL(icon.appendingPathComponent(filename) as CFURL, UTType.png.identifier as CFString, 1, nil)!
    CGImageDestinationAddImage(output, cg.makeImage()!, nil)
    guard CGImageDestinationFinalize(output) else { throw NSError(domain: "Icon export failed", code: 1) }
    var entry = ["idiom": idiom, "filename": filename, "size": points, "scale": scale]
    if idiom == "ios" { entry.removeValue(forKey: "scale"); entry["platform"] = "ios"; entry["idiom"] = "universal" }
    images.append(entry)
}
let contents: [String: Any] = ["images": images, "info": ["author": "xcode", "version": 1]]
try JSONSerialization.data(withJSONObject: contents, options: [.prettyPrinted, .sortedKeys]).write(to: icon.appendingPathComponent("Contents.json"))
try JSONSerialization.data(withJSONObject: ["info": ["author": "xcode", "version": 1]], options: [.prettyPrinted, .sortedKeys]).write(to: root.appendingPathComponent("Contents.json"))
