package persona.core.domain.answer

import persona.core.contracts.Interaction

/**
 * One boundary for the existing Android answer builder. Kotlin is the only production implementation; callers keep
 * the original `AnswerBuilder.build/display` signatures and receive no native observer state.
 */
interface AnswerAdapter {
    fun build(
        interaction: Interaction,
        action: AnswerAction,
        draft: AnswerDraft = AnswerDraft(),
        context: AnswerContext = AnswerContext(),
    ): AnswerResult

    fun display(label: String): OptionDisplay
}

object AnswerAdapters {
    @Volatile private var installed: AnswerAdapter = KotlinAnswerAdapter()

    /** Tests and the DEBUG launcher install an observer before answering; Release uses this Kotlin path unchanged. */
    @JvmStatic fun install(adapter: AnswerAdapter) {
        installed = adapter
    }

    @JvmStatic fun current(): AnswerAdapter = installed
}

class KotlinAnswerAdapter : AnswerAdapter {
    override fun build(
        interaction: Interaction,
        action: AnswerAction,
        draft: AnswerDraft,
        context: AnswerContext,
    ): AnswerResult = KotlinAnswerBuilder.build(interaction, action, draft, context)

    override fun display(label: String): OptionDisplay = KotlinAnswerBuilder.display(label)
}
