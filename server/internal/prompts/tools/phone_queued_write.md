{{.}}

If the phone is offline and this write can wait for the user to open Persona and confirm it, explicitly set queue_if_offline=true. The hub then saves it and returns a queue receipt immediately, not a completed write. Use phone_write_queue_list to check its state and phone_write_queue_cancel to cancel it before delivery. Defaults to false: ordinary calls keep their existing behavior. Never enqueue a retry of an uncertain write without checking the phone first.
