@file:OptIn(kotlinx.serialization.ExperimentalSerializationApi::class)

package persona.core.navigation

import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Test

class RouteSourceCompatibilityTest {
    private val json = Json { encodeDefaults = true }

    @Test fun storedChatSourcesKeepTheirOriginalTypeNames() {
        val sources = listOf(
            """{"type":"persona.core.navigation.RouteSource.InboxItem","itemId":"example-item"}""",
            """{"type":"persona.core.navigation.RouteSource.LarkConversation","chatId":"example-chat"}""",
            """{"type":"persona.core.navigation.RouteSource.Interaction","machineId":"example-machine","requestId":"example-request"}""",
        )
        val expected = listOf(
            RouteSource.InboxItem("example-item"),
            RouteSource.LarkConversation("example-chat"),
            RouteSource.Interaction("example-machine", "example-request"),
        )
        sources.zip(expected).forEach { (source, value) ->
            val stored = """{"machineId":"example-machine","sessionId":"example-session","kind":"Assistant","source":$source}"""
            val restored = json.decodeFromString(ChatRoute.serializer(), stored)
            assertEquals(value, restored.source)
            assertEquals(json.parseToJsonElement(stored),
                json.parseToJsonElement(json.encodeToString(ChatRoute.serializer(), restored)))
        }
    }

    @Test fun navigationKeysKeepTheirSavedStateNames() {
        assertEquals("persona.core.navigation.AssistantRoute", AssistantRoute.serializer().descriptor.serialName)
        assertEquals("persona.core.navigation.ChatRoute", ChatRoute.serializer().descriptor.serialName)
        assertEquals("persona.core.navigation.MachineDetailRoute", MachineDetailRoute.serializer().descriptor.serialName)
        assertEquals("persona.core.navigation.MachineSpacesRoute", MachineSpacesRoute.serializer().descriptor.serialName)
        assertEquals("persona.core.navigation.TerminalRoute", TerminalRoute.serializer().descriptor.serialName)
        assertEquals("Persona", AppTab.Persona.name)
    }
}
