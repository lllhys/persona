import Foundation
import os
import Testing
@testable import Persona
import PersonaWire

/// Developing Persona (#9460, docs/operations/developing-persona.md): what may go into the package (scrubbing, route
/// patterns), what the recorder writes a minute and per foreground period, how the files are kept small, and what the
/// export holds for each choice of categories.
struct DevModeTests {
    private static let t0 = Date(timeIntervalSince1970: 1_800_000_000) // 2027-01-15T08:00:00Z

    /// A clock and a CPU counter the test moves.
    private final class Fake: @unchecked Sendable {
        // Now, not a fixed date: the store keeps MetricKit files by their real modification time.
        private let lock = OSAllocatedUnfairLock(initialState: (Date(), DevRecorder.Power()))
        var now: Date { lock.withLock { $0.0 } }
        var power: DevRecorder.Power { lock.withLock { $0.1 } }
        func advance(_ seconds: TimeInterval, cpuMillis: UInt64 = 0, wakeups: UInt64 = 0) {
            lock.withLock {
                $0.0 = $0.0.addingTimeInterval(seconds)
                $0.1.cpuNanos += cpuMillis * 1_000_000
                $0.1.interruptWakeups += wakeups
                $0.1.energyNanojoules += cpuMillis * 1_000_000
            }
        }
    }

    private func scratch() -> URL {
        FileManager.default.temporaryDirectory.appending(path: "dev-mode-tests-\(UUID().uuidString)", directoryHint: .isDirectory)
    }

    private func recorder(_ dir: URL, _ fake: Fake, maxBytes: Int64 = DevStore.defaultMaxBytes) -> DevRecorder {
        DevRecorder(store: DevStore(directory: dir, maxBytes: maxBytes), clock: { fake.now }, power: { fake.power }, build: "612")
    }

    private func lines(_ store: DevStore) -> [[String: Any]] {
        store.eventFiles().flatMap { url in
            ((try? String(contentsOf: url, encoding: .utf8)) ?? "").split(separator: "\n").compactMap {
                (try? JSONSerialization.jsonObject(with: Data($0.utf8))) as? [String: Any]
            }
        }
    }

    // MARK: Scrubbing

