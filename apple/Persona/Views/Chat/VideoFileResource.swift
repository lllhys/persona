import AVFoundation
import Foundation
import UniformTypeIdentifiers

/// A bounded response from the authenticated file endpoint, never a whole-file fallback.
nonisolated struct VideoFileRange: Sendable {
    let range: Range<Int64>
    let total: Int64
    let revision: String
    let mime: String
    let data: Data
}

nonisolated struct VideoFileSource: Sendable {
    typealias Read = @Sendable (Range<Int64>, String?) async throws -> VideoFileRange
    let read: Read
    var close: @Sendable () -> Void = {}

    init(read: @escaping Read, close: @escaping @Sendable () -> Void = {}) {
        self.read = read
        self.close = close
    }

    init(api: APIClient, ref: FileRef, file: FileContent) {
        let configuration = api.session.configuration
        configuration.urlCache = nil
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        let session = URLSession(configuration: configuration)
        let url = api.url("/api/v1/machines/\(ref.machineID)/file/download",
                          query: ["path": file.path, "cwd": ref.cwd ?? ""])
        let config = api.config
        self.init(read: { range, revision in
            var request = URLRequest(url: url, timeoutInterval: 60)
            config.authorize(&request)
            request.setValue("bytes=\(range.lowerBound)-\(range.upperBound - 1)", forHTTPHeaderField: "Range")
            request.setValue("identity", forHTTPHeaderField: "Accept-Encoding")
            // If-Range would turn a changed file into a complete 200 response instead of rejecting it.
            if let revision { request.setValue(revision, forHTTPHeaderField: "If-Match") }
            let (bytes, response) = try await session.bytes(for: request, delegate: VideoFileRedirectGuard())
            defer { bytes.task.cancel() }
            return try await withTaskCancellationHandler {
                guard let response = response as? HTTPURLResponse else { throw Self.invalidResponse }
                guard response.statusCode == 206 else {
                    if (200..<300).contains(response.statusCode) { throw Self.invalidResponse }
                    var data = Data()
                    for try await byte in bytes {
                        try Task.checkCancellation()
                        data.append(byte)
                        if data.count >= 64 << 10 { break }
                    }
                    throw APIError(status: response.statusCode,
                                   message: response.statusCode == 412 ? String(localized: "The video file changed. Close it and open it again.") :
                                    (APIError.message(in: data) ?? HTTPURLResponse.localizedString(forStatusCode: response.statusCode)),
                                   fields: APIError.fields(in: data), requestID: response.value(forHTTPHeaderField: "X-Request-Id"))
                }
                let metadata = try Self.metadata(response, requested: range, revision: revision)
                var data = Data()
                data.reserveCapacity(Int(metadata.range.count))
                for try await byte in bytes {
                    try Task.checkCancellation()
                    guard data.count < metadata.range.count else { throw Self.invalidResponse }
                    data.append(byte)
                }
                try Task.checkCancellation()
                guard data.count == metadata.range.count else { throw Self.invalidResponse }
                return VideoFileRange(range: metadata.range, total: metadata.total, revision: metadata.revision,
                                      mime: metadata.mime, data: data)
            } onCancel: { bytes.task.cancel() }
        }, close: { session.invalidateAndCancel() })
    }

    static var invalidResponse: APIError { APIError(status: 0, message: String(localized: "Couldn't read the whole video segment, or the server doesn't support streaming. Try again or update the hub.")) }

    static func metadata(_ response: HTTPURLResponse, requested: Range<Int64>, revision: String?) throws -> VideoFileRange {
        guard let header = response.value(forHTTPHeaderField: "Content-Range"), header.hasPrefix("bytes "),
              let etag = response.value(forHTTPHeaderField: "ETag"), etag.hasPrefix("\""), etag.hasSuffix("\""),
              revision == nil || revision == etag else { throw invalidResponse }
        let parts = header.dropFirst(6).split(separator: "/")
        guard parts.count == 2, let total = Int64(parts[1]), total > 0 else { throw invalidResponse }
        let bounds = parts[0].split(separator: "-")
        guard bounds.count == 2, let start = Int64(bounds[0]), let last = Int64(bounds[1]),
              start == requested.lowerBound, last >= start, last < total,
              last == min(requested.upperBound, total) - 1 else { throw invalidResponse }
        let range = start..<(last + 1)
        if response.expectedContentLength >= 0, response.expectedContentLength != range.count { throw invalidResponse }
        return VideoFileRange(range: range, total: total, revision: etag,
                              mime: response.mimeType ?? "application/octet-stream", data: Data())
    }
}

