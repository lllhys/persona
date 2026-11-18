package persona.core.domain

/**
 * An assistant conversation's title in the hub's format "MMDD | Type | Topic" (docs/assistant/assistant.md "Chat titles"),
 * split so a list can quiet the day and kind and let the topic be what the eye finds (iOS `ConversationTitle`): [head] is
 * "1101 | Fix | " and [topic] what follows it. [parse] gives null for any other title (the user's own, an older conversation's).
 */
class ConversationTitle private constructor(val head: String, val topic: String) {
    override fun equals(other: Any?): Boolean = other is ConversationTitle && other.head == head && other.topic == topic
    override fun hashCode(): Int = 31 * head.hashCode() + topic.hashCode()
    override fun toString(): String = "ConversationTitle($head$topic)"

    companion object {
        private const val SEPARATOR = " | "

        fun parse(title: String): ConversationTitle? {
            val parts = title.split(SEPARATOR)
            if (parts.size < 3 || parts[0].length != 4 || !parts[0].all { it in '0'..'9' }) return null
            if (parts[1].codePointCount(0, parts[1].length) !in 1..4) return null
            val topic = parts.drop(2).joinToString(SEPARATOR).trim()
            if (topic.isEmpty()) return null
            return ConversationTitle(parts[0] + SEPARATOR + parts[1] + SEPARATOR, topic)
        }
    }
}
