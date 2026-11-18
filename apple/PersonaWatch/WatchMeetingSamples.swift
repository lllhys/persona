#if DEBUG
// i18n-ignore-file: DEBUG-only made-up meetings for screenshots
import Foundation

/// `-showMeetingSample recording`: a meeting 12:34 into its recording on the watch (no microphone), its page open;
/// `running`: the same with the home list left in front, whose Recordings row then reads "Recording… Open";
/// `minutes` (or with either): two meetings' minutes as the phone relays them, one ready and one still being written.
enum WatchMeetingSamples {
    static let live = UUID(uuidString: "00000000-0000-0000-0000-000000000004")!

    @MainActor static func install(_ model: WatchModel) {
        guard let mode = UserDefaults.standard.string(forKey: "showMeetingSample") else { return }
        let start = Date(timeIntervalSince1970: 1_800_003_600)
        model.minutes = WatchMinutes(meetings: [
            WatchMinutes.Meeting(id: UUID(uuidString: "00000000-0000-0000-0000-000000000003")!, title: "产品周会", date: start,
                                 state: .ready,
                                 points: ["新版录音入口：目标是一步就能开始录", "锁屏和灵动岛能看到时长，也能直接停止"],
                                 decisions: ["默认用中文转写，中英混说也要能用"],
                                 actions: ["张三: 补上长录音的真机测试 · 周五前"]),
            WatchMinutes.Meeting(id: UUID(uuidString: "00000000-0000-0000-0000-000000000005")!, title: "设计评审",
                                 date: start.addingTimeInterval(7200), state: .writing),
        ])
        guard mode == "recording" || mode == "running" else { return }
        RecordingLibrary.shared.restore(live)
        var entry = RecordingEntry(id: live, createdAt: .now.addingTimeInterval(-754), source: .watch, active: true)
        entry.meeting = RecordingMeeting()
        entry.marks = [65]
        try? RecordingLibrary.shared.save(entry)
        RecordingCapture.shared.showSample(live, elapsed: 754)
        model.meetingAsked = mode == "recording"
    }
}
#endif
