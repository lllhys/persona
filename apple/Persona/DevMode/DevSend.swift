#if os(iOS)
import SwiftUI

/// Send to Agent (Settings › About › Export Diagnostics, #9460, docs/operations/developing-persona.md "Send to Agent"):
/// the package goes into a chat the way a picked file does. The targets are Forward To's (`ForwardTargets`, its rows):
/// Persona's main chat, the recent ones, each computer's Agents, with search. Picking one opens that chat with the zip
/// as a file card over its box (uploading to the computer the chat runs on) and a note in the box; the owner sends it.
struct DevSendSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    let zip: URL
    let size: Int64?
    /// Called with the target before the sheet goes; the export page hands the package over once it has gone.
    var picked: (ForwardTarget) -> Void
    @State private var query = ""
    @State private var sessions: [Session] = []
    @State private var recents: [SessionKey] = []
    @State private var sections: [ForwardTargets.Section] = []

    private var group: String { model.hub?.group ?? "" }
    private var main: SessionKey? {
        guard let main = model.assistantMain, main.group == group else { return nil }
        return main
    }
    /// No chat is the source: a key no session has, on this hub.
    private var from: SessionKey { SessionKey(group: group, machineID: "", sessionID: "") }

    private func list() {
        let machines = model.machines.filter { $0.group == group }.map { (ref: $0.ref, name: $0.name) }
        sections = ForwardTargets.sections(sessions, from: from, recents: recents, main: main, machines: machines, query: query) {
            model.machine($0)?.online == true
        }
    }

    var body: some View {
        NavigationStack {
            List {
                Section {
                    HStack(spacing: Theme.Space.m) {
                        IconTile(Theme.Icon.fileArchive, tint: Theme.accent)
                        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                            Text(verbatim: zip.lastPathComponent).font(Theme.Font.secondaryStrong).lineLimit(1).truncationMode(.middle)
                            Text(size.map(DevModeSection.bytes) ?? "zip").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                        }
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityIdentifier("dev-send.file")
                }
                if query.isEmpty {
                    Section {
                        Button { pick(.persona) } label: {
                            ForwardFixedRow(title: "Persona", detail: String(localized: "Main Chat"), icon: Theme.Icon.persona)
                        }
                        .accessibilityIdentifier("dev-send.persona")
                    }
                }
                ForEach(sections) { section in
                    Section {
                        ForEach(section.sessions) { s in
                            let machine = section.kind != .machine && !s.assistant ? (model.machine(s.machineRef)?.name ?? s.machineID) : nil
                            Button { pick(.session(s.key)) } label: { ForwardSessionRow(session: s, machine: machine) }
                                .accessibilityIdentifier("dev-send.session." + s.sessionID)
                        }
                    } header: {
                        switch section.kind {
                        case .recent: Text("Recent")
                        case .machine: Text(verbatim: section.title)
                        case .results: EmptyView()
                        }
                    }
                }
                if !query.isEmpty && sections.isEmpty {
                    Text("No matching chats").foregroundStyle(.secondary)
                }
            }
            .pageSearchable(text: $query, placement: .navigationBarDrawer(displayMode: .always),
                            prompt: String(localized: "Search chats, computers or projects"))
            .navigationTitle(String(localized: "Send To"))
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                SheetClose(id: "dev-send.cancel") { dismiss() }
            }
        }
        .presentationDetents([.medium, .large])
        .presentationDragIndicator(.visible)
        .onAppear {
            sessions = model.sessionsNow
            recents = ForwardMemory(group: group).sessions
            list()
        }
        .onChange(of: query) { list() }
        .accessibilityIdentifier("dev-send.sheet")
    }

    private func pick(_ target: ForwardTarget) {
        Haptics.tap()
        picked(target)
        dismiss()
    }
}

extension AppModel {
    /// Puts the diagnostics package over `target`'s box as a picked file (uploading to the computer that chat runs
    /// on), writes `note` into the box unless it holds words already, and opens the chat focused. Persona with no main
    /// conversation yet: the new conversation's box.
    func attachDiagnostics(_ zip: URL, note: String, to target: ForwardTarget, group: String) {
        ForwardMemory(group: group).remember(target)
        switch target {
        case .newAgent:
            return
        case .persona:
            if let main = assistantMain, main.group == group {
                attachDiagnostics(zip, note: note, on: main)
                openAssistantConversation(main)
            } else {
                let draft = starterDraft("")
                draft.held.files.start([zip], machine: hubRef(assistantConfig?.machineID ?? hubMachineID), model: self, owned: true)
                if draft.text.isEmpty { draft.text = note }
                selectedTab = .assistant
                if let shell = tabRouter as? ShellState {
                    shell.show(.newConversation)
                    shell.drawer.close()
                } else {
                    assistantCurrent = nil
                }
            }
        case .session(let key):
            attachDiagnostics(zip, note: note, on: key)
            if session(key)?.assistant == true { openAssistantConversation(key) } else { open(key) }
        }
    }

    private func attachDiagnostics(_ zip: URL, note: String, on key: SessionKey) {
        composerAttachments(key).files.start([zip], machine: key.machineRef, model: self, owned: true)
        if (drafts[key] ?? "").isEmpty { drafts[key] = note }
        composerFocus = key
    }
}
#endif
