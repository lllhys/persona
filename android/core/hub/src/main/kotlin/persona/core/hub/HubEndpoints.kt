package persona.core.hub

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.Endpoint
import persona.core.contracts.api.EndpointCheck
import persona.core.contracts.api.EndpointOverrides
import java.net.Inet6Address
import java.net.InetAddress
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull

/** At most this many candidates can be added by hand per pairing. */
const val MAX_MANUAL_ENDPOINTS = 10

/**
 * The key every candidate comparison uses (dedupe, disabled, the user's order, the active route):
 * `scheme://host:port`, lowercase, explicit port, IPv6 in brackets, no trailing slash. Pairing addresses are stored as
 * `https://host:port/` while the hub sends `https://host:port`; both get the same key. A hub behind a reverse proxy at
 * a sub-path keeps it (`https://host:443/persona`): another prefix on the same host is another endpoint.
 */
fun endpointKey(url: String): String = url.trim().toHttpUrlOrNull()?.key() ?: url.trim().lowercase()

private fun HttpUrl.key() = "$scheme://${if (':' in host) "[$host]" else host}:$port" + encodedPath.trimEnd('/')

/**
 * `lan` for localhost, `*.local` and IP literals [policy] treats as local; `ipv6` / `ipv4` for other literals;
 * `name` for other hosts; `unknown` when [url] is not an http(s) URL. Never touches DNS.
 */
fun endpointKind(url: String, policy: LocalAddressPolicy = LocalAddressPolicy()): String {
    val host = url.trim().toHttpUrlOrNull()?.host ?: return "unknown"
    val literal = literalAddress(host)
    return when {
        host == "localhost" || host.endsWith(".local") || literal?.let(policy::allows) == true -> "lan"
        literal is Inet6Address -> "ipv6"
        literal != null -> "ipv4"
        else -> "name"
    }
}

/** The address of an IP literal host (IPv6 with or without brackets, strict dotted-quad IPv4); null for names. */
internal fun literalAddress(host: String): InetAddress? {
    val bare = host.removePrefix("[").removeSuffix("]")
    if (':' in bare) {
        if (!bare.all { it.isHex() || it == ':' || it == '.' }) return null
        // OkHttp's own parser decides validity; its canonical form is a literal getByName parses without DNS.
        val canonical = "http://[$bare]/".toHttpUrlOrNull()?.host ?: return null
        return runCatching { InetAddress.getByName(canonical) }.getOrNull()
    }
    return dottedQuad(bare)?.let(InetAddress::getByAddress)
}

private fun Char.isHex() = this in '0'..'9' || this in 'a'..'f' || this in 'A'..'F'

/** Four decimal parts 0–255 without leading zeros (`010.0.0.1` is octal to some parsers, so it is refused). */
private fun dottedQuad(text: String): ByteArray? {
    val parts = text.split('.')
    if (parts.size != 4) return null
    val bytes = ByteArray(4)
    for ((index, part) in parts.withIndex()) {
        if (part.isEmpty() || part.length > 3 || !part.all { it in '0'..'9' } || part.length > 1 && part[0] == '0') return null
        val value = part.toInt()
        if (value > 255) return null
        bytes[index] = value.toByte()
    }
    return bytes
}

/** A candidate typed on this phone, stored in its canonical form. */
fun manualEndpoint(url: String, policy: LocalAddressPolicy = LocalAddressPolicy()): Endpoint {
    val parsed = url.toHttpUrlOrNull()
    return Endpoint(url, endpointKind(url, policy), parsed?.port, parsed?.scheme)
}

/** Manual candidates (oldest first), then [base] in its order, deduplicated by key: a manual one wins. */
fun mergeEndpoints(manual: List<String>, base: List<Endpoint>, policy: LocalAddressPolicy = LocalAddressPolicy()): List<Endpoint> =
    (manual.map { manualEndpoint(it, policy) } + base).distinctBy { endpointKey(it.url) }

/**
 * The default order, used until the user arranges one: by kind (ipv6, name, lan, ipv4, anything else), https before
 * http, [manual] keys before the pairing's and the hub's, then as given. A duplicate key keeps its first spelling.
 */
fun orderEndpoints(candidates: List<Endpoint>, manual: Set<String> = emptySet()): List<Endpoint> =
    candidates.map { endpointKey(it.url) to it }.distinctBy { it.first }.sortedWith(
        compareBy<Pair<String, Endpoint>> { (_, endpoint) -> kindRank(endpoint.kind) }
            .thenBy { (key, _) -> if (key.startsWith("https://")) 0 else 1 }
            .thenBy { (key, _) -> if (key in manual) 0 else 1 },
    ).map { it.second }

/**
 * Every candidate in the one order the connection tries them and settings lists them; disabled ones keep their place
 * (the connection skips them). With [EndpointOverrides.order] the user's order wins: keys that are no longer candidates
 * are skipped, and candidates it does not list (new ones from the hub) follow in the default order. Without it, the
 * default order ([orderEndpoints]).
 */
fun arrangeEndpoints(candidates: List<Endpoint>, overrides: EndpointOverrides): List<Endpoint> {
    val manual = overrides.manual.mapTo(HashSet(), ::endpointKey)
    val order = overrides.order ?: return orderEndpoints(candidates, manual)
    val byKey = LinkedHashMap<String, Endpoint>()
    candidates.forEach { byKey.putIfAbsent(endpointKey(it.url), it) }
    val listed = order.distinct().mapNotNull(byKey::get)
    val listedKeys = listed.mapTo(HashSet()) { endpointKey(it.url) }
    return listed + orderEndpoints(byKey.values.filter { endpointKey(it.url) !in listedKeys }, manual)
}

