package persona.core.contracts.api

/**
 * Takes back what the assistant added on this phone, for the Undo on a quick result in its conversation (iOS
 * `CalendarTool.remove` in `TasksStore.undo(_:)`). It deletes directly instead of going through
 * `phone_calendar_delete_event`: the user is undoing the assistant's own write, just shown to them, so no second
 * before → after card asks again (docs/apple/phone-tools.md, "Confirmation flow").
 */
fun interface PhoneQuickResults {
    /** Deletes this phone's calendar event [id]; throws when the calendar refuses or the event is not on this phone. */
    suspend fun removeCalendarEvent(id: Long)
}
