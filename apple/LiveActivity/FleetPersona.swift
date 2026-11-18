import Foundation
import OSLog
import SwiftUI
import UIKit

/// The look's drawing and where the app leaves it for the extension (the value itself: Shared/FleetState.swift).
nonisolated extension FleetLook {
    /// Before the app told the activity anything: the default persona in the app's default accent.
    static let standard = FleetLook(design: PersonaFace.Design.mochi.rawValue, light: 0x007AFF, dark: 0x0A84FF)

    /// A design in a (dynamic) color, resolved to its light and dark values.
    init(design: PersonaFace.Design, tint: Color, package: String? = nil) {
        self.init(design: design.rawValue, light: Self.hex(tint, .light), dark: Self.hex(tint, .dark), package: package)
    }

    static func hex(_ c: Color, _ scheme: ColorScheme) -> UInt32 {
        var env = EnvironmentValues()
        env.colorScheme = scheme
        let r = c.resolve(in: env)
        func ch(_ v: Float) -> UInt32 { UInt32((max(0, min(1, v)) * 255).rounded()) }
        return ch(r.red) << 16 | ch(r.green) << 8 | ch(r.blue)
    }

    var faceDesign: PersonaFace.Design { PersonaFace.Design(rawValue: design) ?? .mochi }

    /// Packages keep their own faces; old local drawings fall back to Mochi.
    var supported: FleetLook {
        if let package {
            guard let id = package.split(separator: "/").first.map(String.init),
                  PersonaSelection.resolve(id) == id, id != PersonaSelection.builtinID else { return .standard }
            return FleetLook(design: "mochi", light: light, dark: dark, package: package)
        }
        return design == "mochi" ? self : .standard
    }


    /// What the app saved last; `standard` when nothing is readable. Cheap enough to call on every render. The widget
    /// extension tells what it read (`FleetLookReport`).
    static var saved: FleetLook {
        let store = FleetLookStore.shared
        let data = store.read(FleetLookStore.lookFile)
        let look = data.flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) }
        FleetLookReport.note { $0.saved(store.root != nil, look) }
        return look ?? standard
    }

    static func load(_ store: FleetLookStore) -> FleetLook {
        store.read(FleetLookStore.lookFile).flatMap { try? JSONDecoder().decode(FleetLook.self, from: $0) } ?? standard
    }

    /// The package's picture for a state at the size it is drawn, as saved, when this look is a package's; read on its
    /// own, the one the activity is about to draw.
    func face(_ state: PersonaState, points: CGFloat) -> (png: Data, own: Bool)? {
        guard let package else { return nil }
        let face = FleetLookStore.shared.face(package, state, points: points)
        FleetLookReport.note { $0.face(package, face.map { ($0.file, $0.png.count) }) }
        return face.map { ($0.png, $0.own) }
    }

    func faces(_ store: FleetLookStore) -> FleetFaces? {
        package.flatMap { store.faces($0) }
    }

    /// Saves it for the extension, with a package's faces (made only when the saved ones are not this package's). True
    /// when what the extension reads changed. Faces that cannot be made leave the package drawn in its color.
    @discardableResult
    static func save(_ look: FleetLook, faces: () -> FleetFaces?, to store: FleetLookStore = .shared) -> Bool {
        var changed = false
        if let package = look.package {
            // The faces before the look; a look is drawn only with faces of its own package, never another's.
            if !store.hasFaces(package) {
                if let made = faces(), made.package == package {
                    changed = store.save(made)
                } else {
                    FleetLookStore.log.error("no faces for \(package, privacy: .public): drawn in its color")
                }
            }
        } else {
            _ = store.save(nil) // a built-in: no picture of the user's left behind
        }
        if look != load(store), let data = try? JSONEncoder().encode(look), store.write(data, FleetLookStore.lookFile) {
            changed = true
        }
        return changed
    }
}

nonisolated extension FleetState {
    /// The persona to draw: the one the content carries (the app's updates, the hub's pushes), else — an older hub —
    /// what the app saved. The saved one is read either way, so the extension's report says whether it can be.
    var drawnLook: FleetLook {
        let kept = FleetLook.saved
        FleetLookReport.note { $0.content(look) }
        return (look ?? kept).supported
    }
}

