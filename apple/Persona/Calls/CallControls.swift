#if os(iOS)
import AVKit
import SwiftUI

// The round controls of a system call's page (#9290, docs/design/DESIGN_SYSTEM.md "Call screen"): the call screen's Mute,
// Speaker or Audio, Resume and End, and the urgent call page's Mute, Resume and Hang Up. A circle a thumb finds without
// looking, as the Phone app's, with its one-line label under it; every one acts through `PersonaCalls`, never on the
// voice itself, so the system's call UI and AirPods say the same.

/// What a round control is: a toggle (filled while it is on, as the Phone app's Mute), a plain action, or the red End.
nonisolated enum CallControlKind: Equatable, Sendable {
    case toggle(on: Bool)
    case action
    case end

    var isOn: Bool { self == .toggle(on: true) }
}

/// The disc a round control draws, its glyph in the middle: the shell's ink for a toggle that is on, its quieter grey
/// otherwise (the dictation bar's round buttons), the danger red for End.
struct CallControlDisc: View {
    let symbol: String
    let kind: CallControlKind
    @Environment(\.isEnabled) private var enabled

    var body: some View {
        ThemedIcon(symbol)
            .font(Theme.Font.title)
            .foregroundStyle(ink)
            .scaledFrame(width: Theme.Size.callControl, height: Theme.Size.callControl)
            .background(fill, in: .circle)
            .opacity(enabled ? 1 : 0.45)
    }

    private var fill: Color {
        switch kind {
        case .end: Theme.danger
        case .toggle(on: true): Theme.Shell.ink
        case .toggle, .action: Theme.Shell.roundMuted
        }
    }

    private var ink: Color {
        switch kind {
        case .end: Theme.onSolid
        case .toggle(on: true): Theme.Shell.inkText
        case .toggle, .action: Theme.Shell.ink
        }
    }
}

/// A round control: the disc and its label under it, one line at every text size (it may shrink a little, never wraps).
/// VoiceOver hears the label, a toggle that is on as selected, and the hint.
struct CallControl: View {
    let symbol: String
    let label: String
    let id: String
    var kind: CallControlKind = .action
    var hint: String?
    let action: () -> Void

    var body: some View {
        Button {
            Haptics.tap()
            action()
        } label: {
            VStack(spacing: Theme.Space.s) {
                CallControlDisc(symbol: symbol, kind: kind)
                CallControlLabel(text: label)
            }
            .contentShape(.rect)
        }
        .buttonStyle(CallControlPress())
        .accessibilityLabel(label)
        .accessibilityHint(hint ?? "")
        // A toggle that is on reads as selected, as the Phone app's Mute does ("Mute, selected, button").
        .accessibilityAddTraits(kind.isOn ? .isSelected : [])
        .accessibilityIdentifier(id)
        .accessibilityShowsLargeContentViewer {
            ThemedIcon(symbol)
            Text(label)
        }
    }
}

/// A round control's label: the caption level, one line.
struct CallControlLabel: View {
    let text: String
    @Environment(\.isEnabled) private var enabled

    var body: some View {
        Text(text)
            .font(Theme.Font.caption)
            .foregroundStyle(enabled ? Color.primary : Color.secondary)
            .buttonLabelLine()
    }
}

/// Pressed, a round control dims as a plain button does; it does not move (nothing for Reduce Motion to stop).
private struct CallControlPress: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.opacity(configuration.isPressed ? 0.7 : 1)
    }
}

/// Audio, in Speaker's place while Bluetooth, a car or headphones can take the call: the system's route picker
/// (`AVRoutePickerView`) over the round control, so a tap opens the system's own list (iPhone, Speaker, the AirPods…).
/// The picker draws nothing of its own (its glyph is clear): the disc under it is the style's. Filled while the sound
/// goes anywhere but the earpiece.
struct CallAudioRouteControl: View {
    let route: CallRoute

    var body: some View {
        VStack(spacing: Theme.Space.s) {
            CallControlDisc(symbol: Theme.Icon.callAudioRoute, kind: .toggle(on: route.output != .receiver))
                .accessibilityHidden(true)
                .overlay {
                    CallRoutePicker(label: String(localized: "Audio [Call Control]"), value: Self.name(route.output))
                }
            CallControlLabel(text: String(localized: "Audio [Call Control]"))
                .accessibilityHidden(true)
        }
    }

    /// Where the sound goes, as the system's route list names it.
    static func name(_ output: CallRoute.Output) -> String {
        switch output {
        case .receiver: String(localized: "iPhone [Call Audio Route]")
        case .speaker: String(localized: "Speaker [Call Control]")
        case .bluetooth(let device): device
        case .headphones: String(localized: "Headphones [Call Audio Route]")
        case .car: String(localized: "Car [Call Audio Route]")
        case .other: ""
        }
    }
}

/// The system's route picker, its own glyph clear: one VoiceOver element that names the control and where the sound
/// goes now.
private struct CallRoutePicker: UIViewRepresentable {
    let label: String
    let value: String

    func makeUIView(context: Context) -> AVRoutePickerView {
        let picker = AVRoutePickerView()
        picker.tintColor = .clear
        picker.activeTintColor = .clear
        picker.backgroundColor = .clear
        picker.prioritizesVideoDevices = false
        picker.isAccessibilityElement = true
        picker.accessibilityTraits = .button
        picker.accessibilityIdentifier = "call-audio"
        return picker
    }

    func updateUIView(_ picker: AVRoutePickerView, context: Context) {
        picker.accessibilityLabel = label
        picker.accessibilityValue = value
    }
}
#endif
