import Foundation

// The interface language (docs/protocol/i18n.md「Choosing a language」, docs/apple/ios.md「Interface language」): zh-Hans, en or ja.
//
// Settings › Appearance › Language picks Match System (default) or one of the three. The choice is applied the way iOS's own per-app
// language setting applies it: `AppleLanguages` in the app's defaults, read by Foundation when the process starts.
// So a change takes effect the next time the app opens (Foundation's `String(localized:)` cannot be pointed at
// another language in a running process); the settings page says so. Following the system resolves the system's
// first preferred language with docs/protocol/i18n.md's rule rather than the bundle's matching (which would fall back to the
// development language for French and miss Traditional Chinese).
//
// The extensions and the watch use the same language: the app writes the resolved code into the App Group
// (`LanguageStore.shared`); the watch gets it from the iPhone (WatchSync).

nonisolated enum AppLanguage: String, CaseIterable, Identifiable, Sendable {
    case system
    case zhHans = "zh-Hans"
    case en
    case ja

    var id: String { rawValue }

    /// The three interface languages, as sent to the hub (`Accept-Language`, `client_hello.locale`).
    static let codes = ["zh-Hans", "en", "ja"]

    /// The option's name in the picker: the languages' own names never change with the interface language.
    var title: String {
        switch self {
        case .system: String(localized: "Match System")
        case .zhHans: "简体中文" // i18n-ignore
        case .en: "English"
        case .ja: "日本語" // i18n-ignore
        }
    }

    /// The language this choice means when the system's first preferred language is `system`.
    func resolved(system: String?) -> String {
        self == .system ? Self.resolve(system) : rawValue
    }

    /// docs/protocol/i18n.md: any Chinese → zh-Hans, any Japanese → ja, anything else (or nothing) → en. Only the language
    /// subtag counts, case-insensitive, `_` read as `-`.
    static func resolve(_ tag: String?) -> String {
        let lang = (tag ?? "").replacingOccurrences(of: "_", with: "-").split(separator: "-").first.map { $0.lowercased() }
        switch lang {
        case "zh": return "zh-Hans"
        case "ja": return "ja"
        default: return "en"
        }
    }
}

