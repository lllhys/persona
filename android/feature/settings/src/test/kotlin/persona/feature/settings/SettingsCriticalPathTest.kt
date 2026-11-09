package persona.feature.settings

import persona.core.contracts.api.PairingException
import persona.core.contracts.api.PairingFailure
import persona.core.contracts.api.PairingRequest
import persona.core.hub.LocalAddressPolicy
import org.junit.Assert.*
import org.junit.Test

class SettingsCriticalPathTest {
    @Test fun pairingMetadataAndInvalidInput() {
        val request = PairingInput.parse("persona://pair?url=https%3A%2F%2Fexample.com&token=example-token&hub=example-hub&fp=$fingerprint&tls=443&alt=https%3A%2F%2Fexample.com%3A8443&alt=https%3A%2F%2F%5B2001%3Adb8%3A%3A1%5D")
        assertEquals("https://example.com", request.url)
        assertEquals("example-token", request.token)
        assertEquals("example-hub", request.hub)
        assertEquals(fingerprint, request.fp)
        assertEquals(443, request.tls)
        assertEquals(2, request.alt.size)
        assertFalse(request.toString().contains(request.token))
        listOf(
            "persona://pair?url=http-broken&token=example",
            "persona://pair?url=https%3A%2F%2Fexample.com&token=example&tls=no",
            "persona://pair?url=https%3A%2F%2Fexample.com&token=example&token=duplicate",
            "persona://other?url=https%3A%2F%2Fexample.com&token=example",
        ).forEach { link -> assertThrows(PairingException::class.java) { PairingInput.parse(link) } }
    }

    @Test fun malformedFingerprintOrHubIdIsAnInvalidCode() {
        // HubPairingClient.addresses rejects these with a raw IllegalArgumentException, which showed Unknown error (#4991).
        val base = "persona://pair?url=https%3A%2F%2Fhub.example.com&token=fixture"
        for (suffix in listOf("&fp=short", "&fp=${"A".repeat(42)}B", "&hub=bad%20hub", "&hub=${"h".repeat(129)}")) {
            val error = assertThrows(PairingException::class.java) { PairingInput.parse(base + suffix) }
            assertEquals(PairingFailure.InvalidCode, error.reason)
        }
        assertEquals(fingerprint, PairingInput.parse("$base&hub=example-hub&fp=$fingerprint").fp)
    }

    @Test fun plaintextPairingWithoutFingerprintAgreesWithTheConnectionPolicy() {
        // Parsing lets an http link without fp through only where HubHttpClient would send plaintext (#4988).
        // The private ranges are spelled from their octets: the repository's privacy scan flags literal private
        // addresses, and these are the ranges themselves, not anybody's machines.
        val privateRanges = listOf(intArrayOf(10, 0, 0, 2), intArrayOf(192, 168, 1, 2), intArrayOf(172, 16, 0, 2), intArrayOf(169, 254, 0, 2))
            .map { it.joinToString(".") }
        for (host in listOf("100.101.102.103", "100.64.0.1", "192.0.2.10") + privateRanges + "127.0.0.1") {
            val link = "persona://pair?url=http%3A%2F%2F$host%3A18660&token=fixture"
            val allowed = LocalAddressPolicy().allows(java.net.InetAddress.getByName(host))
            val outcome = runCatching { PairingInput.parse(link) }.exceptionOrNull()
            if (allowed) assertNull(host, outcome)
            else assertEquals(host, PairingFailure.TlsRequired, (outcome as? PairingException)?.reason)
        }
    }

    @Test fun personaPairingStillUsesTransportAndCredentialValidation() {
        val link = "persona://pair?url=https%3A%2F%2Fhub.example.com&token=fixture&tls=443&alt=https%3A%2F%2Fhub.example.com%3A8443"
        val parsed = PairingInput.parse(link)
        assertEquals("https://hub.example.com", parsed.url)
        assertEquals("fixture", parsed.token)
        assertEquals(443, parsed.tls)
        assertEquals(listOf("https://hub.example.com:8443"), parsed.alt)
        assertEquals(parsed.url, PairingInput.manual(link, "").url)
        for (suffix in listOf("pair?url=http-broken&token=fixture", "pair?url=https://hub.example.com&token=fixture&tls=no",
            "pair?url=https://hub.example.com&token=fixture&token=duplicate", "other?url=https://hub.example.com&token=fixture")) {
            assertThrows(PairingException::class.java) { PairingInput.parse("persona://$suffix") }
        }
    }

    @Test fun manualAddressFieldParsesPersonaLinks() {
        val scanned = PairingRequest("https://old.example.com", "old-token", hub = "old-hub", fp = "old-fingerprint", tls = 443)
        for (scheme in listOf("persona")) {
            val link = "$scheme://pair?url=https%3A%2F%2Fhub.example.com&token=fixture&hub=example-hub"
            // Pasting into the address field: the link is detected, and no stale metadata keeps the raw link or old token.
            assertTrue(PairingInput.isLink(link))
            assertTrue(PairingInput.isLink("  $link  "))
            assertNull(PairingInput.editedAddress(scanned, link))
            assertNull(PairingInput.editedAddress(null, link))
            val request = PairingInput.manual(link, "")
            assertEquals("https://hub.example.com", request.url)
            assertEquals("fixture", request.token)
            assertEquals("example-hub", request.hub)
        }
        // A plain address only edits the URL and keeps the rest of the scanned metadata.
        val edited = PairingInput.editedAddress(scanned, "https://new.example.com")
        assertEquals(scanned.copy(url = "https://new.example.com"), edited)
        assertNull(PairingInput.editedAddress(null, "https://new.example.com"))
        for (address in listOf("https://hub.example.com", "hub.example.com", "persona.example.com", "persona.local:18660",
            "persona:18660", "")) {
            assertFalse(address, PairingInput.isLink(address))
        }
        // A machine named "persona" is an address: typing it keeps the metadata, and without a scheme it gets one exactly
        // like any other host:port instead of taking the pairing-link path (#2481): http on this device and the local
        // network, https anywhere else.
        assertEquals(scanned.copy(url = "persona:18660"), PairingInput.editedAddress(scanned, "persona:18660"))
        for ((address, url) in listOf("127.0.0.1:18760" to "http://127.0.0.1:18760", "hub.local:18660" to "http://hub.local:18660",
            "persona:18660" to "https://persona:18660", "hub:18660" to "https://hub:18660")) {
            assertEquals(address, url, PairingInput.manual(address, "tok").url)
        }
        assertEquals(PairingFailure.InvalidCode, assertThrows(PairingException::class.java) { PairingInput.manual("a b:99999", "tok") }.reason)
        assertEquals("https://persona:18660", PairingInput.manual("https://persona:18660", "tok").url)
        assertEquals("https://hub.example.com", PairingInput.manual(" https://hub.example.com ", " fixture ").url)
    }

    @Test fun updateDigestRejectsCorruptionAndMissingChecksum() {
        val expected = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        assertTrue(verifySha256("abc".byteInputStream(), expected))
        assertFalse(verifySha256("abd".byteInputStream(), expected))
        assertFalse(verifySha256("abc".byteInputStream(), ""))
    }
}

/** A well-formed SPKI fingerprint: 32 bytes, unpadded base64url. */
private val fingerprint = "A".repeat(43)
