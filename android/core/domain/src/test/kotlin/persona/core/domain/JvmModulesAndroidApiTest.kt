package persona.core.domain

import java.io.File
import org.junit.Assert.assertEquals
import org.junit.Test

/**
 * The plain JVM modules (core:contracts, core:domain, core:hub, core:terminal) compile against the JDK, not android.jar:
 * neither lint's NewApi check nor desugaring sees them, so a JDK method newer than the app's minSdk (30) builds and
 * then throws NoSuchMethodError on the phone. LocalDate.ofInstant (Android 34) crashed the main conversation on
 * Android 12 and HarmonyOS 4; URLEncoder.encode(String, Charset) (Android 33) broke the connector pages there.
 *
 * Limits: this only looks for the calls listed below, by matching the source text line by line. It is not an API
 * check: any other newer method, a call split over lines, or a Charset passed in a variable still builds and
 * still crashes. Comparing the compiled classes with android.jar's api-versions.xml (what found the URLEncoder
 * call) is the real check.
 */
class JvmModulesAndroidApiTest {
    private class Newer(val pattern: Regex, val call: String, val instead: String)

    private val newer = listOf(
        Newer(Regex("""\bLocalDate\.ofInstant\("""), "LocalDate.ofInstant (API 34)", "instant.atZone(zone).toLocalDate()"),
        Newer(Regex("""\bLocalTime\.ofInstant\("""), "LocalTime.ofInstant (API 34)", "instant.atZone(zone).toLocalTime()"),
        Newer(Regex("""\.toEpochSecond\(\s*\w*[Tt]ime\b"""), "LocalDate.toEpochSecond(LocalTime, ZoneOffset) (API 34)",
            "date.atTime(time).toEpochSecond(offset)"),
        Newer(Regex("""\.toDaysPart\(\)"""), "Duration.toDaysPart (API 31)", "toDays()"),
        Newer(Regex("""\.toHoursPart\(\)"""), "Duration.toHoursPart (API 31)", "(toHours() % 24).toInt()"),
        Newer(Regex("""\.toMinutesPart\(\)"""), "Duration.toMinutesPart (API 31)", "(toMinutes() % 60).toInt()"),
        Newer(Regex("""\.toSecondsPart\(\)"""), "Duration.toSecondsPart (API 31)", "(seconds % 60).toInt()"),
        Newer(Regex("""\.dividedBy\(\s*Duration\b"""), "Duration.dividedBy(Duration) (API 31)", "toNanos() / other.toNanos()"),
        // The Charset overloads (API 33), when the charset is written as one: Charsets.UTF_8, StandardCharsets.UTF_8,
        // Charset.forName(…). "UTF-8" and StandardCharsets.UTF_8.name() are Strings: the old overload, fine.
        Newer(Regex("""\bURL(En|De)coder\.(encode|decode)\((?:[^,()]|\([^()]*\))*,\s*(?:(?:kotlin\.text\.)?Charsets\.\w+\b|(?:java\.nio\.charset\.)?StandardCharsets\.\w+\b(?!\s*\.name\(\))|(?:java\.nio\.charset\.)?Charset\.forName\()"""),
            "URLEncoder.encode / URLDecoder.decode(String, Charset) (API 33)", "the \"UTF-8\" overload"),
    )

    @Test fun noListedJdkCallNewerThanTheAppsMinimumAndroid() {
        val root = File(System.getProperty("persona.repo.root") ?: "../..", "android")
        val found = listOf("contracts", "domain", "hub", "terminal").flatMap { module ->
            File(root, "core/$module/src/main").walkTopDown().filter { it.isFile && it.extension == "kt" }.flatMap { file ->
                file.readLines().withIndex().flatMap { (n, line) ->
                    val code = line.substringBefore("//")
                    newer.filter { it.pattern.containsMatchIn(code) }.map { "${file.relativeTo(root)}:${n + 1}: ${it.call}, use ${it.instead}" }
                }
            }.toList()
        }
        assertEquals("JDK calls Android adds after API 30", emptyList<String>(), found)
    }

    @Test fun thePatternsCatchTheCallsTheyName() {
        fun caught(line: String) = newer.filter { it.pattern.containsMatchIn(line) }.map { it.call }
        assertEquals(1, caught("""if (LocalDate.ofInstant(at, clock.zone) == today) return""").size)
        assertEquals(1, caught("""fun encode(v: String) = java.net.URLEncoder.encode(v, Charsets.UTF_8).replace("+", "%20")""").size)
        assertEquals(1, caught("""URLDecoder.decode(value, StandardCharsets.UTF_8)""").size)
        assertEquals(1, caught("""URLEncoder.encode(name.trim(), Charset.forName("UTF-8"))""").size)
        assertEquals(1, caught("""day.toEpochSecond(LocalTime.MIDNIGHT, ZoneOffset.UTC)""").size)
        // The old overloads and look-alikes are not flagged.
        assertEquals(emptyList<String>(), caught("""URLEncoder.encode(segment, "UTF-8").replace("+", "%20")"""))
        assertEquals(emptyList<String>(), caught("""URLEncoder.encode(name.trim(), "UTF-8")"""))
        assertEquals(emptyList<String>(), caught("""URLDecoder.decode(value, StandardCharsets.UTF_8.name())"""))
        assertEquals(emptyList<String>(), caught("""at.atZone(clock.zone).toLocalDate()"""))
        assertEquals(emptyList<String>(), caught("""dateTime.toEpochSecond(ZoneOffset.UTC)"""))
    }
}
