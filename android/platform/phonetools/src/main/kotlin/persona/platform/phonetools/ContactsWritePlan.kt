package persona.platform.phonetools

import android.content.ContentProviderOperation
import android.content.ContentProviderResult
import android.content.ContentResolver
import android.content.res.Resources
import android.net.Uri
import android.provider.ContactsContract
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.Locale

/** One provider operation, kept as data so tests can inspect values and back references without a fake OS provider. */
internal data class ProviderWrite(
    val kind: Kind,
    val uri: Uri,
    val values: Map<String, Any?> = emptyMap(),
    val id: Long? = null,
    val backReferences: Map<String, Int> = emptyMap(),
) {
    enum class Kind { Insert, Update, Delete }
}

/**
 * Runs the re-read and comparison that precede a batch. Any failure here happened before a single write was attempted,
 * so it is a definite refusal, never the uncertain outcome that a failure inside the batch may be.
 */
internal inline fun <T> beforeWrite(block: () -> T): T = try {
    block()
} catch (refused: PhoneToolWriteRefusal) {
    throw refused
} catch (cancelled: kotlinx.coroutines.CancellationException) {
    throw cancelled
} catch (failure: Exception) {
    throw PhoneToolWriteRefusal("${failure.message.orEmpty()}\n\n没有写入任何内容 / Nothing was written.")  // i18n-ignore: tool result for the model
}

internal fun ContentResolver.applyProviderWrites(authority: String, writes: List<ProviderWrite>): Array<ContentProviderResult> {
    val operations = ArrayList<ContentProviderOperation>(writes.size)
    for (write in writes) {
        val builder = when (write.kind) {
            ProviderWrite.Kind.Insert -> ContentProviderOperation.newInsert(write.uri)
            ProviderWrite.Kind.Update -> ContentProviderOperation.newUpdate(write.uri)
            ProviderWrite.Kind.Delete -> ContentProviderOperation.newDelete(write.uri)
        }
        if (write.id != null) {
            builder.withSelection("${android.provider.BaseColumns._ID} = ?", arrayOf(write.id.toString()))
            builder.withExpectedCount(1)
        }
        for ((key, value) in write.values) {
            builder.withValue(key, value)
        }
        for ((key, index) in write.backReferences) builder.withValueBackReference(key, index)
        operations += builder.build()
    }
    val results = applyBatch(authority, operations)
    if (results.size != writes.size) error("The provider returned ${results.size} results for ${writes.size} operations")
    writes.forEachIndexed { index, write ->
        if (write.kind == ProviderWrite.Kind.Insert && results[index].uri == null) {
            error("The provider returned no id for insert $index")
        }
    }
    return results
}

internal data class LabeledEntry(val value: String, val label: String? = null) {
    val normalized: String get() = value.filter(Char::isDigit)

    fun providerValues(email: Boolean): Map<String, Any?> {
        val kind = label?.lowercase(Locale.ROOT)
        val type: Int
        var custom: String? = null
        if (email) {
            type = when (kind) {
                "home" -> ContactsContract.CommonDataKinds.Email.TYPE_HOME
                "work" -> ContactsContract.CommonDataKinds.Email.TYPE_WORK
                "iphone" -> ContactsContract.CommonDataKinds.Email.TYPE_CUSTOM
                else -> ContactsContract.CommonDataKinds.Email.TYPE_OTHER
            }
            if (kind == "iphone") {
                custom = "iPhone"
            }
        } else {
            type = when (kind) {
                "home" -> ContactsContract.CommonDataKinds.Phone.TYPE_HOME
                "work" -> ContactsContract.CommonDataKinds.Phone.TYPE_WORK
                "main" -> ContactsContract.CommonDataKinds.Phone.TYPE_MAIN
                "other" -> ContactsContract.CommonDataKinds.Phone.TYPE_OTHER
                "iphone" -> ContactsContract.CommonDataKinds.Phone.TYPE_CUSTOM
                else -> ContactsContract.CommonDataKinds.Phone.TYPE_MOBILE
            }
            if (kind == "iphone") custom = "iPhone"
        }
        return buildMap {
            put(ContactsContract.Data.MIMETYPE, if (email) ContactsContract.CommonDataKinds.Email.CONTENT_ITEM_TYPE
                else ContactsContract.CommonDataKinds.Phone.CONTENT_ITEM_TYPE)
            put(ContactsContract.Data.DATA1, value)
            put(ContactsContract.Data.DATA2, type)
            put(ContactsContract.Data.DATA3, custom)
        }
    }

