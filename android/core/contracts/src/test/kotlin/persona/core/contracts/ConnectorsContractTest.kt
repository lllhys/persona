package persona.core.contracts

import kotlinx.serialization.json.JsonPrimitive
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import persona.core.contracts.api.*

class ConnectorsContractTest {
    @Test
    fun roundTripMailAccount() {
        val original = MailAccount(
            id = "acc1",
            name = "Test",
            email = "user@example.com",
            provider = "gmail",
            auth = "oauth2",
            username = "user",
            imapHost = "imap.gmail.com",
            imapPort = 993,
            status = "ok",
        )
        val json = JsonCodec.encodeToString(MailAccount.serializer(), original)
        val decoded = JsonCodec.decodeFromString(MailAccount.serializer(), json)
        assertEquals(original, decoded)
        assertFalse(decoded.isError)
        assertEquals("Gmail · Google 账号登录", (decoded.detail).zhAny())
    }

    @Test
    fun roundTripAddMailAccountRequest() {
        val req = AddMailAccountRequest(
            name = "name@example.com",
            email = "name@example.com",
            provider = "imap",
            auth = "password",
            password = "secret",
            imapHost = "imap.example.com",
            imapPort = 993,
            smtpHost = "smtp.example.com",
            smtpPort = 587,
            smtpSecurity = "starttls",
        )
        val json = JsonCodec.encodeToString(AddMailAccountRequest.serializer(), req)
        val decoded = JsonCodec.decodeFromString(AddMailAccountRequest.serializer(), json)
        assertEquals(req, decoded)
    }

    @Test
    fun roundTripLarkStatusAndAccounts() {
        val status = LarkStatus(configured = true, connected = true, name = "张三", lastEventAt = 1700000000L)
        val json = JsonCodec.encodeToString(LarkStatus.serializer(), status)
        val decodedStatus = JsonCodec.decodeFromString(LarkStatus.serializer(), json)
        assertEquals(status, decodedStatus)

        val accounts = LarkAccounts(
            accounts = listOf(
                LarkAccount(id = "u1", title = "张三", tenant = "公司A", unread = 3),
                LarkAccount(id = "u2", title = "李四", tenant = "公司B", unread = 0),
            ),
            default = "u1",
        )
        val accJson = JsonCodec.encodeToString(LarkAccounts.serializer(), accounts)
        val decodedAccs = JsonCodec.decodeFromString(LarkAccounts.serializer(), accJson)
        assertEquals(accounts, decodedAccs)
    }

    @Test
    fun roundTripHubConnector() {
        val connector = HubConnector(
            id = "github",
            name = "GitHub",
            status = "ok",
            account = "octocat",
            via = "macbook",
            enabled = true,
            archive = true,
            inbox = false,
            capabilities = listOf("login", "archive", "inbox"),
        )
        val json = JsonCodec.encodeToString(HubConnector.serializer(), connector)
        val decoded = JsonCodec.decodeFromString(HubConnector.serializer(), json)
        assertEquals(connector, decoded)
        assertFalse(json.contains("options"))
    }

    @Test
    fun gitHubAuthAndInboxOptionsTolerateMissingFields() {
        val auth = JsonCodec.decodeFromString(GitHubAuth.serializer(), """{"source":"device","scopes_known":false,"extra":1}""")
        assertEquals("device", auth.source)
        assertNull(auth.machine)
        assertEquals(emptyList<GitHubMachine>(), auth.machines)
        assertEquals(false, auth.scopesKnown)
        assertNull(auth.missing)

        val machine = JsonCodec.decodeFromString(
            GitHubAuth.serializer(),
            """{"source":"machine","machine":"studio","machines":[{"id":"studio","name":"Studio","state":"offline","extra":true}]}""",
        )
        assertEquals("studio", machine.machine)
        assertEquals("offline", machine.machines.single().state)
        assertNull(machine.machines.single().account)

        val options = GitHubInboxOptions.parse(JsonCodec.parseToJsonElement("{}"))
        assertEquals(GitHubInboxOptions.CI_MINE, options?.ci)
        assertEquals(emptyList<String>(), options?.mutedRepos)
        assertNull(options?.ignoreBranches)
        assertEquals(listOf("train/*"), options?.ignoreBranchesShown)
        assertNull(GitHubInboxOptions.parse(null))
        assertNull(GitHubInboxOptions.parse(JsonPrimitive("x")))

        val connector = JsonCodec.decodeFromString(
            HubConnector.serializer(),
            """{"id":"github","name":"GitHub","options":{"muted_repos":["acme/app"]}}""",
        )
        val parsed = GitHubInboxOptions.parse(connector.options)
        assertEquals(listOf("acme/app"), parsed?.mutedRepos)
        assertEquals(GitHubInboxOptions.CI_MINE, parsed?.ci)
        val explicitNull = GitHubInboxOptions.parse(JsonCodec.parseToJsonElement("""{"ci":"off","muted_repos":[],"ignore_branches":null}"""))
        assertEquals("off", explicitNull?.ci)
        assertNull(explicitNull?.ignoreBranches)
    }
}
