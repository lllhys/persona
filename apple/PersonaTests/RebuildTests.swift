import Foundation
import Testing
@testable import Persona
import PersonaWire

@MainActor
struct RebuildTests {
    /// A small deterministic generator (the same sequences every run).
    struct LCG {
        var state: UInt64
        mutating func next(_ n: Int) -> Int {
            state = state &* 6364136223846793005 &+ 1442695040888963407
            return Int((state >> 33) % UInt64(n))
        }
    }

    static func item(_ seq: Int, _ role: String, _ kind: ItemKind, text: String? = nil, tool: String? = nil, queued: Bool? = nil,
                     wake: Bool? = nil) -> Item {
        Item(id: "i\(seq)#0", seq: seq, ts: nil, role: role, kind: kind, text: text, toolUseID: tool, toolName: tool == nil ? nil : "Bash",
             toolInput: nil, isError: nil, fullSize: nil, image: nil, queued: queued, from: nil, wake: wake)
    }

    /// Items as a session writes them: prompts, queued input, thoughts, tool calls whose results sometimes come after the
    /// next prompt, notices (wakes, interruptions), recaps.
    static func session(_ seed: UInt64, count: Int) -> [Item] {
        var r = LCG(state: seed)
        var out: [Item] = []
        var open: [String] = []
        var seq = 0
        while out.count < count {
            seq += 1
            switch r.next(12) {
            case 0, 1: out.append(item(seq, "user", .text, text: "问 \(seq)"))
            case 2: out.append(item(seq, "user", .text, text: "插一句", queued: true))
            case 3: out.append(item(seq, "assistant", .thinking, text: "想"))
            case 4, 5:
                let t = "t\(seq)"
                out.append(item(seq, "assistant", .toolUse, tool: t))
                open.append(t)
            case 6, 7:
                if !open.isEmpty { out.append(item(seq, "user", .toolResult, text: "ok", tool: open.remove(at: r.next(open.count)))) }
            case 8: out.append(item(seq, "assistant", .text, text: "答 \(seq)"))
            case 9: out.append(item(seq, "system", .notice, text: r.next(2) == 0 ? "已中断" : "后台任务完成", wake: r.next(2) == 0))
            case 10: out.append(item(seq, "system", .recap, text: "回顾"))
            default: out.append(item(seq, "assistant", .text, text: "说明"))
            }
        }
        return out
    }

    @Test func incrementalRebuildEqualsAFullOne() {
        for seed in UInt64(1)...40 {
            let all = Self.session(seed, count: 160)
            var settled: ChatStore.SettledRows?
            var r = LCG(state: seed)
            var n = 0
            while n < all.count {
                n = min(all.count, n + 1 + r.next(4)) // events arrive one at a time or in small bursts
                let items = Array(all[..<n])
                let built = ChatStore.rebuild(items, settled: settled)
                let rows = ChatStore.buildRows(items)
                #expect(built.rows == rows, "seed \(seed), \(n) items: rows")
                #expect(built.blocks == ChatBlock.build(rows), "seed \(seed), \(n) items: blocks")
                if built.rows != rows { return }
                settled = built.settled
            }
            // An older page arrives in front: what was settled no longer holds.
            let older = Self.session(seed + 1000, count: 30).map { var it = $0; it.id = "old-" + it.id; it.toolUseID = it.toolUseID.map { "old-" + $0 }; return it }
            let items = older + all
            let built = ChatStore.rebuild(items, settled: settled)
            let rows = ChatStore.buildRows(items)
            #expect(built.rows == rows && built.blocks == ChatBlock.build(rows), "seed \(seed): after an older page")
        }
    }

    /// A supervision round's input of a picture and two text blocks, then a queued message with its picture after its text.
    static var multiBlockInputs: [Item] {
        var supervise = Self.item(3, "user", .text, text: "Fictional supervision", queued: true)
        supervise.id = "u3#1"
        supervise.injected = Injected(kind: "system", state: "supervise", line: "Check tasks", level: "triage")
        var photo = Self.item(3, "user", .image)
        photo.id = "u3#0"
        photo.image = ImagePayload(mime: "image/png", data: "")
        var extra = Self.item(3, "user", .text, text: "Fictional additional details")
        extra.id = "u3#2" // A second nonqueued text block must not be an incremental boundary inside this input.
        var nextPhoto = photo
        nextPhoto.seq = 6
        nextPhoto.id = "u6#1"
        var next = Self.item(6, "user", .text, text: "Next genuine queued input", queued: true)
        next.id = "u6#0"
        return [Self.item(1, "user", .text, text: "First request"), Self.item(2, "assistant", .text, text: "First answer"),
                photo, supervise, extra, Self.item(4, "assistant", .text, text: "Supervision answer"),
                Self.item(5, "system", .recap, text: "Fictional recap"), next, nextPhoto,
                Self.item(7, "assistant", .text, text: "Next answer"), Self.item(8, "user", .text, text: "Last request")]
    }

