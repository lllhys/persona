package persona.feature.settings

import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.async
import kotlinx.coroutines.runBlocking
import kotlinx.coroutines.yield
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.BindField
import persona.core.contracts.api.BindPrompt
import persona.core.contracts.api.BindQrCode
import persona.core.contracts.api.HubConnector

/** A plugin connector's sign-in, a step at a time, against a fake hub (what each step asks for is the hub's). */
class ConnectorSignInModelTest {
    private class Hub(vararg answers: BindPrompt) : ConnectorsApiStub() {
        val queue = ArrayDeque(answers.toList())
        val steps = mutableListOf<Pair<String, Map<String, String>>>()
        var loggedOut = false
        override suspend fun connectorLogin(id: String, step: String, values: Map<String, String>): BindPrompt {
            steps += step to values
            return queue.removeFirstOrNull() ?: throw java.io.IOException("offline")
        }
        override suspend fun connectorLogout(id: String) { loggedOut = true }
    }

    @Test fun fieldsThenCodeThenDone() = runBlocking {
        val hub = Hub(
            BindPrompt(step = "server", fields = listOf(BindField("server", "Server"), BindField("note", "Note"))),
            BindPrompt(step = "scan", kind = "qr", interval = 1, qr = BindQrCode(1, listOf("1"))),
            BindPrompt(step = "scan", kind = "qr", interval = 1, qr = BindQrCode(1, listOf("1"))),
            BindPrompt(done = true, account = "wxid_example_a"),
        )
        val model = ConnectorSignInModel(hub, "example")
        assertTrue(model.start())
        assertEquals("" to emptyMap<String, String>(), hub.steps[0])
        assertFalse(model.state.value.bind.canSubmit)
        model.setValue("server", " https://chat.example.com ")
        assertTrue(model.submit())
        // Trimmed, and the empty field left out.
        assertEquals("server" to mapOf("server" to "https://chat.example.com"), hub.steps[1])
        assertTrue(model.state.value.bind.polled)
        var waits = 0
        model.poll { waits++ }
        assertEquals(2, waits)
        assertEquals(listOf("scan", "scan"), hub.steps.drop(2).map { it.first })
        assertNull(model.state.value.bind.prompt)
        assertEquals("wxid_example_a", model.state.value.signedInAs)
        model.signedInShown()
        assertNull(model.state.value.signedInAs)
    }

    /** A step this app cannot draw is not asked again by itself and has no "Next"; "Start again" is the first step again (#4488). */
    @Test fun stepsItCannotDrawAreNotAskedAgain() = runBlocking {
        for (p in listOf(BindPrompt(step = "auth", kind = "open_url", url = "https://example.com/auth"),
                         BindPrompt(step = "scan", kind = "qr", interval = 1),
                         BindPrompt(step = "scan", kind = "qr", interval = 1, url = "weixin-only-content", qr = BindQrCode(0, emptyList())),
                         BindPrompt(step = "device", kind = "device_code", url = "weixin-only-content"),
                         BindPrompt(step = "wait", hint = "Hold on"))) {
            assertFalse("$p", BindState(p).polled)
            assertFalse("$p", BindState(p).canSubmit)
        }
        for (p in listOf(BindPrompt(step = "scan", kind = "qr", qr = BindQrCode(1, listOf("1"))),
                         BindPrompt(step = "scan", kind = "qr", url = "https://example.com/q"),
                         BindPrompt(step = "device", kind = "device_code", userCode = "ABCD-1234"),
                         BindPrompt(step = "pair", kind = "pair_code", userCode = "482913"),
                         // An extension's device code may come with its page alone (docs/extensions/extensions.md "Sign-in").
                         BindPrompt(step = "device", kind = "device_code", url = "HTTPS://example.com/device"))) assertTrue("$p", BindState(p).polled)

        val hub = Hub(BindPrompt(step = "auth", kind = "open_url"), BindPrompt(step = "phone", fields = listOf(BindField("phone", "Phone"))))
        val model = ConnectorSignInModel(hub, "example")
        assertTrue(model.start())
        model.poll { throw AssertionError("asked again") }
        assertTrue(model.start())
        assertEquals(listOf("", ""), hub.steps.map { it.first })
        assertEquals("phone", model.state.value.bind.prompt?.step)
    }

