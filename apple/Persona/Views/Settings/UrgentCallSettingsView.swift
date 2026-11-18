#if os(iOS)
import SwiftUI
import UIKit

/// Settings › Notifications › Urgent Calls (docs/assistant/urgent-call.md "Settings"): whether Persona may ring this
/// iPhone like a phone call, what counts as urgent, how many a day, quiet hours of their own, and a test call. The
/// manager keeps the settings (`GET` / `PUT /api/v1/assistant/urgent-call`); every change shows at once and the
/// manager's answer then stands. Off by default.
struct UrgentCallSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var page = UrgentCallSettingsModel()
    @State private var reach = UrgentCallReach.shared
    /// The rule being written: a new one (empty id) or one being reworded.
    @State private var editing: UrgentCallSettings.Rule?
    @State private var draft = ""
    @State private var voiceAllowed = UrgentCallVoice.allowed

    var body: some View {
        SettingsForm {
            if let error = page.error {
                Section {
                    LoadProblem(text: error)
                    Button { Task { await page.load(model.api) } } label: { Label("Retry", systemImage: Theme.Icon.retry).lineLimit(1) }
                }
            }
            if let set = page.settings {
                Section {
                    SettingsSwitchRow(title: String(localized: "Urgent Calls"),
                                      detail: String(localized: "When something can't wait, Persona rings this iPhone like a phone call and talks it through with you."),
                                      symbol: Theme.Icon.voiceChannel, tint: Theme.danger, iconColor: Theme.danger,
                                      accessibilityID: "urgentCall.enabled", isOn: enabledBinding(set))
                        .disabled(!page.canWrite)
                } footer: {
                    if set.enabled { statusLine }
                }
                if set.enabled {
                    rulesSection(set)
                    limitSection(set)
                    quietSection(set)
                    testSection
                }
            }
        }
        .themedPage()
        .detailTitle(String(localized: "Urgent Calls"))
        .navigationBarTitleDisplayMode(.inline)
        .overlay { if page.settings == nil && page.error == nil { ProgressView() } }
        .task { await page.load(model.api) }
        .alert(editing?.id.isEmpty == false ? String(localized: "Edit Rule") : String(localized: "Add Rule"),
               isPresented: Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })) {
            TextField(String(localized: "For example: anything about picking up the kids"), text: $draft)
            Button(String(localized: "Save")) { saveDraft() }
            Button(String(localized: "Cancel"), role: .cancel) { editing = nil }
        } message: {
            Text("Persona calls when something like this is happening now and can't wait.")
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didBecomeActiveNotification)) { _ in
            voiceAllowed = UrgentCallVoice.allowed
        }
    }

    // MARK: Sections

    @ViewBuilder private func rulesSection(_ set: UrgentCallSettings) -> some View {
        Section {
            ForEach(set.rules) { rule in
                Button {
                    draft = Self.label(rule)
                    editing = rule
                } label: {
                    Text(Self.label(rule)).font(Theme.Font.body).foregroundStyle(.primary).frame(maxWidth: .infinity, alignment: .leading)
                }
                .tint(.primary) // a rule reads as text; tapping it edits it
                .accessibilityHint(Text("Edit"))
            }
            .onDelete { offsets in
                var next = set
                next.rules.remove(atOffsets: offsets)
                page.save(next, api: model.api)
            }
            Button {
                draft = ""
                editing = UrgentCallSettings.Rule(id: "", text: nil, builtin: nil)
            } label: {
                Label("Add Rule", systemImage: Theme.Icon.add).lineLimit(1)
            }
            .disabled(set.rules.count >= 20)
            if !set.missingBuiltins.isEmpty {
                Button { page.save(set.restoringBuiltins(), api: model.api) } label: {
                    Label("Restore Default Rules", systemImage: Theme.Icon.retry).lineLimit(1)
                }
            }
        } header: {
            Text("What Counts as Urgent")
        } footer: {
            Text("Persona calls only for these, when it's happening now and waiting would make it worse, or when you ask it to call you about something. Everything else stays a notification. You can also tell Persona “call me about this kind of thing too”; it asks you to confirm before adding it.")
        }
    }

    @ViewBuilder private func limitSection(_ set: UrgentCallSettings) -> some View {
        Section {
            Stepper(value: capBinding(set), in: UrgentCallSettings.capRange) {
                LabeledContent(String(localized: "Calls a Day"), value: set.dailyCap.formatted())
            }
            .accessibilityIdentifier("urgentCall.cap")
            if let today = page.today {
                LabeledContent(String(localized: "Today"), value: today.formatted())
            }
        } footer: {
            Text("A call nobody answers rings once more after \(set.retryMinutes) minutes; that doesn't count again. When neither is answered, Persona sends a notification and reminds you after 10 and 30 minutes.")
        }
    }

    @ViewBuilder private func quietSection(_ set: UrgentCallSettings) -> some View {
        Section {
            SettingsSwitchRow(title: String(localized: "Quiet Hours for Calls"),
                              detail: set.quiet == nil ? String(localized: "Off: calls can come at any time") : nil,
                              symbol: Theme.Icon.quietHours, tint: Theme.Hue.indigo, iconColor: Theme.Hue.indigo,
                              accessibilityID: "urgentCall.quiet", isOn: quietOnBinding(set))
            if let quiet = set.quiet {
                DatePicker(String(localized: "From"), selection: clockBinding(set, quiet, \.start), displayedComponents: .hourAndMinute)
                DatePicker(String(localized: "Until"), selection: clockBinding(set, quiet, \.end), displayedComponents: .hourAndMinute)
            }
        } footer: {
            Text("During these hours Persona doesn't call; it sends a notification instead. In the assistant's time zone.")
        }
    }

    private var testSection: some View {
        Section {
            Button { page.test(model) } label: {
                Label("Test Call", systemImage: Theme.Icon.voiceChannel).lineLimit(1)
            }
            .disabled(page.testing)
            .accessibilityIdentifier("urgentCall.test")
            if !voiceAllowed {
                Label {
                    Text("Persona needs the microphone and speech recognition to hear you on a call.").font(Theme.Font.caption)
                } icon: {
                    ThemedIcon(Theme.Icon.voiceMute).foregroundStyle(Theme.caution)
                }
                Button {
                    UIApplication.shared.open(URL(string: UIApplication.openSettingsURLString)!)
                } label: {
                    Label("Open Settings", systemImage: Theme.Icon.settings).lineLimit(1)
                }
            }
        } footer: {
            Text("Lock your iPhone after tapping to see how a call looks. On some iPhones a call shows only at the top of the screen; a notification stays as well.")
        }
    }

    /// Whether a ring reaches this iPhone, as the hub said when it registered.
    @ViewBuilder private var statusLine: some View {
        let route = model.hub.flatMap { reach.routes[$0.group] ?? nil }
        switch route?.route {
        case "direct", "relay":
            Text("This iPhone can take urgent calls.")
        case "none" where route?.reason == "relay_failed":
            Text("This iPhone can't take calls yet: the push relay doesn't support them (\(route?.detail ?? "")). Persona sends notifications instead.")
        default:
            Text("This iPhone isn't registered for calls yet. Open Persona again in a moment; until then Persona sends notifications instead.")
        }
    }

    // MARK: Bindings

    private func enabledBinding(_ set: UrgentCallSettings) -> Binding<Bool> {
        Binding { set.enabled } set: { on in
            var next = set
            next.enabled = on
            Task {
                if on { voiceAllowed = await UrgentCallVoice.requestAccess() } // asked now: on a locked phone mid-call it can't be
                page.save(next, api: model.api)
            }
        }
    }

    private func capBinding(_ set: UrgentCallSettings) -> Binding<Int> {
        Binding { set.dailyCap } set: { cap in
            var next = set
            next.dailyCap = cap
            page.save(next, api: model.api)
        }
    }

    private func quietOnBinding(_ set: UrgentCallSettings) -> Binding<Bool> {
        Binding { set.quiet != nil } set: { on in
            var next = set
            next.quiet = on ? UrgentCallSettings.Quiet(start: "23:00", end: "07:00") : nil
            page.save(next, api: model.api)
        }
    }

    private func clockBinding(_ set: UrgentCallSettings, _ quiet: UrgentCallSettings.Quiet,
                              _ path: WritableKeyPath<UrgentCallSettings.Quiet, String>) -> Binding<Date> {
        Binding {
            let minutes = QuietHours.minutes(quiet[keyPath: path]) ?? 0
            return Calendar.current.date(bySettingHour: minutes / 60, minute: minutes % 60, second: 0, of: .now) ?? .now
        } set: { date in
            let c = Calendar.current.dateComponents([.hour, .minute], from: date)
            var nextQuiet = quiet
            nextQuiet[keyPath: path] = QuietHours.clock((c.hour ?? 0) * 60 + (c.minute ?? 0))
            guard nextQuiet != quiet, nextQuiet.start != nextQuiet.end else { return }
            var next = set
            next.quiet = nextQuiet
            page.save(next, api: model.api)
        }
    }

    private func saveDraft() {
        guard let rule = editing, var set = page.settings else { return }
        editing = nil
        let text = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        if rule.id.isEmpty {
            set.rules.append(UrgentCallSettings.Rule(id: UrgentCallSettings.newRuleID(), text: String(text.prefix(200)), builtin: nil))
        } else if let i = set.rules.firstIndex(where: { $0.id == rule.id }), text != Self.label(rule) {
            set.rules[i].text = String(text.prefix(200))
        }
        page.save(set, api: model.api)
    }

    /// A rule as the list shows it: the owner's words, or a built-in rule's name in the app's language.
    static func label(_ rule: UrgentCallSettings.Rule) -> String {
        if let text = rule.text, !text.isEmpty { return text }
        switch rule.id {
        case "money_loss": return String(localized: "Money or resources being lost: runaway charges, deleted resources, fraudulent payments")
        case "harm": return String(localized: "Personal harm or safety: smoke, gas or water alarms, a call for help")
        case "agent_disaster": return String(localized: "A catastrophic agent error: data deleted for good, a production incident, an irreversible send")
        default: return rule.id
        }
    }
}

