import Foundation
import Testing
@testable import Persona

/// The section bar's rules beyond the shared cases (#9053, `InboxBarFixtureTests`): what each choice draws, how Done
/// counts what the history loaded, and the page's landing record.
@MainActor
struct InboxBarTests {
    private func item(_ id: String, tier: String? = nil, status: String? = nil, done: Bool = false, kind: String = "dm") -> InboxItem {
        var it = InboxItem(id: id, source: "lark", kind: kind, account: "feishu", accountName: "feishu", title: id, preview: "", count: 1)
        it.tier = tier
        it.status = status
        if done { it.doneAt = Date(timeIntervalSince1970: 100) }
        return it
    }

    @Test func theInboxOpensOnNeedsYouAndNothingIsKept() {
        let state = InboxBarState()
        #expect(state.chosen == .action)
        #expect(state.saved == nil)
        #expect(state.query.isEmpty)
    }

    @Test func aChoiceDrawsItsSectionAloneAndCollapsedOnlyUnderAll() {
        for choice in InboxChoice.allCases {
            let tiers = InboxItem.Tier.allCases.filter { InboxBar.draws($0, chosen: choice) }
            switch choice {
            case .all: #expect(tiers == InboxItem.Tier.allCases)
            case .done: #expect(tiers.isEmpty)
            default: #expect(tiers == [choice.tier].compactMap { $0 }, "\(choice)")
            }
            #expect(InboxBar.drawsDone(chosen: choice) == (choice == .done || choice == .all), "\(choice)")
        }
        #expect(!InboxItem.Tier.allCases.contains { InboxBar.draws($0, chosen: .action) && $0 == .noise })
    }

    @Test func groupingAppliesWhereASectionHasGroups() {
        #expect(InboxChoice.action.groups)
        #expect(InboxChoice.fyi.groups)
        #expect(InboxChoice.all.groups)
        #expect(!InboxChoice.done.groups, "Done is always by when it was done")
        #expect(!InboxChoice.working.groups, "In Progress is never grouped")
    }

    @Test func doneCountsTheHistoryWhenTheListHasNone() {
        let filter = InboxProjection.Filter()
        let projection = InboxProjection(items: [item("a", tier: "action")], filter: filter)
        let none = InboxBar.Rows(projection, agents: 0, filter: filter, history: [], doneDays: nil)
        #expect(!none.hasDone)
        let loaded = InboxBar.Rows(projection, agents: 0, filter: filter, history: [item("old", done: true)], doneDays: nil)
        #expect(loaded.hasDone)
        #expect(InboxBar.entries(loaded, chosen: .action).map(\.choice) == [.action, .done, .all])
        // a search the history's rows don't match: nothing to show, and the hub's day counts don't apply
        let searched = InboxProjection.Filter(query: "zzz")
        let filtered = InboxBar.Rows(InboxProjection(items: [], filter: searched), agents: 0, filter: searched,
                                     history: [item("old", done: true)], doneDays: ["2026-01-01": 3])
        #expect(!filtered.hasDone)
    }

    @Test func theHubsDaysPutDoneOnTheBarOnlyWhenDoneCanListThem() {
        let filter = InboxProjection.Filter()
        let projection = InboxProjection(items: [item("a", tier: "action")], filter: filter)
        func rows(_ days: [String: Int]) -> InboxBar.Rows {
            InboxBar.Rows(projection, agents: 0, filter: filter, history: [], doneDays: days)
        }
        #expect(rows(["2026-01-01": 2]).hasDone)
        #expect(rows(["junk": 2, "2026-01-01": 0]).hasDone == false, "a day Done can't place, or none done that day")
        // the list asks the same question (`showsDone`) as the shared cases (`shown`)
        for days in [["2026-01-01": 2], ["junk": 2]] {
            let r = rows(days)
            #expect(InboxBar.showsDone(r, chosen: .done) == r.hasDone)
            #expect(InboxBar.shown(r, chosen: .done) == (r.hasDone ? ["done"] : []))
            #expect(InboxBar.entries(r, chosen: .action).contains { $0.choice == .done } == r.hasDone)
        }
        // the day groups the list draws from the same counts
        #expect(InboxGroups.dayCounts(["junk": 2], now: Date()).isEmpty)
    }

    @Test func aSectionShowsWhenItsCountHasARowAgentsIncluded() {
        var rows = InboxBar.Rows()
        rows.action = 2 // two waiting agents, no item
        #expect(InboxBar.shows(.action, rows, chosen: .action))
        #expect(InboxBar.shows(.action, rows, chosen: .all))
        #expect(!InboxBar.shows(.action, rows, chosen: .fyi))
        #expect(!InboxBar.shows(.fyi, rows, chosen: .all), "an empty section isn't drawn under All")
        #expect(InboxBar.shown(rows, chosen: .all) == ["action"])
    }

