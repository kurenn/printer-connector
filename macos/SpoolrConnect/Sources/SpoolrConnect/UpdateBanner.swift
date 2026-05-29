import SwiftUI
import AppKit

/// Compact "Update available" strip shown at the top of the popover when
/// `UpdateChecker.availableUpdate` is non-nil. "Download" opens the GitHub
/// release page; "Dismiss" hides the banner until a strictly newer version
/// appears.
struct UpdateBanner: View {
    let info: UpdateInfo
    var onDownload: () -> Void
    var onDismiss: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundColor(Theme.warning)
                Text("Update available")
                    .font(Theme.sans(12, weight: .semibold))
                    .foregroundColor(Theme.text1)
                Spacer(minLength: 4)
                Text("v\(info.currentVersion) → v\(info.latestVersion)")
                    .font(Theme.mono(10.5))
                    .foregroundColor(Theme.text3)
            }

            HStack(spacing: 6) {
                Button(action: onDownload) {
                    HStack(spacing: 4) {
                        Text("Download")
                        Image(systemName: "arrow.up.right")
                            .font(.system(size: 9, weight: .semibold))
                    }
                    .font(Theme.sans(11, weight: .semibold))
                    .foregroundColor(Theme.onAccent)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 5)
                    .background(Theme.warning, in: RoundedRectangle(cornerRadius: 6))
                }
                .buttonStyle(.plain)

                Button("Dismiss", action: onDismiss)
                    .font(Theme.sans(11))
                    .foregroundColor(Theme.text2)
                    .buttonStyle(.plain)

                Spacer()
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 10)
        .background(Theme.warningGlow)
        .overlay(
            Rectangle()
                .fill(Theme.warningDim)
                .frame(height: 0.5),
            alignment: .bottom
        )
    }
}
