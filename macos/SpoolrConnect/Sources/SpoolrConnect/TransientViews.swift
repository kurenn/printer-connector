import SwiftUI

// MARK: - Cancel / back affordance used in transient headers

private struct HeaderLink: View {
    var label: String
    var action: () -> Void
    var body: some View {
        Button(action: action) { Text(label).font(Theme.sans(11)).foregroundColor(Theme.text2) }
            .buttonStyle(.plain)
    }
}

// MARK: - State 1 · Empty (first run)

struct EmptyStateView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Spoolr Connect", subtitle: "\(model.workspace).spoolr.io") {
                StatusBadge(text: "NO PRINTERS", tone: Theme.warning)
            }
            VStack(spacing: 0) {
                RippleArt().padding(.top, 4).padding(.bottom, 14)
                Text("No printers yet")
                    .font(Theme.sans(14, weight: .medium)).foregroundColor(Theme.text1)
                Text("Your agent is running and linked. Power on a printer\non this network — Connect will find it automatically.")
                    .font(Theme.sans(12)).foregroundColor(Theme.text3)
                    .multilineTextAlignment(.center).lineSpacing(2).padding(.top, 5)
                VStack(spacing: 6) {
                    AccentFillButton(title: "Enter pairing code") { model.showTokenEntry() }
                    Button("Scan this network instead") { model.beginScan() }.buttonStyle(.plain)
                        .font(Theme.sans(11.5)).foregroundColor(Theme.text2).padding(.vertical, 7)
                }
                .padding(.top, 16)
            }
            .padding(.horizontal, 22).padding(.vertical, 18)

            FootStrip(left: "v\(model.version) · listening", rightLabel: "Help")
        }
    }
}

private struct RippleArt: View {
    @State private var animate = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        ZStack {
            Circle().fill(RadialGradient(colors: [Theme.accent.opacity(0.10), .clear],
                                         center: .center, startRadius: 0, endRadius: 30))
            Circle().stroke(Theme.accent.opacity(0.16), lineWidth: 0.5)
            Circle().stroke(Theme.accent.opacity(0.10), lineWidth: 0.5)
                .scaleEffect(animate ? 1.35 : 0.6)
                .opacity(animate ? 0 : 1)
            Image(systemName: "magnifyingglass")
                .font(.system(size: 22, weight: .regular)).foregroundColor(Theme.accent)
        }
        .frame(width: 60, height: 60)
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeOut(duration: 2.6).repeatForever(autoreverses: false)) { animate = true }
        }
    }
}

// MARK: - State 2 · Scanning (discovery)

struct ScanningView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Add printer",
                        subtitle: "Scanning \(model.workspace).local · 192.168.1.0/24") {
                HeaderLink(label: "Cancel") { model.backToAttention() }
            }

            HStack(spacing: 10) {
                ScanPulse()
                (Text("Probing mDNS · ").foregroundColor(Theme.text2)
                 + Text("\(model.scanProbed)").foregroundColor(Theme.text1)
                 + Text(" / ").foregroundColor(Theme.text2)
                 + Text("\(model.scanTotal)").foregroundColor(Theme.text1)
                 + Text(" hosts").foregroundColor(Theme.text2))
                    .font(Theme.mono(10.5)).tracking(0.4)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12).padding(.vertical, 9)
            .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Color.white.opacity(0.025)))
            .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous).stroke(Color.white.opacity(0.05), lineWidth: 0.5))
            .padding(.horizontal, 14).padding(.top, 6)

            SectionLabel("Discovered · \(model.discovered.filter { $0.status == .discovered }.count)")
            VStack(spacing: 4) {
                ForEach(model.discovered) { d in ScanRow(printer: d) { model.beginPairing(d) } }
            }
            .padding(.horizontal, 6).padding(.bottom, 6)

            HairlineDivider()
            VStack(spacing: 0) {
                ActionRow(symbol: "network", title: "Add by IP address…", shortcut: "⌘I")
                ActionRow(symbol: "arrow.clockwise", title: "Rescan network", shortcut: "⌘R") { model.beginScan() }
            }
            .padding(.horizontal, 6).padding(.bottom, 8).padding(.top, 2)

            FootStrip(left: "DISCOVERY · 0:14", rightLabel: "Done") { model.backToAttention() }
        }
    }
}

