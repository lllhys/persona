import AppKit
import ApplicationServices
import PersonaInputKit

// Phone Screen's window moves (#9314): a thin layer over the Accessibility API. Adapted from Snug's AX.swift and
// WindowService.swift (https://github.com/zephyr4123/snug, MIT, Copyright (c) 2026 AoyamaShou, Copyright (c) 2026
// zephyr4123; THIRD_PARTY_NOTICES.md):
// - a window is found again each time by pid + window number, never kept: an app that rebuilds a window makes the old
//   object stale;
// - a move sets size, then position, then size again and checks, up to 10 times: across displays an app clamps the size
//   to the display it thinks it is on, and a new display may not be known to it yet. The same frame twice means the app
//   (a minimum size) or macOS (the menu bar) holds it there, so it stops;
// - every call waits at most 1 s for an app that hangs (the system default is about 6 s).

/// A window by its owner and macOS window number.
struct WindowKey: Hashable, Sendable {
    var pid: pid_t
    var id: CGWindowID
}

enum AX {
    static let timeout: Float = 1
    static let retryDelay: TimeInterval = 0.25
    static let maxTries = 10
    static let fullScreen = "AXFullScreen"

    static var trusted: Bool { AXIsProcessTrusted() }

    static func configureTimeout() {
        AXUIElementSetMessagingTimeout(AXUIElementCreateSystemWide(), timeout)
    }

    static func attribute(_ e: AXUIElement, _ name: String) -> AnyObject? {
        var value: AnyObject?
        return AXUIElementCopyAttributeValue(e, name as CFString, &value) == .success ? value : nil
    }

    static func string(_ e: AXUIElement, _ name: String) -> String? { attribute(e, name) as? String }
    static func flag(_ e: AXUIElement, _ name: String) -> Bool { (attribute(e, name) as? Bool) == true }

    @discardableResult
    static func set(_ e: AXUIElement, _ name: String, _ value: AnyObject) -> Bool {
        AXUIElementSetAttributeValue(e, name as CFString, value) == .success
    }

    static func isStandard(_ e: AXUIElement) -> Bool { string(e, kAXSubroleAttribute) == kAXStandardWindowSubrole }

    /// The app's windows in the current Space (minimized ones included); nil when it does not answer.
    static func windows(pid: pid_t) -> [AXUIElement]? {
        var value: AnyObject?
        switch AXUIElementCopyAttributeValue(AXUIElementCreateApplication(pid), kAXWindowsAttribute as CFString, &value) {
        case .success: return (value as? [AXUIElement]) ?? []
        case .noValue: return []
        default: return nil
        }
    }

    static func windowID(_ e: AXUIElement) -> CGWindowID? {
        var id: CGWindowID = 0
        return _AXUIElementGetWindow(e, &id) == .success && id != 0 ? id : nil
    }

    /// The window again, by its key; nil when it is gone or its app does not answer.
    static func element(_ key: WindowKey) -> AXUIElement? {
        windows(pid: key.pid)?.first { windowID($0) == key.id }
    }

    static func frame(_ e: AXUIElement) -> CGRect? {
        guard let p = attribute(e, kAXPositionAttribute), let s = attribute(e, kAXSizeAttribute),
              CFGetTypeID(p) == AXValueGetTypeID(), CFGetTypeID(s) == AXValueGetTypeID() else { return nil }
        var origin = CGPoint.zero, size = CGSize.zero
        guard AXValueGetValue(p as! AXValue, .cgPoint, &origin), AXValueGetValue(s as! AXValue, .cgSize, &size) else { return nil }
        return CGRect(origin: origin, size: size)
    }

    static func setPosition(_ e: AXUIElement, _ point: CGPoint) {
        var p = point
        if let v = AXValueCreate(.cgPoint, &p) { set(e, kAXPositionAttribute, v) }
    }

    static func setSize(_ e: AXUIElement, _ size: CGSize) {
        var s = size
        if let v = AXValueCreate(.cgSize, &s) { set(e, kAXSizeAttribute, v) }
    }

    /// Size, position, size, then check; again until it holds or the app's minimum size stops it. The frame it took.
    @discardableResult
    static func move(_ e: AXUIElement, to target: CGRect) -> CGRect? {
        var got: CGRect?
        for _ in 0..<maxTries {
            setSize(e, target.size)
            setPosition(e, target.origin)
            setSize(e, target.size)
            Thread.sleep(forTimeInterval: retryDelay)
            let previous = got
            got = frame(e)
            guard let now = got, !now.near(target) else { break }
            // The same frame twice: the app or macOS holds it there (a minimum size, the menu bar), trying again won't move it.
            if let previous, now.near(previous) { break }
        }
        return got
    }

