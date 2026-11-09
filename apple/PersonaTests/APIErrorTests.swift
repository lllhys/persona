import Foundation
import Testing
@testable import Persona

struct APIErrorTests {
    @Test func pairingAcceptsOnlyPersonaSchemeIgnoringCase() throws {
        for scheme in ["persona", "PERSONA"] {
            let config = try #require(ServerConfig(pairURL: "\(scheme)://pair?url=https%3A%2F%2Fhub.example.com&token=fixture"))
            #expect(config.url.absoluteString == "https://hub.example.com")
            #expect(config.token == "fixture")
        }
        for other in ["other-product", "personas", "personad"] {
            let link = "\(other)://pair?url=https%3A%2F%2Fhub.example.com&token=fixture"
            #expect(ServerConfig(pairURL: link) == nil)
            #expect(!ServerConfig.isPairLink(link))
            #expect(!ServerConfig.isComputerLink(URL(string: "\(other)://link?token=fixture")!))
        }
        #expect(ServerConfig(pairURL: "https://pair?url=https%3A%2F%2Fhub.example.com&token=fixture") == nil)
        #expect(ServerConfig(pairURL: "persona://pair?url=https%3A%2F%2Fhub.example.com&token=") == nil)
        // A computer's link (「添加电脑」, Add computer) is for Persona Link on that computer: this device never pairs with it (#4198).
        for scheme in ["persona", "PERSONA"] {
            let link = "\(scheme)://link?url=https%3A%2F%2Fhub.example.com&token=fixture&machine=macbook"
            #expect(ServerConfig(pairURL: link) == nil)
            #expect(ServerConfig.isComputerLink(URL(string: link)!))
        }
        #expect(!ServerConfig.isComputerLink(URL(string: "persona://pair?url=https%3A%2F%2Fhub.example.com&token=t")!))
    }
    /// A pairing link cut short is still told apart from other links, so opening it says so instead of nothing (#4273).
    @Test func anIncompletePairingLinkIsStillAPairingLink() {
        for link in ["persona://pair?url=https%3A%2F%2Fhub.example.com", "PERSONA://PAIR?token=fixture", "persona://pair", " persona://pair?url= \n"] {
            #expect(ServerConfig(pairURL: link) == nil)
            #expect(ServerConfig.isPairLink(link), "\(link)")
        }
        for other in ["persona://open?machine=m&session=s", "persona://extension/x?group=g", "https://pair?url=x&token=t", "pair", ""] {
            #expect(!ServerConfig.isPairLink(other), "\(other)")
        }
        #expect(ServerConfig.isPairLink("persona://pair?url=https%3A%2F%2Fhub.example.com&token=fixture"))
    }
    /// An id the caller escaped (a Knowledge project slug with Chinese in it) reaches the hub once escaped, not twice (404).
    @MainActor @Test func escapedIdsAreNotEscapedAgain() throws {
        let cfg = try #require(ServerConfig(pairURL: "persona://pair?url=http%3A%2F%2Fhub.example.com%3A18660&token=t"))
        let api = APIClient(config: cfg)
        let id = "gsb-打分平台".addingPercentEncoding(withAllowedCharacters: .urlPathAllowed.subtracting(["/"]))!
        #expect(api.url("/api/v1/kb/projects/\(id)").absoluteString
                == "http://hub.example.com:18660/api/v1/kb/projects/gsb-%E6%89%93%E5%88%86%E5%B9%B3%E5%8F%B0")
        #expect(api.url("/api/v1/kb/projects/a%2Fb").path(percentEncoded: true) == "/api/v1/kb/projects/a%2Fb")
        // Raw paths are still escaped, and queries still work.
        #expect(api.url("/api/v1/files/a b", query: ["q": "x y"]).absoluteString
                == "http://hub.example.com:18660/api/v1/files/a%20b?q=x%20y")
    }

    @Test func serverErrorsNameTheRequest() {
        let e = APIError(status: 502, message: "node exited", requestID: "1a2b3c4d")
        #expect(e.errorDescription == "node exited（请求 1a2b3c4d）")
    }

    @Test func answersAreShownAsTheyAre() {
        // A refusal or a missing item is the hub's answer, not a failure to look up.
        let e = APIError(status: 409, message: "电脑上的输入框里有字", fields: ["code": "input_draft"], requestID: "1a2b3c4d")
        #expect(e.errorDescription == "电脑上的输入框里有字")
        #expect(e.code == "input_draft")
    }

    @Test func readsStringFields() {
        let data = Data(#"{"error":"x","code":"input_draft","delivered":"none","n":3}"#.utf8)
        let f = APIError.fields(in: data)
        #expect(f["code"] == "input_draft" && f["delivered"] == "none" && f["error"] == "x")
    }

    /// The hub's error body in every shape: the text beside other fields (some add non-string ones such as `retryable`),
    /// the plain and object shapes, and nil without JSON or an error text.
    @Test(arguments: [
        (body: #"{"error":"todo not found","code":"not_found","retryable":false}"#, expected: "todo not found" as String?),
        (body: #"{"error":"x","code":"input_draft","delivered":"none","n":3}"#, expected: "x"),
        (body: #"{"error":"plain"}"#, expected: "plain"),
        (body: #"{"error":{"code":"99991663","message":"token invalid"}}"#, expected: "token invalid（99991663）"),
        (body: "404 page not found\n", expected: nil),
        (body: #"{"code":"not_found","retryable":false}"#, expected: nil),
        (body: #"{"error":true}"#, expected: nil),
    ])
    func messageReadsEveryErrorBodyShape(body: String, expected: String?) {
        #expect(APIError.message(in: Data(body.utf8)) == expected, "\(body)")
    }
}
