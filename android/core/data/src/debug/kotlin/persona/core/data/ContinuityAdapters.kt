package persona.core.data

/** JVM selection only; the app passes its build-gated factory explicitly. */
internal object ContinuityAdapters {
    fun create(chat: String, applied: (ContinuityAdapter.Applied) -> Unit): ContinuityAdapter =
        when (System.getProperty("persona.continuity", "kotlin")) {
            "rust" -> RustContinuityAdapter(chat, shared, applied)
            "shadow" -> ShadowContinuityAdapter(chat, sink = ShadowContinuityDiagnostics::report,
                afterFirstFrame = if (System.getProperty("persona.continuity.frameScheduler") != null) ({ it() })
                    else AndroidContinuityFrameScheduler()::afterFirstFrame)
            else -> KotlinContinuityAdapter(chat)
        }

    private val shared by lazy {
        val scheduler = System.getProperty("persona.continuity.frameScheduler")?.let {
            Class.forName(it).getDeclaredConstructor().newInstance() as ContinuityFrameScheduler
        } ?: AndroidContinuityFrameScheduler()
        RustContinuityDriver(scheduler)
    }
}
