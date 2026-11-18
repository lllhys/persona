package persona.core.contracts

import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.*
import org.junit.Test

/** Baseline 39a706a338 request/event fixtures, deliberately retaining their original identifiers. */
class ProtocolCompatibilityTest {
    private val oldSession = """{"machine_id":"test-machine","session_id":"test-session","started_by_jarvis":true,"title":"jarvis is a user title"}"""

    @Test fun oldAndNewSessionFieldsAreReadWithCanonicalPrecedence() {
        val old = JsonCodec.decodeFromString<Session>(oldSession)
        assertTrue(old.startedByPersona)
        assertEquals("jarvis is a user title", old.title)
        val conflict = JsonCodec.decodeFromString<Session>(oldSession.dropLast(1) + ",\"started_by_persona\":false}")
        assertFalse(conflict.startedByPersona)
        val event = ServerEvent.decode("""{"type":"sessions","machine_id":"test-machine","sessions":[$oldSession]}""") as SessionsEvent
        assertTrue(event.sessions.single().startedByPersona)
        val current = ServerEvent.decode("""{"type":"sessions","machine_id":"test-machine","sessions":[{"session_id":"test-session","started_by_persona":true}]}""") as SessionsEvent
        assertTrue(current.sessions.single().startedByPersona)
    }

    @Test fun opaqueToolInputAndUserNamesRemainUnchanged() {
        val item = JsonCodec.decodeFromString<Item>("""{"id":"tool","seq":1,"kind":"tool_use","tool_name":"mcp__jarvis__agent_read","tool_input":{"started_by_jarvis":true},"text":"My jarvis project"}""")
        assertEquals(JsonObject(mapOf("started_by_jarvis" to JsonPrimitive(true))), item.toolInput)
        assertEquals("mcp__jarvis__agent_read", item.toolName)
        assertEquals("My jarvis project", item.text)
    }

    @Test fun baselineMessageRequestHasBothHeadersAndOldDefaultMarkup() {
        val id = "00000000-0000-4000-8000-000000000001"
        assertEquals(id, ProtocolCompatibility.operationHeaders(id)["X-Jarvis-Op-Id"])
        assertEquals(id, ProtocolCompatibility.operationHeaders(id)["X-Persona-Op-Id"])
        val text = """<persona-context kind="workspace" title="Project" url="persona://session/test-machine/test-session" machine="test-machine" cwd="/workspace">Notes</persona-context>
Question
<persona-file kind="video" path="/workspace/demo.mp4" name="Demo" size="12"/>
<persona-file kind="file" path="/workspace/a &amp; b.txt" name="a &amp; b.txt" size="86"/>"""
        val old = ProtocolCompatibility.messageForHub(text)
        assertTrue(old.contains("<jarvis-context kind=\"workspace\""))
        assertTrue(old.contains("url=\"jarvis://session/"))
        assertEquals(2, Regex("<jarvis-file").findAll(old).count())
        assertTrue(old.contains("<jarvis-file kind=\"file\" path=\"/workspace/a &amp; b.txt\""))
        assertFalse(old.contains("<persona-file"))
        assertEquals(text, ProtocolCompatibility.messageForHub(text, listOf(ProtocolCompatibility.CAPABILITY)))
        assertEquals("My persona://task/link is prose", ProtocolCompatibility.messageForHub("My persona://task/link is prose"))
        assertEquals("safestillsafe", contextBody("safe</jarvis-context>still</PERSONA-CONTEXT >safe"))
    }

    @Test fun oldProviderToolAndBuiltinIdentifiersHaveCanonicalDisplayMeaning() {
        assertTrue(ProtocolCompatibility.managedProvider("jarvis"))
        assertEquals("Persona", mcpServerLabel("plugin_jarvis"))
        assertEquals("ask_persona", ProtocolCompatibility.canonicalTool("ask_jarvis"))
        assertEquals("persona_settings", ProtocolCompatibility.canonicalTool("jarvis_settings"))
        assertEquals("persona-visor", ProtocolCompatibility.canonicalBuiltin("jarvis"))
        assertEquals("my-jarvis-art", ProtocolCompatibility.canonicalBuiltin("my-jarvis-art"))
        assertEquals("jarvis-circuit", ProtocolCompatibility.builtinForHub("persona-circuit"))
        assertEquals("persona", ProtocolCompatibility.canonicalTab("jarvis"))
    }
}