private fun kindRank(kind: String) = when (kind) {
    "ipv6" -> 0
    "name" -> 1
    "lan" -> 2
    "ipv4" -> 3
    else -> 4
}

/** What a probe or a settings test found; null is a candidate that passed the identity check. */
fun endpointResultLabel(error: HubError?): CoreText = CoreText.of(when (error) {
    null -> Key.EndpointVerified
    HubError.AddressNotFound -> Key.EndpointAddressNotFound
    HubError.ConnectionRefused -> Key.EndpointRefused
    HubError.Unreachable -> Key.EndpointUnreachable
    HubError.Timeout -> Key.EndpointTimeout
    HubError.CertificateMismatch -> Key.EndpointCertificateMismatch
    HubError.HubMismatch -> Key.EndpointHubMismatch
    HubError.TlsRequired -> Key.EndpointTlsRequired
    HubError.NoNetwork -> Key.EndpointNoNetwork
    HubError.Server -> Key.EndpointServer
    HubError.Protocol -> Key.EndpointProtocol
    HubError.PairingExpired -> Key.EndpointRejected
    HubError.RateLimited -> Key.EndpointRateLimited
    HubError.AccessRequired -> Key.EndpointAccessRequired
    HubError.AccessRejected -> Key.EndpointAccessRejected
    else -> Key.EndpointFailed
})

/**
 * Validates an address typed in settings without DNS or network. [taken] holds the keys of every current candidate
 * (disabled ones too), [manualCount] how many were added by hand; [replacingKey] is the manual candidate being edited.
 */
fun checkEndpoint(
    input: String, pinned: Boolean, policy: LocalAddressPolicy, taken: Set<String>, manualCount: Int,
    replacingKey: String? = null,
): EndpointCheck {
    fun invalid(key: Key, vararg args: Any) = EndpointCheck.Invalid(CoreText(key, args.toList()))
    if (input.isBlank()) return invalid(Key.AddressEmpty)
    val text = input.trim()
    if (text.any { it.isWhitespace() }) return invalid(Key.AddressSpaces)
    val separator = text.indexOf("://")
    val scheme = if (separator < 0) "https" else text.substring(0, separator).lowercase()
    if (scheme != "https" && scheme != "http") return invalid(Key.AddressScheme)
    val rest = if (separator < 0) text else text.substring(separator + 3)
    val end = rest.indexOfFirst { it == '/' || it == '?' || it == '#' || it == '\\' }.let { if (it < 0) rest.length else it }
    val authority = rest.substring(0, end)
    if ('@' in authority) return invalid(Key.AddressUserInfo)
    if (rest.substring(end).let { it.isNotEmpty() && it != "/" }) return invalid(Key.AddressPath)

    val missingPort = Key.AddressMissingPort
    val host: String
    val port: String?
    if (authority.startsWith("[")) {
        val close = authority.indexOf(']')
        if (close < 0) return invalid(Key.AddressIpv6Bracket)
        host = authority.substring(1, close)
        val after = authority.substring(close + 1)
        port = if (after.isEmpty()) null else if (after.startsWith(":")) after.substring(1) else return invalid(missingPort)
        if (host.isEmpty()) return invalid(Key.AddressMissingHost)
        if ('%' in host) return invalid(Key.AddressLinkLocal)
        if (':' !in host || literalAddress(host) == null) return invalid(Key.AddressIpv6Invalid)
    } else {
        if (authority.count { it == ':' } > 1) return invalid(Key.AddressIpv6NeedsBrackets)
        val colon = authority.indexOf(':')
        host = if (colon < 0) authority else authority.substring(0, colon)
        port = if (colon < 0) null else authority.substring(colon + 1)
        if (host.isEmpty()) return invalid(Key.AddressMissingHost)
        if (host.all { it in '0'..'9' || it == '.' } && dottedQuad(host) == null) return invalid(Key.AddressIpv4Invalid)
        // No percent-escapes: OkHttp would decode them into a different host than the one shown.
        if ('%' in host || "http://$host/".toHttpUrlOrNull() == null) return invalid(Key.AddressHostInvalid)
    }
    if (port.isNullOrEmpty()) return invalid(missingPort)
    val number = port.takeIf { it.all { char -> char in '0'..'9' } }?.toIntOrNull()?.takeIf { it in 1..65535 }
        ?: return invalid(Key.AddressPortRange)

    // Runs on every keystroke: never throw, whatever was typed.
    val url = runCatching { HttpUrl.Builder().scheme(scheme).host(host).port(number).build() }.getOrNull()
        ?: return invalid(Key.AddressHostInvalid)
    val literal = literalAddress(url.host)
    if (literal != null && (literal.isAnyLocalAddress || literal.isMulticastAddress ||
            literal.address.size == 4 && literal.address.all { it == 0xff.toByte() })) {
        return invalid(Key.AddressNotConnectable)
    }
    if (scheme == "https" && !pinned) {
        return invalid(Key.AddressNoFingerprint)
    }
    // The same rule as HubHttpClient's guard, which still checks the resolved peer at runtime.
    if (scheme == "http" && !(literal?.let(policy::allows) ?: (url.host == "localhost" || url.host.endsWith(".local")))) {
        return invalid(Key.AddressHttpPublic)
    }
    val key = url.key()
    if (key in taken && key != replacingKey) return invalid(Key.AddressDuplicate)
    if (replacingKey == null && manualCount >= MAX_MANUAL_ENDPOINTS) return invalid(Key.AddressTooMany, MAX_MANUAL_ENDPOINTS)
    return EndpointCheck.Valid(key, endpointKind(key, policy))
}
