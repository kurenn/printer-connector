import SwiftUI
import AppKit

/// Wraps `NSVisualEffectView` so the popover sits on a vibrant material rather
/// than a flat color, matching the reference's `rgba(22,23,28,0.78)` + blur.
/// The caller layers a tint overlay on top (see `RootView`).
struct VisualEffectView: NSViewRepresentable {
    var material: NSVisualEffectView.Material = .hudWindow
    var blendingMode: NSVisualEffectView.BlendingMode = .behindWindow

    func makeNSView(context: Context) -> NSVisualEffectView {
        let view = NSVisualEffectView()
        view.material = material
        view.blendingMode = blendingMode
        view.state = .active
        return view
    }

    func updateNSView(_ nsView: NSVisualEffectView, context: Context) {
        nsView.material = material
        nsView.blendingMode = blendingMode
        nsView.state = .active
    }
}
