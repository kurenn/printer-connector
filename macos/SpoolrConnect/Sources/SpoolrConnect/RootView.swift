import SwiftUI
import AppKit

/// Hosts whichever of the five views the state machine selects, on the vibrant
/// popover material. Fixed 340pt width; height is content-driven.
struct RootView: View {
    @EnvironmentObject var model: FleetModel
    @EnvironmentObject var updateChecker: UpdateChecker

    var body: some View {
        VStack(spacing: 0) {
            if let info = updateChecker.availableUpdate {
                UpdateBanner(
                    info: info,
                    onDownload: { NSWorkspace.shared.open(info.releaseURL) },
                    onDismiss: { updateChecker.dismiss() }
                )
            }
            content
        }
            .frame(width: Theme.popoverWidth)
            .background(
                // Opaque, top-lit dark panel to match the design. The 0.55-alpha
                // tint used before let the desktop bleed through the blur, washing
                // the whole popover out. We keep VisualEffectView underneath only
                // for its rounded-corner mask + window shadow, and paint a solid
                // gradient over it (lighter at the top, near-black at the bottom).
                VisualEffectView()
                    .overlay(
                        LinearGradient(
                            colors: [Color(hex: "#15171c"), Color(hex: "#0c0d11")],
                            startPoint: .top, endPoint: .bottom
                        )
                    )
            )
            .overlay(
                // top edge highlight (CSS .mb::before)
                LinearGradient(colors: [.clear, Color.white.opacity(0.08), .clear],
                               startPoint: .leading, endPoint: .trailing)
                    .frame(height: 1).padding(.horizontal, 12),
                alignment: .top
            )
            .environment(\.colorScheme, .dark)
    }

    @ViewBuilder private var content: some View {
        switch model.state {
        case .attention:  AttentionModeView()
        case .empty:      EmptyStateView()
        case .tokenEntry: TokenEntryView()
        case .scanning:   ScanningView()
        case .linking:    LinkingView()
        case .bambuCredentials: BambuCredentialsView()
        case .justPaired: JustPairedView()
        }
    }
}
