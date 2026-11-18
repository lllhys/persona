import SwiftUI
#if os(iOS)
import UIKit.UIGestureRecognizerSubclass
#endif

// Moving a to-do on the calendar (#2045, docs/assistant/todos.md "Calendar", "Moving one"): picked up by a finger held
// still or by a pointer that moves, carried to another time, day or All Day row, and saved when it is let go, with Undo
// in the toast that says where it went. What a drop means is `TodoCalendar` (Shared); here are the gesture, where on
// the grids a finger is, the save, and the way without dragging (VoiceOver's "Change Due Date…"). iOS and iPadOS
// only: the Mac app is frozen.

/// A to-do being dragged: which, where the finger is (in the grid's coordinate space), and how far below the top edge
/// of its block or chip it was picked up: the block keeps that offset as it moves, so it stays under the finger.
struct CalendarDragging: Equatable {
    var entry: CalendarEntry
    var point: CGPoint
    var grab: CGFloat
    /// It has moved since it was picked up: until then it lands nowhere, so letting go of a held one changes nothing
    /// (as on the web).
    var moved = false
}

/// What the grids do with a to-do that is moved (`TodoCalendarView`); without it nothing is dragged.
struct CalendarMoves {
    /// Let go somewhere it can land.
    var drop: (CalendarEntry, CalendarDrop) -> Void
    /// The way without dragging: VoiceOver's and Switch Control's "Change Due Date…".
    var reschedule: (CalendarEntry) -> Void
    /// A drag starts (true) or ends: while one is carried, a sideways move doesn't turn the page.
    var carrying: (Bool) -> Void
}

/// Where on a day or week's time axis a dragged to-do would land, from the parts' frames in the timeline's coordinate
/// space: on the axis its top edge `grab` above the finger, in the day's column under the finger; on a day's All Day
/// row, all day; nowhere above the row or below the axis.
nonisolated struct TimelineDropMap: Equatable {
    /// The timeline's width, the hours' column at its left, how many days.
    var width: CGFloat = 0
    var gutter: CGFloat = 0
    var days = 1
    /// The All Day row, and the part of the axis in sight (the scroll view), and how far the axis is scrolled.
    var allDay = CGRect.zero
    var axis = CGRect.zero
    var scrolled: CGFloat = 0
    /// An hour's height on the axis.
    var hour: CGFloat = 1

    nonisolated enum Spot: Equatable {
        case allDay(day: Int)
        case axis(day: Int, minutes: Double)
    }

    /// The day column under `x`: the nearest one when it is over the hours.
    func day(_ x: CGFloat) -> Int {
        let n = max(days, 1)
        let column = (width - gutter) / CGFloat(n)
        guard column > 0 else { return 0 }
        return min(max(Int(((x - gutter) / column).rounded(.down)), 0), n - 1)
    }

    func spot(_ p: CGPoint, grab: CGFloat) -> Spot? {
        if p.y > axis.maxY { return nil }
        if p.y >= axis.minY {
            let top = p.y - grab - axis.minY + scrolled
            return .axis(day: day(p.x), minutes: Double(top / hour) * 60)
        }
        if p.y >= allDay.minY { return .allDay(day: day(p.x)) }
        return nil
    }

    /// Which way the axis scrolls while a finger is near its top or bottom edge (within `edge`): -1 up, 1 down, 0 not.
    /// Below the axis it keeps going down; over the All Day row it stays.
    func autoscroll(_ p: CGPoint, edge: CGFloat) -> Int {
        guard axis.height > edge * 2 else { return 0 }
        if p.y > axis.maxY - edge { return 1 }
        if p.y >= axis.minY && p.y < axis.minY + edge { return -1 }
        return 0
    }
}

