import Foundation

/// A secret provider's reference a setting holds: whose it is, the reference and what it names in words
/// (docs/extensions/secret-providers.md): an extension's secret, or a model provider's key and a computer's own key for it.
public nonisolated struct SecretReference: Codable, Hashable, Sendable {
    public init(provider: String, providerName: String, reference: String, title: String?) {
        self.provider = provider
        self.providerName = providerName
        self.reference = reference
        self.title = title
    }

    public var provider: String
    public var providerName: String
    public var reference: String
    public var title: String?
    private enum CodingKeys: String, CodingKey { case provider, reference, title, providerName = "provider_name" }
}
