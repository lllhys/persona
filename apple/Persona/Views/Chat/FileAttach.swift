import SwiftUI
import UniformTypeIdentifiers

// Sending files to an agent (docs/protocol/protocol.md「Sending files」): a chat's composer holds picked files as cards
// above the box (`FileAttachments`, #8361), uploaded to the machine the conversation runs on, and the message carries
// each as a `<persona-file kind="file">` tag after what was typed; nothing goes into the text. `FileUploads` and
// `PathInsert` remain for what still wants paths as words: Health's reports, New Agent's first message, the share sheet.

/// Paths as words (Health's reports, New Agent's first message, the share sheet) and the composer's cursor as offsets.
/// Offsets are UTF-16 (what TextSelection converts to and from cleanly).
nonisolated enum PathInsert {
    /// A path with spaces or quotes is single-quoted, so the agent reads it as one path.
    static func quoted(_ path: String) -> String {
        guard path.contains(where: { $0.isWhitespace || $0 == "'" || $0 == "\"" }) else { return path }
        return "'" + path.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// The selection as UTF-16 offsets into text, nil when there is none.
    static func range(_ selection: TextSelection?, in text: String) -> Range<Int>? {
        guard let selection else { return nil }
        let r: Range<String.Index>
        switch selection.indices {
        case .selection(let s): r = s
        case .multiSelection(let set): guard let first = set.ranges.first else { return nil }; r = first
        @unknown default: return nil
        }
        // The selection may come from an earlier text (the draft changed since): an index past the
        // end traps in utf16Offset, so clamp it first.
        let offset = { (i: String.Index) in min(i, text.endIndex).utf16Offset(in: text) }
        let lo = offset(r.lowerBound), hi = offset(r.upperBound)
        return lo..<max(lo, hi)
    }
}

@MainActor @Observable
final class FileUploads {
    static let limit: Int64 = 200 << 20

    struct Item: Identifiable {
        let id = UUID()
        var name: String
        var size: Int64
        var sent: Int64 = 0
        var waiting = true // behind another upload
    }

    private(set) var items: [Item] = []
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]
    @ObservationIgnored private var last: Task<Void, Never>?

    /// Queues picked files for machine; done gets each one's path on the machine, in the order picked.
    /// dest "health": a report for the health space, kept in its reports/ (docs/assistant/health.md「Checkup reports」); nil the temp directory.
    func start(_ urls: [URL], machine: MachineRef, model: AppModel, dest: String? = nil, done: @escaping (String) -> Void) {
        let features = model.features(machine.group)
        if !features.isEmpty, !features.contains("file_upload") {
            model.toast = Toast(String(localized: "The hub is too old to receive files. Update the hub and try again."), style: .error)
            return
        }
        for url in urls {
            let scoped = url.startAccessingSecurityScopedResource()
            let size = Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
            if size > Self.limit {
                if scoped { url.stopAccessingSecurityScopedResource() }
                model.toast = Toast(String(localized: "“\(url.lastPathComponent)” is \(size.storageText), over the 200 MB limit, and can't be sent"), style: .error)
                continue
            }
            let item = Item(name: url.lastPathComponent, size: size)
            items.append(item)
            let prev = last
            let task = Task { [weak self] in
                defer { if scoped { url.stopAccessingSecurityScopedResource() } }
                await prev?.value
                guard let self, !Task.isCancelled else { return }
                await self.run(item.id, url: url, machine: machine, model: model, dest: dest, done: done)
            }
            tasks[item.id] = task
            last = task
        }
    }

    /// `start` for files wanted together: done gets their paths once every upload has ended (a failed or cancelled one
    /// is left out; nothing got through, no call). It runs even if the view that started them has gone.
    func startAll(_ urls: [URL], machine: MachineRef, model: AppModel, dest: String? = nil, done: @escaping ([String]) -> Void) {
        let got = Collected()
        start(urls, machine: machine, model: model, dest: dest) { got.paths.append($0) }
        let ending = last
        Task {
            await ending?.value
            if !got.paths.isEmpty { done(got.paths) }
        }
    }

    private final class Collected { var paths: [String] = [] }

    func cancel(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        items.removeAll { $0.id == id }
    }

    private func run(_ id: UUID, url: URL, machine: MachineRef, model: AppModel, dest: String?, done: (String) -> Void) async {
        defer {
            tasks[id] = nil
            items.removeAll { $0.id == id }
        }
        update(id) { $0.waiting = false }
        guard let api = model.api(machine.group) else { return }
        let name = url.lastPathComponent
        do {
            let staged = try await Self.stage(url)
            defer { try? FileManager.default.removeItem(at: staged.deletingLastPathComponent()) }
            update(id) { $0.size = (try? staged.resourceValues(forKeys: [.fileSizeKey]).fileSize).map(Int64.init) ?? $0.size }
            let out = try await api.upload(file: staged, name: name, machine: machine.id, dest: dest) { sent in
                Task { @MainActor [weak self] in self?.update(id) { $0.sent = max($0.sent, sent) } }
            }
            guard !Task.isCancelled else { return }
            Haptics.tap()
            done(out.path)
        } catch {
            if let msg = Self.message(error, name: name) { model.toast = Toast(msg, style: .error) }
        }
    }

    private func update(_ id: UUID, _ change: (inout Item) -> Void) {
        if let i = items.firstIndex(where: { $0.id == id }) { change(&items[i]) }
    }

    /// A private copy to upload from: the picker's URL may be an iCloud placeholder or lose its access mid-upload.
    nonisolated static func stage(_ url: URL, directory: URL? = nil) async throws -> URL {
        try await Task.detached {
            let dir = directory ?? FileManager.default.temporaryDirectory.appending(path: "uploads/" + UUID().uuidString)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let dest = dir.appending(path: url.lastPathComponent)
            var coordError: NSError?
            var copyError: Error?
            NSFileCoordinator().coordinate(readingItemAt: url, options: .withoutChanges, error: &coordError) { src in
                do { try FileManager.default.copyItem(at: src, to: dest) } catch { copyError = error }
            }
            if let e = coordError ?? copyError {
                try? FileManager.default.removeItem(at: dir)
                throw e
            }
            return dest
        }.value
    }

    /// What to tell the user when a file did not make it; nil when they cancelled it.
    nonisolated static func message(_ error: Error, name: String) -> String? {
        if error.isCancellation { return nil }
        if let e = error as? APIError {
            switch e.code {
            case "too_large": return String(localized: "“\(name)” is over 200 MB and can't be sent")
            case "node_outdated": return e.message
            default: break
            }
            if e.status == 404 { return String(localized: "The hub is too old to receive files. Update the hub and try again.") }
        }
        return String(localized: "Couldn't send “\(name)”: \(error.localizedDescription)")
    }
}

