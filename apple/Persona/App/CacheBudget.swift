import Foundation
import WebKit

/// Keeps the caches that grow with use under a ceiling (#8341). Nothing else trims them: chat pictures and Feishu media
/// are kept for good once downloaded, and WebKit sizes its own network cache from the phone's free space (760 MB after
/// 16 days on one phone, mostly links `WebViewPool` preloaded and nobody opened). Runs once per process, when the app
/// first goes to the background; everything removed is fetched again when it is shown.
nonisolated enum CacheBudget {
    /// A folder whose files are trimmed oldest first: over `limit`, down to `target`, so a phone just over the line is
    /// not trimmed on every visit to the background.
    struct Folder: Sendable {
        let name: String
        let limit: Int64
        let target: Int64
    }

    static let folders = [
        Folder(name: "chat-images", limit: 200 << 20, target: 150 << 20),
        Folder(name: "lark-media", limit: 200 << 20, target: 150 << 20),
    ]
    /// Over this, WebKit's disk cache is cleared (it has no public API to trim part of it).
    static let webLimit: Int64 = 200 << 20

    @MainActor private static var ran = false

    /// The first time the app goes to the background in this process: trims the folders off the main thread, then
    /// clears WebKit's disk cache if it is over its limit. Cookies, local storage and logins stay.
    @MainActor static func runOnce() {
        guard !ran else { return }
        ran = true
        Task {
            let web = await Task.detached(priority: .utility) {
                for f in folders { trim(URL.cachesDirectory.appending(path: f.name), limit: f.limit, target: f.target) }
                return webCacheSize()
            }.value
            guard web > webLimit else { return }
            let types: Set<String> = [WKWebsiteDataTypeDiskCache, WKWebsiteDataTypeFetchCache]
            await WKWebsiteDataStore.default().removeData(ofTypes: types, modifiedSince: .distantPast)
        }
    }

    /// Removes the files of `dir` with the oldest modification dates until at most `target` bytes are left, when it
    /// holds more than `limit`. Returns the bytes removed.
    @discardableResult static func trim(_ dir: URL, limit: Int64, target: Int64) -> Int64 {
        let keys: Set<URLResourceKey> = [.contentModificationDateKey, .totalFileAllocatedSizeKey, .fileAllocatedSizeKey, .isRegularFileKey]
        guard let e = FileManager.default.enumerator(at: dir, includingPropertiesForKeys: Array(keys)) else { return 0 }
        var files: [(url: URL, date: Date, size: Int64)] = []
        var total: Int64 = 0
        for case let url as URL in e {
            guard let v = try? url.resourceValues(forKeys: keys), v.isRegularFile == true else { continue }
            let size = Int64(v.totalFileAllocatedSize ?? v.fileAllocatedSize ?? 0)
            files.append((url, v.contentModificationDate ?? .distantPast, size))
            total += size
        }
        guard total > limit else { return 0 }
        var removed: Int64 = 0
        for f in files.sorted(by: { $0.date < $1.date }) where total - removed > target {
            if (try? FileManager.default.removeItem(at: f.url)) != nil { removed += f.size }
        }
        return removed
    }

    /// WebKit's network cache on disk: Caches/WebKit on a phone, Caches/<bundle id>/WebKit where a build keeps it there.
    static func webCacheSize() -> Int64 {
        let caches = URL.cachesDirectory
        var dirs = [caches.appending(path: "WebKit")]
        if let id = Bundle.main.bundleIdentifier { dirs.append(caches.appending(path: id).appending(path: "WebKit")) }
        return dirs.reduce(0) { $0 + StorageUsage.size(of: $1) }
    }
}
