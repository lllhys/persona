import Foundation

/// Retains a JSON number's exact source token for fixture comparisons. Foundation's NSNumber bridge can round a large
/// integer before it exposes a string, so this small test-only scanner validates each token before Foundation decodes.
/// Canonical integer spellings compare numerically; every other spelling is compared as its exact source token.
enum ExactJSONNumbers {
    static func validate(data: Data) throws {
        var scanner = Scanner(data)
        try scanner.value()
        try scanner.end()
        // A lexeme accepted by the scanner can still be rounded when Foundation materializes NSNumber. Every integer
        // token must survive that conversion exactly; decimals/exponents use the scanner's bounded grammar instead.
        for token in scanner.integerTokens {
            guard let object = try? JSONSerialization.jsonObject(with: Data(token.utf8), options: [.fragmentsAllowed]),
                  let number = object as? NSNumber,
                  normalizedInteger(number.stringValue) == normalizedInteger(token) else {
                throw ContinuityFixtureError.invalidFixture("JSON integer loses precision during decoding")
            }
        }
    }

    static func isValid(_ text: String) -> Bool {
        (try? validate(data: Data(text.utf8))) != nil
    }

    static func normalizedInteger(_ text: String) -> String? {
        guard !text.isEmpty, text.first != "-", text.allSatisfy(\.isNumber) else { return nil }
        let digits = text.drop(while: { $0 == "0" })
        return digits.isEmpty ? "0" : String(digits)
    }
}

private struct Scanner {
    let data: Data
    var index = 0
    var integerTokens: [String] = []

    init(_ data: Data) { self.data = data }

    mutating func value() throws {
        whitespace()
        guard let byte = peek() else { throw ContinuityFixtureError.invalidFixture("truncated JSON") }
        switch byte {
        case 123: try object()
        case 91: try array()
        case 34: try string()
        case 116: try literal("true")
        case 102: try literal("false")
        case 110: try literal("null")
        case 45, 48...57: try number()
        default: throw ContinuityFixtureError.invalidFixture("invalid JSON value")
        }
    }

    mutating func object() throws {
        try expect(123)
        whitespace()
        if take(125) { return }
        repeat {
            whitespace()
            guard peek() == 34 else { throw ContinuityFixtureError.invalidFixture("object key is not a string") }
            try string()
            whitespace()
            try expect(58)
            try value()
            whitespace()
        } while take(44)
        try expect(125)
    }

    mutating func array() throws {
        try expect(91)
        whitespace()
        if take(93) { return }
        repeat {
            try value()
            whitespace()
        } while take(44)
        try expect(93)
    }

    mutating func string() throws {
        try expect(34)
        var escaped = false
        while let byte = peek() {
            index += 1
            if escaped {
                if byte == 117 {
                    for _ in 0..<4 { guard let digit = peek(), digit.isHexDigit else {
                        throw ContinuityFixtureError.invalidFixture("invalid JSON Unicode escape")
                    }; index += 1 }
                } else if ![34, 47, 92, 98, 102, 110, 114, 116].contains(byte) {
                    throw ContinuityFixtureError.invalidFixture("invalid JSON string escape")
                }
                escaped = false
            } else if byte == 92 {
                escaped = true
            } else if byte == 34 {
                return
            } else if byte < 0x20 {
                throw ContinuityFixtureError.invalidFixture("control character in JSON string")
            }
        }
        throw ContinuityFixtureError.invalidFixture("unterminated JSON string")
    }

    mutating func number() throws {
        let start = index
        _ = take(45)
        if take(48) {
            guard peek() == nil || !(48...57).contains(peek()!) else {
                throw ContinuityFixtureError.invalidFixture("leading zero in JSON number")
            }
        } else {
            try digits(required: true)
        }
        if take(46) { try digits(required: true) }
        if peek() == 101 || peek() == 69 {
            index += 1
            if peek() == 43 || peek() == 45 { index += 1 }
            try digits(required: true)
        }
        let text = String(decoding: data[start..<index], as: UTF8.self)
        guard let mantissaEnd = text.firstIndex(where: { $0 == "." || $0 == "e" || $0 == "E" }) else {
            // Arbitrarily long integers remain comparable, not silently converted through a bounded numeric type.
            integerTokens.append(text)
            return
        }
        let exponentText = text[text.index(after: mantissaEnd)...]
        guard let exponent = Int(exponentText), abs(exponent) <= 100_000 else {
            throw ContinuityFixtureError.invalidFixture("JSON exponent is out of range")
        }
        let mantissa = text[..<mantissaEnd]
        let fractionDigits = mantissa.reversed().firstIndex(of: ".") ?? 0
        let decimalPlaces = exponent - fractionDigits
        guard decimalPlaces >= 0 && decimalPlaces <= 10_000 else {
            throw ContinuityFixtureError.invalidFixture("JSON decimal precision is out of range")
        }
        var normalized = mantissa.replacingOccurrences(of: ".", with: "")
        if decimalPlaces > 0 { normalized += String(repeating: "0", count: decimalPlaces) }
        guard normalized.allSatisfy(\.isNumber) else {
            throw ContinuityFixtureError.invalidFixture("invalid JSON decimal")
        }
        // Bounded decimal allocation is enough for supported projection fields; huge exponents fail explicitly.
    }

    mutating func digits(required: Bool) throws {
        let start = index
        while let byte = peek(), (48...57).contains(byte) { index += 1 }
        if required && start == index { throw ContinuityFixtureError.invalidFixture("missing JSON digits") }
    }

    mutating func literal(_ text: String) throws {
        for byte in text.utf8 { try expect(byte) }
    }

    mutating func whitespace() {
        while let byte = peek(), [9, 10, 13, 32].contains(byte) { index += 1 }
    }

    mutating func end() throws {
        whitespace()
        guard index == data.count else { throw ContinuityFixtureError.invalidFixture("trailing JSON content") }
    }

    func peek() -> UInt8? { index < data.count ? data[index] : nil }

    mutating func expect(_ expected: UInt8) throws {
        guard take(expected) else { throw ContinuityFixtureError.invalidFixture("unexpected JSON byte") }
    }

    mutating func take(_ expected: UInt8) -> Bool {
        guard peek() == expected else { return false }
        index += 1
        return true
    }
}

private extension UInt8 {
    var isHexDigit: Bool { (48...57).contains(self) || (65...70).contains(self) || (97...102).contains(self) }
}
