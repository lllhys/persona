package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Test

/** A time as a work line writes it (iOS `WorkLine.duration`): the work line, a subagent's card and a wake-up's delay. */
class WorkTimeTest {
    @Test fun readsAsIOSWritesIt() {
        listOf(
            0L to "0 s", 39L to "39 s", 59L to "59 s", 60L to "1 min", 80L to "1m 20s", 120L to "2 min", 3599L to "59m 59s",
            3600L to "1 hr 0 min", 3900L to "1 hr 5 min", 7200L to "2 hr 0 min", -5L to "0 s",
        ).forEach { (seconds, words) -> assertEquals("$seconds", words, workTime(seconds).english()) }
    }

    @Test fun readsInChineseAndJapanese() {
        assertEquals("39 秒", workTime(39).zh())
        assertEquals("2 分", workTime(120).zh())
        assertEquals("1 分 20 秒", workTime(80).zh())
        assertEquals("1 小时 5 分", workTime(3900).zh())
        assertEquals("39秒", workTime(39).ja())
        assertEquals("1分20秒", workTime(80).ja())
        assertEquals("1時間5分", workTime(3900).ja())
    }
}