/// The uploads in flight above the input box: name, progress, cancel.
struct UploadsBar: View {
    var uploads: FileUploads

    var body: some View {
        VStack(spacing: Theme.Space.sm) {
            ForEach(uploads.items) { item in UploadRow(item: item, uploads: uploads) }
        }
    }
}

/// One upload in flight: name, progress, cancel. `boxed` as in `AttachmentRow`.
struct UploadRow: View {
    var item: FileUploads.Item
    var uploads: FileUploads
    var boxed = true

    var body: some View {
        AttachmentRow(symbol: Theme.Icon.file, name: item.name,
                      status: item.waiting ? String(localized: "Waiting [File Attach 26c8cf]") : "\(item.sent.storageText) / \(item.size.storageText)",
                      progress: item.size > 0 ? Double(min(item.sent, item.size)) / Double(item.size) : 0,
                      removeLabel: String(localized: "Cancel upload of \(item.name)"), boxed: boxed) { uploads.cancel(item.id) }
    }
}

/// One row above the input box (a file uploading, a video on its way or ready): icon, name, status, the progress bar
/// while there is one (nil: none), and the button that cancels or removes it. `boxed`: on a surface of its own, as over
/// an input box; off, a form's row (New Agent's attachments, #2321).
struct AttachmentRow: View {
    var symbol: String
    var name: String
    var status: String
    var progress: Double?
    var removeLabel: String
    var boxed = true
    var remove: () -> Void

