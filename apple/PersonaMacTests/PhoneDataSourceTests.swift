import Testing
@testable import Persona

/// Settings › Phone Data on the Mac: the sources the Mac has tools for. A Mac has no health data (no health tools are
/// registered there), so no health switch either.
@MainActor
struct PhoneDataSourceTests {
    @Test func noHealthOnTheMac() {
        #expect(!PhoneDataSource.onThisDevice.contains(.health))
        #expect(PhoneDataSource.onThisDevice == PhoneDataSource.allCases.filter { $0 != .health && !PhoneDataSource.iPhoneOnly.contains($0) })
    }
}