    @Test func supervisionAndMultiBlockInputsRebuildIncrementallyLikeAFullTranscript() {
        let items = Self.multiBlockInputs
        var settled: ChatStore.SettledRows?
        for count in 1...items.count {
            let prefix = Array(items.prefix(count))
            let built = ChatStore.rebuild(prefix, settled: settled)
            let fullRows = ChatStore.buildRows(prefix) // once, as a full build does (#8706)
            #expect(built.rows == fullRows, "\(count) items: rows")
            #expect(built.blocks == ChatBlock.build(fullRows), "\(count) items: blocks")
            settled = built.settled
        }
    }

    /// Cost of one event in a 1000-exchange session (printed, for the record): the full rebuild against the incremental one.
    @Test func incrementalRebuildIsCheap() {
        var items: [Item] = []
        var seq = 0
        for t in 0..<1000 {
            seq += 1; items.append(Self.item(seq, "user", .text, text: "第 \(t) 轮"))
            for _ in 0..<6 {
                seq += 1; items.append(Self.item(seq, "assistant", .toolUse, tool: "t\(seq)"))
                seq += 1; items.append(Self.item(seq, "user", .toolResult, text: "ok", tool: "t\(seq - 1)"))
            }
            seq += 1; items.append(Self.item(seq, "assistant", .text, text: "答"))
        }
        let settled = ChatStore.rebuild(items, settled: nil).settled
        seq += 1; items.append(Self.item(seq, "assistant", .thinking, text: "又想"))
        let clock = ContinuousClock()
        let full = clock.measure { _ = ChatBlock.build(ChatStore.buildRows(items)) }
        var built: (rows: [ChatRow], blocks: [ChatBlock], settled: ChatStore.SettledRows?)?
        let incremental = clock.measure { built = ChatStore.rebuild(items, settled: settled) }
        print("REBUILD-COST \(items.count) items: full \(full), incremental \(incremental)")
        #expect(built?.rows == ChatStore.buildRows(items))
    }

    /// Real sessions (exported by the server's TestCorpusDumpItems; `TEST_RUNNER_PERSONA_ITEMS_DIR=<dir> xcodebuild test`):
    /// the last 200 items arrive one by one; the incremental rebuild must equal the full one at every step. Prints
    /// the cost of both per event. Skipped without the directory.
    @Test func realSessionsRebuildIncrementally() throws {
        guard let dir = ProcessInfo.processInfo.environment["PERSONA_ITEMS_DIR"] else { return }
        let files = try FileManager.default.contentsOfDirectory(atPath: dir).filter { $0.hasSuffix(".json") }.sorted()
        var lines: [String] = []
        for name in files {
            let items = try JSON.decoder.decode([Item].self, from: Data(contentsOf: URL(fileURLWithPath: dir).appending(path: name)))
            let start = max(0, items.count - 200)
            var settled = ChatStore.rebuild(Array(items[..<start]), settled: nil).settled
            var full: [Duration] = [], inc: [Duration] = []
            var mismatches = 0
            let clock = ContinuousClock()
            for n in (start + 1)...items.count {
                let now = Array(items[..<n])
                var rows: [ChatRow] = [], blocks: [ChatBlock] = []
                full.append(clock.measure { rows = ChatStore.buildRows(now); blocks = ChatBlock.build(rows) })
                var built: (rows: [ChatRow], blocks: [ChatBlock], settled: ChatStore.SettledRows?)?
                inc.append(clock.measure { built = ChatStore.rebuild(now, settled: settled) })
                if built?.rows != rows || built?.blocks != blocks { mismatches += 1 }
                settled = built?.settled
            }
            func p(_ d: [Duration], _ q: Double) -> String {
                let s = d.sorted()
                let v = s[min(s.count - 1, Int(Double(s.count) * q))]
                return String(format: "%.2f", Double(v.components.attoseconds) / 1e15 + Double(v.components.seconds) * 1000)
            }
            lines.append("\(name)\t\(items.count)\t\(p(full, 0.5))\t\(p(full, 0.99))\t\(p(inc, 0.5))\t\(p(inc, 0.99))\t\(mismatches)")
            #expect(mismatches == 0, "\(name): incremental rebuild differs from the full one \(mismatches) times")
        }
        let out = "file\titems\tfull p50 ms\tfull p99 ms\tincremental p50 ms\tincremental p99 ms\tmismatches\n" + lines.joined(separator: "\n")
        try out.write(to: URL(fileURLWithPath: dir).appending(path: "rebuild-cost.tsv"), atomically: true, encoding: .utf8)
        print("REBUILD-REAL\n" + out)
    }
}