private struct ScanPulse: View {
    @State private var animate = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        ZStack {
            Circle().stroke(Theme.accent.opacity(0.5), lineWidth: 1)
                .frame(width: 8, height: 8)
                .scaleEffect(animate ? 2.3 : 1).opacity(animate ? 0 : 1)
            Circle().fill(Theme.accent).frame(width: 8, height: 8)
        }
        .frame(width: 8, height: 8)
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeOut(duration: 1.4).repeatForever(autoreverses: false)) { animate = true }
        }
    }
}

private struct ScanRow: View {
    var printer: DiscoveredPrinter
    var onPair: () -> Void
    private var discovered: Bool { printer.status == .discovered }

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: Glyph.symbol(for: printer.kind))
                .font(.system(size: 13)).foregroundColor(discovered ? Theme.accent : Theme.text3)
                .frame(width: 18, height: 18)
            VStack(alignment: .leading, spacing: 2) {
                Text(printer.name).font(Theme.sans(13, weight: .medium)).foregroundColor(Theme.text1).lineLimit(1)
                Text(printer.detail).font(Theme.mono(11)).foregroundColor(Theme.text3).lineLimit(1)
            }
            Spacer(minLength: 6)
            if discovered {
                Button(action: onPair) {
                    Text("Pair").font(Theme.sans(11, weight: .semibold)).foregroundColor(Theme.onAccent)
                        .padding(.horizontal, 10).padding(.vertical, 4)
                        .background(RoundedRectangle(cornerRadius: 5, style: .continuous).fill(Theme.accent))
                }.buttonStyle(.plain)
            } else {
                Text("CHECKING").font(Theme.mono(9.5)).tracking(1.1).foregroundColor(Theme.text3)
            }
        }
        .padding(.horizontal, 10).padding(.vertical, 8)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous)
            .fill(discovered ? Theme.accent.opacity(0.06) : .clear))
        .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous)
            .stroke(discovered ? Theme.accent.opacity(0.16) : .clear, lineWidth: 0.5))
    }
}

// MARK: - State 3 · Pairing handshake

struct PairingView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Pairing…",
                        subtitle: "Negotiating with \(model.pairingTarget?.host ?? "")") {
                HeaderLink(label: "Cancel") { model.backToAttention() }
            }

            VStack(spacing: 0) {
                PairTrio(kind: model.pairingTarget?.kind ?? .printer)
                Text(model.pairingTarget?.name ?? "")
                    .font(Theme.sans(13.5, weight: .medium)).foregroundColor(Theme.text1)
                Text("\(model.pairingTarget?.detail ?? "")")
                    .font(Theme.mono(11)).foregroundColor(Theme.text3).padding(.top, 3).lineLimit(1)

                VStack(spacing: 7) {
                    ForEach(model.pairSteps) { PairStepRow(step: $0) }
                }
                .padding(.top, 14)
                .overlay(Rectangle().fill(Color.white.opacity(0.06)).frame(height: 0.5), alignment: .top)
                .padding(.top, 12)
            }
            .padding(.horizontal, 16).padding(.top, 18).padding(.bottom, 14)
            .background(LinearGradient(colors: [Theme.accent.opacity(0.04), Color.white.opacity(0.01)],
                                       startPoint: .top, endPoint: .bottom))
            .clipShape(RoundedRectangle(cornerRadius: Theme.radiusInner, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: Theme.radiusInner, style: .continuous)
                .stroke(Theme.accent.opacity(0.16), lineWidth: 0.5))
            .padding(.horizontal, 12).padding(.top, 4).padding(.bottom, 8)

            FootStrip(left: "PAIRING · 1 of 1", rightLabel: "")
        }
        // Demonstrate the flow: handshake completes after a short beat.
        .task {
            try? await Task.sleep(nanoseconds: 2_300_000_000)
            if case .pairing = model.state { model.completePairing() }
        }
    }
}

private struct PairTrio: View {
    var kind: PrinterKind
    var body: some View {
        HStack(spacing: 8) {
            node(agent: true) { SpoolrMark(size: 18) }
            PairDots()
            node(agent: false) { Image(systemName: Glyph.symbol(for: kind)).font(.system(size: 18)).foregroundColor(Theme.text2) }
        }
        .frame(maxWidth: 200)
        .padding(.bottom, 12)
    }

