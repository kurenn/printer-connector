import Foundation
import Combine

/// One available update — the banner reads from this.
struct UpdateInfo: Equatable {
    let latestVersion: String   // normalized, e.g. "0.6.0"
    let currentVersion: String  // normalized, e.g. "0.5.0"
    let releaseURL: URL
}

/// Polls the GitHub Releases API for newer versions of the connector.
///
/// Cadence: a debounced check on launch (skipped if the last check is younger
/// than `launchDebounce`) plus a repeating `checkInterval` timer while the app
/// is running. Network and parse failures are silent — `availableUpdate` only
/// flips to non-nil on a confirmed newer release.
///
/// Dismissals stick across launches: once the user dismisses v0.6.0, the
/// banner stays hidden until a strictly newer version appears.
@MainActor
final class UpdateChecker: ObservableObject {
    @Published private(set) var availableUpdate: UpdateInfo?

    private let currentVersion: String
    private let repo: String
    private let session: URLSession
    private let defaults: UserDefaults

    static let checkInterval: TimeInterval = 24 * 60 * 60        // 24h
    static let launchDebounce: TimeInterval = 12 * 60 * 60       // 12h

    private var timer: Timer?

    init(currentVersion: String = UpdateChecker.bundledVersion(),
         repo: String = "kurenn/printer-connector",
         session: URLSession = .shared,
         defaults: UserDefaults = .standard) {
        self.currentVersion = UpdateChecker.normalize(currentVersion)
        self.repo = repo
        self.session = session
        self.defaults = defaults
    }

    /// Start the check loop: opportunistic check now (if we're past the debounce),
    /// then every `checkInterval` thereafter.
    func start() {
        if shouldCheckOnLaunch() {
            Task { await self.checkNow() }
        }
        timer?.invalidate()
        timer = Timer.scheduledTimer(withTimeInterval: Self.checkInterval, repeats: true) { [weak self] _ in
            Task { @MainActor [weak self] in await self?.checkNow() }
        }
    }

    /// Force a check immediately (e.g. wired to a "Check now" menu item).
    func checkNow() async {
        guard let url = URL(string: "https://api.github.com/repos/\(repo)/releases/latest") else { return }
        var request = URLRequest(url: url)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        request.setValue("SpoolrConnect/\(currentVersion)", forHTTPHeaderField: "User-Agent")

        do {
            let (data, response) = try await session.data(for: request)
            guard let http = response as? HTTPURLResponse, http.statusCode == 200 else { return }
            defaults.set(Date().timeIntervalSince1970, forKey: Keys.lastCheckedAt)
            guard let parsed = Self.parseRelease(data: data) else { return }
            evaluate(latest: parsed.tag, releaseURL: parsed.url)
        } catch {
            // Network failures are intentionally silent.
        }
    }

    /// User dismissed the banner — hide until a strictly newer version appears.
    func dismiss() {
        if let v = availableUpdate?.latestVersion {
            defaults.set(v, forKey: Keys.dismissedVersion)
        }
        availableUpdate = nil
    }

    // MARK: - Pure helpers (tested)

    /// Normalize a tag/version string: drop a leading `v` and any `-suffix`.
    nonisolated static func normalize(_ raw: String) -> String {
        var v = raw.hasPrefix("v") ? String(raw.dropFirst()) : raw
        if let dash = v.firstIndex(of: "-") {
            v = String(v[..<dash])
        }
        return v
    }

    /// Lexicographic numeric compare. Missing trailing components count as 0
    /// (so "0.5" and "0.5.0" are equal).
    nonisolated static func compare(_ a: String, _ b: String) -> ComparisonResult {
        let aParts = a.split(separator: ".").compactMap { Int($0) }
        let bParts = b.split(separator: ".").compactMap { Int($0) }
        for i in 0..<max(aParts.count, bParts.count) {
            let ai = i < aParts.count ? aParts[i] : 0
            let bi = i < bParts.count ? bParts[i] : 0
            if ai < bi { return .orderedAscending }
            if ai > bi { return .orderedDescending }
        }
        return .orderedSame
    }

    nonisolated static func isNewer(_ candidate: String, than current: String) -> Bool {
        compare(normalize(candidate), normalize(current)) == .orderedDescending
    }

    /// Pull `tag_name` and `html_url` from a GitHub /releases/latest response.
    nonisolated static func parseRelease(data: Data) -> (tag: String, url: URL)? {
        struct Response: Decodable {
            let tag_name: String
            let html_url: String
        }
        guard let resp = try? JSONDecoder().decode(Response.self, from: data),
              let url = URL(string: resp.html_url) else { return nil }
        return (resp.tag_name, url)
    }

    /// The bundle short version from Info.plist. Stamped by build_app.sh from
    /// the git tag in CI; falls back to "dev" during local SwiftPM runs.
    nonisolated static func bundledVersion() -> String {
        (Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String) ?? "dev"
    }

    // MARK: - Private

    private func shouldCheckOnLaunch() -> Bool {
        let last = defaults.double(forKey: Keys.lastCheckedAt)
        if last == 0 { return true }
        return Date().timeIntervalSince1970 - last >= Self.launchDebounce
    }

    private func evaluate(latest rawLatest: String, releaseURL: URL) {
        let latest = Self.normalize(rawLatest)
        // No update if the latest tag isn't strictly newer than what's running.
        guard Self.isNewer(latest, than: currentVersion) else {
            availableUpdate = nil
            return
        }
        // Honor an existing dismissal until a strictly newer version appears.
        if let dismissed = defaults.string(forKey: Keys.dismissedVersion),
           !Self.isNewer(latest, than: dismissed) {
            availableUpdate = nil
            return
        }
        availableUpdate = UpdateInfo(
            latestVersion: latest,
            currentVersion: currentVersion,
            releaseURL: releaseURL
        )
    }

    private enum Keys {
        static let lastCheckedAt = "UpdateChecker.lastCheckedAt"
        static let dismissedVersion = "UpdateChecker.dismissedVersion"
    }
}
