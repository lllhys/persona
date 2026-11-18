import Foundation
import Testing
@testable import Persona
import PersonaWire

/// Side sessions live in their parent's chat (#7944, `SideFold`): with the hub's `sessions_side_fold`, a row the hub
/// folds as a side (or as a row of a side's chat) is no row or count of its own, its parent shows and counts in its
/// folded state, and its cards are the parent's; without the feature nothing changes. Made-up rows, as a hub sends them.
@MainActor struct SideFoldTests {
    private static let on: Set<String> = [""]

    private func row(_ id: String, state: String = "idle", machine: String = "box", under: String? = nil, as kind: String? = nil,
                     side: String? = nil, updated: String = "2026-11-15T10:00:00Z", extra: [String: Any] = [:]) throws -> Session {
        var fields: [String: Any] = ["machine_id": machine, "session_id": id, "title": id, "cwd": "/code/app",
                                     "status": state == "offline" ? "offline" : "idle", "state": state, "updated_at": updated]
        if let under { fields["folded_under"] = under }
        if let kind { fields["folded_as"] = kind }
        if let side { fields["folded_side"] = side }
        fields.merge(extra) { _, new in new }
        return try JSON.decoder.decode(Session.self, from: JSONSerialization.data(withJSONObject: fields))
    }

    private func card(_ id: String, session: String, under: String? = nil, as kind: String? = nil, side: String? = nil) throws -> Interaction {
        var fields: [String: Any] = ["id": id, "machine_id": "box", "session_id": session, "kind": "permission"]
        if let under { fields["folded_under"] = under }
        if let kind { fields["folded_as"] = kind }
        if let side { fields["folded_side"] = side }
        return try JSON.decoder.decode(Interaction.self, from: JSONSerialization.data(withJSONObject: fields))
    }

    private func ids(_ sessions: [Session]) -> [String] { sessions.map(\.sessionID) }
    private func key(_ id: String) -> SessionKey { SessionKey(group: "", machineID: "box", sessionID: id) }

    // MARK: Decoding

    @Test func theHubsFieldsAreDecodedAndRoundTrip() throws {
        let s = try row("s", under: "p", as: "side", side: nil, extra: [
            "parent_agent_id": "p", "started_at": "2026-11-15T09:00:00Z",
            "children": ["total": 3, "working": 1, "sides": 2, "sides_working": 1, "sides_needs_you": 1, "sides_unread": 1],
        ])
        #expect(s.parentAgentID == "p" && s.foldedUnder == "p" && s.foldedAs == "side" && s.foldedSide == nil)
        #expect(s.startedAt != nil)
        #expect(s.children?.total == 3 && s.children?.sides == 2 && s.children?.sidesNeedsYou == 1 && s.children?.sidesUnread == 1)
        // The snapshot and the watch's envelope keep them.
        let again = try JSON.decoder.decode(Session.self, from: JSON.encoder.encode(s))
        #expect(again == s)
        let enveloped = try JSON.decoder.decode(Session.self, from: JSON.encoder.encode(NativeSessionEnvelope(session: s)))
        #expect(enveloped.foldedUnder == "p" && enveloped.foldedAs == "side" && enveloped.children == s.children)
    }

