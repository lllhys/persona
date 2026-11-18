import Foundation
import PersonaCore

extension ProjectionBindingReplay {
    public static func turns() throws -> (cases: Int, turnObjects: Int) {
        let documents = try ProjectionFixture.fixtures(in: "lines")
        guard documents.count == 9 else {
            throw ProjectionFixtureError.invalidFixture("the lines file set must have nine JSON documents")
        }
        var all: [ProjectionFixture] = []
        for document in documents {
            for item in try ProjectionFixture.require(document["cases"]?.array, "line cases") {
                all.append(ProjectionFixture(
                    name: try ProjectionFixture.require(item.text("name"), "line case name"),
                    input: try ProjectionFixture.require(item["input"], "line input"),
                    expected: try ProjectionFixture.require(item["expected"], "line expected")
                ))
            }
        }
        try ProjectionFixture.exactManifest(all, Set(all.map(\.name)), "entire lines corpus")
        guard all.count == 61 else { throw ProjectionFixtureError.invalidFixture("expected 61 total lines cases") }
        let staticCases = all.filter {
            $0.expected["checkpoints"] == nil && $0.input["operations"] == nil
        }
        try ProjectionFixture.exactManifest(staticCases, staticTurnCases, "static turns")
        let overlay = try ContinuityJSON.load(file: repositoryFixtures.appending(path: "lines/ios/differences.json"))
        try validateOverlay(overlay, allCases: all)
        var turnObjects = 0
        for fixture in staticCases {
            let actual = try ProjectionFixture.project(fixture.input, fixture.name, using: PersonaCoreProjections.turns)
            let expected = try expectedTurns(fixture, overlay: overlay, applyCasePatch: true)
            try ProjectionFixture.assertEqual(actual, expected, "turns/\(fixture.name)")
            if overlay["cases"]?.array?.contains(where: { $0.text("name") == fixture.name }) == true {
                let withoutCasePatch = try expectedTurns(fixture, overlay: overlay, applyCasePatch: false)
                guard withoutCasePatch != actual else {
                    throw ProjectionFixtureError.invalidFixture("\(fixture.name): overlay no longer changes its expectation")
                }
            }
            if emptyTurnCases.contains(fixture.name) {
                guard expected == .array([]) else {
                    throw ProjectionFixtureError.invalidFixture("\(fixture.name): an empty-array case is not layout coverage")
                }
            } else {
                guard let values = expected.array, !values.isEmpty else {
                    throw ProjectionFixtureError.invalidFixture("\(fixture.name): missing turn-grouping coverage")
                }
                turnObjects += values.count
            }
        }
        guard turnObjects == 73 else {
            throw ProjectionFixtureError.invalidFixture("expected 73 complete turn objects, got \(turnObjects)")
        }
        try ProjectionFixture.rejectMalformed(PersonaCoreProjections.turns, label: "turns")
        let plain = try ProjectionFixture.require(staticCases.first { $0.name == "plain_exchange" }, "plain_exchange")
        let actual = try ProjectionFixture.project(plain.input, plain.name, using: PersonaCoreProjections.turns)
        try ProjectionFixture.assertWrongExpectationIsRejected(
            actual, wrong: .array([.object([:])]), label: "turns/plain_exchange"
        )
        return (staticCases.count, turnObjects)
    }

