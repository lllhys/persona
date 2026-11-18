package persona.feature.settings

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.api.ProviderPasteDraft
import persona.core.contracts.api.ProviderPasteNote
import persona.core.contracts.api.ProviderPasteSkipped
import persona.core.contracts.api.PastedProvider

/** Every parser note is shown in words, and skipped values never become UI text. */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class ProviderPasteNotesTest {
    private val resources get() = ApplicationProvider.getApplicationContext<Context>().resources

    @Test fun everyKnownAndFutureNoteCodeHasReadableText() {
        val codes = listOf("unrecognized", "invalid_json", "line_unused", "not_claude", "no_gateway", "url_shell", "url_user", "url_query",
            "key_shell", "no_key", "key_helper", "args_left_out", "header_unreadable", "curl_option", "curl_path", "cindy_kind", "cindy_auth",
            "cindy_endpoint", "cindy_request_path", "link_invalid", "link_version", "link_too_long", "link_field", "too_many", "future_code")
        codes.forEach { code ->
            val words = providerPasteNoteText(ProviderPasteNote(code, line = 3, name = "example"), resources)
            assertTrue("$code has no words", words.isNotBlank())
            assertFalse("the raw code is not the explanation", words.contains(code))
            assertTrue("the line or name is retained: $words", words.contains("3") || words.contains("example"))
        }
        assertEquals("没有可用密钥，请在保存前输入", providerPasteNoteText(ProviderPasteNote("no_key"), resources))
        assertEquals("JSON 无法解析（第 2 行）", providerPasteNoteText(ProviderPasteNote("invalid_json", line = 2), resources))
        assertEquals("这个 curl 选项未使用（--user）", providerPasteNoteText(ProviderPasteNote("curl_option", name = "--user"), resources))
        assertEquals("这部分内容未被使用（第 9 行）", providerPasteNoteText(ProviderPasteNote("future_code", line = 9), resources))
    }

    @Test @Config(qualifiers = "en") fun providerAndModelCountsUsePluralResources() {
        assertEquals("1 model", resources.getQuantityString(R.plurals.settings_paste_models_count, 1, 1))
        assertEquals("2 models", resources.getQuantityString(R.plurals.settings_paste_models_count, 2, 2))
        assertEquals("Choose the 1 provider found.", resources.getQuantityString(R.plurals.settings_paste_several, 1, 1))
        assertEquals("Choose one of the 2 providers found.", resources.getQuantityString(R.plurals.settings_paste_several, 2, 2))
    }

    @Test fun draftDetailsNameEveryKeptAndSkippedVariableAndHeaderButNoValues() {
        val draft = ProviderPasteDraft(
            provider = PastedProvider(headers = "X-Team: secret-value\nX-Other: another-secret"),
            envNames = listOf("MAX_OUTPUT", "DISABLE_TELEMETRY"),
            skippedEnv = listOf(ProviderPasteSkipped("OTHER_TOKEN", "credential"), ProviderPasteSkipped("HOME", "shell")),
            skippedHeaders = listOf(ProviderPasteSkipped("X-Api-Key", "credential"), ProviderPasteSkipped("Content-Type", "protocol")),
        )
        val lines = draft.configurationLines(resources)
        assertEquals(listOf(
            "带入环境变量：MAX_OUTPUT, DISABLE_TELEMETRY",
            "未带入环境变量（凭据）：OTHER_TOKEN",
            "未带入环境变量（shell 展开）：HOME",
            "带入请求头：X-Team, X-Other",
            "未带入请求头（凭据）：X-Api-Key",
            "未带入请求头（协议）：Content-Type",
        ), lines)
        assertFalse(lines.joinToString("\n").contains("secret"))
        assertFalse(lines.joinToString("\n").contains("another"))
    }
}
