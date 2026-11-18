package persona.feature.chat

import android.content.Context
import android.graphics.Bitmap
import androidx.test.core.app.ApplicationProvider
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.runtime.mutableStateOf
import coil3.ImageLoader
import coil3.asImage
import coil3.memory.MemoryCache
import coil3.request.Options
import kotlinx.coroutines.flow.MutableStateFlow
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNotSame
import org.junit.Assert.assertNull
import org.junit.Assert.assertSame
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import persona.core.contracts.Interaction
import persona.core.contracts.Item
import persona.core.contracts.Machine
import persona.core.contracts.Session
import persona.core.contracts.SessionKey
import persona.core.contracts.api.ChatRepository
import persona.core.contracts.api.ChatSnapshot
import persona.core.contracts.api.InteractionCenter
import persona.core.contracts.api.MessageSender
import persona.core.contracts.api.OutboxMessage
import persona.core.contracts.api.SessionsRepository
import java.io.File

@RunWith(AndroidJUnit4::class)
@Config(sdk = [35], qualifiers = "en-w400dp-h900dp")
class ChatImageLoadersTest {
    @get:Rule val compose = createComposeRule()
    private val context = ApplicationProvider.getApplicationContext<Context>()

    @Test
    fun revisitsReuseBoundedCachesAndPairingsStayIsolated() {
        val loaders = ChatImageLoaders()
        val firstGeneration = Any()
        val secondGeneration = Any()
        val first = loaders.get("first", firstGeneration) { loader() }
        val second = loaders.get("second", secondGeneration) { loader() }

        assertSame(first, loaders.get("first", firstGeneration) { error("must reuse") })
        assertNotSame(first, second)
        assertEquals(CHAT_IMAGE_MEMORY_BYTES, first.memoryCache?.maxSize)
        assertEquals(CHAT_IMAGE_MEMORY_BYTES, second.memoryCache?.maxSize)

        val key = MemoryCache.Key("same-image")
        first.memoryCache?.set(key, MemoryCache.Value(Bitmap.createBitmap(8, 8, Bitmap.Config.ARGB_8888).asImage()))
        assertNull(second.memoryCache?.get(key))

        loaders.retain(mapOf("second" to secondGeneration))
        assertEquals(0L, first.memoryCache?.size)
        assertSame(second, loaders.get("second", secondGeneration) { error("must retain") })

        val rebuilt = loaders.get("second", Any()) { loader() }
        assertNotSame(second, rebuilt)
        assertEquals(0L, second.memoryCache?.size)
        rebuilt.shutdown()
    }

    @Test
    fun chatPageDoesNotShutdownPairingLoader() {
        val key = SessionKey("machine", "session")
        val pairingLoader = loader()
        val cacheKey = MemoryCache.Key("decoded")
        pairingLoader.memoryCache?.set(cacheKey,
            MemoryCache.Value(Bitmap.createBitmap(8, 8, Bitmap.Config.ARGB_8888).asImage()))
        var requested = false
        val chats = object : ChatRepository, ChatLocalStorage {
            override val localStorage = MutableStateFlow<ChatStorageSnapshot?>(null)
            override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot(loading = false))
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
            override suspend fun imageFile(session: SessionKey, item: Item, width: Int, height: Int): File = error("unused")
            override fun imageLoader(session: SessionKey): ImageLoader {
                requested = true
                return pairingLoader
            }
        }
        val sender = object : MessageSender {
            override val outbox = MutableStateFlow<List<OutboxMessage>>(emptyList())
            override suspend fun send(session: SessionKey, text: String, clientMessageId: String, clearDraft: String?) = Unit
        }
        val interactions = object : InteractionCenter {
            override val pending = MutableStateFlow<List<Interaction>>(emptyList())
            override suspend fun refresh() = Unit
            override suspend fun resolve(id: String, resolution: persona.core.contracts.Resolution) = Unit
        }
        val sessions = object : SessionsRepository {
            override val machines = MutableStateFlow<List<Machine>>(emptyList())
            override val sessions = MutableStateFlow<List<Session>>(emptyList())
            override suspend fun refresh() = Unit
        }

        val show = mutableStateOf(true)
        compose.setContent { if (show.value) ChatScreen(key, chats, sender, interactions, sessions, onBack = {}) }
        compose.waitForIdle()
        assertTrue(requested)
        compose.runOnIdle { show.value = false }
        compose.waitForIdle()

        assertNotNull(pairingLoader.memoryCache?.get(cacheKey))
        pairingLoader.shutdown()
    }

    @Test
    fun imageKeysUseTheirOwnPairingScope() {
        val repository = object : ChatRepository, ChatLocalStorage {
            override val localStorage = MutableStateFlow<ChatStorageSnapshot?>(null)
            override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot())
            override suspend fun loadOlder(session: SessionKey) = Unit
            override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
            override suspend fun imageFile(session: SessionKey, item: Item, width: Int, height: Int): File = error("unused")
        }
        val loader = chatImageLoader(context, repository, "second-pairing")
        val image = ChatImage(SessionKey("machine", "session"), Item("image", kind = "image"))

        assertEquals("second-pairing/machine/session/image", loader.components.key(image, Options(context)))
        loader.shutdown()
    }

    private fun loader() = chatImageLoader(context, object : ChatRepository {
        override fun observe(session: SessionKey) = MutableStateFlow(ChatSnapshot())
        override suspend fun loadOlder(session: SessionKey) = Unit
        override suspend fun expandFold(session: SessionKey, fromSeq: Int, toSeq: Int) = emptyList<Item>()
    })
}
