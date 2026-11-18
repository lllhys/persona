package persona.feature.chat

import org.junit.Assert.*
import org.junit.Test
import persona.core.domain.AttachedFile

class AttachedFilesModelTest {
    @Test fun aSentVideoIsAFileNotText() {
        val model = messageModel("看这个\n\n<persona-file kind=\"video\" path=\"/up/a.mov\" name=\"a.mov\" size=\"10\"/>", user = true)
        assertEquals(listOf("/up/a.mov"), model.attached.map { it.path })
        assertEquals("看这个", model.plain)
        // Its path is not also a chip.
        assertTrue(model.files.isEmpty())
    }

    @Test fun editingPutsTheFilesBack() {
        val raw = "看这个\n\n<persona-file kind=\"video\" path=\"/up/a &amp; b.mov\" name=\"a.mov\" size=\"10\" duration=\"12.6\"/>"
        val edit = messageModel(raw, user = true).editText()
        assertEquals("看这个\n\n<persona-file kind=\"video\" path=\"/up/a &amp; b.mov\" name=\"a.mov\" size=\"10\" duration=\"12.6\"/>", edit)
        // Sent again, it is the same message.
        assertEquals(messageModel(raw, user = true).attached, messageModel(edit, user = true).attached)
        assertEquals("纯文字", messageModel("纯文字", user = true).editText())
    }

    @Test fun outgoingFilesFoldPastFourAndOpenInPlace() {
        val files = List(10) { AttachedFile("file", "/up/$it.txt", "$it.txt", it.toLong()) }
        assertFalse(attachedFilesFold(files.take(4), alignEnd = true))
        assertEquals(4, visibleAttachedFiles(files.take(4), alignEnd = true, expanded = false).size)
        assertTrue(attachedFilesFold(files.take(5), alignEnd = true))
        assertEquals(listOf("0.txt", "1.txt", "2.txt"),
            visibleAttachedFiles(files.take(5), alignEnd = true, expanded = false).map { it.name })
        assertEquals(5, visibleAttachedFiles(files.take(5), alignEnd = true, expanded = true).size)
        assertEquals(10, visibleAttachedFiles(files, alignEnd = true, expanded = true).size)
        // Answer-derived cards keep their existing full list.
        assertFalse(attachedFilesFold(files, alignEnd = false))
        assertEquals(10, visibleAttachedFiles(files, alignEnd = false, expanded = false).size)
    }

    @Test fun ordinaryFileTagsStayOutOfCopyShareAndQuoteText() {
        val raw = "Read\n\n<persona-file kind=\"file\" path=\"/up/a.txt\" name=\"a.txt\" size=\"10\"/>"
        val model = messageModel(raw, user = true)
        assertEquals("Read", model.plain)
        assertFalse(model.plain.contains("persona-file"))
        assertEquals(raw, model.editText())
    }

    @Test fun documentsKnowWhatTheyAre() {
        assertEquals(R.string.chat_doc_word, documentKind("/a/b.DOCX"))
        assertEquals(R.string.chat_doc_keynote, documentKind("/a/b.key"))
        assertEquals(R.string.chat_doc_other, documentKind("/a/b"))
    }
}
