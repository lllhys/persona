import SwiftUI
import PersonaWire

/// A machine's OS as a small mark (docs/server/machines.md「System information」): the Apple logo for macOS, each Linux distribution's
/// own mark (Simple Icons, `Theme.Brand.os`) in its color, a generic Linux penguin for the rest.
struct OSBadge: View {
    var system: MachineSystem?
    /// The machine's own symbol, for one that never said what it runs.
    var fallback: String = Theme.Icon.machine
    var size: CGFloat = 44
    var online = true

    var body: some View {
        let family = system?.family
        let tint = online ? (family.map(Self.color) ?? Theme.accent) : Color.secondary
        ZStack {
            RoundedRectangle(cornerRadius: size * 0.27, style: .continuous)
                .fill(tint.opacity(0.13))
            mark(family, tint: tint)
                .frame(width: size * 0.56, height: size * 0.56)
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true) // the OS name is written next to it everywhere; a card's label starts with the machine
    }

    @ViewBuilder private func mark(_ f: OSFamily?, tint: Color) -> some View {
        if f == .macos {
            Image(systemName: "apple.logo").resizable().scaledToFit().padding(size * 0.02).foregroundStyle(online ? Color.primary : .secondary)
        } else if let name = f.flatMap(Theme.Brand.os) {
            Image(name).resizable().scaledToFit().foregroundStyle(tint)
        } else {
            Image(systemName: fallback).resizable().scaledToFit().foregroundStyle(tint)
        }
    }

    /// The distributions' own colors, stepped for each appearance.
    static func color(_ f: OSFamily) -> Color {
        switch f {
        case .macos: Color.primary
        case .ubuntu: Color(light: 0xE95420, dark: 0xF26B3A)
        case .debian: Color(light: 0xA80030, dark: 0xE0245E)
        case .fedora: Color(light: 0x3C6EB4, dark: 0x51A2DA)
        case .redhat: Color(light: 0xCC0000, dark: 0xFF4D4D)
        case .centos: Color(light: 0x932279, dark: 0xC14FA8)
        case .rocky: Color(light: 0x10B981, dark: 0x34D399)
        case .alma: Color(light: 0x0F4266, dark: 0x6CA6D9)
        case .arch: Color(light: 0x1793D1, dark: 0x3BAEE8)
        case .alpine: Color(light: 0x0D597F, dark: 0x5AA9D6)
        case .opensuse: Color(light: 0x4E8A1E, dark: 0x73BA25)
        case .nixos: Color(light: 0x5277C3, dark: 0x7EBAE4)
        case .linux, .other: Theme.accent
        }
    }
}
