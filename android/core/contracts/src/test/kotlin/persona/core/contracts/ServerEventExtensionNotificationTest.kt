package persona.core.contracts

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ServerEventExtensionNotificationTest {
    @Test fun oldAndNullPayloadsRemainOrdinaryAlerts() {
        val old = ServerEvent.decode("""{"type":"notify","kind":"alert","machine_id":"","session_id":"","title":"Alert"}""")
        val nullPayload = ServerEvent.decode("""{"type":"notify","kind":"alert","machine_id":"","session_id":"","extension_notification":null}""")
        assertTrue(old is NotifyEvent)
        assertTrue(nullPayload is NotifyEvent)
        assertNull((old as NotifyEvent).extensionNotification)
        assertNull((nullPayload as NotifyEvent).extensionNotification)
    }

    @Test fun completePayloadDecodesWithoutDroppingUnknownFields() {
        val event = ServerEvent.decode("""{
            "type":"notify","kind":"alert","machine_id":"","session_id":"",
            "extension_notification":{"extension":"example-gateway","ui":"usage","state":"range=1d",
                "future_field":true,"actions":[{"id":"stop","title":"Disable this key","role":"destructive",
                "foreground":true,"input":{"placeholder":"Why"},"future_action_field":1}]}
        }""") as NotifyEvent
        val payload = event.extensionNotification!!
        assertEquals("example-gateway", payload.extension)
        assertEquals("usage", payload.ui)
        assertEquals("range=1d", payload.state)
        assertEquals("stop", payload.actions!!.single().id)
        assertEquals("destructive", payload.actions.single().role)
        assertEquals(true, payload.actions.single().foreground)
        assertEquals("Why", payload.actions.single().input!!.placeholder)
    }

    @Test fun malformedRunFlagsOnlyLoseTheExtensionPayload() {
        for (field in listOf("\"foreground\":\"yes\"", "\"input\":[]")) {
            val event = ServerEvent.decode("""{"type":"notify","kind":"alert","machine_id":"","session_id":"",
                "title":"Still shown","extension_notification":{"extension":"example-gateway","ui":"usage",
                "actions":[{"id":"note","title":"Reply",$field}]}}""") as NotifyEvent
            assertEquals("Still shown", event.title)
            assertNull(event.extensionNotification)
        }
    }
}
