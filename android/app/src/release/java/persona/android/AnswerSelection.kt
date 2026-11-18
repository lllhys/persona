package persona.android

import android.content.Context
import persona.core.domain.answer.AnswerAdapter
import persona.core.domain.answer.KotlinAnswerAdapter

/** Release has no debug preferences, system properties, Rust answer call or shadow implementation. */
internal object AnswerSelection {
    fun choose(context: Context): AnswerAdapter = KotlinAnswerAdapter()
}
