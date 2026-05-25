import SwiftUI
import AppKit

// MARK: - Design system
//
// Single source of truth mirrored from the Spoolr web app + the menubar design
// handoff (`reference/connect-menubar.css` :root). Dark theme only — the menubar
// app has no light mode. All values are taken verbatim from the styleguide so the
// two products feel like one brand.

extension Color {
    /// Hex initializer (`"#rrggbb"` or `"rrggbb"`), opaque.
    init(hex: String) {
        let cleaned = hex.hasPrefix("#") ? String(hex.dropFirst()) : hex
        var value: UInt64 = 0
        Scanner(string: cleaned).scanHexInt64(&value)
        let r = Double((value & 0xFF0000) >> 16) / 255
        let g = Double((value & 0x00FF00) >> 8) / 255
        let b = Double(value & 0x0000FF) / 255
        self.init(.sRGB, red: r, green: g, blue: b, opacity: 1)
    }
}

enum Theme {
    // Surfaces
    static let surface   = Color(hex: "#101115")
    static let surface2  = Color(hex: "#14161b")
    static let surface3  = Color(hex: "#1a1c22")
    static let border    = Color(hex: "#22242c")

    // Text
    static let text1 = Color(hex: "#e8e9ee") // primary
    static let text2 = Color(hex: "#95979f") // secondary
    static let text3 = Color(hex: "#5e6069") // tertiary / mono labels
    static let text4 = Color(hex: "#44464d") // offline / disabled

    // Brand / status
    static let accent  = Color(hex: "#22d67a")
    static let warning = Color(hex: "#f5a524")
    static let danger  = Color(hex: "#e5484d")

    /// Text painted on top of an accent-filled surface (buttons, success ring).
    static let onAccent = Color(hex: "#0a1f12")

    // Accent tints
    static let accentGlow = Color.accentToken.opacity(0.06) // soft fills
    static let accentDim  = Color.accentToken.opacity(0.16) // borders
    static let warningGlow = warning.opacity(0.10)
    static let warningDim  = warning.opacity(0.16)

    // Hairlines on the translucent popover
    static let hairline = Color.white.opacity(0.07)
    static let rowHover = Color.white.opacity(0.045)

    // Radii
    static let radiusPopover: CGFloat = 14
    static let radiusInner: CGFloat = 10
    static let radiusRow: CGFloat = 8

    // Fixed popover width (height is content-driven, capped ~640pt).
    static let popoverWidth: CGFloat = 340
    static let popoverMaxHeight: CGFloat = 640

    // Type — Geist / Geist Mono when bundled, otherwise SF Pro / SF Mono
    // (metrically close per the handoff, so no spacing changes needed).
    static func sans(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight)
    }
    static func mono(_ size: CGFloat, weight: Font.Weight = .regular) -> Font {
        .system(size: size, weight: weight, design: .monospaced)
    }
}

private extension Color {
    static let accentToken = Color(hex: "#22d67a")
}
