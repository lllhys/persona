#if os(iOS)
import SwiftUI

/// Glasses Photo (docs/apple/meta-glasses.md): connect the Meta glasses, preview what they see, take a photo and attach it.
/// Works in both builds: without the SDK, `MetaGlasses` only reports that the glasses are not built in.
struct GlassesCaptureSheet: View {
    @Environment(\.dismiss) private var dismiss
    @Environment(\.scenePhase) private var scenePhase
    @State private var glasses = MetaGlasses.shared
    @State private var allowed = false
    @State private var photo: UIImage?
    @State private var busy = false
    var pick: (UIImage) -> Void

    var body: some View {
        NavigationStack {
            Group {
                if !glasses.registered {
                    connectView
                } else if !allowed {
                    permissionView
                } else {
                    captureView
                }
            }
            .navigationTitle("Glasses Photo")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                SheetClose { dismiss() }
            }
            .task(id: glasses.registered) {
                guard glasses.registered else {
                    allowed = false
                    return
                }
                allowed = await glasses.cameraAllowed()
            }
            .onChange(of: scenePhase) { _, phase in
                if phase == .background {
                    glasses.stop()
                } else if phase == .active, glasses.registered {
                    Task {
                        allowed = await glasses.cameraAllowed()
                        if allowed, photo == nil {
                            try? await glasses.startPreview()
                        }
                    }
                }
            }
            .onDisappear { glasses.stop() }
        }
    }

    private var connectView: some View {
        VStack(spacing: Theme.Space.l) {
            Spacer(minLength: 0)
            Text("Connect your glasses in Meta AI first. The button below opens Meta AI; come back to Persona once they are connected.")
                .font(Theme.Font.body)
                .multilineTextAlignment(.center)
            Button { Task { await act { try await glasses.connect() } } } label: {
                Label("Connect Meta Glasses", systemImage: Theme.Icon.glasses).lineLimit(1)
            }
            .themedButton(prominent: true)
            .disabled(busy)
            errorText
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Theme.Space.page)
        .themedPage()
    }

    private var permissionView: some View {
        VStack(spacing: Theme.Space.l) {
            Spacer(minLength: 0)
            Text("Allow Persona to use the glasses' camera in Meta AI. This opens Meta AI.")
                .font(Theme.Font.body)
                .multilineTextAlignment(.center)
            Button {
                Task {
                    await act {
                        allowed = try await glasses.requestCamera()
                    }
                }
            } label: {
                Label("Allow [Glasses Capture Sheet beea4f]", systemImage: Theme.Icon.glasses).lineLimit(1)
            }
            .themedButton(prominent: true)
            .disabled(busy)
            errorText
            Spacer(minLength: 0)
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Theme.Space.page)
        .themedPage()
    }

    private var captureView: some View {
        VStack(spacing: Theme.Space.l) {
            ZStack {
                if let photo {
                    Image(uiImage: photo).resizable().scaledToFit()
                } else if let frame = glasses.preview {
                    Image(uiImage: frame).resizable().scaledToFit()
                } else if glasses.error == nil {
                    ProgressView(String(localized: "Connecting to the glasses")).font(Theme.Font.secondary)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .clipShape(RoundedRectangle(cornerRadius: Theme.Radius.card))
            errorText
            if let photo {
                HStack(spacing: Theme.Space.m) {
                    Button {
                        self.photo = nil
                        Task { try? await glasses.startPreview() }
                    } label: {
                        Label("Retake", systemImage: Theme.Icon.retry).lineLimit(1)
                    }
                    .themedButton()
                    Button {
                        pick(photo)
                        dismiss()
                    } label: {
                        Label("Use This Photo", systemImage: Theme.Icon.done).lineLimit(1)
                    }
                    .themedButton(prominent: true)
                }
            } else if glasses.preview == nil, glasses.error != nil {
                Button { Task { await act { try await glasses.startPreview() } } } label: {
                    Label("Retry [Components b8784c]", systemImage: Theme.Icon.retry).lineLimit(1)
                }
                .themedButton(prominent: true)
                .disabled(busy)
            } else {
                Button { Task { await shoot() } } label: {
                    Label("Take Photo [Glasses Capture Sheet d0ccbf]", systemImage: Theme.Icon.camera).lineLimit(1)
                }
                .themedButton(prominent: true)
                .disabled(glasses.preview == nil || busy)
            }
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.l)
        .themedPage()
        .task { try? await glasses.startPreview() }
    }

    @ViewBuilder private var errorText: some View {
        if let message = glasses.error {
            Text(message).font(Theme.Font.secondary).foregroundStyle(Theme.danger)
                .multilineTextAlignment(.center)
        }
    }

    /// Runs one step that may throw; the sheet shows `MetaGlasses.error`, so a failure needs no alert here.
    private func act(_ step: () async throws -> Void) async {
        busy = true
        defer { busy = false }
        try? await step()
    }

    private func shoot() async {
        busy = true
        defer { busy = false }
        if let image = try? await glasses.capture() {
            photo = image
            glasses.stop()
        }
    }
}
#endif
