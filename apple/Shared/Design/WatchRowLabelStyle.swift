import SwiftUI

/// A row's label on the watch's home list (all-day recording, Mark Meeting, Meeting Recording, Meeting Minutes,
/// Record): its symbol in one column for every row (`Theme.Size.icon`, larger with the text), the title after it, so the
/// titles start at the same place. A symbol wider than the column (Mark Meeting's people with sound waves) is drawn a
/// size smaller to fit, where the system's list label let it run into the title (#6046). The phone does not use it;
/// its tests draw it (`WatchRowLabelStyleTests`).
struct WatchRowLabelStyle: LabelStyle {
    @ScaledMetric(relativeTo: .subheadline) private var column = Theme.Size.icon

    func makeBody(configuration: Configuration) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.sm) {
            ViewThatFits(in: .horizontal) {
                configuration.icon
                configuration.icon.imageScale(.small)
            }
            .frame(width: column)
            configuration.title
        }
    }
}

extension LabelStyle where Self == WatchRowLabelStyle {
    static var watchRow: WatchRowLabelStyle { WatchRowLabelStyle() }
}