    private func node<Content: View>(agent: Bool, @ViewBuilder _ content: () -> Content) -> some View {
        content()
            .frame(width: 36, height: 36)
            .background(
                agent
                    ? AnyView(RadialGradient(colors: [Theme.accent.opacity(0.2), .clear],
                                             center: .init(x: 0.35, y: 0.3), startRadius: 0, endRadius: 24)
                        .background(Theme.surface2))
                    : AnyView(Theme.surface2)
            )
            .clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: 9, style: .continuous).stroke(Color.white.opacity(0.07), lineWidth: 0.5))
    }
}

private struct PairDots: View {
    @State private var animate = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        HStack(spacing: 5) {
            ForEach(0..<3, id: \.self) { i in
                Circle().fill(Theme.accent)
                    .frame(width: 5, height: 5)
                    .opacity(animate ? 1 : 0.25)
                    .scaleEffect(animate ? 1.3 : 1)
                    .animation(reduceMotion ? nil :
                        .easeInOut(duration: 1.2).repeatForever().delay(Double(i) * 0.2), value: animate)
            }
        }
        .frame(maxWidth: .infinity)
        .onAppear { animate = true }
    }
}

private struct PairStepRow: View {
    var step: PairStep
    var body: some View {
        HStack(spacing: 9) {
            mark.frame(width: 16, height: 16)
            Text(step.label).font(Theme.sans(11.5)).tracking(-0.07).foregroundColor(labelColor)
            Spacer(minLength: 4)
            if let t = step.time { Text(t).font(Theme.mono(10)).foregroundColor(Theme.text3) }
        }
    }

    @ViewBuilder private var mark: some View {
        switch step.state {
        case .done:
            ZStack {
                Circle().fill(Theme.accent)
                Image(systemName: "checkmark").font(.system(size: 8, weight: .bold)).foregroundColor(Theme.onAccent)
            }
        case .active:
            ZStack {
                Circle().fill(Theme.accent.opacity(0.06)).overlay(Circle().stroke(Theme.accent, lineWidth: 0.5))
                Spinner()
            }
        case .pending:
            Circle().fill(Theme.surface3).overlay(Circle().stroke(Color.white.opacity(0.06), lineWidth: 0.5))
                .overlay(Circle().fill(Theme.text4).frame(width: 4, height: 4))
        }
    }

    private var labelColor: Color {
        switch step.state {
        case .done:    return Theme.text1
        case .active:  return Theme.accent
        case .pending: return Theme.text3
        }
    }
}

private struct Spinner: View {
    @State private var spin = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        Circle()
            .trim(from: 0, to: 0.75)
            .stroke(Theme.accent, style: StrokeStyle(lineWidth: 1.4, lineCap: .round))
            .frame(width: 9, height: 9)
            .rotationEffect(.degrees(spin ? 360 : 0))
            .onAppear {
                guard !reduceMotion else { return }
                withAnimation(.linear(duration: 0.85).repeatForever(autoreverses: false)) { spin = true }
            }
    }
}

// MARK: - State 4 · Just paired (success)

struct JustPairedView: View {
    @EnvironmentObject var model: FleetModel

