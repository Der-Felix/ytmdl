// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "YTMDLCore",
    platforms: [.macOS("27.0"), .iOS("27.0"), .tvOS("27.0")],
    products: [.library(name: "YTMDLCore", targets: ["YTMDLCore"])],
    targets: [
        .target(name: "YTMDLCore"),
        .target(name: "YTMDLAppleSupport", dependencies: ["YTMDLCore"], path: "App", exclude: ["YTMDLApp.swift"]),
        .testTarget(name: "YTMDLCoreTests", dependencies: ["YTMDLCore"]),
        .testTarget(name: "YTMDLAppleSupportTests", dependencies: ["YTMDLAppleSupport", "YTMDLCore"]),
    ]
)