/// Where on a month a dragged to-do would land: the day under the finger, from the grid's part in sight (the scroll
/// view, in the month's coordinate space), how far it is scrolled and a day's height.
nonisolated struct MonthDropMap: Equatable {
    var grid = CGRect.zero
    var scrolled: CGFloat = 0
    var cell: CGFloat = 1
    var rows = 1

    /// The index of the day under `p` (0 is the first day shown), nil off the grid.
    func day(_ p: CGPoint) -> Int? {
        guard grid.contains(p), grid.width > 0, cell > 0 else { return nil }
        let column = min(Int(((p.x - grid.minX) / (grid.width / 7)).rounded(.down)), 6)
        let row = Int(((p.y - grid.minY + scrolled) / cell).rounded(.down))
        guard row >= 0, row < rows, column >= 0 else { return nil }
        return row * 7 + column
    }

    /// How far the grid scrolls: its weeks' height past the part in sight.
    var scrollRange: CGFloat { max(0, cell * CGFloat(rows) - grid.height) }

    /// Which way the grid scrolls while a finger is near its top or bottom edge (within `edge`), or past it: -1 up,
    /// 1 down, 0 not, nor when the weeks all fit.
    func autoscroll(_ p: CGPoint, edge: CGFloat) -> Int {
        guard scrollRange > 0, grid.height > edge * 2 else { return 0 }
        if p.y > grid.maxY - edge { return 1 }
        if p.y < grid.minY + edge { return -1 }
        return 0
    }
}

/// Saving a moved to-do: drawn there at once (the store), a toast that says where it went with Undo, and a failed save
/// put back with its error.
@MainActor enum TodoCalendarMove {
    static func save(_ id: String, to due: APIClient.TodoPlanChange.Due, store: TodoStore = .shared) {
        guard let todo = store.todo(id: id) else { return }
        let back = TodoCalendar.due(of: todo)
        guard back != due else { return }
        let words = TodoPlanText.due(TodoCalendar.moved(todo, to: due)) ?? ""
        let saving = store.reschedule(id, to: due)
        Task {
            guard await saving.value else { return }
            Haptics.success()
            AppModel.shared.toast = Toast(String(localized: "Moved to \(words)"), style: .success,
                                          action: Toast.Action(title: String(localized: "Undo")) { _ = store.reschedule(id, to: back) })
        }
    }
}

#if DEBUG
/// What the last drags on the calendar did, for the UI tests (#2045): where one was picked up and let go, and what
/// that meant. Read as the accessibility value of the calendar's bar under it (`debugDragTrace()`).
@MainActor @Observable final class CalendarDragTrace {
    static let shared = CalendarDragTrace()
    private(set) var text = ""

    func note(_ line: String) {
        text = (text.split(separator: "|").suffix(11).map(String.init) + [line]).joined(separator: "|")
    }

    static func point(_ p: CGPoint) -> String { "(\(Int(p.x)),\(Int(p.y)))" } // i18n-ignore: DEBUG trace
    static func rect(_ r: CGRect) -> String { "\(point(r.origin))+\(Int(r.width))x\(Int(r.height))" } // i18n-ignore: DEBUG trace
}
#endif

extension View {
    /// The calendar's drag trace for the UI tests, in DEBUG builds; nothing otherwise.
    func debugDragTrace() -> some View {
        #if DEBUG
        accessibilityValue(Text(verbatim: CalendarDragTrace.shared.text))
        #else
        self
        #endif
    }

    /// A to-do on the calendar that can be moved (#2045): a plain button that opens it (`open`; nil: not a button, a
    /// phone's month chip, whose day opens on a tap), picked up and carried in the grid's coordinate space `space`,
    /// faded while it is (`lifted`), and "Change Due Date…" for VoiceOver and Switch Control (not on a phone's month,
    /// whose day is one element that opens the day). Only the button without `moves`, and on the Mac (frozen).
    @ViewBuilder
    func calendarDraggable(_ entry: CalendarEntry, in space: String, moves: CalendarMoves?, lifted: Bool,
                           open: (() -> Void)?, accessible: Bool = true,
                           began: @escaping (CGPoint, CGFloat) -> Void, moved: @escaping (CGPoint) -> Void,
                           ended: @escaping (Bool) -> Void) -> some View {
        #if os(iOS)
        if let moves {
            let hold = CalendarHold(space: space, began: began, moved: moved, ended: ended)
            let carried = Group {
                if let open {
                    Button(action: open) { self }
                        .buttonStyle(CalendarCarryStyle(hold: hold))
                } else {
                    gesture(hold)
                }
            }
            .opacity(lifted ? Theme.CalendarGrid.liftedFade : 1)
            if accessible {
                carried.accessibilityAction(named: Text("Change Due Date…")) { moves.reschedule(entry) }
            } else {
                carried
            }
        } else {
            calendarButton(open)
        }
        #else
        calendarButton(open)
        #endif
    }

