package persona.feature.agents

import android.content.res.Configuration
import android.content.res.Resources
import androidx.lifecycle.viewModelScope
import androidx.test.ext.junit.runners.AndroidJUnit4
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.test.*
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RuntimeEnvironment
import org.robolectric.annotation.Config
import persona.core.contracts.CoreText
import persona.core.contracts.HubScope
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.*
import persona.core.domain.AttachedFile
import persona.core.domain.ComposerMessage
import persona.core.designsystem.failureReasonText
import persona.core.hub.HubError
import persona.core.hub.HubFailure
import persona.feature.chat.ChatAppBindings
import persona.feature.chat.ChatComposerDraft
import persona.feature.chat.ChatComposerState
import persona.feature.chat.ChatWiringSource
import persona.feature.chat.ComposerAttachment
import java.io.File
import java.io.IOException
import java.lang.reflect.Proxy
import java.util.Locale

/**
 * New Agent's attachments (#2321, iOS NewAgentAttachments): the first message written as the composer writes it, uploads
 * through the chat's service of the machine's hub group, the Codex rule, failures and retries, removal and another
 * computer picked.
 */
@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en")
@OptIn(kotlinx.coroutines.ExperimentalCoroutinesApi::class)
class NewAgentAttachmentsTest {
    private val resources get() = RuntimeEnvironment.getApplication().resources
    private val machines = MutableStateFlow(listOf(Machine("first", "MacBook", online = true), Machine("second", "Studio", online = true)))

    private fun localizedResources(language: String): Resources {
        val configuration = Configuration(resources.configuration).apply { setLocale(Locale.forLanguageTag(language)) }
        return RuntimeEnvironment.getApplication().createConfigurationContext(configuration).resources
    }

    private fun test(block: suspend TestScope.() -> Unit) = runTest {
        Dispatchers.setMain(StandardTestDispatcher(testScheduler))
        try { block() } finally { Dispatchers.resetMain() }
    }

    private fun photo(id: String, path: String? = null) =
        ComposerAttachment(id, "$id.heic", "content://media/$id", image = true, path = path, uploadState = if (path != null) DraftUploadState.Uploaded else DraftUploadState.Pending)
    private fun file(id: String, path: String? = null) =
        ComposerAttachment(id, id, "content://docs/$id", image = false, path = path, size = 2048, uploadState = if (path != null) DraftUploadState.Uploaded else DraftUploadState.Pending)

    @Test fun aGenericUploadFailureRowSaysOnlyWhy() {
        val errors = listOf(
            HubFailure(HubError.Unreachable),
            HubFailure(HubError.Server, "No space left on the computer", status = 507, code = "insufficient_storage"),
            IOException("The computer refused it"),
        )
        for (error in errors) {
            val row = uploadFailureRow("build.log", error).resolve(resources)
            assertEquals(failureReasonText(error).resolve(resources), row)
            assertFalse(row.contains("build.log"))
            assertFalse(row.contains("Couldn't send"))
        }
        assertEquals("No space left on the computer", uploadFailureRow("build.log", errors[1]).resolve(resources))
    }

    @Test fun sizeAndVersionRefusalRowsKeepTheWholeReason() {
        val refusals = listOf(
            CoreText.of(CoreText.Key.UploadTooLarge),
            CoreText.of(CoreText.Key.ComposerFileTooLarge, 200),
            CoreText.of(CoreText.Key.UploadNodeOutdated),
            CoreText.of(CoreText.Key.UploadHubUnsupported),
        )
        for (refusal in refusals) {
            val error = ComposerOperationException(refusal)
            assertEquals(uploadFailure("build.log", error), uploadFailureRow("build.log", error))
            assertEquals(uploadFailure("build.log", error).resolve(resources), uploadFailureRow("build.log", error).resolve(resources))
        }
    }

    @Test fun anUnreachableHubRowUsesTheScreensLanguage() {
        val error = HubFailure(HubError.Unreachable)
        val expected = mapOf("en" to "Can't reach the hub", "zh-Hans" to "连不上 hub", "ja" to "ハブに接続できません")
        for ((language, words) in expected) {
            val localized = localizedResources(language)
            assertEquals(words, failureReasonText(error).resolve(localized))
            assertEquals(words, uploadFailureRow("build.log", error).resolve(localized))
        }
    }