    var body: some View {
        if boxed {
            row
                .padding(.horizontal, Theme.Space.m)
                .padding(.vertical, Theme.Space.s)
                .surface(in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        } else {
            row
        }
    }

    private var row: some View {
        HStack(spacing: Theme.Space.m) {
            ThemedIcon(symbol).font(Theme.Font.body).foregroundStyle(.secondary)
            VStack(alignment: .leading, spacing: Theme.Space.xs) {
                HStack {
                    Text(name).font(Theme.Font.secondary).lineLimit(1).truncationMode(.middle)
                    Spacer(minLength: 8)
                    Text(status).font(Theme.Font.label.monospacedDigit()).foregroundStyle(.secondary).lineLimit(1)
                }
                if let progress { ProgressView(value: progress).tint(Theme.accent) }
            }
            Button(action: remove) {
                ThemedIcon(Theme.Icon.clear).font(Theme.Font.title).symbolRenderingMode(.hierarchical)
                    .foregroundStyle(.secondary)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(removeLabel)
        }
    }
}

extension View {
    /// The Files picker of a composer: any kind of file, several at once.
    func fileUploadPicker(isPresented: Binding<Bool>, picked: @escaping ([URL]) -> Void) -> some View {
        fileImporter(isPresented: isPresented, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            if case .success(let urls) = result, !urls.isEmpty { picked(urls) }
        }
    }
}

/// A chat composer's files (#8361, docs/protocol/protocol.md「Sending files」): each copied out of the picker's sandbox,
/// uploaded to the conversation's machine `together` at a time (kept there for good where the hub can,
/// `upload_attachment`), then held as a card above the box until the message goes with it as a `<persona-file
/// kind="file">` tag. A failed one stays as a card to try again or remove; nothing is ever typed into the text.
@MainActor @Observable
final class FileAttachments {
    /// How many files one message takes.
    static let most = 10
    /// How many upload at once; the others wait their turn.
    static let together = 3

    struct Item: Identifiable {
        enum Step: Equatable { case waiting, uploading, ready, failed }
        let id = UUID()
        var name: String
        var size: Int64
        var sent: Int64 = 0
        var step = Step.waiting
        /// Why it did not get there (`failed`), for VoiceOver; the toast said it once.
        var problem: String?
        /// Set once it is on the machine.
        var file: AttachedFile?
    }

    /// Where one item comes from and goes: the picked file (its security scope open until it is copied, or the app's
    /// own copy when `owned`), and the machine and upload destination.
    private struct Job {
        var url: URL
        var scoped: Bool
        var owned: Bool
        let machine: MachineRef
        var dest: String?
        let model: AppModel
    }

    private(set) var items: [Item] = []
    @ObservationIgnored private var jobs: [UUID: Job] = [:]
    /// Each item's private copy (`FileUploads.stage`), kept until it is on the machine so a failed one can go again.
    @ObservationIgnored private var copies: [UUID: URL] = [:]
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]

    /// The files on the machine, in the order picked: what the message carries.
    var ready: [AttachedFile] { items.compactMap(\.file) }
    /// One still on its way: the message waits for it.
    var busy: Bool { items.contains { $0.step == .waiting || $0.step == .uploading } }

    /// Files picked for machine. `dest`: the composer's own destination (a health conversation's reports), else kept
    /// with the chat's pictures where the hub knows how (`upload_attachment`), else the temp directory as before.
    /// `owned`: the app's own copies, each in a directory of its own (a scan's PDF, DEBUG files), deleted with it once
    /// on the machine or removed.
    func start(_ urls: [URL], machine: MachineRef, model: AppModel, dest: String? = nil, owned: Bool = false) {
        let features = model.features(machine.group)
        if !features.isEmpty, !features.contains("file_upload") {
            model.toast = Toast(String(localized: "The hub is too old to receive files. Update the hub and try again."), style: .error)
            if owned { urls.forEach(Self.discardOwned) }
            return
        }
        let dest = Self.destination(dest, features: features)
        let room = max(0, Self.most - items.count)
        if urls.count > room {
            model.toast = Toast(String(localized: "Up to \(Self.most) files in one message"), style: .error)
            if owned { urls.dropFirst(room).forEach(Self.discardOwned) }
        }
        for url in urls.prefix(room) {
            let scoped = !owned && url.startAccessingSecurityScopedResource()
            let item = Item(name: url.lastPathComponent, size: 0) // its size is read off the main actor (`run`)
            items.append(item)
            jobs[item.id] = Job(url: url, scoped: scoped, owned: owned, machine: machine, dest: dest, model: model)
        }
        pump()
    }

    /// Where a file goes: the composer's own destination (a health conversation's reports), else kept with the chat's
    /// pictures where the hub knows how (`upload_attachment`) or may (its features not in yet: an older hub refuses it
    /// and the file goes again without, `refusedDest`), else the temp directory (nil) as before.
    nonisolated static func destination(_ own: String?, features: Set<String>) -> String? {
        own ?? (features.isEmpty || features.contains("upload_attachment") ? "attachment" : nil)
    }

    /// An older hub refused dest=attachment (400 "unknown dest"): the file goes to the temp directory instead.
    nonisolated static func refusedDest(_ error: Error) -> Bool {
        guard let e = error as? APIError else { return false }
        return e.status == 400 && e.message.contains("unknown dest")
    }

    /// Cancels one on its way, or takes it off the message.
    func remove(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        items.removeAll { $0.id == id }
        drop(id)
        pump()
    }

    /// A failed one goes again, from the copy already made.
    func retry(_ id: UUID) {
        guard let i = items.firstIndex(where: { $0.id == id }), items[i].step == .failed else { return }
        items[i].step = .waiting
        items[i].sent = 0
        items[i].problem = nil
        pump()
    }

    /// The message went with the ready ones; a failed one stays to try again or remove.
    func sent() {
        for item in items where item.file != nil { drop(item.id) }
        items.removeAll { $0.file != nil }
    }

    /// Files already on the machine, ready cards again (a queued message taken back to edit, #3022), as many as fit;
    /// returns the ones that did not.
    @discardableResult func restore(_ files: [AttachedFile]) -> [AttachedFile] {
        let room = max(0, Self.most - items.count)
        for f in files.prefix(room) {
            items.append(Item(name: f.name, size: f.size, sent: f.size, step: .ready, file: f))
        }
        return Array(files.dropFirst(room))
    }

    /// Starts the next waiting ones while fewer than `together` are on their way.
    private func pump() {
        while tasks.count < Self.together,
              let next = items.first(where: { $0.step == .waiting && tasks[$0.id] == nil && jobs[$0.id] != nil }) {
            let id = next.id
            update(id) { $0.step = .uploading }
            tasks[id] = Task { [weak self] in
                await self?.run(id)
                self?.tasks[id] = nil
                self?.pump()
            }
        }
    }

    private func run(_ id: UUID) async {
        guard var job = jobs[id], let name = items.first(where: { $0.id == id })?.name else { return }
        do {
            let copy: URL
            if let made = copies[id] {
                copy = made
            } else {
                defer { // the picked file is needed only until it is copied
                    if jobs[id]?.scoped == true { job.url.stopAccessingSecurityScopedResource(); jobs[id]?.scoped = false }
                }
                let picked = await Self.size(of: job.url)
                if picked > FileUploads.limit {
                    let size = picked.storageText
                    job.model.toast = Toast(String(localized: "“\(name)” is \(size), over the 200 MB limit, and can't be sent"), style: .error)
                    items.removeAll { $0.id == id }
                    drop(id)
                    return
                }
                if picked > 0 { update(id) { $0.size = picked } }
                if job.owned {
                    copy = job.url
                } else {
                    copy = try await FileUploads.stage(job.url)
                }
                guard jobs[id] != nil else { // removed while it was being copied
                    if !job.owned { try? FileManager.default.removeItem(at: copy.deletingLastPathComponent()) }
                    return
                }
                copies[id] = copy
            }
            try Task.checkCancellation()
            let size = await Self.size(of: copy)
            if size > 0 { update(id) { $0.size = size } }
            guard let api = job.model.api(job.machine.group) else {
                throw APIError(status: 0, message: String(localized: "Not connected to the hub [App Model 93d955]"))
            }
            let progress: @Sendable (Int64) -> Void = { sent in
                Task { @MainActor [weak self] in self?.update(id) { if $0.step == .uploading { $0.sent = max($0.sent, sent) } } }
            }
            let out: APIClient.UploadedFile
            do {
                out = try await api.upload(file: copy, name: name, machine: job.machine.id, dest: job.dest, progress: progress)
            } catch let refused where job.dest == "attachment" && Self.refusedDest(refused) {
                job.dest = nil
                jobs[id] = job
                out = try await api.upload(file: copy, name: name, machine: job.machine.id, dest: nil, progress: progress)
            }
            try Task.checkCancellation()
            guard jobs[id] != nil else { return }
            update(id) {
                $0.step = .ready
                $0.file = AttachedFile(kind: "file", path: out.path, name: name, size: out.size > 0 ? out.size : $0.size)
            }
            drop(id) // on the machine: the copy and the picked file are no longer needed
            if !busy { Haptics.tap() }
        } catch {
            guard jobs[id] != nil, !Task.isCancelled else { return } // removed
            let message = FileUploads.message(error, name: name)
            update(id) { $0.step = .failed; $0.sent = 0; $0.problem = message }
            if let message { job.model.toast = Toast(message, style: .error) }
        }
    }

    /// Forgets an item's job: its picked file's scope closed, its copy (and an owned original) deleted.
    private func drop(_ id: UUID) {
        let job = jobs.removeValue(forKey: id), copy = copies.removeValue(forKey: id)
        if let job, job.scoped { job.url.stopAccessingSecurityScopedResource() }
        if let job, job.owned {
            Self.discardOwned(job.url) // the copy is the file itself
        } else if let copy {
            try? FileManager.default.removeItem(at: copy.deletingLastPathComponent()) // FileUploads.stage's own folder
        }
    }

    /// A file's size, read off the main actor (docs/apple/performance.md P1); 0 when it can't be read.
    nonisolated static func size(of url: URL) async -> Int64 {
        await Task.detached { Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0) }.value
    }