    public static func taskIndex() throws -> Int {
        let documents = try ProjectionFixture.fixtures(in: "lines")
        var replayed = Set<String>()
        var receiptChecked = false
        for document in documents {
            for item in try ProjectionFixture.require(document["cases"]?.array, "task-index lines cases") {
                let name = try ProjectionFixture.require(item.text("name"), "task-index name")
                let input = try ProjectionFixture.require(item["input"], "task-index input")
                if input["operations"] != nil { continue }
                let expected = try ProjectionFixture.require(item["expected"], "task-index expected")["task_index"]
                let receipt = name == "background_running_badge"
                if expected == nil || expected == .null, !receipt { continue }
                let actual = try ProjectionFixture.project(input, name, using: PersonaCoreProjections.taskIndex)
                if receipt {
                    try ProjectionFixture.assertEqual(actual, .object([
                        "started": .array([.string("call_a")]), "calls": .object([:]),
                        "descriptions": .object(["call_a": .string("检查示例文件")]), // i18n-ignore: unchanged fixture
                        "started_at": .object([:]), "statuses": .object([:]),
                    ]), "task_index/\(name) background receipt")
                    receiptChecked = true
                } else {
                    try ProjectionFixture.assertEqual(
                        actual, try ProjectionFixture.require(expected, name), "task_index/\(name)"
                    )
                    guard replayed.insert(name).inserted else {
                        throw ProjectionFixtureError.invalidFixture("duplicate task-index case \(name)")
                    }
                }
            }
        }
        guard replayed == taskIndexCases, receiptChecked else {
            throw ProjectionFixtureError.invalidFixture("task-index exact manifest or receipt: \(replayed), \(receiptChecked)")
        }
        try ProjectionFixture.rejectMalformed(PersonaCoreProjections.taskIndex, label: "task_index")
        let name = "task_notices_find_calls"
        let fixture = try ProjectionFixture.cases(in: "lines").first { $0.name == name }
        let plain = try ProjectionFixture.require(fixture, name)
        let actual = try ProjectionFixture.project(plain.input, name, using: PersonaCoreProjections.taskIndex)
        let wrong: ContinuityJSON = .object([
            "started": .array([]), "calls": .object([:]), "descriptions": .object([:]),
            "started_at": .object([:]), "statuses": .object([:]),
        ])
        try ProjectionFixture.assertWrongExpectationIsRejected(actual, wrong: wrong, label: "task_index/\(name)")
        return replayed.count
    }

    private static func expectedTurns(
        _ fixture: ProjectionFixture, overlay: ContinuityJSON, applyCasePatch: Bool
    ) throws -> ContinuityJSON {
        let wake = try ProjectionFixture.require(overlay["turn_wake_state"], "turn_wake_state")
        let drop = try ProjectionFixture.require(wake.text("drop"), "wake drop")
        let set = try ProjectionFixture.require(wake.text("set"), "wake set")
        let pending = try ProjectionFixture.require(wake["pending"]?.array, "wake pending")
        var expected = fixture.expected
        var turns = try ProjectionFixture.require(expected["turns"]?.array, "\(fixture.name) expected turns")
        turns = try turns.map { item in
            var turn = try ProjectionFixture.require(item.object, "\(fixture.name) turn")
            guard turn.removeValue(forKey: drop) != nil, turn[set] == nil else {
                throw ProjectionFixtureError.invalidFixture("\(fixture.name): stale wake overlay")
            }
            let id = try ProjectionFixture.require(turn["id"]?.string, "turn id")
            let isPending = pending.contains { reference in
                reference.text("case") == fixture.name && reference.text("turn") == id
            }
            turn[set] = .bool(isPending)
            return .object(turn)
        }
        var expectedObject = try ProjectionFixture.require(expected.object, "expected object")
        expectedObject["turns"] = .array(turns)
        expected = .object(expectedObject)
        if applyCasePatch {
            for entry in try ProjectionFixture.require(overlay["cases"]?.array, "overlay cases")
            where entry.text("name") == fixture.name {
                for (key, replacement) in entry["replace"]?.object ?? [:] {
                    expectedObject = try ProjectionFixture.require(expected.object, "replacement object")
                    guard expectedObject[key] != replacement else {
                        throw ProjectionFixtureError.invalidFixture("\(fixture.name): stale replace \(key)")
                    }
                    expectedObject[key] = replacement
                    expected = .object(expectedObject)
                }
                for (id, fields) in entry["turn_fields"]?.object ?? [:] {
                    var matched = 0
                    turns = try ProjectionFixture.require(expected["turns"]?.array, "turns")
                    turns = try turns.map { item in
                        var turn = try ProjectionFixture.require(item.object, "turn")
                        guard turn["id"]?.string == id else { return item }
                        matched += 1
                        for (key, replacement) in try ProjectionFixture.require(fields.object, "turn fields") {
                            guard turn[key] != replacement else {
                                throw ProjectionFixtureError.invalidFixture("\(fixture.name)/\(id): stale \(key)")
                            }
                            turn[key] = replacement
                        }
                        return .object(turn)
                    }
                    guard matched == 1 else {
                        throw ProjectionFixtureError.invalidFixture("\(fixture.name)/\(id): overlay must name one turn")
                    }
                    expectedObject = try ProjectionFixture.require(expected.object, "expected object")
                    expectedObject["turns"] = .array(turns)
                    expected = .object(expectedObject)
                }
            }
        }
        return try ProjectionFixture.require(expected["turns"], "turns")
    }