/// A hub package's faces on the activity (its pictures are the user's and stay on the hub and the phone, never in the
/// repository): one picture a state, the first frame of its dot (the one the app holds still with Reduce Motion), and
/// the avatar only when a state has none. Each at every size the activity draws a face (compact, minimal / limited,
/// expanded, lock screen), points × 3 pixels: a phone may not draw a Live Activity whose picture is larger than where
/// it shows (an image decoded at 1× is as many points as it has pixels), and the simulator draws anything
/// (docs/apple/live-activity.md「Why build 98 was still wrong」). Each is a file of its own, read only when drawn.
nonisolated struct FleetFaces: Hashable, Sendable {
    /// One picture at each size: points → PNG.
    typealias Picture = [Int: Data]

    var package: String
    /// PersonaState raw value → its picture, only the states the package draws itself.
    var states: [String: Picture]
    var avatar: Picture?

    /// The sizes the activity draws a face at, in points: compact 22, minimal / limited 24, expanded and lock screen 26;
    /// and the Home Screen widgets' larger ones (`widgetSizes`).
    static let sizes = Set([IslandMetrics.face, IslandMetrics.limitedContent, IslandMetrics.expandedFace, IslandMetrics.lockFace].map { Int($0) }
                           + widgetSizes).sorted()
    #if os(iOS)
    /// The widgets' faces past the island's: the Lock Screen's 40 and the empty states' 46 (their corner's 22 is the
    /// island's), docs/apple/home-widgets.md「The persona, as an accent」.
    static let widgetSizes = [HomeWidgetTokens.faceLock, HomeWidgetTokens.faceEmpty].map { Int($0) }
    #else
    static let widgetSizes: [Int] = []
    #endif
    /// Pixels a point: every iPhone with a Dynamic Island.
    static let scale: CGFloat = 3

    /// The stored size a face of `points` reads: the smallest that is not smaller, else the largest.
    static func size(_ points: CGFloat) -> Int { sizes.first { CGFloat($0) >= points } ?? sizes[sizes.count - 1] }

    /// The picture for a state at the size it is drawn, and whether it is that state's own (else the avatar or the
    /// idle frame: the activity adds the state's cue, as it does on the built-ins).
    func face(_ state: PersonaState, points: CGFloat) -> (png: Data, own: Bool)? {
        let size = Self.size(points)
        if let own = states[state.rawValue]?[size] { return (own, true) }
        if let other = (avatar ?? states[PersonaState.idle.rawValue])?[size] { return (other, false) }
        return nil
    }

    /// A picture from the package (its 144 px dot frame, an avatar file at least as large as the largest face) made at
    /// every size; nil when one cannot be.
    static func picture(_ image: UIImage) -> Picture? {
        var out: Picture = [:]
        for size in sizes {
            guard let png = png(image, points: CGFloat(size)) else { return nil }
            out[size] = png
        }
        return out
    }

    /// Square, points × 3 on a side, scaled down with high-quality interpolation, PNG (lossless), 8 bits a channel.
    static func png(_ image: UIImage, points: CGFloat) -> Data? {
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        format.preferredRange = .standard // 8 bits a channel, whatever the screen
        let rect = CGRect(x: 0, y: 0, width: points * scale, height: points * scale)
        return UIGraphicsImageRenderer(size: rect.size, format: format).image { ctx in
            ctx.cgContext.interpolationQuality = .high
            image.draw(in: rect)
        }.pngData()
    }

    /// A picture as the activity draws it: `points` on a side, at the scale its pixels give.
    static func image(_ png: Data, points: CGFloat) -> UIImage? {
        guard let cg = UIImage(data: png)?.cgImage else { return nil }
        return UIImage(cgImage: cg, scale: CGFloat(cg.width) / points, orientation: .up)
    }
}