    @Test func theAgentKindCountsOnlyTheWaitingAgents() {
        let filter = InboxProjection.Filter(kind: "agent")
        let projection = InboxProjection(items: [item("a", tier: "action", kind: "agent"), item("f", tier: "fyi", kind: "agent")], filter: filter)
        let rows = InboxBar.Rows(projection, agents: 2, filter: filter, history: [], doneDays: nil)
        #expect(rows.action == 2)
        #expect(rows.fyi == 0)
        #expect(rows.filtered)
    }

    @Test func aChosenEmptySectionSaysSoUnlessNothingIsOpen() {
        var rows = InboxBar.Rows()
        rows.fyi = 3
        #expect(InboxBar.empty(rows, chosen: .action, source: "", kind: "", query: "") == .section)
        #expect(InboxBar.empty(rows, chosen: .fyi, source: "", kind: "", query: "") == nil)
        #expect(InboxBar.empty(rows, chosen: .all, source: "", kind: "", query: "") == nil)
        #expect(InboxBar.empty(InboxBar.Rows(), chosen: .working, source: "", kind: "", query: "") == .caughtUp)
        #expect(InboxBar.empty(InboxBar.Rows(), chosen: .done, source: "", kind: "", query: "") == .section,
                "Done with nothing never says the whole Inbox is caught up")
    }

    @Test func everyChosenEmptyHasItsOwnWords() {
        let titles = InboxChoice.allCases.map(InboxPage.emptyTitle)
        #expect(Set(titles).count == InboxChoice.allCases.count)
        #expect(titles.allSatisfy { !$0.isEmpty })
        #expect(InboxPage.emptyTitle(.all) == String(localized: "All Caught Up"))
    }

    /// The line under an empty choice is the contract's, word for word in each language (docs/assistant/inbox.md
    /// "Section bar"), so the three clients read alike.
    @Test func theLineUnderAnEmptyChoiceIsTheContracts() throws {
        #expect(InboxPage.emptyMessage(.done) == String(localized: "What you mark done shows up here."))
        for choice in [InboxChoice.action, .working, .fyi] {
            #expect(InboxPage.emptyMessage(choice) == String(localized: "The rest are in the other sections above."))
        }
        let words: [String: [String]] = [
            "en": ["The rest are in the other sections above.", "What you mark done shows up here."],
            "zh-Hans": ["其余的在上面的其他分区里。", "标为已处理的会出现在这里。"], // i18n-ignore: the contract's words
            "ja": ["残りは上のほかの区分にあります。", "対応済みにしたものがここに表示されます。"], // i18n-ignore: the contract's words
        ]
        for (language, expected) in words {
            let path = try #require(Bundle.main.path(forResource: language, ofType: "lproj"), "no \(language) strings")
            let bundle = try #require(Bundle(path: path))
            let got = ["The rest are in the other sections above.", "What you mark done shows up here."]
                .map { bundle.localizedString(forKey: $0, value: nil, table: nil) }
            #expect(got == expected, "\(language)")
        }
    }

    /// A choice landed on or tapped scrolls wholly into view, clear of the 24 pt fade at a cut edge, by the least it can;
    /// at the content's ends there is no fade to clear (docs/assistant/inbox.md "Section bar", `.edgeFade()`).
    @Test func aChosenChipScrollsClearOfTheFade() {
        // 300 pt in view of 500 pt of content, at its start
        let start = CapsuleReveal.Visible(offset: 0, width: 300, minOffset: 0, maxOffset: 200)
        #expect(CapsuleReveal.offset(revealing: 100...160, in: start, fade: 24) == nil, "already clear")
        #expect(CapsuleReveal.offset(revealing: 250...320, in: start, fade: 24) == 44, "past the end: clear of the trailing fade")
        #expect(CapsuleReveal.offset(revealing: 4...60, in: start, fade: 24) == nil, "the first chip at rest: no leading fade")
        let middle = CapsuleReveal.Visible(offset: 120, width: 300, minOffset: 0, maxOffset: 200)
        #expect(CapsuleReveal.offset(revealing: 110...170, in: middle, fade: 24) == 86, "under the leading fade")
        #expect(CapsuleReveal.offset(revealing: 430...496, in: middle, fade: 24) == 200, "the last chip: to the content's end")
        #expect(CapsuleReveal.offset(revealing: 4...60, in: middle, fade: 24) == 0, "the first chip: to the content's start")
        // a narrow strip fades a quarter of its width at most
        let narrow = CapsuleReveal.Visible(offset: 0, width: 80, minOffset: 0, maxOffset: 400)
        #expect(CapsuleReveal.offset(revealing: 100...140, in: narrow, fade: 24) == 80)
    }

    @Test func theLandingRecordFollowsTheItemInFront() {
        let landing = InboxBarLanding()
        let a = item("a", tier: "fyi")
        let b = item("b", tier: "noise")
        landing.appeared(a)
        landing.appeared(b) // a chain link pushed over a
        landing.disappeared(a) // a goes behind after the push
        #expect(landing.shown?.key == b.key)
        landing.disappeared(b)
        #expect(landing.shown == nil)
        // the place left with b open and brought back: b is in front again before its page appears
        landing.broughtBack()
        #expect(landing.shown?.key == b.key)
    }
}
