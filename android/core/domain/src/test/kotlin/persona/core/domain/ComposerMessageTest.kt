package persona.core.domain

import org.junit.Assert.assertEquals
import org.junit.Test

/** New Agent's first message is written as the chat composer writes its messages (#2321); feature:agents compares the two. */
class ComposerMessageTest {
    private val tag = "\n\n<attached_images>\n/tmp/photo.jpg\n</attached_images>"
    private val file = AttachedFile("file", "/tmp/notes.txt", "notes.txt", 128)

    @Test fun photosGoInTheirBlockAfterTheWordsAndFiles() {
        assertEquals("看这里$tag", ComposerMessage.text("看这里", listOf("/tmp/photo.jpg"), emptyList(), photosOnly = "请看图片"))
        assertEquals("请看图片$tag", ComposerMessage.text("", listOf("/tmp/photo.jpg"), emptyList(), photosOnly = "请看图片"))
        assertEquals("请看图片$tag", ComposerMessage.text("  \n", listOf("/tmp/photo.jpg"), emptyList(), photosOnly = "请看图片"))
        // Photos keep their stand-in when a file is attached too.
        assertEquals("请看图片\n\n${file.toXml()}$tag",
            ComposerMessage.text("", listOf("/tmp/photo.jpg"), listOf(file), photosOnly = "请看图片", filesOnly = "请看附件"))
        assertEquals("只有字", ComposerMessage.text("只有字", emptyList(), emptyList()))
        assertEquals("", ComposerMessage.text("", emptyList(), emptyList(), photosOnly = "请看图片"))
        assertEquals("", ComposerMessage.text("  \n", emptyList(), emptyList(), photosOnly = "请看图片"))
    }

    @Test fun filesAreTagsAfterTheExactWordsNeverQuotedPathsOnTheWordsLine() {
        val spaced = AttachedFile("file", "/tmp/my notes.txt", "my notes.txt", 12)
        val quoted = AttachedFile("file", "/tmp/it's.md", "it's.md", 13)
        assertEquals("修一下\n\n${spaced.toXml()}\n${quoted.toXml()}",
            ComposerMessage.text("修一下", emptyList(), listOf(spaced, quoted), filesOnly = "请看附件"))
        assertEquals("  keep\nthis exactly  \n\n${file.toXml()}",
            ComposerMessage.text("  keep\nthis exactly  ", emptyList(), listOf(file), filesOnly = "unused"))
        assertEquals("'/tmp/a(1).txt'", ComposerMessage.quotePath("/tmp/a(1).txt"))
    }

    @Test fun filesOnlyUseTheSameStandInInEveryLanguage() {
        for ((language, words) in mapOf("en" to "Please look at the attached files", "zh-Hans" to "请看附件", "ja" to "添付ファイルを見てください")) {
            assertEquals(language, "$words\n\n${file.toXml()}",
                ComposerMessage.text("", emptyList(), listOf(file), filesOnly = words, photosOnly = "unused", videosOnly = "unused"))
            assertEquals(language, "$words\n\n${file.toXml()}",
                ComposerMessage.text(" \n", emptyList(), listOf(file), filesOnly = words))
        }
    }

    @Test fun videoTagsPrecedeFileTagsAndThePhotoBlock() {
        val video = AttachedFile("video", "/tmp/clip.mp4", "clip.mp4", 2048, 3.0)
        assertEquals("Please look at the attached files\n\n${video.toXml()}\n${file.toXml()}",
            ComposerMessage.text("", emptyList(), listOf(file), listOf(video),
                photosOnly = "Please look at the photos", videosOnly = "Please look at the video", filesOnly = "Please look at the attached files"))
        assertEquals("Please look at the photos\n\n${video.toXml()}\n${file.toXml()}$tag",
            ComposerMessage.text("", listOf("/tmp/photo.jpg"), listOf(file), listOf(video),
                photosOnly = "Please look at the photos", videosOnly = "Please look at the video", filesOnly = "Please look at the attached files"))
        assertEquals("Please look at the video\n\n${video.toXml()}",
            ComposerMessage.text("", emptyList(), emptyList(), listOf(video), photosOnly = "Please look at the photos", videosOnly = "Please look at the video"))
        assertEquals("Read\n\n${video.toXml()}\n${file.toXml()}$tag",
            ComposerMessage.text("Read", listOf("/tmp/photo.jpg"), listOf(file), listOf(video), filesOnly = "unused"))
    }

    @Test fun fileNamesAndPathsKeepTheirXmlEscapingAndRoundTrip() {
        val escaped = AttachedFile("file", "/up/a&\"<> ' 中文.txt", "n&\"<> ' 中文.txt", 86)
        val raw = ComposerMessage.text("Read", emptyList(), listOf(escaped))
        assertEquals("Read\n\n<persona-file kind=\"file\" path=\"/up/a&amp;&quot;&lt;&gt; ' 中文.txt\" " +
            "name=\"n&amp;&quot;&lt;&gt; ' 中文.txt\" size=\"86\"/>", raw)
        assertEquals(listOf(escaped) to "Read", parseAttachedFiles(raw))
    }
}
