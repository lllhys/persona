import Foundation
import PersonaCore

/// Fixture-only JSON support. Expectations come from the checked-in files, never from the Rust projection.
/// Integer equality is exact; objects compare by key, arrays by position, and JSON booleans never equal numbers.
public enum ProjectionFixtureError: Error, CustomStringConvertible, Equatable {
    case invalidFixture(String)
    case mismatch(String)

    public var description: String {
        switch self {
        case let .invalidFixture(detail): "Invalid projection fixture: \(detail)"
        case let .mismatch(detail): "Projection mismatch: \(detail)"
        }
    }
}

public struct ProjectionFixture {
    public let name: String
    public let input: ContinuityJSON
    public let expected: ContinuityJSON

    public static func fixtures(in domain: String) throws -> [ContinuityJSON] {
        let directory = repositoryFixtures.appending(path: domain)
        let files = try FileManager.default.contentsOfDirectory(at: directory, includingPropertiesForKeys: nil)
            .filter { $0.pathExtension == "json" }.sorted { $0.lastPathComponent < $1.lastPathComponent }
        guard !files.isEmpty else { throw ProjectionFixtureError.invalidFixture("no JSON files in \(domain)") }
        return try files.map { try ContinuityJSON.load(file: $0) }
    }

    public static func cases(in domain: String, operation: String? = nil) throws -> [ProjectionFixture] {
        var result: [ProjectionFixture] = []
        for document in try fixtures(in: domain) {
            if let operation, document.text("operation") != operation { continue }
            for item in try require(document["cases"]?.array, "cases") {
                guard let name = item.text("name"), let input = item["input"], let expected = item["expected"] else {
                    throw ProjectionFixtureError.invalidFixture("name/input/expected in \(domain)")
                }
                result.append(ProjectionFixture(name: name, input: input, expected: expected))
            }
        }
        return result
    }

    public static func exactManifest(
        _ fixtures: [ProjectionFixture], _ expectedNames: Set<String>, _ domain: String
    ) throws {
        var names = Set<String>()
        for fixture in fixtures {
            guard names.insert(fixture.name).inserted else {
                throw ProjectionFixtureError.invalidFixture("duplicate \(domain) case: \(fixture.name)")
            }
        }
        guard names == expectedNames else {
            throw ProjectionFixtureError.invalidFixture("\(domain) case manifest differs: \(names)")
        }
    }

    public static func assertEqual(
        _ actual: ContinuityJSON, _ expected: ContinuityJSON, _ label: String
    ) throws {
        guard actual == expected else {
            throw ProjectionFixtureError.mismatch(
                "\(label)\nactual: \(try actual.encoded())\nexpected: \(try expected.encoded())"
            )
        }
    }

    public static func assertWrongExpectationIsRejected(
        _ actual: ContinuityJSON, wrong: ContinuityJSON, label: String
    ) throws {
        do {
            try assertEqual(actual, wrong, label)
        } catch ProjectionFixtureError.mismatch(let detail) {
            guard detail.contains(label) else {
                throw ProjectionFixtureError.invalidFixture("negative comparison lacks its case label")
            }
            return
        }
        throw ProjectionFixtureError.invalidFixture("\(label): a wrong expectation compared equal")
    }

    public static func decode(_ json: String, _ label: String) throws -> ContinuityJSON {
        guard let data = json.data(using: .utf8), !data.isEmpty else {
            throw ProjectionFixtureError.invalidFixture("\(label): the C binding returned no JSON")
        }
        try ExactJSONNumbers.validate(data: data)
        return try ContinuityJSON.from(JSONSerialization.jsonObject(with: data, options: [.fragmentsAllowed]))
    }

    public static func project(
        _ input: ContinuityJSON, _ label: String,
        using projection: (String) throws -> String
    ) throws -> ContinuityJSON {
        try decode(projection(try input.encoded()), label)
    }

    public static func rejectMalformed(
        _ projection: (String) throws -> String, label: String
    ) throws {
        for malformed in [#"{"invalid":true}"#, "not JSON", ""] {
            do {
                let output = try projection(malformed)
                throw ProjectionFixtureError.invalidFixture("\(label): malformed input returned \(output)")
            } catch PersonaCoreProjectionError.emptyResult {
                // The C ABI's specified failure result is mapped to an error, never treated as an empty JSON value.
            }
        }
    }

    public static func require<T>(_ value: T?, _ label: String) throws -> T {
        guard let value else { throw ProjectionFixtureError.invalidFixture("missing \(label)") }
        return value
    }
}

public let repositoryFixtures = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent() // PersonaCoreTestSupport
    .deletingLastPathComponent() // Tests
    .deletingLastPathComponent() // PersonaCore package
    .deletingLastPathComponent() // Packages
    .deletingLastPathComponent() // apple
    .deletingLastPathComponent() // repository
    .appending(path: "spec/fixtures")
