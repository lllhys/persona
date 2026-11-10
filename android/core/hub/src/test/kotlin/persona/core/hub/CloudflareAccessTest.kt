package persona.core.hub

import java.net.InetAddress
import java.security.MessageDigest
import java.util.Base64
import kotlinx.coroutines.runBlocking
import okhttp3.HttpUrl.Companion.toHttpUrl
import okhttp3.Request
import okhttp3.mockwebserver.MockResponse
import okhttp3.mockwebserver.MockWebServer
import okhttp3.tls.HandshakeCertificates
import okhttp3.tls.HeldCertificate
import org.junit.Assert.*
import org.junit.Test
import persona.core.contracts.Endpoint
import persona.core.contracts.api.HubAccess
import persona.core.contracts.api.HubTunnelInfo
import persona.core.contracts.api.PairingRequest

/** A Cloudflare Access service token goes to its paired hub only, and Access answering instead of the hub is named (#3502). */
class CloudflareAccessTest {
    private val certificate = HeldCertificate.Builder().commonName("example.com").build()
    private val tls = HandshakeCertificates.Builder().heldCertificate(certificate).build()
    private val pin = Base64.getUrlEncoder().withoutPadding()
        .encodeToString(MessageDigest.getInstance("SHA-256").digest(certificate.certificate.publicKey.encoded))
    private val token = HubAccess("client.access", "s3cret-value")
    private val health = MockResponse().setBody("""{"ok":true,"hub_id":"test-hub"}""")

    private fun server() = MockWebServer().apply {
        useHttps(tls.sslSocketFactory(), false)
        start(InetAddress.getLoopbackAddress(), 0)
    }

    private fun MockWebServer.address() = url("/").newBuilder().host(InetAddress.getLoopbackAddress().hostAddress!!).build().toString()

    @Test fun pairingSendsTheTokenToThePairedOriginOnly() = runBlocking {
        server().use { hub ->
            server().use { other ->
                hub.enqueue(health)
                other.enqueue(health)
                HubPairingClient("test-hub", pin, listOf(hub.address()), "test", token).use { client ->
                    client.probe()
                    val sent = hub.takeRequest()
                    assertEquals("client.access", sent.getHeader(HubAccess.ClientIdHeader))
                    assertEquals("s3cret-value", sent.getHeader(HubAccess.ClientSecretHeader))
                    // WebSocket upgrades go through the same interceptor.
                    hub.enqueue(MockResponse().withWebSocketUpgrade(object : okhttp3.WebSocketListener() {}))
                    client.http.client.newWebSocket(Request.Builder().url(hub.address().toHttpUrl().hubUrl("/api/v1/ws")).build(),
                        object : okhttp3.WebSocketListener() {})
                    assertEquals("s3cret-value", hub.takeRequest().getHeader(HubAccess.ClientSecretHeader))
                    // The same client, the same trusted certificate, another origin: nothing.
                    client.http.client.newCall(Request.Builder().url(other.address().toHttpUrl().hubUrl("/api/v1/health")).build())
                        .awaitResponse().close()
                    val elsewhere = other.takeRequest()
                    assertNull(elsewhere.getHeader(HubAccess.ClientIdHeader))
                    assertNull(elsewhere.getHeader(HubAccess.ClientSecretHeader))
                }
            }
        }
    }

    @Test fun connectionSendsEachPairedHubsTokenToItsOwnCandidatesOnly() {
        val home = "https://hub.example.com"
        val member = "https://member.example.com:8443"
        val mesh = "https://mesh.example.com"
        val tokens = mapOf("home" to token, "member" to HubAccess("member.access", "other-secret"), "mesh" to HubAccess("x", "y"))
        OkHttpHubConnection(
            HubConnectionConfig("home", null, "t", listOf(Endpoint(home, "name"), Endpoint("http://hub.local:18660", "lan")), "test",
                members = listOf(HubMember("member", null, "own", listOf(Endpoint(member, "name"))),
                    HubMember("mesh", null, null, listOf(Endpoint(mesh, "name"))))),
            HubNetwork("offline", available = false), access = tokens::get,
        ).use { connection ->
            val access = connection.http.access
            assertEquals(token, access("$home/api/v1/ws".toHttpUrl()))
            assertEquals(tokens["member"], access("$member/api/v1/machines".toHttpUrl()))
            // A hub of the mesh is not paired here; another port, plain HTTP and third parties are other origins.
            assertNull(access("$mesh/api/v1/health".toHttpUrl()))
            assertNull(access("https://hub.example.com:8443/api/v1/health".toHttpUrl()))
            assertNull(access("http://hub.local:18660/api/v1/health".toHttpUrl()))
            assertNull(access("https://team.cloudflareaccess.com/cdn-cgi/access/login/hub.example.com".toHttpUrl()))
        }
    }

    @Test fun redirectToTheAccessLoginIsNotFollowedAndIsNamed() = runBlocking {
        server().use { hub ->
            val login = MockResponse().setResponseCode(302)
                .setHeader("Location", "https://team.cloudflareaccess.com/cdn-cgi/access/login/hub.example.com?kid=1")
            hub.enqueue(login)
            hub.enqueue(login)
            HubHttpClient("test-hub", pin, "test").use { client ->
                val endpoint = Endpoint(hub.address(), "lan")
                assertEquals(HubError.AccessRequired, runCatching { client.health(endpoint) }.exceptionOrNull()?.let { HubFailure.from(it).kind })
                client.access = { token }
                assertEquals(HubError.AccessRejected, runCatching { client.health(endpoint) }.exceptionOrNull()?.let { HubFailure.from(it).kind })
            }
            assertEquals(2, hub.requestCount)
        }
    }

    @Test fun accessRefusalsAreToldFromTheHubsOwn() = runBlocking {
        server().use { hub ->
            hub.enqueue(MockResponse().setResponseCode(403).setHeader("Server", "cloudflare").setHeader("Content-Type", "text/html")
                .setBody("<html><title>Forbidden</title><p>Cloudflare Access: you don't have access.</p></html>"))
            hub.enqueue(MockResponse().setResponseCode(302).setHeader("Location", "/cdn-cgi/access/login/hub.example.com"))
            // The hub's own refusals are JSON: never Access's.
            hub.enqueue(MockResponse().setResponseCode(403).setHeader("Server", "cloudflare").setHeader("Content-Type", "application/json")
                .setBody("""{"error":"Cloudflare Access is not the hub","code":"forbidden"}"""))
            HubHttpClient("test-hub", pin, "test").use { client ->
                val endpoint = Endpoint(hub.address(), "lan")
                suspend fun kind() = runCatching { client.health(endpoint) }.exceptionOrNull()?.let { HubFailure.from(it).kind }
                assertEquals(HubError.AccessRequired, kind())
                assertEquals(HubError.AccessRequired, kind())
                assertEquals(HubError.Server, kind())
            }
        }
    }

    @Test fun theSecretIsNeverPrinted() {
        val texts = listOf(token.toString(), PairingRequest("https://hub.example.com", "code", access = token).toString(),
            HubTunnelInfo("hub", "Hub", "https://hub.example.com", access = token).toString(),
            HubFailure(HubError.AccessRejected, status = 403).toString())
        texts.forEach { assertFalse(it, it.contains("s3cret-value") || it.contains("client.access")) }
        assertNull(HubAccess.of(" id ", "  "))
        assertEquals(HubAccess("id", "secret"), HubAccess.of(" id ", " secret "))
    }
}