extension RebuildTests {
    @Test func backgroundRebuildConvergesAfterBurstsAndReplacement() async throws {
        let store = ChatStore(key: SessionKey(machineID: "rebuild-tests", sessionID: UUID().uuidString))
        let first = Self.session(8, count: 1800)
        for end in stride(from: 30, through: first.count, by: 30) {
            store.merge(Array(first[(end - 30)..<end]))
            await Task.yield()
        }
        var replacement = Self.session(91, count: 400)
        for index in replacement.indices { replacement[index].id = "replacement-" + replacement[index].id }
        store.replace(with: replacement, hasMore: false, span: ItemSpan(history: "new", from: nil, to: 400))
        let expected = ChatStore.buildRows(store.items)
        let deadline = ContinuousClock.now + .seconds(5)
        while store.rows != expected && ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(store.rows == expected)
        #expect(store.blocks == ChatBlock.build(expected))
        #expect(store.items.allSatisfy { $0.id.hasPrefix("replacement-") })
    }
}

/// A picture sent before two text blocks of its message (#8706).
extension RebuildTests {
    private static let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()

    /// One entry of a picture and two text blocks: claude -p writes a message's pictures first.
    static func pictureBeforeTwoTexts(_ seq: Int) -> [Item] {
        var photo = item(seq, "user", .image)
        photo.id = "u\(seq)#0"
        photo.image = ImagePayload(mime: "image/png", data: "")
        var first = item(seq, "user", .text, text: "Fictional first block")
        first.id = "u\(seq)#1"
        var second = item(seq, "user", .text, text: "Fictional second block")
        second.id = "u\(seq)#2"
        return [photo, first, second]
    }

    /// The picture goes after the first text block and stays there: `buildRows`, a full `rebuild` and an incremental one
    /// draw the same rows. `rebuild` ordered the items and `buildRows` ordered them again, which moved it behind the second.
    @Test func aPictureBeforeTwoTextBlocksStaysWithTheFirst() {
        let entry = Self.pictureBeforeTwoTexts(5)
        let kept = ["u5#1", "u5#0", "u5#2"]
        func entryRows(_ rows: [ChatRow]) -> [String] { rows.map(\.id).filter { $0.hasPrefix("u5#") } }
        #expect(ChatStore.picturesAfterTheirText(entry).map(\.id) == kept)
        let before = [Self.item(1, "user", .text, text: "First request"), Self.item(2, "assistant", .text, text: "First answer"),
                      Self.item(3, "user", .text, text: "Second request"), Self.item(4, "assistant", .text, text: "Second answer")]
        let answer = [Self.item(6, "assistant", .text, text: "About the picture")]
        let next = [Self.item(7, "user", .text, text: "Last request"), Self.item(8, "assistant", .text, text: "Last answer")]
        // The message alone, last (its exchange is the tail), and settled before the next message.
        for items in [entry, before + entry, before + entry + answer, before + entry + answer + next] {
            let rows = ChatStore.buildRows(items)
            #expect(entryRows(rows) == kept, "\(items.count) items: buildRows")
            let full = ChatStore.rebuild(items, settled: nil)
            #expect(entryRows(full.rows) == kept, "\(items.count) items: rebuild")
            #expect(full.rows == rows && full.blocks == ChatBlock.build(rows), "\(items.count) items: rebuild")
        }
        // Incrementally, as the lines arrive.
        var all = before
        var settled = ChatStore.rebuild(all, settled: nil).settled
        for more in [entry, answer, next] {
            #expect(settled != nil, "\(all.count) items: settled")
            all += more
            let built = ChatStore.rebuild(all, settled: settled)
            let rows = ChatStore.buildRows(all)
            #expect(entryRows(built.rows) == kept, "\(all.count) items: incremental")
            #expect(built.rows == rows && built.blocks == ChatBlock.build(rows), "\(all.count) items: incremental")
            settled = built.settled
        }
    }

