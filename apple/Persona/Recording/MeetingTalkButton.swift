#if os(iOS)
import BeautifulUI
import SwiftUI

/// The meeting page's **Talk** (#3862 phase 4, DESIGN_SYSTEM.md「Meeting page (#3862)」), in send's place while the field
/// is empty (as the chat's voice button takes it), so the row keeps one solid button. A capsule in the composer's ink with
/// its icon and its word. Like every mic (#9619, #9658): a tap turns it on and the next tap ends it and sends; held, it is
/// on until the finger lifts. The span opens as the finger lands either way, so the first words count: let go before
/// `Theme.Hold.delay` it stays open as the page's talk window (`keep`), held longer it ends as the finger lifts. On, it
/// takes Persona's color and grows a little (not under Reduce Motion), and the words over the field say Persona hears
/// (`MeetingPageBar`). VoiceOver's double tap opens the same window, and another ends it. Its hint says where the words
/// go. Taken off the screen while held (Just Me, the page left, a word typed with another finger), it ends the hold: the
/// finger's lift would never reach it; a window stays, ending by itself.
struct MeetingTalkButton: View {
    /// A span is open from this button.
    var held: Bool
    /// The open span is a talk window (a tap, VoiceOver, the Lock Screen): the next touch ends it as it lifts.
    var inWindow = false
    /// What Talk does with the audio (VoiceOver's hint): sent to the voice service, or transcribed on the phone.
    var hint: String
    var press: () -> Void
    var release: () -> Void
    /// VoiceOver's double tap: a talk window, which ends on its own or on the next double tap.
    var window: () -> Void
    /// The button left the screen: a hold still open ends (a window stays).
    var gone: () -> Void
    /// Let go before it was a hold: the span the touch opened stays open as the page's window.
    var keep: () -> Void = {}

    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The finger is down: set by the gesture, and back to false however it ends (lifted, cancelled by the system).
    @GestureState private var touching = false
    /// Counts the touch toward a hold.
    @State private var counting: Task<Void, Never>?
    /// This touch has been down `Theme.Hold.delay`: letting go ends the span.
    @State private var long = false
    /// This touch came while a window was open: letting go ends it.
    @State private var ends = false

    var body: some View {
        face
            .scaledFrame(minHeight: Theme.Size.tapTarget)
            .contentShape(.capsule)
            .gesture(DragGesture(minimumDistance: 0).updating($touching) { _, state, _ in state = true })
            .onChange(of: touching) { _, down in down ? touched() : lifted() }
            .onDisappear { gone() }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(Text("Talk [Meeting Talk]"))
            .accessibilityValue(held ? Text("Listening to you…") : Text(verbatim: ""))
            .accessibilityHint(Text(hint))
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { held ? release() : window() }
            .accessibilityIdentifier("meeting-talk")
            .shellScaled()
            .transition(.scale.combined(with: .opacity))
    }

    private func touched() {
        long = false
        ends = held && inWindow
        guard !ends else { return }
        press()
        counting = Task {
            guard (try? await Task.sleep(for: Theme.Hold.delay)) != nil else { return }
            long = true
        }
    }

    private func lifted() {
        counting?.cancel()
        counting = nil
        if ends {
            ends = false
            release()
        } else if long {
            release()
        } else {
            keep()
        }
    }

    private var face: some View {
        HStack(spacing: Theme.Space.xs) {
            ThemedIcon(Theme.Icon.talk).font(Theme.Font.bodyHeavy)
            Text("Talk [Meeting Talk]").font(Theme.Font.bodyStrong).lineLimit(1).minimumScaleFactor(0.8)
        }
        .foregroundStyle(held ? Theme.ai : ink)
        .padding(.horizontal, Theme.Space.m)
        .scaledFrame(minWidth: Theme.Size.wellL, minHeight: Theme.Size.wellL)
        .background(held ? Theme.aiTint : fill, in: .capsule)
        .scaleEffect(held && !reduceMotion ? Theme.Recording.talkHeldScale : 1)
        .animation(reduceMotion ? nil : Theme.Motion.state, value: held)
    }

    /// Send's colors (`ComposerRoundButton`): ink on paper, the style's own under BeautifulUI.
    private var ink: Color { AppStyle.current.usesBeautifulUI ? theme.colors.canvas : Theme.Shell.inkText }
    private var fill: Color { AppStyle.current.usesBeautifulUI ? theme.colors.ink : Theme.Shell.ink }
}
#endif
