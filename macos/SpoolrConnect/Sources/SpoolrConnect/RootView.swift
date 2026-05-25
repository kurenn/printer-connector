import SwiftUI

/// Hosts whichever of the five views the state machine selects, on the vibrant
/// popover material. Fixed 340pt width; height is content-driven.
struct RootView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        content
            .frame(width: Theme.popoverWidth)
            .background(
                VisualEffectView()
                    .overlay(Color(.sRGB, red: 0.086, green: 0.090, blue: 0.110, opacity: 0.55))
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
        case .pairing:    PairingView()
        case .justPaired: JustPairedView()
        }
    }
}
