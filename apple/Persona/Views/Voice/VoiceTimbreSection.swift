import SwiftUI

/// The voice a voice chat speaks with, outside a call (docs/assistant/voice.md 5.7, 6.7, #2411): a section on the voice
/// extension's page in Settings › Extensions. Lists the voices of the model the use `plugin.<id>` picks, with the
/// current one (picked on this device, else the model's default); a pick is kept per group of hubs (`VoiceTimbre`),
/// where every call reads it, and a call going on with this extension takes it up at once. In a call the same choice
/// is under ⋯ › Voice.
struct VoiceTimbreSection: View {
    let extensionID: String
    let group: String
    @State private var voices: VoiceVoicesLoader
    @State private var chosen: String?

    init(extensionID: String, group: String) {
        self.extensionID = extensionID
        self.group = group
        _voices = State(initialValue: VoiceVoicesLoader(extensionID: extensionID, api: { AppModel.shared.api(group) }))
        _chosen = State(initialValue: VoiceTimbre.chosen(group: group, extensionID: extensionID))
    }

    /// Whether an extension's page has it: the voice extension, on and running, on a device with calls, and the
    /// default manager's copy (calls talk to that one).
    static func shows(_ plugin: HubExtension, manager: String?, platform: VoicePlatform = .current) -> Bool {
        platform == .phone && manager == nil && plugin.kind == "voice" && plugin.status == "running" && plugin.enabled != false
    }

    var body: some View {
        Section {
            row
        } header: {
            SettingsHeading(String(localized: "Voice Chat"))
        } footer: {
            Text("Voice chats on this device speak with this voice. You can also change it during a chat, under ⋯.")
        }
        .task {
            // The voices of the model calls start with (the use plugin.<id>; its default when that cannot be used).
            var model: String?
            if let api = AppModel.shared.api(group), case .ready(let picked) = await VoiceConversation.resolveModel(api, extensionID) {
                model = picked
            }
            voices.show(model: model)
        }
        .onDisappear { voices.stop() }
    }

    @ViewBuilder private var row: some View {
        switch voices.state {
        case .loaded(let list) where !list.voices.isEmpty:
            let current = chosen ?? list.default ?? ""
            Picker(selection: Binding(get: { current }, set: { pick($0) })) {
                ForEach(list.voices, id: \.id) { v in Text(v.name ?? v.id).tag(v.id) }
                // A voice kept from another model still shows as the current one.
                if !current.isEmpty, !list.voices.contains(where: { $0.id == current }) { Text(current).tag(current) }
            } label: {
                label
            }
            .pickerStyle(.menu)
            .accessibilityIdentifier("voice-timbre-setting")
        case .loaded:
            LabeledContent {
                Text("None for this model").foregroundStyle(.secondary).lineLimit(1)
            } label: {
                label
            }
            .accessibilityIdentifier("voice-timbre-setting")
        case .idle, .loading:
            LabeledContent {
                HStack(spacing: Theme.Space.s) {
                    ProgressView()
                    Text("Loading voices…").foregroundStyle(.secondary).lineLimit(1)
                }
            } label: {
                label
            }
            .accessibilityIdentifier("voice-timbre-setting")
        case .failed:
            LabeledContent {
                Button {
                    Haptics.tap()
                    voices.retry()
                } label: {
                    Text("Retry [Components b8784c]").lineLimit(1)
                }
                .accessibilityIdentifier("voice-timbre-retry")
            } label: {
                Label {
                    VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                        Text("Voice").lineLimit(1)
                        Text("Couldn't load voices").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                    }
                } icon: {
                    ThemedIcon(Theme.Icon.voiceTimbre)
                }
                // On the words, not the row: the row's would be Retry's too.
                .accessibilityIdentifier("voice-timbre-setting")
            }
        }
    }

    private var label: some View {
        Label { Text("Voice").lineLimit(1) } icon: { ThemedIcon(Theme.Icon.voiceTimbre) }
    }

    private func pick(_ voice: String) {
        chosen = voice
        VoiceTimbre.choose(voice, group: group, extensionID: extensionID)
        // A call going on with this extension speaks with it from now on.
        if let call = VoiceCalls.shared.current, call.key.group == group, call.extensionID == extensionID, !call.phase.isEnded {
            call.change(voice: voice)
        }
    }
}
