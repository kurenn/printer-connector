import SwiftUI

// MARK: - Brand mark

/// The Spoolr mark: outer ring + concentric filled core. Accent-tinted.
struct SpoolrMark: View {
    var color: Color = Theme.accent
    var size: CGFloat = 16

    var body: some View {
        ZStack {
            Circle().stroke(color, lineWidth: size * 0.125)
            Circle().fill(color).frame(width: size * 0.29, height: size * 0.29)
        }
        .frame(width: size, height: size)
    }
}

/// 26×26 brand chip with a soft accent radial glow on a surface-2 background.
struct BrandChip: View {
    var body: some View {
        SpoolrMark(size: 16)
            .frame(width: 26, height: 26)
            .background(
                RadialGradient(colors: [Theme.accent.opacity(0.18), .clear],
                               center: .init(x: 0.3, y: 0.3), startRadius: 0, endRadius: 18)
                    .background(Theme.surface2)
            )
            .clipShape(RoundedRectangle(cornerRadius: 7, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 7, style: .continuous)
                .stroke(Color.white.opacity(0.08), lineWidth: 0.5))
    }
}

// MARK: - Icons

enum Glyph {
    /// SF Symbol substitutes per driver kind (production swaps in custom art).
    static func symbol(for kind: PrinterKind) -> String {
        switch kind {
        case .bambu:   return "cube.box"
        case .klipper: return "square.stack.3d.up"
        case .printer: return "printer"
        }
    }
}

// MARK: - Pulsing live dot (header badge)

struct PulsingDot: View {
    var color: Color = Theme.accent
    var diameter: CGFloat = 5
    @State private var pulsing = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Circle()
            .fill(color)
            .frame(width: diameter, height: diameter)
            .shadow(color: color.opacity(0.9), radius: 3)
            .scaleEffect(pulsing ? 1.18 : 1.0)
            .opacity(pulsing ? 1.0 : 0.55)
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.easeInOut(duration: 0.9).repeatForever(autoreverses: true)) {
                    pulsing = true
                }
            }
    }
}

// MARK: - Status badge (header pill)

struct StatusBadge: View {
    var text: String
    var tone: Color = Theme.accent

    var body: some View {
        HStack(spacing: 5) {
            PulsingDot(color: tone, diameter: 5)
            Text(text)
                .font(Theme.mono(9.5, weight: .medium))
                .tracking(0.8)
                .foregroundColor(tone)
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 3)
        .background(Capsule().fill(tone.opacity(0.10)))
        .overlay(Capsule().stroke(tone.opacity(0.16), lineWidth: 0.5))
    }
}

// MARK: - Header

struct BrandHeader<Trailing: View>: View {
    var title: String
    var subtitle: String
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack(spacing: 10) {
            BrandChip()
            VStack(alignment: .leading, spacing: 1) {
                Text(title)
                    .font(Theme.sans(13, weight: .semibold))
                    .tracking(-0.13)
                    .foregroundColor(Theme.text1)
                Text(subtitle)
                    .font(Theme.mono(11))
                    .foregroundColor(Theme.text3)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }
            Spacer(minLength: 6)
            trailing()
        }
        .padding(.horizontal, 14)
        .padding(.top, 14)
        .padding(.bottom, 10)
    }
}

// MARK: - Section label

struct SectionLabel<Trailing: View>: View {
    var text: String
    var tone: Color = Theme.text3
    @ViewBuilder var trailing: () -> Trailing

    var body: some View {
        HStack {
            Text(text.uppercased())
                .font(Theme.mono(9.5))
                .tracking(1.3)
                .foregroundColor(tone)
            Spacer()
            trailing()
        }
        .padding(.horizontal, 16)
        .padding(.vertical, 4)
        .padding(.top, 4)
    }
}

extension SectionLabel where Trailing == EmptyView {
    init(_ text: String, tone: Color = Theme.text3) {
        self.init(text: text, tone: tone) { EmptyView() }
    }
}

// MARK: - Mini progress bar (printing rows)

struct MiniProgressBar: View {
    var progress: Double // 0...1

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(Color.white.opacity(0.08))
                Capsule()
                    .fill(LinearGradient(
                        colors: [Theme.accent, Theme.accent.opacity(0.85)],
                        startPoint: .leading, endPoint: .trailing))
                    .frame(width: max(0, min(1, progress)) * geo.size.width)
            }
        }
        .frame(height: 3)
    }
}

// MARK: - Printer row

struct PrinterRowView: View {
    var printer: Printer

