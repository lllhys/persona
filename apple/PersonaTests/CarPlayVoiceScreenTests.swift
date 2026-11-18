import AVFoundation
import Foundation
import Testing
@testable import Persona

/// The CarPlay voice screen's states and buttons, as data (CarPlayVoiceScreen): no CarPlay needed.
struct CarPlayVoiceScreenTests {
    @Test func everyStateHasOneOrTwoButtons() {
        for state in CarPlayVoiceScreen.states(endedReason: nil) {
            #expect((1...2).contains(state.buttons.count), "\(state.id) has \(state.buttons.count) buttons")
        }
    }

    @Test func everyStateHasTitlesAndAnIcon() {
        for state in CarPlayVoiceScreen.states(endedReason: "断开了") {
            #expect((1...3).contains(state.titles.count), "\(state.id)")
            #expect(state.titles.allSatisfy { !$0.isEmpty }, "\(state.id)")
            #expect(!state.icon.isEmpty, "\(state.id)")
        }
    }

    @Test func idsAreUnique() {
        let ids = CarPlayVoiceScreen.states(endedReason: "断开了").map(\.id)
        #expect(Set(ids).count == ids.count)
        #expect(ids.count == 5)
    }

    @Test func onlyTheEndedStateStopsRepeating() {
        for state in CarPlayVoiceScreen.states(endedReason: nil) {
            #expect(state.repeats == (state.id != "ended"), "\(state.id)")
        }
    }

    @Test func endedUsesTheReasonFirst() throws {
        let states = CarPlayVoiceScreen.states(endedReason: "网络断了")
        let ended = try #require(states.first { $0.id == "ended" })
        #expect(ended.titles.first == "网络断了")
        #expect(ended.titles.count == 2)
        #expect(ended.buttons == [.resume])
    }

    @Test func endedDropsAnEmptyReason() throws {
        for reason in [nil, "", "   "] as [String?] {
            let states = CarPlayVoiceScreen.states(endedReason: reason)
            let ended = try #require(states.first { $0.id == "ended" })
            #expect(ended.titles.first == "已结束", "reason \(String(describing: reason))")
            #expect(!ended.titles.contains(""))
        }
    }

    @Test func endedPointsAtSiriWithoutButtons() throws {
        let ended = try #require(CarPlayVoiceScreen.states(endedReason: "网络断了", buttonsShown: false).first { $0.id == "ended" })
        #expect(ended.titles == ["网络断了", CarPlayVoiceScreen.resumeBySiri])
        #expect(!ended.titles.contains { $0.contains("继续") })
        let withButtons = try #require(CarPlayVoiceScreen.states(endedReason: nil).first { $0.id == "ended" })
        #expect(withButtons.titles.last == "轻点「继续」再聊")
    }

    @Test func theInitialStateComesFirst() {
        let all = CarPlayVoiceScreen.states(endedReason: "网络断了").map(\.id)
        for id in all {
            let ordered = CarPlayVoiceScreen.states(endedReason: "网络断了", initial: id).map(\.id)
            #expect(ordered.first == id)
            #expect(Set(ordered) == Set(all))
            #expect(ordered.count == all.count)
        }
        #expect(CarPlayVoiceScreen.states(endedReason: nil, initial: "nope").map(\.id) == all)
    }

    @Test func anEndedTemplateOpensOnTheEndedState() throws {
        let ended = try #require(CarPlayVoiceScreen.states(endedReason: "没有麦克风权限", initial: "ended").first)
        #expect(ended.id == "ended")
        #expect(ended.titles.first == "没有麦克风权限")
    }

    // MARK: Starting by itself

    /// Which "became active" calls start the conversation, for a sequence of scene events.
    private func starts(_ events: [String]) -> [Bool] {
        var auto = CarPlayAutoStart()
        var out: [Bool] = []
        for event in events {
            if event == "active" { out.append(auto.takeOnBecomingActive()) } else { auto.arm() }
        }
        return out
    }

    @Test func startsOnceAfterConnecting() {
        #expect(starts(["active"]) == [false])
        #expect(starts(["connect", "active"]) == [true])
    }

    @Test func becomingActiveAgainDoesNotStartAgain() {
        // A banner, Siri or a call ending: resign active, become active, with no trip through the background.
        #expect(starts(["connect", "active", "active", "active", "active"]) == [true, false, false, false])
    }

