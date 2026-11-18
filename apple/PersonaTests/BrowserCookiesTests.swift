import Foundation
import Testing
@testable import Persona
import PersonaWire

/// The cookie file reader (Shared/BrowserCookies.swift) on the cases the web's lib/cookies.test.ts holds lib/cookies.ts
/// to (#1304): both clients read a file the same way.
struct BrowserCookiesTests {
    @Test func readsNetscapeCookiesTxt() throws {
        let txt = ["# Netscape HTTP Cookie File", "", ".example.com\tTRUE\t/\tTRUE\t2000000000\tsid\tabc\tdef",
                   "#HttpOnly_www.example.org\tFALSE\t/app\tFALSE\t0\tpref\tx", "broken line"].joined(separator: "\n")
        let r = try CookieFile.parse(txt)
        #expect(r.bad == 1)
        #expect(r.cookies == [
            BrowserCookie(name: "sid", value: "abc\tdef", domain: ".example.com", path: "/", expires: 2_000_000_000, secure: true, httpOnly: false, hostOnly: false),
            BrowserCookie(name: "pref", value: "x", domain: "www.example.org", path: "/app", expires: nil, secure: false, httpOnly: true, hostOnly: true),
        ])
    }

    @Test func readsExtensionJSON() throws {
        let json = """
        [{"domain":".example.com","expirationDate":2000000000.5,"hostOnly":false,"httpOnly":true,"name":"sid","path":"/","sameSite":"lax","secure":true,"session":false,"value":"v"},
         {"domain":"shop.example.com","hostOnly":true,"name":"cart","path":"/","sameSite":"no_restriction","secure":true,"session":true,"value":"1"},
         {"domain":"x.test","expires":2000000000000,"name":"ms","value":"1"},
         {"name":"no domain","value":"1"}]
        """
        let r = try CookieFile.parse(json)
        #expect(r.bad == 1)
        #expect(r.cookies[0].expires == 2_000_000_000 && r.cookies[0].httpOnly == true && r.cookies[0].sameSite == "Lax" && r.cookies[0].hostOnly == false)
        #expect(r.cookies[1].expires == nil && r.cookies[1].sameSite == "None" && r.cookies[1].hostOnly == true)
        #expect(r.cookies[2].expires == 2_000_000_000)
        #expect(try CookieFile.parse(#"{"cookies":[{"name":"a","value":"b","domain":"c.d"}]}"#).cookies.count == 1)
    }

    @Test func explainsWhatIsNoCookieFile() {
        #expect(throws: CookieFile.Failure.self) { try CookieFile.parse("{oops") }
        #expect(throws: CookieFile.Failure.self) { try CookieFile.parse("[]") }
        #expect(throws: CookieFile.Failure.self) { try CookieFile.parse("hello\nworld") }
    }

    @Test func groupsBySiteAndLeavesTheExpiredOut() {
        #expect([".www.example.com", "mail.example.co.uk", "a.b.example.com.cn", "localhost", "198.51.100.7"].map(CookieFile.site(of:))
            == ["example.com", "example.co.uk", "example.com.cn", "localhost", "198.51.100.7"])
        let sites = CookieFile.sites([
            BrowserCookie(name: "a", value: "1", domain: ".example.com", expires: 2_000_000_000),
            BrowserCookie(name: "b", value: "2", domain: "www.example.com"),
            BrowserCookie(name: "old", value: "3", domain: "example.com", expires: 1_000),
            BrowserCookie(name: "c", value: "4", domain: "shop.example.org", expires: 1_950_000_000),
        ], now: Date(timeIntervalSince1970: 1_900_000_000))
        #expect(sites.map(\.site) == ["example.com", "example.org"])
        #expect(sites[0].cookies.count == 2 && sites[0].expired == 1 && sites[0].expires == 2_000_000_000)
    }

    @Test func sendsTheHubItsWireNames() throws {
        let data = try JSONEncoder().encode(BrowserCookie(name: "a", value: "b", domain: "c.d", httpOnly: true, sameSite: "Lax", hostOnly: true))
        let s = String(decoding: data, as: UTF8.self)
        #expect(s.contains(#""http_only":true"#) && s.contains(#""same_site":"Lax""#) && s.contains(#""host_only":true"#))
    }

    // MARK: A computer's sites on its page (#5821), as the web's lib/cookies.test.ts holds siteList / siteSummary

    private static let now = Date(timeIntervalSince1970: 1_794_470_400) // 2026-11-12 08:00 UTC
    private static let sites = [
        BrowserCookieSite(domain: "maple.example", count: 2, expires: now.addingTimeInterval(50 * 86_400)),
        BrowserCookieSite(domain: "aspen.example", count: 1),
        BrowserCookieSite(domain: "cedar.example", count: 4, expires: now.addingTimeInterval(-11 * 86_400)),
        BrowserCookieSite(domain: "birch.example", count: 3, expires: now.addingTimeInterval(19 * 86_400)),
    ]

    @Test func listsByNameOrTheSoonestToExpireFirst() {
        #expect(BrowserSiteList.shown(Self.sites, query: "", order: .name).map(\.domain) == ["aspen.example", "birch.example", "cedar.example", "maple.example"])
        #expect(BrowserSiteList.shown(Self.sites, query: "", order: .expiry).map(\.domain) == ["cedar.example", "birch.example", "maple.example", "aspen.example"])
    }

    @Test func filtersByDomainInAnyCase() {
        #expect(BrowserSiteList.shown(Self.sites, query: " AR ", order: .name).map(\.domain) == ["cedar.example"])
        #expect(BrowserSiteList.shown(Self.sites, query: "example", order: .name).count == 4)
        #expect(BrowserSiteList.shown(Self.sites, query: "nothing", order: .name).isEmpty)
    }

    @Test func sumsUpTheFirstExpiryStillToComeAndTheExpired() {
        let s = BrowserSiteList.summary(Self.sites, now: Self.now)
        #expect(s == BrowserSiteList.Summary(count: 4, first: Self.now.addingTimeInterval(19 * 86_400), expired: 1))
        #expect(BrowserSiteList.summary([BrowserCookieSite(domain: "a.example", count: 1)], now: Self.now) == BrowserSiteList.Summary(count: 1, first: nil, expired: 0))
        #expect(Self.sites[2].expired(at: Self.now) && !Self.sites[1].expired(at: Self.now) && !Self.sites[0].expired(at: Self.now))
        #expect(BrowserSiteList.inPlace == 3)
    }
}