/// Where the app leaves the look and a package's pictures for the widget extension: files in the App Group container
/// they share (Info.plist `PersonaAppGroup`), under `Fleet/`. On a phone a Live Activity's extension cannot use the
/// keychain (build 103's Dynamic Island Diagnostics, 2026-11-02: the app wrote and read back both items, the extension never reported),
/// so nothing goes there any more. Readable on the lock screen once the phone was unlocked after boot. Tests pass a
/// directory of their own.
///
///     Fleet/look.json                    the look, for content without one (an older hub)
///     Fleet/faces/<package>/<state>_<pt>.png  a picture a state at each face size, avatar_<pt>.png for the states
///                                             without one; one package
///     Fleet/seen.json                    what the extension last read (FleetLookReport)
nonisolated struct FleetLookStore: Sendable {
    static let lookFile = "look.json"
    static let seenFile = "seen.json"
    static let facesDir = "faces"
    static let avatarName = "avatar"

    /// The island's log (Console: subsystem `persona`, category `island`): every write on either side and every read
    /// that fails, so a phone that draws the wrong persona says which step failed (docs/apple/live-activity.md「Diagnostics」).
    static let log = Logger(subsystem: "persona", category: "island")

    /// `Fleet/` in the container; nil when this bundle has none (not signed with the group: nothing is saved, and the
    /// extension draws the content's look, a package in its color).
    var root: URL?

    static var shared: FleetLookStore { FleetLookStore(root: container?.appending(path: "Fleet", directoryHint: .isDirectory)) }

    /// The group's id, `group.<bundle id>` unless the config says otherwise, in both bundles' Info.plist.
    static var group: String? {
        NativeStateMigration.infoString("PersonaAppGroup")
    }

    static var container: URL? {
        group.flatMap { FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: $0) }
    }

    func read(_ name: String) -> Data? {
        guard let url = root?.appending(path: name) else { return nil }
        do {
            return try Data(contentsOf: url)
        } catch CocoaError.fileReadNoSuchFile {
            return nil
        } catch {
            Self.log.error("read \(name, privacy: .public): \(error.localizedDescription, privacy: .public)")
            return nil
        }
    }

    /// Saves (nil deletes) a file, whole or not at all. False when it could not be.
    func write(_ data: Data?, _ name: String) -> Bool {
        let error: Error?
        do {
            guard let root else { throw CocoaError(.fileNoSuchFile) }
            let url = root.appending(path: name)
            if let data {
                try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
                try data.write(to: url, options: [.atomic, .completeFileProtectionUntilFirstUserAuthentication])
            } else if FileManager.default.fileExists(atPath: url.path(percentEncoded: false)) {
                try FileManager.default.removeItem(at: url)
            }
            error = nil
        } catch let e {
            error = e
        }
        Self.wrote(name, error, bytes: data?.count)
        return error == nil
    }

    /// The package's directory: its id and rev with anything but letters, digits, `.`, `-` and `_` as `_`, and `_3xw`
    /// (a picture at each face size, the widgets' too: the ones an older build made, all at 4 KB (#349) or without the
    /// widgets' sizes (`_3x`), are made again).
    static func folder(_ package: String) -> String {
        String(package.map { $0.isASCII && ($0.isLetter || $0.isNumber || "._-".contains($0)) ? $0 : "_" }) + "_3xw"
    }

    func hasFaces(_ package: String) -> Bool {
        guard let dir = root?.appending(path: Self.facesDir).appending(path: Self.folder(package)) else { return false }
        return FileManager.default.fileExists(atPath: dir.path(percentEncoded: false))
    }

    /// A picture's file: "<state or avatar>_<points>.png".
    static func file(_ name: String, _ size: Int) -> String { "\(name)_\(size).png" }

    /// One state's picture at the size a face of `points` reads, its file and whether it is the state's own (else the
    /// avatar, else the idle frame).
    func face(_ package: String, _ state: PersonaState, points: CGFloat) -> (png: Data, own: Bool, file: String)? {
        let dir = Self.facesDir + "/" + Self.folder(package) + "/"
        let size = FleetFaces.size(points)
        for (name, own) in [(state.rawValue, true), (Self.avatarName, false), (PersonaState.idle.rawValue, false)] {
            if let png = read(dir + Self.file(name, size)) { return (png, own, Self.file(name, size)) }
        }
        return nil
    }

    /// All of a package's pictures (tests).
    func faces(_ package: String) -> FleetFaces? {
        let dir = Self.facesDir + "/" + Self.folder(package) + "/"
        func picture(_ name: String) -> FleetFaces.Picture? {
            var out: FleetFaces.Picture = [:]
            for size in FleetFaces.sizes { out[size] = read(dir + Self.file(name, size)) }
            return out.isEmpty ? nil : out
        }
        var states: [String: FleetFaces.Picture] = [:]
        for s in PersonaState.allCases { states[s.rawValue] = picture(s.rawValue) }
        let avatar = picture(Self.avatarName)
        return states.isEmpty && avatar == nil ? nil : FleetFaces(package: package, states: states, avatar: avatar)
    }

    /// Replaces the saved faces with these (nil: none), whole: written beside the old ones, then moved in, so the
    /// extension never reads half of one package and half of another. True when they were.
    func save(_ faces: FleetFaces?) -> Bool {
        let name = Self.facesDir + (faces.map { "/" + Self.folder($0.package) } ?? "")
        let fm = FileManager.default
        let error: Error?
        do {
            guard let root else { throw CocoaError(.fileNoSuchFile) }
            let all = root.appending(path: Self.facesDir, directoryHint: .isDirectory)
            try fm.createDirectory(at: all, withIntermediateDirectories: true)
            var keep: String?
            if let faces {
                let folder = Self.folder(faces.package)
                let new = all.appending(path: ".new-" + folder, directoryHint: .isDirectory)
                try? fm.removeItem(at: new)
                try fm.createDirectory(at: new, withIntermediateDirectories: true)
                var pictures = faces.states.map { ($0.key, $0.value) }
                if let avatar = faces.avatar { pictures.append((Self.avatarName, avatar)) }
                for (name, picture) in pictures {
                    for (size, png) in picture {
                        try png.write(to: new.appending(path: Self.file(name, size)), options: .completeFileProtectionUntilFirstUserAuthentication)
                    }
                }
                let dest = all.appending(path: folder, directoryHint: .isDirectory)
                if fm.fileExists(atPath: dest.path(percentEncoded: false)) { try fm.removeItem(at: dest) }
                try fm.moveItem(at: new, to: dest)
                keep = folder
            }
            for other in try fm.contentsOfDirectory(atPath: all.path(percentEncoded: false)) where other != keep {
                try fm.removeItem(at: all.appending(path: other))
            }
            error = nil
        } catch let e {
            error = e
        }
        let bytes = faces.map { f in (Array(f.states.values) + [f.avatar].compactMap { $0 }).reduce(0) { $0 + $1.values.reduce(0) { $0 + $1.count } } }
        Self.wrote(name, error, bytes: bytes)
        return error == nil
    }

    /// One file as the container lists it.
    struct File: Hashable, Sendable {
        var name: String
        var bytes: Int
        var modified: Date?
    }

    /// Every file under `Fleet/`, by path; nil without a container.
    func files() -> [File]? {
        guard let root, let walk = FileManager.default.enumerator(at: root, includingPropertiesForKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]) else {
            return root == nil ? nil : []
        }
        let base = root.standardizedFileURL.path(percentEncoded: false)
        var found: [File] = []
        for case let url as URL in walk {
            guard let v = try? url.resourceValues(forKeys: [.fileSizeKey, .contentModificationDateKey, .isRegularFileKey]), v.isRegularFile == true else { continue }
            let path = url.standardizedFileURL.path(percentEncoded: false)
            found.append(File(name: path.hasPrefix(base) ? String(path.dropFirst(base.count)).trimmingCharacters(in: ["/"]) : url.lastPathComponent,
                              bytes: v.fileSize ?? 0, modified: v.contentModificationDate))
        }
        return found.sorted { $0.name < $1.name }
    }

    /// The last write of each file in this process: name → (when, the error or nil, bytes).
    nonisolated(unsafe) private static var writes: [String: (at: Date, error: String?, bytes: Int?)] = [:]
    private static let lock = NSLock()

    static var lastWrites: [String: (at: Date, error: String?, bytes: Int?)] { lock.withLock { writes } }

    private static func wrote(_ name: String, _ error: Error?, bytes: Int?) {
        if let error {
            log.error("write \(name, privacy: .public), \(bytes ?? 0) bytes: \(error.localizedDescription, privacy: .public)")
        } else {
            log.notice("wrote \(name, privacy: .public), \(bytes ?? 0) bytes")
        }
        lock.withLock { writes[name] = (.now, error?.localizedDescription, bytes) }
    }
}

