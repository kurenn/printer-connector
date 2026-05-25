import SwiftUI

/// Home screen (locked direction — "Attention Mode"). Surfaces only what needs
/// action: hero counts, errors inline, active prints with progress, and quiet
/// groups (idle / offline) collapsed by default.
struct AttentionModeView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Spoolr Connect",
                        subtitle: "\(model.workspace).spoolr.io · \(model.linkedCount) printers") {
                StatusBadge(text: "\(model.printing.count) LIVE")
            }

            HeroStats(printing: model.printing.count,
                      idle: model.idle.count,
                      attention: model.errored.count)

            ScrollView {
                VStack(spacing: 0) {
                    if !model.errored.isEmpty {
                        SectionLabel("Needs attention · \(model.errored.count)", tone: Theme.warning)
                        rows(Array(model.errored.prefix(3)))
                    }

                    SectionLabel(text: "Printing · \(model.printing.count)") {
                        Button("Show all") {}.buttonStyle(.plain)
                            .font(Theme.sans(11)).foregroundColor(Theme.text2)
                    }
                    rows(Array(model.printing.prefix(5)))

                    GroupToggle(title: "Idle · \(model.idle.count)",
                                pill: "all ready",
                                expanded: $model.idleExpanded)
                    if model.idleExpanded {
                        rows(Array(model.idle.prefix(6)))
                        if model.idle.count > 6 {
                            ShowMore(label: "+ \(model.idle.count - 6) more idle")
                        }
                    }

                    if !model.offline.isEmpty {
                        GroupToggle(title: "Offline · \(model.offline.count)",
                                    pill: nil,
                                    expanded: $model.offlineExpanded)
                        if model.offlineExpanded { rows(model.offline) }
                    }
                }
            }
            .frame(maxHeight: 320)

            HairlineDivider()
            VStack(spacing: 0) {
                ActionRow(symbol: "plus", title: "Add printer…", shortcut: "⌘N", primary: true) {
                    model.beginScan()
                }
                ActionRow(symbol: "globe", title: "Open dashboard", shortcut: "⌘D")
            }
            .padding(.horizontal, 6)
            .padding(.bottom, 8)
            .padding(.top, 2)

            FootStrip(left: "v\(model.version) · \(model.linkedCount) linked", rightLabel: "Quit") {
                NSApplication.shared.terminate(nil)
            }
        }
    }

    private func rows(_ printers: [Printer]) -> some View {
        VStack(spacing: 0) {
            ForEach(printers) { p in
                HoverHighlight { PrinterRowView(printer: p) }
            }
        }
        .padding(.horizontal, 6)
        .padding(.bottom, 6)
        .padding(.top, 2)
    }
}

// MARK: - Group toggle (collapsed quiet sections)

struct GroupToggle: View {
    var title: String
    var pill: String?
    @Binding var expanded: Bool
    @State private var hovering = false

    var body: some View {
        Button {
            withAnimation(.easeInOut(duration: 0.2)) { expanded.toggle() }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "chevron.right")
                    .font(.system(size: 9, weight: .semibold))
                    .foregroundColor(Theme.text3)
                    .rotationEffect(.degrees(expanded ? 90 : 0))
                Text(title)
                    .font(Theme.mono(9.5))
                    .tracking(1.3)
                    .foregroundColor(Theme.text3)
                Spacer()
                if let pill {
                    Text(pill)
                        .font(Theme.mono(9))
                        .foregroundColor(Theme.text3)
                        .padding(.horizontal, 7).padding(.vertical, 2)
                        .background(Capsule().fill(Color.white.opacity(0.05)))
                }
            }
            .textCase(.uppercase)
            .padding(.horizontal, 14)
            .padding(.vertical, 7)
            .background(hovering ? Color.white.opacity(0.04) : .clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { hovering = $0 }
    }
}

struct ShowMore: View {
    var label: String
    var body: some View {
        Button(label) {}.buttonStyle(.plain)
            .font(Theme.sans(11))
            .foregroundColor(Theme.text2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, 16).padding(.vertical, 6)
    }
}
