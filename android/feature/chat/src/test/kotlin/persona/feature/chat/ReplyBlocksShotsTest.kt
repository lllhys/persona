package persona.feature.chat

import android.content.ClipboardManager
import android.content.Context
import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onAllNodesWithContentDescription
import androidx.compose.ui.test.onLast
import androidx.compose.ui.test.onNodeWithContentDescription
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onAllNodesWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.Item
import persona.core.designsystem.*
import java.io.File

/**
 * A reply's code blocks, inline code and quotes as Telegram draws them (#2984): a fence's language and Copy in a bar on
 * top of it, inline code in the body's color on its well, and quote cards, a long one folded. In every style, light and
 * dark; PNGs in `build/reply-block-shots/` (`-Dpersona.shots=<dir>`).
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
@Config(sdk = [35], qualifiers = "zh-w400dp-h1400dp-xhdpi")
class ReplyBlocksShotsTest {
    @get:Rule val rule = createComposeRule()

    private val reply = "先跑 `npm run build`，再清掉 `.cache` 目录：\n\n```ts\nexport async function clear(page: string) {\n" +
        "  await caches.delete(`page:\${page}`)\n  return reload(page)\n}\n```\n\n不带语言的也一样：\n\n```\nrm -rf .cache && npm run build\n```\n\n" +
        "> 缓存键改了以后，旧缓存要等一天才过期。\n\n" +
        "> 上线前请确认三件事：一是首页的请求已经合并，跑过一遍回归测试；二是价格页换成了新的缓存键，旧键在 CDN 上的副本会在一天内过期；" +
        "三是文档页最后做，它的请求最多，改动的风险最低。如果中途发现首屏时间反而变长，先回滚价格页，再看是不是缓存没命中。\n\n有问题再找我。"

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/reply-block-shots").also { it.mkdirs() }

    @Test fun shotsInEveryStyle() {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Column(Modifier.testTag("shot").width(400.dp).background(PersonaTheme.palette.background).padding(Tokens.Space.l),
                    verticalArrangement = Arrangement.spacedBy(Tokens.Space.l)) {
                    MessageContent(Item("a1", role = "assistant", kind = "text", text = reply), user = false)
                }
            }
        }
        for (s in AppStyle.entries) for (d in listOf(false, true)) {
            style = s; dark = d
            rule.waitForIdle()
            Thread.sleep(200)
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "reply-blocks-${s.name.lowercase()}-${if (d) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
    }

    @Test fun copyTakesTheWholeBlockAndALongQuoteFolds() {
        rule.setContent {
            PersonaTheme(darkTheme = false, reduceMotion = true, style = AppStyle.Glass) {
                CompositionLocalProvider(LocalReplyStreaming provides false) {
                    Column(Modifier.width(400.dp).padding(Tokens.Space.l)) {
                        MessageContent(Item("a1", role = "assistant", kind = "text", text = reply), user = false)
                    }
                }
            }
        }
        rule.waitForIdle()
        // Two code blocks, each with Copy in its bar; the first copies its code, without the fence.
        val copies = rule.onAllNodesWithText("复制")
        assertEquals(2, copies.fetchSemanticsNodes().size)
        copies[0].performClick()
        val clipboard = ApplicationProvider.getApplicationContext<Context>().getSystemService(ClipboardManager::class.java)
        assertEquals("export async function clear(page: string) {\n  await caches.delete(`page:\${page}`)\n  return reload(page)\n}",
            clipboard.primaryClip?.getItemAt(0)?.text?.toString())
        // The long quote is folded (the folded words open it too); its chevron opens it and folds it again.
        rule.onAllNodesWithContentDescription("展开全部").onLast().performClick()
        rule.onNodeWithContentDescription("收起").assertExists().performClick()
        rule.onAllNodesWithContentDescription("展开全部").onLast().assertExists()
    }

    @Test fun inlineCodeWordsAreTheSpans() {
        assertEquals("npm run build", codeSpanWords("`npm run build`"))
        assertEquals("a `b` c", codeSpanWords("`` a `b` c ``"))
        assertEquals("  ", codeSpanWords("`  `"))
    }
}
