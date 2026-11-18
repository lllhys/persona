package persona.feature.chat

import android.content.Context
import androidx.core.content.FileProvider
import org.robolectric.Robolectric

/**
 * The chat's FileProvider for a test that hands out files from `cache/chat-captures/`. FileProvider keeps the first cache
 * directory it saw for an authority, in a static map the Robolectric sandbox shares between tests that each have their own
 * cache directory: it is emptied here, and the provider is set up to open what it hands out (a photo taken back from the queue).
 */
internal fun useChatFileProvider(context: Context) {
    runCatching {
        val cache = FileProvider::class.java.getDeclaredField("sCache").apply { isAccessible = true }.get(null) as MutableMap<*, *>
        cache.clear()
    }
    runCatching { Robolectric.setupContentProvider(FileProvider::class.java, "${context.packageName}.chat.files") }
}