/// File reads must stay at the configured hub; never forward its bearer token through a redirect.
nonisolated private final class VideoFileRedirectGuard: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

/// Sparse disk cache. The lock protects file offsets and close; no network await holds the lock.
nonisolated final class VideoFileCache: @unchecked Sendable {
    static let chunkBytes: Int64 = 256 << 10
    let url: URL
    let info: VideoFileRange
    private let source: VideoFileSource
    private let progress: @Sendable (Int64, Int64) -> Void
    private let lock = NSLock()
    private let handle: FileHandle
    private var chunks: Set<Int64> = []
    private var received: Int64 = 0
    private var closed = false

    init(source: VideoFileSource, info: VideoFileRange, name: String,
         progress: @escaping @Sendable (Int64, Int64) -> Void = { _, _ in }) throws {
        self.source = source
        self.info = info
        self.progress = progress
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("persona-video-" + UUID().uuidString, isDirectory: true)
        url = directory.appendingPathComponent((name as NSString).lastPathComponent)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        do {
            guard FileManager.default.createFile(atPath: url.path, contents: nil) else { throw CocoaError(.fileWriteUnknown) }
            handle = try FileHandle(forUpdating: url)
        } catch {
            try? FileManager.default.removeItem(at: directory)
            throw error
        }
    }

    func data(at offset: Int64, upToCount: Int) async throws -> Data {
        try Task.checkCancellation()
        guard offset >= 0, offset < info.total, upToCount > 0 else { throw VideoFileSource.invalidResponse }
        let start = offset / Self.chunkBytes * Self.chunkBytes
        let range = start..<min(start + Self.chunkBytes, info.total)
        let count = min(upToCount, Int(range.upperBound - offset))
        if let cached = try lock.withLock({ () throws -> Data? in
            guard !closed else { throw CancellationError() }
            return chunks.contains(start) ? try read(offset: offset, count: count) : nil
        }) { return cached }
        let part = try await source.read(range, info.revision)
        try Task.checkCancellation()
        guard part.range == range, part.total == info.total, part.revision == info.revision,
              part.data.count == range.count else { throw VideoFileSource.invalidResponse }
        let (data, received) = try lock.withLock {
            guard !closed else { throw CancellationError() }
            if !chunks.contains(start) {
                try handle.seek(toOffset: UInt64(start))
                try handle.write(contentsOf: part.data)
                chunks.insert(start)
                self.received += Int64(part.data.count)
            }
            return (try read(offset: offset, count: count), self.received)
        }
        progress(received, info.total)
        return data
    }

    private func read(offset: Int64, count: Int) throws -> Data {
        try handle.seek(toOffset: UInt64(offset))
        let data = try handle.read(upToCount: count) ?? Data()
        guard data.count == count else { throw VideoFileSource.invalidResponse }
        return data
    }

    func close() {
        lock.withLock {
            guard !closed else { return }
            closed = true
            source.close()
            try? handle.close()
            try? FileManager.default.removeItem(at: url.deletingLastPathComponent())
        }
    }

    deinit { close() }
}

