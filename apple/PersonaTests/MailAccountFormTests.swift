import Foundation
import Testing
@testable import Persona

/// Add Mail Account's form (`MailAccountForm`): the request it makes.
@MainActor struct MailAccountFormTests {
    @Test func imapOn587IsStartTLSAndEmptyHostsAreLeftToTheHub() {
        var f = MailAccountForm(kind: .imap, email: "me@example.com", password: "p")
        f.smtpHost = "smtp.example.com"
        f.smtpPort = "587"
        let r = f.request
        #expect(r?.provider == "imap" && r?.password == "p")
        #expect(r?.smtpSecurity == "starttls" && r?.smtpPort == 587)
        #expect(r?.imapHost == nil, "no host typed: the hub finds it")
        f.smtpPort = "465"
        #expect(f.request?.smtpSecurity == "tls")
    }

    @Test func gmailMakesNoPasswordRequest() {
        #expect(MailAccountForm(kind: .gmail, email: "me@example.com", password: "p").request == nil)
        #expect(MailAccountForm(kind: .outlook, email: "me@example.com", password: "p").request == nil) // signs in with Microsoft
        #expect(MailAccountForm(kind: .feishu, email: "me@example.com", password: "p").request?.provider == "feishu")
    }

    /// QQ / NetEase addresses get where to make their authorization code (#5123); other addresses and kinds get none.
    @Test func qqAndNetEaseAddressesGetTheCodeGuide() {
        var f = MailAccountForm(kind: .imap, email: "me@QQ.com")
        #expect(f.codeGuide?.provider == .qq && f.codeGuide?.webMail.absoluteString == "https://mail.qq.com")
        f.email = "me@foxmail.com"
        #expect(f.codeGuide?.provider == .qq)
        f.email = "me@126.com"
        #expect(f.codeGuide?.provider == .netease && f.codeGuide?.webMail.absoluteString == "https://mail.126.com")
        f.email = "me@vip.163.com"
        #expect(f.codeGuide?.webMail.absoluteString == "https://vip.163.com")
        f.email = "me@188.com"
        #expect(f.codeGuide?.webMail.absoluteString == "https://www.188.com")
        f.email = "me@example.com"
        #expect(f.codeGuide == nil)
        f.email = "me"
        #expect(f.codeGuide == nil)
        #expect(MailAccountForm(kind: .feishu, email: "me@qq.com").codeGuide == nil)
        f.email = "me@qq.com"
        f.imapHost = "imap.example.com"
        #expect(f.codeGuide == nil, "servers typed: they may not be QQ's")
    }

    /// Gmail and iCloud addresses get where to make their app password (#9389).
    @Test func gmailAndICloudAddressesGetTheAppPasswordGuide() {
        var f = MailAccountForm(kind: .imap, email: "me@Gmail.com")
        #expect(f.codeGuide?.provider == .gmail && f.codeGuide?.webMail.absoluteString == "https://myaccount.google.com/apppasswords")
        f.email = "me@icloud.com"
        #expect(f.codeGuide?.provider == .icloud && f.codeGuide?.webMail.absoluteString == "https://account.apple.com")
        #expect(CodeGuide.gmailAppPassword.provider == .gmail)
    }

    /// On a hub without google.client_id, Gmail is added with an app password, Google's servers spelled out so a Google
    /// Workspace address (its own domain) works too (#2442).
    @Test func gmailWithAppPasswordOnHubWithoutGoogleSignIn() {
        let req = MailAccountForm(kind: .gmail, email: "me@mail.test", password: "abcd efgh ijkl mnop", gmailAppPassword: true).request
        #expect(req?.provider == "gmail")
        #expect(req?.auth == "password")
        #expect(req?.password == "abcd efgh ijkl mnop")
        #expect(req?.imapHost == "imap.gmail.com" && req?.imapPort == 993)
        #expect(req?.smtpHost == "smtp.gmail.com" && req?.smtpPort == 465 && req?.smtpSecurity == "tls")
        #expect(MailAccountForm(kind: .outlook, email: "me@mail.test", password: "p", gmailAppPassword: true).request == nil)
    }
}