    @Test func comingBackToTheForegroundStartsAgain() {
        // Connected, entering the foreground on launch, a banner went away, back from the background.
        #expect(starts(["connect", "foreground", "active", "active", "foreground", "active"]) == [true, false, true])
    }

    // MARK: Siri

    @Test func siriStartsOnlyWithTheScreenInTheCar() {
        #expect(CarPlayTalk.decide(connected: true, foreground: true) == .start)
        #expect(CarPlayTalk.decide(connected: true, foreground: false) == .notOnScreen)
        #expect(CarPlayTalk.decide(connected: false, foreground: false) == .notConnected)
        #expect(CarPlayTalk.decide(connected: false, foreground: true) == .notConnected)
    }

    /// Only with no car connected is Talk to Persona a call on the phone (#9290); with CarPlay connected and Persona not on
    /// its screen Siri still says to open it there (a call would need the phone unlocked while driving).
    @Test func onlyNoCarCallsOnThePhone() {
        #expect(CarPlayTalk.notConnected.callsOnThePhone)
        #expect(!CarPlayTalk.notOnScreen.callsOnThePhone)
        #expect(!CarPlayTalk.start.callsOnThePhone)
        #expect(CarPlayTalk.decide(connected: false, foreground: true).callsOnThePhone)
    }

    /// A car without Persona's scene is a car (#9290): Persona has a CarPlay scene only once it was opened on the car's
    /// screen since it launched (and only with its entitlement), so the audio route going to the car is enough for
    /// "open Persona there", never a call on the phone. Persona's own scene still decides `.start`.
    @Test func aCarWithoutPersonasSceneIsStillACar() {
        #expect(CarPlayTalk.decide(connected: false, foreground: false, carOnRoute: true) == .notOnScreen)
        #expect(!CarPlayTalk.decide(connected: false, foreground: false, carOnRoute: true).callsOnThePhone)
        #expect(CarPlayTalk.decide(connected: false, foreground: true, carOnRoute: true) == .notOnScreen)
        #expect(CarPlayTalk.decide(connected: true, foreground: true, carOnRoute: true) == .start)
        #expect(CarPlayTalk.decide(connected: true, foreground: false, carOnRoute: true) == .notOnScreen)
        #expect(CarPlayTalk.decide(connected: false, foreground: false, carOnRoute: false) == .notConnected)
    }

    /// Siri launching Persona cold while the car shows Maps: no scene of Persona's has connected in this process (the
    /// presence as the app starts), the phone's sound goes to the car. `talkNow`, which Talk to Persona asks, says to
    /// open Persona there; only with no car on the route is it a call on the phone. With Persona's scene there the route
    /// is not asked.
    @MainActor @Test func siriColdLaunchedInACarDoesNotCallOnThePhone() async {
        let saved = (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground)
        defer { (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground) = saved }
        (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground) = (false, false)
        let inACar = await CarPlayVoicePresence.talkNow(car: { true })
        #expect(inACar == .notOnScreen)
        #expect(!inACar.callsOnThePhone)
        let noCar = await CarPlayVoicePresence.talkNow(car: { false })
        #expect(noCar == .notConnected)
        #expect(noCar.callsOnThePhone)
        (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground) = (true, true)
        let onScreen = await CarPlayVoicePresence.talkNow(car: {
            Issue.record("the route asked with Persona's scene there")
            return false
        })
        #expect(onScreen == .start)
    }

    /// CarPlay's output is the car; Bluetooth hands-free (a car without CarPlay), the earpiece or nothing is not.
    @Test func onlyCarPlaysOutputIsACar() {
        #expect(CarPlayVoicePresence.isCar([.carAudio]))
        #expect(CarPlayVoicePresence.isCar([.builtInSpeaker, .carAudio]))
        #expect(!CarPlayVoicePresence.isCar([.bluetoothHFP]))
        #expect(!CarPlayVoicePresence.isCar([.builtInReceiver]))
        #expect(!CarPlayVoicePresence.isCar([]))
    }

