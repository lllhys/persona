package persona.core.rustcore

/** DEBUG/test answer shadow's call sites; Release has neither these wrappers nor the corresponding native exports. */
object RustCoreAnswer {
    fun project(input: String): String = NativeBridge.projectAnswer(input)
    fun display(label: String): String = NativeBridge.displayAnswer(label)
}
