import SwiftUI

// Mac presentation preferences use actual layout/font sizes, never a scaled bitmap.
enum DesktopTheme: String, CaseIterable, Identifiable {
    case rose, ocean, forest, amber, violet, graphite
    var id: String { rawValue }
    var name: String {
        switch self { case .rose: "Rose"; case .ocean: "Ozean"; case .forest: "Wald"; case .amber: "Abendsonne"; case .violet: "Lavendel"; case .graphite: "Graphit" }
    }
    var accent: Color {
        switch self {
        case .rose: Color(red: 0.88, green: 0.16, blue: 0.40)
        case .ocean: Color(red: 0.04, green: 0.47, blue: 0.73)
        case .forest: Color(red: 0.10, green: 0.51, blue: 0.36)
        case .amber: Color(red: 0.76, green: 0.36, blue: 0.10)
        case .violet: Color(red: 0.54, green: 0.32, blue: 0.80)
        case .graphite: Color(red: 0.43, green: 0.48, blue: 0.58)
        }
    }
    func accent(_ scheme: ColorScheme) -> Color {
        scheme == .dark ? accent.mix(with: .white, by: 0.25) : accent
    }
    func background(_ scheme: ColorScheme) -> Color {
        let values: (Double, Double, Double)
        if scheme == .light {
            switch self {
            case .rose: values = (0.985, 0.958, 0.965)
            case .ocean: values = (0.944, 0.971, 0.985)
            case .forest: values = (0.950, 0.978, 0.961)
            case .amber: values = (0.990, 0.967, 0.937)
            case .violet: values = (0.970, 0.955, 0.989)
            case .graphite: values = (0.963, 0.967, 0.975)
            }
        } else {
            switch self {
            case .rose: values = (0.105, 0.073, 0.095)
            case .ocean: values = (0.055, 0.093, 0.140)
            case .forest: values = (0.058, 0.115, 0.088)
            case .amber: values = (0.133, 0.090, 0.065)
            case .violet: values = (0.105, 0.080, 0.155)
            case .graphite: values = (0.075, 0.081, 0.098)
            }
        }
        return Color(red: values.0, green: values.1, blue: values.2)
    }
    func surface(_ scheme: ColorScheme) -> Color {
        background(scheme).overlayColor(scheme == .dark ? .white : .black, fraction: scheme == .dark ? 0.055 : 0.025)
    }
}
private extension Color {
    // Stable RGB blends keep palette surfaces consistent in AppKit and SwiftUI.
    func overlayColor(_ other: Color, fraction: Double) -> Color {
        // Color.mix is a native SwiftUI color operation; no artwork sampling or network calls.
        mix(with: other, by: fraction)
    }
}
enum DesktopTextSize: String, CaseIterable, Identifiable {
    case normal, large, extraLarge
    var id: String { rawValue }
    var name: String { switch self { case .normal: "Normal"; case .large: "Groß"; case .extraLarge: "Sehr groß" } }
    var scale: CGFloat { switch self { case .normal: 1; case .large: 1.15; case .extraLarge: 1.3 } }
}
private struct DesktopThemeKey: EnvironmentKey { static let defaultValue = DesktopTheme.rose }
private struct DesktopAccentKey: EnvironmentKey { static let defaultValue = Color.pink }
private struct DesktopTextScaleKey: EnvironmentKey { static let defaultValue: CGFloat = 1 }
extension EnvironmentValues {
    var desktopAccent: Color { get { self[DesktopAccentKey.self] } set { self[DesktopAccentKey.self] = newValue } }
    var desktopTheme: DesktopTheme { get { self[DesktopThemeKey.self] } set { self[DesktopThemeKey.self] = newValue } }
    var desktopTextScale: CGFloat { get { self[DesktopTextScaleKey.self] } set { self[DesktopTextScaleKey.self] = newValue } }
}
private struct DesktopFontModifier: ViewModifier {
    @Environment(\.desktopTextScale) private var scale
    let size: CGFloat
    let weight: Font.Weight
    var fallback: Font?
    func body(content: Content) -> some View {
        #if os(macOS)
        content.font(.system(size: size * scale, weight: weight))
        #else
        content.font(fallback ?? .system(size: size, weight: weight))
        #endif
    }
}
extension View {
    func desktopScaledFont(_ size: CGFloat, weight: Font.Weight = .regular, fallback: Font? = nil) -> some View {
        modifier(DesktopFontModifier(size: size, weight: weight, fallback: fallback))
    }
}