    private var linked: [Printer] { model.linkedPrinters }
    private var count: Int { max(linked.count, model.justPairedPrinter == nil ? 0 : 1) }

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Spoolr Connect", subtitle: "\(model.workspace).spoolr.io") {
                StatusBadge(text: "\(max(count, 1)) LINKED")
            }

            VStack(spacing: 0) {
                SuccessRing().padding(.bottom, 10)
                Text("Connected.").font(Theme.sans(15, weight: .semibold)).foregroundColor(Theme.text1)
                Text(subtitle)
                    .font(Theme.sans(11.5)).foregroundColor(Theme.text3)
                    .multilineTextAlignment(.center).lineSpacing(2).padding(.top, 4)
            }
            .padding(.horizontal, 22).padding(.top, 18).padding(.bottom, 14)
            .overlay(Rectangle().fill(Color.white.opacity(0.06)).frame(height: 0.5), alignment: .bottom)

            ScrollView {
                VStack(spacing: 4) {
                    ForEach(rows) { p in
                        PrinterRowView(printer: p)
                            .background(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                                .fill(Theme.accent.opacity(0.04)))
                            .overlay(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                                .stroke(Theme.accent.opacity(0.16), lineWidth: 0.5))
                    }
                }
                .padding(.horizontal, 12).padding(.top, 6)
            }
            .frame(maxHeight: 180)

            HairlineDivider()
            VStack(spacing: 0) {
                ActionRow(symbol: "globe", title: "Open dashboard", shortcut: "⌘D", primary: true)
                ActionRow(symbol: "plus", title: "Add more printers", shortcut: "⌘N") { model.showTokenEntry() }
            }
            .padding(.horizontal, 6).padding(.bottom, 8).padding(.top, 2)

            FootStrip(left: "v\(model.version) · linked", rightLabel: "Done") { model.dismissJustPaired() }
        }
        // Auto-return to Attention Mode after ~6s (handoff §behavior).
        .task {
            try? await Task.sleep(nanoseconds: 6_000_000_000)
            if case .justPaired = model.state { model.dismissJustPaired() }
        }
    }

    private var subtitle: String {
        if linked.isEmpty {
            return "\(model.justPairedPrinter?.name ?? "Printer") is online and pushing telemetry to the dashboard."
        }
        let n = linked.count
        return "\(n) printer\(n == 1 ? "" : "s") linked to \(model.workspace).spoolr.io and pushing telemetry."
    }

    private var rows: [Printer] {
        if !linked.isEmpty { return linked }
        if let p = model.justPairedPrinter { return [p] }
        return []
    }
}

// MARK: - Token entry (paste code → register everything)

struct TokenEntryView: View {
    @EnvironmentObject var model: FleetModel
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Add printers", subtitle: "Paste your pairing code") {
                HeaderLink(label: "Cancel") { model.backToAttention() }
            }

            VStack(spacing: 0) {
                ZStack {
                    Circle().fill(RadialGradient(colors: [Theme.accent.opacity(0.10), .clear],
                                                 center: .center, startRadius: 0, endRadius: 28))
                    Image(systemName: "link").font(.system(size: 22)).foregroundColor(Theme.accent)
                }
                .frame(width: 54, height: 54).padding(.bottom, 12)

                Text("Link this network").font(Theme.sans(14, weight: .medium)).foregroundColor(Theme.text1)
                Text("Paste the pairing code from your Spoolr dashboard. Connect finds every printer on this network and links them all.")
                    .font(Theme.sans(12)).foregroundColor(Theme.text3)
                    .multilineTextAlignment(.center).lineSpacing(2).padding(.top, 5)

                TextField("pairing code", text: $model.token)
                    .textFieldStyle(.plain)
                    .font(Theme.mono(13))
                    .foregroundColor(Theme.text1)
                    .multilineTextAlignment(.center)
                    .padding(.vertical, 9).padding(.horizontal, 12)
                    .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(Theme.surface2))
                    .overlay(RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .stroke(focused ? Theme.accent.opacity(0.5) : Color.white.opacity(0.08), lineWidth: 0.5))
                    .focused($focused)
                    .onSubmit { model.register() }
                    .padding(.top, 14)

                if let err = model.linkError {
                    Text(err).font(Theme.sans(11)).foregroundColor(Theme.danger)
                        .multilineTextAlignment(.center).padding(.top, 8)
                }

                AccentFillButton(title: "Connect") { model.register() }.padding(.top, 10)
                Button("Scan this network instead") { model.beginScan() }.buttonStyle(.plain)
                    .font(Theme.sans(11.5)).foregroundColor(Theme.text2).padding(.vertical, 7)
            }
            .padding(.horizontal, 22).padding(.top, 14).padding(.bottom, 16)

            FootStrip(left: "v\(model.version) · listening", rightLabel: "Help")
        }
        .onAppear { focused = true }
    }
}

// MARK: - Linking (discovering + registering all under a token)

