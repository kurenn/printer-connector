import SwiftUI

/// Compact "agent stalled / down" warning shown at the bottom of the popover
/// (just above the FootStrip) whenever `FleetModel.agentHealth != .healthy`.
/// Tone matches the banner family — warning yellow for stalled, danger red
/// for down — and the Restart button kicks the subprocess.
struct AgentHealthRow: View {
    let health: AgentHealth
    var onRestart: () -> Void

    private var tone: Color {
        health == .down ? Theme.danger : Theme.warning
    }

    private var label: String {
        switch health {
        case .down:    return "Agent stopped"
        case .stalled: return "Agent not responding"
        case .healthy: return ""    // never rendered when healthy
        }
    }

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "exclamationmark.triangle.fill")
                .font(.system(size: 11, weight: .semibold))
                .foregroundColor(tone)
            Text(label)
                .font(Theme.sans(11, weight: .semibold))
                .foregroundColor(Theme.text1)
            Spacer(minLength: 4)
            Button(action: onRestart) {
                HStack(spacing: 4) {
                    Image(systemName: "arrow.clockwise")
                        .font(.system(size: 9, weight: .semibold))
                    Text("Restart Agent")
                }
                .font(Theme.sans(11, weight: .semibold))
                .foregroundColor(Theme.onAccent)
                .padding(.horizontal, 9)
                .padding(.vertical, 4)
                .background(tone, in: RoundedRectangle(cornerRadius: 6))
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 8)
        .background(tone.opacity(0.10))
        .overlay(
            Rectangle()
                .fill(tone.opacity(0.18))
                .frame(height: 0.5),
            alignment: .top
        )
    }
}