    @Test fun theProductionUploadFactoryWiresTheRowAndComposerText() {
        val context = RuntimeEnvironment.getApplication()
        val uploads = newAgentUploads(context, Source(Composer()).apply { online = false })
        val error = HubFailure(HubError.Server, "No space left on the computer", status = 507, code = "insufficient_storage")
        val row = uploads.errorText("build.log", error)
        assertEquals(failureReasonText(error).resolve(context.resources), row)
        assertFalse(row.contains("build.log"))

        val oversize = ComposerOperationException(CoreText.of(CoreText.Key.UploadTooLarge))
        assertEquals(uploadFailure("build.log", oversize).resolve(context.resources), uploads.errorText("build.log", oversize))
        assertEquals(context.getString(persona.feature.chat.R.string.chat_composer_files_only), uploads.filesOnly())
        assertEquals(context.getString(persona.feature.chat.R.string.chat_composer_photos_only), uploads.photosOnly())
    }

    @Test fun theFirstMessageIsWrittenAsTheComposerWritesItsMessages() {
        val photosOnly = "Please look at the photos"
        val filesOnly = "Please look at the attached files"
        val cases = listOf(
            "" to listOf(photo("a", "/tmp/a.jpg")),
            "看下这个报错" to listOf(photo("a", "/tmp/a.jpg"), photo("b", "/tmp/b c.jpg")),
            "" to listOf(file("notes.txt", "/tmp/notes.txt"), photo("a", "/tmp/a.jpg")),
            "整理日志" to listOf(file("my log.txt", "/tmp/my log.txt"), file("it's.md", "/tmp/it's.md")),
            "" to listOf(file("notes.txt", "/tmp/notes.txt")),
            "看下这个文件" to listOf(file("notes.txt", "/tmp/notes.txt")),
            "只有字" to emptyList(),
        )
        for ((typed, attachments) in cases) {
            val composer = ChatComposerState(ChatComposerDraft(typed, attachments)).apply {
                photosOnlyText = photosOnly; filesOnlyText = filesOnly
            }
            assertEquals(composer.message(), NewAgentAttachments.firstMessage(typed, attachments, photosOnly, filesOnly))
        }
        // Videos too: the shared writer gives the composer's tags (New Agent itself picks only pictures, as iOS).
        val video = ComposerAttachment("v", "clip.mp4", "content://media/v", image = false, path = "/tmp/clip.mp4", size = 4096,
            uploadState = DraftUploadState.Uploaded, video = true, duration = 2.5)
        val composer = ChatComposerState(ChatComposerDraft("", listOf(video, photo("a", "/tmp/a.jpg")))).apply {
            photosOnlyText = photosOnly; videosOnlyText = "Please look at the video"
        }
        assertEquals(composer.message(), ComposerMessage.text("", listOf("/tmp/a.jpg"), emptyList(),
            listOf(AttachedFile("video", "/tmp/clip.mp4", "clip.mp4", 4096, 2.5)), photosOnly, "Please look at the video"))
    }

