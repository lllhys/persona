package persona.feature.chat

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35])
class FileAttachmentResourcesTest {
    private val resources = ApplicationProvider.getApplicationContext<Context>().resources

    @Config(qualifiers = "en") @Test fun englishFileWordsAndPlurals() {
        check("Please look at the attached files", "Up to 10 files in one message", "Waiting", "Show less")
        assertEquals("1 more file", resources.getQuantityString(R.plurals.chat_files_more, 1, 1))
        assertEquals("2 more files", resources.getQuantityString(R.plurals.chat_files_more, 2, 2))
    }

    @Config(qualifiers = "zh") @Test fun chineseFileWordsAndPlurals() {
        check("请看附件", "一条消息最多 10 个文件", "等待中", "收起")
        assertEquals("还有 2 个文件", resources.getQuantityString(R.plurals.chat_files_more, 2, 2))
    }

    @Config(qualifiers = "ja") @Test fun japaneseFileWordsAndPlurals() {
        check("添付ファイルを見てください", "1件のメッセージに添付できるファイルは10個までです", "待機中", "閉じる")
        assertEquals("ほか2個のファイル", resources.getQuantityString(R.plurals.chat_files_more, 2, 2))
    }

    private fun check(standIn: String, limit: String, waiting: String, less: String) {
        assertEquals(standIn, resources.getString(R.string.chat_composer_files_only))
        assertEquals(limit, resources.getString(R.string.chat_composer_file_limit))
        assertEquals(waiting, resources.getString(R.string.chat_attach_waiting))
        assertEquals(less, resources.getString(R.string.chat_files_show_less))
    }
}