    @Test func freeTextLosesSecretsAddressesAndHosts() {
        let cases: [(String, String)] = [
            ("GET https://hub.example.com:8443/api/v1/sessions?token=abc#x failed", "GET /api/v1/sessions failed"),
            ("open persona://pair?url=https%3A%2F%2Fh&token=s3cr3t now", "open  now"),
            ("Authorization: Bearer eyJhbGciOi.eyJzdWIiOiIx.c2lnbmF0dXJl", "Authorization: <redacted> <redacted>"),
            ("header Bearer abc.def-123", "header Bearer <redacted>"),
            ("password=hunter2 and api_key: 'k-1'", "password=<redacted> and api_key: <redacted>"),
            ("cookie=a%3Db; next", "cookie=<redacted>; next"),
            ("mail me@example.com please", "mail <email> please"),
            ("connect to 192.0.2.10 failed", "connect to <ip> failed"),
            ("v6 2001:db8:0:0:0:0:0:1 and fe80::1", "v6 <ip> and <ip>"),
            ("could not resolve myhub.example.net", "could not resolve <host>"),
            ("file /Users/someone/Library/x.json", "file ~/Library/x.json"),
            ("id 0123456789abcdef0123456789abcdef01 end", "id <redacted> end"),
            ("at 10:32:15 the status code 502", "at 10:32:15 the status code 502"),
        ]
        for (raw, want) in cases { #expect(DevScrub.text(raw) == want, "\(raw)") }
    }

    @Test func freeTextIsCut() {
        let long = String(repeating: "长", count: 2000)
        let out = DevScrub.text(long)
        #expect(out.utf8.count <= DevScrub.maxText)
        #expect(out.hasSuffix("…"))
    }

    @Test func routesKeepOnlyTheAPIsWords() {
        #expect(DevScrub.route("https://h.example.com/api/v1/machines/my-mac/sessions/9f1e/items?since=3") == "/machines/:id/sessions/:id/items")
        #expect(DevScrub.route("/api/v1/sessions") == "/sessions")
        #expect(DevScrub.route("/api/v1/assistant/chats/abc/messages") == "/assistant/chats/:id/messages")
        #expect(DevScrub.route("/api/v1") == "/")
        #expect(DevScrub.route("/blob/x?y#z") == "/blob/:id")
    }

    @Test func wordsAreCodeChosenOnly() {
        #expect(DevScrub.word("sessions_list") == "sessions_list")
        #expect(DevScrub.word("hello world") == "other")
        #expect(DevScrub.word("会话") == "other")
        #expect(DevScrub.word(String(repeating: "a", count: 49)) == "other")
    }

    /// The same words as the web's error reports (`web/src/lib/error-reports.ts` `API_WORDS`).
    @Test func apiWordsMatchTheWeb() throws {
        let repo = URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        let source = try String(contentsOf: repo.appending(path: "web/src/lib/error-reports.ts"), encoding: .utf8)
        let start = try #require(source.range(of: "const API_WORDS = new Set("))
        let end = try #require(source.range(of: ".split(\" \")", range: start.upperBound..<source.endIndex))
        let body = source[start.upperBound..<end.lowerBound]
        let words = body.split(separator: "\"").enumerated().filter { $0.offset % 2 == 1 }.map(\.element).joined()
        #expect(Set(words.split(separator: " ").map(String.init)) == DevScrub.apiWords)
    }

    // MARK: Recording

    @Test func aMinuteLineCountsRequestsFramesAndPower() throws {
        let dir = scratch(), fake = Fake()
        defer { try? FileManager.default.removeItem(at: dir) }
        let r = recorder(dir, fake)
        r.start(active: true, page: "assistant", device: .init(thermal: "nominal", battery: 0.8))
        r.path(.init(type: "cellular", expensive: true, constrained: false))
        let url = URL(string: "https://hub.example.com/api/v1/machines/m1/sessions/s1/items?since=4")!
        r.request(method: "get", url: url, status: 200, bytesIn: 1000, bytesOut: 100, millis: 30)
        r.request(method: "GET", url: url, status: 200, bytesIn: 500, bytesOut: 100, millis: 50)
        r.request(method: "GET", url: URL(string: "https://hub.example.com/api/v1/sessions")!, status: 502, bytesIn: 10, bytesOut: 10, millis: 900)
        r.request(method: "GET", url: URL(string: "https://hub.example.com/api/v1/sessions")!, status: 502, bytesIn: 10, bytesOut: 10, millis: 900)
        r.frame(type: "sessions", bytes: 300)
        r.frame(type: "sessions", bytes: 300)
        r.frame(type: "item", bytes: 50)
        r.socket(.dropped)
        r.socket(.connected)
        fake.advance(20)
        r.page("chat")
        fake.advance(40, cpuMillis: 250, wakeups: 7)
        r.roll(device: .init(thermal: "fair", battery: 0.79))
        r.settle()

        let all = lines(r.store)
        let minute = try #require(all.first { $0["kind"] as? String == "minute" })
        #expect(minute["requests"] as? Int == 4)
        #expect(minute["build"] as? String == "612")
        #expect(minute["background"] as? Bool == false)
        #expect(minute["cpu_ms"] as? Double == 250)
        #expect((minute["wakeups"] as? [String: Any])?["interrupt"] as? Int == 7)
        #expect(minute["thermal"] as? String == "fair")
        #expect(minute["battery"] as? Double == 0.79)
        #expect((minute["pages"] as? [String: Int]) == ["assistant": 20, "chat": 40])
        #expect((minute["net"] as? [String: Any])?["type"] as? String == "cellular")
        let http = try #require(minute["http"] as? [[String: Any]])
        let items = try #require(http.first { $0["route"] as? String == "GET /machines/:id/sessions/:id/items" })
        #expect(items["n"] as? Int == 2)
        #expect(items["in"] as? Int == 1500)
        #expect(items["max_ms"] as? Int == 50)
        #expect((items["status"] as? [String: Int]) == ["200": 2])
        let ws = try #require(minute["ws"] as? [String: Any])
        #expect((ws["frames"] as? [String: Int]) == ["sessions": 2, "item": 1])
        #expect(ws["drops"] as? Int == 1)
        #expect(ws["connects"] as? Int == 1)
        // One line for a failing route and status in a minute, however often it fails.
        #expect(all.filter { $0["kind"] as? String == "request_error" }.count == 1)
        #expect(all.contains { $0["kind"] as? String == "page" && $0["page"] as? String == "chat" })
        // No host, id or query anywhere.
        let text = r.store.eventFiles().compactMap { try? String(contentsOf: $0, encoding: .utf8) }.joined()
        for banned in ["hub.example.com", "m1", "s1", "since"] { #expect(!text.contains(banned), "\(banned)") }
    }

    @Test func aForegroundPeriodIsSummedWhenTheAppLeaves() throws {
        let dir = scratch(), fake = Fake()
        defer { try? FileManager.default.removeItem(at: dir) }
        let r = recorder(dir, fake)
        r.start(active: true, page: "agents", device: .init(thermal: "nominal", battery: 0.9))
        r.request(method: "GET", url: URL(string: "/api/v1/sessions"), status: 200, bytesIn: 100, bytesOut: 10, millis: 5)
        fake.advance(60, cpuMillis: 100)
        r.roll(device: .init(thermal: "nominal", battery: 0.9))
        r.request(method: "GET", url: URL(string: "/api/v1/sessions"), status: 200, bytesIn: 100, bytesOut: 10, millis: 5)
        r.frame(type: "sessions", bytes: 10)
        fake.advance(30, cpuMillis: 400)
        r.lifecycle(active: false, device: .init(thermal: "serious", battery: 0.88))
        // A repeated notice changes nothing.
        r.lifecycle(active: false, device: .init(thermal: "serious", battery: 0.88))
        fake.advance(600, cpuMillis: 50)
        r.lifecycle(active: true, device: .init(thermal: "fair", battery: 0.87))
        r.settle()

        let all = lines(r.store)
        let periods = all.filter { $0["kind"] as? String == "foreground" }
        #expect(periods.count == 1)
        let p = try #require(periods.first)
        #expect(p["seconds"] as? Int == 90)
        #expect(p["cpu_s"] as? Double == 0.5)
        #expect(p["requests"] as? Int == 2)
        #expect(p["ws_frames"] as? Int == 1)
        #expect((p["pages"] as? [String: Int]) == ["agents": 90])
        #expect((p["thermal"] as? [String: String]) == ["start": "nominal", "end": "serious"])
        let minutes = all.filter { $0["kind"] as? String == "minute" }
        #expect(minutes.count == 3) // the full minute, the 30 s before leaving, the 10 minutes away
        let away = try #require(minutes.last)
        #expect(away["background"] as? Bool == true)
        #expect(away["cpu_ms"] as? Double == 50)
    }

    @Test func nothingIsRecordedWhileOff() {
        let dir = scratch(), fake = Fake()
        defer { try? FileManager.default.removeItem(at: dir) }
        let r = recorder(dir, fake)
        r.request(method: "GET", url: URL(string: "/api/v1/sessions"), status: 200, bytesIn: 1, bytesOut: 1, millis: 1)
        r.frame(type: "sessions", bytes: 1)
        r.error(ErrorReports.report(kind: "log", level: "warning", message: "x"))
        r.roll(device: .init())
        r.settle()
        #expect(r.store.recordedBytes() == 0)
    }

    @Test func errorsAreKeptScrubbed() throws {
        let dir = scratch(), fake = Fake()
        defer { try? FileManager.default.removeItem(at: dir) }
        let r = recorder(dir, fake)
        r.start(active: true, page: "assistant", device: .init())
        r.error(ErrorReports.report(kind: "log", level: "warning", message: "upload to https://hub.example.com/x?token=t failed for me@example.com",
                                    at: fake.now))
        r.settle()
        let kept = r.store.errorLines(now: fake.now)
        #expect(kept.count == 1)
        let line = try #require(kept.first)
        let report = try JSON.decoder.decode(ExtensionErrorReport.self, from: Data(line.utf8))
        #expect(report.message == "upload to /x failed for <email>")
        #expect(!line.contains("example.com"))
    }

    // MARK: Keeping it small

    @Test func oldHoursAndOverflowAreDropped() throws {
        let dir = scratch()
        defer { try? FileManager.default.removeItem(at: dir) }
        let store = DevStore(directory: dir, maxBytes: 3100)
        let now = Self.t0
        store.appendEvents(["{\"kind\":\"old\"}"], at: now.addingTimeInterval(-26 * 3600))
        store.appendEvents([String(repeating: "a", count: 1500)], at: now.addingTimeInterval(-3 * 3600))
        store.appendEvents([String(repeating: "b", count: 1500)], at: now.addingTimeInterval(-2 * 3600))
        store.appendEvents([String(repeating: "c", count: 1500)], at: now)
        let names = store.eventFiles().map(\.lastPathComponent)
        #expect(names == [DevStore.hourName(now.addingTimeInterval(-2 * 3600)), DevStore.hourName(now)])
        #expect(store.recordedBytes() <= 3100)
        #expect(DevStore.hourDate(DevStore.hourName(now)) == now) // t0 is on the hour
    }

    // MARK: The package

    @Test func thePackageHoldsWhatWasPicked() throws {
        let dir = scratch(), out = scratch(), fake = Fake(), start = fake.now
        defer { try? FileManager.default.removeItem(at: dir); try? FileManager.default.removeItem(at: out) }
        let r = recorder(dir, fake)
        r.start(active: true, page: "assistant", device: .init())
        r.request(method: "GET", url: URL(string: "/api/v1/sessions"), status: 200, bytesIn: 1, bytesOut: 1, millis: 1)
        fake.advance(60)
        r.roll(device: .init())
        r.page("chat")
        r.error(ErrorReports.report(kind: "log", level: "warning", message: "boom", at: fake.now))
        r.settle()
        r.store.writeMetric(Data("{\"a\":1}".utf8), name: "metrics-1.json")
        r.store.writeMetric(Data("{\"b\":1}".utf8), name: "diagnostics-1.json")
        var device = DevPackage.Device()
        device.model = "iPhone17,1"

        let all = try DevPackage.writePackage(into: out, store: r.store, categories: Set(DevPackage.Category.allCases), since: start,
                                       device: device, log: ["l1"], now: fake.now)
        #expect(all.events >= 3)
        #expect(all.errors == 1)
        #expect(all.metricPayloads == 1)
        #expect(all.diagnosticPayloads == 1)
        #expect(all.logLines == 1)
        let names = Set(try FileManager.default.contentsOfDirectory(atPath: out.path(percentEncoded: false)))
        #expect(names == ["README.md", "manifest.json", "events.jsonl", "errors.jsonl", "metrickit", "log.txt"])
        let manifest = try #require(JSONSerialization.jsonObject(with: Data(contentsOf: out.appending(path: "manifest.json"))) as? [String: Any])
        #expect(manifest["format"] as? String == "persona-diagnostics")
        #expect(manifest["device_model"] as? String == "iPhone17,1")
        #expect(manifest["categories"] as? [String] == ["device", "logs", "errors", "energy"])

        let energyOnly = try DevPackage.writePackage(into: out, store: r.store, categories: [.energy], since: start, device: device, log: ["l1"], now: fake.now)
        #expect(energyOnly.errors == 0 && energyOnly.diagnosticPayloads == 0 && energyOnly.logLines == 0)
        #expect(energyOnly.metricPayloads == 1)
        let events = try String(contentsOf: out.appending(path: "events.jsonl"), encoding: .utf8)
        #expect(events.contains("\"kind\":\"minute\""))
        #expect(!events.contains("\"kind\":\"page\""))
        let leftNames = Set(try FileManager.default.contentsOfDirectory(atPath: out.path(percentEncoded: false)))
        #expect(!leftNames.contains("errors.jsonl") && !leftNames.contains("log.txt"))
        #expect(try String(contentsOf: out.appending(path: "README.md"), encoding: .utf8).contains("Left out by the owner: logs, errors."))
    }

    @Test func exportIsAZip() throws {
        let dir = scratch(), fake = Fake()
        defer { try? FileManager.default.removeItem(at: dir) }
        let r = recorder(dir, fake)
        r.start(active: true, page: "assistant", device: .init())
        r.settle()
        let zip = try DevPackage.exportZip(store: r.store, categories: [.energy], since: fake.now, now: fake.now)
        defer { try? FileManager.default.removeItem(at: zip.deletingLastPathComponent()) }
        #expect(zip.lastPathComponent.hasPrefix("persona-diagnostics-"))
        #expect(zip.pathExtension == "zip")
        let head = try FileHandle(forReadingFrom: zip).read(upToCount: 2)
        #expect(head == Data("PK".utf8))
    }

    @Test func metricKitTextIsScrubbedButIdentifiersStay() throws {
        let json = Data("""
        {"crashDiagnostics":[{"diagnosticMetaData":{"exceptionReason":{"composedMessage":"bad url https://h.example.com/a?token=x for me@example.com"},
        "appBuildVersion":"612"},"callStackTree":{"callStacks":[{"callStackRootFrames":[{"binaryName":"Persona","binaryUUID":"0123456789ABCDEF0123456789ABCDEF01","offsetIntoBinaryTextSegment":123}]}]}}]}
        """.utf8)
        let out = try #require(String(data: DevMetrics.scrubbed(json), encoding: .utf8))
        #expect(out.contains("bad url /a for <email>"))
        #expect(out.contains("0123456789ABCDEF0123456789ABCDEF01"))
        #expect(out.contains("\"appBuildVersion\" : \"612\""))
    }
}