    /// Full screen on or off, waiting for the change (a window still animating ignores it) and for the animation.
    static func setFullScreen(_ e: AXUIElement, _ on: Bool) {
        guard flag(e, fullScreen) != on else { return }
        wait(seconds: 5) {
            if flag(e, fullScreen) == on { return true }
            set(e, fullScreen, on as CFBoolean)
            return false
        }
        Thread.sleep(forTimeInterval: 1.2)
    }

    static func setMinimized(_ e: AXUIElement, _ on: Bool) {
        guard flag(e, kAXMinimizedAttribute) != on else { return }
        set(e, kAXMinimizedAttribute, on as CFBoolean)
        wait(seconds: 3) { flag(e, kAXMinimizedAttribute) == on }
        Thread.sleep(forTimeInterval: 0.5)
    }

    /// In front and the app's main window, then the app frontmost, so typing goes to it. Through Accessibility: macOS 14+
    /// ignores `NSRunningApplication.activate()` from a helper in the background.
    static func focus(_ e: AXUIElement, pid: pid_t) {
        AXUIElementPerformAction(e, kAXRaiseAction as CFString)
        set(e, kAXMainAttribute, kCFBooleanTrue)
        if !set(AXUIElementCreateApplication(pid), kAXFrontmostAttribute, kCFBooleanTrue) {
            NSRunningApplication(processIdentifier: pid)?.activate()
        }
    }

    /// ⌘+ (`steps` > 0) or ⌘− (< 0) to the app alone (`postToPid`, nothing global), a key every 80 ms. The app gives them
    /// to its front window, so the window is raised first.
    static func zoom(pid: pid_t, steps: Int) {
        let key: CGKeyCode = steps > 0 ? 0x18 : 0x1B // kVK_ANSI_Equal, kVK_ANSI_Minus
        for _ in 0..<abs(steps) {
            for down in [true, false] {
                guard let event = CGEvent(keyboardEventSource: nil, virtualKey: key, keyDown: down) else { continue }
                event.flags = .maskCommand
                event.postToPid(pid)
            }
            Thread.sleep(forTimeInterval: 0.08)
        }
    }

    static func wait(seconds: TimeInterval, until done: () -> Bool) {
        let deadline = ProcessInfo.processInfo.systemUptime + seconds
        while !done(), ProcessInfo.processInfo.systemUptime < deadline { Thread.sleep(forTimeInterval: 0.25) }
    }
}

/// The window server's windows on screen, front to back (owner, number, frame): no permission needed for these.
enum WindowServer {
    struct Entry { var key: WindowKey; var frame: CGRect }

    static func onScreen() -> [Entry] {
        let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]]
        return (list ?? []).compactMap { info in
            guard (info[kCGWindowLayer as String] as? NSNumber)?.intValue == 0,
                  let pid = (info[kCGWindowOwnerPID as String] as? NSNumber)?.int32Value,
                  let id = (info[kCGWindowNumber as String] as? NSNumber)?.uint32Value,
                  let bounds = info[kCGWindowBounds as String] as? NSDictionary,
                  let frame = CGRect(dictionaryRepresentation: bounds as CFDictionary) else { return nil }
            return Entry(key: WindowKey(pid: pid, id: id), frame: frame)
        }
    }
}

extension CGRect {
    /// Within 2 points on every edge.
    func near(_ o: CGRect) -> Bool {
        abs(minX - o.minX) < 2 && abs(minY - o.minY) < 2 && abs(width - o.width) < 2 && abs(height - o.height) < 2
    }

    var phoneRect: PhoneScreenRect { PhoneScreenRect(x: minX, y: minY, width: width, height: height) }

    init(_ r: PhoneScreenRect) { self.init(x: r.x, y: r.y, width: r.width, height: r.height) }
}

/// An Accessibility window object's window number (private; yabai, Rectangle and AltTab use it).
@_silgen_name("_AXUIElementGetWindow")
private func _AXUIElementGetWindow(_ element: AXUIElement, _ id: UnsafeMutablePointer<CGWindowID>) -> AXError
