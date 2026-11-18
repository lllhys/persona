// swift-tools-version: 6.2
// Persona Input (#7775): the live screen's pixel scrolling helper, an app bundle inside Persona Link.app whose
// Accessibility permission (posting scroll events, and moving windows for Phone Screen, #9314) is its own. A package of
// its own, apart from Persona Screen's (../Package.swift), so building one never changes the other's binary and with it
// a permission the user gave.
// mac-link.sh builds it only when its sources change, and from no path-dependent input: Swift 6 language mode (#file
// is the module and file name) and no debug map in the executable.
import PackageDescription

let package = Package(
    name: "PersonaInput",
    platforms: [.macOS(.v15)],
    dependencies: [
        .package(path: "../../Packages/PersonaInputKit")
    ],
    targets: [
        // The private CGVirtualDisplay API's declarations, for Phone Screen (#9314).
        .target(name: "CGVirtualDisplayBridge", path: "Sources/CGVirtualDisplayBridge"),
        .executableTarget(name: "PersonaInput", dependencies: [.product(name: "PersonaInputKit", package: "PersonaInputKit"),
                                                               "CGVirtualDisplayBridge"],
                          path: "Sources/PersonaInput",
                          linkerSettings: [.unsafeFlags(["-Xlinker", "-S"])])
    ]
)
