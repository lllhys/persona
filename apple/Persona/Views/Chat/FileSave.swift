#if os(iOS)
import Photos
import SwiftUI
import UIKit

// Download in the file viewer (#5826): the file from the computer onto this phone. A tap saves a picture or a video
// to Photos and anything else through the system's Save to Files; a long press offers the other ways and Share.
// The bytes come from what the viewer already holds (a decoded picture, a played video, a QuickLook copy) or from
// `/file/download`, into a temporary folder of their own that goes once the file is saved, shared or let go.

/// Where Download puts a file.
nonisolated enum FileSaveTarget: Sendable { case photos, files, share }

/// What Photos takes, by name: still pictures and the video containers it plays. Anything else (SVG, WebM, MKV…)
/// goes to Files.
nonisolated enum FileSaveKind {
    static let pictures: Set<String> = ["jpg", "jpeg", "png", "gif", "heic", "heif", "tif", "tiff", "webp"]
    static let videos: Set<String> = ["mov", "mp4", "m4v"]

    static func photosResource(_ name: String) -> PHAssetResourceType? {
        let ext = (name as NSString).pathExtension.lowercased()
        if pictures.contains(ext) { return .photo }
        if videos.contains(ext) { return .video }
        return nil
    }

    /// A tap on Download: Photos for what it takes, Files for the rest.
    static func defaultTarget(_ name: String) -> FileSaveTarget { photosResource(name) == nil ? .files : .photos }

    /// The Files app opened on a saved file (`shareddocuments://` and its path).
    static func filesAppURL(_ saved: URL) -> URL? {
        let path = saved.path(percentEncoded: false)
        guard !path.isEmpty, let encoded = path.addingPercentEncoding(withAllowedCharacters: .urlPathAllowed) else { return nil }
        return URL(string: "shareddocuments://" + encoded)
    }
}

/// A copy of the file on this phone waiting for the share sheet.
struct SharedFile: Identifiable {
    let id = UUID()
    let url: URL
}

@MainActor @Observable
final class FileSaveModel {
    /// The download under way, for the bar under the viewer.
    struct Progress: Equatable {
        var name: String
        var done: Int64
        var total: Int64
    }

    private(set) var progress: Progress?
    /// A copy waiting for Save to Files (`fileMover` moves it out of its folder).
    var exporting: URL?
    var sharing: SharedFile?
    @ObservationIgnored private var task: Task<Void, Never>?

    var busy: Bool { progress != nil || exporting != nil || sharing != nil || task != nil }

    /// Saves the file at ref. `local`: a copy the viewer already has; `base64`: its bytes in memory (a picture shown).
    func save(_ target: FileSaveTarget, ref: FileRef, name: String, size: Int64, local: URL?, base64: String?, model: AppModel) {
        guard task == nil else { return }
        let api = model.api(ref.group)
        task = Task { [weak self] in
            defer { self?.task = nil; self?.progress = nil }
            do {
                guard let self else { return }
                let url = try await self.copy(ref: ref, name: name, size: size, local: local, base64: base64, api: api)
                try Task.checkCancellation()
                self.progress = nil
                switch target {
                case .photos: await Self.saveToPhotos(url, name: name, model: model)
                case .files: self.exporting = url
                case .share: self.sharing = SharedFile(url: url)
                }
            } catch {
                guard !error.isCancellation else { return }
                Haptics.error()
                model.toast = Toast(String(localized: "Couldn’t download “\(name)”: \(error.localizedDescription)"), style: .error)
            }
        }
    }

    /// Stops a download under way (the bar's ✕).
    func cancel() {
        task?.cancel()
        task = nil
        progress = nil
    }

    /// Save to Files is over: saved (the toast opens it in the Files app) or cancelled. Either way the copy's folder goes.
    func exported(_ result: Result<URL, Error>?, model: AppModel) {
        guard let url = exporting else { return }
        exporting = nil
        MachineFileDownload.remove(url)
        switch result {
        case .success(let saved)?:
            Haptics.success()
            let open = FileSaveKind.filesAppURL(saved)
            model.toast = Toast(String(localized: "Saved to Files"), style: .success, action: open.map { link in
                Toast.Action(title: String(localized: "Open")) { UIApplication.shared.open(link) }
            })
        case .failure(let error)?:
            if !error.isCancellation { model.toast = Toast(error.localizedDescription, style: .error) }
        case nil:
            break
        }
    }

    /// The share sheet closed.
    func shared() {
        if let url = sharing?.url { MachineFileDownload.remove(url) }
        sharing = nil
    }

    /// A copy of its own in a fresh temporary folder: copied or written off the main thread, or downloaded.
    private func copy(ref: FileRef, name: String, size: Int64, local: URL?, base64: String?, api: APIClient?) async throws -> URL {
        if local != nil || base64 != nil {
            let dir = FileManager.default.temporaryDirectory.appendingPathComponent("persona-save-" + UUID().uuidString, isDirectory: true)
            let url = dir.appendingPathComponent((name as NSString).lastPathComponent)
            try await Task.detached(priority: .userInitiated) {
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                if let local {
                    try FileManager.default.copyItem(at: local, to: url)
                } else if let base64, let data = Data(base64Encoded: base64) {
                    try data.write(to: url)
                } else {
                    throw CocoaError(.fileReadCorruptFile)
                }
            }.value
            return url
        }
        guard let api else { throw URLError(.notConnectedToInternet) }
        progress = Progress(name: name, done: 0, total: size)
        return try await MachineFileDownload.fetch(ref, path: ref.path, name: name, api: api, prefix: "persona-save-") { [weak self] done, total in
            self?.progress = Progress(name: name, done: done, total: total > 0 ? total : size)
        }
    }

    private static func saveToPhotos(_ url: URL, name: String, model: AppModel) async {
        defer { MachineFileDownload.remove(url) }
        guard let type = FileSaveKind.photosResource(name) else { return }
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            Haptics.error()
            model.toast = Toast(String(localized: "No access to Photos. In Settings › Persona › Photos, allow “Add Photos Only”."), style: .error)
            return
        }
        do {
            // `@Sendable`: Photos runs the block on its own queue, where main-actor code is a crash (test_apple_background_callbacks.py).
            try await PHPhotoLibrary.shared().performChanges { @Sendable in
                let options = PHAssetResourceCreationOptions()
                options.originalFilename = name
                PHAssetCreationRequest.forAsset().addResource(with: type, fileURL: url, options: options)
            }
            Haptics.success()
            let photos = URL(string: "photos-redirect://")
            model.toast = Toast(String(localized: "Saved to Photos"), style: .success, action: photos.map { link in
                Toast.Action(title: String(localized: "Open")) { UIApplication.shared.open(link) }
            })
        } catch {
            Haptics.error()
            if !error.isCancellation {
                model.toast = Toast(String(localized: "Couldn’t save to Photos: \(error.localizedDescription). Touch and hold Download to save it to Files."), style: .error)
            }
        }
    }
}

/// The system share sheet for a file on this phone; done runs once it closes (it dismisses itself after sharing).
struct FileShareSheet: UIViewControllerRepresentable {
    let url: URL
    let done: @MainActor @Sendable () -> Void

    func makeUIViewController(context: Context) -> UIActivityViewController {
        let sheet = UIActivityViewController(activityItems: [url], applicationActivities: nil)
        let done = done
        sheet.completionWithItemsHandler = { @Sendable _, _, _, _ in Task { @MainActor in done() } }
        return sheet
    }

    func updateUIViewController(_ controller: UIActivityViewController, context: Context) {}
}
#endif
