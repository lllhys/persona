#if os(macOS)
import Darwin
import Foundation

/// One ffmpeg for x264 (#8859): a separate program the helper starts with fixed arguments, NV12 frames in on a socket,
/// FLV out on a pipe (`X264Plan.arguments`, `FLVReader`).
///
/// It is started as **its own responsible process** (`responsibility_spawnattrs_setdisclaim`, a private call in
/// libSystem that Chromium uses for its helpers too; verified on macOS 26.6.2 (25G83)): a child is otherwise attributed to its
/// parent for macOS's privacy checks, and this parent holds the Screen Recording grant, which user-installed code must
/// not use. Without the call (`canSpawn` false) the helper offers no x264. It also gets an empty environment, only
/// stdin, stdout and /dev/null as stderr (POSIX_SPAWN_CLOEXEC_DEFAULT), and ends with the stream: killed when the
/// helper stops it, and at end of input when the helper itself is gone.
public final class X264Process: @unchecked Sendable {
    public enum Event: Sendable {
        case item(FLVReader.Item)
        /// The process ended (or was ended): nil when its output simply stopped, else what was wrong with it.
        case ended(Error?)
    }

    public enum SpawnError: Error, Equatable {
        case noDisclaim
        case notExecutable
        case system(String, Int32)
    }

    /// The input socket's buffers: a whole 4K NV12 frame fits, so a write never waits on ffmpeg reading it.
    public static let bufferBytes: Int32 = 16 << 20

    public let pid: pid_t
    private let input: Int32
    private let writer = DispatchQueue(label: "persona-screen.x264.write", qos: .userInteractive)
    /// The input is open: read and set only on `writer`, so no write ever goes to a closed (and reused) descriptor.
    private var inputOpen = true
    private let lock = NSLock()
    private var killed = false

    private typealias Disclaim = @convention(c) (UnsafeMutablePointer<posix_spawnattr_t?>?, Int32) -> Int32

    private static let disclaim: Disclaim? = {
        guard let symbol = dlsym(UnsafeMutableRawPointer(bitPattern: -2), "responsibility_spawnattrs_setdisclaim") else {
            return nil
        }
        return unsafeBitCast(symbol, to: Disclaim.self)
    }()

    /// This macOS lets the helper start ffmpeg as its own responsible process.
    public static var canSpawn: Bool { disclaim != nil }

    /// Starts `ffmpeg` with `arguments`; `onEvent` is called on a reader thread of its own, in order.
    public init(ffmpeg: String, arguments: [String], onEvent: @escaping @Sendable (Event) -> Void) throws {
        guard let disclaim = Self.disclaim else { throw SpawnError.noDisclaim }
        guard ffmpeg.hasPrefix("/"), FileManager.default.isExecutableFile(atPath: ffmpeg) else { throw SpawnError.notExecutable }

        var pair: [Int32] = [-1, -1]
        guard socketpair(AF_UNIX, SOCK_STREAM, 0, &pair) == 0 else { throw SpawnError.system("socketpair", errno) }
        var out: [Int32] = [-1, -1]
        guard pipe(&out) == 0 else {
            close(pair[0]); close(pair[1])
            throw SpawnError.system("pipe", errno)
        }
        let (ours, theirs, readEnd, writeEnd) = (pair[0], pair[1], out[0], out[1])
        var size = Self.bufferBytes
        let len = socklen_t(MemoryLayout<Int32>.size)
        setsockopt(ours, SOL_SOCKET, SO_SNDBUF, &size, len)
        setsockopt(theirs, SOL_SOCKET, SO_RCVBUF, &size, len)
        var on: Int32 = 1
        setsockopt(ours, SOL_SOCKET, SO_NOSIGPIPE, &on, len)
        _ = fcntl(ours, F_SETFD, FD_CLOEXEC)
        _ = fcntl(readEnd, F_SETFD, FD_CLOEXEC)

        var attr: posix_spawnattr_t?
        posix_spawnattr_init(&attr)
        defer { posix_spawnattr_destroy(&attr) }
        posix_spawnattr_setflags(&attr, Int16(POSIX_SPAWN_CLOEXEC_DEFAULT))
        var actions: posix_spawn_file_actions_t?
        posix_spawn_file_actions_init(&actions)
        defer { posix_spawn_file_actions_destroy(&actions) }
        posix_spawn_file_actions_adddup2(&actions, theirs, 0)
        posix_spawn_file_actions_adddup2(&actions, writeEnd, 1)
        posix_spawn_file_actions_addopen(&actions, 2, "/dev/null", O_WRONLY, 0)

        let argv: [UnsafeMutablePointer<CChar>?] = ([ffmpeg] + arguments).map { strdup($0) } + [nil]
        defer { argv.forEach { free($0) } }
        let envp: [UnsafeMutablePointer<CChar>?] = [nil]
        var pid: pid_t = 0
        let disclaimed = disclaim(&attr, 1)
        let spawned = disclaimed == 0 ? posix_spawn(&pid, ffmpeg, &actions, &attr, argv, envp) : disclaimed
        close(theirs)
        close(writeEnd)
        guard spawned == 0 else {
            close(ours); close(readEnd)
            throw SpawnError.system(disclaimed == 0 ? "posix_spawn" : "disclaim", spawned)
        }
        self.pid = pid
        input = ours

        let reader = Thread { [pid] in
            var flv = FLVReader()
            var failure: Error?
            var chunk = [UInt8](repeating: 0, count: 1 << 18)
            while true {
                let n = chunk.withUnsafeMutableBytes { read(readEnd, $0.baseAddress, $0.count) }
                if n < 0, errno == EINTR { continue }
                guard n > 0 else { break }
                do {
                    for item in try flv.push(Data(chunk[0 ..< n])) { onEvent(.item(item)) }
                } catch {
                    failure = error
                    kill(pid, SIGKILL)
                    break
                }
            }
            close(readEnd)
            var status: Int32 = 0
            while waitpid(pid, &status, 0) < 0, errno == EINTR {}
            onEvent(.ended(failure))
        }
        reader.qualityOfService = .userInteractive
        reader.name = "persona-screen.x264.read"
        reader.start()
    }