/// The page's state: the settings as the manager last said, and the change in flight.
@MainActor @Observable final class UrgentCallSettingsModel {
    private(set) var settings: UrgentCallSettings?
    private(set) var today: Int?
    private(set) var canWrite = false
    private(set) var error: String?
    private(set) var testing = false
    /// The write in flight: an older one's answer does not undo a newer choice.
    @ObservationIgnored private var request = 0

    func load(_ api: APIClient?) async {
        guard let api = api?.currentAssistantSettings() else { return }
        do {
            apply(try await api.urgentCallSettings())
            error = nil
        } catch {
            if !error.isCancellation { self.error = error.localizedDescription }
        }
    }

    /// Shown at once; the manager's answer then stands, and a refusal puts it back and says why.
    func save(_ next: UrgentCallSettings, api: APIClient?) {
        guard let api = api?.currentAssistantSettings(), let before = settings, next != before else { return }
        settings = next
        request += 1
        let request = request
        UrgentCallPush.shared.set(on: next.enabled)
        Task {
            do {
                let saved = try await api.setUrgentCallSettings(next)
                guard request == self.request else { return }
                apply(saved)
                Haptics.success()
            } catch {
                guard request == self.request else { return }
                settings = before
                UrgentCallPush.shared.set(on: before.enabled)
                AppModel.shared.show(error)
            }
        }
    }

    func test(_ model: AppModel) {
        guard let api = model.api?.currentAssistantSettings(), !testing else { return }
        testing = true
        Task {
            defer { testing = false }
            do {
                let rang = try await api.testUrgentCall()
                model.toast = rang > 0 ? Toast(String(localized: "Calling this iPhone. Lock it to see how a call looks."))
                    : Toast(String(localized: "No iPhone can take calls yet."), style: .error)
            } catch {
                model.show(error)
            }
        }
    }

    private func apply(_ answer: UrgentCallSettingsAnswer) {
        settings = answer.settings
        today = answer.today
        canWrite = answer.canWrite ?? true
        UrgentCallPush.shared.set(on: answer.settings.enabled)
    }
}
#endif
