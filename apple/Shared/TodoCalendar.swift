import Foundation

// The to-dos' calendar (#2029, docs/assistant/todos.md "Calendar"): which entries a day, a week or a month shows, and
// where. The views draw entries, not to-dos: a to-do due at a moment or on a day is one source; the phone's calendar
// events can be another later (an entry with an end), and nothing here needs to change for it.

/// What the calendar shows: a day, a week or a month.
nonisolated enum TodoCalendarMode: String, CaseIterable, Sendable {
    case day, week, month
}

/// One thing on the calendar.
nonisolated struct CalendarEntry: Identifiable, Sendable, Equatable {
    /// Where it comes from, and what tapping it opens.
    nonisolated enum Source: Sendable, Equatable, Hashable {
        case todo(String)
    }

    var source: Source
    var title: String
    /// When it is: a to-do's due moment, or the start of its due day when `allDay`.
    var start: Date
    /// When it ends: nil for a to-do, which is due at a moment; an event's end.
    var end: Date?
    var allDay: Bool
    /// Its state, colored and named as in the list.
    var state: TaskState
    var stateLabel: String
    var done: Bool
    var overdue: Bool
    var highPriority: Bool
    /// A reminder still to be pushed (the list's bell).
    var reminding: Bool

    var id: String {
        switch source {
        case .todo(let id): "todo:" + id
        }
    }
}

/// Where a to-do dragged on the calendar lands (#2045): due at a moment (`at`), or all day on `day` (its start)
/// without one.
nonisolated struct CalendarDrop: Sendable, Equatable {
    var day: Date
    var at: Date?
}

/// Where the calendar's entries come from, asked for the days it shows: a source that reads by date (the hub once its
/// to-dos are paged, the phone's calendar) fetches only those.
protocol CalendarEntrySource {
    func entries(in range: DateInterval, calendar: Calendar, now: Date) -> [CalendarEntry]
}

/// The to-dos with a due date, from the list the app already has (`GET /api/v1/todos` returns them all).
struct TodoEntrySource: CalendarEntrySource {
    var todos: [TodoItem]
    var state: (TodoItem) -> TaskState
    var showDone: Bool

    func entries(in range: DateInterval, calendar: Calendar, now: Date) -> [CalendarEntry] {
        todos.compactMap { todo -> CalendarEntry? in
            guard showDone || !todo.isDone else { return nil }
            return TodoCalendar.entry(todo, state: state(todo), calendar: calendar, now: now)
        }
        .filter { $0.start >= range.start && $0.start < range.end }
    }
}