struct LinkingView: View {
    @EnvironmentObject var model: FleetModel

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Connecting…", subtitle: "Linking to \(model.workspace).spoolr.io") {
                EmptyView()
            }

            VStack(spacing: 14) {
                ZStack {
                    Circle().fill(RadialGradient(colors: [Theme.accent.opacity(0.10), .clear],
                                                 center: .center, startRadius: 0, endRadius: 30))
                        .frame(width: 60, height: 60)
                    ProgressView().controlSize(.large).tint(Theme.accent)
                }
                VStack(spacing: 4) {
                    Text("Discovering & registering printers")
                        .font(Theme.sans(13, weight: .medium)).foregroundColor(Theme.text1)
                    Text("Scanning your network and linking every printer it finds. This can take a few seconds.")
                        .font(Theme.sans(11.5)).foregroundColor(Theme.text3)
                        .multilineTextAlignment(.center).lineSpacing(2)
                }
            }
            .padding(.horizontal, 24).padding(.top, 22).padding(.bottom, 20)

            FootStrip(left: "LINKING…", rightLabel: "")
        }
    }
}

private struct SuccessRing: View {
    @State private var popped = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    var body: some View {
        ZStack {
            Circle().fill(Theme.accent)
            Image(systemName: "checkmark").font(.system(size: 20, weight: .bold)).foregroundColor(Theme.onAccent)
        }
        .frame(width: 46, height: 46)
        .shadow(color: Theme.accent.opacity(0.35), radius: 10)
        .overlay(Circle().stroke(Theme.accent.opacity(0.06), lineWidth: 6))
        .scaleEffect(popped ? 1 : (reduceMotion ? 1 : 0.4))
        .opacity(popped ? 1 : (reduceMotion ? 1 : 0))
        .onAppear {
            withAnimation(.spring(response: 0.38, dampingFraction: 0.55)) { popped = true }
        }
    }
}

// MARK: - Bambu credentials (access code per discovered Bambu printer)

struct BambuCredentialsView: View {
    @EnvironmentObject var model: FleetModel

    private var ready: Bool { model.bambuDiscovered.allSatisfy { !$0.accessCode.isEmpty } }

    var body: some View {
        VStack(spacing: 0) {
            BrandHeader(title: "Bambu printers", subtitle: "Enter each access code") {
                HeaderLink(label: "Cancel") { model.backToAttention() }
            }

            Text("Found \(model.bambuDiscovered.count) Bambu printer\(model.bambuDiscovered.count == 1 ? "" : "s"). The LAN access code is on the printer screen → Settings → Network.")
                .font(Theme.sans(11.5)).foregroundColor(Theme.text3)
                .multilineTextAlignment(.center).lineSpacing(2)
                .padding(.horizontal, 22).padding(.top, 4).padding(.bottom, 10)

            ScrollView {
                VStack(spacing: 8) {
                    ForEach($model.bambuDiscovered) { $device in
                        VStack(alignment: .leading, spacing: 6) {
                            HStack(spacing: 8) {
                                Image(systemName: "cube.box").font(.system(size: 13)).foregroundColor(Theme.accent)
                                    .frame(width: 18, height: 18)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(device.name).font(Theme.sans(13, weight: .medium)).foregroundColor(Theme.text1)
                                    Text("\(device.model) · \(device.host)").font(Theme.mono(11)).foregroundColor(Theme.text3)
                                }
                            }
                            TextField("access code", text: $device.accessCode)
                                .textFieldStyle(.plain).font(Theme.mono(13)).foregroundColor(Theme.text1)
                                .padding(.vertical, 7).padding(.horizontal, 10)
                                .background(RoundedRectangle(cornerRadius: 7, style: .continuous).fill(Theme.surface2))
                                .overlay(RoundedRectangle(cornerRadius: 7, style: .continuous)
                                    .stroke(Color.white.opacity(0.08), lineWidth: 0.5))
                        }
                        .padding(10)
                        .background(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous).fill(Theme.accent.opacity(0.04)))
                        .overlay(RoundedRectangle(cornerRadius: Theme.radiusRow, style: .continuous)
                            .stroke(Theme.accent.opacity(0.16), lineWidth: 0.5))
                    }
                }
                .padding(.horizontal, 12)
            }
            .frame(maxHeight: 240)

            AccentFillButton(title: "Link printers") { model.confirmBambuAndRegister() }
                .padding(.horizontal, 22).padding(.top, 12).padding(.bottom, 16)
                .opacity(ready ? 1 : 0.5)
                .disabled(!ready)

            FootStrip(left: "v\(model.version) · pairing", rightLabel: "")
        }
    }
}
