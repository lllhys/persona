import Foundation

// Several managers on one hub (docs/assistant/managers.md "Clients"), as the app reads them outside Settings › Managers:
// each group's roster, read when its hub says hello and on each of its `managers` pushes. Pages that group a list read
// their own (`ManagerRoster.read`); this one is for what is decided outside such a page: whose copy of an extension a
// push is about, the inbox's groups, a computer whose manager is offline, and the manager the Persona page shows (its
// switcher). With one installed manager none of it shows.

extension AppModel {
    /// The roster of the hub-level group's hub; nil from a hub before managers, or until it is read.
    var managerRoster: ManagerRoster? { managerRosters[hub?.group ?? ""] }

    /// Reads group's roster again. A hub without one (404) has none; one that can't be read now keeps the last.
    func refreshManagerRoster(_ group: String) async {
        guard let api = api(group) else { return }
        let read = managerRosterReads[group, default: 0] + 1
        managerRosterReads[group] = read
        let roster = await ManagerRoster.read(api, keeping: managerRosters[group])
        guard read == managerRosterReads[group], managerRosters[group] != roster else { return }
        let was = managerRosters[group].map { shownOwner($0, group: group) }
        let wasOffline = group == hub?.group && assistantManagerOffline != nil
        // Another manager is the default now: what was held as the default's settings is the one before's.
        let defaultMoved = managerRosters[group] != nil && roster != nil && managerRosters[group]?.defaultID != roster?.defaultID
        if defaultMoved { assistantByGroup[group] = nil }
        managerRosters[group] = roster
        // A pick of a manager that left goes with it: the page doesn't jump to it should it come back.
        assistantPicks[group] = roster.flatMap { $0.isMulti ? $0.named(assistantPicks[group]) : nil }
        // Another manager's page stays shown: Health and the channels read the new default's settings all the same.
        if defaultMoved, assistantPicks[group] != nil, let moved = roster?.defaultID {
            Task {
                guard let st = try? await api.manager(nil).assistant(), managerRosters[group]?.defaultID == moved,
                      assistantPick(in: group) != nil else { return }
                assistantByGroup[group] = st
            }
        }
        guard group == hub?.group else { return }
        // The manager the Persona page showed left the roster (or the hub has one manager again), the default moved, or the
        // shown one is back online: the page reads its manager again.
        let now = roster.map { shownOwner($0, group: group) }
        let back = wasOffline && assistantManagerOffline == nil
        if defaultMoved || back || (was != nil && was != now) { assistantManagerChanged() } else { relistAssistant() }
    }

    /// The manager group's Persona page shows by `roster`.
    private func shownOwner(_ roster: ManagerRoster, group: String) -> String {
        roster.isMulti ? roster.owner(assistantPicks[group]) : roster.defaultID
    }

    /// The manager the Persona page shows, when another than the default: its switcher's pick while the roster has it
    /// installed. nil for the default, and always with one manager.
    var assistantManagerID: String? { assistantPick(in: hub?.group ?? "") }

    /// `assistantManagerID` for group's Persona page.
    func assistantPick(in group: String) -> String? {
        guard let roster = managerRosters[group], roster.isMulti, let pick = assistantPicks[group] else { return nil }
        return roster.named(pick)
    }

    /// The manager the Persona page shows, as the roster has it, on a hub with several managers; nil with one.
    var assistantManager: HubManager? {
        guard let roster = managerRoster, roster.isMulti else { return nil }
        let id = roster.owner(assistantManagerID)
        return roster.installed.first { $0.id == id }
    }

    /// That manager while it is offline: its page shows what was read of it in this run, and nothing is sent to it.
    var assistantManagerOffline: HubManager? { assistantManager.flatMap { $0.online ? nil : $0 } }

    /// Whether a thing stamped `manager` is the Persona page's manager's (everything is, with one manager).
    func assistantShows(_ manager: String?) -> Bool { sameManager(manager, assistantManagerID) }

    /// Whether things stamped `a` and `b` are one manager's (everything is, with one manager).
    func sameManager(_ a: String?, _ b: String?) -> Bool {
        guard let roster = managerRoster, roster.isMulti else { return true }
        return roster.owner(a) == roster.owner(b)
    }

    /// The Persona page's manager for its lists; nil with one manager.
    var assistantShown: AssistantLists.Shown? {
        guard let roster = managerRoster, roster.isMulti else { return nil }
        return AssistantLists.Shown(roster: roster, manager: roster.owner(assistantManagerID))
    }

    /// The Persona page shows manager `id` from now on (nil, or the default's id: the default): its settings,
    /// conversations and readiness are that manager's. Nothing with one manager.
    func selectAssistantManager(_ id: String?) {
        guard let group = hub?.group, let roster = managerRosters[group], roster.isMulti else { return }
        let next = roster.named(id)
        let was = assistantManagerID
        assistantPicks[group] = next
        if next != was { assistantManagerChanged() }
    }

    /// Whether `api` was made for the Persona page as it is now (its group and manager): what it read lands.
    func showsPage(of api: APIClient) -> Bool { api.group == hub?.group && api.shownManager == assistantManagerID }

    #if DEBUG
    /// `-openAssistantManager <id>`: the Persona page on that manager once the hub's roster is in (screenshots, UI tests).
    func openAssistantManagerWhenListed(_ id: String) {
        selectedTab = .assistant
        Task {
            for _ in 0..<40 where managerRoster?.isMulti != true { try? await Task.sleep(for: .milliseconds(250)) }
            selectAssistantManager(id)
        }
    }
    #endif

    /// `assistantByManager`'s key.
    static func managerKey(_ group: String, _ manager: String) -> String { group + "\u{0}" + manager }

    /// This device's prefs as a page just saved them, for what reads them here (the Mac's banners, the Live
    /// Activities); a prefs read already on its way doesn't put the old ones back.
    func savedNotifyPrefs(_ prefs: NotifyPrefs) {
        notifyPrefsWrites += 1
        LiveActivityController.shared.settings(prefs)
    }

    /// Whose copy a hub-level push is about, as the pages name managers (`ManagerRoster.named`): nil for the default
    /// manager's, else its id. A push without `manager` is the hub's own assistant's, the default until the roster says
    /// otherwise.
    func pushedBy(_ manager: String?) -> String? { ManagerRoster.pushed(by: manager, defaultID: managerRoster?.defaultID) }

    /// The manager of `machine` while a hub with several managers has that manager offline: the computer comes back
    /// with it, and Reconnect, which goes through it, can't work meanwhile. nil otherwise.
    func offlineManager(of machine: Machine?, group: String) -> HubManager? {
        guard let machine, !machine.online else { return nil }
        return managerRosters[group]?.offlineManager(of: machine.manager)
    }

    /// The manager of conversation `session` while a hub with several managers has it offline: nothing goes to it until
    /// it is back, wherever the conversation was opened. nil otherwise.
    func offlineManager(of session: Session?) -> HubManager? {
        guard let session else { return nil }
        return managerRosters[session.group]?.offlineManager(of: session.manager)
    }
}

extension ManagerRoster {
    /// `AppModel.pushedBy` with the default manager's id, nil until a roster is read.
    static func pushed(by manager: String?, defaultID: String?) -> String? {
        let id = manager ?? assistant
        return id == (defaultID ?? assistant) ? nil : id
    }

    /// The installed manager a thing stamped `manager` belongs to, when it is offline on a hub with several managers;
    /// nil otherwise (with one manager the pages show none of it).
    func offlineManager(of manager: String?) -> HubManager? {
        guard isMulti else { return nil }
        let id = owner(manager)
        return installed.first { $0.id == id && !$0.online }
    }
}
