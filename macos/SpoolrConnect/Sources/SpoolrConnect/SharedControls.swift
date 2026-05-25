import SwiftUI

// MARK: - Divider

struct HairlineDivider: View {
    var body: some View {
        Rectangle()
            .fill(Color.white.opacity(0.07))
            .frame(height: 0.5)
            .padding(.horizontal, 12)
            .padding(.vertical, 4)
    }
}

// MARK: - Action row

struct ActionRow: View {
    var symbol: String
    var title: String
    var shortcut: String?
    var primary: Bool = false
    var action: () -> Void = {}

    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: symbol)
                    .font(.system(size: 14))
                    .foregroundColor(primary ? Theme.accent : Theme.text2)
                    .frame(width: 18, height: 18)
                Text(title)
                    .font(Theme.sans(13))
                    .tracking(-0.07)
                    .foregroundColor(primary ? Theme.accent : Theme.text1)
                Spacer(minLength: 6)
                if let shortcut {
                    Text(shortcut)
                        .font(Theme.mono(10.5))
                        .foregroundColor(Theme.text3)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(background)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
        .animation(.easeOut(duration: 0.06), value: hovering)
    }

    @ViewBuilder private var background: some View {
        if primary {
            RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                .fill(Theme.accent.opacity(hovering ? 0.12 : 0.06))
                .overlay(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                    .stroke(Theme.accent.opacity(0.16), lineWidth: 0.5))
        } else {
            RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                .fill(hovering ? Color.white.opacity(0.05) : .clear)
        }
    }
}

// MARK: - Foot strip

struct FootStrip: View {
    var left: String
    var rightLabel: String
    var rightAction: () -> Void = {}

    var body: some View {
        HStack {
            Text(left)
                .font(Theme.mono(10))
                .tracking(0.6)
                .foregroundColor(Theme.text3)
            Spacer()
            Button(action: rightAction) {
                Text(rightLabel)
                    .font(Theme.sans(11))
                    .foregroundColor(Theme.text2)
            }
            .buttonStyle(.plain)
        }
        .padding(.horizontal, 14)
        .padding(.top, 8)
        .padding(.bottom, 10)
        .background(Color.black.opacity(0.18))
        .overlay(Rectangle().fill(Color.white.opacity(0.06)).frame(height: 0.5), alignment: .top)
    }
}

// MARK: - Hero counts (3-up)

struct HeroStats: View {
    var printing: Int
    var idle: Int
    var attention: Int

    var body: some View {
        HStack(spacing: 12) {
            cell("\(printing)", "Printing", Theme.accent)
            cell("\(idle)", "Idle", Theme.text1)
            cell("\(attention)", "Attention", Theme.warning)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 12)
        .background(
            LinearGradient(colors: [Color.white.opacity(0.025), Color.white.opacity(0.005)],
                           startPoint: .top, endPoint: .bottom))
        .clipShape(RoundedRectangle(cornerRadius: Theme.radiusInner, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: Theme.radiusInner, style: .continuous)
            .stroke(Color.white.opacity(0.05), lineWidth: 0.5))
        .padding(.horizontal, 14)
        .padding(.bottom, 8)
    }

    private func cell(_ value: String, _ label: String, _ color: Color) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(value)
                .font(Theme.mono(17, weight: .medium))
                .tracking(-0.3)
                .foregroundColor(color)
            Text(label.uppercased())
                .font(Theme.mono(9))
                .tracking(1.1)
                .foregroundColor(Theme.text3)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Filled accent button (empty / scan CTAs)

struct AccentFillButton: View {
    var title: String
    var action: () -> Void = {}
    @State private var hovering = false

    var body: some View {
        Button(action: action) {
            Text(title)
                .font(Theme.sans(12.5, weight: .semibold))
                .tracking(-0.07)
                .foregroundColor(Theme.onAccent)
                .frame(maxWidth: .infinity)
                .padding(.vertical, 9)
                .background(RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Theme.accent.opacity(hovering ? 0.9 : 1.0)))
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}
