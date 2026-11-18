package persona.core.data

/** Release has no native or shadow selection, including JVM properties. */
internal object ContinuityAdapters {
    fun create(chat: String, applied: (ContinuityAdapter.Applied) -> Unit): ContinuityAdapter =
        KotlinContinuityAdapter(chat)
}