/// Only this serial queue touches AVFoundation loading requests. Tasks read at most one cache chunk at a time.
nonisolated final class VideoFileResource: NSObject, AVAssetResourceLoaderDelegate, @unchecked Sendable {
    let asset: AVURLAsset
    let cache: VideoFileCache
    private let queue = DispatchQueue(label: "persona.video.resource")
    private let queueKey = DispatchSpecificKey<Bool>()
    private let contentType: String
    private let failed: @Sendable (String) -> Void
    private var jobs: [ObjectIdentifier: Job] = [:]
    private var closed = false

    private struct Job {
        let request: AVAssetResourceLoadingRequest
        let task: Task<Void, Never>
    }

    private final class Request: @unchecked Sendable {
        let value: AVAssetResourceLoadingRequest
        init(_ value: AVAssetResourceLoadingRequest) { self.value = value }
    }

    init(cache: VideoFileCache, name: String, failed: @escaping @Sendable (String) -> Void) {
        self.cache = cache
        self.failed = failed
        if let type = UTType(mimeType: cache.info.mime), type.conforms(to: .audiovisualContent) {
            contentType = type.identifier
        } else {
            contentType = (UTType(filenameExtension: (name as NSString).pathExtension) ?? .mpeg4Movie).identifier
        }
        // The asset URL contains neither the hub's address nor its credentials or the remote path.
        var components = URLComponents()
        components.scheme = "persona-video"
        components.host = UUID().uuidString
        components.path = "/" + (name as NSString).lastPathComponent
        asset = AVURLAsset(url: components.url!)
        super.init()
        queue.setSpecific(key: queueKey, value: true)
        asset.resourceLoader.setDelegate(self, queue: queue)
    }

    func resourceLoader(_ resourceLoader: AVAssetResourceLoader, shouldWaitForLoadingOfRequestedResource loadingRequest: AVAssetResourceLoadingRequest) -> Bool {
        guard !closed else {
            loadingRequest.finishLoading(with: CancellationError())
            return true
        }
        if let info = loadingRequest.contentInformationRequest {
            info.contentType = contentType
            info.contentLength = cache.info.total
            info.isByteRangeAccessSupported = true
        }
        guard let data = loadingRequest.dataRequest else {
            loadingRequest.finishLoading()
            return true
        }
        let start = max(data.currentOffset, data.requestedOffset)
        let (requestedEnd, overflow) = data.requestedOffset.addingReportingOverflow(Int64(data.requestedLength))
        let end = data.requestsAllDataToEndOfResource || overflow ? cache.info.total : min(requestedEnd, cache.info.total)
        guard start >= 0, start <= end else {
            loadingRequest.finishLoading(with: VideoFileSource.invalidResponse)
            return true
        }
        let request = Request(loadingRequest)
        let id = ObjectIdentifier(loadingRequest)
        let task = Task { [self] in
            do {
                var offset = start
                while offset < end {
                    let part = try await cache.data(at: offset, upToCount: Int(min(end - offset, VideoFileCache.chunkBytes)))
                    try Task.checkCancellation()
                    let accepted = await withCheckedContinuation { continuation in
                        queue.async { [self] in
                            guard !closed, jobs[id] != nil else { continuation.resume(returning: false); return }
                            request.value.dataRequest?.respond(with: part)
                            continuation.resume(returning: true)
                        }
                    }
                    guard accepted else { return }
                    offset += Int64(part.count)
                }
                queue.async { [self] in
                    guard jobs.removeValue(forKey: id) != nil else { return }
                    request.value.finishLoading()
                }
            } catch {
                let cancelled = Task.isCancelled || error is CancellationError
                queue.async { [self] in
                    guard jobs.removeValue(forKey: id) != nil else { return }
                    request.value.finishLoading(with: error)
                    if !closed, !cancelled, let message = error.shown { failed(message) }
                }
            }
        }
        jobs[id] = Job(request: loadingRequest, task: task)
        return true
    }

    func resourceLoader(_ resourceLoader: AVAssetResourceLoader, didCancel loadingRequest: AVAssetResourceLoadingRequest) {
        jobs.removeValue(forKey: ObjectIdentifier(loadingRequest))?.task.cancel()
    }

    func close() {
        asset.cancelLoading()
        let stop = { [self] in
            guard !closed else { return }
            closed = true
            for job in jobs.values {
                job.task.cancel()
                job.request.finishLoading(with: CancellationError())
            }
            jobs.removeAll()
            cache.close()
        }
        if DispatchQueue.getSpecific(key: queueKey) == true { stop() } else { queue.sync(execute: stop) }
    }

    deinit { close() }
}