/// What the widget extension last drew from, written by it into the container (`seen.json`) and shown on Dynamic Island Diagnostics:
/// whether it has the container, the look saved there, and the package picture it read. Written only when it changes
/// or ten minutes on, never on every render.
nonisolated struct FleetLookReport: Codable, Hashable, Sendable {
    var at = Date.distantPast
    /// The look in the activity's content ("design" or "design · package"), nil when it carried none.
    var content: String?
    /// Whether the extension has the App Group container.
    var container: Bool?
    /// The look saved there, nil when there is none it can read.
    var saved: String?
    /// The package whose picture it looked for last (nil: a built-in, none looked for), the file it read (nil: none
    /// there) and its size.
    var facesPackage: String?
    var faceFile: String?
    var faceBytes: Int?

    mutating func content(_ look: FleetLook?) {
        content = look.map { Self.describe($0) }
    }

    static func describe(_ look: FleetLook) -> String { [look.design, look.package].compactMap { $0 }.joined(separator: " · ") }

    mutating func saved(_ container: Bool, _ look: FleetLook?) {
        (self.container, saved) = (container, look.map { Self.describe($0) })
    }

    mutating func face(_ package: String, _ read: (file: String, bytes: Int)?) {
        (facesPackage, faceFile, faceBytes) = (package, read?.file, read?.bytes)
    }

    /// The same reading, whenever it was.
    func same(_ other: FleetLookReport) -> Bool {
        var a = self, b = other
        (a.at, b.at) = (.distantPast, .distantPast)
        return a == b
    }

    /// What the extension wrote last; nil when it never did (or this bundle cannot read it).
    static var seen: FleetLookReport? {
        FleetLookStore.shared.read(FleetLookStore.seenFile).flatMap { try? JSONDecoder().decode(FleetLookReport.self, from: $0) }
    }

    nonisolated(unsafe) private static var current = FleetLookReport()
    nonisolated(unsafe) private static var written: FleetLookReport?
    private static let lock = NSLock()
    private static let inExtension = Bundle.main.bundleURL.pathExtension == "appex"

    /// Records a reading in the widget extension (the app's own readings are not the extension's) and writes the report
    /// when it says something new.
    static func note(_ change: (inout FleetLookReport) -> Void) {
        guard inExtension else { return }
        let due: FleetLookReport? = lock.withLock {
            change(&current)
            if let w = written, w.same(current), Date.now.timeIntervalSince(w.at) < 600 { return nil }
            current.at = .now
            written = current
            return current
        }
        guard let due else { return }
        FleetLookStore.log.notice("extension drew \(due.content ?? "no look in the content", privacy: .public); container \(due.container == true ? "yes" : "no", privacy: .public), saved \(due.saved ?? "-", privacy: .public), face \(due.facesPackage ?? "-", privacy: .public) \(due.faceFile ?? "none", privacy: .public) \(due.faceBytes ?? 0) bytes")
        if let data = try? JSONEncoder().encode(due) { _ = FleetLookStore.shared.write(data, FleetLookStore.seenFile) }
    }
}