    deinit { stop() }

    /// Runs `body` on this process's writer queue with a function that writes all of a buffer to ffmpeg's input
    /// (false once it can't: the process ended). Frames go in the order sent.
    public func send(_ body: @escaping @Sendable (_ write: (UnsafeRawPointer, Int) -> Bool) -> Void) {
        writer.async { [self] in
            guard inputOpen else { return }
            body { pointer, count in
                var done = 0
                while done < count {
                    let n = Darwin.write(input, pointer + done, count - done)
                    if n < 0, errno == EINTR { continue }
                    guard n > 0 else { return false }
                    done += n
                }
                return true
            }
        }
    }

    /// Ends the process now; a write it was taking returns false. Its last output is not waited for.
    public func stop() {
        lock.lock()
        let first = !killed
        killed = true
        lock.unlock()
        guard first else { return }
        kill(pid, SIGKILL)
        writer.async { [self] in // after any write in progress, which the kill ends
            inputOpen = false
            close(input)
        }
    }
}

/// x264 for one stream's run at one size and frame rate (#8859): the ffmpeg encoding now, a spare one started and
/// waiting, and the frames given to it that haven't come back.
///
/// ffmpeg can't be asked for a keyframe while it runs, so a keyframe (the phone's `keyframe`, a decoding error, the
/// node after dropping frames) comes from the spare: its first frame always is one (5–7 ms from a waiting process,
/// 30–40 ms from a cold Homebrew ffmpeg). The old process is ended and its frames still inside it dropped, since the
/// keyframe supersedes them; a new spare starts.
///
/// Each frame out maps to the frame given in by order, checked by its FLV timestamp (`X264Plan.timestampMs`). Anything
/// off — a process ending, bad output, a frame that isn't the next, one overdue or too slow (`X264Watch`) — ends x264
/// for the stream with an `off` reason and what was seen; the caller goes back to VideoToolbox.
///
/// A frame's encode time for the overlay runs from its write to its end, queueing included. Whether x264 keeps up is
/// judged on each frame's own time, from when ffmpeg could start on it (it came in, and the frame before it was out) to
/// its end: frames that waited behind a cold process's first one (30–40 ms, ffmpeg loading) are not slow themselves
/// (#8859: on the first real session the frame after the first read 21 ms and x264 stopped as "the Mac couldn't keep
/// up" a fraction of a second into the stream).
///
/// Called only on `queue`, which its processes' events hop onto too.
public final class X264Session<Context>: @unchecked Sendable {
    public enum Event {
        case parameterSets([Data])
        case frame(sample: Data, keyframe: Bool, context: Context, encodeMicros: Int64)
        /// x264 stopped: why, and what was seen, for the helper's log ("frame 12: 61.0 ms").
        case off(X264Plan.Off, String)
    }

    private struct Pending {
        var index: Int
        var given: UInt64
        var context: Context
    }

    private final class Run: @unchecked Sendable {
        var process: X264Process!
        var next = 0
        var pending: [Pending] = []
        /// When its last frame came out (µs), 0 before the first.
        var lastOut: UInt64 = 0
    }

