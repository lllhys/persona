import SwiftUI

struct LarkEncryptionModeBar: View {
    @Binding var draft: LarkEncryptedDraft?
    var canEncrypt: Bool
    var checking = false
    var selectEncrypted: () -> Void
    var pickCover: () -> Void
    /// The cover is picked here (the old API); a messaging source asks for it when sending (compose, #2138).
    var picksCover = true
    /// The source can send sealed messages now (its `modes`); a draft kept in the mode says it cannot.
    var seals = true

    var body: some View {
        HStack(spacing: Theme.Space.m) {
            Menu {
                Button { draft = nil } label: { Text("Regular").lineLimit(1) }
                    .accessibilityIdentifier("lark.encryption.plain")
                Button(action: selectEncrypted) { Text("Encrypted message").lineLimit(1) }
                    .disabled(!canEncrypt)
                    .accessibilityIdentifier("lark.encryption.encrypted")
            } label: {
                HStack(spacing: Theme.Space.xs) {
                    Label(mode.title, systemImage: mode.icon)
                    Image(systemName: Theme.Icon.expand)
                }
                .font(Theme.Font.label).lineLimit(1)
            }
            .accessibilityIdentifier("lark.encryption.mode")
            if draft != nil, !picksCover {
                Text(seals ? String(localized: "Public text is confirmed when sending") : String(localized: "The message source can't send encrypted messages right now")).font(Theme.Font.label).lineLimit(1)
            } else if let draft {
                Button(action: pickCover) {
                    Text(draft.cover.isEmpty ? String(localized: "Choose public text") : String(localized: "Public text: \(draft.cover)"))
                        .font(Theme.Font.label).lineLimit(1).truncationMode(.tail)
                }
                .accessibilityIdentifier("lark.encryption.cover")
                .disabled(checking)
            }
            if checking { ProgressView().controlSize(.small).accessibilityLabel("Checking encryption service") }
            Spacer(minLength: 0)
        }
        .foregroundStyle(.secondary)
        .padding(.horizontal, Theme.Space.page)
        .padding(.top, Theme.Space.s)
    }

    private var mode: (title: String, icon: String) {
        if draft != nil { (String(localized: "Encrypted message"), Theme.Icon.locked) }
        else { (String(localized: "Regular"), Theme.Icon.message) }
    }
}

struct LarkEncryptionCoverPicker: View {
    @Environment(\.dismiss) private var dismiss
    var api: APIClient
    var account: String
    var feed: String
    var select: (String) -> Void
    @State private var lines: [String] = []
    @State private var loading = true
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            List {
                Section {
                    if loading { ProgressView(String(localized: "Loading history…")) }
                    else if let problem {
                        Text(problem).font(Theme.Font.secondary)
                        Button("Retry [Components b8784c]") { Task { await load() } }.lineLimit(1)
                    } else if lines.isEmpty {
                        Text("No history available yet").foregroundStyle(.secondary)
                    } else {
                        ForEach(Array(lines.enumerated()), id: \.offset) { _, line in
                            Button { select(line); dismiss() } label: {
                                Text(line).font(Theme.Font.body).lineLimit(1)
                            }
                            .accessibilityIdentifier("lark.encryption.coverChoice")
                        }
                    }
                } footer: {
                    Text("Choose an earlier message to show to people who can't decrypt this one.")
                }
            }
            .themedGroupedPage()
            .navigationTitle("Public Text")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { SheetClose { dismiss() } }
            .task { await load() }
        }
    }
    private func load() async {
        loading = true; problem = nil
        defer { loading = false }
        do { lines = try await api.larkEncryptionCovers(account: account, feed: feed) }
        catch { if !error.isCancellation { problem = error.shown ?? String(localized: "Couldn't load. Try again.") } }
    }
}

struct LarkEncryptionUnlockSheet: View {
    @Environment(\.dismiss) private var dismiss
    var api: APIClient
    var account: String
    var unified: Bool
    var unlocked: () -> Void
    @State private var password = ""
    @State private var busy = false
    @State private var problem: String?

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    SecureField(unified ? String(localized: "Lark gateway password") : String(localized: "Keystore password"), text: $password)
                        .textContentType(.password)
                        .accessibilityIdentifier("lark.encryption.password")
                    if let problem { Text(problem).font(Theme.Font.secondary).foregroundStyle(Theme.danger) }
                    Button {
                        let input = password
                        password = ""
                        busy = true
                        problem = nil
                        Task {
                            defer { busy = false }
                            do {
                                let result = try await api.larkUnlock(account: account, password: input)
                                guard result.accountId == account, !result.locked else { throw LarkOutboxIdentity.Failure.changed }
                                dismiss()
                                unlocked()
                            } catch { problem = error.shown ?? String(localized: "Couldn't unlock. Try again.") }
                        }
                    } label: {
                        HStack(spacing: Theme.Space.s) {
                            if busy { ProgressView().controlSize(.small) }
                            Text(busy ? String(localized: "Unlocking…") : String(localized: "Unlock and Send")).lineLimit(1)
                        }
                    }
                    .disabled(busy || password.isEmpty)
                    .accessibilityIdentifier("lark.encryption.unlock")
                } footer: { Text("Your password is used only for this unlock and isn't saved.") }
            }
            .themedGroupedPage()
            .navigationTitle("Unlock Encrypted Messages")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { SheetClose(disabled: busy) { password = ""; dismiss() } }
            .interactiveDismissDisabled(busy)
            .onDisappear { password = "" }
        }
    }
}