    companion object {
        private val labels = setOf("mobile", "iphone", "home", "work", "main", "other")

        fun list(value: kotlinx.serialization.json.JsonElement?, email: Boolean): List<LabeledEntry>? {
            if (value == null || value is kotlinx.serialization.json.JsonNull) return null
            val items = value as? JsonArray ?: invalid(if (email) "emails must be a list" else "phones must be a list")
            return items.map { item ->
                val objectValue = item as? JsonObject
                val raw = when {
                    item is JsonPrimitive && item.isString -> item.content.trim()
                    objectValue != null -> objectValue.text("value").orEmpty()
                    else -> ""
                }
                if (email) {
                    if (raw.count { it == '@' } != 1 || raw.any(Char::isWhitespace) ||
                        raw.startsWith('@') || raw.endsWith('@')) invalid("\"$raw\" is not an email address")
                } else if (raw.count(Char::isDigit) < 3) {
                    invalid("\"$raw\" is not a phone number")
                }
                val label = objectValue?.text("label")?.lowercase(Locale.ROOT)
                if (label != null && label !in labels) invalid("unknown label \"$label\"")
                LabeledEntry(raw, label)
            }
        }
    }
}

internal data class ContactFields(
    val givenName: String = "",
    val familyName: String = "",
    val company: String = "",
    val jobTitle: String = "",
    val phones: List<String> = emptyList(),
    val emails: List<String> = emptyList(),
) {
    val name: String get() = listOf(givenName, familyName).filter(String::isNotEmpty)
        .joinToString(" ").ifEmpty { company }
}

internal data class ContactEntryRow(val id: Long, val entry: LabeledEntry)

/** Every value that an approved edit can replace; unrelated Data rows deliberately do not block a write. */
internal data class ContactSnapshot(
    val contactId: Long,
    val rawContactId: Long,
    val fields: ContactFields,
    val nameRowId: Long?,
    val organizationRowId: Long?,
    val phones: List<ContactEntryRow>,
    val emails: List<ContactEntryRow>,
)