    @Test fun uploadsGoThroughTheChatServiceOfTheMachinesHubGroupAndThePathsGoWithTheFirstMessage() = test {
        val composer = Composer()
        val source = Source(composer)
        // A computer of a second hub group: the merged id is what the form knows; bindingsFor finds the group's services.
        assertSame(composer, source.composerFor("mac~g0a1b2c3d"))
        assertEquals(SessionKey("mac~g0a1b2c3d", ""), source.asked.last())

        val sender = Sender()
        val service = Service().apply { result = AgentStartResult.Created(Session(machineId = "first", sessionId = "new", status = "idle", starting = true)) }
        val discarded = mutableListOf<String>()
        val model = NewAgentViewModel(service, MemoryStore(), machines, sender, uploads = uploads(source, discarded))
        try {
            advanceUntilIdle()
            assertTrue(model.attachable)
            assertTrue(model.canUpload("first"))
            model.attach(listOf(photo("p1"), file("notes.txt")))
            assertEquals(R.string.agents_attach_uploading_hold, model.state.value.attachmentsHold?.id)
            advanceUntilIdle()
            assertEquals(listOf(SessionKey("first", "") to "p1.heic", SessionKey("first", "") to "notes.txt"), composer.uploads)
            assertTrue(source.asked.contains(SessionKey("first", "")))
            assertNull(model.state.value.attachmentsHold)
            model.prompt("  看下这个  ")
            model.submit()
            advanceUntilIdle()
            assertEquals("看下这个", service.request?.nameHint)
            assertNull(service.request?.prompt)
            assertEquals(listOf("  看下这个  \n\n<persona-file kind=\"file\" path=\"/up/notes.txt\" name=\"notes.txt\" size=\"2048\"/>\n\n" +
                "<attached_images>\n/up/p1.heic\n</attached_images>"), sender.sent.map { it.second })
            model.opened()
            advanceUntilIdle()
            assertTrue(model.state.value.attachments.isEmpty())
            assertEquals(listOf("p1", "notes.txt"), discarded)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun photosAloneSayTheComposersWordsAndTheTabIsNamedByTheHub() = test {
        val sender = Sender()
        val service = Service().apply { result = AgentStartResult.Created(Session(machineId = "first", sessionId = "new")) }
        val model = NewAgentViewModel(service, MemoryStore(), machines, sender, uploads = uploads(Source(Composer())))
        try {
            advanceUntilIdle()
            model.attach(listOf(photo("p1")))
            advanceUntilIdle()
            model.submit()
            advanceUntilIdle()
            assertNull(service.request?.nameHint)
            assertEquals("Please look at the photos\n\n<attached_images>\n/up/p1.heic\n</attached_images>", sender.sent.single().second)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun aCodexLauncherOffersNoAttachmentsAndHoldsTheStartWhileThereAreSome() = test {
        val service = Service().apply { available = listOf(AgentLauncher("claude"), AgentLauncher("codex", agentType = "codex")) }
        val model = NewAgentViewModel(service, MemoryStore(), machines, Sender(), uploads = uploads(Source(Composer())))
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(launcher = "codex"))
            assertFalse(model.state.value.takesAttachments)
            // No menu, so nothing comes in.
            model.attach(listOf(file("a.txt")))
            assertTrue(model.state.value.attachments.isEmpty())

            model.selection(model.state.value.selection.copy(launcher = "claude"))
            model.attach(listOf(file("a.txt")))
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(launcher = "codex"))
            val hold = requireNotNull(model.state.value.attachmentsHold)
            assertEquals(R.string.agents_attach_launcher_hold, hold.id)
            assertEquals(listOf("Codex"), hold.args)
            model.prompt("修一下登录")
            model.submit()
            advanceUntilIdle()
            assertEquals(0, service.starts)
            // Taking it out lets Codex start, its first message with the start as before.
            model.removeAttachment("a.txt")
            model.submit()
            advanceUntilIdle()
            assertEquals("修一下登录", service.request?.prompt)
        } finally { model.viewModelScope.cancel() }
    }

    /**
     * #8143: Paseo's providers other than Claude Code and Codex ("paseo") take attachments as iOS and the web send them:
     * the start carries no first message, which goes as an ordinary message once the hub answers, with the files' paths
     * and the photos. Without attachments their first message goes the same way.
     */
    @Test fun aPaseoProviderTakesAttachmentsInTheFirstMessageAfterTheStart() = test {
        val sender = Sender()
        val service = Service().apply {
            available = listOf(AgentLauncher("claude"), AgentLauncher("grok", agentType = "paseo"), AgentLauncher("codex", agentType = "codex"))
            result = AgentStartResult.Created(Session(machineId = "first", sessionId = "paseo-1", agent = "paseo", launcher = "grok", status = "idle"))
        }
        val model = NewAgentViewModel(service, MemoryStore(), machines, sender, uploads = uploads(Source(Composer())))
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(launcher = "grok", cwd = "/code"))
            assertTrue(model.state.value.takesAttachments)
            model.attach(listOf(photo("p1"), file("notes.txt")))
            advanceUntilIdle()
            assertNull(model.state.value.attachmentsHold)
            model.prompt("整理这些照片")
            model.submit()
            advanceUntilIdle()
            val request = requireNotNull(service.request)
            assertEquals("grok", request.launcher)
            assertEquals("paseo", request.agentType)
            assertEquals(true, request.async)
            assertNull(request.prompt)
            assertEquals("整理这些照片", request.nameHint)
            assertEquals(listOf(SessionKey("first", "paseo-1") to "整理这些照片\n\n<persona-file kind=\"file\" path=\"/up/notes.txt\" " +
                "name=\"notes.txt\" size=\"2048\"/>\n\n<attached_images>\n/up/p1.heic\n</attached_images>"),
                sender.sent.map { it.first to it.second })
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun aPaseoProvidersFirstMessageGoesAfterTheStartWithoutAttachmentsToo() = test {
        val sender = Sender()
        val service = Service().apply {
            available = listOf(AgentLauncher("grok", agentType = "paseo"))
            result = AgentStartResult.Created(Session(machineId = "first", sessionId = "paseo-2", agent = "paseo", launcher = "grok", status = "idle"))
        }
        val model = NewAgentViewModel(service, MemoryStore(), machines, sender, uploads = uploads(Source(Composer())))
        try {
            advanceUntilIdle()
            model.selection(model.state.value.selection.copy(launcher = "grok", cwd = "/code"))
            model.prompt("看一下构建")
            model.submit()
            advanceUntilIdle()
            assertNull(service.request?.prompt)
            assertEquals(listOf("看一下构建"), sender.sent.map { it.second })
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun aFailedFileStaysWithItsReasonAndRetriesWithoutLosingTheWords() = test {
        val composer = Composer().apply { failures = 1; failure = HubFailure(HubError.Server, "The computer refused it", status = 507) }
        val service = Service()
        val discarded = mutableListOf<String>()
        val source = Source(composer)
        val model = NewAgentViewModel(service, MemoryStore(), machines, Sender(), uploads = uploads(source, discarded))
        val notices = mutableListOf<AgentText>()
        val collecting = launch { model.notices.collect { notices += it } }
        try {
            advanceUntilIdle()
            model.prompt("看日志")
            model.attach(listOf(file("build.log")))
            advanceUntilIdle()
            val failed = model.state.value.attachments.single()
            val error = HubFailure(HubError.Server, "The computer refused it", status = 507)
            val reason = failureReasonText(error).resolve(resources)
            assertEquals("build.log", failed.id)
            assertEquals(DraftUploadState.Failed, failed.uploadState)
            assertEquals(reason, failed.error)
            assertFalse(failed.error!!.contains("build.log"))
            assertFalse(failed.error!!.contains("Couldn't send"))
            assertTrue(discarded.isEmpty())
            val said = notices.single()
            assertEquals(R.string.agents_attach_upload_failed, said.id)
            assertEquals("build.log", said.args.first())
            assertEquals(uploadFailure("build.log", error).resolve(resources), said.resolve(resources))
            assertEquals("Couldn't send “build.log”: The computer refused it", said.resolve(resources))
            assertEquals(R.string.agents_attach_uploading_hold, model.state.value.attachmentsHold?.id)
            assertEquals("看日志", model.state.value.prompt)

            // Retry while no hub service is available leaves the failed row and its local content untouched.
            source.online = false
            model.retryAttachment("build.log")
            advanceUntilIdle()
            assertEquals(DraftUploadState.Failed, model.state.value.attachments.single().uploadState)
            assertNotNull(model.state.value.attachments.single().error)
            assertEquals(1, composer.uploads.count { it.second == "build.log" })
            assertTrue(discarded.isEmpty())
            assertEquals(2, notices.size)
            assertEquals(R.string.agents_attach_upload_failed, notices.last().id)

            source.online = true
            model.retryAttachment("build.log")
            advanceUntilIdle()
            assertEquals("/up/build.log", model.state.value.attachments.single().path)
            assertNull(model.state.value.attachments.single().error)
            assertNull(model.state.value.attachmentsHold)
            assertEquals(2, composer.uploads.count { it.second == "build.log" })

            // Over the hub's limit: the same retained-row behavior and iOS's own toast wording.
            composer.refusal = ComposerOperationException(persona.core.contracts.CoreText.of(persona.core.contracts.CoreText.Key.UploadTooLarge))
            model.attach(listOf(file("huge.iso")))
            advanceUntilIdle()
            assertEquals(AgentText(persona.feature.chat.R.string.chat_attach_too_large, "huge.iso"), notices.last())
            assertEquals(DraftUploadState.Failed, model.state.value.attachments.single { it.id == "huge.iso" }.uploadState)
            composer.refusal = null
            model.removeAttachment("huge.iso")

            // Removing one still uploading stops it.
            composer.gate = CompletableDeferred()
            model.attach(listOf(file("big.zip")))
            runCurrent()
            assertEquals(DraftUploadState.Uploading, model.state.value.attachments.single { it.id == "big.zip" }.uploadState)
            model.removeAttachment("big.zip")
            composer.gate?.complete(Unit)
            advanceUntilIdle()
            assertTrue(model.state.value.attachments.none { it.id == "big.zip" })
            assertEquals(3, notices.size)
        } finally { collecting.cancel(); model.viewModelScope.cancel() }
    }

    @Test fun closingTheFormOrClearingItsModelTakesTheAttachmentsAndStopsUploads() = test {
        val composer = Composer().apply { gate = CompletableDeferred() }
        val discarded = mutableListOf<String>()
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(), uploads = uploads(Source(composer), discarded))
        advanceUntilIdle()
        model.prompt("草稿")
        model.attach(listOf(photo("p1"), file("notes.txt")))
        runCurrent()
        model.closeAttachments()
        composer.gate?.complete(Unit)
        advanceUntilIdle()
        assertTrue(model.state.value.attachments.isEmpty())
        assertEquals(listOf("p1", "notes.txt"), discarded)
        // The first message's words are not this change's: they stay as before.
        assertEquals("草稿", model.state.value.prompt)

        // The view model going (onCleared) takes what is left too.
        composer.gate = CompletableDeferred()
        model.attach(listOf(photo("p2")))
        runCurrent()
        val store = androidx.lifecycle.ViewModelStore()
        androidx.lifecycle.ViewModelProvider(store, object : androidx.lifecycle.ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : androidx.lifecycle.ViewModel> create(modelClass: Class<T>): T = model as T
        })[NewAgentViewModel::class.java]
        store.clear()
        assertEquals(listOf("p1", "notes.txt", "p2"), discarded)
        assertTrue(model.state.value.attachments.isEmpty())
    }

    @Test fun switchingToCodexWhileAFileUploadsHoldsTheStart() = test {
        val composer = Composer().apply { gate = CompletableDeferred() }
        val service = Service().apply { available = listOf(AgentLauncher("claude"), AgentLauncher("codex", agentType = "codex")) }
        val model = NewAgentViewModel(service, MemoryStore(), machines, Sender(), uploads = uploads(Source(composer)))
        try {
            advanceUntilIdle()
            model.attach(listOf(file("a.txt")))
            runCurrent()
            model.selection(model.state.value.selection.copy(launcher = "codex"))
            assertEquals(R.string.agents_attach_launcher_hold, model.state.value.attachmentsHold?.id)
            composer.gate?.complete(Unit)
            advanceUntilIdle()
            // On the computer now, but Codex still takes none: the start stays held.
            assertEquals("/up/a.txt", model.state.value.attachments.single().path)
            assertEquals(R.string.agents_attach_launcher_hold, model.state.value.attachmentsHold?.id)
            model.prompt("修一下")
            model.submit()
            advanceUntilIdle()
            assertEquals(0, service.starts)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun anUploadToTheComputerPickedBeforeIsDroppedWhenItEnds() = test {
        val composer = Composer().apply { gate = CompletableDeferred(); gateMachine = "first" }
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(), uploads = uploads(Source(composer)))
        try {
            advanceUntilIdle()
            model.attach(listOf(photo("p1"), file("notes.txt")))
            runCurrent()
            model.selectMachine("second")
            advanceUntilIdle()
            assertEquals("/up/second/p1.heic", model.state.value.attachments.single().path)
            // The first computer's answers come late: nothing of theirs comes back.
            composer.gate?.complete(Unit)
            advanceUntilIdle()
            val state = model.state.value
            assertEquals(listOf("p1"), state.attachments.map { it.id })
            assertEquals("/up/second/p1.heic", state.attachments.single().path)
            assertEquals("second", state.attachedOn)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun anotherComputerTakesTheFilesOutWithANoteAndThePhotosGoUpAgain() = test {
        val composer = Composer()
        val discarded = mutableListOf<String>()
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(), uploads = uploads(Source(composer), discarded))
        try {
            advanceUntilIdle()
            assertEquals("first", model.state.value.machineId)
            model.attach(listOf(photo("p1"), file("notes.txt")))
            advanceUntilIdle()
            model.selectMachine("second")
            advanceUntilIdle()
            val state = model.state.value
            assertEquals(listOf("p1"), state.attachments.map { it.id })
            assertEquals("second", state.attachedOn)
            assertEquals(AgentText(R.string.agents_attach_moved, "MacBook"), state.attachNote)
            assertEquals(listOf("notes.txt"), discarded)
            assertEquals(SessionKey("second", "") to "p1.heic", composer.uploads.last())
            assertEquals("/up/second/p1.heic", state.attachments.single().path)
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun theEleventhFileIsRefusedAndUploadsRunThreeAtATime() = test {
        val composer = Composer().apply { gate = CompletableDeferred() }
        val discarded = mutableListOf<String>()
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(), uploads = uploads(Source(composer), discarded))
        val notices = mutableListOf<AgentText>()
        val collecting = launch { model.notices.collect { notices += it } }
        try {
            advanceUntilIdle()
            model.attach(List(11) { file("f$it.txt") })
            runCurrent()
            assertEquals(10, model.state.value.attachments.size)
            assertTrue(model.state.value.attachments.none { it.id == "f10.txt" })
            assertEquals(listOf("f10.txt"), discarded)
            assertEquals(AgentText(persona.feature.chat.R.string.chat_composer_file_limit), notices.single())
            assertEquals(3, composer.uploads.size)
            assertEquals(3, composer.active)
            assertEquals(3, composer.maxActive)
            assertEquals(listOf(DraftUploadState.Uploading, DraftUploadState.Uploading, DraftUploadState.Uploading,
                DraftUploadState.Pending, DraftUploadState.Pending, DraftUploadState.Pending, DraftUploadState.Pending,
                DraftUploadState.Pending, DraftUploadState.Pending, DraftUploadState.Pending),
                model.state.value.attachments.map { it.uploadState })
            assertEquals(R.string.agents_attach_uploading_hold, model.state.value.attachmentsHold?.id)

            composer.gate?.complete(Unit)
            advanceUntilIdle()
            assertEquals(10, composer.uploads.size)
            assertEquals(3, composer.maxActive)
            assertTrue(model.state.value.attachments.all { it.path != null })
            assertNull(model.state.value.attachmentsHold)
        } finally { collecting.cancel(); model.viewModelScope.cancel() }
    }

    @Test fun stagedBytesReplaceAnUnknownProviderSizeBeforeTheFirstMessage() = test {
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(),
            uploads = uploads(Source(Composer())))
        try {
            advanceUntilIdle()
            model.attach(listOf(file("notes.txt").copy(size = null)))
            advanceUntilIdle()
            val attachment = model.state.value.attachments.single()
            assertEquals(2048L, attachment.size)
            val message = NewAgentAttachments.firstMessage("Read", model.state.value.attachments, "unused", "unused")
            assertTrue(message.contains("name=\"notes.txt\" size=\"2048\""))
        } finally { model.viewModelScope.cancel() }
    }

    @Test fun theModelRefusesAFifthPhotoEvenWhenPickerResultsOverlap() = test {
        val discarded = mutableListOf<String>()
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(),
            uploads = uploads(Source(Composer()), discarded))
        val notices = mutableListOf<AgentText>()
        val collecting = launch { model.notices.collect { notices += it } }
        try {
            advanceUntilIdle()
            model.attach(List(3) { photo("p$it") })
            advanceUntilIdle()
            model.attach(List(2) { photo("p${it + 3}") })
            advanceUntilIdle()
            assertEquals(4, model.state.value.attachments.count { it.media })
            assertEquals(listOf("p4"), discarded)
            assertEquals(AgentText(R.string.agents_attach_photo_limit), notices.single())
        } finally { collecting.cancel(); model.viewModelScope.cancel() }
    }

    @Test fun withoutHubServicesNoAttachmentsAreOffered() = test {
        val model = NewAgentViewModel(Service(), MemoryStore(), machines, Sender())
        val offline = NewAgentViewModel(Service(), MemoryStore(), machines, Sender(),
            uploads = NewAgentUploads({ null }, { _, _, _, _ -> "" }, errorText = { _, error -> error.message.orEmpty() }))
        try {
            advanceUntilIdle()
            assertFalse(model.attachable)
            assertTrue(offline.attachable)
            assertFalse(offline.canUpload("first"))
        } finally { model.viewModelScope.cancel(); offline.viewModelScope.cancel() }
    }

    private fun uploads(source: ChatWiringSource, discarded: MutableList<String> = mutableListOf()) = NewAgentUploads(
        service = { source.composerFor(it) },
        upload = { service, session, attachment, onStaged ->
            onStaged(attachment.size ?: 2048)
            service.upload(session, attachment.name, File(attachment.name))
        },
        photosOnly = { "Please look at the photos" },
        filesOnly = { "Please look at the attached files" },
        errorText = { name, error -> uploadFailureRow(name, error).resolve(resources) },
        discard = { discarded += it.id },
    )

    private class Composer : ChatComposerService {
        override val deliveries = MutableStateFlow<Map<String, ComposerDelivery>>(emptyMap())
        val uploads = mutableListOf<Pair<SessionKey, String>>()
        var failures = 0
        var failure: Exception = IOException("Upload failed")
        var gate: CompletableDeferred<Unit>? = null
        /** Only uploads to this computer wait for [gate]; null: all of them. */
        var gateMachine: String? = null
        var refusal: Exception? = null
        var active = 0
        var maxActive = 0
        override suspend fun upload(session: SessionKey, name: String, file: File): String {
            uploads += session to name
            active++
            maxActive = maxOf(maxActive, active)
            try {
                if (gateMachine == null || gateMachine == session.machineId) gate?.await()
                refusal?.let { throw it }
                if (failures > 0) { failures--; throw failure }
                return if (session.machineId == "first") "/up/$name" else "/up/${session.machineId}/$name"
            } finally { active-- }
        }
        override suspend fun interrupt(session: SessionKey) = Unit
        override suspend fun retryRefused(clientMessageId: String, clearDraft: Boolean) = error("unused")
    }

    /** The app's chat wiring: every machine's group has [composer]; which sessions were asked for is kept. */
    private class Source(private val composer: ChatComposerService) : ChatWiringSource {
        val asked = mutableListOf<SessionKey>()
        var online = true
        override val current = MutableStateFlow<ChatAppBindings?>(null)
        override fun bindingsFor(session: SessionKey): StateFlow<ChatAppBindings?> {
            asked += session
            val bindings = if (online) ChatAppBindings(HubScope("hub", "generation"), CoroutineScope(Dispatchers.Unconfined),
                unused(), unused(), unused(), composer, unused(), unused(), unused()) else null
            return MutableStateFlow(bindings)
        }
    }

    private class MemoryStore : AgentCreationStore {
        var operation: AgentStartOperation? = null
        override suspend fun lastMachine(scope: String) = "first"
        override suspend fun defaults(scope: String, machine: String) = AgentDefaults()
        override suspend fun saveDefaults(scope: String, machine: String, defaults: AgentDefaults) = Unit
        override suspend fun pending(scope: String) = operation
        override suspend fun pending(scope: String, value: AgentStartOperation?) { operation = value }
    }

    private class Sender : MessageSender {
        override val outbox = MutableStateFlow(emptyList<OutboxMessage>())
        val sent = mutableListOf<Triple<SessionKey, String, String>>()
        override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) {
            sent += Triple(session, text, clientMessageId)
        }
    }

    private class Service : AgentCreationService {
        override val scope = "hub/pairing"
        var starts = 0
        var request: AgentStartRequest? = null
        var result: AgentStartResult = AgentStartResult.Created(Session(machineId = "first", sessionId = "created"))
        var available = listOf(AgentLauncher("claude"))
        override suspend fun launchers(machineId: String) = available
        override suspend fun directories(machineId: String, path: String) = AgentDirectories(path.ifEmpty { "/code" })
        override suspend fun start(request: AgentStartRequest, operationId: String): AgentStartResult {
            starts++
            this.request = request
            return result.let { if (it is AgentStartResult.Created) it.copy(session = it.session.copy(machineId = request.machineId)) else it }
        }
        override suspend fun operation(operationId: String): AgentStartResult = result
        override suspend fun newAgentModel(): ModelPurposeDetail? = null
    }
}

private inline fun <reified T> unused(): T = Proxy.newProxyInstance(T::class.java.classLoader, arrayOf(T::class.java)) { proxy, method, args ->
    when (method.name) {
        "toString" -> "Unused boundary"
        "hashCode" -> System.identityHashCode(proxy)
        "equals" -> proxy === args?.firstOrNull()
        else -> error("Unexpected boundary access")
    }
} as T
