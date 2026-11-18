import Foundation
import Testing
@testable import Persona

/// UI version 6's new fields' values on iOS (#827): dates and times as the wire writes them in the page's time zone,
/// `earliest`…`latest`, and several choices as a JSON array in the choices' order.
struct ExtensionFieldValuesTests {
    private let zone = TimeZone(identifier: "Asia/Shanghai")!

    @Test func datesAndTimesRoundTrip() throws {
        for (kind, s) in [("date", "2026-11-11"), ("time", "07:30"), ("datetime", "2026-11-11T07:30")] {
            let d = try #require(ExtensionFieldValues.date(s, kind: kind, zone: zone), "\(kind) \(s)")
            #expect(ExtensionFieldValues.text(d, kind: kind, zone: zone) == s)
        }
        #expect(ExtensionFieldValues.date("2026-13-40", kind: "date", zone: zone) == nil, "not a date")
        #expect(ExtensionFieldValues.date("7:30pm", kind: "time", zone: zone) == nil, "24 hours, as the wire writes it")
        #expect(ExtensionFieldValues.date("", kind: "date") == nil && ExtensionFieldValues.date("x", kind: "text") == nil)
    }

    @Test func aTimeIsOnTheGivenDay() throws {
        let day = try #require(ExtensionFieldValues.date("2026-11-11", kind: "date", zone: zone))
        let t = try #require(ExtensionFieldValues.date("23:15", kind: "time", zone: zone, day: day))
        #expect(ExtensionFieldValues.text(t, kind: "datetime", zone: zone) == "2026-11-11T23:15")
    }

    @Test func theRangeHoldsADate() throws {
        let r = ExtensionFieldValues.range(earliest: "2026-11-01", latest: "2026-11-30", kind: "date", zone: zone)
        let early = try #require(ExtensionFieldValues.date("2026-10-01", kind: "date", zone: zone))
        #expect(ExtensionFieldValues.text(ExtensionFieldValues.clamp(early, r), kind: "date", zone: zone) == "2026-11-01")
        let backwards = ExtensionFieldValues.range(earliest: "2026-11-30", latest: "2026-11-01", kind: "date", zone: zone)
        #expect(backwards.from == nil && backwards.to == nil, "the wrong way round allows any")
    }

    @Test func severalChoices() {
        #expect(ExtensionFieldValues.chosen(#"["b","a"]"#) == ["b", "a"])
        #expect(ExtensionFieldValues.chosen("b,a").isEmpty && ExtensionFieldValues.chosen(nil).isEmpty, "only a JSON array")
        #expect(ExtensionFieldValues.multichoice(["c", "a"], order: ["a", "b", "c"]) == #"["a","c"]"#, "in the choices' order")
        #expect(ExtensionFieldValues.multichoice([], order: ["a"]) == "[]")
        #expect(ExtensionFieldValues.multichoice(["a/b"], order: ["a/b"]) == #"["a/b"]"#)
    }
}