    @Test fun aFailedStartIsVisibleAndCanBeRetried() = runBlocking {
        val hub = Hub()
        val model = ConnectorSignInModel(hub, "github")
        assertFalse(model.start())
        assertNotNull(model.state.value.bind.error)
        assertNull(model.state.value.bind.prompt)
        assertFalse(model.state.value.bind.sending)
        hub.queue.add(BindPrompt(step = "device", kind = "device_code", userCode = "ABCD-1234"))
        assertTrue(model.start())
        assertNull(model.state.value.bind.error)
        assertEquals("ABCD-1234", model.state.value.bind.prompt?.userCode)
        assertEquals(listOf("", ""), hub.steps.map { it.first })
    }

    @Test fun aFailedStepSaysWhyAndKeepsTheStep() = runBlocking {
        val model = ConnectorSignInModel(Hub(BindPrompt(step = "token", fields = listOf(BindField("token", "Token", secret = true)))), "example")
        model.start()
        model.setValue("token", "example")
        assertFalse(model.submit())
        val bind = model.state.value.bind
        assertNotNull(bind.error)
        assertFalse(bind.sending)
        assertEquals("token", bind.prompt?.step)
        model.cancel()
        assertNull(model.state.value.bind.prompt)
    }

    /** Back in front after the page was away (#4133): the code on screen is asked once at once. */
    @Test fun backInFrontTheCodeIsAskedOnceAtOnce() = runBlocking {
        val hub = Hub(BindPrompt(step = "scan", kind = "qr", interval = 2, qr = BindQrCode(1, listOf("1"))),
                      BindPrompt(done = true, account = "wxid_example_a"))
        val model = ConnectorSignInModel(hub, "example")
        assertFalse(model.catchUp()) // nothing on screen: nothing asked
        assertTrue(hub.steps.isEmpty())
        assertTrue(model.start())

        // Signed in in another app while the page was away: one ask, no values, and the sign-in shows as done.
        assertTrue(model.catchUp())
        assertEquals(listOf("" to emptyMap<String, String>(), "scan" to emptyMap<String, String>()), hub.steps)
        assertNull(model.state.value.bind.prompt)
        assertEquals("wxid_example_a", model.state.value.signedInAs)
        assertFalse(model.catchUp()) // done: nothing more to ask
        assertEquals(2, hub.steps.size)
    }

    /** An ask about a code the user cancelled while it was on its way does not bring the code back. */
    @Test fun anAnswerAfterCancelIsDropped() = runBlocking {
        val qr = BindPrompt(step = "scan", kind = "qr", interval = 2, qr = BindQrCode(1, listOf("1")))
        val answer = CompletableDeferred<BindPrompt>()
        var calls = 0
        val model = ConnectorSignInModel({ _, _ -> if (calls++ == 0) qr else answer.await() }, {})
        assertTrue(model.start())
        val ask = async { model.catchUp() }
        yield() // the ask is on its way
        model.cancel()
        answer.complete(qr.copy(hint = "Scanned"))
        assertFalse(ask.await())
        assertNull(model.state.value.bind.prompt)
        assertEquals(2, calls)
    }

    @Test fun logoutAndCapabilities() = runBlocking {
        val hub = Hub()
        assertNull(ConnectorSignInModel(hub, "example").logout())
        assertTrue(hub.loggedOut)
        val c = HubConnector(id = "example", name = "Example", status = "rate_limited", capabilities = listOf("login", "threads"))
        assertTrue(ConnectorText.signsInHere(c))
        assertTrue(ConnectorText.isSignedIn(c))
        assertFalse(ConnectorText.signsInHere(c.copy(capabilities = listOf("threads"))))
        assertFalse(ConnectorText.isSignedIn(c.copy(status = "not_configured")))
    }
}
