package connector

import "testing"

// A tool's name is split into words however it is written; only a whole word that writes refuses it.
func TestWriteVerb(t *testing.T) {
	for name, want := range map[string]string{
		"list_messages":         "",
		"search_messages":       "",
		"get_message_by_id":     "",
		"messages.read":         "",
		"listUnreadMessages":    "",
		"fetch-calendar-events": "",
		"sendMessage":           "send",
		"messages_send":         "send",
		"DeleteEvent":           "delete",
		"create-draft":          "create",
		"place_order":           "place",
		"get_unsubscribe_link":  "unsubscribe",
		"HTTPPostUpdate":        "update",
		"resend":                "",
		"settings_list":         "",
		"list_sent_items":       "",
	} {
		if got := WriteVerb(name); got != want {
			t.Errorf("WriteVerb(%q) = %q, want %q", name, got, want)
		}
	}
}
