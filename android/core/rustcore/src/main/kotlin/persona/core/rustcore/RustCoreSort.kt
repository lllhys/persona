package persona.core.rustcore

/** DEBUG sort shadow's call site; Release R8 removes this unused wrapper and has no native export. */
object RustCoreSort {
    fun project(input: String): String = NativeBridge.projectSort(input)
}
