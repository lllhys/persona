package persona.android

import android.content.Context
import persona.android.debug.DebugAnswer
import persona.core.domain.answer.AnswerAdapter
import persona.core.domain.answer.KotlinAnswerAdapter

internal object AnswerSelection {
    fun choose(context: Context): AnswerAdapter = DebugAnswer.wrap(context, KotlinAnswerAdapter())
}
