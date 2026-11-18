package persona.feature.agents

import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config

/**
 * The Agents filters that are agent states carry the shared state names (`docs/protocol/i18n.md`, "Agent states"), the words
 * the web and iOS use (#9121). Simplified Chinese once said 未读 for Unread, a name that table lists as one no page may coin.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class AgentsFilterNamesTest {
    private val states = listOf(R.string.agents_filter_needs_you, R.string.agents_filter_unread, R.string.agents_filter_working,
        R.string.agents_filter_background_waiting)

    @Test fun theStateFiltersUseTheSharedStateNamesInEveryLanguage() {
        val context = RuntimeEnvironment.getApplication()
        val expected = mapOf(
            "zh" to listOf("需要你", "完成未看", "工作中", "后台等待中"),
            "en" to listOf("Needs you", "Unread", "Working", "Waiting in the background"),
            "ja" to listOf("要対応", "未確認", "作業中", "バックグラウンドで待機中"),
        )
        for ((language, names) in expected) {
            val config = android.content.res.Configuration(context.resources.configuration).apply {
                setLocale(java.util.Locale.forLanguageTag(language))
            }
            val resources = context.createConfigurationContext(config).resources
            assertEquals(language, names, states.map { resources.getString(it) })
        }
    }
}