    @Test func oddValuesNeverDropTheRowOrTheCard() throws {
        let s = try row("s", extra: ["folded_under": 7, "folded_as": "", "folded_side": ["x"], "parent_agent_id": false,
                                     "started_at": 12, "children": ["total": "many", "sides": 2]])
        #expect(s.foldedUnder == nil && s.foldedAs == nil && s.foldedSide == nil && s.parentAgentID == nil && s.startedAt == nil)
        #expect(s.children?.total == 0 && s.children?.sides == 2)
        let ix = try JSON.decoder.decode(Interaction.self, from: Data(
            #"{"id":"c","machine_id":"box","session_id":"s","kind":"question","folded_under":3,"folded_as":"side","folded_side":""}"#.utf8))
        #expect(ix.foldedUnder == nil && ix.foldedAs == "side" && ix.foldedSide == nil)
        let ix2 = try card("c2", session: "s", under: "p", as: "side")
        let back = try JSON.decoder.decode(Interaction.self, from: JSON.encoder.encode(ix2))
        #expect(back.foldedUnder == "p" && back.foldedAs == "side" && back == ix2)
    }

    // MARK: Folding

    @Test func withoutTheFeatureNothingFolds() throws {
        let rows = try [row("p"), row("s", state: "needs_you", under: "p", as: "side")]
        let out = SideFold.fold(rows, groups: [])
        #expect(ids(out.listed) == ["p", "s"])
        #expect(out.listed == rows)
        #expect(SideFold.fold(rows, groups: ["another hub"]).listed == rows, "only the hubs that fold")
    }

    @Test func aSideAndTheRowsOfItsChatFoldUnderTheParent() throws {
        let rows = try [row("p"), row("s", state: "needs_you", under: "p", as: "side"),
                        row("child", state: "working", under: "p", side: "s"), row("other")]
        let out = SideFold.fold(rows, groups: Self.on)
        #expect(ids(out.listed) == ["p", "other"])
        #expect(out.listed[0].state == .needsYou, "a side that needs the user makes its parent need them")
        #expect(out.sides[key("p")].map(ids) == ["s"], "the sheet lists the sides, not the rows of their chats")
        #expect(out.home[key("s")] == key("p") && out.home[key("child")] == key("p"))
        #expect(out.chat[key("s")] == key("s") && out.chat[key("child")] == key("s"), "a side's child belongs to the side's chat")
        #expect(out.rollups[key("p")] == SideFold.Rollup(sides: 1, working: 1, needsYou: 1, unread: 0))
    }

    @Test func aSideOfASideFoldsUnderTheSameTopmostRow() throws {
        let rows = try [row("p"), row("s1", under: "p", as: "side"), row("s2", under: "p", as: "side", extra: ["parent_agent_id": "s1"])]
        let out = SideFold.fold(rows, groups: Self.on)
        #expect(ids(out.listed) == ["p"])
        #expect(Set(out.sides[key("p")].map(ids) ?? []) == ["s1", "s2"])
    }

    @Test func theParentsFoldedState() throws {
        func folded(_ parent: Session, _ side: Session) -> AgentState {
            SideFold.fold([parent, side], groups: Self.on).listed[0].state
        }
        #expect(try folded(row("p"), row("s", state: "working", under: "p", as: "side")) == .working)
        #expect(try folded(row("p", state: "unread"), row("s", state: "working", under: "p", as: "side")) == .unread,
                "a result not looked at yet stays in view")
        #expect(try folded(row("p", extra: ["awaits_reply": true]), row("s", state: "working", under: "p", as: "side")) == .idle,
                "it awaits the user's reply")
        #expect(try folded(row("p"), row("s", state: "unread", under: "p", as: "side")) == .unread, "a side's unread is the parent's")
        #expect(try folded(row("p"), row("c", state: "unread", under: "p", side: "s")) == .idle,
                "only sides make it unread, not the rows of their chats")
        #expect(try folded(row("p", state: "offline"), row("s", state: "unread", under: "p", as: "side")) == .unread,
                "the parent need not run")
    }

    @Test func aRowWhoseParentIsNotHeldStaysARow() throws {
        let rows = try [row("s", state: "needs_you", under: "elsewhere", as: "side"), row("p")]
        let out = SideFold.fold(rows, groups: Self.on)
        #expect(ids(out.listed) == ["s", "p"], "nothing out of sight: a side whose parent is on another page is a row")
        #expect(out.home.isEmpty)
    }

    @Test func otherFoldsAndOwnConversationsStayAsTheyWere() throws {
        let rows = try [row("lead"), row("worker", state: "working", under: "lead", as: "worker"),
                        row("a", extra: ["assistant": true]), row("as", under: "a", as: "side", extra: ["assistant": true])]
        let out = SideFold.fold(rows, groups: Self.on)
        #expect(ids(out.listed) == ["lead", "worker", "a", "as"], "a tab's worker and Persona's own conversations are no sides")
    }

    @Test func aPagedRowGoesByTheHubsRollup() throws {
        let p = try row("p", extra: ["children": ["total": 2, "sides": 2, "sides_needs_you": 1]])
        let out = SideFold.fold([p], groups: Self.on)
        #expect(out.listed[0].state == .needsYou)
        #expect(out.rollups[key("p")]?.sides == 2 && out.sides[key("p")] == nil)
    }

    @Test func foldingTwiceChangesNothing() throws {
        let rows = try [row("p", extra: ["pending_interactions": 1]),
                        row("s", state: "working", under: "p", as: "side", updated: "2026-11-15T11:00:00Z", extra: ["pending_interactions": 2]),
                        row("t", state: "unread", under: "p", as: "side")]
        let once = SideFold.fold(rows, groups: Self.on)
        #expect(once.listed[0].pendingInteractions == 3, "the sides' cards are the parent's")
        #expect(once.listed[0].updatedAt == rows[1].updatedAt, "the parent is as recent as its newest side")
        let twice = SideFold.fold(once.listed, groups: Self.on)
        #expect(twice.listed == once.listed)
        #expect(twice.rollups[key("p")] == once.rollups[key("p")])
    }

    @Test func theSheetsOrder() throws {
        let rows = try [row("p"),
                        row("old", under: "p", as: "side", updated: "2026-11-15T08:00:00Z"),
                        row("stopped", state: "offline", under: "p", as: "side", updated: "2026-11-15T12:00:00Z"),
                        row("new", under: "p", as: "side", updated: "2026-11-15T11:00:00Z"),
                        row("works", state: "working", under: "p", as: "side", updated: "2026-11-15T07:00:00Z"),
                        row("asks", state: "needs_you", under: "p", as: "side", updated: "2026-11-15T06:00:00Z")]
        let out = SideFold.fold(rows, groups: Self.on)
        #expect(out.sides[key("p")].map(ids) == ["asks", "works", "new", "old", "stopped"])
    }

    // MARK: Cards

    @Test func aSidesCardIsItsParentsAndSaysWhichSide() throws {
        let fold = SideFold.fold(try [row("p"), row("s", under: "p", as: "side")], groups: Self.on)
        let byHub = try card("c1", session: "s", under: "p", as: "side")
        #expect(byHub.requestHome == "p", "shown on its parent, as every folded child's card (#8010)")
        #expect(SideFold.chat(byHub, fold: fold, on: true) == key("s"))
        let ofChild = try card("c2", session: "child", under: "p", side: "s")
        #expect(ofChild.requestHome == "p" && SideFold.chat(ofChild, fold: fold, on: true) == key("s"))
        let bare = try card("c3", session: "s") // a hub that stamped nothing: the client's own fold
        #expect(SideFold.chat(bare, fold: fold, on: true) == key("s"))
        #expect(SideFold.chat(byHub, fold: fold, on: false) == nil, "without the feature no card is a side's")
        let own = try card("c4", session: "p")
        #expect(SideFold.chat(own, fold: fold, on: true) == nil)
    }

    @Test func needsYouCountsTheParentOnce() throws {
        let fold = SideFold.fold(try [row("p"), row("s", state: "needs_you", under: "p", as: "side")], groups: Self.on)
        let cards = try [card("c2", session: "s", under: "p", as: "side")]
        let rows = AgentsNeedsYou.rows(cards: cards, waiting: AgentsNeedsYou.waiting(fold.listed))
        #expect(rows.map(\.key) == [key("p")], "the parent needs you by its side, and the side's card is under it")
        #expect(rows[0].cards.map(\.id) == ["c2"])
    }

    // MARK: The list's projection

    @Test func aPagedWindowFoldsInTheProjection() throws {
        let rows = try [row("p"), row("s", state: "needs_you", under: "p", as: "side"), row("q")]
        var settings = AgentListProjection.Settings()
        settings.grouping = "recent"
        settings.sideFold = Self.on
        let list = AgentListProjection.build(.init(sessions: rows, machines: [], settings: settings))
        #expect(Set(ids(list.base)) == ["p", "q"])
        #expect(list.filterCounts[.needsYou] == 1, "the parent, once, by the side it holds")
        settings.sideFold = []
        let unfolded = AgentListProjection.build(.init(sessions: rows, machines: [], settings: settings))
        #expect(Set(ids(unfolded.base)) == ["p", "q"], "the hub's child fold (#8010) still takes the row out")
        #expect(unfolded.filterCounts[.needsYou] == nil || unfolded.filterCounts[.needsYou] == 0,
                "and goes by the hub's rollup alone, which this row has none of")
    }

    // MARK: The model

    private func model() throws -> AppModel {
        let model = AppModel(stateFolder: FileManager.default.temporaryDirectory.appending(path: "side-fold-" + UUID().uuidString))
        model.linkHooksForTests = (transport: try #require(FakeHub().client.transport), socket: { SyncSocket() })
        model.connectOnly(.demo)
        return model
    }

    @Test func theModelListsAndCountsTheParentOnly() throws {
        let model = try model()
        defer {
            model.link("")?.received(HubHello(features: ["sessions_list"])) // the remembered feature off again
        }
        let rows = try [row("p"), row("s", state: "needs_you", under: "p", as: "side", extra: ["pending_interactions": 1])]
        model.applySessions(Dictionary(grouping: rows, by: \.machineRef))
        model.replaceInteractions("", [try card("c", session: "s", under: "p", as: "side")])
        model.link("")?.received(HubHello(features: ["sessions_list"]))
        model.refoldSides()
        #expect(Set(ids(model.allSessions)) == ["p", "s"], "without the feature: as before")

        model.link("")?.received(HubHello(features: ["sessions_list", SideFold.feature]))
        model.refoldSides()
        #expect(ids(model.allSessions) == ["p"])
        #expect(model.allSessions[0].state == .needsYou)
        #expect(model.session(key("p"))?.state == .idle, "the parent's own page keeps its own state")
        #expect(model.session(key("s")) != nil, "the side's page still finds it")
        #expect(model.badges.agents == 1)
        let sides = model.sides(of: key("p"))
        #expect(sides.count == 1 && sides.needsYou && sides.rows.map(\.key) == [key("s")])
        #expect(model.interactions(for: key("p")).map(\.id) == ["c"], "its chat has the side's card")
        #expect(model.watchSummary().sessions.map(\.sessionID) == ["p"])
        #expect(model.watchSummary().sideFold == [""])

        // A link or a notification naming the side opens its parent with it shown.
        model.openChat = key("s")
        #expect(model.openChat == key("p"))
        #expect(model.sideLanding == SideLanding(parent: key("p"), side: key("s")))
    }

    @Test func seeingASideUpdatesTheParentAtOnce() throws {
        let model = try model()
        defer { model.link("")?.received(HubHello(features: ["sessions_list"])) }
        model.link("")?.received(HubHello(features: ["sessions_list", SideFold.feature]))
        let ref = MachineRef(group: "", id: "box")
        model.applySessions([ref: try [row("p"), row("s", state: "unread", under: "p", as: "side")]])
        #expect(model.allSessions.first?.state == .unread)
        let cell = model.sides(of: key("p"))
        #expect(cell.rollup.unread == 1)
        model.applySessions([ref: try [row("p"), row("s", state: "idle", under: "p", as: "side")]])
        #expect(model.allSessions.first?.state == .idle)
        #expect(model.sides(of: key("p")).rollup.unread == 0)
    }
}