/// Where the choice lives and how it reaches Foundation. One per process.
nonisolated final class LanguageStore: @unchecked Sendable {
    static let shared = LanguageStore()

    static let choiceKey = "persona.language"
    /// The code the app resolved, for the extensions and the watch (they cannot see the app's own defaults).
    static let resolvedKey = "persona.language.resolved"
    /// What this process last wrote to `AppleLanguages`: a different value there means iOS's per-app language
    /// setting (Settings › Persona › Language) changed it.
    static let appliedKey = "persona.language.applied"
    static let appleLanguages = "AppleLanguages"

    private let defaults: UserDefaults
    private let group: UserDefaults?
    /// `-AppleLanguages (…)` on the command line (tests, screenshots): wins for this run, never persisted.
    private let argument: Any?
    /// `AppleLanguages` in the app's own domain (what this app or iOS's per-app setting wrote).
    private let appValue: () -> Any?
    /// The device's preferred languages.
    private let systemValue: () -> Any?
    /// The language Foundation picked for this process (`Bundle.main`), fixed until the next launch.
    let launched: String

    init(defaults: UserDefaults = .standard, group: UserDefaults? = LanguageStore.groupDefaults(), bundle: Bundle = .main,
         argument: Any? = UserDefaults.standard.volatileDomain(forName: UserDefaults.argumentDomain)[LanguageStore.appleLanguages],
         appValue: @escaping () -> Any? = { CFPreferencesCopyAppValue(LanguageStore.appleLanguages as CFString, kCFPreferencesCurrentApplication) },
         systemValue: @escaping () -> Any? = {
             CFPreferencesCopyValue(LanguageStore.appleLanguages as CFString, kCFPreferencesAnyApplication,
                                    kCFPreferencesCurrentUser, kCFPreferencesAnyHost)
         }) {
        self.defaults = defaults
        self.group = group
        self.argument = argument
        self.appValue = appValue
        self.systemValue = systemValue
        let first = bundle.preferredLocalizations.first ?? bundle.developmentLocalization
        launched = AppLanguage.resolve(first)
    }

    static func groupDefaults() -> UserDefaults? {
        guard let name = NativeStateMigration.infoString("PersonaAppGroup") else { return nil }
        return UserDefaults(suiteName: name)
    }

    var choice: AppLanguage {
        get { defaults.string(forKey: Self.choiceKey).flatMap(AppLanguage.init(rawValue:)) ?? .system }
        set {
            defaults.set(newValue.rawValue, forKey: Self.choiceKey)
            apply()
        }
    }

    /// The language the interface will be in after the next launch (and is in now, unless the choice changed).
    var current: String { choice.resolved(system: systemFirst) }

    /// Whether the app has to be opened again before `current` shows.
    var needsRelaunch: Bool { current != launched }

    /// The system's first preferred language for this app: a launch argument (`-AppleLanguages (ja)`: tests,
    /// screenshots), else what iOS's per-app setting wrote, else the device's list.
    var systemFirst: String? {
        if let argument { return Self.first(argument) }
        let app = Self.first(appValue())
        if let app, app != defaults.string(forKey: Self.appliedKey) { return app }
        return Self.first(systemValue())
    }

    /// Writes the resolved language where Foundation (next launch) and the extensions read it. Called at launch and
    /// whenever the choice changes. A launch argument wins for this run and is not persisted.
    @discardableResult
    func apply() -> String {
        guard argument == nil else {
            group?.set(current, forKey: Self.resolvedKey)
            return current
        }
        // iOS's per-app setting picked something since this app last wrote it: adopt it as the choice, so Settings ›
        // Appearance › Language and the system's page agree.
        if let app = Self.first(appValue()), let applied = defaults.string(forKey: Self.appliedKey), app != applied {
            defaults.set(AppLanguage.resolve(app), forKey: Self.choiceKey)
        }
        let next = current
        defaults.set([next], forKey: Self.appleLanguages)
        defaults.set(next, forKey: Self.appliedKey)
        group?.set(next, forKey: Self.resolvedKey)
        return next
    }

    /// An extension's or the watch's language: the app's resolved code when it has written one, else its own
    /// system's. Also written to this process's `AppleLanguages`, so its next launch matches.
    func follow(_ code: String?) {
        guard let code, AppLanguage.codes.contains(code), argument == nil else { return }
        defaults.set([code], forKey: Self.appleLanguages)
    }

    /// The App Group's resolved code (extensions).
    var groupResolved: String? { group?.string(forKey: Self.resolvedKey) }

    private static func first(_ value: Any?) -> String? {
        if let list = value as? [String] { return list.first }
        if let s = value as? String { // a launch argument arrives as the property list text "(ja)"
            let trimmed = s.trimmingCharacters(in: CharacterSet(charactersIn: "() \"\n"))
            return trimmed.split(separator: ",").first.map { $0.trimmingCharacters(in: CharacterSet(charactersIn: " \"")) }
        }
        return nil
    }
}

extension Locale {
    /// The locale for formatting dates and numbers (docs/protocol/i18n.md「Dates, numbers, and sorting」): the system's when its language
    /// is the interface language (keeps regional habits, en-GB's day order), else the interface language's.
    nonisolated static var interface: Locale {
        let ui = LanguageStore.shared.launched
        let current = Locale.current
        return AppLanguage.resolve(current.identifier) == ui ? current : Locale(identifier: ui)
    }
}

extension LanguageStore {
    /// The interface language the app last resolved (App Group), else this process's. The widget extension draws
    /// the Live Activity for hours in one process, so it looks this up instead of waiting for its next launch.
    nonisolated static var drawingLocale: Locale { Locale(identifier: shared.groupResolved ?? shared.launched) }
}

/// `String(localized:)`, except that the interface locale is explicit. In the widget extension it follows the app's
/// current language at once (`LanguageStore.drawingLocale`); elsewhere it follows the launched interface language.
/// An explicit locale also makes English keys with a bracketed suffix resolve to their zh-Hans catalog entries in
/// the unit-test host. A `LocalizedStringResource` parameter, so the compiler still extracts the literals passed in.
nonisolated func interfaceText(_ resource: LocalizedStringResource) -> String {
    var resource = resource
    resource.locale = interfaceTextLocale
    return String(localized: resource)
}

/// The locale `interfaceText(_:)` resolves words in. Words kept once per locale (docs/apple/performance.md P4) key on it,
/// so a change of language resolves them again.
nonisolated var interfaceTextLocale: Locale {
    #if WIDGET_EXTENSION
    LanguageStore.drawingLocale
    #else
    .interface
    #endif
}
