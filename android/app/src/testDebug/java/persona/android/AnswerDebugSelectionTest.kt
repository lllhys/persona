package persona.android

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.After
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import persona.android.debug.DebugAnswer
import persona.core.domain.answer.KotlinAnswerAdapter
import persona.core.domain.answer.ShadowAnswerAdapter

@RunWith(RobolectricTestRunner::class)
class AnswerDebugSelectionTest {
    @Before fun freshProcess() { DebugAnswer.resetForTest() }
    @After fun resetSelection() { DebugAnswer.resetForTest() }

    @Test fun debugLauncherSelectionIsOffByDefault() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        assertTrue(AnswerSelection.choose(context) is KotlinAnswerAdapter)
    }

    @Test fun persistedOptInIsReadByTheNextProcessSelection() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        DebugAnswer.persist(context, true)
        DebugAnswer.resetForTest()
        assertTrue(AnswerSelection.choose(context) is ShadowAnswerAdapter)
    }

    @Test fun persistedOffIsReadByTheNextProcessSelection() {
        val context = ApplicationProvider.getApplicationContext<Context>()
        DebugAnswer.persist(context, true)
        DebugAnswer.resetForTest()
        assertTrue(AnswerSelection.choose(context) is ShadowAnswerAdapter)
        DebugAnswer.persist(context, false)
        DebugAnswer.resetForTest()
        assertTrue(AnswerSelection.choose(context) is KotlinAnswerAdapter)
    }
}
