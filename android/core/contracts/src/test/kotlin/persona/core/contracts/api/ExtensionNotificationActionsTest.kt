package persona.core.contracts.api

import persona.core.contracts.generated.ExtensionNotifyAction
import persona.core.contracts.generated.ExtensionNotifyActionInput
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ExtensionNotificationActionsTest {
    private val scope = ExtensionNotificationScope("hub/generation", "group", "assistant")
    private var time = 10_000L
    private var tokens = 0
    private fun store(capacity: Int = 64) = ExtensionNotificationActionStore(
        capacity = capacity, now = { time }, token = { "%032x".format(++tokens) }, currentScope = { scope })

    private fun ExtensionNotificationActionStore.register(
        tag: String = "alert:one", action: ExtensionNotifyAction? = null,
    ) = register(tag, "example-gateway", "usage", "state", action, scope)!!

    @Test fun tokensAreIndependentRandomSizedCapabilitiesAndVanishWithTheStore() {
        val store = store()
        val records = store.registerBatch("alert:one", "example-gateway", "usage", "state",
            listOf(null, ExtensionNotifyAction("stop", "Stop")), scope)!!
        val body = records[0]
        val button = records[1]
        assertEquals(32, body.token.length)
        assertNotEquals(body.token, button.token)
        assertTrue(body.token.matches(Regex("[0-9a-f]{32}")))
        assertNull(ExtensionNotificationActionStore(currentScope = { scope }).peek(body.token))
    }

    @Test fun replacingOneAlertInvalidatesOnlyThatAlertsTokens() {
        val store = store()
        val old = store.register(action = ExtensionNotifyAction("one", "One"))
        val other = store.register(tag = "alert:other")
        val replacement = store.register(action = ExtensionNotifyAction("two", "Two"))
        assertNull(store.peek(old.token))
        assertTrue(store.peek(other.token) != null)
        assertTrue(store.peek(replacement.token) != null)
    }

    @Test fun invalidReplacementStillRetiresOnlyTheExactNotificationTag() {
        val store = store()
        val old = store.register(action = ExtensionNotifyAction("one", "One"))
        val other = store.register(tag = "alert:other")
        val replacement = store.registerBatch("alert:one", "example-gateway", "usage", "state",
            listOf(null, ExtensionNotifyAction("same", "Same"), ExtensionNotifyAction("same", "Duplicate")), scope)

        assertNull(replacement)
        assertNull(store.peek(old.token))
        assertTrue(store.peek(other.token) != null)
    }

    @Test fun boundedStoreEvictsTheOldestRecord() {
        val store = store(capacity = 2)
        val first = store.register(tag = "one")
        store.register(tag = "two")
        store.register(tag = "three")
        assertNull(store.peek(first.token))
    }

    @Test fun oneButtonClaimsOnceWhileAnotherButtonOfTheSameAlertStillRuns() {
        val store = store()
        val records = store.registerBatch("alert:one", "example-gateway", "usage", "state", listOf(
            ExtensionNotifyAction("one", "One", input = ExtensionNotifyActionInput("Text")),
            ExtensionNotifyAction("two", "Two")), scope)!!
        val first = records[0]
        val second = records[1]
        assertTrue(store.claim(first.token, "one", mapOf("text" to "hello")) is ExtensionNotificationClaim.Claimed)
        assertTrue(store.claim(first.token, "one", mapOf("text" to "again")) is ExtensionNotificationClaim.AlreadyHandled)
        assertTrue(store.claim(second.token, "two", emptyMap()) is ExtensionNotificationClaim.Claimed)
    }

    @Test fun shortDedupeAlsoCoversAReplacementTokenForTheSameButton() {
        val store = store()
        val first = store.register(action = ExtensionNotifyAction("one", "One"))
        assertTrue(store.claim(first.token, "one", emptyMap()) is ExtensionNotificationClaim.Claimed)
        val replacement = store.register(action = ExtensionNotifyAction("one", "One"))
        assertTrue(store.claim(replacement.token, "one", emptyMap()) is ExtensionNotificationClaim.AlreadyHandled)
        time += 300_001
        assertTrue(store.claim(replacement.token, "one", emptyMap()) is ExtensionNotificationClaim.Claimed)
    }

    @Test fun expiryWrongActionAndInputKeysCannotExecute() {
        val store = store()
        val record = store.register(action = ExtensionNotifyAction("note", "Reply", input = ExtensionNotifyActionInput()))
        assertTrue(store.claim(record.token, "other", mapOf("text" to "hello")) is ExtensionNotificationClaim.Invalid)
        assertTrue(store.claim(record.token, "note", mapOf("other" to "hello")) is ExtensionNotificationClaim.Invalid)
        assertTrue(store.claim(record.token, "note", mapOf("text" to "x".repeat(16_385))) is ExtensionNotificationClaim.Invalid)
        time += 900_001
        assertTrue(store.claim(record.token, "note", mapOf("text" to "hello")) is ExtensionNotificationClaim.Expired)
    }

    @Test fun aTokenNeverCrossesItsPairingGroupOrManager() {
        var current = scope
        val store = ExtensionNotificationActionStore(now = { time }, currentScope = { current })
        val record = store.register("alert", "example-gateway", "usage", null,
            ExtensionNotifyAction("stop", "Stop"), scope)!!
        current = scope.copy(pairingScope = "hub/other-generation")
        assertTrue(store.claim(record.token, "stop", emptyMap()) is ExtensionNotificationClaim.WrongScope)
        current = scope.copy(namespace = "other")
        assertTrue(store.claim(record.token, "stop", emptyMap()) is ExtensionNotificationClaim.WrongScope)
        current = scope.copy(manager = "other")
        assertTrue(store.claim(record.token, "stop", emptyMap()) is ExtensionNotificationClaim.WrongScope)
    }

    @Test fun malformedOwnershipAndForgedTokensAreRejected() {
        val store = store()
        for (bad in listOf(scope.copy(manager = "../other"), scope.copy(namespace = "bad/group"), scope.copy(pairingScope = "bad"))) {
            assertNull(store.register("alert", "example-gateway", "usage", null, null, bad))
        }
        assertTrue(store.claim("not-a-token", "one", emptyMap()) is ExtensionNotificationClaim.Missing)
        assertTrue(store.claim("0".repeat(32), "one", emptyMap()) is ExtensionNotificationClaim.Missing)
    }
}