    /// What the store publishes comes from `rebuild`: it settles on `buildRows` (#8705's replay saw it never did).
    @Test func theStoreDrawsAPictureBeforeTwoTextBlocksWithTheFirst() async throws {
        let store = ChatStore(key: SessionKey(machineID: "rebuild-tests", sessionID: UUID().uuidString))
        store.merge([Self.item(1, "user", .text, text: "First request"), Self.item(2, "assistant", .text, text: "First answer")])
        store.merge(Self.pictureBeforeTwoTexts(3) + [Self.item(4, "assistant", .text, text: "About the picture")])
        let expected = ChatStore.buildRows(store.items)
        #expect(expected.map(\.id).filter { $0.hasPrefix("u3#") } == ["u3#1", "u3#0", "u3#2"])
        let deadline = ContinuousClock.now + .seconds(5)
        while store.rows != expected && ContinuousClock.now < deadline {
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(store.rows == expected)
        #expect(store.blocks == ChatBlock.build(expected))
    }

    /// A second pass changes nothing: over the fixtures here, every `items` list of the lines corpus
    /// (`spec/fixtures/lines`, its iOS cases too), and every run of up to five items made of two lines' pictures and
    /// texts, an answer and a tool's picture.
    @Test func picturesAfterTheirTextTwiceIsOnce() throws {
        var fixtures: [[Item]] = (UInt64(1)...40).map { Self.session($0, count: 160) }
        fixtures += [Self.multiBlockInputs, Self.pictureBeforeTwoTexts(5)]
        let corpus = try Self.linesCorpusItems()
        #expect(corpus.contains { $0.contains { $0.kind == .image && $0.toolUseID == nil } }, "the corpus has the user's pictures")
        fixtures += corpus
        var tool = Self.item(1, "user", .image, tool: "t1")
        tool.toolName = nil
        let pieces = [Self.item(1, "user", .image), Self.item(1, "user", .text, text: "one"), Self.item(2, "user", .image),
                      Self.item(2, "user", .text, text: "two"), Self.item(1, "assistant", .text, text: "answer"), tool]
        func runs(_ run: [Item], _ left: Int) {
            if !run.isEmpty { fixtures.append(run) }
            guard left > 0 else { return }
            for piece in pieces {
                var it = piece
                it.id = "i\(piece.seq)#\(run.count)"
                runs(run + [it], left - 1)
            }
        }
        runs([], 5)
        var moved = 0
        var failures: [[String]] = []
        for items in fixtures {
            let once = ChatStore.picturesAfterTheirText(items)
            if once != items { moved += 1 }
            if ChatStore.picturesAfterTheirText(once) != once { failures.append(items.map { "\($0.id) \($0.role) \($0.kind)" }) }
        }
        #expect(moved > 0, "no picture moved in \(fixtures.count) runs")
        #expect(failures.isEmpty, "\(failures.count) of \(fixtures.count), first: \(failures.prefix(3))")
    }

    /// Every `items` list in the lines corpus: the cases' inputs and their operations' pages.
    static func linesCorpusItems() throws -> [[Item]] {
        let dir = repo.appending(path: "spec/fixtures/lines")
        let files = (FileManager.default.enumerator(at: dir, includingPropertiesForKeys: nil)?.allObjects as? [URL] ?? [])
            .filter { $0.pathExtension == "json" }.sorted { $0.path < $1.path }
        #expect(!files.isEmpty, "no corpus at \(dir.path)")
        var out: [[Item]] = []
        func walk(_ value: Any) throws {
            if let object = value as? [String: Any] {
                for (key, inner) in object {
                    if key == "items", let list = inner as? [[String: Any]], list.first?["seq"] != nil {
                        out.append(try JSON.decoder.decode([Item].self, from: JSONSerialization.data(withJSONObject: list)))
                    } else {
                        try walk(inner)
                    }
                }
            } else if let array = value as? [Any] {
                for inner in array { try walk(inner) }
            }
        }
        for file in files { try walk(JSONSerialization.jsonObject(with: Data(contentsOf: file))) }
        return out
    }
}
