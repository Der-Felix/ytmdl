import CoreGraphics
import ImageIO
import UniformTypeIdentifiers
import Foundation

// Use the original repository artwork for both in-app branding and app icons.
// Resolve the source relative to this script so generation works from any cwd.
let repository = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
let markURL = repository.appendingPathComponent("frontend/public/logo-mark.png")
guard let source = CGImageSourceCreateWithURL(markURL as CFURL, nil),
      let mark = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
    throw NSError(domain: "Original logo missing", code: 1)
}
let root = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "apple/Resources/Assets.xcassets")
let icon = root.appendingPathComponent("AppIcon.appiconset")
try FileManager.default.createDirectory(at: icon, withIntermediateDirectories: true)
let branding = root.appendingPathComponent("BrandMark.imageset")
try FileManager.default.createDirectory(at: branding, withIntermediateDirectories: true)
try Data(contentsOf: markURL).write(to: branding.appendingPathComponent("logo-mark.png"))
let brandingContents: [String: Any] = ["images": [["idiom": "universal", "filename": "logo-mark.png"]], "info": ["author": "xcode", "version": 1]]
try JSONSerialization.data(withJSONObject: brandingContents, options: [.prettyPrinted, .sortedKeys]).write(to: branding.appendingPathComponent("Contents.json"))
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
    cg.interpolationQuality = .high
    let inset = CGFloat(size) * 0.10
    let side = CGFloat(size) - inset * 2
    let ratio = CGFloat(mark.width) / CGFloat(mark.height)
    let width = side * min(1, ratio), height = side / max(1, ratio)
    cg.draw(mark, in: CGRect(x: (CGFloat(size) - width) / 2, y: (CGFloat(size) - height) / 2, width: width, height: height))
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
