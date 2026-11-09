package prompts

// RenderPhoneQueuedWrite adds the hub's optional delivery contract to a phone's description.
func RenderPhoneQueuedWrite(description string) string {
	return render("tools/phone_queued_write", description)
}

// RenderPhoneQueueOption describes the argument consumed by the hub before forwarding to a phone.
func RenderPhoneQueueOption() string { return render("tools/phone_queue_option", nil) }