/// The Fleet's state as the persona's (PersonaState): the face says what the counts say.
nonisolated enum FleetPersona {
    /// - needs you → Waiting for you (red ring); working → Working; finished-unseen or nothing → Idle;
    /// - only the assistant answering (no agent in the fleet working) → Thinking: the nearest the fleet has to it;
    /// - stale (the hub stopped updating it) → Offline, gray: the face of "this may be out of date". The fleet has no
    ///   error state of its own; a hub that cannot be reached is the error it shows.
    static func state(_ s: FleetState, stale: Bool) -> PersonaState {
        if stale { return .offline }
        switch s.phase {
        case .needsYou: return .needsYou
        case .working: return .working
        case .done, .none: return s.assistant ? .thinking : .idle
        }
    }
}

/// The persona on the activity: its still frame for the state (a Live Activity runs no timelines or repeating
/// effects, docs/apple/live-activity.md「Pitfalls」), changing with a cross-fade when the state does. A package draws its picture
/// for the state; a state it has no frames for gets the built-ins' cue (the red ring, gray when offline). Its
/// drawing in its color while the faces cannot be read.
struct FleetFace: View {
    var size: CGFloat
    var state: PersonaState
    var look: FleetLook = .saved
    /// The package's faces; the state's picture is read from where the app left it when not given.
    var faces: FleetFaces?

    var body: some View {
        let look = look.supported
        let matchingFaces = faces.flatMap { $0.package == look.package ? $0 : nil }
        Group {
            if let face = matchingFaces?.face(state, points: size) ?? look.face(state, points: size),
               let image = FleetFaces.image(face.png, points: CGFloat(FleetFaces.size(size))) {
                Image(uiImage: image)
                    .resizable()
                    .interpolation(.high)
                    .scaledToFill()
                    .frame(width: size, height: size)
                    .clipShape(.circle)
                    .grayscale(!face.own && state == .offline ? 1 : 0)
                    .opacity(!face.own && state == .offline ? PersonaFace.offlineOpacity : 1)
                    .overlay {
                        if !face.own && state == .needsYou {
                            Circle().strokeBorder(FleetPalette.needsYou, lineWidth: max(PersonaFace.ringMin, size * PersonaFace.ringShare))
                        }
                    }
            } else {
                PersonaFace(size: size, design: look.faceDesign, state: state)
            }
        }
        .frame(width: size, height: size)
        .id(state)
        .transition(.opacity)
        .accessibilityLabel("Persona \(state.title)")
    }
}
