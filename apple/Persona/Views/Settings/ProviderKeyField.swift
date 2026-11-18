import SwiftUI
import PersonaWire

/// A model provider's key, or a secret provider's reference in its place (docs/extensions/secret-providers.md "Launcher
/// keys"): "Use a Reference" where the hub takes references (`references`), the reference in plain text, and what a stored
/// reference names; as an extension's secret does (ExtensionsViews `ExtensionSecretSheet`). Switching clears what is typed.
struct ProviderKeyField: View {
    var title: String
    var filled: String
    @Binding var text: String
    @Binding var byReference: Bool
    /// The hub takes a reference for this key (`launcher_secret_references`).
    var references: Bool
    /// The stored key is this reference.
    var stored: SecretReference?
    var identifier: String
    /// The section's footer says what a reference keeps (the provider forms): a new reference's caption would say it twice.
    var explainedBelow = false

    var body: some View {
        if references {
            Toggle(isOn: Binding(get: { byReference }, set: { byReference = $0; text = "" })) {
                Text("Use a Reference").lineLimit(1)
            }
            .accessibilityIdentifier(identifier + ".use-reference")
        }
        if byReference {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                // The placeholder is a reference's shape, the same in every language.
                TextField(String(localized: "Use a Reference"), text: $text, prompt: Text(verbatim: stored?.reference ?? "op://vault/item/field"))
                    .font(Theme.Font.code)
                    .textInputAutocapitalization(.never)
                    .autocorrectionDisabled()
                    .accessibilityIdentifier(identifier + ".reference")
                if let caption = stored.map(ExtensionText.referenceState)
                    ?? (explainedBelow ? nil : String(localized: "Its provider gives the key whenever this provider needs it. Persona keeps only the reference.")) {
                    Text(caption)
                        .font(Theme.Font.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        } else {
            SecretField(title, filled: filled, text: $text, identifier: identifier)
        }
    }
}