nonisolated enum TodoCalendar {
    /// A to-do on the calendar: at its due moment, or all day on its due day; nil without either.
    static func entry(_ todo: TodoItem, state: TaskState, calendar: Calendar, now: Date) -> CalendarEntry? {
        let start: Date
        let allDay: Bool
        if let at = todo.dueAt {
            start = at
            allDay = false
        } else if let day = TodosLogic.dueDay(todo, calendar: calendar) {
            start = day
            allDay = true
        } else {
            return nil
        }
        return CalendarEntry(
            source: .todo(todo.id), title: todo.title, start: start, end: nil, allDay: allDay, state: state,
            stateLabel: todo.statusLabel(state), done: todo.isDone, overdue: TodosLogic.isOverdue(todo, now: now, calendar: calendar),
            highPriority: todo.priority == "high" && !todo.isDone,
            reminding: !todo.isDone && todo.remindAt != nil && todo.remindedAt == nil)
    }

    /// Open to-dos without a due date: not on the calendar, counted under it ("N without a due date").
    static func undatedCount(_ todos: [TodoItem], calendar: Calendar) -> Int {
        todos.filter { !$0.isDone && $0.dueAt == nil && TodosLogic.dueDay($0, calendar: calendar) == nil }.count
    }

    // MARK: Days

    /// The user's calendar, its weeks starting on `firstWeekday(locale:calendar:chosen:)`: the one calendar the day's
    /// strip, the week and the month all use.
    static var userCalendar: Calendar {
        var calendar = Calendar.current
        calendar.firstWeekday = firstWeekday(locale: .current, calendar: calendar,
                                             chosen: UserDefaults.standard.object(forKey: "AppleFirstWeekday") != nil)
        return calendar
    }

    /// The weekday weeks start on (1 Sunday … 7 Saturday): the one the user chose (Language & Region's first day of the
    /// week); else Monday in Simplified Chinese or on the Chinese mainland, where Apple's data still has Sunday (current
    /// CLDR, the browsers and so the web's calendar have Monday); else the region's.
    static func firstWeekday(locale: Locale, calendar: Calendar, chosen: Bool = false) -> Int {
        if chosen || Locale.Components(locale: locale).firstDayOfWeek != nil { return calendar.firstWeekday }
        if locale.language.script?.identifier == "Hans" || locale.region?.identifier == "CN" { return 2 }
        return calendar.firstWeekday
    }

    /// The first day of the week `date` is in, as `calendar` starts weeks (`userCalendar`: as the user's do).
    static func startOfWeek(_ date: Date, calendar: Calendar) -> Date {
        calendar.dateInterval(of: .weekOfYear, for: date)?.start ?? calendar.startOfDay(for: date)
    }

    /// The days a mode shows around `anchor`: the day; its week; the weeks its month touches, whole.
    static func days(_ mode: TodoCalendarMode, anchor: Date, calendar: Calendar) -> [Date] {
        let day = calendar.startOfDay(for: anchor)
        func run(from start: Date, count: Int) -> [Date] {
            (0..<count).compactMap { calendar.date(byAdding: .day, value: $0, to: start) }
        }
        switch mode {
        case .day:
            return [day]
        case .week:
            return run(from: startOfWeek(day, calendar: calendar), count: 7)
        case .month:
            guard let month = calendar.dateInterval(of: .month, for: day) else { return [day] }
            let first = startOfWeek(month.start, calendar: calendar)
            let lastDay = calendar.date(byAdding: .day, value: -1, to: month.end) ?? month.start
            let last = startOfWeek(lastDay, calendar: calendar)
            let weeks = (calendar.dateComponents([.day], from: first, to: last).day ?? 0) / 7 + 1
            return run(from: first, count: weeks * 7)
        }
    }

    /// From the start of the first day shown to the end of the last.
    static func range(_ mode: TodoCalendarMode, anchor: Date, calendar: Calendar) -> DateInterval {
        let shown = days(mode, anchor: anchor, calendar: calendar)
        let start = shown.first ?? calendar.startOfDay(for: anchor)
        let end = shown.last.flatMap { calendar.date(byAdding: .day, value: 1, to: $0) } ?? start
        return DateInterval(start: start, end: end)
    }

    /// The anchor a page later (`by` 1) or earlier (-1): a day, a week or a month.
    static func step(_ anchor: Date, mode: TodoCalendarMode, by n: Int, calendar: Calendar) -> Date {
        let unit: Calendar.Component = switch mode {
        case .day: .day
        case .week: .weekOfYear
        case .month: .month
        }
        return calendar.date(byAdding: unit, value: n, to: anchor) ?? anchor
    }

    /// A day's entries in the order they are drawn: all-day first, then by time; at one time open before done, then by title.
    static func on(_ day: Date, _ entries: [CalendarEntry], calendar: Calendar) -> [CalendarEntry] {
        entries.filter { calendar.isDate($0.start, inSameDayAs: day) }.sorted(by: drawOrder)
    }

    static func drawOrder(_ a: CalendarEntry, _ b: CalendarEntry) -> Bool {
        if a.allDay != b.allDay { return a.allDay }
        if a.start != b.start { return a.start < b.start }
        if a.done != b.done { return !a.done }
        if a.title != b.title { return a.title < b.title }
        return a.id < b.id
    }

    // MARK: The time axis

    /// Minutes from the start of `day` to `date`.
    static func minutes(_ date: Date, in day: Date, calendar: Calendar) -> Double {
        date.timeIntervalSince(calendar.startOfDay(for: day)) / 60
    }

    /// An entry placed on a day's axis: in lane `lane` of `lanes` side by side.
    nonisolated struct Placed: Sendable, Equatable, Identifiable {
        var entry: CalendarEntry
        var lane: Int
        var lanes: Int
        var id: String { entry.id }
    }

    /// The timed entries of one day whose blocks overlap, grouped: a block is `minutes` long from its start (a to-do's
    /// is a fixed height), or until its end when that is later.
    static func clusters(_ timed: [CalendarEntry], minutes: Double) -> [[CalendarEntry]] {
        var out: [[CalendarEntry]] = []
        var group: [CalendarEntry] = []
        var groupEnd = Date.distantPast
        for e in timed.sorted(by: drawOrder) {
            if !group.isEmpty && e.start >= groupEnd {
                out.append(group)
                group = []
            }
            group.append(e)
            groupEnd = max(groupEnd, blockEnd(e, minutes: minutes))
        }
        if !group.isEmpty { out.append(group) }
        return out
    }

    /// Side by side where blocks overlap: each entry in the first lane free at its start, every entry of a cluster
    /// as narrow as the cluster's lanes need.
    static func lanes(_ timed: [CalendarEntry], minutes: Double) -> [Placed] {
        clusters(timed, minutes: minutes).flatMap { group -> [Placed] in
            var ends: [Date] = []
            let laned = group.map { e -> (CalendarEntry, Int) in
                let lane = ends.firstIndex { $0 <= e.start } ?? ends.count
                if lane == ends.count { ends.append(.distantPast) }
                ends[lane] = blockEnd(e, minutes: minutes)
                return (e, lane)
            }
            return laned.map { Placed(entry: $0.0, lane: $0.1, lanes: ends.count) }
        }
    }

    static func blockEnd(_ e: CalendarEntry, minutes: Double) -> Date {
        max(e.end ?? e.start, e.start.addingTimeInterval(minutes * 60))
    }

    /// The moment a tap `minutes` into `day` means: snapped down to `snap` minutes, within the day.
    static func slot(day: Date, minutes: Double, snap: Int, calendar: Calendar) -> Date {
        let clamped = min(max(minutes, 0), 24 * 60 - Double(snap))
        let snapped = (clamped / Double(snap)).rounded(.down) * Double(snap)
        return calendar.startOfDay(for: day).addingTimeInterval(snapped * 60)
    }

    /// Where the axis first scrolls to on the days shown: the earlier of an hour and a half before now (when today is
    /// shown) and an hour before their first timed entry, so the morning's to-dos are in sight; the morning without either.
    static func firstHour(days: [Date], timed: [CalendarEntry], now: Date, calendar: Calendar) -> Int {
        var hours: [Int] = []
        if days.contains(where: { calendar.isDate($0, inSameDayAs: now) }) {
            let back = now.addingTimeInterval(-90 * 60)
            hours.append(calendar.isDate(back, inSameDayAs: now) ? calendar.component(.hour, from: back) : 0)
        }
        let shown = timed.filter { e in days.contains { calendar.isDate(e.start, inSameDayAs: $0) } }
        if let first = shown.map(\.start).min() {
            hours.append(max(0, calendar.component(.hour, from: first) - 1))
        }
        return hours.min() ?? morning
    }

    /// Where a day without anything on it starts.
    static let morning = 8

    /// Minutes either side of the moment where an hour's label gives way to the now line's.
    static let nowLabelRoom = 15

    // MARK: The month

    /// What a day of the month shows in `lines` lines: all when they fit, else a line fewer and how many more.
    static func fit(_ entries: [CalendarEntry], lines: Int) -> (shown: [CalendarEntry], more: Int) {
        guard entries.count > lines, lines > 0 else { return (entries, 0) }
        let shown = Array(entries.prefix(lines - 1))
        return (shown, entries.count - shown.count)
    }

    // MARK: Moving one (#2045)

    /// What a block dragged on the axis snaps to, in minutes (the web's `DROP_SNAP`).
    static let dropSnap = 15

    /// A block dropped with its top edge `minutes` into `day`: due at the nearest quarter hour, within the day.
    static func dropOnAxis(day: Date, minutes: Double, snap: Int = dropSnap, calendar: Calendar) -> CalendarDrop {
        let clamped = min(max(minutes, 0), Double(24 * 60 - snap))
        let snapped = Int((clamped / Double(snap)).rounded()) * snap
        let start = calendar.startOfDay(for: day)
        let at = calendar.date(bySettingHour: snapped / 60, minute: snapped % 60, second: 0, of: start)
        return CalendarDrop(day: start, at: at ?? start.addingTimeInterval(Double(snapped) * 60))
    }

    /// `entry` dropped on `day` as a whole: at its time of day there (the month), or all day there (the All Day row,
    /// `allDay`, or one that already was all day).
    static func dropOnDay(_ entry: CalendarEntry, day: Date, allDay: Bool = false, calendar: Calendar) -> CalendarDrop {
        let start = calendar.startOfDay(for: day)
        if allDay || entry.allDay { return CalendarDrop(day: start, at: nil) }
        let t = calendar.dateComponents([.hour, .minute, .second], from: entry.start)
        let at = calendar.date(bySettingHour: t.hour ?? 0, minute: t.minute ?? 0, second: t.second ?? 0, of: start)
        return CalendarDrop(day: start, at: at ?? start)
    }

    /// Whether `entry` dropped there lands somewhere else: nothing is saved for one put back where it was.
    static func moves(_ entry: CalendarEntry, to drop: CalendarDrop, calendar: Calendar) -> Bool {
        if let at = drop.at { return entry.allDay || entry.start != at }
        return !entry.allDay || !calendar.isDate(entry.start, inSameDayAs: drop.day)
    }

    /// The due date a drop gives, as `PATCH /api/v1/todos/{id}` takes it: `due_at`, or `due_date`, either replacing
    /// the other.
    static func due(_ drop: CalendarDrop, calendar: Calendar) -> APIClient.TodoPlanChange.Due {
        drop.at.map { .at($0) } ?? .day(TodosLogic.dayKey(drop.day, calendar: calendar))
    }

    /// The due date `todo` has now: what Undo puts back.
    static func due(of todo: TodoItem) -> APIClient.TodoPlanChange.Due {
        if let at = todo.dueAt { return .at(at) }
        if let day = todo.dueDate { return .day(day) }
        return .none
    }

    /// `todo` due as `due` says: drawn there at once, before the hub answers.
    static func moved(_ todo: TodoItem, to due: APIClient.TodoPlanChange.Due) -> TodoItem {
        var t = todo
        switch due {
        case .none: (t.dueAt, t.dueDate) = (nil, nil)
        case .at(let at): (t.dueAt, t.dueDate) = (at, nil)
        case .day(let day): (t.dueAt, t.dueDate) = (nil, day)
        }
        return t
    }

    /// `entry` where it would land, as it would be drawn there while it is dragged: at the moment or on the day,
    /// overdue or not by then.
    static func landed(_ entry: CalendarEntry, at drop: CalendarDrop, calendar: Calendar, now: Date) -> CalendarEntry {
        var e = entry
        e.start = drop.at ?? drop.day
        e.allDay = drop.at == nil
        let by = drop.at ?? calendar.date(byAdding: .day, value: 1, to: drop.day)?.addingTimeInterval(-1) ?? drop.day
        e.overdue = !entry.done && by < now
        return e
    }

    // MARK: Words

    /// The period's heading: "Mon, Nov 9" · "Nov 9 – 15" · "November 2026", as the locale writes them.
    static func title(_ mode: TodoCalendarMode, anchor: Date, calendar: Calendar, locale: Locale) -> String {
        var style = Date.FormatStyle(locale: locale, calendar: calendar, timeZone: calendar.timeZone)
        switch mode {
        case .day:
            style = style.month(.abbreviated).day().weekday(.abbreviated)
            return anchor.formatted(style)
        case .week:
            let shown = days(.week, anchor: anchor, calendar: calendar)
            guard let first = shown.first, let last = shown.last else { return "" }
            let interval = Date.IntervalFormatStyle(locale: locale, calendar: calendar, timeZone: calendar.timeZone)
            return (first..<last).formatted(interval.month(.abbreviated).day())
        case .month:
            style = style.year().month(.wide)
            return anchor.formatted(style)
        }
    }

    /// The weekdays' short names in the order the weeks start, as a column's header: "Mon" (`narrow`: "M"), in the
    /// calendar's locale.
    static func weekdaySymbols(calendar: Calendar, narrow: Bool) -> [String] {
        let symbols = narrow ? calendar.veryShortStandaloneWeekdaySymbols : calendar.shortStandaloneWeekdaySymbols
        let first = calendar.firstWeekday - 1
        return Array(symbols[first...] + symbols[..<first])
    }
}
