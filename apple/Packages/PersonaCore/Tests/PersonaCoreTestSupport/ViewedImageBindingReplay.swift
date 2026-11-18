import Foundation
import PersonaCore

/// Real Swift/C replay of the viewed-image leaf. The ordinary expected object comes from the checked-in iOS overlay;
/// the other complete objects preserve the source-derived assertions in ViewedImageBindingFixtureTest (#8860/#8943).
/// Fixture input is never changed and expected objects are never generated from the projection under test.
extension ProjectionBindingReplay {
    public static func viewedImages() throws -> (completeObjects: Int, noLineBoundaries: Int) {
        var completeObjects = 0
        var noLineBoundaries = 0
        func assert(
            _ input: ContinuityJSON, _ expected: ContinuityJSON, _ label: String
        ) throws {
            let actual = try ProjectionFixture.project(input, label, using: PersonaCoreProjections.viewedImages)
            try ProjectionFixture.assertEqual(actual, expected, label)
            try ProjectionFixture.assertWrongExpectationIsRejected(
                actual, wrong: .object(["id": .string("wrong"), "image_ids": .array([]),
                                       "is_input": .bool(true), "type": .string("viewed")]),
                label: "\(label)/wrong expectation"
            )
            completeObjects += 1
        }
        func assertNoLine(_ input: ContinuityJSON, _ label: String) throws {
            do {
                let output = try PersonaCoreProjections.viewedImages(inputJSON: input.encoded())
                throw ProjectionFixtureError.invalidFixture("\(label): a no-line boundary returned \(output)")
            } catch PersonaCoreProjectionError.emptyResult {
                noLineBoundaries += 1
            }
        }

        let tool = try lineCase("tool_images_belong_to_call")
        let overlay = try ContinuityJSON.load(file: repositoryFixtures.appending(path: "lines/ios/differences.json"))
        let ordinaryExpected = try expectedOrdinaryViewedLine(overlay)
        try assert(tool.input, ordinaryExpected, "viewed_images/tool_images_belong_to_call")
        guard try rawViewedLine(tool.expected) != ordinaryExpected else {
            throw ProjectionFixtureError.invalidFixture("the ordinary iOS overlay no longer changes the viewed line")
        }

        // Same-image, failed-result and character-count identity cases are complete post-overlay objects, not just
        // image-ID fragments. A lazy ref has priority over the inline payload; metadata has priority over derived size.
        let duplicate = try mutatingTool(tool) { items in
            let image = try require(items.first { $0.text("kind") == "image" }, "ordinary image")
            items.append(image)
        }
        try assert(duplicate, ordinaryExpected, "viewed_images/duplicate result")
        try assert(try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string(""),
            "ref": .string("m1/s1/" + String(repeating: "x", count: 60)),
        ]) }, viewed("vi-2#0", ["d:m1/s1/" + String(repeating: "x", count: 60)]), "viewed_images/lazy ref")
        try assert(try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string("ignored"),
            "size": .number("900"), "width": .number("16"), "height": .number("10"),
        ]) }, viewed("vi-2#0", ["s:image/png:900:16x10"]), "viewed_images/size metadata")
        let combining = String(repeating: "e\u{301}", count: 64)
        for (data, dimensions, id, label) in [
            (String(repeating: "é", count: 64), false,
             "d:" + String(repeating: "é", count: 64) + "64", "unicode character count"),
            (combining, false, "d:" + combining + "64", "grapheme cluster count"),
            (combining, true, "s:image/png:48:4x3", "derived inline-data size"),
        ] {
            try assert(try changingToolImage(tool) { _ in
                var image: [String: ContinuityJSON] = ["mime": .string("image/png"), "data": .string(data)]
                if dimensions { image["width"] = .number("4"); image["height"] = .number("3") }
                return .object(image)
            }, viewed("vi-2#0", [id]), "viewed_images/\(label)")
        }
        let failedWithImage = try mutatingTool(tool) { values in
            values = try editTool(values, id: "4#0") { $0["is_error"] = .bool(true) }
        }
        try assert(failedWithImage, ordinaryExpected, "viewed_images/failed tool retains its image")

        let derived = try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string("iVBORw0KGgo="),
            "width": .number("16"), "height": .number("10"),
        ]) }
        try assert(derived, viewed("vi-2#0", ["s:image/png:9:16x10"]), "viewed_images/derived inline-data size")
        var explicitObject = try require(items(derived).first { $0.text("kind") == "image" }?.object, "explicit-size source")
        explicitObject["image"] = .object([
            "mime": .string("image/png"), "data": .string("iVBORw0KGgo="), "size": .number("9"),
            "width": .number("16"), "height": .number("10"),
        ])
        let explicitImageIndex = try require(items(derived).firstIndex(where: { $0.text("kind") == "image" }), "explicit-size image")
        let explicit = try replacingItem(derived, id: try require(items(derived)[explicitImageIndex].text("id"), "image id")) { item in
            item = explicitObject
        }
        let explicitImage = try require(items(explicit)[explicitImageIndex]["image"], "explicit-size payload")
        let derivedImage = try require(items(derived)[explicitImageIndex]["image"], "derived-size payload")
        guard explicitImage["size"] == .number("9"), derivedImage["size"] == nil else {
            throw ProjectionFixtureError.invalidFixture("explicit-size mutation is absent or already present in the derived input")
        }
        try assert(explicit, viewed("vi-2#0", ["s:image/png:9:16x10"]), "viewed_images/explicit inline-data size")
        guard try ProjectionFixture.project(explicit, "explicit identity", using: PersonaCoreProjections.viewedImages)
            == ProjectionFixture.project(derived, "derived identity", using: PersonaCoreProjections.viewedImages) else {
            throw ProjectionFixtureError.invalidFixture("explicit and derived size identities differ")
        }
        try assert(try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string("iVBORw0KGgo="), "size": .number("0"),
            "width": .number("16"), "height": .number("10"),
        ]) }, ordinaryExpected, "viewed_images/zero explicit size falls back")
        try assert(try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string(""), "width": .number("16"), "height": .number("10"),
        ]) }, viewed("vi-2#0", ["d:0"]), "viewed_images/empty inline-data identity")
        for dimensions in [(Int?, Int?)]([(nil, 10), (16, nil), (nil, nil)]) {
            try assert(try changingToolImage(tool) { _ in
                var image: [String: ContinuityJSON] = [
                    "mime": .string("image/png"), "data": .string("iVBORw0KGgo="),
                ]
                if let width = dimensions.0 { image["width"] = .number(String(width)) }
                if let height = dimensions.1 { image["height"] = .number(String(height)) }
                return .object(image)
            }, ordinaryExpected, "viewed_images/missing dimensions keep inline identity")
        }
        try assert(try changingToolImage(tool) { _ in .object([
            "mime": .string("image/png"), "data": .string("iVBORw0KGgo="),
            "ref": .string("https://example.com/image.png"),
        ]) }, viewed("vi-2#0", ["d:https://example.com/image.png"]), "viewed_images/ref precedes derived size")

        let fold = try lineCase("fold_counts_tools_and_thoughts")
        let foldImages: [ContinuityJSON] = [
            try foldImage(itemID: "5#0", toolUseID: "a", path: "/w/shot.png", size: 100, width: 4, height: 3),
            try foldImage(itemID: "8#0", toolUseID: "b", size: 900, width: 16, height: 10,
                          tool: "mcp__persona__computer_state", source: "screenshot"),
            try foldImage(toolUseID: "d", path: "/w/big.JPG"),
            try foldImage(itemID: "20#0", toolUseID: "g", size: 300, width: 8, height: 5,
                          tool: "mcp__persona__browser_look", source: "browser"),
        ]
        // ViewedImagesTests.aFoldGivesTheSameStripAsItsWork independently supplies these four FoldImage identities.
        let foldedExpected = viewed("vi-1#0", ["p:/w/shot.png", "s:image/png:900:16x10", "p:/w/big.JPG", "s:image/png:300:8x5"])
        try assert(try withFoldImages(fold, foldImages), foldedExpected, "viewed_images/folded Apple strip")
        try assert(try withFoldImages(fold, [
            try foldImage(itemID: "5#0", toolUseID: "a", width: 4, height: 3),
            try foldImage(toolUseID: "call_b", size: 0, width: 16, height: 10),
        ]), viewed("vi-1#0", ["i:5#0", "i:call_b"]), "viewed_images/folded identity fallback")
        try assert(try withFoldImages(fold, [foldImages[0], foldImages[0], foldImages[1], foldImages[2]]),
                   viewed("vi-1#0", ["p:/w/shot.png", "s:image/png:900:16x10", "p:/w/big.JPG"]),
                   "viewed_images/folded duplicate identity")
        try assertNoLine(fold.input, "viewed_images/fold without images")

        let queued = try queuedInput()
        try assert(queued, viewed("vi-1#0", ["p:/w/first.png"]), "viewed_images/queued text ends the first segment")
        let imageQueued = try replacingItem(queued, id: "5#0") { item in
            item["kind"] = .string("image")
            item.removeValue(forKey: "text")
            item["image"] = .object(["mime": .string("image/png"), "data": .string("aGVsbG8=")])
        }
        try assert(imageQueued, viewed("vi-1#0", ["p:/w/first.png"]), "viewed_images/queued image ends the first segment")
        let textAndImage = try replacingItem(queued, id: "5#0") { item in
            item["kind"] = .string("text")
        }
        let textAndImageInput = try insertingAfter(textAndImage, id: "5#0", newItem: .object([
            "id": .string("5#1"), "seq": .number("5"), "role": .string("user"), "kind": .string("image"),
            "queued": .bool(true), "image": .object(["mime": .string("image/png"), "data": .string("aGVsbG8=")]),
        ]))
        // #9024: absent optional fold payloads must not reject this queued text+image message. The complete viewed
        // object still ends at the next queued user segment; the original image belongs only to its first turn.
        let turn = try ProjectionFixture.project(textAndImageInput, "viewed_images/queued text+image turn", using: PersonaCoreProjections.turns)
        guard turn.array?.contains(where: {
            $0.text("id") == "1#0" && $0["user_ids"]?.array == [.string("1#0"), .string("5#0"), .string("5#1")]
        }) == true else {
            throw ProjectionFixtureError.invalidFixture("queued text+image did not retain both message blocks")
        }
        try assert(textAndImageInput, viewed("vi-1#0", ["p:/w/first.png"]), "viewed_images/queued text+image")

        // Additional existing JNI identity/order cases, retained so this wrapper replays the whole current slice.
        for (label, foldFirst, includeSize, expectedIDs) in [
            ("ordinary then folded Read identity", false, false, ["p:/w/shot.png"]),
            ("folded then ordinary Read identity", true, false, ["p:/w/shot.png"]),
            ("ordinary then folded first-seen order", false, true, ["p:/w/shot.png", "s:image/png:900:16x10"]),
            ("folded then ordinary first-seen order", true, true, ["s:image/png:900:16x10", "p:/w/shot.png"]),
        ] {
            try assert(try mixedInput(tool, foldFirst: foldFirst, includeSizeImage: includeSize),
                       viewed("vi-2#0", expectedIDs), "viewed_images/\(label)")
        }
        let readPath = try changingToolImage(tool) { image in
            image["mime"] = .string("image/png"); image["data"] = .string("AAAA")
            image["width"] = .number("4"); image["height"] = .number("3")
            image.removeValue(forKey: "ref")
            return .object(image)
        }
        try assert(try editingToolUse(readPath) { use in
            use["tool_name"] = .string("Read"); use["tool_input"] = .object(["file_path": .string("picture.png")])
        }, viewed("vi-2#0", ["p:picture.png"]), "viewed_images/Read path precedes derived size")
        try assert(try editingToolUse(readPath) { use in
            use["tool_name"] = .string("mcp__persona__computer_state")
            use["tool_input"] = .object(["machine": .string("m1")])
        }, viewed("vi-2#0", ["s:image/png:3:4x3"]), "viewed_images/non-Read derived size")

        for (label, update) in noLineMutations {
            try assertNoLine(try mutatingTool(tool, update), "viewed_images/\(label)")
        }
        try assertNoLine(.object(["items": .array([])]), "viewed_images/empty items")
        for status in ["running", "failed", "error"] {
            try assertNoLine(try mutatingTool(tool) { values in
                values = try editTool(values, id: "3#0") {
                    $0["tool_name"] = .string("Read"); $0["tool_input"] = .object(["file_path": .string("/w/a.png")])
                }
                values.removeAll { $0.text("kind") == "image" }
                let index = try require(values.firstIndex(where: { $0.text("kind") == "tool_result" }), "agent result")
                var result = try require(values[index].object, "result")
                result["task"] = .object(["kind": .string("agent"), "status": .string(status)])
                values[index] = .object(result)
                result.removeValue(forKey: "task")
                values.insert(.object(result), at: index + 1
                )
            }, "viewed_images/agent \(status) result")
        }
        try ProjectionFixture.rejectMalformed(PersonaCoreProjections.viewedImages, label: "viewed_images")
        let ordinaryActual = try ProjectionFixture.project(tool.input, "wrong ordinary owner", using: PersonaCoreProjections.viewedImages)
        try ProjectionFixture.assertWrongExpectationIsRejected(
            ordinaryActual, wrong: viewed("vi-fold:2#0", []), label: "viewed_images/wrong owner and identity"
        )
        let foldedActual = try ProjectionFixture.project(try withFoldImages(fold, foldImages), "wrong folded owner", using: PersonaCoreProjections.viewedImages)
        try ProjectionFixture.assertWrongExpectationIsRejected(
            foldedActual, wrong: viewed("vi-1#0", ["i:5#0"]), label: "viewed_images/wrong folded expectation"
        )
        let queuedActual = try ProjectionFixture.project(queued, "wrong queued owner", using: PersonaCoreProjections.viewedImages)
        try ProjectionFixture.assertWrongExpectationIsRejected(
            queuedActual, wrong: viewed("vi-5#0", ["p:/w/second.png"]), label: "viewed_images/wrong queued owner"
        )
        guard completeObjects == 28, noLineBoundaries == 8 else {
            throw ProjectionFixtureError.invalidFixture("viewed-image replay inventory changed: \(completeObjects)/\(noLineBoundaries)")
        }
        return (completeObjects, noLineBoundaries)
    }

    private static func expectedOrdinaryViewedLine(_ overlay: ContinuityJSON) throws -> ContinuityJSON {
        let entry = try require(overlay["cases"]?.array?.first { $0.text("name") == "tool_images_belong_to_call" }, "viewed overlay")
        return try require(entry["replace"]?["lines"]?.array?.first { $0.text("type") == "viewed" }, "post-overlay viewed line")
    }

    private static func rawViewedLine(_ expected: ContinuityJSON) throws -> ContinuityJSON {
        try require(expected["lines"]?.array?.first { $0.text("type") == "viewed" }, "raw viewed line")
    }

    private static func viewed(_ id: String, _ ids: [String]) -> ContinuityJSON {
        .object([
            "id": .string(id), "image_ids": .array(ids.map(ContinuityJSON.string)),
            "is_input": .bool(false), "type": .string("viewed"),
        ])
    }

    private static func lineCase(_ name: String) throws -> ProjectionFixture {
        try require(try ProjectionFixture.cases(in: "lines").first { $0.name == name }, name)
    }

    private static func items(_ input: ContinuityJSON) throws -> [ContinuityJSON] {
        try require(input["items"]?.array, "items")
    }

    private static func input(_ items: [ContinuityJSON]) -> ContinuityJSON { .object(["items": .array(items)]) }

    private static func mutatingTool(
        _ fixture: ProjectionFixture, _ update: (inout [ContinuityJSON]) throws -> Void
    ) throws -> ContinuityJSON {
        var values = try items(fixture.input)
        try update(&values)
        return input(values)
    }

    private static func editTool(
        _ values: [ContinuityJSON], id: String, update: (inout [String: ContinuityJSON]) throws -> Void
    ) throws -> [ContinuityJSON] {
        guard let index = values.firstIndex(where: {
            $0.text("id") == id && ["tool_use", "tool_result"].contains($0.text("kind") ?? "")
        }) else { throw ProjectionFixtureError.invalidFixture("missing tool mutation target \(id)") }
        var result = values
        var item = try require(result[index].object, id)
        try update(&item)
        result[index] = .object(item)
        return result
    }

    private static func editingToolUse(
        _ input: ContinuityJSON, _ update: (inout [String: ContinuityJSON]) throws -> Void
    ) throws -> ContinuityJSON {
        Self.input(try editTool(try items(input), id: "3#0", update: update))
    }

    private static func changingToolImage(
        _ fixture: ProjectionFixture, _ update: (inout [String: ContinuityJSON]) throws -> ContinuityJSON
    ) throws -> ContinuityJSON {
        var values = try items(fixture.input)
        guard let index = values.firstIndex(where: { $0.text("kind") == "image" && $0.text("tool_use_id") == "call_a" }),
              var image = values[index]["image"]?.object else {
            throw ProjectionFixtureError.invalidFixture("missing ordinary image mutation target")
        }
        values[index] = .object(try require(values[index].object, "image item").merging(["image": try update(&image)]) { _, new in new })
        return input(values)
    }

    private static func replacingItem(
        _ input: ContinuityJSON, id: String, update: (inout [String: ContinuityJSON]) throws -> Void
    ) throws -> ContinuityJSON {
        var values = try items(input)
        guard let index = values.firstIndex(where: { $0.text("id") == id }) else {
            throw ProjectionFixtureError.invalidFixture("missing item \(id)")
        }
        var item = try require(values[index].object, id)
        try update(&item)
        values[index] = .object(item)
        return Self.input(values)
    }

    private static func insertingAfter(_ source: ContinuityJSON, id: String, newItem: ContinuityJSON) throws -> ContinuityJSON {
        var values = try items(source)
        guard let index = values.firstIndex(where: { $0.text("id") == id }) else {
            throw ProjectionFixtureError.invalidFixture("missing insertion anchor \(id)")
        }
        values.insert(newItem, at: index + 1)
        return input(values)
    }

    private static func foldImage(
        itemID: String? = nil, toolUseID: String, path: String? = nil,
        size: Int? = nil, width: Int? = nil, height: Int? = nil,
        tool: String = "Read", source: String = "read"
    ) throws -> ContinuityJSON {
        var value: [String: ContinuityJSON] = [
            "tool_use_id": .string(toolUseID), "tool": .string(tool), "source": .string(source), "mime": .string("image/png"),
        ]
        if let itemID { value["item_id"] = .string(itemID) }
        if let path { value["path"] = .string(path) }
        if let size { value["size"] = .number(String(size)) }
        if let width { value["width"] = .number(String(width)) }
        if let height { value["height"] = .number(String(height)) }
        return .object(value)
    }

    private static func withFoldImages(_ fixture: ProjectionFixture, _ images: [ContinuityJSON]) throws -> ContinuityJSON {
        var values = try items(fixture.input)
        guard let index = values.firstIndex(where: { $0.text("kind") == "fold" }) else {
            throw ProjectionFixtureError.invalidFixture("missing fold mutation target")
        }
        var item = try require(values[index].object, "fold item")
        var fold = try require(item["fold"]?.object, "fold payload")
        fold["images"] = .array(images)
        item["fold"] = .object(fold)
        values[index] = .object(item)
        return input(values)
    }

    private static func mixedInput(
        _ fixture: ProjectionFixture, foldFirst: Bool, includeSizeImage: Bool
    ) throws -> ContinuityJSON {
        var values = try editingToolUse(fixture.input) { use in
            use["tool_name"] = .string("Read"); use["tool_input"] = .object(["file_path": .string("/w/shot.png")])
        }
        var source = try items(values)
        let answer = try require(source.popLast(), "mixed-input answer")
        let imageIndex = try require(source.firstIndex(where: { $0.text("kind") == "image" && $0.text("tool_use_id") == "call_a" }), "mixed-input image")
        var image = try require(source[imageIndex].object, "mixed-input image object")
        image["image"] = .object([
            "mime": .string("image/png"), "data": .string(""), "ref": .string("m1/s1/5#0"),
            "size": .number("100"), "width": .number("4"), "height": .number("3"),
        ])
        source[imageIndex] = .object(image)
        var images: [ContinuityJSON] = []
        let sized = try foldImage(itemID: "8#0", toolUseID: "b", size: 900, width: 16, height: 10)
        let path = try foldImage(itemID: "5#0", toolUseID: "a", path: "/w/shot.png", size: 100, width: 4, height: 3)
        if includeSizeImage && foldFirst { images.append(sized) }
        images.append(path)
        if includeSizeImage && !foldFirst { images.append(sized) }
        let fold = ContinuityJSON.object([
            "id": .string("fold:10#0"), "seq": .number("10"), "role": .string("assistant"), "kind": .string("fold"),
            "ts": .string("2026-01-01T00:00:10Z"), "fold": .object([
                "items": .number("1"), "tools": .number("1"), "thoughts": .number("0"),
                "through_seq": .number("11"), "through_id": .string("11#0"), "until": .string("2026-01-01T00:00:11Z"),
                "images": .array(images),
            ]),
        ])
        let insertion = try require(source.firstIndex(where: { $0.text("kind") == "tool_use" }), "mixed-input tool anchor")
        source.insert(fold, at: foldFirst ? insertion : source.count)
        var answerObject = try require(answer.object, "answer")
        answerObject["seq"] = .number("20"); answerObject["ts"] = .string("2026-01-01T00:00:20Z")
        source.append(.object(answerObject))
        values = input(source)
        let turns = try ProjectionFixture.project(values, "viewed_images/mixed turn", using: PersonaCoreProjections.turns)
        guard turns.array?.contains(where: {
            $0["user_ids"]?.array == [.string("2#0")] && ($0["ordered_ids"]?.array?.contains(.string(foldFirst ? "tools-fold:10#0" : "tools-3#0")) == true)
        }) == true else { throw ProjectionFixtureError.invalidFixture("mixed-input turn owner differs") }
        return values
    }

    private static func queuedInput() throws -> ContinuityJSON {
        let fixture = try lineCase("finished_queued_messages_apart")
        var values = try items(fixture.input)
        for (id, path) in [("3#0", "/w/first.png"), ("7#0", "/w/second.png")] {
            values = try editTool(values, id: id) { use in
                use["tool_name"] = .string("Read"); use["tool_input"] = .object(["file_path": .string(path)])
            }
        }
        values.append(.object([
            "id": .string("12#0"), "seq": .number("12"), "role": .string("user"), "kind": .string("tool_result"),
            "tool_use_id": .string("b"), "text": .string("ok"),
        ]))
        return input(values)
    }

    private static var noLineMutations: [(String, @Sendable (inout [ContinuityJSON]) throws -> Void)] { [
        ("running Read without images", { values in
            values = try editTool(values, id: "3#0") {
                $0["tool_name"] = .string("Read"); $0["tool_input"] = .object(["file_path": .string("picture.png")])
            }
            values.removeAll { $0.text("kind") == "tool_result" || $0.text("kind") == "image" }
        }),
        ("failed Read without images", { values in
            values = try editTool(values, id: "3#0") {
                $0["tool_name"] = .string("Read"); $0["tool_input"] = .object(["file_path": .string("picture.png")])
            }
            values.removeAll { $0.text("kind") == "image" }
            values = try editTool(values, id: "4#0") { $0["is_error"] = .bool(true) }
        }),
        ("unsupported Read path", { values in
            values = try editTool(values, id: "3#0") {
                $0["tool_name"] = .string("Read"); $0["tool_input"] = .object(["file_path": .string("document.txt")])
            }
            values.removeAll { $0.text("kind") == "image" }
        }),
    ] }

    private static func require<T>(_ value: T?, _ label: String) throws -> T {
        try ProjectionFixture.require(value, label)
    }
}
