import Foundation
import PersonaWire

/// The production continuity entry points. `ChatStore` remains the native, authoritative implementation (#8970).
/// A DEBUG mirror observes this protocol; it can never change the native result or apply a core patch to a view.
nonisolated struct NativeContinuityInput: Sendable {
    enum Kind: String, Sendable {
        case latest, pushed, after, reloaded, older, openFold, range, reconnected, loadOlder, fillCompleted, fillFailed, olderFailed, rangeFailed
    }

    let kind: Kind
    var items: [Item] = []
    var span = ItemSpan()
    var backlog = false
    var hasMore: Bool?
    var moreAfter = false
    var rangeMore = false
    var fold: Item?

    static func batch(_ kind: Kind, items: [Item], span: ItemSpan, backlog: Bool = false,
                      hasMore: Bool? = nil, moreAfter: Bool = false) -> Self {
        Self(kind: kind, items: items, span: span, backlog: backlog, hasMore: hasMore, moreAfter: moreAfter)
    }

    static func range(_ items: [Item], fold: Item, more: Bool) -> Self {
        var input = Self(kind: .range, items: items, fold: fold)
        input.rangeMore = more
        return input
    }

    static func event(_ kind: Kind, fold: Item? = nil) -> Self { Self(kind: kind, fold: fold) }
}

nonisolated struct NativeContinuityResult: Sendable {
    let items: [Item]
    let covered: Int
    let history: String?
    let hasMore: Bool
    let loadedOnce: Bool
}

nonisolated struct NativeContinuitySnapshot: Sendable {
    let items: [Item]
    let covered: Int
    let history: String?
    let hasMore: Bool
    let loadedOnce: Bool
}

nonisolated struct NativeContinuityObservation: Sendable {
    let snapshot: NativeContinuitySnapshot
    let native: NativeContinuityResult
    let gap: ChatStore.Gap?
    let coreInput: NativeContinuityInput
}

/// This is deliberately the native method boundary, not a second reducer. The same input reaches the current
/// ChatStore first, then the DEBUG shadow. Transport remains in AppModel, so shadow requests never reach the network.
nonisolated protocol ChatContinuityAdapting: AnyObject {
    func apply(_ input: NativeContinuityInput) -> ChatStore.Gap?
    func nativeResult() -> NativeContinuityResult
    func nativeSnapshot() -> NativeContinuitySnapshot
}

extension ChatStore: ChatContinuityAdapting {
    #if DEBUG
    @ObservationIgnored private static var shadowHook: (@MainActor (ChatStore, NativeContinuityInput, NativeContinuityObservation) -> Void)?

    /// Installed only by the iOS app's DEBUG shadow. Shared/ still has no PersonaCore dependency, so the watch,
    /// widgets and Vision keep their existing native path and cannot link the core through this adapter.
    @MainActor static func installContinuityShadowForTests(_ hook: (@MainActor (ChatStore, NativeContinuityInput, NativeContinuityObservation) -> Void)?) {
        shadowHook = hook
    }

    /// The fixture-level native/C contract tests intentionally drive the legacy native reducer on their own.
    /// Production adapter tests leave this true; it never disables the installed process-wide shadow for real chats.
    @MainActor func setContinuityShadowEnabledForFixtureReplay(_ enabled: Bool) {
        continuityShadowEnabled = enabled
    }

    private func shadow(_ input: NativeContinuityInput, gap: ChatStore.Gap? = nil) {
        guard let hook = Self.shadowHook, continuityShadowEnabled else { return }
        let snapshot = initialContinuitySnapshot ?? nativeSnapshot()
        let result = nativeResult()
        if initialContinuitySnapshot == nil { initialContinuitySnapshot = snapshot }
        var core = input
        let needsNormalization = core.items.contains { item in
            item.modelRefusal != nil || item.image?.lazy == true || item.fold?.images?.isEmpty == false ||
                (item.role == "user" && (item.text ?? "").contains("<persona-view-errors"))
        }
        if needsNormalization {
            core.items = Self.withImageRefs(core.items.publicRefusals(), key)
        }
        hook(self, input, .init(snapshot: snapshot, native: result, gap: gap, coreInput: core))
    }

    /// A caller outside the production adapter keeps the DEBUG shadow's cache baseline without mirroring its input.
    func updateContinuitySnapshotForNativeMutation() {
        guard Self.shadowHook != nil, continuityShadowEnabled else { return }
        initialContinuitySnapshot = nativeSnapshot()
    }
    #endif

    func apply(_ input: NativeContinuityInput) -> ChatStore.Gap? {
        switch input.kind {
        case .latest, .pushed:
            let gap = applyNative(input.items, span: input.span, backlog: input.backlog,
                                  hasMore: input.hasMore, moreAfter: input.moreAfter)
            #if DEBUG
            shadow(input, gap: gap)
            #endif
            return gap
        case .after:
            let gap = applyNative(input.items, span: input.span, moreAfter: input.moreAfter)
            #if DEBUG
            shadow(input, gap: gap)
            #endif
            return gap
        case .reloaded:
            replaceNative(with: input.items, hasMore: input.hasMore ?? false, span: input.span)
            #if DEBUG
            shadow(input)
            #endif
            return nil
        case .older:
            prependNative(input.items, hasMore: input.hasMore ?? false)
            #if DEBUG
            shadow(input)
            #endif
            return nil
        case .range:
            if let fold = input.fold { unfoldNative(fold, into: input.items) }
            #if DEBUG
            shadow(input)
            #endif
            return nil
        case .openFold, .reconnected, .loadOlder, .fillCompleted, .fillFailed, .olderFailed, .rangeFailed:
            #if DEBUG
            shadow(input)
            #endif
            return nil
        }
    }

    #if DEBUG
    /// An explicit native-only input for a fixture replay or a legacy call that is not part of the production adapter.
    /// It updates the snapshot exactly as apply does but does not mirror the input a second time.
    func applyNativeOnly(_ input: NativeContinuityInput) -> ChatStore.Gap? {
        let enabled = continuityShadowEnabled
        continuityShadowEnabled = false
        defer { continuityShadowEnabled = enabled }
        return apply(input)
    }
    #endif

    func nativeResult() -> NativeContinuityResult {
        .init(items: items, covered: covered, history: history, hasMore: hasMore, loadedOnce: loadedOnce)
    }

    func nativeSnapshot() -> NativeContinuitySnapshot {
        .init(items: items, covered: covered, history: history, hasMore: hasMore, loadedOnce: loadedOnce)
    }
}
