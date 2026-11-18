package persona.core.rustcore

/**
 * Test-only raw JSON navigation for the wire-envelope corpus. Production decoding is not involved.
 *
 * Its input member is returned as source text so duplicate keys survive exactly as the fixture wrote them. A small
 * recursive descent locator replaces a map round-trip, which would silently discard the later duplicate member.
 */
internal object EnvelopeRawJson {
    fun inputs(document: String): List<String> {
        val cases = Cursor(document).member("cases") ?: error("wire-envelope document has no cases")
        return Cursor(cases).arrayElements().map { case ->
            Cursor(case).member("input") ?: error("wire-envelope case has no input")
        }
    }

    private class Cursor(private val json: String) {
        private var at = 0

        fun arrayElements(): List<String> {
            expect('[')
            val values = mutableListOf<String>()
            if (take(']')) return values
            while (true) {
                values.add(value())
                when {
                    take(',') -> Unit
                    take(']') -> return values
                    else -> fail("array separator")
                }
            }
        }

        fun member(key: String): String? {
            expect('{')
            var found: String? = null
            if (take('}')) return null
            while (true) {
                val name = string()
                expect(':')
                val value = value()
                if (name == key && found == null) found = value
                when {
                    take(',') -> Unit
                    take('}') -> return found
                    else -> fail("object separator")
                }
            }
        }

        private fun value(): String {
            whitespace()
            val start = at
            when (peek()) {
                '{' -> objectValue()
                '[' -> arrayValue()
                '"' -> stringValue()
                else -> {
                    while (at < json.length && json[at] !in ",]} \t\n\r") at++
                    if (at == start) fail("JSON value")
                }
            }
            return json.substring(start, at)
        }

        private fun objectValue() {
            expect('{')
            if (take('}')) return
            while (true) {
                stringValue()
                expect(':')
                value()
                when {
                    take(',') -> Unit
                    take('}') -> return
                    else -> fail("object separator")
                }
            }
        }

        private fun arrayValue() {
            expect('[')
            if (take(']')) return
            while (true) {
                value()
                when {
                    take(',') -> Unit
                    take(']') -> return
                    else -> fail("array separator")
                }
            }
        }

        private fun string(): String {
            val raw = stringValue()
            return kotlinx.serialization.json.Json.parseToJsonElement(raw)
                .let { it as kotlinx.serialization.json.JsonPrimitive }.content
        }

        private fun stringValue(): String {
            expect('"')
            val start = at - 1
            while (true) {
                when (json.getOrElse(at) { fail("string end") }) {
                    '"' -> {
                        at++
                        return json.substring(start, at)
                    }
                    '\\' -> at += 2
                    else -> at++
                }
            }
        }

        private fun expect(char: Char) {
            whitespace()
            if (json.getOrNull(at) != char) fail("'$char'")
            at++
        }

        private fun take(char: Char): Boolean {
            whitespace()
            if (json.getOrNull(at) != char) return false
            at++
            return true
        }

        private fun peek(): Char {
            whitespace()
            return json.getOrElse(at) { fail("JSON value") }
        }

        private fun whitespace() {
            while (at < json.length && json[at] in " \t\n\r") at++
        }

        private fun fail(expected: String): Nothing = error("Malformed fixture at $at: expected $expected")
    }
}