    private var hasProgress: Bool { printer.state == .printing && printer.progress != nil }

    var body: some View {
        VStack(spacing: 7) {
            HStack(alignment: hasProgress ? .top : .center, spacing: 10) {
                icon
                VStack(alignment: .leading, spacing: 2) {
                    Text(printer.name)
                        .font(Theme.sans(13, weight: .medium))
                        .foregroundColor(Theme.text1)
                        .lineLimit(1)
                    subline
                }
                Spacer(minLength: 6)
                trailing
            }
            if hasProgress { progressBlock }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, hasProgress ? 9 : 7)
        .background(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
            .fill(Color.clear))
        .contentShape(Rectangle())
    }

    private var icon: some View {
        ZStack(alignment: .bottomTrailing) {
            Image(systemName: Glyph.symbol(for: printer.kind))
                .font(.system(size: 13, weight: .regular))
                .foregroundColor(Theme.text2)
                .frame(width: 18, height: 18)
            Circle()
                .fill(stateDotColor)
                .frame(width: 7, height: 7)
                .overlay(Circle().stroke(Theme.surface, lineWidth: 1.5))
                .shadow(color: stateGlow, radius: stateGlow == .clear ? 0 : 3)
                .offset(x: 1, y: 1)
        }
        .frame(width: 18, height: 18)
    }

    private var subline: some View {
        Group {
            switch printer.state {
            case .printing:
                if let p = printer.progress {
                    (Text((printer.job ?? "Printing") + " · ")
                        .foregroundColor(Theme.text3)
                     + Text("\(Int((p * 100).rounded()))%").foregroundColor(Theme.accent))
                } else {
                    (Text("PRINTING").foregroundColor(Theme.accent)
                     + Text(printer.job.map { " · \($0)" } ?? "").foregroundColor(Theme.text3))
                }
            case .error:
                (Text("ERROR").foregroundColor(Theme.danger)
                 + Text(" · \(printer.error ?? "fault")").foregroundColor(Theme.text3))
            case .idle:
                Text("Idle · \(printer.temp ?? "ready")").foregroundColor(Theme.text3)
            case .offline:
                Text("Offline · last seen \(printer.lastSeen ?? "—")").foregroundColor(Theme.text4)
            }
        }
        .font(Theme.mono(11))
        .lineLimit(1)
    }

    @ViewBuilder private var trailing: some View {
        switch printer.state {
        case .printing:
            Text(printer.eta ?? "")
                .font(Theme.mono(10.5))
                .foregroundColor(Theme.text2)
        case .error:
            Text("Resolve").font(Theme.sans(11)).foregroundColor(Theme.text2)
        case .idle:
            Text(printer.temp ?? "").font(Theme.mono(10.5)).foregroundColor(Theme.text3)
        case .offline:
            Text("Retry").font(Theme.sans(11)).foregroundColor(Theme.text2)
        }
    }

    private var progressBlock: some View {
        VStack(spacing: 4) {
            MiniProgressBar(progress: printer.progress ?? 0)
            HStack {
                (Text("Layer ").foregroundColor(Theme.text3)
                 + Text(printer.layer ?? "—").foregroundColor(Theme.text1))
                Spacer()
                Text("\(Int(((printer.progress ?? 0) * 100).rounded()))%").foregroundColor(Theme.accent)
                Spacer()
                (Text(printer.eta ?? "—").foregroundColor(Theme.text1)
                 + Text(" left").foregroundColor(Theme.text3))
            }
            .font(Theme.mono(9.5))
            .tracking(0.4)
        }
        .padding(.leading, 28) // align under the row body, past the 18px icon + gap
    }

    private var stateDotColor: Color {
        switch printer.state {
        case .printing: return Theme.accent
        case .idle:     return Theme.text3
        case .error:    return Theme.danger
        case .offline:  return Theme.text4
        }
    }
    private var stateGlow: Color {
        switch printer.state {
        case .printing: return Theme.accent
        case .error:    return Theme.danger
        default:        return .clear
        }
    }
}

// MARK: - Hover background wrapper (rows / actions)

struct HoverHighlight<Content: View>: View {
    var cornerRadius: CGFloat = Theme.radiusRow
    @ViewBuilder var content: () -> Content
    @State private var hovering = false

    var body: some View {
        content()
            .background(RoundedRectangle(cornerRadius: cornerRadius, style: .continuous)
                .fill(hovering ? Theme.rowHover : .clear))
            .onHover { hovering = $0 }
            .animation(.easeOut(duration: 0.06), value: hovering)
    }
}
