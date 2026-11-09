#if os(iOS)
import SwiftUI

struct WorkoutReportSettingsView: View {
    @Environment(AppModel.self) private var model
    @State private var reports = WorkoutReports.shared

    var body: some View {
        SettingsForm {
            Section {
                SettingsSwitchRow(title: String(localized: "Automatic workout reports"),
                                  symbol: Theme.Icon.health, isOn: Binding(
                                    get: { reports.enabled },
                                    set: { value in Task { await reports.setEnabled(value, model: model) } }))
                .disabled(reports.busy || model.hub == nil || model.isDemo)
            } footer: {
                Text("Off by default. After you enable this, opening Persona checks for new workouts of at least 10 minutes and sends their measurements to your hub to create a report. Older workouts are not imported. Reports already submitted may finish after you turn this off.")
            }
            Section {
                Text("Reports contain measurement cards and a heart-rate chart when data is available, followed by a brief summary. Health read permissions are managed in iOS Settings; an empty result can also mean access was not granted.")
            }
            if let error = reports.error {
                Section { Text(error).foregroundStyle(Theme.caution) }
            }
        }
        .themedPage()
        .detailTitle(String(localized: "Health"))
    }
}
#endif
