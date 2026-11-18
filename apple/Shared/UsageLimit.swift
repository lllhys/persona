import Foundation
import PersonaWire

/// A subscription usage limit that stopped a turn (docs/assistant/agent-work.md "Usage limits"; the web's
/// `lib/usage-limit.ts`). The hub reports it on the session; rows and cards say when it resumes instead of calling the
/// turn interrupted. A hub that does not send the fields is not one of these.
extension Session {
    /// How long past its reset a usage-limit wait still counts (the hub's `limitResumeGrace`).
    nonisolated static let usageLimitGrace: TimeInterval = 10 * 60

    /// When the limit resets: the session's own time, else the one on its work state.
    nonisolated var usageLimitResetsAt: Date? { turnErrorResetsAt ?? work?.resetsAt }

    /// The CLI said it goes on by itself, or the hub scheduled this limit's continuation. A wake counts only with the
    /// limit's own `resets_at`: another wait's `wake_at` does not.
    nonisolated var usageLimitContinues: Bool {
        if turnErrorResumes == true { return true }
        guard let w = work else { return false }
        return w.phase == "blocked" && w.resetsAt != nil && w.wakeAt != nil
    }

    /// Stopped on a usage limit whose reset is still ahead, or not known. A known reset more than the grace ago is
    /// over: the turn reads as interrupted again.
    nonisolated func isUsageLimited(now: Date = Date()) -> Bool {
        guard !(turnError ?? "").isEmpty, turnErrorKind == "usage_limit", state == .unread || state == .idle else { return false }
        guard let at = usageLimitResetsAt else { return true }
        return now <= at.addingTimeInterval(Session.usageLimitGrace)
    }

    /// When a limit resets, in the user's time zone: the clock when that is today, the date as well on another day.
    nonisolated static func usageLimitClock(_ at: Date, now: Date = Date(), calendar: Calendar = .current) -> String {
        // rows and cards ask on every redraw: the format style is built once per time and form (docs/apple/performance.md P4)
        let today = calendar.isDate(at, inSameDayAs: now)
        let key = "\(Int(at.timeIntervalSince1970))|\(today)|\(calendar.identifier)|\(calendar.timeZone.identifier)|\(Locale.current.identifier)"
        return clockMemo(key) { _ in
            today ? at.formatted(Date.FormatStyle(calendar: calendar, timeZone: calendar.timeZone).hour().minute())
                  : at.formatted(Date.FormatStyle(calendar: calendar, timeZone: calendar.timeZone).month(.defaultDigits).day().hour().minute())
        }
    }

    nonisolated private static let clockMemo = TextMemo<String>(limit: 64)


    /// The line for rows and cards; nil when this is not a usage-limit wait, so callers keep the interrupted text.
    nonisolated func usageLimitLabel(now: Date = Date(), calendar: Calendar = .current) -> String? {
        guard isUsageLimited(now: now) else { return nil }
        let goesOn = usageLimitContinues
        guard let at = usageLimitResetsAt else {
            return goesOn ? String(localized: "Usage Limit · resumes after the reset") : String(localized: "Usage Limit")
        }
        let time = Session.usageLimitClock(at, now: now, calendar: calendar)
        return goesOn ? String(localized: "Usage Limit · resumes \(time)") : String(localized: "Usage Limit · resets \(time)")
    }

    /// Only the time half of the line, for a row that already says "Usage Limit": "Resets 13:05", "Resumes 13:05" or "Resumes
    /// after the reset"; nil when it is not a usage-limit wait, or nothing says when.
    nonisolated func usageLimitWhen(now: Date = Date(), calendar: Calendar = .current) -> String? {
        guard isUsageLimited(now: now) else { return nil }
        let goesOn = usageLimitContinues
        guard let at = usageLimitResetsAt else { return goesOn ? String(localized: "Resumes after the reset") : nil }
        let time = Session.usageLimitClock(at, now: now, calendar: calendar)
        return goesOn ? String(localized: "Resumes \(time)") : String(localized: "Resets \(time)")
    }
}