    public let width: Int
    public let height: Int
    public let fps: Int
    private let ffmpeg: String
    private let arguments: [String]
    private let queue: DispatchQueue
    private let clock: () -> UInt64
    private let onEvent: (Event) -> Void
    private var active: Run?
    private var spare: Run?
    private var watch = X264Watch()
    private var ended = false

    public init(ffmpeg: String, width: Int, height: Int, fps: Int, threads: Int, capKbps: Int, queue: DispatchQueue,
                clock: @escaping () -> UInt64, onEvent: @escaping (Event) -> Void) {
        self.ffmpeg = ffmpeg
        self.width = width
        self.height = height
        self.fps = fps
        arguments = X264Plan.arguments(width: width, height: height, fps: fps, threads: threads, capKbps: capKbps)
        self.queue = queue
        self.clock = clock
        self.onEvent = onEvent
    }

    /// Starts the encoding process and its spare. False when ffmpeg can't be started.
    public func start() -> Bool {
        guard let first = spawn(), let second = spawn() else {
            stop()
            return false
        }
        active = first
        spare = second
        return true
    }

    /// Frames given to the encoding process and not back yet.
    public var inFlight: Int { active?.pending.count ?? 0 }

    /// Gives the next frame. `keyframe`: this one must be a keyframe, so it goes to the spare unless the process
    /// encoding now has had no frame yet (its first is one anyway). `write` writes the frame's NV12 planes, packed,
    /// through the function it is given, on the process's writer queue.
    public func encode(context: Context, keyframe: Bool,
                       write: @escaping @Sendable (_ write: (UnsafeRawPointer, Int) -> Bool) -> Void) {
        guard !ended, var run = active else { return }
        if keyframe, run.next > 0 {
            guard let spare, let fresh = spawn() else { return off(.failed, "a new spare didn't start") }
            run.process.stop()
            run = spare
            active = spare
            self.spare = fresh
        }
        run.pending.append(Pending(index: run.next, given: clock(), context: context))
        run.next += 1
        run.process.send(write)
    }

    /// Every 100 ms or so: x264 ends when the oldest frame inside ffmpeg is overdue.
    public func tick() {
        let now = clock()
        guard !ended, let run = active, let oldest = run.pending.first,
              X264Watch.overdue(oldest: oldest.given, now: now) else { return }
        let detail = String(format: "frame %d still inside after %.0f ms", oldest.index, Double(now &- oldest.given) / 1000)
        off(oldest.index == 0 ? .failed : .slow, detail)
    }

    /// Ends both processes; nothing more is said.
    public func stop() {
        ended = true
        active?.process.stop()
        spare?.process.stop()
        active = nil
        spare = nil
    }

    private func spawn() -> Run? {
        let run = Run()
        do {
            run.process = try X264Process(ffmpeg: ffmpeg, arguments: arguments) { [weak self, weak run, queue] event in
                queue.async { self?.handle(event, from: run) }
            }
        } catch {
            return nil
        }
        return run
    }

    private func handle(_ event: X264Process.Event, from run: Run?) {
        guard !ended, let run else { return }
        let isActive = run === active
        switch event {
        case let .ended(error):
            if isActive || run === spare { // an old process ends because it was ended
                off(.failed, "ffmpeg ended" + (error.map { ": \($0)" } ?? ""))
            }
        case let .item(item):
            guard isActive else { return } // an old process's last frames: superseded by the keyframe
            switch item {
            case let .parameterSets(sets):
                onEvent(.parameterSets(sets))
            case let .frame(timestamp, keyframe, sample):
                guard !run.pending.isEmpty else { return off(.failed, "a frame out that wasn't given") }
                let pending = run.pending.removeFirst()
                guard timestamp == X264Plan.timestampMs(index: pending.index, fps: fps),
                      pending.index > 0 || keyframe else {
                    return off(.failed, "frame \(pending.index) out of order (\(timestamp) ms)")
                }
                let now = clock()
                let micros = Int64(bitPattern: now &- pending.given)
                let own = X264Watch.ownMicros(given: pending.given, lastOut: run.lastOut, now: now)
                run.lastOut = now
                onEvent(.frame(sample: sample, keyframe: keyframe, context: pending.context, encodeMicros: micros))
                if watch.encoded(micros: own, at: now, index: pending.index) {
                    off(.slow, "frame \(pending.index): " + watch.why)
                }
            }
        }
    }

    private func off(_ reason: X264Plan.Off, _ detail: String) {
        guard !ended else { return }
        stop()
        onEvent(.off(reason, detail))
    }
}
#endif
