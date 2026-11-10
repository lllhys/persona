package persona.core.data

import java.io.File
import java.io.FileOutputStream
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.security.MessageDigest
import persona.core.contracts.JsonCodec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import kotlinx.serialization.encodeToString
import kotlinx.serialization.ExperimentalSerializationApi
import kotlinx.serialization.json.DecodeSequenceMode
import kotlinx.serialization.json.decodeToSequence

/** Atomic, pairing-scoped snapshots. Failed reads never delete or replace the original file. */
class ListSnapshotStore(private val directory: File) {
    private val lock = Mutex()
    suspend fun read(key: String, maxBytes: Long = Long.MAX_VALUE): String? = withContext(Dispatchers.IO) { lock.withLock {
        val file = File(directory, snapshotHash(key))
        if (file.exists() && file.length() <= maxBytes) file.readText() else null
    } }
    /** Decode only the first window, including when an older app left a full history snapshot. */
    @OptIn(ExperimentalSerializationApi::class)
    internal suspend fun sessionWindow(limit: Int): List<persona.core.contracts.Session>? = withContext(Dispatchers.IO) {
        lock.withLock {
            val file = File(directory, snapshotHash("sessions"))
            if (!file.exists()) null else file.inputStream().use { input ->
                JsonCodec.decodeToSequence<persona.core.contracts.Session>(input, DecodeSequenceMode.ARRAY_WRAPPED)
                    .take(limit).toList()
            }
        }
    }

    suspend fun write(key: String, value: String) = withContext(Dispatchers.IO) { lock.withLock {
        check(directory.isDirectory || directory.mkdirs())
        val temp = File.createTempFile("snapshot-", ".tmp", directory)
        try {
            FileOutputStream(temp).use { it.write(value.toByteArray()); it.fd.sync() }
            Files.move(temp.toPath(), File(directory, snapshotHash(key)).toPath(),
                StandardCopyOption.ATOMIC_MOVE, StandardCopyOption.REPLACE_EXISTING)
            Unit
        } finally { temp.delete() }
    } }
}

internal fun snapshotHash(value: String): String = MessageDigest.getInstance("SHA-256")
    .digest(value.toByteArray()).joinToString("") { "%02x".format(it) }

/** A file over [maxBytes] is skipped before it is read, and left as it is. */
internal suspend inline fun <reified T> ListSnapshotStore?.restore(key: String, maxBytes: Long = Long.MAX_VALUE): T? {
    var result: T? = null
    quietly { this?.read(key, maxBytes)?.let { result = JsonCodec.decodeFromString<T>(it) } }
    return result
}

internal suspend inline fun <reified T> ListSnapshotStore?.save(key: String, value: T) {
    quietly { this?.write(key, JsonCodec.encodeToString(value)) }
}

/** The pairing's to-dos as last read from its hub, for TasksStore's first paint (#4533). */
fun ListSnapshotStore.todoCache(): persona.core.domain.tasks.TodoCache = object : persona.core.domain.tasks.TodoCache {
    override suspend fun read(): List<persona.core.contracts.Todo>? = restore<List<persona.core.contracts.Todo>>("todos")
    override suspend fun write(todos: List<persona.core.contracts.Todo>) = save("todos", todos)
}