    @Test func siriSaysSomethingDifferentForEachCase() {
        let sentences = [CarPlayTalk.start, .notConnected, .notOnScreen].map(\.sentence)
        #expect(Set(sentences).count == 3)
        #expect(sentences.allSatisfy { !$0.isEmpty })
    }

    @MainActor @Test func presenceNeedsTheForeground() {
        let saved = (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground)
        defer { (CarPlayVoicePresence.isConnected, CarPlayVoicePresence.isForeground) = saved }
        CarPlayVoicePresence.isConnected = true
        CarPlayVoicePresence.isForeground = false
        #expect(CarPlayVoicePresence.talk == .notOnScreen)
        CarPlayVoicePresence.isForeground = true
        #expect(CarPlayVoicePresence.talk == .start)
    }

    @Test func speakingCanBeInterrupted() throws {
        let states = CarPlayVoiceScreen.states(endedReason: nil)
        let speaking = try #require(states.first { $0.id == "speaking" })
        #expect(speaking.buttons == [.interrupt, .end])
    }

    @Test func buttonTitles() {
        #expect(CarPlayVoiceScreen.title(.end) == "结束")
        #expect(CarPlayVoiceScreen.title(.interrupt) == "打断")
        #expect(CarPlayVoiceScreen.title(.resume) == "继续")
    }

    @Test func buttonIconsAreRegistered() {
        #expect(CarPlayVoiceScreen.icon(.end) == Theme.Icon.end)
        #expect(CarPlayVoiceScreen.icon(.interrupt) == Theme.Icon.voiceInterruptSpeech)
        #expect(CarPlayVoiceScreen.icon(.resume) == Theme.Icon.voiceResume)
    }

    #if os(iOS)
    @Test func everyPhaseShowsAState() {
        let ids = Set(CarPlayVoiceScreen.states(endedReason: nil).map(\.id))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: nil)))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .connecting)))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .reconnecting(attempt: 1))))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .live(.listening))))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .live(.hearing))))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .live(.speaking))))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .live(.listening), tools: 1)))
        #expect(ids.contains(CarPlayVoiceScreen.stateID(for: .ended(.user))))
    }

    /// Connecting the car: no call yet but about to start, so the screen opens on connecting, not Ended.
    @Test func connectingTheCarOpensOnConnecting() {
        #expect(CarPlayVoiceScreen.openingStateID(for: nil, startingNow: true) == "connecting")
        #expect(CarPlayVoiceScreen.openingStateID(for: .ended(.idle), startingNow: true) == "connecting")
        #expect(CarPlayVoiceScreen.openingStateID(for: nil, startingNow: false) == "ended")
        #expect(CarPlayVoiceScreen.openingStateID(for: .ended(.lost), startingNow: false) == "ended")
        // Already running (started by Siri): the phase's own state.
        #expect(CarPlayVoiceScreen.openingStateID(for: .live(.speaking), startingNow: true) == "speaking")
        #expect(CarPlayVoiceScreen.openingStateID(for: .live(.listening), startingNow: false) == "listening")
    }

    @Test func stateIDsFollowThePhase() {
        #expect(CarPlayVoiceScreen.stateID(for: nil) == "ended")
        #expect(CarPlayVoiceScreen.stateID(for: .ended(.user)) == "ended")
        #expect(CarPlayVoiceScreen.stateID(for: .connecting) == "connecting")
        #expect(CarPlayVoiceScreen.stateID(for: .reconnecting(attempt: 2)) == "connecting")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.listening)) == "listening")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.hearing)) == "listening")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.speaking)) == "speaking")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.listening), tools: 1) == "working")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.listening), asking: true) == "working")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.listening), targetBusy: true) == "working")
        #expect(CarPlayVoiceScreen.stateID(for: .live(.listening), link: .paused) == "working")
    }

    @Test func endedHeadlinesCoverTheCommonEnds() {
        #expect(!CarPlayVoiceScreen.endedHeadline(for: .user).isEmpty)
        #expect(!CarPlayVoiceScreen.endedHeadline(for: .permissionDenied).isEmpty)
        #expect(CarPlayVoiceScreen.endedHeadline(for: .noHub) == CarPlayMicGate.noHub)
        #expect(CarPlayVoiceScreen.endedHeadline(for: .failed(code: nil, message: "上游断了")) == "上游断了")
    }
    #endif
}
