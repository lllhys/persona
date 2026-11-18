package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test

/** The history dims a hub title's "MMDD | Type | " and shows any other title as it is (iOS `ConversationTitleTests`). */
class ConversationTitleTest {
    @Test fun splitsTheHubsFormat() {
        val title = ConversationTitle.parse("1101 | 修复 | 搜索框和顶栏重叠")!!
        assertEquals("1101 | 修复 | ", title.head)
        assertEquals("搜索框和顶栏重叠", title.topic)
        assertEquals("早报", ConversationTitle.parse("0102 | 定时 | 早报")?.topic)
        assertEquals("1101 | Fix | ", ConversationTitle.parse("1101 | Fix | Search overlaps")?.head)
        // A bar inside the topic stays in the topic.
        assertEquals("A | B", ConversationTitle.parse("1101 | 研究 | A | B")?.topic)
        assertEquals("a topic is trimmed", "x", ConversationTitle.parse("1101 | 研究 |  x ")?.topic)
        assertEquals(ConversationTitle.parse("1101 | 修复 | x"), ConversationTitle.parse("1101 | 修复 | x"))
        assertNotNull(ConversationTitle.parse("1101 | 修复 | x"))
    }

    @Test fun leavesOtherTitlesAlone() {
        listOf("帮我约周五吃饭", "1101 修复 搜索框", "11/01 | 修复 | 搜索框", "1101 | 修复 | ", "| 修复 | 搜索框", "abcd | 修复 | x",
            "1101 | 很长的一个类型 | x", "", "Claude Code").forEach { assertNull(it, ConversationTitle.parse(it)) }
    }
}