    /// The app's own copy of a file, with the directory made for it.
    private static func discardOwned(_ url: URL) {
        try? FileManager.default.removeItem(at: url.deletingLastPathComponent())
    }

    private func update(_ id: UUID, _ change: (inout Item) -> Void) {
        if let i = items.firstIndex(where: { $0.id == id }) { change(&items[i]) }
    }
}

/// The files above the input box (#8361): a card each in a row that scrolls sideways, as the photos' strip does.
struct FileAttachmentsBar: View {
    var files: FileAttachments

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Theme.Space.sm) {
                ForEach(files.items) { item in FileAttachmentCard(item: item, files: files) }
            }
            .padding(.top, Theme.Space.sm) // room for the ✕ above each card, as over a photo
            .padding(.trailing, Theme.Space.xs)
        }
        .accessibilityIdentifier("file-attachments")
    }
}

/// One file above the input box: its kind's mark, name and size (or how far it got, waiting, failed), a thin bar
/// while it uploads, and ✕ to cancel or remove it; a tap on a failed one tries again.
struct FileAttachmentCard: View {
    var item: FileAttachments.Item
    var files: FileAttachments

    private var failed: Bool { item.step == .failed }

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            ZStack {
                RoundedRectangle(cornerRadius: Theme.Radius.badge, style: .continuous)
                    .fill((failed ? Theme.danger : Theme.accent).opacity(0.14))
                ThemedIcon(failed ? Theme.Icon.error : FileViewer.symbol(for: item.name)).font(Theme.Font.secondary)
                    .foregroundStyle(failed ? Theme.danger : Theme.accent)
            }
            .scaledFrame(width: Theme.Size.well, height: Theme.Size.well)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(item.name).font(Theme.Font.secondary).lineLimit(1).truncationMode(.middle)
                Text(Self.status(item)).font(Theme.Font.label.monospacedDigit())
                    .foregroundStyle(failed ? Theme.danger : .secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Theme.Space.s)
        .scaledFrame(width: Theme.Size.box160, height: Theme.Size.box56)
        .background(Theme.Fill.well, in: .rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .overlay(alignment: .bottom) {
            if let progress = Self.progress(item) {
                ProgressView(value: progress).tint(Theme.accent)
                    .padding(.horizontal, Theme.Space.s)
                    .padding(.bottom, Theme.Space.xxs)
            }
        }
        .contentShape(.rect(cornerRadius: Theme.Radius.card, style: .continuous))
        .onTapGesture { if failed { files.retry(item.id) } }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(String(localized: "File \(item.name)"))
        .accessibilityValue(item.problem.map { Self.status(item) + ", " + $0 } ?? Self.status(item))
        .accessibilityAddTraits(failed ? .isButton : [])
        .accessibilityHint(failed ? String(localized: "Try again") : "")
        .accessibilityAction(named: item.file == nil && !failed ? String(localized: "Cancel upload of \(item.name)")
                                                                : String(localized: "Remove \(item.name) [Beautiful U I Bridge 271f20]")) {
            files.remove(item.id)
        }
        .overlay(alignment: .topTrailing) {
            Button { files.remove(item.id) } label: {
                ThemedIcon(Theme.Icon.clear).symbolRenderingMode(.palette).foregroundStyle(.white, .black.opacity(0.6))
                    .tapTargetOutset(.all) // drawn small at the corner, hit over the card around it
            }
            .offset(x: 5, y: -5)
            .accessibilityLabel(item.file == nil && !failed ? String(localized: "Cancel upload of \(item.name)")
                                                            : String(localized: "Remove \(item.name) [Beautiful U I Bridge 271f20]"))
        }
        .accessibilityIdentifier(item.file != nil ? "file-attachment-ready" : failed ? "file-attachment-failed" : "file-attachment-busy")
    }

    /// "1.3 MB / 2.4 MB" on its way, the size once there, Waiting behind others, Failed · Tap to Retry.
    static func status(_ item: FileAttachments.Item) -> String {
        switch item.step {
        case .waiting: String(localized: "Waiting [File Attach 26c8cf]")
        case .uploading: item.size > 0 ? "\(item.sent.storageText) / \(item.size.storageText)" : item.sent.storageText
        case .ready: item.size.storageText
        case .failed: String(localized: "Failed · Tap to Retry")
        }
    }

    /// The bar's 0…1 while it waits or uploads; nil once it is there or failed (no bar).
    static func progress(_ item: FileAttachments.Item) -> Double? {
        switch item.step {
        case .waiting: 0
        case .uploading: item.size > 0 ? Double(min(item.sent, item.size)) / Double(item.size) : 0
        case .ready, .failed: nil
        }
    }
}