    /// The view at `frame` in its parent, placed by layout: drawn and touched in the same place (`.offset` moves only
    /// the drawing; offset buttons in scroll views, FB17939266).
    func calendarPlaced(_ frame: CGRect) -> some View {
        self.frame(width: frame.width, height: frame.height)
            .position(x: frame.midX, y: frame.midY)
    }

    /// A to-do that opens on a tap, as a plain button; nil: not a button.
    @ViewBuilder private func calendarButton(_ open: (() -> Void)?) -> some View {
        if let open {
            Button(action: open) { self }
                .buttonStyle(.plain)
        } else {
            self
        }
    }

    /// The block or chip drawn where a dragged to-do would land: on the calendar's ground, ringed in the accent.
    func calendarLanding() -> some View {
        self
            .background(Theme.CalendarGrid.ground, in: .rect(cornerRadius: Theme.Radius.badge))
            .overlay {
                RoundedRectangle(cornerRadius: Theme.Radius.badge)
                    .strokeBorder(Theme.accent, lineWidth: Theme.Size.lineMedium)
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }
}

#if os(iOS)
/// Picks a to-do up (#2045): a finger once it has been held still `hold` seconds (moving first is a swipe, the page's:
/// it scrolls the axis or turns the page), a pointer (an iPad's trackpad or mouse) once it has moved a few points (a
/// click still opens the to-do). Then it follows the finger or pointer until it is let go.
final class CalendarHoldRecognizer: UIGestureRecognizer {
    /// As the web's touch (`HOLD_MS`) and the chat's long press.
    static let hold: TimeInterval = 0.35
    /// How far a held finger may wander before it is a swipe, and how far a pointer moves before it drags.
    static let wander = Theme.Space.s
    static let pointerMove = Theme.Space.xs

    /// A touch let go before it is picked up is a tap, the button's (`CalendarCarryStyle`): recognized as one, so
    /// nothing under it takes it as well. Without, it fails (a phone's month chip: its day opens on the tap).
    var taps = false
    /// The touch that ended was a tap, not a drag (for the action, before `reset`).
    private(set) var tapped = false

    private var start: CGPoint?
    private var pointer = false
    private var timer: Timer?

    /// How far down the touch has gone since it came down: a pointer is picked up only once it has moved, a held finger
    /// may have wandered a little. The carried to-do keeps the touch where it was first put on it.
    var travel: CGFloat { start.map { location(in: nil).y - $0.y } ?? .zero }

    override func touchesBegan(_ touches: Set<UITouch>, with event: UIEvent) {
        super.touchesBegan(touches, with: event)
        // A second finger is a pinch or a scroll, not a drag.
        guard start == nil, touches.count == 1, let touch = touches.first else {
            if state == .began || state == .changed { state = .cancelled } else if state == .possible { state = .failed }
            return
        }
        start = touch.location(in: nil)
        pointer = touch.type == .indirectPointer
        trace(pointer ? "down pointer" : "down finger") // i18n-ignore: DEBUG trace
        guard !pointer else { return }
        // in the common modes: it fires while the run loop tracks a scroll too
        let timer = Timer(timeInterval: Self.hold, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated { self?.held() }
        }
        RunLoop.main.add(timer, forMode: .common)
        self.timer = timer
    }

    private func held() {
        trace("held \(state.rawValue)") // i18n-ignore: DEBUG trace
        if state == .possible { state = .began }
    }

    /// A step for the UI tests' drag trace, in DEBUG builds.
    private func trace(_ line: @autoclosure () -> String) {
        #if DEBUG
        CalendarDragTrace.shared.note(line())
        #endif
    }

    override func touchesMoved(_ touches: Set<UITouch>, with event: UIEvent) {
        super.touchesMoved(touches, with: event)
        guard let start, let p = touches.first?.location(in: nil) else { return }
        switch state {
        case .possible:
            let moved = hypot(p.x - start.x, p.y - start.y)
            if pointer, moved > Self.pointerMove {
                state = .began
            } else if !pointer, moved > Self.wander {
                trace("swipe") // i18n-ignore: DEBUG trace
                state = .failed
            }
        case .began, .changed:
            state = .changed
        default:
            break
        }
    }

    override func touchesEnded(_ touches: Set<UITouch>, with event: UIEvent) {
        super.touchesEnded(touches, with: event)
        switch state {
        case .began, .changed:
            state = .ended
        case .possible where taps:
            trace("tap") // i18n-ignore: DEBUG trace
            tapped = true
            state = .ended
        default:
            state = .failed
        }
    }

