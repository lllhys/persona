package persona.platform.notify

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import persona.core.contracts.NotifyEvent
import persona.core.contracts.api.ExtensionNotificationActionRecord
import persona.core.contracts.api.ExtensionNotificationActionStore
import persona.core.contracts.api.ExtensionNotificationScope
import persona.core.contracts.generated.ExtensionNotification
import persona.core.contracts.generated.ExtensionNotifyAction
import persona.core.contracts.generated.ExtensionNotifyActionInput

@RunWith(RobolectricTestRunner::class)
class ExtensionNotificationActionsTest {
    private val context = ApplicationProvider.getApplicationContext<Context>()
    private val scope = ExtensionNotificationScope("hub/generation", null, "assistant")

    @Test fun validationAcceptsAtMostThreeDistinctActionsAndPreservesTheirDeclarations() {
        val actions = listOf(
            ExtensionNotifyAction("stop", " Disable   this\nkey ", "destructive", true),
            ExtensionNotifyAction("note", "Reply", input = ExtensionNotifyActionInput("Say something")),
            ExtensionNotifyAction("refresh", "Refresh"),
        )
        val validated = ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage", "state", actions))!!
        val validatedActions = validated.actions!!
        assertEquals("Disable this key", validatedActions[0].title)
        assertEquals("destructive", validatedActions[0].role)
        assertEquals(true, validatedActions[0].foreground)
        assertEquals("Say something", validatedActions[1].input!!.placeholder)
        assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage",
            actions = actions + ExtensionNotifyAction("four", "Four"))))
        assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage",
            actions = listOf(actions[0], actions[0].copy(title = "Again")))))
        assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage",
            actions = listOf(ExtensionNotifyAction("bad", "Bad", "unknown")))))
        assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", null,
            actions = listOf(ExtensionNotifyAction("bad", "Bad")))))
        assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage",
            state = "x".repeat(16_385))))
        for (id in listOf("", "x".repeat(129), "default", "dismiss")) {
            assertNull(ExtensionNotificationPayload.validate(ExtensionNotification("example-gateway", "usage",
                actions = listOf(ExtensionNotifyAction(id, "Title")))))
        }
    }

    @Test fun bodyDestinationsUseTheDeclaredPageOrTheExtensionDetails() {
        val page = record("body", null).copy(ui = "usage")
        val details = record("details", null).copy(ui = null)
        val intents = ExtensionNotificationIntents(context)
        val pageLink = intents.activityIntent(page, "Usage").data!!
        val detailLink = intents.activityIntent(details, "Usage").data!!
        assertEquals("usage", pageLink.getQueryParameter("ui"))
        assertEquals(page.token, pageLink.getQueryParameter("notification"))
        assertNull(detailLink.getQueryParameter("ui"))
        assertNull(detailLink.getQueryParameter("notification"))
        assertNull(pageLink.getQueryParameter("action"))
    }

    @Test fun buttonIntentsCannotOverwriteEachOtherAndInputIsOnlyDeclaredWhereAsked() {
        val records = listOf(
            record("note", ExtensionNotifyAction("note", "Reply", input = ExtensionNotifyActionInput("Say something"))),
            record("refresh", ExtensionNotifyAction("refresh", "Refresh")),
            record("stop", ExtensionNotifyAction("stop", "Stop", "destructive", true)),
        )
        val intents = ExtensionNotificationIntents(context)
        val data = records.map { intents.broadcastIntent(it).data!! }
        assertEquals(3, data.toSet().size)
        assertNotEquals(data[0], data[1])
        val actions = records.map { intents.action(it) }
        assertEquals("Say something", actions[0].remoteInputs!!.single().label)
        assertTrue(actions[1].remoteInputs.isNullOrEmpty())
        assertTrue(actions[2].isAuthenticationRequired)
        assertEquals("destructive", records[2].action!!.role)
        assertTrue(data.all { it.getQueryParameter("action") == null })
    }

    @Test fun unscopedAlertsNeedOneOpenGroupAndNamespacedIdsStayOnTheirOwnHub() {
        assertNull(extensionEventScope(environment(scope), "", null))
        assertEquals(scope, extensionEventScope(environment(scope, unscoped = true), "", null))
        assertNull(extensionEventScope(environment(scope), "machine~g00000002", null))
        assertNull(extensionEventScope(environment(scope), "", "alert~g00000002"))
        val other = scope.copy(namespace = "g00000002")
        assertEquals(other, extensionEventScope(environment(other), "machine~g00000002", null))
        assertEquals(other, extensionEventScope(environment(other), "", "alert~g00000002"))

        assertEquals("raw machine and alert ids consistently name the primary scope", scope,
            extensionEventScope(environment(scope), "machine", "alert"))
        assertNull("primary machine conflicts with a namespaced alert",
            extensionEventScope(environment(scope), "machine", "alert~g00000002"))
        val first = scope.copy(namespace = "g00000001")
        assertNull("two different namespaces conflict",
            extensionEventScope(environment(first), "machine~g00000001", "alert~g00000002"))
        assertEquals("matching machine and alert namespaces are accepted", first,
            extensionEventScope(environment(first), "machine~g00000001", "alert~g00000001"))
    }

    @Test fun everyDegradedReplacementRetiresOldTokensAndPreservesOtherTags() {
        val environment = environment(scope)
        var tokens = 0
        val store = ExtensionNotificationActionStore(currentScope = environment::currentScope,
            token = { "%032x".format(++tokens) })
        val payload = ExtensionNotification("example-gateway", "usage", "state",
            listOf(ExtensionNotifyAction("stop", "Stop", foreground = true)))
        fun event(body: ExtensionNotification?, machine: String = "machine", id: String? = "alert") =
            NotifyEvent("alert", machine, "", id = id, extensionNotification = body)
        fun valid() = prepareExtensionNotification(event(payload), "alert:one", environment, store)!!

        val other = prepareExtensionNotification(event(payload, id = "other"), "alert:other", environment, store)!!
        val withActions = valid()
        val noActions = prepareExtensionNotification(event(payload.copy(actions = emptyList())), "alert:one", environment, store)!!
        assertTrue(noActions.buttons.isEmpty())
        assertNull(store.peek(withActions.body.token))
        assertTrue(store.peek(other.body.token) != null)

        val invalid = valid()
        assertNull(prepareExtensionNotification(event(payload.copy(actions = listOf(
            ExtensionNotifyAction("same", "Same"), ExtensionNotifyAction("same", "Duplicate")))), "alert:one", environment, store))
        assertNull(store.peek(invalid.body.token))
        assertTrue(store.peek(other.body.token) != null)

        val wrongSource = valid()
        assertNull(prepareExtensionNotification(event(payload, machine = "machine~g00000002", id = null),
            "alert:one", environment, store))
        assertNull(store.peek(wrongSource.body.token))
        assertTrue(store.peek(other.body.token) != null)

        val ordinary = valid()
        assertNull(prepareExtensionNotification(event(null), "alert:one", environment, store))
        assertNull(store.peek(ordinary.body.token))
        assertTrue(store.peek(other.body.token) != null)
    }

    private fun environment(scope: ExtensionNotificationScope, unscoped: Boolean = false) = object : ExtensionNotificationEnvironment {
        override fun currentScope() = scope
        override fun canRouteUnscopedEvents() = unscoped
        override fun extensionUi(record: ExtensionNotificationActionRecord) = null
    }

    private fun record(token: String, action: ExtensionNotifyAction?) = ExtensionNotificationActionRecord(
        token = token.padEnd(32, '0'), tag = "alert:$token", extension = "example-gateway", ui = "usage", state = "state",
        action = action, scope = scope, createdAt = 1, dedupeKey = token)
}
