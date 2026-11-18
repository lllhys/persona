package persona.core.domain

import persona.core.contracts.CoreText
import java.io.File
import javax.xml.parsers.DocumentBuilderFactory

/**
 * [CoreText] in Simplified Chinese for JVM tests, read from core/designsystem's values-zh/strings_core.xml (the app
 * resolves it with Android resources; these modules have none).
 */
internal fun CoreText.zh(): String = when (key) {
    CoreText.Key.Verbatim -> args.first().toString()
    CoreText.Key.Joined -> args.drop(1).map { (it as CoreText).zh() }.filter(String::isNotEmpty)
        .joinToString(args.first().let { if (it is CoreText) it.zh() else it.toString() })
    CoreText.Key.CompactCount -> (args.first() as Number).toLong().let { count ->
        if (count < 10_000) count.toString()
        else String.format(ZhStrings.table.getValue("designsystem_core_compact_count"), java.math.BigDecimal(count).divide(java.math.BigDecimal(10_000))
            .setScale(1, java.math.RoundingMode.HALF_UP).stripTrailingZeros().toPlainString())
    }
    else -> {
        val name = "designsystem_core_" + key.name.replace(Regex("(?<!^)(?=[A-Z])"), "_").lowercase()
        val text = ZhStrings.table[name] ?: error("no zh string $name")
        val values = args.map { if (it is CoreText) it.zh() else it }.toTypedArray()
        if (values.isEmpty()) text else String.format(text, *values)
    }
}

private object ZhStrings {
    val table: Map<String, String> by lazy { stringsOf("values-zh") }
}

/** [CoreText] in Japanese, from core/designsystem's values-ja/strings_core.xml (as [zh] does for Simplified Chinese). */
internal fun CoreText.ja(): String = when (key) {
    CoreText.Key.Verbatim -> args.first().toString()
    CoreText.Key.Joined -> args.drop(1).map { (it as CoreText).ja() }.filter(String::isNotEmpty)
        .joinToString(args.first().let { if (it is CoreText) it.ja() else it.toString() })
    else -> {
        val name = "designsystem_core_" + key.name.replace(Regex("(?<!^)(?=[A-Z])"), "_").lowercase()
        val text = JaStrings.table[name] ?: error("no ja string $name")
        val values = args.map { if (it is CoreText) it.ja() else it }.toTypedArray()
        if (values.isEmpty()) text else String.format(text, *values)
    }
}

private object JaStrings {
    val table: Map<String, String> by lazy { stringsOf("values-ja") }
}

/** core/designsystem's strings_core.xml of one language, a plural by its first item (Chinese and Japanese have only "other"). */
private fun stringsOf(dir: String): Map<String, String> {
    val file = generateSequence(File("").absoluteFile) { it.parentFile }
        .map { File(it, "core/designsystem/src/main/res/$dir/strings_core.xml") }.first { it.exists() }
    val root = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(file).documentElement
    val out = mutableMapOf<String, String>()
    val nodes = root.childNodes
    for (i in 0 until nodes.length) {
        val node = nodes.item(i) as? org.w3c.dom.Element ?: continue
        val text = if (node.tagName == "plurals") node.getElementsByTagName("item").item(0).textContent else node.textContent
        out[node.getAttribute("name")] = text.replace("\\n", "\n").replace("\\'", "'").replace("\\\"", "\"").replace("\\@", "@").replace("\\?", "?")
    }
    return out
}

/** [zh] through lists and nulls, for assertions on values that may be [CoreText]. */
internal fun Any?.zhAny(): Any? = when (this) {
    is CoreText -> zh()
    is List<*> -> map { it.zhAny() }
    else -> this
}