internal data class ContactEdit(
    val fields: List<Field> = emptyList(),
    val phones: List<LabeledEntry> = emptyList(),
    val emails: List<LabeledEntry> = emptyList(),
    val replacePhones: Boolean = false,
    val replaceEmails: Boolean = false,
) {
    internal data class Field(val key: String, val before: String, val after: String)

    val isEmpty: Boolean get() = fields.isEmpty() && phones.isEmpty() && emails.isEmpty() && !replacePhones && !replaceEmails

    fun after(key: String): String? = fields.firstOrNull { it.key == key }?.after

    private fun fieldLines(res: Resources, creating: Boolean): List<String> = fields.map { field ->
        // A new contact has no earlier value to show.
        if (creating) res.getString(R.string.phonetools_contact_field_value, fieldName(res, field.key), field.after)
        else res.getString(R.string.phonetools_contact_field_change, fieldName(res, field.key),
            field.before.ifEmpty { res.getString(R.string.phonetools_not_set) }, field.after)
    }

    /** [current] is the contact before the edit: a replaced list is shown old → new so the overwritten values are visible. */
    fun lines(res: Resources, creating: Boolean = false, current: ContactFields? = null): List<String> = buildList {
        fun joined(values: List<String>) = values.joinToString(", ").ifEmpty { res.getString(R.string.phonetools_not_set) }
        addAll(fieldLines(res, creating))
        if (replacePhones) add(res.getString(R.string.phonetools_contact_field_change, res.getString(R.string.phonetools_contact_phones),
            joined(current?.phones.orEmpty()), joined(phones.map { it.value })))
        else if (phones.isNotEmpty()) add(res.getString(R.string.phonetools_contact_phones_add,
            phones.joinToString(", ") { it.value }))
        if (replaceEmails) add(res.getString(R.string.phonetools_contact_field_change, res.getString(R.string.phonetools_contact_emails),
            joined(current?.emails.orEmpty()), joined(emails.map { it.value })))
        else if (emails.isNotEmpty()) add(res.getString(R.string.phonetools_contact_emails_add,
            emails.joinToString(", ") { it.value }))
    }

    fun createSummary(res: Resources): String {
        val name = ContactFields(after("given_name").orEmpty(), after("family_name").orEmpty(), after("company").orEmpty()).name
        return (listOf(res.getString(R.string.phonetools_contact_new, name)) + lines(res, creating = true)).joinToString("\n")
    }

    fun updateSummary(res: Resources, current: ContactFields): String {
        val name = current.name.ifEmpty { res.getString(R.string.phonetools_untitled) }
        return (listOf(res.getString(R.string.phonetools_contact_change, name)) + lines(res, current = current)).joinToString("\n")
    }

    companion object {
        fun fieldName(res: Resources, key: String): String = res.getString(when (key) {
            "given_name" -> R.string.phonetools_contact_given_name
            "family_name" -> R.string.phonetools_contact_family_name
            "company" -> R.string.phonetools_contact_company
            else -> R.string.phonetools_contact_job_title
        })

        fun plan(current: ContactFields?, args: JsonObject): ContactEdit {
            val replace = args.bool("replace")
            val base = current ?: ContactFields()
            val scalars = listOf(
                "given_name" to base.givenName, "family_name" to base.familyName,
                "company" to base.company, "job_title" to base.jobTitle,
            )
            val fields = mutableListOf<Field>()
            for ((key, before) in scalars) {
                val after = args.text(key) ?: continue
                if (after == before) continue
                if (before.isNotEmpty() && !replace) {
                    invalid("the contact's $key is already \"$before\"; pass replace=true only if the user asked to change it")
                }
                fields += Field(key, before, after)
            }
            var edit = ContactEdit(fields = fields)
            LabeledEntry.list(args["phones"], email = false)?.let { incoming ->
                if (replace && current != null) {
                    edit = edit.copy(phones = incoming, replacePhones = true)
                } else {
                    val seen = base.phones.map { it.filter(Char::isDigit) }.toMutableSet()
                    edit = edit.copy(phones = incoming.filter { seen.add(it.normalized) })
                }
            }
            LabeledEntry.list(args["emails"], email = true)?.let { incoming ->
                if (replace && current != null) {
                    edit = edit.copy(emails = incoming, replaceEmails = true)
                } else {
                    val seen = base.emails.map { it.lowercase(Locale.ROOT) }.toMutableSet()
                    edit = edit.copy(emails = incoming.filter { seen.add(it.value.lowercase(Locale.ROOT)) })
                }
            }
            if (current == null) {
                if (edit.fields.none { it.key in setOf("given_name", "family_name", "company") }) {
                    invalid("a new contact needs given_name, family_name or company")
                }
            } else if (edit.isEmpty) {
                invalid("nothing to change: the contact already has these values")
            }
            return edit
        }
    }
}

internal data class ContactUpdatePreparation(
    val args: JsonObject,
    val snapshot: ContactSnapshot,
    val edit: ContactEdit,
) {
    val text: PhoneConfirmationText = { res -> edit.updateSummary(res, snapshot.fields) }
}