    override func touchesCancelled(_ touches: Set<UITouch>, with event: UIEvent) {
        super.touchesCancelled(touches, with: event)
        trace("touch cancelled \(state.rawValue)") // i18n-ignore: DEBUG trace
        state = state == .began || state == .changed ? .cancelled : .failed
    }

    /// A scroll view under it waits for this to fail (a finger that moves before it is held, or none at all): once a
    /// to-do is picked up, the scroll under it never takes the drag over (a pointer drags after 4 pt, before a scroll's
    /// 10 pt).
    override func shouldBeRequiredToFail(by other: UIGestureRecognizer) -> Bool {
        other is UIPanGestureRecognizer && other.view is UIScrollView
    }

    override func reset() {
        super.reset()
        timer?.invalidate()
        timer = nil
        start = nil
        pointer = false
        tapped = false
    }
}

/// The pick-up as SwiftUI sees it: where it began in `space` and how far below the view's top edge, where it moves,
/// and whether it ended let go (true) or cancelled; and a tap, when it is a button's (`tapped`).
struct CalendarHold: UIGestureRecognizerRepresentable {
    var space: String
    var began: (CGPoint, CGFloat) -> Void
    var moved: (CGPoint) -> Void
    var ended: (Bool) -> Void
    var tapped: (() -> Void)?

    func makeUIGestureRecognizer(context: Context) -> CalendarHoldRecognizer {
        let recognizer = CalendarHoldRecognizer()
        recognizer.taps = tapped != nil
        return recognizer
    }

    func updateUIGestureRecognizer(_ recognizer: CalendarHoldRecognizer, context: Context) {
        recognizer.taps = tapped != nil
    }

    func handleUIGestureRecognizerAction(_ recognizer: CalendarHoldRecognizer, context: Context) {
        let point = context.converter.location(in: .named(space))
        switch recognizer.state {
        case .began:
            began(point, context.converter.localLocation.y - recognizer.travel)
        case .changed:
            moved(point)
        case .ended where recognizer.tapped:
            tapped?()
        case .ended:
            ended(true)
        case .cancelled, .failed:
            ended(false)
        default:
            break
        }
    }
}

/// A to-do's button on the calendar that can be moved (#2045): the hold takes its touches, and a tap that is let go
/// before the pick-up opens it. A hold attached around a plain button never began: `gesture(_:)` ranks below the
/// button's own press, so a drag ended as a tap, which opened the to-do. No hover effect, as a plain button.
struct CalendarCarryStyle: PrimitiveButtonStyle {
    var hold: CalendarHold

    func makeBody(configuration: Configuration) -> some View {
        var hold = hold
        hold.tapped = { configuration.trigger() }
        return configuration.label
            .contentShape(.rect)
            .gesture(hold)
    }
}
#endif

/// A to-do's due date set without dragging it (#2045): VoiceOver's and Switch Control's "Change Due Date…" on the
/// calendar, saved as a drop is, with Undo.
struct TodoRescheduleSheet: View {
    @Environment(\.dismiss) private var dismiss
    var todo: TodoItem
    @State private var allDay: Bool
    @State private var due: Date

    init(todo: TodoItem) {
        self.todo = todo
        // As the to-do's page starts its due date: an all-day one at 18:00 should it become a moment.
        let draft = TodoPlanDraft(todo)
        _allDay = State(initialValue: draft.allDay)
        _due = State(initialValue: draft.due)
    }

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    FormDatePicker("Due", selection: $due, displayedComponents: allDay ? [.date] : [.date, .hourAndMinute],
                                   identifier: "todo.calendar.rescheduleDue")
                    Toggle(String(localized: "All Day"), isOn: $allDay)
                        .accessibilityIdentifier("todo.calendar.rescheduleAllDay")
                } header: {
                    Text(verbatim: todo.title)
                        .lineLimit(1)
                }
            }
            .themedPage()
            .navigationTitle("Change Due Date")
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                SheetClose { dismiss() }
                SheetConfirm(id: "todo.calendar.rescheduleSave", name: String(localized: "Save")) {
                    TodoCalendarMove.save(todo.id, to: allDay ? .day(TodosLogic.dayKey(due)) : .at(due))
                    dismiss()
                }
            }
        }
        .formSheetDetents([.medium])
    }
}