    private static func validateOverlay(_ overlay: ContinuityJSON, allCases: [ProjectionFixture]) throws {
        let entries = try ProjectionFixture.require(overlay["cases"]?.array, "overlay cases")
        var patchNames = Set<String>()
        let allowedKeys: Set<String> = ["name", "why", "source", "replace", "turn_fields"]
        for entry in entries {
            let name = try ProjectionFixture.require(entry.text("name"), "overlay name")
            guard patchNames.insert(name).inserted else {
                throw ProjectionFixtureError.invalidFixture("duplicate overlay case \(name)")
            }
            guard Set(entry.object?.keys.map { $0 } ?? []).isSubset(of: allowedKeys),
                  entry.text("why")?.isEmpty == false, entry["source"]?.array?.isEmpty == false else {
                throw ProjectionFixtureError.invalidFixture("\(name): malformed overlay rationale")
            }
        }
        guard patchNames.isSubset(of: Set(allCases.map(\.name))) else {
            throw ProjectionFixtureError.invalidFixture("overlay names a nonexistent case")
        }
        var allTurns = Set<String>()
        for fixture in allCases {
            let states = fixture.expected["checkpoints"]?.array ?? [fixture.expected]
            for state in states {
                for turn in state["turns"]?.array ?? [] {
                    let id = try ProjectionFixture.require(turn.text("id"), "overlay turn id")
                    allTurns.insert("\(fixture.name)/\(id)")
                }
            }
        }
        var references = Set<String>()
        for reference in try ProjectionFixture.require(
            overlay["turn_wake_state"]?["pending"]?.array, "pending overlay references"
        ) {
            let name = try ProjectionFixture.require(reference.text("case"), "pending case")
            let turn = try ProjectionFixture.require(reference.text("turn"), "pending turn")
            let key = "\(name)/\(turn)"
            guard references.insert(key).inserted, allTurns.contains(key) else {
                throw ProjectionFixtureError.invalidFixture("invalid pending overlay reference \(key)")
            }
        }
    }

    private static let staticTurnCases: Set<String> = [
        "absorbed_user_images", "background_running_badge", "closed_work_identity",
        "compaction_stays_inside_work", "empty_session", "empty_thinking_between_tools_is_retained",
        "failed_tool_counts_known_failure", "fold_counts_tools_and_thoughts",
        "frozen_snapshot_excludes_newer_items", "frozen_uses_numeric_block_order", "interrupt_is_trailing_notice",
        "last_result_for_call_wins", "late_result_attaches_to_original_call", "live_answer_after_wake",
        "live_empty_work", "live_queued_message_keeps_position", "live_reply_stays_after_work",
        "live_tail_runs_behind_chip", "long_answer_after_wake", "menu_owner_per_message",
        "missing_start_time_hides_duration", "missing_start_time_uses_first_step", "missing_tool_result_is_running",
        "monitor_event_uses_first_detail_line", "multiple_interactions_in_footer",
        "multiple_wakes_pick_latest_answer", "notification_without_wake",
        "older_page_stays_visible_during_freeze", "only_last_reply_in_each_wake_is_candidate",
        "open_queued_messages_work", "open_work_identity", "orphan_assistant", "orphan_tool_result_is_not_a_row",
        "orphan_wake_opens_with_notice", "ordinary_user_starts_next_turn", "photos_after_wake_stay_with_message",
        "plain_exchange", "queued_without_existing_turn", "recap_separates_turns", "regular_user_images",
        "short_reply_after_wake_is_the_answer", "single_interaction_in_footer", "standalone_empty_thinking_hidden",
        "task_notices_find_calls", "task_status_outcomes", "task_title_containing_interrupt_is_not_interrupt",
        "task_without_loaded_call_uses_title", "thinking_before_between_after_tools",
        "thinking_narration_preserved", "three_users_apart_without_steps", "timed_work_ends_at_answer",
        "tool_images_belong_to_call", "trailing_tool_after_answer", "unknown_item_is_ignored",
        "wake_after_recap_reopens_turn", "wake_without_any_reply", "finished_queued_messages_apart",
    ]

    private static let emptyTurnCases: Set<String> = [
        "empty_session", "single_interaction_in_footer", "multiple_interactions_in_footer",
    ]

    private static let taskIndexCases: Set<String> = [
        "task_notices_find_calls", "task_title_containing_interrupt_is_not_interrupt",
        "monitor_event_uses_first_detail_line", "task_without_loaded_call_uses_title", "task_status_outcomes",
    ]
}
